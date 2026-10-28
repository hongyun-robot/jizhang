package service

import (
	"context"
	"crypto/rand"
	"errors"
	"github.com/golang-jwt/jwt/v5"
	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"
	"io"
	"jizhang/internal/model"
	"strconv"
	"time"
)

var (
	ErrInvalidCredentials = errors.New("用户名或密码错误")
	ErrUserDisabled       = errors.New("账号已停用")
	ErrRefreshInvalid     = errors.New("登录已过期，请重新登录")
	ErrSessionReplaced    = errors.New("账号已在另一台同类设备登录，请重新登录")
	ErrAccessExpired      = errors.New("访问令牌已过期")
	ErrRefreshConflict    = errors.New("会话正在刷新，请稍后重试")
	ErrAuthUnavailable    = errors.New("认证服务暂不可用，请重试")
	ErrAuthChanged        = errors.New("登录状态已变化，请重试")
	ErrRateLimited        = errors.New("尝试次数过多，请稍后再试")
)

const (
	loginFailUserThreshold = 5
	loginFailIPThreshold   = 20
	loginFailTTL           = 10 * time.Minute
)

type UserStore interface {
	GetByUsername(username string) (*model.User, error)
	GetByID(id int64) (*model.User, error)
}
type AuthClaims struct {
	Version int        `json:"ver"`
	UID     int64      `json:"uid"`
	Role    model.Role `json:"role"`
	SID     string     `json:"sid"`
	Device  DeviceType `json:"device"`
	Epoch   int64      `json:"ep"`
	jwt.RegisteredClaims
}

func (c *AuthClaims) ref() sessionRef { return sessionRef{c.UID, c.Device, c.SID} }

type AuthResult struct {
	AccessToken  string
	RefreshToken string
	SessionID    string
	DeviceType   DeviceType
	User         *model.User
	ExpiresAt    int64
}
type AuthService struct {
	store        UserStore
	rdb          *redis.Client
	sessions     authSessionStore
	secret       []byte
	accessTTL    time.Duration
	refreshTTL   time.Duration
	cookieSecure bool
	cookiePath   string
	now          func() time.Time
	random       io.Reader
	ratePrefix   string
}

func NewAuthService(store UserStore, rdb *redis.Client, secret string, accessTTL, refreshTTL time.Duration, secure bool, path string) *AuthService {
	if path == "" {
		path = "/api/v1/auth"
	}
	return &AuthService{store: store, rdb: rdb, sessions: authSessionStore{rdb, "auth:v2:"}, secret: []byte(secret), accessTTL: accessTTL, refreshTTL: refreshTTL, cookieSecure: secure, cookiePath: path, now: time.Now, random: rand.Reader}
}
func (s *AuthService) CookieSecure() bool        { return s.cookieSecure }
func (s *AuthService) CookiePath() string        { return s.cookiePath }
func (s *AuthService) RefreshTTL() time.Duration { return s.refreshTTL }
func (s *AuthService) AccessTTL() time.Duration  { return s.accessTTL }

func (s *AuthService) limited(ctx context.Context, key string, threshold int) (bool, error) {
	n, err := s.rdb.Get(ctx, key).Int()
	if errors.Is(err, redis.Nil) {
		return false, nil
	}
	if err != nil {
		return false, authUnavailable(err)
	}
	return n >= threshold, nil
}
func (s *AuthService) recordFailure(ctx context.Context, keys ...string) {
	for _, key := range keys {
		n, err := s.rdb.Incr(ctx, key).Result()
		if err == nil && n == 1 {
			s.rdb.Expire(ctx, key, loginFailTTL)
		}
	}
}

// Login prepares both credentials before atomically replacing the device slot.
func (s *AuthService) Login(ctx context.Context, username, password, ip string, device DeviceType) (*AuthResult, error) {
	if !device.valid() {
		return nil, ErrInvalidCredentials
	}
	userKey, ipKey := s.ratePrefix+"login_fail:"+username, s.ratePrefix+"login_fail_ip:"+ip
	for key, threshold := range map[string]int{userKey: loginFailUserThreshold, ipKey: loginFailIPThreshold} {
		limited, err := s.limited(ctx, key, threshold)
		if err != nil {
			return nil, err
		}
		if limited {
			return nil, ErrRateLimited
		}
	}
	locator, err := s.store.GetByUsername(username)
	if err != nil {
		return nil, authUnavailable(err)
	}
	if locator == nil {
		s.recordFailure(ctx, userKey, ipKey)
		return nil, ErrInvalidCredentials
	}
	ep, err := s.sessions.epoch(ctx, locator.ID)
	if err != nil {
		return nil, err
	}
	u, err := s.store.GetByID(locator.ID)
	if err != nil {
		return nil, authUnavailable(err)
	}
	if u == nil || bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) != nil {
		s.recordFailure(ctx, userKey, ipKey)
		return nil, ErrInvalidCredentials
	}
	if !u.IsActive() {
		return nil, ErrUserDisabled
	}
	sid, err := randomHex(s.random, 16)
	if err != nil {
		return nil, authUnavailable(err)
	}
	ref := sessionRef{u.ID, device, sid}
	now := s.now()
	result, record, err := s.prepare(u, ref, ep, now)
	if err != nil {
		return nil, err
	}
	if err := s.sessions.mutate(ctx, "login", ref, strconv.FormatInt(ep, 10), "", record, now, false); err != nil {
		return nil, err
	}
	s.rdb.Del(ctx, userKey)
	return result, nil
}
func (s *AuthService) prepare(u *model.User, ref sessionRef, ep int64, now time.Time) (*AuthResult, *authSession, error) {
	refresh, err := newRefreshToken(s.random, ref)
	if err != nil {
		return nil, nil, authUnavailable(err)
	}
	claims := &AuthClaims{Version: AuthProtocolVersion, UID: u.ID, Role: u.Role, SID: ref.SID, Device: ref.Device, Epoch: ep, RegisteredClaims: jwt.RegisteredClaims{Issuer: "jizhang", ID: refreshDigest(refresh), IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(s.accessTTL))}}
	access, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.secret)
	if err != nil {
		return nil, nil, authUnavailable(err)
	}
	return &AuthResult{AccessToken: access, RefreshToken: refresh, SessionID: ref.SID, DeviceType: ref.Device, User: u, ExpiresAt: claims.ExpiresAt.Unix()}, &authSession{SID: ref.SID, Epoch: ep, Digest: refreshDigest(refresh), CreatedAt: now.UnixMilli(), RefreshedAt: now.UnixMilli(), ExpiresAt: now.Add(s.refreshTTL).UnixMilli()}, nil
}

// Verify structure and signature even for expired access, then check its live slot.
func (s *AuthService) parseAccess(token string) (*AuthClaims, bool, error) {
	c := &AuthClaims{}
	parsed, err := jwt.ParseWithClaims(token, c, func(_ *jwt.Token) (interface{}, error) { return s.secret, nil }, jwt.WithValidMethods([]string{"HS256"}), jwt.WithoutClaimsValidation())
	if err != nil || !parsed.Valid || c.Version != AuthProtocolVersion || c.UID <= 0 || !validHex(c.SID, 16) || !c.Device.valid() || c.Issuer != "jizhang" || c.ExpiresAt == nil || c.IssuedAt == nil || c.Epoch < 0 {
		return nil, false, ErrRefreshInvalid
	}
	now := s.now()
	if c.IssuedAt.After(now) || (c.NotBefore != nil && c.NotBefore.After(now)) {
		return nil, false, ErrRefreshInvalid
	}
	return c, !now.Before(c.ExpiresAt.Time), nil
}
func (s *AuthService) Authenticate(token string) (*model.User, error) {
	return s.AuthenticateContext(context.Background(), token)
}
func (s *AuthService) AuthenticateContext(ctx context.Context, token string) (*model.User, error) {
	u, _, err := s.AuthenticateSession(ctx, token)
	return u, err
}

func (s *AuthService) AuthenticateSession(ctx context.Context, token string) (*model.User, *AuthClaims, error) {
	c, expired, err := s.parseAccess(token)
	if err != nil {
		return nil, nil, err
	}
	record, err := s.sessions.read(ctx, c.ref(), s.now())
	if err != nil {
		return nil, nil, err
	}
	if record.Epoch != c.Epoch {
		return nil, nil, ErrRefreshInvalid
	}
	u, err := s.store.GetByID(c.UID)
	if err != nil {
		return nil, nil, authUnavailable(err)
	}
	if u == nil || !u.IsActive() {
		return nil, nil, ErrUserDisabled
	}
	if expired {
		return nil, nil, ErrAccessExpired
	}
	return u, c, nil
}
func (s *AuthService) Refresh(ctx context.Context, token, expectedSID string) (*AuthResult, error) {
	ref, err := parseRefresh(token)
	if err != nil {
		return nil, err
	}
	if expectedSID != "" && expectedSID != ref.SID {
		return nil, ErrRefreshInvalid
	}
	now := s.now()
	current, err := s.sessions.read(ctx, ref, now)
	if err != nil {
		// An unsigned refresh locator cannot prove an older session existed.
		if errors.Is(err, ErrSessionReplaced) {
			return nil, ErrRefreshInvalid
		}
		return nil, err
	}
	u, err := s.store.GetByID(ref.UID)
	if err != nil {
		return nil, authUnavailable(err)
	}
	if u == nil || !u.IsActive() {
		return nil, ErrUserDisabled
	}
	next, record, err := s.prepare(u, ref, current.Epoch, now)
	if err != nil {
		return nil, err
	}
	record.CreatedAt = current.CreatedAt
	record.Previous = refreshDigest(token)
	record.PreviousUntil = min(current.ExpiresAt, now.Add(time.Minute).UnixMilli())
	if err := s.sessions.mutate(ctx, "refresh", ref, strconv.FormatInt(current.Epoch, 10), refreshDigest(token), record, now, false); err != nil {
		return nil, err
	}
	return next, nil
}

// Logout verifies the still-current actor in the same atomic operation as revocation.
func (s *AuthService) Logout(ctx context.Context, refresh, bearer, expectedSID string) error {
	if !validHex(expectedSID, 16) {
		return ErrRefreshInvalid
	}
	var ref sessionRef
	var claims *AuthClaims
	if refresh != "" {
		var err error
		ref, err = parseRefresh(refresh)
		if err != nil {
			return err
		}
	}
	if bearer != "" {
		c, expired, err := s.parseAccess(bearer)
		if err != nil {
			return err
		}
		if refresh != "" && ref != c.ref() {
			return ErrRefreshInvalid
		}
		if !expired {
			claims = c
		}
		if refresh == "" {
			ref = c.ref()
		}
	}
	if ref.SID != expectedSID || (refresh == "" && claims == nil) {
		return ErrRefreshInvalid
	}
	ep := ""
	if claims != nil {
		ep = strconv.FormatInt(claims.Epoch, 10)
	}
	err := s.sessions.mutate(ctx, "logout", ref, ep, refreshDigest(refresh), nil, s.now(), claims != nil)
	if claims == nil && errors.Is(err, ErrSessionReplaced) {
		return ErrRefreshInvalid
	}
	return err
}
func (s *AuthService) RevokeAll(ctx context.Context, uid int64) error {
	if uid <= 0 {
		return ErrRefreshInvalid
	}
	return s.sessions.mutate(ctx, "revoke", sessionRef{UID: uid}, "", "", nil, s.now(), false)
}

package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"

	"jizhang/internal/model"
)

// 认证相关业务错误（handler 据此映射 HTTP 状态）。
var (
	ErrInvalidCredentials = errors.New("用户名或密码错误")
	ErrUserDisabled       = errors.New("账号已停用")
	ErrRefreshInvalid     = errors.New("登录已过期，请重新登录")
	ErrRateLimited        = errors.New("尝试次数过多，请稍后再试")
)

const (
	loginFailUserThreshold = 5
	loginFailIPThreshold   = 20
	loginFailTTL           = 10 * time.Minute
)

// UserStore users 表读取依赖（repository.UserRepo 满足；测试可 mock）。
type UserStore interface {
	GetByUsername(username string) (*model.User, error)
	GetByID(id int64) (*model.User, error)
}

// AuthClaims JWT access token 载荷。Epoch 为会话代次：RevokeAll 递增后代次不匹配的旧 token 全部失效。
type AuthClaims struct {
	UID   int64      `json:"uid"`
	Role  model.Role `json:"role"`
	Epoch int64      `json:"ep,omitempty"`
	jwt.RegisteredClaims
}

// AuthService 登录、双 token 与限流。
type AuthService struct {
	store        UserStore
	rdb          *redis.Client
	secret       []byte
	accessTTL    time.Duration
	refreshTTL   time.Duration
	cookieSecure bool
	cookiePath   string
}

func NewAuthService(store UserStore, rdb *redis.Client, secret string,
	accessTTL, refreshTTL time.Duration, cookieSecure bool, cookiePath string) *AuthService {
	if cookiePath == "" {
		cookiePath = "/api/v1/auth"
	}
	return &AuthService{
		store:        store,
		rdb:          rdb,
		secret:       []byte(secret),
		accessTTL:    accessTTL,
		refreshTTL:   refreshTTL,
		cookieSecure: cookieSecure,
		cookiePath:   cookiePath,
	}
}

// CookieSecure refresh cookie 是否带 Secure 标志。
func (s *AuthService) CookieSecure() bool { return s.cookieSecure }

// CookiePath refresh cookie 的 Path（子路径部署时带 /jizhang 前缀）。
func (s *AuthService) CookiePath() string { return s.cookiePath }

// RefreshTTL 返回 refresh token 有效期（cookie MaxAge 用）。
func (s *AuthService) RefreshTTL() time.Duration { return s.refreshTTL }

// AccessTTL 返回 access token 有效期（前端倒计时用）。
func (s *AuthService) AccessTTL() time.Duration { return s.accessTTL }

// ---- 登录限流（Redis 计数，5 次封 10 分钟；IP 阈值更宽防止全家误伤） ----

func (s *AuthService) limited(ctx context.Context, key string, threshold int) bool {
	n, err := s.rdb.Get(ctx, key).Int()
	return err == nil && n >= threshold
}

func (s *AuthService) recordFailure(ctx context.Context, keys ...string) {
	for _, k := range keys {
		n, err := s.rdb.Incr(ctx, k).Result()
		if err == nil && n == 1 {
			s.rdb.Expire(ctx, k, loginFailTTL)
		}
	}
}

func (s *AuthService) clearFailures(ctx context.Context, keys ...string) {
	if len(keys) > 0 {
		s.rdb.Del(ctx, keys...)
	}
}

// Login 校验凭据并签发双 token。返回 access token 与用户。
func (s *AuthService) Login(ctx context.Context, username, password, ip string) (string, *model.User, error) {
	userKey := "login_fail:" + username
	ipKey := "login_fail_ip:" + ip
	if s.limited(ctx, userKey, loginFailUserThreshold) || s.limited(ctx, ipKey, loginFailIPThreshold) {
		return "", nil, ErrRateLimited
	}

	user, err := s.store.GetByUsername(username)
	if err != nil {
		return "", nil, err
	}
	if user == nil || bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) != nil {
		s.recordFailure(ctx, userKey, ipKey)
		return "", nil, ErrInvalidCredentials
	}
	if !user.IsActive() {
		return "", nil, ErrUserDisabled
	}
	s.clearFailures(ctx, userKey)

	access, err := s.issueAccess(user)
	if err != nil {
		return "", nil, err
	}
	return access, user, nil
}

// ---- access token ----

func (s *AuthService) issueAccess(u *model.User) (string, error) {
	// 记录签发时的会话代次（键不存在=0），吊销后代次不匹配即拒绝
	ep, err := s.rdb.Get(context.Background(), epochKey(u.ID)).Int64()
	if err != nil {
		ep = 0
	}
	claims := &AuthClaims{
		UID:   u.ID,
		Role:  u.Role,
		Epoch: ep,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(s.accessTTL)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "jizhang",
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.secret)
}

// Authenticate 解析 access token 并加载启用状态的用户；失败返回 error。
func (s *AuthService) Authenticate(token string) (*model.User, error) {
	claims := &AuthClaims{}
	parsed, err := jwt.ParseWithClaims(token, claims, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return s.secret, nil
	})
	if err != nil || !parsed.Valid {
		return nil, ErrRefreshInvalid
	}
	// 会话代次校验：RevokeAll（重置密码/停用/角色调整/refresh 盗用）会递增代次，
	// 代次不匹配的旧签发 token 一律失效；Redis 异常时放行（登录/刷新本身依赖 Redis，读路径保持可用）
	if cur, e := s.rdb.Get(context.Background(), epochKey(claims.UID)).Int64(); e == nil && claims.Epoch != cur {
		return nil, ErrRefreshInvalid
	}
	u, err := s.store.GetByID(claims.UID)
	if err != nil {
		return nil, err
	}
	if u == nil || !u.IsActive() {
		return nil, ErrUserDisabled
	}
	return u, nil
}

// ---- refresh token（随机不透明串，uid 前缀支持盗用时按用户吊销） ----

func newRefreshToken(uid int64) string {
	buf := make([]byte, 32)
	_, _ = rand.Read(buf)
	return strconv.FormatInt(uid, 10) + "." + hex.EncodeToString(buf)
}

func refreshKey(token string) string  { return "refresh:" + token }
func refreshUserKey(uid int64) string { return fmt.Sprintf("refresh_user:%d", uid) }

// epochKey 用户会话代次（不设 TTL，避免过期重建后旧代次复活）。键缺失视为 0。
func epochKey(uid int64) string { return fmt.Sprintf("session_epoch:%d", uid) }

// issueRefresh 签发 refresh token 并登记到用户会话集合。
func (s *AuthService) issueRefresh(ctx context.Context, u *model.User) (string, error) {
	token := newRefreshToken(u.ID)
	if err := s.rdb.Set(ctx, refreshKey(token), u.ID, s.refreshTTL).Err(); err != nil {
		return "", err
	}
	s.rdb.SAdd(ctx, refreshUserKey(u.ID), token)
	s.rdb.Expire(ctx, refreshUserKey(u.ID), s.refreshTTL)
	return token, nil
}

// IssueRefreshFor 对已登录用户签发 refresh token（登录流程用）。
func (s *AuthService) IssueRefreshFor(ctx context.Context, u *model.User) (string, error) {
	return s.issueRefresh(ctx, u)
}

// RefreshResult 刷新结果。
type RefreshResult struct {
	AccessToken  string
	RefreshToken string
}

// Refresh 轮换 refresh token：校验旧值 → 删除旧值 → 签发新对。
// 旧值不存在视为盗用/重放，吊销该用户全部会话。
func (s *AuthService) Refresh(ctx context.Context, oldToken string) (*RefreshResult, error) {
	if oldToken == "" {
		return nil, ErrRefreshInvalid
	}
	uidStr, err := s.rdb.Get(ctx, refreshKey(oldToken)).Result()
	if err != nil {
		// 旧值缺失：按 uid 前缀尽量吊销该用户其余会话
		if uid, e := parseUIDPrefix(oldToken); e == nil && uid > 0 {
			s.RevokeAll(ctx, uid)
		}
		return nil, ErrRefreshInvalid
	}
	uid, err := strconv.ParseInt(uidStr, 10, 64)
	if err != nil {
		return nil, ErrRefreshInvalid
	}
	u, err := s.store.GetByID(uid)
	if err != nil {
		return nil, err
	}
	if u == nil || !u.IsActive() {
		s.RevokeAll(ctx, uid)
		return nil, ErrUserDisabled
	}

	// 轮换：旧 refresh 立即失效
	s.rdb.Del(ctx, refreshKey(oldToken))
	s.rdb.SRem(ctx, refreshUserKey(uid), oldToken)

	access, err := s.issueAccess(u)
	if err != nil {
		return nil, err
	}
	next, err := s.issueRefresh(ctx, u)
	if err != nil {
		return nil, err
	}
	return &RefreshResult{AccessToken: access, RefreshToken: next}, nil
}

// Logout 删除指定 refresh token。
func (s *AuthService) Logout(ctx context.Context, token string) {
	if token == "" {
		return
	}
	if uid, err := parseUIDPrefix(token); err == nil {
		s.rdb.SRem(ctx, refreshUserKey(uid), token)
	}
	s.rdb.Del(ctx, refreshKey(token))
}

// RevokeAll 吊销某用户全部会话：删除全部 refresh，并递增会话代次，
// 使重置密码/停用/角色调整前签发的 access token 立即失效（specs/identity 场景）。
func (s *AuthService) RevokeAll(ctx context.Context, uid int64) {
	tokens, err := s.rdb.SMembers(ctx, refreshUserKey(uid)).Result()
	if err == nil && len(tokens) > 0 {
		keys := make([]string, 0, len(tokens))
		for _, t := range tokens {
			keys = append(keys, refreshKey(t))
		}
		s.rdb.Del(ctx, keys...)
	}
	s.rdb.Del(ctx, refreshUserKey(uid))
	s.rdb.Incr(ctx, epochKey(uid))
}

func parseUIDPrefix(token string) (int64, error) {
	i := strings.IndexByte(token, '.')
	if i <= 0 {
		return 0, errors.New("bad token format")
	}
	return strconv.ParseInt(token[:i], 10, 64)
}

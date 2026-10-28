package service

import (
	"context"
	"errors"
	"github.com/alicebob/miniredis/v2"
	"github.com/golang-jwt/jwt/v5"
	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"
	"io"
	"jizhang/internal/model"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

type authEnv struct {
	svc  *AuthService
	rdb  *redis.Client
	mr   *miniredis.Miniredis
	head *model.User
}

func newAuthEnv(t *testing.T) *authEnv {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	hash, err := bcrypt.GenerateFromPassword([]byte("admin123"), bcrypt.MinCost)
	mustOK(t, err)
	head := &model.User{ID: 1, Username: "admin", PasswordHash: string(hash), Role: model.RoleAdmin, Status: model.StatusActive}
	return &authEnv{NewAuthService(newFakeUserStore(head), rdb, "test-secret", 30*time.Minute, 30*24*time.Hour, false, "/api/v1/auth"), rdb, mr, head}
}
func loginDevice(t *testing.T, s *AuthService, d DeviceType) *AuthResult {
	t.Helper()
	r, err := s.Login(context.Background(), "admin", "admin123", "test-ip", d)
	mustOK(t, err)
	return r
}
func assertAccess(t *testing.T, s *AuthService, r *AuthResult, want error) {
	t.Helper()
	_, err := s.Authenticate(r.AccessToken)
	if !errors.Is(err, want) {
		t.Fatalf("access err=%v want=%v", err, want)
	}
}
func TestLoginScenario(t *testing.T) {
	e := newAuthEnv(t)
	r := loginDevice(t, e.svc, DeviceDesktop)
	if r.AccessToken == "" || r.RefreshToken == "" || r.User.ID != 1 || !validHex(r.SessionID, 16) {
		t.Fatal("missing session result")
	}
	assertAccess(t, e.svc, r, nil)
	for _, name := range []string{"admin", "ghost"} {
		_, err := e.svc.Login(context.Background(), name, "wrong", "test-ip", DeviceDesktop)
		if !errors.Is(err, ErrInvalidCredentials) {
			t.Fatal(err)
		}
	}
	assertAccess(t, e.svc, r, nil)
}
func TestLoginRateLimited(t *testing.T) {
	e := newAuthEnv(t)
	ctx := context.Background()
	for i := 0; i < loginFailUserThreshold; i++ {
		_, err := e.svc.Login(ctx, "admin", "wrong", "a", DeviceDesktop)
		if !errors.Is(err, ErrInvalidCredentials) {
			t.Fatal(err)
		}
	}
	_, err := e.svc.Login(ctx, "admin", "admin123", "a", DeviceDesktop)
	if !errors.Is(err, ErrRateLimited) {
		t.Fatal(err)
	}
	e.mr.FastForward(loginFailTTL + time.Second)
	loginDevice(t, e.svc, DeviceDesktop)
	if e.mr.Exists("login_fail:admin") {
		t.Fatal("login failure count not cleared")
	}
}
func TestDeviceClassification(t *testing.T) {
	for _, tc := range []struct {
		ua    string
		touch int
		want  DeviceType
	}{
		{"iPhone Mobile", 5, DevicePhone}, {"iPod", 1, DevicePhone}, {"Android 14 Mobile", 5, DevicePhone},
		{"Android 14", 5, DeviceTablet}, {"iPad", 5, DeviceTablet}, {"Macintosh Intel Mac OS X", 5, DeviceTablet},
		{"Macintosh Intel Mac OS X", 0, DeviceDesktop}, {"Windows NT 10.0", 10, DeviceDesktop},
		{"Linux X11", 0, DeviceDesktop}, {"unknown", 0, DeviceDesktop}, {"", 0, DeviceDesktop},
	} {
		t.Run(tc.ua, func(t *testing.T) {
			if got := ClassifyDevice(tc.ua, tc.touch); got != tc.want {
				t.Fatalf("%s != %s", got, tc.want)
			}
		})
	}
}

type failingRandom struct{}

func (failingRandom) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func TestTokenValidationAndPreparationFailure(t *testing.T) {
	e := newAuthEnv(t)
	r := loginDevice(t, e.svc, DeviceDesktop)
	ref, err := parseRefresh(r.RefreshToken)
	mustOK(t, err)
	if ref.SID != r.SessionID || ref.UID != 1 || ref.Device != DeviceDesktop {
		t.Fatal(ref)
	}
	for _, bad := range []string{"1.old", "v1.1.desktop.x.y", strings.Replace(r.RefreshToken, "desktop", "invalid", 1), "v2.0.desktop." + ref.SID + "." + strings.Repeat("a", 64)} {
		if _, err := parseRefresh(bad); err == nil {
			t.Fatal("accepted", bad)
		}
	}
	e.svc.random = failingRandom{}
	_, err = e.svc.Login(context.Background(), "admin", "admin123", "test-ip", DeviceDesktop)
	if !errors.Is(err, ErrAuthUnavailable) {
		t.Fatal(err)
	}
	assertAccess(t, e.svc, r, nil)
	old := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"uid": 1, "iss": "jizhang", "exp": time.Now().Add(time.Hour).Unix()})
	encoded, err := old.SignedString(e.svc.secret)
	mustOK(t, err)
	if _, err = e.svc.Authenticate(encoded); !errors.Is(err, ErrRefreshInvalid) {
		t.Fatal(err)
	}
	claims, _, err := e.svc.parseAccess(r.AccessToken)
	mustOK(t, err)
	claims.Issuer = "wrong"
	encoded, err = jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(e.svc.secret)
	mustOK(t, err)
	if _, err = e.svc.Authenticate(encoded); !errors.Is(err, ErrRefreshInvalid) {
		t.Fatal(err)
	}
}
func TestDevicesCoexistAndReplacementIsIsolated(t *testing.T) {
	e := newAuthEnv(t)
	phone := loginDevice(t, e.svc, DevicePhone)
	tablet := loginDevice(t, e.svc, DeviceTablet)
	desktop := loginDevice(t, e.svc, DeviceDesktop)
	for _, r := range []*AuthResult{phone, tablet, desktop} {
		assertAccess(t, e.svc, r, nil)
	}
	replacement := loginDevice(t, e.svc, DevicePhone)
	assertAccess(t, e.svc, phone, ErrSessionReplaced)
	for _, r := range []*AuthResult{replacement, tablet, desktop} {
		assertAccess(t, e.svc, r, nil)
	}
	ctx := context.Background()
	if _, err := e.svc.Refresh(ctx, phone.RefreshToken, phone.SessionID); !errors.Is(err, ErrRefreshInvalid) {
		t.Fatal(err)
	}
	if err := e.svc.Logout(ctx, phone.RefreshToken, phone.AccessToken, phone.SessionID); !errors.Is(err, ErrSessionReplaced) {
		t.Fatal(err)
	}
	for _, r := range []*AuthResult{replacement, tablet, desktop} {
		assertAccess(t, e.svc, r, nil)
	}
	fields, err := e.rdb.HGetAll(ctx, e.svc.sessions.key(1)).Result()
	mustOK(t, err)
	if len(fields) != 4 {
		t.Fatal("unbounded or missing fields", fields)
	}
	for _, value := range fields {
		if strings.Contains(value, replacement.RefreshToken) {
			t.Fatal("raw refresh persisted")
		}
	}
}
func TestRefreshRotation(t *testing.T) {
	e := newAuthEnv(t)
	r := loginDevice(t, e.svc, DevicePhone)
	other := loginDevice(t, e.svc, DeviceDesktop)
	ctx := context.Background()
	next, err := e.svc.Refresh(ctx, r.RefreshToken, r.SessionID)
	mustOK(t, err)
	if next.SessionID != r.SessionID || next.RefreshToken == r.RefreshToken {
		t.Fatal("bad rotation")
	}
	_, err = e.svc.Refresh(ctx, r.RefreshToken, r.SessionID)
	if !errors.Is(err, ErrRefreshConflict) {
		t.Fatal(err)
	}
	assertAccess(t, e.svc, next, nil)
	assertAccess(t, e.svc, other, nil)
	forged := r.RefreshToken[:len(r.RefreshToken)-64] + strings.Repeat("0", 64)
	if _, err := e.svc.Refresh(ctx, forged, r.SessionID); !errors.Is(err, ErrRefreshInvalid) {
		t.Fatal(err)
	}
	_, err = e.svc.Refresh(ctx, "1.fake", "")
	if !errors.Is(err, ErrRefreshInvalid) {
		t.Fatal(err)
	}
	if _, err := e.svc.Refresh(ctx, next.RefreshToken, "wrong"); !errors.Is(err, ErrRefreshInvalid) {
		t.Fatal(err)
	}
	if _, err := e.svc.Refresh(ctx, next.RefreshToken, next.SessionID); err != nil {
		t.Fatal(err)
	}
	assertAccess(t, e.svc, other, nil)
}
func TestSessionExpiryAndSlidingTTL(t *testing.T) {
	e := newAuthEnv(t)
	now := time.Now()
	e.svc.now = func() time.Time { return now }
	phone := loginDevice(t, e.svc, DevicePhone)
	now = now.Add(10 * 24 * time.Hour)
	desktop := loginDevice(t, e.svc, DeviceDesktop)
	phoneRef, _ := parseRefresh(phone.RefreshToken)
	desktopRef, _ := parseRefresh(desktop.RefreshToken)
	before, err := e.svc.sessions.read(context.Background(), desktopRef, now)
	mustOK(t, err)
	now = now.Add(10 * 24 * time.Hour)
	next, err := e.svc.Refresh(context.Background(), phone.RefreshToken, phone.SessionID)
	mustOK(t, err)
	p, err := e.svc.sessions.read(context.Background(), phoneRef, now)
	mustOK(t, err)
	d, err := e.svc.sessions.read(context.Background(), desktopRef, now)
	mustOK(t, err)
	if p.ExpiresAt != now.Add(e.svc.refreshTTL).UnixMilli() || before.ExpiresAt != d.ExpiresAt {
		t.Fatal("TTL leaked between slots")
	}
	now = now.Add(21 * 24 * time.Hour)
	if _, err := e.svc.Refresh(context.Background(), desktop.RefreshToken, desktop.SessionID); !errors.Is(err, ErrRefreshInvalid) {
		t.Fatal(err)
	}
	if _, err := e.svc.Refresh(context.Background(), next.RefreshToken, next.SessionID); err != nil {
		t.Fatal(err)
	}
}
func TestLogoutInvalidatesAllTokens(t *testing.T) {
	e := newAuthEnv(t)
	ctx := context.Background()
	sessions := []*AuthResult{loginDevice(t, e.svc, DevicePhone), loginDevice(t, e.svc, DeviceTablet), loginDevice(t, e.svc, DeviceDesktop)}
	if err := e.svc.Logout(ctx, "", "", ""); !errors.Is(err, ErrRefreshInvalid) {
		t.Fatal(err)
	}
	actor := sessions[0]
	mustOK(t, e.svc.Logout(ctx, actor.RefreshToken, actor.AccessToken, actor.SessionID))
	for _, r := range sessions {
		assertAccess(t, e.svc, r, ErrRefreshInvalid)
		if _, err := e.svc.Refresh(ctx, r.RefreshToken, r.SessionID); !errors.Is(err, ErrRefreshInvalid) {
			t.Fatal(err)
		}
	}
	next := loginDevice(t, e.svc, DevicePhone)
	if err := e.svc.Logout(ctx, actor.RefreshToken, actor.AccessToken, actor.SessionID); err == nil {
		t.Fatal("stale logout succeeded")
	}
	assertAccess(t, e.svc, next, nil)
}
func TestAccessExpiryAndUnavailable(t *testing.T) {
	e := newAuthEnv(t)
	now := time.Now()
	e.svc.now = func() time.Time { return now }
	r := loginDevice(t, e.svc, DevicePhone)
	now = now.Add(31 * time.Minute)
	assertAccess(t, e.svc, r, ErrAccessExpired)
	e.rdb.Close()
	assertAccess(t, e.svc, r, ErrAuthUnavailable)
	if _, err := e.svc.Refresh(context.Background(), r.RefreshToken, r.SessionID); !errors.Is(err, ErrAuthUnavailable) {
		t.Fatal(err)
	}
	if err := e.svc.RevokeAll(context.Background(), 1); !errors.Is(err, ErrAuthUnavailable) {
		t.Fatal(err)
	}
}
func TestDisabledAccountRejected(t *testing.T) {
	e := newAuthEnv(t)
	r := loginDevice(t, e.svc, DevicePhone)
	e.head.Status = model.StatusDisabled
	assertAccess(t, e.svc, r, ErrUserDisabled)
	if _, err := e.svc.Login(context.Background(), "admin", "admin123", "test-ip", DevicePhone); !errors.Is(err, ErrUserDisabled) {
		t.Fatal(err)
	}
	mustOK(t, e.svc.RevokeAll(context.Background(), 1))
	e.head.Status = model.StatusActive
	assertAccess(t, e.svc, r, ErrRefreshInvalid)
	assertAccess(t, e.svc, loginDevice(t, e.svc, DevicePhone), nil)
}
func TestConcurrentSessionMutations(t *testing.T) {
	e := newAuthEnv(t)
	checkConcurrentMutations(t, e.svc)
}

func TestAdminOperationsRevokeThreeDeviceTypes(t *testing.T) {
	for _, op := range []string{"role", "status", "password"} {
		t.Run(op, func(t *testing.T) {
			e := newAuthEnv(t)
			store := e.svc.store.(*fakeUserStore)
			store.users[2] = admin(2, "another-admin")
			users := NewUserService(store, e.svc)
			tokens := []*AuthResult{loginDevice(t, e.svc, DevicePhone), loginDevice(t, e.svc, DeviceTablet), loginDevice(t, e.svc, DeviceDesktop)}
			var err error
			switch op {
			case "role":
				_, err = users.SetRole(1, model.RoleMember)
			case "status":
				_, err = users.SetStatus(1, model.StatusDisabled)
			case "password":
				err = users.ResetPassword(1, "changed123")
			}
			mustOK(t, err)
			for _, r := range tokens {
				assertAccess(t, e.svc, r, ErrRefreshInvalid)
				if _, err := e.svc.Refresh(context.Background(), r.RefreshToken, r.SessionID); err == nil {
					t.Fatal("revoked refresh accepted")
				}
			}
			if op == "status" {
				_, err = users.SetStatus(1, model.StatusActive)
				mustOK(t, err)
				for _, r := range tokens {
					assertAccess(t, e.svc, r, ErrRefreshInvalid)
				}
			}
		})
	}
}
func checkConcurrentMutations(t *testing.T, s *AuthService) {
	t.Helper()
	ctx := context.Background()
	phone := loginDevice(t, s, DevicePhone)
	other := loginDevice(t, s, DeviceTablet)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var successes []*AuthResult
	for i := 0; i < 8; i++ {
		wg.Go(func() {
			next, err := s.Refresh(ctx, phone.RefreshToken, phone.SessionID)
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				successes = append(successes, next)
			} else if !errors.Is(err, ErrRefreshConflict) {
				t.Errorf("refresh: %v", err)
			}
		})
	}
	wg.Wait()
	if len(successes) != 1 {
		t.Fatalf("refresh successes %d", len(successes))
	}
	assertAccess(t, s, other, nil)
	candidates := make(chan *AuthResult, 8)
	for i := 0; i < 8; i++ {
		wg.Go(func() {
			r, err := s.Login(ctx, "admin", "admin123", "test-ip", DeviceDesktop)
			if err != nil {
				t.Error(err)
				return
			}
			candidates <- r
		})
	}
	wg.Wait()
	close(candidates)
	valid := 0
	for r := range candidates {
		if _, err := s.Authenticate(r.AccessToken); err == nil {
			valid++
		} else if !errors.Is(err, ErrSessionReplaced) {
			t.Error(err)
		}
	}
	if valid != 1 {
		t.Fatalf("valid desktop slots=%d", valid)
	}
	// A prepared refresh cannot rebuild a slot after revocation.
	winner := successes[0]
	ref, _ := parseRefresh(winner.RefreshToken)
	record, err := s.sessions.read(ctx, ref, s.now())
	mustOK(t, err)
	_, prepared, err := s.prepare(winner.User, ref, record.Epoch, s.now())
	mustOK(t, err)
	mustOK(t, s.RevokeAll(ctx, 1))
	if err := s.sessions.mutate(ctx, "refresh", ref, "0", refreshDigest(winner.RefreshToken), prepared, s.now(), false); err == nil {
		t.Fatal("refresh resurrected revoked slot")
	}
	assertAccess(t, s, winner, ErrRefreshInvalid)
}
func TestNativeRedisSessions(t *testing.T) {
	addr := os.Getenv("AUTH_REDIS_ADDR")
	if addr == "" {
		t.Skip("set AUTH_REDIS_ADDR for isolated native Redis script validation")
	}
	rdb := redis.NewClient(&redis.Options{Addr: addr, Password: os.Getenv("AUTH_REDIS_PASSWORD")})
	ctx := context.Background()
	mustOK(t, rdb.Ping(ctx).Err())
	suffix, err := randomHex(strings.NewReader(strings.Repeat("a", 32)), 8)
	mustOK(t, err)
	suffix += time.Now().Format("150405.000000000")
	hash, err := bcrypt.GenerateFromPassword([]byte("admin123"), bcrypt.MinCost)
	mustOK(t, err)
	head := &model.User{ID: 1, Username: "admin", PasswordHash: string(hash), Role: model.RoleAdmin, Status: model.StatusActive}
	s := NewAuthService(newFakeUserStore(head), rdb, "isolated-test", time.Minute, time.Hour, false, "")
	s.sessions.prefix = "auth:test:" + suffix + ":"
	s.ratePrefix = s.sessions.prefix
	t.Cleanup(func() {
		if err := rdb.Del(context.Background(), s.sessions.key(1), s.ratePrefix+"login_fail:admin", s.ratePrefix+"login_fail_ip:test-ip").Err(); err != nil {
			t.Error(err)
		}
		rdb.Close()
	})
	checkConcurrentMutations(t, s)
	now := time.Now()
	s.now = func() time.Time { return now }
	phone := loginDevice(t, s, DevicePhone)
	desktop := loginDevice(t, s, DeviceDesktop)
	now = now.Add(45 * time.Minute)
	renewed, err := s.Refresh(ctx, phone.RefreshToken, phone.SessionID)
	mustOK(t, err)
	now = now.Add(30 * time.Minute)
	if _, err := s.Refresh(ctx, desktop.RefreshToken, desktop.SessionID); !errors.Is(err, ErrRefreshInvalid) {
		t.Fatal(err)
	}
	if _, err := s.Refresh(ctx, "1.legacy-token", ""); !errors.Is(err, ErrRefreshInvalid) {
		t.Fatal(err)
	}
	legacyAccess, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"uid": 1, "iss": "jizhang", "exp": now.Add(time.Hour).Unix()}).SignedString(s.secret)
	mustOK(t, err)
	if _, err := s.Authenticate(legacyAccess); !errors.Is(err, ErrRefreshInvalid) {
		t.Fatal(err)
	}
	renewed, err = s.Refresh(ctx, renewed.RefreshToken, renewed.SessionID)
	mustOK(t, err)
	broken := redis.NewClient(&redis.Options{Addr: addr})
	broken.Close()
	s.sessions.rdb = broken
	assertAccess(t, s, renewed, ErrAuthUnavailable)
	s.sessions.rdb = rdb
	assertAccess(t, s, renewed, nil)
	mustOK(t, s.Logout(ctx, renewed.RefreshToken, renewed.AccessToken, renewed.SessionID))
	assertAccess(t, s, renewed, ErrRefreshInvalid)
}

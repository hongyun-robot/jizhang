package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"

	"jizhang/internal/model"
)

// ---- 测试脚手架：复用 user_service_test.go 的 fakeUserStore + miniredis ----

// authEnv 一次登录链路所需的环境：服务 + 内嵌 Redis + 可变用户表。
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
	if err != nil {
		t.Fatalf("bcrypt: %v", err)
	}
	head := &model.User{
		ID: 1, Username: "admin", PasswordHash: string(hash),
		Role: model.RoleAdmin, Status: model.StatusActive,
	}
	svc := NewAuthService(newFakeUserStore(head), rdb, "test-secret", 30*time.Minute, 30*24*time.Hour, false, "/api/v1/auth")
	return &authEnv{svc: svc, rdb: rdb, mr: mr, head: head}
}

// TestLoginScenario 覆盖「用户名密码登录」：
// 成功签发 access；失败返回统一提示，不泄露账号是否存在（不存在的用户名与错密码同一条错误）。
func TestLoginScenario(t *testing.T) {
	env := newAuthEnv(t)
	ctx := context.Background()

	access, u, err := env.svc.Login(ctx, "admin", "admin123", "1.1.1.1")
	mustOK(t, err)
	if access == "" {
		t.Fatal("登录成功应返回 access token")
	}
	if u.ID != env.head.ID {
		t.Errorf("登录用户 = %d, want %d", u.ID, env.head.ID)
	}

	// 错密码
	_, _, err = env.svc.Login(ctx, "admin", "wrong", "1.1.1.1")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("错密码 err = %v, want ErrInvalidCredentials", err)
	}
	// 账号不存在：与错密码完全相同的提示（不泄露存在性）
	_, _, err = env.svc.Login(ctx, "ghost", "whatever", "1.1.1.1")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("不存在账号 err = %v, want ErrInvalidCredentials", err)
	}
	if err.Error() != "用户名或密码错误" {
		t.Errorf("统一提示 = %q, want 用户名或密码错误", err.Error())
	}
}

// TestLoginRateLimited 覆盖「登录失败限流 / 暴力破解防护」：
// 同一用户名连续 5 次失败后，第 6 次即使密码正确也被直接拒绝。
func TestLoginRateLimited(t *testing.T) {
	env := newAuthEnv(t)
	ctx := context.Background()

	for i := 1; i <= loginFailUserThreshold; i++ {
		_, _, err := env.svc.Login(ctx, "admin", "wrong", "1.1.1.1")
		if !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("第 %d 次错密码 err = %v, want ErrInvalidCredentials", i, err)
		}
	}
	// 第 6 次：密码正确也拒绝（冷却期内）
	_, _, err := env.svc.Login(ctx, "admin", "admin123", "1.1.1.1")
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("第 6 次 err = %v, want ErrRateLimited", err)
	}
	// 冷却期（10 分钟）后可再次尝试
	env.mr.FastForward(loginFailTTL + time.Second)
	_, _, err = env.svc.Login(ctx, "admin", "admin123", "1.1.1.1")
	mustOK(t, err)

	// 成功登录清零计数：连续 5 次失败后仍能登录（说明计数在成功后归零重新累计）
	for i := 0; i < loginFailUserThreshold; i++ {
		_, _, err = env.svc.Login(ctx, "admin", "wrong", "2.2.2.2")
		if !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("重置后第 %d 次错密码 err = %v", i+1, err)
		}
	}
	_, _, err = env.svc.Login(ctx, "admin", "admin123", "2.2.2.2")
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("5 次失败后 err = %v, want ErrRateLimited（成功登录需已清零旧计数）", err)
	}
}

// TestRefreshRotation 覆盖「token 刷新轮换 / refresh token 失效」：
// 刷新签发新对且旧 refresh 立即失效；重放旧值按盗用处理，吊销该用户全部会话。
func TestRefreshRotation(t *testing.T) {
	env := newAuthEnv(t)
	ctx := context.Background()

	old, err := env.svc.IssueRefreshFor(ctx, env.head)
	mustOK(t, err)

	res, err := env.svc.Refresh(ctx, old)
	mustOK(t, err)
	if res.RefreshToken == "" || res.AccessToken == "" {
		t.Fatal("刷新应返回新的 access 与 refresh")
	}
	if res.RefreshToken == old {
		t.Error("刷新必须轮换出不同的 refresh token")
	}

	// 重放旧 refresh → 失败（且触发 RevokeAll，新的也一并吊销）
	_, err = env.svc.Refresh(ctx, old)
	if !errors.Is(err, ErrRefreshInvalid) {
		t.Fatalf("重放旧 refresh err = %v, want ErrRefreshInvalid", err)
	}
	// 盗用检测后该用户全部会话被吊销：新 token 也失效
	_, err = env.svc.Refresh(ctx, res.RefreshToken)
	if !errors.Is(err, ErrRefreshInvalid) {
		t.Fatalf("吊销后新 refresh err = %v, want ErrRefreshInvalid", err)
	}
}

// TestLogoutInvalidatesRefresh 覆盖「登出」：登出后该 refresh 立即不可再用。
func TestLogoutInvalidatesRefresh(t *testing.T) {
	env := newAuthEnv(t)
	ctx := context.Background()

	token, err := env.svc.IssueRefreshFor(ctx, env.head)
	mustOK(t, err)

	env.svc.Logout(ctx, token)

	_, err = env.svc.Refresh(ctx, token)
	if !errors.Is(err, ErrRefreshInvalid) {
		t.Fatalf("登出后刷新 err = %v, want ErrRefreshInvalid", err)
	}
}

// TestDisabledAccountRejected 覆盖「停用账号拒绝访问」的两个场景：
// 停用后登录失败；停用前签发的 access token 在校验时被拒绝（会话立即失效）。
func TestDisabledAccountRejected(t *testing.T) {
	env := newAuthEnv(t)
	ctx := context.Background()

	// 停用前签发 access
	access, _, err := env.svc.Login(ctx, "admin", "admin123", "1.1.1.1")
	mustOK(t, err)
	if _, err = env.svc.Authenticate(access); err != nil {
		t.Fatalf("停用前 access 校验 err = %v, want nil", err)
	}

	// 停用（在线会话失效：停用流程会调 RevokeAll 递增代次，此处直接改状态亦须拒绝）
	env.head.Status = model.StatusDisabled
	if _, err = env.svc.Authenticate(access); !errors.Is(err, ErrUserDisabled) {
		t.Fatalf("停用后 access 校验 err = %v, want ErrUserDisabled", err)
	}
	// 停用后登录：返回账号已停用（凭据正确）
	_, _, err = env.svc.Login(ctx, "admin", "admin123", "1.1.1.1")
	if !errors.Is(err, ErrUserDisabled) {
		t.Fatalf("停用后登录 err = %v, want ErrUserDisabled", err)
	}

	// 恢复
	env.head.Status = model.StatusActive
	_, _, err = env.svc.Login(ctx, "admin", "admin123", "1.1.1.1")
	mustOK(t, err)
}

// TestRevokeAllInvalidatesAccess 覆盖重置密码/停用/角色调整的共性前提：
// RevokeAll 后此会话代次之前签发的 access token 全部失效。
func TestRevokeAllInvalidatesAccess(t *testing.T) {
	env := newAuthEnv(t)
	ctx := context.Background()

	access, _, err := env.svc.Login(ctx, "admin", "admin123", "1.1.1.1")
	mustOK(t, err)

	env.svc.RevokeAll(ctx, env.head.ID)

	if _, err = env.svc.Authenticate(access); !errors.Is(err, ErrRefreshInvalid) {
		t.Fatalf("RevokeAll 后旧 access err = %v, want ErrRefreshInvalid", err)
	}
	// 重新登录可得新一代次的有效 token
	access2, _, err := env.svc.Login(ctx, "admin", "admin123", "1.1.1.1")
	mustOK(t, err)
	if _, err = env.svc.Authenticate(access2); err != nil {
		t.Fatalf("新 access 校验 err = %v, want nil", err)
	}
}

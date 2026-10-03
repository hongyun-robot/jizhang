package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"jizhang/internal/model"
)

// TestRequireRolesGate 覆盖 specs/identity/access-control「设置类操作仅限管理员」
// 与「API 与界面双重执行」的服务端一环：非管理员直调高权限 API 一律 403，管理员放行。
// （界面显隐由 e2e 走查覆盖，这里只守「绕过界面直调」这条硬防线。）
func TestRequireRolesGate(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// 路由结构与 router.go 一致：setUser（模拟 Auth 装载用户）→ RequireRoles → 业务 handler
	build := func(user *model.User) *gin.Engine {
		r := gin.New()
		r.GET("/admin-only",
			func(c *gin.Context) {
				if user != nil {
					c.Set(ctxUserKey, user)
				}
			},
			RequireRoles(model.RoleAdmin),
			func(c *gin.Context) { c.Status(http.StatusOK) },
		)
		return r
	}
	do := func(r *gin.Engine) int {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/admin-only", nil))
		return w.Code
	}

	cases := []struct {
		name string
		role model.Role
		want int
	}{
		{"admin 放行", model.RoleAdmin, http.StatusOK},
		{"recorder 拒绝", model.RoleRecorder, http.StatusForbidden},
		{"member 拒绝", model.RoleMember, http.StatusForbidden},
		{"viewer 拒绝", model.RoleViewer, http.StatusForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			u := &model.User{ID: 1, Role: tc.role, Status: model.StatusActive}
			if got := do(build(u)); got != tc.want {
				t.Errorf("role=%s status = %d, want %d", tc.role, got, tc.want)
			}
		})
	}

	// Auth 未装载用户（未登录/校验失败）→ 同样 403：门禁不依赖上游是否遗漏
	if got := do(build(nil)); got != http.StatusForbidden {
		t.Errorf("无用户 status = %d, want 403", got)
	}
}

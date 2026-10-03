package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"

	"jizhang/internal/model"
	"jizhang/internal/response"
	"jizhang/internal/service"
)

const ctxUserKey = "currentUser"

// Auth 校验 Bearer access token，加载启用状态用户；失败 401。
func Auth(svc *service.AuthService) gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.GetHeader("Authorization")
		if !strings.HasPrefix(h, "Bearer ") {
			response.Unauthorized(c, "未登录")
			c.Abort()
			return
		}
		u, err := svc.Authenticate(strings.TrimPrefix(h, "Bearer "))
		if err != nil {
			response.Unauthorized(c, "登录已失效")
			c.Abort()
			return
		}
		c.Set(ctxUserKey, u)
		c.Next()
	}
}

// CurrentUser 取出 Auth 中间件装载的用户。
func CurrentUser(c *gin.Context) *model.User {
	if v, ok := c.Get(ctxUserKey); ok {
		if u, ok := v.(*model.User); ok {
			return u
		}
	}
	return nil
}

// RequireRoles 仅允许指定角色（未登录 401 已由 Auth 处理，此处只管 403）。
func RequireRoles(roles ...model.Role) gin.HandlerFunc {
	allowed := make(map[model.Role]bool, len(roles))
	for _, r := range roles {
		allowed[r] = true
	}
	return func(c *gin.Context) {
		u := CurrentUser(c)
		if u == nil || !allowed[u.Role] {
			response.Forbidden(c, "无权限")
			c.Abort()
			return
		}
		c.Next()
	}
}

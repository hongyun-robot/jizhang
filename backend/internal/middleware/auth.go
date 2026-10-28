package middleware

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"jizhang/internal/model"
	"jizhang/internal/response"
	"jizhang/internal/service"
)

const ctxUserKey = "currentUser"
const ctxSessionKey = "currentSession"

// Auth 校验 Bearer access token，加载启用状态用户；失败 401。
func Auth(svc *service.AuthService) gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.GetHeader("Authorization")
		if !strings.HasPrefix(h, "Bearer ") {
			response.Unauthorized(c, "未登录")
			c.Abort()
			return
		}
		u, claims, err := svc.AuthenticateSession(c.Request.Context(), strings.TrimPrefix(h, "Bearer "))
		if err != nil {
			AuthFailure(c, err)
			c.Abort()
			return
		}
		c.Set(ctxUserKey, u)
		c.Set(ctxSessionKey, claims)
		c.Next()
	}
}

func CurrentSession(c *gin.Context) *service.AuthClaims {
	value, _ := c.Get(ctxSessionKey)
	claims, _ := value.(*service.AuthClaims)
	return claims
}

// AuthFailure preserves retryable expiry, terminal sessions and service failure.
func AuthFailure(c *gin.Context, err error) {
	status, code, message := http.StatusUnauthorized, response.CodeSessionEnded, "登录已失效，请重新登录"
	switch {
	case errors.Is(err, service.ErrAccessExpired):
		code, message = response.CodeAccessExpired, err.Error()
	case errors.Is(err, service.ErrSessionReplaced):
		code, message = response.CodeSessionReplaced, err.Error()
	case errors.Is(err, service.ErrAuthUnavailable), errors.Is(err, service.ErrAuthChanged):
		status, code, message = http.StatusServiceUnavailable, response.CodeAuthUnavailable, "认证服务暂不可用，请重试"
	case errors.Is(err, service.ErrRefreshConflict):
		status, code, message = http.StatusConflict, response.CodeRefreshConflict, err.Error()
	case errors.Is(err, service.ErrRateLimited):
		status, code, message = http.StatusTooManyRequests, response.CodeTooManyReq, err.Error()
	case errors.Is(err, service.ErrInvalidCredentials):
		code, message = response.CodeUnauthorized, err.Error()
	case errors.Is(err, service.ErrUserDisabled):
		message = err.Error()
	}
	response.Fail(c, status, code, message)
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

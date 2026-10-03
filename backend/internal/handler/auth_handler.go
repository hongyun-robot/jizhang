package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"jizhang/internal/middleware"
	"jizhang/internal/response"
	"jizhang/internal/service"
)

const refreshCookieName = "jz_rt"

type authHandler struct {
	svc *service.AuthService
}

// NewAuth 认证处理器。
func NewAuth(svc *service.AuthService) *authHandler { return &authHandler{svc: svc} }

type loginReq struct {
	Username string `json:"username" binding:"required,max=64"`
	Password string `json:"password" binding:"required,max=128"`
}

type tokenResp struct {
	AccessToken string      `json:"access_token"`
	TokenType   string      `json:"token_type"`
	ExpiresIn   int         `json:"expires_in"`
	User        interface{} `json:"user,omitempty"`
}

// setRefreshCookie 写 refresh cookie。SameSite=Strict 是 design D2 的 CSRF 防线
// （写接口均为 JSON，无简单表单），必须在每次 SetCookie 前显式设置——gin 默认不带该属性。
func (h *authHandler) setRefreshCookie(c *gin.Context, token string) {
	maxAge := int(h.svc.RefreshTTL().Seconds())
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie(refreshCookieName, token, maxAge, h.svc.CookiePath(), "", h.svc.CookieSecure(), true)
}

// Login POST /api/v1/auth/login
func (h *authHandler) Login(c *gin.Context) {
	var req loginReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Invalid(c, "用户名与密码必填")
		return
	}
	access, user, err := h.svc.Login(c.Request.Context(), req.Username, req.Password, c.ClientIP())
	if err != nil {
		switch {
		case errors.Is(err, service.ErrRateLimited):
			response.Fail(c, http.StatusTooManyRequests, response.CodeTooManyReq, err.Error())
		case errors.Is(err, service.ErrUserDisabled), errors.Is(err, service.ErrInvalidCredentials):
			response.Unauthorized(c, err.Error())
		default:
			response.Internal(c, "登录失败")
		}
		return
	}
	refresh, err := h.svc.IssueRefreshFor(c.Request.Context(), user)
	if err != nil {
		response.Internal(c, "登录失败")
		return
	}
	h.setRefreshCookie(c, refresh)
	response.OK(c, tokenResp{
		AccessToken: access,
		TokenType:   "Bearer",
		ExpiresIn:   int(h.svc.AccessTTL().Seconds()),
		User:        user,
	})
}

// Refresh POST /api/v1/auth/refresh
func (h *authHandler) Refresh(c *gin.Context) {
	token, err := c.Cookie(refreshCookieName)
	if err != nil || token == "" {
		response.Unauthorized(c, "登录已过期，请重新登录")
		return
	}
	res, err := h.svc.Refresh(c.Request.Context(), token)
	if err != nil {
		c.SetSameSite(http.SameSiteStrictMode)
		c.SetCookie(refreshCookieName, "", -1, h.svc.CookiePath(), "", h.svc.CookieSecure(), true)
		response.Unauthorized(c, "登录已过期，请重新登录")
		return
	}
	h.setRefreshCookie(c, res.RefreshToken)
	response.OK(c, tokenResp{
		AccessToken: res.AccessToken,
		TokenType:   "Bearer",
		ExpiresIn:   int(h.svc.AccessTTL().Seconds()),
	})
}

// Logout POST /api/v1/auth/logout
func (h *authHandler) Logout(c *gin.Context) {
	if token, err := c.Cookie(refreshCookieName); err == nil {
		h.svc.Logout(c.Request.Context(), token)
	}
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie(refreshCookieName, "", -1, h.svc.CookiePath(), "", h.svc.CookieSecure(), true)
	response.OK(c, nil)
}

// Me GET /api/v1/auth/me（需登录）
func (h *authHandler) Me(c *gin.Context) {
	response.OK(c, middleware.CurrentUser(c))
}

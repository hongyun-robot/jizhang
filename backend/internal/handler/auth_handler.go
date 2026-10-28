package handler

import (
	"errors"
	"github.com/gin-gonic/gin"
	"io"
	"jizhang/internal/middleware"
	"jizhang/internal/model"
	"jizhang/internal/response"
	"jizhang/internal/service"
	"net/http"
	"strings"
)

const refreshCookieName = "jz_rt"

type authHandler struct{ svc *service.AuthService }

func NewAuth(svc *service.AuthService) *authHandler { return &authHandler{svc} }

type loginReq struct {
	Username        string `json:"username" binding:"required,max=64"`
	Password        string `json:"password" binding:"required,max=128"`
	ProtocolVersion int    `json:"protocol_version"`
	MaxTouchPoints  int    `json:"max_touch_points" binding:"min=0,max=64"`
}
type sessionReq struct {
	ProtocolVersion int    `json:"protocol_version"`
	SessionID       string `json:"session_id" binding:"max=32"`
}
type tokenResp struct {
	AccessToken string             `json:"access_token"`
	TokenType   string             `json:"token_type"`
	ExpiresIn   int                `json:"expires_in"`
	SessionID   string             `json:"session_id"`
	DeviceType  service.DeviceType `json:"device_type"`
	User        interface{}        `json:"user,omitempty"`
	ExpiresAt   int64              `json:"expires_at"`
}

func (h *authHandler) setRefreshCookie(c *gin.Context, token string) {
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie(refreshCookieName, token, int(h.svc.RefreshTTL().Seconds()), h.svc.CookiePath(), "", h.svc.CookieSecure(), true)
}
func (h *authHandler) result(c *gin.Context, r *service.AuthResult) {
	h.setRefreshCookie(c, r.RefreshToken)
	response.OK(c, tokenResp{AccessToken: r.AccessToken, TokenType: "Bearer", ExpiresIn: int(h.svc.AccessTTL().Seconds()), SessionID: r.SessionID, DeviceType: r.DeviceType, User: r.User, ExpiresAt: r.ExpiresAt})
}
func (h *authHandler) Login(c *gin.Context) {
	var req loginReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Invalid(c, "用户名、密码或浏览器信息无效")
		return
	}
	if req.ProtocolVersion != service.AuthProtocolVersion {
		response.Invalid(c, "登录页面已更新，请刷新页面后重试")
		return
	}
	if len(c.GetHeader("User-Agent")) > 2048 {
		response.Invalid(c, "浏览器信息过长")
		return
	}
	result, err := h.svc.Login(c.Request.Context(), req.Username, req.Password, c.ClientIP(), service.ClassifyDevice(c.GetHeader("User-Agent"), req.MaxTouchPoints))
	if err != nil {
		middleware.AuthFailure(c, err)
		return
	}
	h.result(c, result)
}
func bindSession(c *gin.Context) (sessionReq, bool) {
	var req sessionReq
	if err := c.ShouldBindJSON(&req); err != nil {
		if errors.Is(err, io.EOF) {
			response.Fail(c, http.StatusUnauthorized, response.CodeSessionEnded, "登录页面已更新，请刷新页面后重新登录")
			return req, false
		}
		response.Invalid(c, "会话请求无效")
		return req, false
	}
	if req.ProtocolVersion != service.AuthProtocolVersion {
		response.Fail(c, http.StatusUnauthorized, response.CodeSessionEnded, "登录页面已更新，请刷新页面后重新登录")
		return req, false
	}
	return req, true
}
func (h *authHandler) Refresh(c *gin.Context) {
	req, ok := bindSession(c)
	if !ok {
		return
	}
	token, _ := c.Cookie(refreshCookieName)
	result, err := h.svc.Refresh(c.Request.Context(), token, req.SessionID)
	if err != nil {
		middleware.AuthFailure(c, err)
		return
	} // Never clear a newer cookie on failure.
	h.result(c, result)
}
func (h *authHandler) Logout(c *gin.Context) {
	req, ok := bindSession(c)
	if !ok {
		return
	}
	token, _ := c.Cookie(refreshCookieName)
	bearer := ""
	if value := c.GetHeader("Authorization"); strings.HasPrefix(value, "Bearer ") {
		bearer = strings.TrimPrefix(value, "Bearer ")
	}
	if err := h.svc.Logout(c.Request.Context(), token, bearer, req.SessionID); err != nil {
		middleware.AuthFailure(c, err)
		return
	}
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie(refreshCookieName, "", -1, h.svc.CookiePath(), "", h.svc.CookieSecure(), true)
	response.OK(c, nil)
}
func (h *authHandler) Me(c *gin.Context) {
	claims := middleware.CurrentSession(c)
	response.OK(c, struct {
		*model.User
		SessionID  string             `json:"session_id"`
		DeviceType service.DeviceType `json:"device_type"`
	}{middleware.CurrentUser(c), claims.SID, claims.Device})
}

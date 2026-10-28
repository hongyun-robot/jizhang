package handler

import (
	"errors"
	"strconv"

	"github.com/gin-gonic/gin"
	"jizhang/internal/middleware"

	"jizhang/internal/model"
	"jizhang/internal/response"
	"jizhang/internal/service"
)

type userHandler struct {
	svc *service.UserService
}

// NewUser 用户管理处理器。
func NewUser(svc *service.UserService) *userHandler { return &userHandler{svc: svc} }

func mapUserErr(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrAuthUnavailable):
		middleware.AuthFailure(c, err)
	case errors.Is(err, service.ErrUserNotFound):
		response.NotFound(c, err.Error())
	case errors.Is(err, service.ErrUsernameTaken), errors.Is(err, service.ErrLastAdmin):
		response.Conflict(c, err.Error())
	case errors.Is(err, service.ErrInvalidRole), errors.Is(err, service.ErrWeakPassword),
		errors.Is(err, service.ErrInvalidStatus):
		response.Invalid(c, err.Error())
	default:
		response.Internal(c, "操作失败")
	}
}

func pathID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.Invalid(c, "无效的用户 ID")
		return 0, false
	}
	return id, true
}

type createUserReq struct {
	Username string     `json:"username" binding:"required,min=2,max=64"`
	Password string     `json:"password" binding:"required,min=6,max=128"`
	Role     model.Role `json:"role" binding:"required"`
}

// Create POST /api/v1/users
func (h *userHandler) Create(c *gin.Context) {
	var req createUserReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Invalid(c, "用户名、密码（至少6位）、角色必填")
		return
	}
	u, err := h.svc.Create(req.Username, req.Password, req.Role)
	if err != nil {
		mapUserErr(c, err)
		return
	}
	response.OK(c, u)
}

// List GET /api/v1/users
func (h *userHandler) List(c *gin.Context) {
	users, err := h.svc.List()
	if err != nil {
		response.Internal(c, "查询失败")
		return
	}
	response.OK(c, users)
}

type roleReq struct {
	Role model.Role `json:"role" binding:"required"`
}

// SetRole PATCH /api/v1/users/:id/role
func (h *userHandler) SetRole(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var req roleReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Invalid(c, "角色必填")
		return
	}
	u, err := h.svc.SetRole(id, req.Role)
	if err != nil {
		mapUserErr(c, err)
		return
	}
	response.OK(c, u)
}

type statusReq struct {
	Status model.UserStatus `json:"status" binding:"required"`
}

// SetStatus PATCH /api/v1/users/:id/status
func (h *userHandler) SetStatus(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var req statusReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Invalid(c, "状态必填")
		return
	}
	u, err := h.svc.SetStatus(id, req.Status)
	if err != nil {
		mapUserErr(c, err)
		return
	}
	response.OK(c, u)
}

type resetPasswordReq struct {
	Password string `json:"password" binding:"required,min=6,max=128"`
}

// ResetPassword POST /api/v1/users/:id/password
func (h *userHandler) ResetPassword(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var req resetPasswordReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Invalid(c, "密码至少 6 位")
		return
	}
	if err := h.svc.ResetPassword(id, req.Password); err != nil {
		mapUserErr(c, err)
		return
	}
	response.OK(c, nil)
}

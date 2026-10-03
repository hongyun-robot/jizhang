package handler

import (
	"errors"

	"github.com/gin-gonic/gin"

	"jizhang/internal/middleware"
	"jizhang/internal/response"
	"jizhang/internal/service"
)

type closingHandler struct {
	svc *service.ClosingService
}

// NewClosing 封账/审计处理器。
func NewClosing(svc *service.ClosingService) *closingHandler {
	return &closingHandler{svc: svc}
}

func mapClosingErr(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrClosingInvalidPeriod),
		errors.Is(err, service.ErrClosingNotEnded):
		response.Invalid(c, err.Error())
	case errors.Is(err, service.ErrClosingAlreadyClosed),
		errors.Is(err, service.ErrClosingNotClosed):
		response.Conflict(c, err.Error())
	default:
		response.Internal(c, "操作失败")
	}
}

type closingReq struct {
	PeriodKey string `json:"period_key" binding:"required"`
}

// Preview GET /api/v1/closings/preview?period_key=
func (h *closingHandler) Preview(c *gin.Context) {
	key := c.Query("period_key")
	if key == "" {
		response.Invalid(c, "period_key 必填")
		return
	}
	p, err := h.svc.Preview(key)
	if err != nil {
		mapClosingErr(c, err)
		return
	}
	response.OK(c, p)
}

// List GET /api/v1/closings 已封账列表。
func (h *closingHandler) List(c *gin.Context) {
	list, err := h.svc.List()
	if err != nil {
		response.Internal(c, "查询失败")
		return
	}
	response.OK(c, list)
}

// Close POST /api/v1/closings（仅管理员）。
func (h *closingHandler) Close(c *gin.Context) {
	var req closingReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Invalid(c, "period_key 必填")
		return
	}
	rec, err := h.svc.Close(middleware.CurrentUser(c), req.PeriodKey)
	if err != nil {
		mapClosingErr(c, err)
		return
	}
	response.OK(c, rec)
}

// Reopen DELETE /api/v1/closings/:period_key（仅管理员）。
func (h *closingHandler) Reopen(c *gin.Context) {
	key := c.Param("period_key")
	if key == "" {
		response.Invalid(c, "period_key 必填")
		return
	}
	if err := h.svc.Reopen(middleware.CurrentUser(c), key); err != nil {
		mapClosingErr(c, err)
		return
	}
	response.OK(c, nil)
}

type auditQuery struct {
	Page     int `form:"page"`
	PageSize int `form:"page_size"`
}

// AuditList GET /api/v1/audit-logs（仅管理员）。
func (h *closingHandler) AuditList(c *gin.Context) {
	var q auditQuery
	_ = c.ShouldBindQuery(&q)
	page := q.Page
	if page < 1 {
		page = 1
	}
	size := q.PageSize
	if size < 1 || size > 100 {
		size = 20
	}
	items, total, err := h.svc.AuditLogs(page, size)
	if err != nil {
		response.Internal(c, "查询失败")
		return
	}
	response.OK(c, gin.H{
		"total":     total,
		"page":      page,
		"page_size": size,
		"items":     items,
	})
}

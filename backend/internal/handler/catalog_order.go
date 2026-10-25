package handler

import (
	"errors"
	"github.com/gin-gonic/gin"
	"jizhang/internal/middleware"
	"jizhang/internal/response"
	"jizhang/internal/service"
)

func respondOrder(c *gin.Context, out *service.CatalogOrderResult, err error) {
	switch {
	case err == nil:
		response.OK(c, out)
	case errors.Is(err, service.ErrTxnForbidden):
		response.Forbidden(c, "只能排序本人可见的条目")
	case errors.Is(err, service.ErrOrderInvalid):
		response.Invalid(c, err.Error())
	case errors.Is(err, service.ErrOrderNotFound):
		response.NotFound(c, err.Error())
	case errors.Is(err, service.ErrOrderConflict):
		response.Conflict(c, err.Error())
	default:
		response.Internal(c, "排序保存失败")
	}
}

func (h *txnHandler) Reorder(c *gin.Context) {
	var request struct {
		service.CatalogOrderInput
		RequestKey string `json:"request_key" binding:"required,max=128"`
	}
	if c.ShouldBindJSON(&request) != nil {
		response.Invalid(c, service.ErrOrderInvalid.Error())
		return
	}
	out, err := h.svc.Reorder(middleware.CurrentUser(c), request.CatalogOrderInput, request.RequestKey)
	respondOrder(c, out, err)
}
func (h *categoryHandler) Reorder(c *gin.Context) {
	var in service.CategoryOrderInput
	var request struct {
		service.CategoryOrderInput
		RequestKey string `json:"request_key" binding:"required,max=128"`
	}
	if c.ShouldBindJSON(&request) != nil {
		response.Invalid(c, service.ErrOrderInvalid.Error())
		return
	}
	in = request.CategoryOrderInput
	out, err := h.svc.ReorderFor(middleware.CurrentUser(c), in, request.RequestKey)
	respondOrder(c, out, err)
}
func (h *accountHandler) Reorder(c *gin.Context) {
	var in service.CatalogOrderInput
	var request struct {
		service.CatalogOrderInput
		RequestKey string `json:"request_key" binding:"required,max=128"`
	}
	if c.ShouldBindJSON(&request) != nil {
		response.Invalid(c, service.ErrOrderInvalid.Error())
		return
	}
	in = request.CatalogOrderInput
	out, err := h.svc.ReorderFor(middleware.CurrentUser(c), in, request.RequestKey)
	respondOrder(c, out, err)
}
func (h *voucherHandler) ReorderPlatforms(c *gin.Context) {
	var in service.CatalogOrderInput
	var request struct {
		service.CatalogOrderInput
		RequestKey string `json:"request_key" binding:"required,max=128"`
	}
	if c.ShouldBindJSON(&request) != nil {
		response.Invalid(c, service.ErrOrderInvalid.Error())
		return
	}
	in = request.CatalogOrderInput
	out, err := h.svc.ReorderPlatforms(middleware.CurrentUser(c), in, request.RequestKey)
	respondOrder(c, out, err)
}

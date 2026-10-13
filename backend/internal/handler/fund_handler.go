package handler

import (
	"errors"
	"github.com/gin-gonic/gin"
	"jizhang/internal/fuyao"
	"jizhang/internal/middleware"
	"jizhang/internal/response"
	"jizhang/internal/service"
	"strconv"
)

type fundHandler struct{ svc *service.FundService }

func NewFund(s *service.FundService) *fundHandler { return &fundHandler{svc: s} }
func fundError(c *gin.Context, e error) {
	switch {
	case errors.Is(e, service.ErrTxnForbidden):
		response.Forbidden(c, "只能按权限操作本人的基金资源")
	case errors.Is(e, service.ErrFundMissing), errors.Is(e, service.ErrAccountNotFound):
		response.NotFound(c, e.Error())
	case errors.Is(e, service.ErrFundInvalid), errors.Is(e, service.ErrFundCode), errors.Is(e, service.ErrTxnAccountInvalid):
		response.Invalid(c, e.Error())
	case errors.Is(e, service.ErrFundConflict), errors.Is(e, service.ErrFundHistory), errors.Is(e, service.ErrTxnClosedPeriod), errors.Is(e, service.ErrFundManaged), errors.Is(e, service.ErrFundPrice), errors.Is(e, fuyao.ErrUnconfigured), errors.Is(e, fuyao.ErrRateLimited), errors.Is(e, fuyao.ErrUnavailable):
		response.Conflict(c, e.Error())
	default:
		response.Internal(c, "基金操作暂时失败，请刷新核对后重试")
	}
}
func (h *fundHandler) Search(c *gin.Context) {
	rows, e := h.svc.Search(c.Request.Context(), middleware.CurrentUser(c), c.Query("q"))
	if e != nil {
		fundError(c, e)
		return
	}
	response.OK(c, rows)
}
func (h *fundHandler) Accounts(c *gin.Context) {
	rows, e := h.svc.Accounts(middleware.CurrentUser(c))
	if e != nil {
		fundError(c, e)
		return
	}
	response.OK(c, rows)
}
func (h *fundHandler) Account(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	v, e := h.svc.Account(middleware.CurrentUser(c), id)
	if e != nil {
		fundError(c, e)
		return
	}
	response.OK(c, v)
}
func (h *fundHandler) Position(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	v, e := h.svc.Position(middleware.CurrentUser(c), id)
	if e != nil {
		fundError(c, e)
		return
	}
	response.OK(c, v)
}
func (h *fundHandler) Opening(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var in service.FundInput
	if c.ShouldBindJSON(&in) != nil {
		response.Invalid(c, "基金资料格式无效")
		return
	}
	r, e := h.svc.CreateOpening(c.Request.Context(), middleware.CurrentUser(c), id, in)
	if e != nil {
		fundError(c, e)
		return
	}
	response.OK(c, r)
}
func (h *fundHandler) FirstBuy(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var in service.FundInput
	if c.ShouldBindJSON(&in) != nil {
		response.Invalid(c, "交易资料格式无效")
		return
	}
	r, e := h.svc.FirstBuy(c.Request.Context(), middleware.CurrentUser(c), id, in)
	if e != nil {
		fundError(c, e)
		return
	}
	response.OK(c, r)
}
func (h *fundHandler) Operate(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var in service.FundInput
	if c.ShouldBindJSON(&in) != nil {
		response.Invalid(c, "交易资料格式无效")
		return
	}
	r, e := h.svc.Operate(c.Request.Context(), middleware.CurrentUser(c), id, in)
	if e != nil {
		fundError(c, e)
		return
	}
	response.OK(c, r)
}
func (h *fundHandler) Amend(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var in service.FundInput
	if c.ShouldBindJSON(&in) != nil {
		response.Invalid(c, "期初资料格式无效")
		return
	}
	r, e := h.svc.AmendOpening(c.Request.Context(), middleware.CurrentUser(c), id, in)
	if e != nil {
		fundError(c, e)
		return
	}
	response.OK(c, r)
}
func (h *fundHandler) Undo(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	event, e := strconv.ParseInt(c.Param("eventId"), 10, 64)
	if e != nil || event <= 0 {
		response.Invalid(c, "事件编号无效")
		return
	}
	var in struct {
		RequestKey string `json:"request_key"`
	}
	if c.ShouldBindJSON(&in) != nil {
		response.Invalid(c, "请求标识无效")
		return
	}
	r, e := h.svc.Undo(middleware.CurrentUser(c), id, event, in.RequestKey)
	if e != nil {
		fundError(c, e)
		return
	}
	response.OK(c, r)
}
func (h *fundHandler) Events(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	r, e := h.svc.Events(middleware.CurrentUser(c), id)
	if e != nil {
		fundError(c, e)
		return
	}
	response.OK(c, r)
}
func (h *fundHandler) Audits(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	r, e := h.svc.Audits(middleware.CurrentUser(c), id)
	if e != nil {
		fundError(c, e)
		return
	}
	response.OK(c, r)
}
func (h *fundHandler) Dividends(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	r, e := h.svc.DividendHints(middleware.CurrentUser(c), id)
	if e != nil {
		fundError(c, e)
		return
	}
	response.OK(c, r)
}
func (h *fundHandler) Valuations(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	r, e := h.svc.Valuations(middleware.CurrentUser(c), id, c.Query("start"), c.Query("end"))
	if e != nil {
		fundError(c, e)
		return
	}
	response.OK(c, r)
}
func (h *fundHandler) Refresh(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	r, e := h.svc.Refresh(c.Request.Context(), middleware.CurrentUser(c), id)
	if e != nil {
		fundError(c, e)
		return
	}
	response.OK(c, r)
}

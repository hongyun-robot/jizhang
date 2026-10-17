package handler

import (
	"errors"
	"github.com/gin-gonic/gin"
	"jizhang/internal/middleware"
	"jizhang/internal/response"
	"jizhang/internal/service"
	"jizhang/internal/wealthdata"
	"strconv"
)

type wealthHandler struct{ svc *service.WealthService }

func NewWealth(s *service.WealthService) *wealthHandler { return &wealthHandler{svc: s} }
func wealthError(c *gin.Context, e error) {
	switch {
	case errors.Is(e, service.ErrWealthCashSetup):
		response.Conflict(c, e.Error())
	case errors.Is(e, service.ErrTxnForbidden):
		response.Forbidden(c, "只能按权限操作本人的理财资源")
	case errors.Is(e, service.ErrWealthMissing), errors.Is(e, service.ErrAccountNotFound):
		response.NotFound(c, e.Error())
	case errors.Is(e, service.ErrWealthInvalid), errors.Is(e, service.ErrWealthCode), errors.Is(e, service.ErrTxnAccountInvalid):
		response.Invalid(c, e.Error())
	case errors.Is(e, service.ErrWealthAutomatic), errors.Is(e, service.ErrWealthUnregistered), errors.Is(e, service.ErrWealthConflict), errors.Is(e, service.ErrWealthHistory), errors.Is(e, service.ErrTxnClosedPeriod), errors.Is(e, service.ErrWealthManaged), errors.Is(e, service.ErrWealthPrice), errors.Is(e, wealthdata.ErrUnconfigured), errors.Is(e, wealthdata.ErrRateLimited), errors.Is(e, wealthdata.ErrUnavailable):
		response.Conflict(c, e.Error())
	default:
		response.Internal(c, "理财操作暂时失败，请刷新核对后重试")
	}
}
func (h *wealthHandler) Catalog(c *gin.Context) {
	rows, e := h.svc.Catalog(middleware.CurrentUser(c))
	if e != nil {
		wealthError(c, e)
		return
	}
	response.OK(c, rows)
}
func (h *wealthHandler) Accounts(c *gin.Context) {
	rows, e := h.svc.Accounts(middleware.CurrentUser(c))
	if e != nil {
		wealthError(c, e)
		return
	}
	response.OK(c, rows)
}
func (h *wealthHandler) Account(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	v, e := h.svc.Account(middleware.CurrentUser(c), id)
	if e != nil {
		wealthError(c, e)
		return
	}
	response.OK(c, v)
}
func (h *wealthHandler) Position(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	v, e := h.svc.Position(middleware.CurrentUser(c), id)
	if e != nil {
		wealthError(c, e)
		return
	}
	response.OK(c, v)
}
func (h *wealthHandler) Opening(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var in service.WealthInput
	if c.ShouldBindJSON(&in) != nil {
		response.Invalid(c, "理财资料格式无效")
		return
	}
	r, e := h.svc.CreateOpening(c.Request.Context(), middleware.CurrentUser(c), id, in)
	if e != nil {
		wealthError(c, e)
		return
	}
	response.OK(c, r)
}
func (h *wealthHandler) FirstBuy(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var in service.WealthInput
	if c.ShouldBindJSON(&in) != nil {
		response.Invalid(c, "交易资料格式无效")
		return
	}
	r, e := h.svc.FirstBuy(c.Request.Context(), middleware.CurrentUser(c), id, in)
	if e != nil {
		wealthError(c, e)
		return
	}
	response.OK(c, r)
}
func (h *wealthHandler) Subscribe(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var in service.WealthInput
	if c.ShouldBindJSON(&in) != nil {
		response.Invalid(c, "申购申请资料格式无效")
		return
	}
	r, e := h.svc.Subscribe(c.Request.Context(), middleware.CurrentUser(c), id, in)
	if e != nil {
		wealthError(c, e)
		return
	}
	response.OK(c, r)
}
func (h *wealthHandler) OrderPreview(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var in service.WealthInput
	if c.ShouldBindJSON(&in) != nil {
		response.Invalid(c, "转出申请资料格式无效")
		return
	}
	r, e := h.svc.PreviewOrder(middleware.CurrentUser(c), id, in)
	if e != nil {
		wealthError(c, e)
		return
	}
	response.OK(c, r)
}
func (h *wealthHandler) Operate(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var in service.WealthInput
	if c.ShouldBindJSON(&in) != nil {
		response.Invalid(c, "交易资料格式无效")
		return
	}
	r, e := h.svc.Operate(c.Request.Context(), middleware.CurrentUser(c), id, in)
	if e != nil {
		wealthError(c, e)
		return
	}
	response.OK(c, r)
}
func (h *wealthHandler) Amend(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var in service.WealthInput
	if c.ShouldBindJSON(&in) != nil {
		response.Invalid(c, "期初资料格式无效")
		return
	}
	r, e := h.svc.AmendOpening(c.Request.Context(), middleware.CurrentUser(c), id, in)
	if e != nil {
		wealthError(c, e)
		return
	}
	response.OK(c, r)
}
func (h *wealthHandler) Undo(c *gin.Context) {
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
		wealthError(c, e)
		return
	}
	response.OK(c, r)
}
func (h *wealthHandler) Events(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	r, e := h.svc.Events(middleware.CurrentUser(c), id)
	if e != nil {
		wealthError(c, e)
		return
	}
	response.OK(c, r)
}
func (h *wealthHandler) Audits(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	r, e := h.svc.Audits(middleware.CurrentUser(c), id)
	if e != nil {
		wealthError(c, e)
		return
	}
	response.OK(c, r)
}
func (h *wealthHandler) Valuations(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	r, e := h.svc.Valuations(middleware.CurrentUser(c), id, c.Query("start"), c.Query("end"))
	if e != nil {
		wealthError(c, e)
		return
	}
	response.OK(c, r)
}
func (h *wealthHandler) Refresh(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	r, e := h.svc.Refresh(c.Request.Context(), middleware.CurrentUser(c), id)
	if e != nil {
		wealthError(c, e)
		return
	}
	response.OK(c, r)
}

func (h *wealthHandler) Quotes(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	rows, e := h.svc.EffectiveNAVs(middleware.CurrentUser(c), id)
	if e != nil {
		wealthError(c, e)
		return
	}
	response.OK(c, rows)
}
func (h *wealthHandler) ManualNAVs(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	rows, e := h.svc.ManualNAVs(middleware.CurrentUser(c), id)
	if e != nil {
		wealthError(c, e)
		return
	}
	response.OK(c, rows)
}
func (h *wealthHandler) ManualAudits(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	rows, e := h.svc.ManualAudits(middleware.CurrentUser(c), id)
	if e != nil {
		wealthError(c, e)
		return
	}
	response.OK(c, rows)
}
func (h *wealthHandler) WriteManual(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var in service.WealthManualInput
	if c.ShouldBindJSON(&in) != nil {
		response.Invalid(c, "净值补录格式无效")
		return
	}
	receipt, e := h.svc.WriteManualNAV(middleware.CurrentUser(c), id, in)
	if e != nil {
		wealthError(c, e)
		return
	}
	response.OK(c, receipt)
}
func (h *wealthHandler) RefreshProduct(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	row, e := h.svc.RefreshProduct(c.Request.Context(), middleware.CurrentUser(c), id)
	if e != nil {
		wealthError(c, e)
		return
	}
	response.OK(c, row)
}

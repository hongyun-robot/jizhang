package handler

import (
	"encoding/json"
	"errors"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"io"
	"jizhang/internal/liability"
	"jizhang/internal/middleware"
	"jizhang/internal/model"
	"jizhang/internal/response"
	"jizhang/internal/service"
)

type liabilityHandler struct{ svc *service.LiabilityService }

func NewLiability(s *service.LiabilityService) *liabilityHandler { return &liabilityHandler{svc: s} }
func mapLiabilityErr(c *gin.Context, e error) {
	switch {
	case errors.Is(e, service.ErrTxnForbidden):
		response.Forbidden(c, e.Error())
	case errors.Is(e, gorm.ErrRecordNotFound), errors.Is(e, service.ErrAccountNotFound), errors.Is(e, service.ErrTxnNotFound):
		response.NotFound(c, "负债记录不存在")
	case errors.Is(e, liability.ErrInvalid):
		response.Invalid(c, "请核对日期、金额、账单分配和手续费配置")
	case errors.Is(e, liability.ErrConflict), errors.Is(e, liability.ErrManaged), errors.Is(e, liability.ErrSetup), errors.Is(e, liability.ErrInvariant):
		response.Conflict(c, e.Error())
	default:
		mapTxnErr(c, e)
	}
}
func liabilityReply(c *gin.Context, data any, e error) {
	if e != nil {
		mapLiabilityErr(c, e)
		return
	}
	response.OK(c, data)
}
func (h *liabilityHandler) List(c *gin.Context) {
	x, e := h.svc.List(middleware.CurrentUser(c))
	liabilityReply(c, x, e)
}
func (h *liabilityHandler) Detail(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	x, e := h.svc.Detail(middleware.CurrentUser(c), id)
	liabilityReply(c, x, e)
}
func (h *liabilityHandler) Settings(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var in service.LiabilitySettingsInput
	if e := c.ShouldBindJSON(&in); e != nil {
		response.Invalid(c, "账单规则格式无效")
		return
	}
	x, e := h.svc.Settings(middleware.CurrentUser(c), id, in)
	liabilityReply(c, x, e)
}
func (h *liabilityHandler) Ensure(c *gin.Context) {
	n, e := h.svc.EnsureDue(h.svc.Now())
	liabilityReply(c, gin.H{"generated": n}, e)
}
func (h *liabilityHandler) Bills(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	d, e := h.svc.Detail(middleware.CurrentUser(c), id)
	if e != nil {
		mapLiabilityErr(c, e)
		return
	}
	response.OK(c, d.Statements)
}
func (h *liabilityHandler) Bill(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	d, e := h.svc.Statement(middleware.CurrentUser(c), id)
	if e != nil {
		mapLiabilityErr(c, e)
		return
	}
	var bill model.LiabilityStatement
	items := []model.LiabilityDebtItem{}
	for _, b := range d.Statements {
		if b.ID == id {
			bill = b
		}
	}
	for _, x := range d.Items {
		if x.StatementID == id {
			items = append(items, x)
		}
	}
	response.OK(c, gin.H{"statement": bill, "items": items, "account": d.Account, "allocations": d.Allocations, "transactions": d.Transactions, "operations": d.Operations})
}
func (h *liabilityHandler) Reconcile(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var in service.LiabilityReconcileInput
	if e := c.ShouldBindJSON(&in); e != nil {
		response.Invalid(c, "核对输入无效")
		return
	}
	x, e := h.svc.Reconcile(middleware.CurrentUser(c), id, in)
	liabilityReply(c, x, e)
}
func ordinaryRepaymentInput(c *gin.Context) (service.LiabilityRepaymentInput, bool) {
	var in service.LiabilityRepaymentInput
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&in); err != nil {
		response.Invalid(c, "还款输入无效")
		return in, false
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		response.Invalid(c, "还款输入无效")
		return in, false
	}
	return in, true
}
func (h *liabilityHandler) Repay(c *gin.Context) {
	in, ok := ordinaryRepaymentInput(c)
	if !ok {
		return
	}
	x, err := h.svc.Repay(middleware.CurrentUser(c), in)
	liabilityReply(c, x, err)
}
func (h *liabilityHandler) OrdinaryRepaymentPreview(c *gin.Context) {
	in, ok := ordinaryRepaymentInput(c)
	if !ok {
		return
	}
	x, err := h.svc.OrdinaryRepaymentPreview(middleware.CurrentUser(c), in)
	liabilityReply(c, x, err)
}
func (h *liabilityHandler) Allocate(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var in service.LiabilityAllocationInput
	if e := c.ShouldBindJSON(&in); e != nil {
		response.Invalid(c, "分配输入无效")
		return
	}
	x, e := h.svc.Allocate(middleware.CurrentUser(c), id, in)
	liabilityReply(c, x, e)
}
func (h *liabilityHandler) CreatePlan(c *gin.Context) {
	var in service.InstallmentInput
	if e := c.ShouldBindJSON(&in); e != nil {
		response.Invalid(c, "分期输入无效")
		return
	}
	x, e := h.svc.CreatePlan(middleware.CurrentUser(c), in)
	liabilityReply(c, x, e)
}
func (h *liabilityHandler) Plan(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	x, e := h.svc.Plan(middleware.CurrentUser(c), id)
	liabilityReply(c, x, e)
}
func (h *liabilityHandler) UpdatePlan(c *gin.Context) { h.updatePlan(c, false) }
func (h *liabilityHandler) RefundPlan(c *gin.Context) { h.updatePlan(c, true) }
func (h *liabilityHandler) updatePlan(c *gin.Context, refund bool) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var in service.InstallmentUpdate
	if e := c.ShouldBindJSON(&in); e != nil {
		response.Invalid(c, "期次调整输入无效")
		return
	}
	x, e := h.svc.UpdatePlan(middleware.CurrentUser(c), id, in, refund)
	liabilityReply(c, x, e)
}
func (h *liabilityHandler) Preview(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var in service.SettlementInput
	if e := c.ShouldBindJSON(&in); e != nil {
		response.Invalid(c, "结清输入无效")
		return
	}
	x, e := h.svc.SettlementPreview(middleware.CurrentUser(c), id, in)
	liabilityReply(c, x, e)
}
func (h *liabilityHandler) Settle(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var in service.SettlementInput
	if e := c.ShouldBindJSON(&in); e != nil {
		response.Invalid(c, "结清输入无效")
		return
	}
	x, e := h.svc.Settle(middleware.CurrentUser(c), id, in)
	liabilityReply(c, x, e)
}

func repaymentInput(c *gin.Context) (service.InstallmentRepaymentInput, bool) {
	var in service.InstallmentRepaymentInput
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&in); err != nil {
		response.Invalid(c, "整期还款输入无效，请重新核对期次，不支持手填本金或手续费")
		return in, false
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		response.Invalid(c, "还款输入须为单个对象")
		return in, false
	}
	return in, true
}

func (h *liabilityHandler) RepaymentPreview(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	in, ok := repaymentInput(c)
	if !ok {
		return
	}
	preview, err := h.svc.InstallmentRepaymentPreview(middleware.CurrentUser(c), id, in)
	liabilityReply(c, preview, err)
}

func (h *liabilityHandler) RepayInstallment(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	in, ok := repaymentInput(c)
	if !ok {
		return
	}
	result, err := h.svc.RepayInstallment(middleware.CurrentUser(c), id, in)
	liabilityReply(c, result, err)
}
func (h *liabilityHandler) Reverse(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var in service.ReverseInput
	if e := c.ShouldBindJSON(&in); e != nil {
		response.Invalid(c, "修正输入无效")
		return
	}
	x, e := h.svc.Reverse(middleware.CurrentUser(c), id, in)
	liabilityReply(c, x, e)
}

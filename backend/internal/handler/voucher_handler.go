package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/shopspring/decimal"
	"jizhang/internal/middleware"
	"jizhang/internal/model"
	"jizhang/internal/response"
	"jizhang/internal/service"
)

type voucherHandler struct{ svc *service.VoucherService }

func NewVoucher(svc *service.VoucherService) *voucherHandler { return &voucherHandler{svc: svc} }

func (h *voucherHandler) ListPlatforms(c *gin.Context) {
	rows, err := h.svc.ListPlatforms(middleware.CurrentUser(c))
	if err != nil {
		mapPlatformErr(c, err)
		return
	}
	response.OK(c, rows)
}

func (h *voucherHandler) CreatePlatform(c *gin.Context) {
	var req struct {
		Name string `json:"name"`
	}
	if !strictVoucherJSON(c, &req) {
		return
	}
	row, err := h.svc.CreatePlatform(middleware.CurrentUser(c), req.Name)
	if err != nil {
		mapPlatformErr(c, err)
		return
	}
	response.OK(c, row)
}

func mapPlatformErr(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrTxnForbidden):
		response.Forbidden(c, "只能管理自己的平台 / 商家")
	case errors.Is(err, service.ErrPlatformNotFound):
		response.NotFound(c, err.Error())
	case errors.Is(err, service.ErrPlatformDuplicate), errors.Is(err, service.ErrPlatformReferenced):
		response.Conflict(c, err.Error())
	case errors.Is(err, service.ErrVoucherInvalid):
		response.Invalid(c, err.Error())
	default:
		response.Internal(c, "平台 / 商家操作失败，请重试")
	}
}

func (h *voucherHandler) RenamePlatform(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if !strictVoucherJSON(c, &req) {
		return
	}
	row, err := h.svc.RenamePlatform(middleware.CurrentUser(c), id, req.Name)
	if err != nil {
		mapPlatformErr(c, err)
		return
	}
	response.OK(c, row)
}

func (h *voucherHandler) DeletePlatform(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	if err := h.svc.DeletePlatform(middleware.CurrentUser(c), id); err != nil {
		mapPlatformErr(c, err)
		return
	}
	response.OK(c, nil)
}

func strictVoucherJSON(c *gin.Context, req any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 16*1024))
	dec.DisallowUnknownFields()
	if err := dec.Decode(req); err != nil {
		response.Invalid(c, "券参数格式无效，金额和结算份额由券操作计算")
		return false
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		response.Invalid(c, "请求必须只包含一份券参数")
		return false
	}
	if err := binding.Validator.ValidateStruct(req); err != nil {
		response.Invalid(c, "请填写完整的券信息")
		return false
	}
	return true
}
func voucherErr(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrVoucherNotFound):
		response.NotFound(c, err.Error())
	case errors.Is(err, service.ErrTxnForbidden):
		response.Forbidden(c, err.Error())
	case errors.Is(err, service.ErrVoucherInvalid), errors.Is(err, service.ErrTxnAmountInvalid), errors.Is(err, service.ErrTxnDateInvalid), errors.Is(err, service.ErrTxnAccountInvalid), errors.Is(err, service.ErrTxnCategoryInvalid):
		response.Invalid(c, err.Error())
	case errors.Is(err, service.ErrTxnClosedPeriod), errors.Is(err, service.ErrVoucherSettled), errors.Is(err, service.ErrVoucherDependent), errors.Is(err, service.ErrVoucherKeyConflict), errors.Is(err, service.ErrVoucherUsed):
		response.Conflict(c, err.Error())
	case errors.Is(err, service.ErrFundManaged):
		response.Invalid(c, err.Error())
	default:
		response.Internal(c, "券操作失败")
	}
}

type voucherReq struct {
	Name              string  `json:"name" binding:"required,max=128"`
	Platform          string  `json:"platform"`
	Note              string  `json:"note" binding:"max=255"`
	PaidAmount        string  `json:"paid_amount" binding:"required"`
	PurchaseDate      string  `json:"purchase_date" binding:"required"`
	AccountID         int64   `json:"account_id" binding:"required"`
	CategoryID        int64   `json:"category_id" binding:"required"`
	TotalUnits        int64   `json:"total_units" binding:"required"`
	ExpiresOn         *string `json:"expires_on"`
	AutoRefundEnabled *bool   `json:"auto_refund_enabled"`
	ResumeAutoRefund  bool    `json:"resume_auto_refund"`
	RequestKey        string  `json:"request_key"`
}

func (r voucherReq) parse() (service.VoucherInput, error) {
	amount, err := decimal.NewFromString(r.PaidAmount)
	if err != nil {
		return service.VoucherInput{}, service.ErrTxnAmountInvalid
	}
	date, err := service.ParseVoucherDate(r.PurchaseDate)
	if err != nil {
		return service.VoucherInput{}, service.ErrTxnDateInvalid
	}
	var expires *time.Time
	if r.ExpiresOn != nil && *r.ExpiresOn != "" {
		d, e := service.ParseVoucherDate(*r.ExpiresOn)
		if e != nil {
			return service.VoucherInput{}, service.ErrTxnDateInvalid
		}
		expires = &d
	}
	auto := expires != nil
	if r.AutoRefundEnabled != nil {
		auto = *r.AutoRefundEnabled
	}
	return service.VoucherInput{Name: r.Name, Platform: r.Platform, Note: r.Note, PaidAmount: amount, PurchaseDate: date, AccountID: r.AccountID, CategoryID: r.CategoryID, TotalUnits: r.TotalUnits, ExpiresOn: expires, AutoRefundEnabled: auto, ResumeAutoRefund: r.ResumeAutoRefund, RequestKey: r.RequestKey}, nil
}
func (h *voucherHandler) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	out, err := h.svc.List(middleware.CurrentUser(c), c.Query("keyword"), c.Query("status"), page, size)
	if err != nil {
		voucherErr(c, err)
		return
	}
	response.OK(c, out)
}
func (h *voucherHandler) Detail(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	h.detail(c, id)
}
func (h *voucherHandler) detail(c *gin.Context, id int64) {
	out, err := h.svc.Detail(middleware.CurrentUser(c), id)
	if err != nil {
		voucherErr(c, err)
		return
	}
	response.OK(c, out)
}
func (h *voucherHandler) Create(c *gin.Context) {
	var req voucherReq
	if !strictVoucherJSON(c, &req) {
		return
	}
	in, err := req.parse()
	if err != nil {
		voucherErr(c, err)
		return
	}
	v, err := h.svc.Create(middleware.CurrentUser(c), in)
	if err != nil {
		voucherErr(c, err)
		return
	}
	h.detail(c, v.ID)
}
func (h *voucherHandler) Update(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var req voucherReq
	if !strictVoucherJSON(c, &req) {
		return
	}
	in, err := req.parse()
	if err != nil {
		voucherErr(c, err)
		return
	}
	v, err := h.svc.Update(middleware.CurrentUser(c), id, in)
	if err != nil {
		voucherErr(c, err)
		return
	}
	h.detail(c, v.ID)
}
func (h *voucherHandler) Delete(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	if err := h.svc.Delete(middleware.CurrentUser(c), id); err != nil {
		voucherErr(c, err)
		return
	}
	response.OK(c, nil)
}

type voucherOpReq struct {
	EntryDate  string `json:"entry_date" binding:"required"`
	Units      int64  `json:"units"`
	RequestKey string `json:"request_key" binding:"required"`
}

func (h *voucherHandler) Operate(action model.VoucherAction) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := pathID(c)
		if !ok {
			return
		}
		var req voucherOpReq
		if !strictVoucherJSON(c, &req) {
			return
		}
		date, err := service.ParseVoucherDate(req.EntryDate)
		if err != nil {
			voucherErr(c, service.ErrTxnDateInvalid)
			return
		}
		if _, err := h.svc.Operate(middleware.CurrentUser(c), id, action, service.VoucherOperation{EntryDate: date, Units: req.Units, RequestKey: req.RequestKey}); err != nil {
			voucherErr(c, err)
			return
		}
		h.detail(c, id)
	}
}
func (h *voucherHandler) Undo(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	eventID, err := strconv.ParseInt(c.Param("eventId"), 10, 64)
	if err != nil || eventID < 1 {
		response.Invalid(c, "操作标识无效")
		return
	}
	if err := h.svc.Undo(middleware.CurrentUser(c), id, eventID); err != nil {
		voucherErr(c, err)
		return
	}
	h.detail(c, id)
}
func (h *voucherHandler) Ensure(c *gin.Context) {
	if !service.CanCreateTransaction(middleware.CurrentUser(c)) {
		response.Forbidden(c, "无权补处理券退款")
		return
	}
	out, err := h.svc.EnsureDue(time.Now())
	if err != nil {
		voucherErr(c, err)
		return
	}
	response.OK(c, out)
}

package handler

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"jizhang/internal/middleware"
	"jizhang/internal/model"
	"jizhang/internal/repository"
	"jizhang/internal/response"
	"jizhang/internal/service"
)

type txnHandler struct {
	svc *service.TransactionService
}

// NewTxn 流水处理器。
func NewTxn(svc *service.TransactionService) *txnHandler {
	return &txnHandler{svc: svc}
}

type txnReq struct {
	Kind              model.TxnKind `json:"kind" binding:"required"`
	Amount            string        `json:"amount" binding:"required"`
	AccountID         int64         `json:"account_id"` // 必填语义由服务层校验；退款带关联时可省略（按原支出预填）
	TransferAccountID int64         `json:"transfer_account_id"`
	CategoryID        int64         `json:"category_id"`
	RelatedTxnID      int64         `json:"related_txn_id"` // 退款关联原支出，0=无
	EntryDate         string        `json:"entry_date" binding:"required"`
	Note              string        `json:"note" binding:"max=255"`
}

func (r *txnReq) parse() (service.TxnInput, bool) {
	amt, err := decimal.NewFromString(r.Amount)
	if err != nil {
		return service.TxnInput{}, false
	}
	d, err := time.ParseInLocation("2006-01-02", r.EntryDate, time.Local)
	if err != nil {
		return service.TxnInput{}, false
	}
	return service.TxnInput{
		Kind:              r.Kind,
		Amount:            amt,
		AccountID:         r.AccountID,
		TransferAccountID: r.TransferAccountID,
		CategoryID:        r.CategoryID,
		RelatedTxnID:      r.RelatedTxnID,
		EntryDate:         d,
		Note:              r.Note,
	}, true
}

func mapTxnErr(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrTxnNotFound):
		response.NotFound(c, err.Error())
	case errors.Is(err, service.ErrTxnForbidden):
		response.Forbidden(c, err.Error())
	case errors.Is(err, service.ErrTxnClosedPeriod):
		response.Conflict(c, err.Error())
	case errors.Is(err, service.ErrTxnAmountInvalid),
		errors.Is(err, service.ErrTxnAccountInvalid),
		errors.Is(err, service.ErrTxnCategoryInvalid),
		errors.Is(err, service.ErrTxnTransferSame),
		errors.Is(err, service.ErrTxnKindInvalid),
		errors.Is(err, service.ErrTxnDateInvalid),
		errors.Is(err, service.ErrTxnRelatedInvalid):
		response.Invalid(c, err.Error())
	case errors.Is(err, service.ErrTxnRefundsExist):
		response.Conflict(c, err.Error())
	default:
		response.Internal(c, "操作失败")
	}
}

// Create POST /api/v1/transactions
func (h *txnHandler) Create(c *gin.Context) {
	var req txnReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Invalid(c, "类型、金额、发生日期必填")
		return
	}
	in, ok := req.parse()
	if !ok {
		response.Invalid(c, response.MsgInvalidAmount)
		return
	}
	t, err := h.svc.Create(middleware.CurrentUser(c), in)
	if err != nil {
		mapTxnErr(c, err)
		return
	}
	response.OK(c, t)
}

// Update PUT /api/v1/transactions/:id
func (h *txnHandler) Update(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var req txnReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Invalid(c, "类型、金额、发生日期必填")
		return
	}
	in, ok := req.parse()
	if !ok {
		response.Invalid(c, response.MsgInvalidAmount)
		return
	}
	t, err := h.svc.Update(middleware.CurrentUser(c), id, in)
	if err != nil {
		mapTxnErr(c, err)
		return
	}
	response.OK(c, t)
}

// Delete DELETE /api/v1/transactions/:id
func (h *txnHandler) Delete(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	if err := h.svc.Delete(middleware.CurrentUser(c), id); err != nil {
		mapTxnErr(c, err)
		return
	}
	response.OK(c, nil)
}

type txnListQuery struct {
	PeriodKey    string `form:"period_key"`
	StartDate    string `form:"start_date"`
	EndDate      string `form:"end_date"`
	Kind         string `form:"kind"`
	AccountID    int64  `form:"account_id"`
	CategoryID   int64  `form:"category_id"`
	AmountMin    string `form:"amount_min"`
	AmountMax    string `form:"amount_max"`
	Keyword      string `form:"keyword"`
	RelatedTxnID int64  `form:"related_txn_id"`
	Page         int    `form:"page"`
	PageSize     int    `form:"page_size"`
}

// buildFilter 查询参数 → 过滤条件（List 与 Export 共用，保证导出与列表同一套筛选语义）。
func buildFilter(q txnListQuery) repository.TxnFilter {
	f := repository.TxnFilter{
		PeriodKey:    q.PeriodKey,
		Kind:         model.TxnKind(q.Kind),
		AccountID:    q.AccountID,
		CategoryID:   q.CategoryID,
		Keyword:      q.Keyword,
		RelatedTxnID: q.RelatedTxnID,
		Page:         q.Page,
		PageSize:     q.PageSize,
	}
	if q.AmountMin != "" {
		f.AmountMin = &q.AmountMin
	}
	if q.AmountMax != "" {
		f.AmountMax = &q.AmountMax
	}
	if q.StartDate != "" {
		f.StartDate = &q.StartDate
	}
	if q.EndDate != "" {
		f.EndDate = &q.EndDate
	}
	return f
}

// List GET /api/v1/transactions
func (h *txnHandler) List(c *gin.Context) {
	var q txnListQuery
	if err := c.ShouldBindQuery(&q); err != nil {
		response.Invalid(c, "查询参数无效")
		return
	}
	list, total, err := h.svc.List(middleware.CurrentUser(c), buildFilter(q))
	if err != nil {
		mapTxnErr(c, err)
		return
	}
	page := q.Page
	if page < 1 {
		page = 1
	}
	size := q.PageSize
	if size < 1 || size > 100 {
		size = 20
	}
	response.OK(c, gin.H{
		"total":     total,
		"page":      page,
		"page_size": size,
		"items":     list,
	})
}

// Export GET /api/v1/transactions/export 按当前筛选导出 CSV（UTF-8 BOM，任务 11.1）。
func (h *txnHandler) Export(c *gin.Context) {
	var q txnListQuery
	if err := c.ShouldBindQuery(&q); err != nil {
		response.Invalid(c, "查询参数无效")
		return
	}
	data, err := h.svc.ExportCSV(middleware.CurrentUser(c), buildFilter(q))
	if err != nil {
		mapTxnErr(c, err)
		return
	}
	name := fmt.Sprintf("transactions-%s.csv", time.Now().Format("2006-01-02"))
	c.Header("Content-Disposition", `attachment; filename="`+name+`"`)
	c.Data(http.StatusOK, "text/csv; charset=utf-8", data)
}

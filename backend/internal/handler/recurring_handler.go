package handler

import (
	"errors"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"jizhang/internal/middleware"
	"jizhang/internal/model"
	"jizhang/internal/response"
	"jizhang/internal/service"
)

type recurringHandler struct {
	svc *service.RecurringService
}

// NewRecurring 周期记账处理器（specs/recurring-entries，任务 9.1–9.3）。
func NewRecurring(svc *service.RecurringService) *recurringHandler {
	return &recurringHandler{svc: svc}
}

func mapRuleErr(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrRuleNotFound):
		response.NotFound(c, err.Error())
	case errors.Is(err, service.ErrRuleForbidden):
		response.Forbidden(c, err.Error())
	case errors.Is(err, service.ErrRuleAmountInvalid),
		errors.Is(err, service.ErrRuleFreqInvalid),
		errors.Is(err, service.ErrRuleDateInvalid),
		errors.Is(err, service.ErrRuleAccountInvalid),
		errors.Is(err, service.ErrRuleCategoryInvalid),
		errors.Is(err, service.ErrRuleStatusInvalid),
		errors.Is(err, service.ErrRuleCatchUpTooLong):
		response.Invalid(c, err.Error())
	default:
		response.Internal(c, "操作失败")
	}
}

type ruleReq struct {
	Amount     string `json:"amount" binding:"required"`
	Freq       string `json:"freq" binding:"required"`
	AccountID  int64  `json:"account_id" binding:"required"`
	CategoryID int64  `json:"category_id" binding:"required"`
	Note       string `json:"note" binding:"max=255"`
	StartDate  string `json:"start_date" binding:"required"`
}

func (r *ruleReq) parse() (service.RuleInput, bool) {
	amt, err := decimal.NewFromString(r.Amount)
	if err != nil {
		return service.RuleInput{}, false
	}
	d, err := time.ParseInLocation("2006-01-02", r.StartDate, time.Local)
	if err != nil {
		return service.RuleInput{}, false
	}
	return service.RuleInput{
		Amount:     amt,
		Freq:       model.RuleFreq(r.Freq),
		AccountID:  r.AccountID,
		CategoryID: r.CategoryID,
		Note:       r.Note,
		StartDate:  d,
	}, true
}

// List GET /api/v1/recurring 模板列表（登录即可读；作用域按创建者归属，管理员全量）。
func (h *recurringHandler) List(c *gin.Context) {
	list, err := h.svc.List(middleware.CurrentUser(c))
	if err != nil {
		mapRuleErr(c, err)
		return
	}
	response.OK(c, list)
}

// Create POST /api/v1/recurring 创建模板（管理员/记账员/普通成员，只读 403）。
func (h *recurringHandler) Create(c *gin.Context) {
	var req ruleReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Invalid(c, "金额、频率、账户、分类、首次生成日期必填")
		return
	}
	in, ok := req.parse()
	if !ok {
		response.Invalid(c, "金额或首次生成日期格式无效")
		return
	}
	r, err := h.svc.Create(middleware.CurrentUser(c), in)
	if err != nil {
		mapRuleErr(c, err)
		return
	}
	response.OK(c, r)
}

// Update PUT /api/v1/recurring/:id 修改模板（按创建者归属：管理员全量，其余仅自己的）。
func (h *recurringHandler) Update(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var req ruleReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Invalid(c, "金额、频率、账户、分类、首次生成日期必填")
		return
	}
	in, ok2 := req.parse()
	if !ok2 {
		response.Invalid(c, "金额或首次生成日期格式无效")
		return
	}
	r, err := h.svc.Update(middleware.CurrentUser(c), id, in)
	if err != nil {
		mapRuleErr(c, err)
		return
	}
	response.OK(c, r)
}

// SetStatus PATCH /api/v1/recurring/:id/status 启用/暂停（按创建者归属）。
func (h *recurringHandler) SetStatus(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var req struct {
		Status string `json:"status" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Invalid(c, "状态必填")
		return
	}
	if err := h.svc.SetStatus(middleware.CurrentUser(c), id, model.RuleStatus(req.Status)); err != nil {
		mapRuleErr(c, err)
		return
	}
	response.OK(c, gin.H{"id": id, "status": req.Status})
}

// Delete DELETE /api/v1/recurring/:id 删除模板（按创建者归属）。
func (h *recurringHandler) Delete(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	if err := h.svc.Delete(middleware.CurrentUser(c), id); err != nil {
		mapRuleErr(c, err)
		return
	}
	response.OK(c, nil)
}

// Ensure POST /api/v1/recurring/ensure 懒补齐（任意登录角色可触发；生成本身幂等）。
// 前端每次打开应用时调用，兜底 cron 失效（specs/recurring-entries「打开页面懒补齐」）；
// 懒补齐路径走 EnsureDueLazy：有新生成时落一条「已补记」提醒（specs/notifications）。
func (h *recurringHandler) Ensure(c *gin.Context) {
	n, err := h.svc.EnsureDueLazy(time.Now())
	if err != nil {
		mapRuleErr(c, err)
		return
	}
	response.OK(c, gin.H{"generated": n})
}

package handler

import (
	"errors"

	"github.com/gin-gonic/gin"

	"jizhang/internal/middleware"
	"jizhang/internal/response"
	"jizhang/internal/service"
)

type budgetHandler struct {
	svc *service.BudgetService
}

// NewBudget 预算处理器。
func NewBudget(svc *service.BudgetService) *budgetHandler {
	return &budgetHandler{svc: svc}
}

func mapBudgetErr(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrTxnForbidden):
		response.Forbidden(c, "无权操作该预算")
	case errors.Is(err, service.ErrBudgetInvalidPeriod),
		errors.Is(err, service.ErrBudgetCategoryInvalid),
		errors.Is(err, service.ErrBudgetAmountInvalid):
		response.Invalid(c, err.Error())
	default:
		response.Internal(c, "操作失败")
	}
}

// Get GET /api/v1/budgets?period_key= 预算配置与进度（全部角色可读；缺省当前账期）。
func (h *budgetHandler) Get(c *gin.Context) {
	key := c.Query("period_key")
	if key == "" {
		key = h.svc.CurrentKey()
	}
	view, err := h.svc.ForUser(middleware.CurrentUser(c)).Get(key)
	if err != nil {
		mapBudgetErr(c, err)
		return
	}
	response.OK(c, view)
}

type budgetSetReq struct {
	Total *string                 `json:"total"`
	Items []service.BudgetSetItem `json:"items"`
}

// Set PUT /api/v1/budgets/:period_key 保存本人预算配置（全部登录角色）。
// 整单提交：total + items 即本人该账期预算全集（缺项 = 清除）。
func (h *budgetHandler) Set(c *gin.Context) {
	key := c.Param("period_key")
	var req budgetSetReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Invalid(c, "预算配置格式无效")
		return
	}
	in := service.BudgetSetInput{Total: req.Total, Items: req.Items}
	if err := h.svc.ForUser(middleware.CurrentUser(c)).Set(key, in); err != nil {
		mapBudgetErr(c, err)
		return
	}
	response.OK(c, gin.H{"period_key": key})
}

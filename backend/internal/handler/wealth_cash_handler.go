package handler

import (
	"github.com/gin-gonic/gin"
	"jizhang/internal/middleware"
	"jizhang/internal/response"
	"jizhang/internal/service"
)

func (h *wealthHandler) CashAccounts(c *gin.Context) {
	rows, err := h.svc.CashAccounts(middleware.CurrentUser(c))
	if err != nil {
		wealthError(c, err)
		return
	}
	response.OK(c, rows)
}
func (h *wealthHandler) CashAccount(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	row, err := h.svc.CashAccount(middleware.CurrentUser(c), id)
	if err != nil {
		wealthError(c, err)
		return
	}
	response.OK(c, row)
}
func (h *wealthHandler) ConfigureCash(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var in service.WealthCashInput
	if c.ShouldBindJSON(&in) != nil {
		response.Invalid(c, "理财零钱配置无效")
		return
	}
	receipt, err := h.svc.ConfigureCash(c.Request.Context(), middleware.CurrentUser(c), id, in)
	if err != nil {
		wealthError(c, err)
		return
	}
	response.OK(c, receipt)
}
func (h *wealthHandler) RefreshCash(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	row, err := h.svc.RefreshCash(c.Request.Context(), middleware.CurrentUser(c), id)
	if err != nil {
		wealthError(c, err)
		return
	}
	response.OK(c, row)
}
func (h *wealthHandler) CashYield(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	row, err := h.svc.CashYield(middleware.CurrentUser(c), id)
	if err != nil {
		wealthError(c, err)
		return
	}
	response.OK(c, row)
}
func (h *wealthHandler) EditCashYield(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var in service.WealthCashYieldInput
	if c.ShouldBindJSON(&in) != nil {
		response.Invalid(c, "收益格式无效")
		return
	}
	receipt, err := h.svc.EditCashYield(c.Request.Context(), middleware.CurrentUser(c), id, in)
	if err != nil {
		wealthError(c, err)
		return
	}
	response.OK(c, receipt)
}

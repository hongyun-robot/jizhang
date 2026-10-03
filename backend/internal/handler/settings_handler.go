package handler

import (
	"github.com/gin-gonic/gin"

	"jizhang/internal/response"
	"jizhang/internal/service"
)

type settingsHandler struct {
	periods *service.PeriodService
}

// NewSetting 账期设置处理器。
func NewSetting(periods *service.PeriodService) *settingsHandler {
	return &settingsHandler{periods: periods}
}

type settingsReq struct {
	SettlementDay int `json:"settlement_day" binding:"required"`
}

// Get GET /api/v1/settings 账期设置（全部角色可读）。
func (h *settingsHandler) Get(c *gin.Context) {
	response.OK(c, gin.H{"settlement_day": h.periods.SettlementDay()})
}

// Set PUT /api/v1/settings 修改结算日（仅管理员）。
// 仅影响此后写入流水的归期计算，不回溯历史 period_key（specs/billing-periods，任务 7.1/7.3）。
func (h *settingsHandler) Set(c *gin.Context) {
	var req settingsReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Invalid(c, "结算日必填")
		return
	}
	if err := h.periods.SetSettlementDay(req.SettlementDay); err != nil {
		response.Invalid(c, err.Error())
		return
	}
	response.OK(c, gin.H{"settlement_day": h.periods.SettlementDay()})
}

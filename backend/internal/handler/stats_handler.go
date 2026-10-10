package handler

import (
	"encoding/json"
	"errors"

	"github.com/gin-gonic/gin"

	"jizhang/internal/middleware"
	"jizhang/internal/response"
	"jizhang/internal/service"
)

type statsHandler struct {
	svc *service.StatsService
}

// NewStats 统计处理器。
func NewStats(svc *service.StatsService) *statsHandler {
	return &statsHandler{svc: svc}
}

func mapStatsErr(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrInvalidStatsRange),
		errors.Is(err, service.ErrInvalidGranularity),
		errors.Is(err, service.ErrInvalidStatsYear):
		response.Invalid(c, err.Error())
	default:
		response.Internal(c, "统计查询失败")
	}
}

// emit 输出缓存 JSON 并附带命中头（X-Stats-Cache: hit|miss，便于验证 30 秒缓存）。
func emit(c *gin.Context, raw json.RawMessage, fromCache bool, err error) {
	if err != nil {
		mapStatsErr(c, err)
		return
	}
	if fromCache {
		c.Header("X-Stats-Cache", "hit")
	} else {
		c.Header("X-Stats-Cache", "miss")
	}
	response.OKRaw(c, raw)
}

// CategoryShare GET /api/v1/stats/category?start_date&end_date
func (h *statsHandler) CategoryShare(c *gin.Context) {
	q := c.Request.URL.Query()
	raw, hit, err := h.svc.ForUser(middleware.CurrentUser(c)).CategoryShare(q.Get("start_date"), q.Get("end_date"))
	emit(c, raw, hit, err)
}

// Trend GET /api/v1/stats/trend?start_date&end_date&granularity=day|week|month
func (h *statsHandler) Trend(c *gin.Context) {
	q := c.Request.URL.Query()
	raw, hit, err := h.svc.ForUser(middleware.CurrentUser(c)).Trend(q.Get("start_date"), q.Get("end_date"), q.Get("granularity"))
	emit(c, raw, hit, err)
}

// Compare GET /api/v1/stats/compare?start_date&end_date
func (h *statsHandler) Compare(c *gin.Context) {
	q := c.Request.URL.Query()
	raw, hit, err := h.svc.ForUser(middleware.CurrentUser(c)).Compare(q.Get("start_date"), q.Get("end_date"))
	emit(c, raw, hit, err)
}

// PeriodSummary GET /api/v1/stats/periods?year=
func (h *statsHandler) PeriodSummary(c *gin.Context) {
	raw, hit, err := h.svc.ForUser(middleware.CurrentUser(c)).PeriodSummary(c.Query("year"))
	emit(c, raw, hit, err)
}

// Assets GET /api/v1/stats/assets?start_date&end_date
func (h *statsHandler) Assets(c *gin.Context) {
	q := c.Request.URL.Query()
	raw, hit, err := h.svc.ForUser(middleware.CurrentUser(c)).Assets(q.Get("start_date"), q.Get("end_date"))
	emit(c, raw, hit, err)
}

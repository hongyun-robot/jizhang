package handler

import (
	"errors"
	"github.com/gin-gonic/gin"
	"jizhang/internal/middleware"
	"jizhang/internal/response"
	"jizhang/internal/service"
	"strconv"
)

type goldHandler struct{ svc *service.GoldService }

func NewGold(s *service.GoldService) *goldHandler { return &goldHandler{s} }
func goldReply(c *gin.Context, v any, e error) {
	if e == nil {
		response.OK(c, v)
		return
	}
	switch {
	case errors.Is(e, service.ErrTxnForbidden):
		response.Forbidden(c, "只能按权限操作本人的黄金资源")
	case errors.Is(e, service.ErrGoldMissing):
		response.NotFound(c, e.Error())
	case errors.Is(e, service.ErrGoldInvalid), errors.Is(e, service.ErrTxnAccountInvalid):
		response.Invalid(c, e.Error())
	case errors.Is(e, service.ErrGoldConflict), errors.Is(e, service.ErrGoldHistory), errors.Is(e, service.ErrTxnClosedPeriod), errors.Is(e, service.ErrGoldManaged):
		response.Conflict(c, e.Error())
	default:
		response.Internal(c, "黄金操作暂时失败，请刷新核对后重试")
	}
}
func (h *goldHandler) Quote(c *gin.Context) { v, e := h.svc.QuoteView(); goldReply(c, v, e) }
func (h *goldHandler) Refresh(c *gin.Context) {
	v, e := h.svc.Refresh(c.Request.Context(), middleware.CurrentUser(c))
	goldReply(c, v, e)
}
func (h *goldHandler) Accounts(c *gin.Context) {
	v, e := h.svc.Accounts(middleware.CurrentUser(c))
	goldReply(c, v, e)
}
func (h *goldHandler) Account(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	v, e := h.svc.Account(middleware.CurrentUser(c), id)
	goldReply(c, v, e)
}
func (h *goldHandler) Write(opening, amend bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := pathID(c)
		if !ok {
			return
		}
		var in service.GoldInput
		if c.ShouldBindJSON(&in) != nil {
			response.Invalid(c, "黄金资料格式无效")
			return
		}
		if opening {
			in.Kind = "opening"
		}
		v, e := h.svc.Write(middleware.CurrentUser(c), id, in, amend)
		goldReply(c, v, e)
	}
}
func (h *goldHandler) Undo(c *gin.Context) {
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
	v, e := h.svc.Undo(middleware.CurrentUser(c), id, event, in.RequestKey)
	goldReply(c, v, e)
}
func (h *goldHandler) Events(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	v, e := h.svc.Account(middleware.CurrentUser(c), id)
	goldReply(c, v.Events, e)
}
func (h *goldHandler) Audits(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	v, e := h.svc.Audits(middleware.CurrentUser(c), id)
	goldReply(c, v, e)
}
func (h *goldHandler) Valuations(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	v, e := h.svc.Valuations(middleware.CurrentUser(c), id, c.Query("start"), c.Query("end"))
	goldReply(c, v, e)
}

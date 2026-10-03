package handler

import (
	"errors"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"jizhang/internal/model"
	"jizhang/internal/response"
	"jizhang/internal/service"
)

type accountHandler struct {
	svc *service.AccountService
}

// NewAccount 账户处理器。
func NewAccount(svc *service.AccountService) *accountHandler {
	return &accountHandler{svc: svc}
}

type accountReq struct {
	Name           string            `json:"name" binding:"required,max=64"`
	Type           model.AccountType `json:"type" binding:"required"`
	OpeningBalance *string           `json:"opening_balance"`
	RepaymentDay   *int              `json:"repayment_day"`
	Sort           *int              `json:"sort"`
	Status         model.UserStatus  `json:"status"`
}

func (r *accountReq) parse() (service.CreateAccountInput, bool) {
	in := service.CreateAccountInput{
		Name:         r.Name,
		Type:         r.Type,
		RepaymentDay: r.RepaymentDay,
	}
	if r.OpeningBalance != nil && *r.OpeningBalance != "" {
		amt, err := decimal.NewFromString(*r.OpeningBalance)
		if err != nil {
			return in, false
		}
		in.OpeningBalance = amt
	}
	if r.Sort != nil {
		in.Sort = *r.Sort
	}
	return in, true
}

func mapAccountErr(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrAccountNotFound):
		response.NotFound(c, err.Error())
	case errors.Is(err, service.ErrAccountHasTransactions):
		response.Conflict(c, err.Error())
	case errors.Is(err, service.ErrInvalidAccountType),
		errors.Is(err, service.ErrInvalidRepaymentDay),
		errors.Is(err, service.ErrInvalidAmount):
		response.Invalid(c, err.Error())
	default:
		response.Internal(c, "操作失败")
	}
}

// List GET /api/v1/accounts（全部角色可看余额）
func (h *accountHandler) List(c *gin.Context) {
	accounts, err := h.svc.List()
	if err != nil {
		response.Internal(c, "查询失败")
		return
	}
	response.OK(c, accounts)
}

// Create POST /api/v1/accounts（管理员）
func (h *accountHandler) Create(c *gin.Context) {
	var req accountReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Invalid(c, "名称与类型必填")
		return
	}
	in, ok := req.parse()
	if !ok {
		response.Invalid(c, response.MsgInvalidAmount)
		return
	}
	a, err := h.svc.Create(in)
	if err != nil {
		mapAccountErr(c, err)
		return
	}
	response.OK(c, a)
}

// Update PUT /api/v1/accounts/:id（管理员）
func (h *accountHandler) Update(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var req accountReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Invalid(c, "名称与类型必填")
		return
	}
	in, ok := req.parse()
	if !ok {
		response.Invalid(c, response.MsgInvalidAmount)
		return
	}
	status := req.Status
	if status == "" {
		status = model.StatusActive
	}
	a, err := h.svc.Update(id, in, status)
	if err != nil {
		mapAccountErr(c, err)
		return
	}
	response.OK(c, a)
}

// Delete DELETE /api/v1/accounts/:id（管理员，有关联流水则 409）
func (h *accountHandler) Delete(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	if err := h.svc.Delete(id); err != nil {
		mapAccountErr(c, err)
		return
	}
	response.OK(c, nil)
}

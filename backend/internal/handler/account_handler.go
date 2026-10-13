package handler

import (
	"errors"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"jizhang/internal/liability"
	"jizhang/internal/middleware"
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
	Name                 string            `json:"name" binding:"required,max=64"`
	RequestKey           string            `json:"request_key"`
	Type                 model.AccountType `json:"type" binding:"required"`
	AssetClass           *string           `json:"asset_class"`
	OpeningBalance       *string           `json:"opening_balance"`
	RepaymentDay         *int              `json:"repayment_day"`
	LiabilityType        string            `json:"liability_type"`
	StatementDay         *int              `json:"statement_day"`
	RepaymentMonthOffset *int              `json:"repayment_month_offset"`
	PrincipalRemainder   string            `json:"principal_remainder"`
	Sort                 *int              `json:"sort"`
	Status               model.UserStatus  `json:"status"`
}

func (r *accountReq) parse() (service.CreateAccountInput, bool) {
	in := service.CreateAccountInput{
		Name:               r.Name,
		RequestKey:         r.RequestKey,
		Type:               r.Type,
		PrincipalRemainder: r.PrincipalRemainder,
		RepaymentDay:       r.RepaymentDay,
		LiabilityType:      r.LiabilityType, StatementDay: r.StatementDay, RepaymentMonthOffset: r.RepaymentMonthOffset,
	}
	if r.AssetClass != nil {
		in.AssetClass = *r.AssetClass
		in.AssetClassProvided = true
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
	case errors.Is(err, service.ErrTxnForbidden):
		response.Forbidden(c, "只能管理自己的账户")
	case errors.Is(err, service.ErrAccountCreateConflict):
		response.Conflict(c, err.Error())
	case errors.Is(err, service.ErrAccountNameInvalid):
		response.Invalid(c, err.Error())
	case errors.Is(err, liability.ErrSetup), errors.Is(err, liability.ErrConflict), errors.Is(err, liability.ErrManaged):
		response.Conflict(c, err.Error())
	case errors.Is(err, liability.ErrInvalid):
		response.Invalid(c, "账单日和还款日须为 1–31，同月还款必须晚于账单日（包含短月份）；请选择有效规则")
	case errors.Is(err, service.ErrAccountNotFound):
		response.NotFound(c, err.Error())
	case errors.Is(err, service.ErrAccountHasTransactions):
		response.Conflict(c, err.Error())
	case errors.Is(err, service.ErrInvalidAccountType),
		errors.Is(err, service.ErrPrincipalRemainderInvalid),
		errors.Is(err, service.ErrInvalidRepaymentDay),
		errors.Is(err, service.ErrInvalidAmount):
		response.Invalid(c, err.Error())
	default:
		response.Internal(c, "操作失败")
	}
}

// List GET /api/v1/accounts（全部角色可看余额）
func (h *accountHandler) List(c *gin.Context) {
	accounts, err := h.svc.ListFor(middleware.CurrentUser(c))
	if err != nil {
		response.Internal(c, "查询失败")
		return
	}
	response.OK(c, accounts)
}
func (h *accountHandler) LiabilityTypes(c *gin.Context) {
	rows, err := h.svc.LiabilityTypes()
	if err != nil {
		response.Internal(c, "账户类型查询失败")
		return
	}
	response.OK(c, rows)
}

// Create POST /api/v1/accounts（管理员）
func (h *accountHandler) Create(c *gin.Context) {
	var req accountReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Invalid(c, "名称与类型必填")
		return
	}
	in, ok := req.parse()
	if actor := middleware.CurrentUser(c); actor != nil {
		in.ActorID = actor.ID
	}
	if !ok {
		response.Invalid(c, response.MsgInvalidAmount)
		return
	}
	a, err := h.svc.CreateFor(middleware.CurrentUser(c), in)
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
	if actor := middleware.CurrentUser(c); actor != nil {
		in.ActorID = actor.ID
	}
	if !ok {
		response.Invalid(c, response.MsgInvalidAmount)
		return
	}
	status := req.Status
	if status == "" {
		status = model.StatusActive
	}
	a, err := h.svc.UpdateFor(middleware.CurrentUser(c), id, in, status)
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
	if err := h.svc.DeleteFor(middleware.CurrentUser(c), id); err != nil {
		mapAccountErr(c, err)
		return
	}
	response.OK(c, nil)
}

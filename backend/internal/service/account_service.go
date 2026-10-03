package service

import (
	"errors"

	"github.com/shopspring/decimal"

	"jizhang/internal/model"
	"jizhang/internal/repository"
)

// 账户业务错误。
var (
	ErrAccountNotFound        = errors.New("账户不存在")
	ErrAccountHasTransactions = errors.New("该账户仍有关联流水，只能停用不能删除")
	ErrInvalidAccountType     = errors.New("无效的账户类型")
	ErrInvalidRepaymentDay    = errors.New("还款日须为 1-28")
	ErrInvalidAmount          = errors.New("金额格式无效")
)

var validAccountTypes = map[model.AccountType]bool{
	model.AccountAsset:     true,
	model.AccountLiability: true,
}

// AccountService 账户管理与余额。
type AccountService struct {
	repo *repository.AccountRepo
	// OnWrite 写成功后的钩子（用于统计缓存主动失效）。
	OnWrite func()
}

func (s *AccountService) fireWrite() {
	if s.OnWrite != nil {
		s.OnWrite()
	}
}

func NewAccountService(repo *repository.AccountRepo) *AccountService {
	return &AccountService{repo: repo}
}

// List 全部账户（含实时余额）。
func (s *AccountService) List() ([]model.Account, error) { return s.repo.List() }

// TotalAssets 总资产 = Σ资产余额 + Σ负债余额（负值贡献）。
func (s *AccountService) TotalAssets() (decimal.Decimal, error) {
	accounts, err := s.repo.List()
	if err != nil {
		return decimal.Zero, err
	}
	total := decimal.Zero
	for _, a := range accounts {
		total = total.Add(a.Balance)
	}
	return total, nil
}

// CreateInput 建户入参。
type CreateAccountInput struct {
	Name           string
	Type           model.AccountType
	OpeningBalance decimal.Decimal
	RepaymentDay   *int
	Sort           int
}

// Create 新建账户（管理员）。
func (s *AccountService) Create(in CreateAccountInput) (*model.Account, error) {
	if !validAccountTypes[in.Type] {
		return nil, ErrInvalidAccountType
	}
	if in.RepaymentDay != nil && (*in.RepaymentDay < 1 || *in.RepaymentDay > 28) {
		return nil, ErrInvalidRepaymentDay
	}
	a := &model.Account{
		Name:           in.Name,
		Type:           in.Type,
		OpeningBalance: in.OpeningBalance,
		RepaymentDay:   in.RepaymentDay,
		Status:         model.StatusActive,
		Sort:           in.Sort,
	}
	if err := s.repo.Create(a); err != nil {
		return nil, err
	}
	s.fireWrite()
	return a, nil
}

// Update 修改账户（不影响余额，余额由流水推导）。
func (s *AccountService) Update(id int64, in CreateAccountInput, status model.UserStatus) (*model.Account, error) {
	a, err := s.repo.GetByID(id)
	if err != nil {
		return nil, err
	}
	if a == nil {
		return nil, ErrAccountNotFound
	}
	if !validAccountTypes[in.Type] {
		return nil, ErrInvalidAccountType
	}
	if in.RepaymentDay != nil && (*in.RepaymentDay < 1 || *in.RepaymentDay > 28) {
		return nil, ErrInvalidRepaymentDay
	}
	a.Name = in.Name
	a.Type = in.Type
	a.OpeningBalance = in.OpeningBalance
	a.RepaymentDay = in.RepaymentDay
	a.Sort = in.Sort
	if status == model.StatusActive || status == model.StatusDisabled {
		a.Status = status
	}
	if err := s.repo.Update(a); err != nil {
		return nil, err
	}
	s.fireWrite()
	return a, nil
}

// Delete 删除账户：仍有关联流水时拒绝（仅可停用）。
func (s *AccountService) Delete(id int64) error {
	a, err := s.repo.GetByID(id)
	if err != nil {
		return err
	}
	if a == nil {
		return ErrAccountNotFound
	}
	has, err := s.repo.HasTransactions(id)
	if err != nil {
		return err
	}
	if has {
		return ErrAccountHasTransactions
	}
	if err := s.repo.Delete(id); err != nil {
		return err
	}
	s.fireWrite()
	return nil
}

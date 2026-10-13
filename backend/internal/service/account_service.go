package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/shopspring/decimal"

	"gorm.io/gorm"
	"jizhang/internal/liability"
	"jizhang/internal/model"
	"jizhang/internal/repository"
	"time"
)

// 账户业务错误。
var (
	ErrAccountNotFound           = errors.New("账户不存在")
	ErrAccountHasTransactions    = errors.New("该账户仍有关联流水或财务配置，只能停用不能删除")
	ErrInvalidAccountType        = errors.New("无效的账户类型")
	ErrInvalidRepaymentDay       = errors.New("还款日须为 1-31")
	ErrInvalidAmount             = errors.New("金额格式无效")
	ErrAccountCreateConflict     = errors.New("账户创建请求已使用，或创建的账户已删除，请核对结果")
	ErrAccountNameInvalid        = errors.New("账户名称须为 1–64 字")
	ErrPrincipalRemainderInvalid = errors.New("分期本金尾差须选择首期或最后一期")
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
func (s *AccountService) LiabilityTypes() ([]model.LiabilityAccountType, error) {
	return liability.AccountTypes(s.repo.DB())
}

func validateLiabilityType(db *gorm.DB, in CreateAccountInput) error {
	if in.Type != model.AccountLiability || in.LiabilityType == "" {
		return nil
	}
	return liability.ValidateAccountType(db, in.LiabilityType)
}

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
	Name                 string
	Type                 model.AccountType
	OpeningBalance       decimal.Decimal
	RepaymentDay         *int
	Sort                 int
	LiabilityType        string
	StatementDay         *int
	RepaymentMonthOffset *int
	PrincipalRemainder   string
	ActorID              int64
	RequestKey           string
}

// Create 新建账户（管理员）。
func (s *AccountService) Create(in CreateAccountInput) (out *model.Account, err error) {
	in.Name = strings.TrimSpace(in.Name)
	err = s.accountWrite(func(tx *gorm.DB) error {
		if in.RequestKey == "" {
			var e error
			out, e = s.createAt(tx, in)
			return e
		}
		if in.ActorID <= 0 || len(in.RequestKey) > 128 || strings.TrimSpace(in.RequestKey) != in.RequestKey {
			return ErrAccountCreateConflict
		}
		payload := in
		payload.RequestKey = ""
		raw, e := json.Marshal(payload)
		if e != nil {
			return e
		}
		hash := sha256.Sum256(raw)
		digest := hex.EncodeToString(hash[:])
		var request model.AccountCreateRequest
		e = tx.Where("owner_id = ? AND request_key = ?", in.ActorID, in.RequestKey).First(&request).Error
		if e == nil {
			if request.Digest != digest || request.AccountID == nil {
				return ErrAccountCreateConflict
			}
			out, e = repository.NewAccountRepo(tx).GetByID(*request.AccountID)
			if e != nil {
				return e
			}
			if out == nil {
				return ErrAccountCreateConflict
			}
			return nil
		}
		if !errors.Is(e, gorm.ErrRecordNotFound) {
			return e
		}
		out, e = s.createAt(tx, in)
		if e != nil {
			return e
		}
		return tx.Create(&model.AccountCreateRequest{OwnerID: in.ActorID, RequestKey: in.RequestKey, Digest: digest, AccountID: &out.ID}).Error
	})
	if err == nil {
		s.fireWrite()
	}
	return
}

func (s *AccountService) accountWrite(fn func(*gorm.DB) error) error {
	db := s.repo.DB()
	if db.Migrator().HasTable(&model.Setting{}) {
		return repository.LedgerWrite(db, fn)
	}
	return db.Transaction(fn)
}

func validateAccountInput(in CreateAccountInput) error {
	if in.PrincipalRemainder != "" && (in.Type != model.AccountLiability || (in.PrincipalRemainder != "first" && in.PrincipalRemainder != "last")) {
		return ErrPrincipalRemainderInvalid
	}
	if n := utf8.RuneCountInString(strings.TrimSpace(in.Name)); n < 1 || n > 64 {
		return ErrAccountNameInvalid
	}
	if !liability.ValidAmount(in.OpeningBalance.Abs(), true) {
		return ErrInvalidAmount
	}
	if in.Type == model.AccountAsset && (in.RepaymentDay != nil || in.StatementDay != nil || in.LiabilityType != "") {
		return liability.ErrInvalid
	}
	if in.Type == model.AccountLiability && !in.OpeningBalance.IsZero() {
		return liability.ErrSetup
	}
	if in.StatementDay != nil {
		if in.RepaymentDay == nil || in.RepaymentMonthOffset == nil {
			return liability.ErrInvalid
		}
		if e := liability.ValidateRule(*in.StatementDay, *in.RepaymentDay, *in.RepaymentMonthOffset); e != nil {
			return e
		}
	}
	return nil
}

func (s *AccountService) createAt(tx *gorm.DB, in CreateAccountInput) (*model.Account, error) {
	if e := validateAccountInput(in); e != nil {
		return nil, e
	}
	if e := validateLiabilityType(tx, in); e != nil {
		return nil, e
	}
	if !validAccountTypes[in.Type] {
		return nil, ErrInvalidAccountType
	}
	if in.RepaymentDay != nil && (*in.RepaymentDay < 1 || *in.RepaymentDay > 31) {
		return nil, ErrInvalidRepaymentDay
	}
	a := &model.Account{
		Name:               in.Name,
		Type:               in.Type,
		OpeningBalance:     in.OpeningBalance,
		RepaymentDay:       in.RepaymentDay,
		Status:             model.StatusActive,
		Sort:               in.Sort,
		LiabilityType:      in.LiabilityType,
		PrincipalRemainder: in.PrincipalRemainder,
	}
	if in.ActorID > 0 {
		owner := in.ActorID
		a.OwnerID = &owner
	}
	if err := repository.NewAccountRepo(tx).Create(a); err != nil {
		return nil, err
	}
	if in.StatementDay != nil {
		kind := in.LiabilityType
		if kind == "" {
			var e error
			kind, e = liability.DefaultAccountType(tx)
			if e != nil {
				return nil, e
			}
		}
		if _, e := liability.Configure(tx, a.ID, kind, *in.StatementDay, *in.RepaymentDay, *in.RepaymentMonthOffset, in.ActorID, time.Now()); e != nil {
			return nil, e
		}
		if e := tx.First(a, a.ID).Error; e != nil {
			return nil, e
		}
	}
	return a, nil
}

// Update 修改账户（不影响余额，余额由流水推导）。
func (s *AccountService) Update(id int64, in CreateAccountInput, status model.UserStatus) (out *model.Account, err error) {
	in.Name = strings.TrimSpace(in.Name)
	err = s.accountWrite(func(tx *gorm.DB) error { var e error; out, e = s.updateAt(tx, id, in, status); return e })
	if err == nil {
		s.fireWrite()
	}
	return
}
func (s *AccountService) updateAt(tx *gorm.DB, id int64, in CreateAccountInput, status model.UserStatus) (*model.Account, error) {
	if e := validateAccountInput(in); e != nil {
		return nil, e
	}
	if e := validateLiabilityType(tx, in); e != nil {
		return nil, e
	}
	r := repository.NewAccountRepo(tx)
	a, err := r.GetByID(id)
	if err != nil {
		return nil, err
	}
	if a == nil {
		return nil, ErrAccountNotFound
	}
	if !validAccountTypes[in.Type] {
		return nil, ErrInvalidAccountType
	}
	if in.RepaymentDay != nil && (*in.RepaymentDay < 1 || *in.RepaymentDay > 31) {
		return nil, ErrInvalidRepaymentDay
	}
	has, e := r.HasTransactions(id)
	if e != nil {
		return nil, e
	}
	if tx.Migrator().HasTable(&model.LiabilityStatement{}) {
		var n int64
		if e := tx.Model(&model.LiabilityStatement{}).Where("account_id = ?", id).Count(&n).Error; e != nil {
			return nil, e
		}
		has = has || n > 0
	}
	if has && (in.Type != a.Type || !in.OpeningBalance.Equal(a.OpeningBalance)) {
		return nil, ErrAccountHasTransactions
	}
	if a.BillingEnabled && in.StatementDay == nil && in.RepaymentDay != nil && (a.RepaymentDay == nil || *in.RepaymentDay != *a.RepaymentDay) {
		return nil, liability.ErrManaged
	}
	a.Name = in.Name
	a.Type = in.Type
	if a.OwnerID == nil && in.ActorID > 0 {
		owner := in.ActorID
		a.OwnerID = &owner
	}
	if in.Type == model.AccountAsset {
		a.PrincipalRemainder = "last"
		a.BillingEnabled = false
		a.LiabilityType = ""
		a.RepaymentDay = nil
		if tx.Migrator().HasTable(&model.LiabilityBillRule{}) {
			if e := tx.Where("account_id = ? AND kind = ?", id, "settings").Delete(&model.LiabilityOperation{}).Error; e != nil {
				return nil, e
			}
			if e := tx.Where("account_id = ?", id).Delete(&model.LiabilityBillRule{}).Error; e != nil {
				return nil, e
			}
		}
	}
	a.OpeningBalance = in.OpeningBalance
	if in.Type == model.AccountLiability && in.PrincipalRemainder != "" {
		a.PrincipalRemainder = in.PrincipalRemainder
	}
	if !a.BillingEnabled {
		a.RepaymentDay = in.RepaymentDay
	}
	if !a.BillingEnabled {
		a.LiabilityType = in.LiabilityType
	}
	a.Sort = in.Sort
	if status == model.StatusActive || status == model.StatusDisabled {
		a.Status = status
	}
	if err := r.Update(a); err != nil {
		return nil, err
	}
	if in.StatementDay != nil {
		kind := in.LiabilityType
		if kind == "" {
			kind = a.LiabilityType
		}
		if kind == "" {
			var e error
			kind, e = liability.DefaultAccountType(tx)
			if e != nil {
				return nil, e
			}
		}
		if _, e := liability.Configure(tx, id, kind, *in.StatementDay, *in.RepaymentDay, *in.RepaymentMonthOffset, in.ActorID, time.Now()); e != nil {
			return nil, e
		}
		if e := tx.First(a, id).Error; e != nil {
			return nil, e
		}
		if e := liability.Reallocate(tx, id); e != nil {
			return nil, e
		}
		if e := liability.RefreshReminders(tx, time.Now(), false); e != nil {
			return nil, e
		}
	}
	return a, nil
}

// Delete 删除账户：仍有关联流水时拒绝（仅可停用）。
func (s *AccountService) Delete(id int64) error {
	err := s.accountWrite(func(tx *gorm.DB) error { return s.deleteAt(tx, id) })
	if err == nil {
		s.fireWrite()
	}
	return err
}
func (s *AccountService) deleteAt(tx *gorm.DB, id int64) error {
	r := repository.NewAccountRepo(tx)
	a, err := r.GetByID(id)
	if err != nil {
		return err
	}
	if a == nil {
		return ErrAccountNotFound
	}
	has, err := r.HasTransactions(id)
	if err != nil {
		return err
	}
	if has {
		return ErrAccountHasTransactions
	}
	for _, ref := range []struct{ table, condition string }{{"vouchers", "account_id = ?"}, {"recurring_rules", "account_id = ?"}, {"installment_plans", "fee_account_id = ?"}} {
		if tx.Migrator().HasTable(ref.table) {
			var n int64
			if e := tx.Table(ref.table).Where(ref.condition, id).Count(&n).Error; e != nil {
				return e
			}
			if n > 0 {
				return ErrAccountHasTransactions
			}
		}
	}
	if tx.Migrator().HasTable(&model.LiabilityOperation{}) {
		var n int64
		if e := tx.Model(&model.LiabilityOperation{}).Where("account_id = ? AND kind <> ?", id, "settings").Count(&n).Error; e != nil {
			return e
		}
		if n > 0 {
			return ErrAccountHasTransactions
		}
		if e := tx.Model(&model.LiabilityStatement{}).Where("account_id = ?", id).Count(&n).Error; e != nil {
			return e
		}
		if n > 0 {
			return ErrAccountHasTransactions
		}
		if e := tx.Where("account_id = ?", id).Delete(&model.LiabilityOperation{}).Error; e != nil {
			return e
		}
		if e := tx.Where("account_id = ?", id).Delete(&model.LiabilityBillRule{}).Error; e != nil {
			return e
		}
	}
	if tx.Migrator().HasTable(&model.AccountCreateRequest{}) {
		if e := tx.Model(&model.AccountCreateRequest{}).Where("account_id = ?", id).Update("account_id", nil).Error; e != nil {
			return e
		}
	}
	if err := r.Delete(id); err != nil {
		return err
	}
	return nil
}

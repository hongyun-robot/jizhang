package service

import (
	"errors"
	"time"

	"github.com/shopspring/decimal"

	"jizhang/internal/model"
	"jizhang/internal/repository"
)

// 流水业务错误。
var (
	ErrTxnNotFound        = errors.New("流水不存在")
	ErrTxnClosedPeriod    = errors.New("该账期已封账，不可增改删")
	ErrTxnAmountInvalid   = errors.New("金额必须大于 0 且不超过 9999999999.99")
	ErrTxnAccountInvalid  = errors.New("账户无效或已停用")
	ErrTxnCategoryInvalid = errors.New("分类无效：须存在且收支类型匹配")
	ErrTxnTransferSame    = errors.New("转账的转出与转入账户不能相同")
	ErrTxnKindInvalid     = errors.New("无效的流水类型")
	ErrTxnDateInvalid     = errors.New("发生日期无效")
	ErrTxnForbidden       = errors.New("无权操作这条流水")
	ErrTxnRelatedInvalid  = errors.New("关联流水不存在或不是支出流水")
	ErrTxnRefundsExist    = errors.New("该支出存在关联退款，请先处理退款")
)

var maxAmount = decimal.RequireFromString("9999999999.99")

// TxnStore 流水仓储依赖。
type TxnStore interface {
	Create(t *model.Transaction) error
	Update(t *model.Transaction) error
	Delete(id int64) error
	GetByID(id int64) (*model.Transaction, error)
	List(f repository.TxnFilter) ([]model.Transaction, int64, error)
	ListAll(f repository.TxnFilter) ([]model.Transaction, error)
	// HasRefunds 关联退款存在性（支出删除约束，specs/refunds）。
	HasRefunds(txnID int64) (bool, error)
	// RefundedTotals 按原支出 id 聚合关联退款累计（列表「已退」标记）。
	RefundedTotals(ids []int64) (map[int64]decimal.Decimal, error)
}

// TxnDeps 流水服务的外部依赖。
type TxnDeps struct {
	Txns     TxnStore
	Accounts *repository.AccountRepo
	Cats     *repository.CategoryRepo
	Closings *repository.ClosingRepo
	Periods  *PeriodService
}

// TransactionService 流水记账。
type TransactionService struct {
	deps TxnDeps
	// OnWrite 写成功后的钩子（用于统计缓存主动失效）。
	OnWrite func()
	// OnWritePeriod 写成功后携带受影响账期（预算超支评估用，任务 8.2）。
	OnWritePeriod func(periodKeys ...string)
}

func (s *TransactionService) fireWrite() {
	if s.OnWrite != nil {
		s.OnWrite()
	}
}

func (s *TransactionService) firePeriods(keys ...string) {
	if s.OnWritePeriod != nil {
		s.OnWritePeriod(keys...)
	}
}

func NewTransactionService(deps TxnDeps) *TransactionService {
	return &TransactionService{deps: deps}
}

// TxnInput 记账/修改入参。
type TxnInput struct {
	Kind              model.TxnKind
	Amount            decimal.Decimal
	AccountID         int64
	TransferAccountID int64
	CategoryID        int64
	EntryDate         time.Time
	Note              string
	RelatedTxnID      int64 // 关联原支出流水（退款用，0=无）
}

// validate 校验并规整入参；返回错误供 handler 映射。
func (s *TransactionService) validate(in *TxnInput) error {
	switch in.Kind {
	case model.TxnExpense, model.TxnIncome, model.TxnTransfer, model.TxnRefund:
	default:
		return ErrTxnKindInvalid
	}
	if in.Amount.IsNegative() || in.Amount.IsZero() || in.Amount.GreaterThan(maxAmount) {
		return ErrTxnAmountInvalid
	}
	// 金额最多两位小数
	if in.Amount.Round(2).Equal(in.Amount) == false {
		return ErrTxnAmountInvalid
	}
	if in.EntryDate.IsZero() || in.EntryDate.Year() < 2000 || in.EntryDate.Year() > 2100 {
		return ErrTxnDateInvalid
	}

	// 退款可关联原支出：目标必须存在且为支出；未显式给出的账户/分类按原支出预填（design D3）
	if in.Kind == model.TxnRefund && in.RelatedTxnID > 0 {
		orig, err := s.deps.Txns.GetByID(in.RelatedTxnID)
		if err != nil {
			return err
		}
		if orig == nil || orig.Kind != model.TxnExpense {
			return ErrTxnRelatedInvalid
		}
		if in.AccountID == 0 {
			in.AccountID = orig.AccountID
		}
		if in.CategoryID == 0 && orig.CategoryID != nil {
			in.CategoryID = *orig.CategoryID
		}
	}

	acc, err := s.deps.Accounts.GetByID(in.AccountID)
	if err != nil {
		return err
	}
	if acc == nil || acc.Status != model.StatusActive {
		return ErrTxnAccountInvalid
	}

	if in.Kind == model.TxnTransfer {
		if in.TransferAccountID == in.AccountID {
			return ErrTxnTransferSame
		}
		tAcc, err := s.deps.Accounts.GetByID(in.TransferAccountID)
		if err != nil {
			return err
		}
		if tAcc == nil || tAcc.Status != model.StatusActive {
			return ErrTxnAccountInvalid
		}
		in.CategoryID = 0 // 转账无分类
		return nil
	}

	// 支出/收入必须挂末级分类且类型匹配；退款分类可留空，给了则须为支出类末级
	if in.Kind == model.TxnRefund && in.CategoryID == 0 {
		return nil
	}
	cat, err := s.deps.Cats.GetByID(in.CategoryID)
	if err != nil {
		return err
	}
	wantKind := model.KindExpense
	if in.Kind == model.TxnIncome {
		wantKind = model.KindIncome
	}
	if cat == nil || cat.Kind != wantKind {
		return ErrTxnCategoryInvalid
	}
	nChildren, err := s.deps.Cats.CountChildren(cat.ID)
	if err != nil {
		return err
	}
	if nChildren > 0 {
		return ErrTxnCategoryInvalid // 一级分类不可直接记账
	}
	return nil
}

// guardOpen 校验目标归期未封账。
func (s *TransactionService) guardOpen(periodKey string) error {
	closed, err := s.deps.Closings.IsClosed(periodKey)
	if err != nil {
		return err
	}
	if closed {
		return ErrTxnClosedPeriod
	}
	return nil
}

// Create 记一笔：权限由调用方（handler）先过 CanCreateTransaction，此处管账务规则。
func (s *TransactionService) Create(actor *model.User, in TxnInput) (*model.Transaction, error) {
	if !CanCreateTransaction(actor) {
		return nil, ErrTxnForbidden
	}
	if err := s.validate(&in); err != nil {
		return nil, err
	}
	periodKey := s.deps.Periods.KeyForDate(in.EntryDate)
	if err := s.guardOpen(periodKey); err != nil {
		return nil, err
	}

	t := &model.Transaction{
		Kind:      in.Kind,
		Amount:    in.Amount,
		AccountID: in.AccountID,
		EntryDate: in.EntryDate,
		PeriodKey: periodKey,
		Note:      in.Note,
		CreatedBy: actor.ID,
		Source:    model.SourceManual,
	}
	if in.Kind == model.TxnTransfer {
		tid := in.TransferAccountID
		t.TransferAccountID = &tid
	} else if in.CategoryID > 0 {
		cid := in.CategoryID
		t.CategoryID = &cid
	}
	if in.Kind == model.TxnRefund && in.RelatedTxnID > 0 {
		rid := in.RelatedTxnID
		t.RelatedTxnID = &rid
	}
	if err := s.deps.Txns.Create(t); err != nil {
		return nil, err
	}
	s.fireWrite()
	s.firePeriods(periodKey)
	return t, nil
}

// CreateFromRule 周期模板生成流水（系统动作，任务 9.1/9.2）：
// 复用同一套账务校验、归期物化与写钩子，标记 source=recurring + rule_id，
// created_by 取模板创建者；封账目标期返回 ErrTxnClosedPeriod 由生成器跳过。
func (s *TransactionService) CreateFromRule(rule *model.RecurringRule, date time.Time) (*model.Transaction, error) {
	cat, err := s.deps.Cats.GetByID(rule.CategoryID)
	if err != nil {
		return nil, err
	}
	if cat == nil {
		return nil, ErrTxnCategoryInvalid
	}
	kind := model.TxnExpense // 模板分类决定收支方向（收入模板 = 周期进账）
	if cat.Kind == model.KindIncome {
		kind = model.TxnIncome
	}
	in := TxnInput{
		Kind:       kind,
		Amount:     rule.Amount,
		AccountID:  rule.AccountID,
		CategoryID: rule.CategoryID,
		EntryDate:  date,
		Note:       rule.Note,
	}
	if err := s.validate(&in); err != nil {
		return nil, err
	}
	periodKey := s.deps.Periods.KeyForDate(in.EntryDate)
	if err := s.guardOpen(periodKey); err != nil {
		return nil, err
	}
	cid := rule.CategoryID
	t := &model.Transaction{
		Kind:       kind,
		Amount:     in.Amount,
		AccountID:  in.AccountID,
		CategoryID: &cid,
		EntryDate:  in.EntryDate,
		PeriodKey:  periodKey,
		Note:       rule.Note,
		CreatedBy:  rule.CreatedBy,
		Source:     model.SourceRecurring,
		RuleID:     &rule.ID,
	}
	if err := s.deps.Txns.Create(t); err != nil {
		return nil, err
	}
	s.fireWrite()
	s.firePeriods(periodKey)
	return t, nil
}

// Update 改一笔：属主校验 + 新旧归期都要未封账，归期随日期重新物化。
func (s *TransactionService) Update(actor *model.User, id int64, in TxnInput) (*model.Transaction, error) {
	t, err := s.deps.Txns.GetByID(id)
	if err != nil {
		return nil, err
	}
	if t == nil {
		return nil, ErrTxnNotFound
	}
	if !CanModifyTransaction(actor, t.CreatedBy) {
		return nil, ErrTxnForbidden
	}
	if err := s.validate(&in); err != nil {
		return nil, err
	}
	newPeriod := s.deps.Periods.KeyForDate(in.EntryDate)
	oldPeriod := t.PeriodKey
	// 旧归期与新归期都必须开放
	for _, key := range []string{t.PeriodKey, newPeriod} {
		if err := s.guardOpen(key); err != nil {
			return nil, err
		}
	}

	t.Kind = in.Kind
	t.Amount = in.Amount
	t.AccountID = in.AccountID
	t.EntryDate = in.EntryDate
	t.PeriodKey = newPeriod
	t.Note = in.Note
	if in.Kind == model.TxnTransfer {
		tid := in.TransferAccountID
		t.TransferAccountID = &tid
		t.CategoryID = nil
	} else {
		t.TransferAccountID = nil
		if in.CategoryID > 0 {
			cid := in.CategoryID
			t.CategoryID = &cid
		} else {
			t.CategoryID = nil
		}
	}
	if in.Kind == model.TxnRefund && in.RelatedTxnID > 0 {
		rid := in.RelatedTxnID
		t.RelatedTxnID = &rid
	} else {
		t.RelatedTxnID = nil
	}
	if err := s.deps.Txns.Update(t); err != nil {
		return nil, err
	}
	s.fireWrite()
	s.firePeriods(oldPeriod, newPeriod)
	return t, nil
}

// Delete 删一笔：属主校验 + 归期未封账。
func (s *TransactionService) Delete(actor *model.User, id int64) error {
	t, err := s.deps.Txns.GetByID(id)
	if err != nil {
		return err
	}
	if t == nil {
		return ErrTxnNotFound
	}
	if !CanModifyTransaction(actor, t.CreatedBy) {
		return ErrTxnForbidden
	}
	if err := s.guardOpen(t.PeriodKey); err != nil {
		return err
	}
	// 关联退款未清空前不可删原支出（specs/refunds 删除约束）
	if t.Kind == model.TxnExpense {
		has, err := s.deps.Txns.HasRefunds(id)
		if err != nil {
			return err
		}
		if has {
			return ErrTxnRefundsExist
		}
	}
	if err := s.deps.Txns.Delete(id); err != nil {
		return err
	}
	s.fireWrite()
	s.firePeriods(t.PeriodKey)
	return nil
}

// List 查询（四档角色均可读），并聚合各支出的累计已退金额。
func (s *TransactionService) List(actor *model.User, f repository.TxnFilter) ([]model.Transaction, int64, error) {
	if !CanView(actor) {
		return nil, 0, ErrTxnForbidden
	}
	list, total, err := s.deps.Txns.List(f)
	if err != nil {
		return nil, 0, err
	}
	s.fillRefunded(list)
	return list, total, nil
}

// fillRefunded 填充支出行的已退累计金额（仅支出会被退款关联）。
func (s *TransactionService) fillRefunded(list []model.Transaction) {
	ids := make([]int64, 0, len(list))
	for _, t := range list {
		if t.Kind == model.TxnExpense {
			ids = append(ids, t.ID)
		}
	}
	if len(ids) == 0 {
		return
	}
	totals, err := s.deps.Txns.RefundedTotals(ids)
	if err != nil {
		return // 聚合失败不阻塞列表，已退标记降级为 0
	}
	for i := range list {
		if v, ok := totals[list[i].ID]; ok {
			list[i].RefundedAmount = v
		}
	}
}

// GetByID 读单条（校验用）。
func (s *TransactionService) GetByID(id int64) (*model.Transaction, error) {
	return s.deps.Txns.GetByID(id)
}

// ExportRows 导出当前筛选的全部命中行（不分页，四档角色均可读）。
func (s *TransactionService) ExportRows(actor *model.User, f repository.TxnFilter) ([]model.Transaction, error) {
	if !CanView(actor) {
		return nil, ErrTxnForbidden
	}
	return s.deps.Txns.ListAll(f)
}

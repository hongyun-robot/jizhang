package service

import (
	"encoding/json"
	"fmt"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"jizhang/internal/liability"
	"jizhang/internal/model"
	"jizhang/internal/repository"
	"strings"
)

type SettlementInput struct {
	RequestKey    string          `json:"request_key"`
	Reason        string          `json:"reason"`
	Version       int             `json:"version"`
	Principal     decimal.Decimal `json:"principal"`
	FeeAmount     decimal.Decimal `json:"fee_amount"`
	FeeAccountID  int64           `json:"fee_account_id"`
	FeeCategoryID int64           `json:"fee_category_id"`
	FromAccountID int64           `json:"from_account_id"`
	EntryDate     string          `json:"entry_date"`
}
type SettlementPreview struct {
	PlanID          int64           `json:"plan_id"`
	Version         int             `json:"version"`
	Principal       decimal.Decimal `json:"principal"`
	PaidPrincipal   decimal.Decimal `json:"paid_principal"`
	OutstandingFees decimal.Decimal `json:"outstanding_fees"`
	CancelledFees   decimal.Decimal `json:"cancelled_fees"`
	PendingCredit   decimal.Decimal `json:"pending_credit"`
	FeeAmount       decimal.Decimal `json:"fee_amount"`
	TransferAmount  decimal.Decimal `json:"transfer_amount"`
}

func previewFrom(d *liability.Detail, p model.InstallmentPlan, in SettlementInput) SettlementPreview {
	out := SettlementPreview{PlanID: p.ID, Version: p.Version, FeeAmount: in.FeeAmount}
	for _, allocation := range d.Allocations {
		var principal bool
		for _, item := range d.Items {
			if item.ID == allocation.DebtItemID && item.PlanID != nil && *item.PlanID == p.ID && item.Kind == "principal" {
				principal = true
				break
			}
		}
		if !principal {
			continue
		}
		for _, credit := range d.Credits {
			if credit.ID != allocation.CreditEventID {
				continue
			}
			for _, source := range d.Transactions {
				if source.ID == credit.SourceID && source.Kind != model.TxnRefund {
					out.PaidPrincipal = out.PaidPrincipal.Add(allocation.Amount)
				}
			}
		}
	}
	for _, x := range d.Items {
		if x.PlanID != nil && *x.PlanID == p.ID {
			if x.Kind == "principal" {
				out.Principal = out.Principal.Add(x.Remaining)
			} else {
				out.OutstandingFees = out.OutstandingFees.Add(x.Remaining)
			}
		}
	}
	for _, t := range p.Terms {
		if t.FeeTxnID == nil && t.FeeStatus != "cancelled" {
			out.CancelledFees = out.CancelledFees.Add(t.FeeAmount)
		}
	}
	goal := out.Principal.Add(out.OutstandingFees)
	if in.FeeAccountID == p.AccountID {
		goal = goal.Add(in.FeeAmount)
	}
	out.PendingCredit = decimal.Min(goal, d.Summary.PendingCredit)
	out.TransferAmount = decimal.Max(goal.Sub(out.PendingCredit), decimal.Zero)
	return out
}
func (s *LiabilityService) SettlementPreview(actor *model.User, id int64, in SettlementInput) (SettlementPreview, error) {
	p, e := s.Plan(actor, id)
	if e != nil {
		return SettlementPreview{}, e
	}
	if !CanModifyTransaction(actor, p.CreatedBy) {
		return SettlementPreview{}, ErrTxnForbidden
	}
	if p.Status == "settled" || p.Status == "needs_reconcile" || !liability.ValidAmount(in.FeeAmount, true) {
		return SettlementPreview{}, liability.ErrConflict
	}
	for _, term := range p.Terms {
		if term.FeeTxnID == nil && term.FeeAmount.IsPositive() && term.FeeStatus != "cancelled" && term.FeeDate <= liability.Date(s.Now()) {
			return SettlementPreview{}, requestConflict("到期手续费尚未入账，请先补齐或处理封账问题")
		}
	}
	d, e := s.Detail(actor, p.AccountID)
	if e != nil {
		return SettlementPreview{}, e
	}
	return previewFrom(d, p, in), nil
}

func (s *LiabilityService) Settle(actor *model.User, id int64, in SettlementInput) (LiabilityResult, error) {
	var p model.InstallmentPlan
	if e := s.db.First(&p, id).Error; e != nil {
		return LiabilityResult{}, e
	}
	return s.operation(actor, p.AccountID, "settle", in.RequestKey, in.Reason, in, func(tx *gorm.DB, op *model.LiabilityOperation) (LiabilityResult, error) {
		result := LiabilityResult{PlanID: id, TransactionIDs: []int64{}, CreditSignatures: map[int64]string{}}
		d, e := detailAt(tx, p.AccountID, s.Now())
		if e != nil {
			return result, e
		}
		for _, x := range d.Plans {
			if x.ID == id {
				p = x
			}
		}
		if !CanModifyTransaction(actor, p.CreatedBy) {
			return result, ErrTxnForbidden
		}
		if p.Version != in.Version || p.Status == "settled" || p.Status == "needs_reconcile" || !liability.ValidAmount(in.FeeAmount, true) {
			return result, liability.ErrConflict
		}
		for _, term := range p.Terms {
			if term.FeeTxnID == nil && term.FeeAmount.IsPositive() && term.FeeStatus != "cancelled" && term.FeeDate <= liability.Date(s.Now()) {
				return result, requestConflict("到期手续费尚未入账，请先补齐或处理封账问题")
			}
		}
		actual := previewFrom(d, p, in)
		if !in.Principal.Equal(actual.Principal) {
			return result, requestConflict("实际剩余本金与记录不符，请先补记或退款核对")
		}
		date, e := liability.Parse(in.EntryDate)
		if e != nil {
			return result, e
		}
		inner := s.txns.bound(tx)
		period := inner.deps.Periods.KeyForDate(date)
		if e := inner.guardOpen(period); e != nil {
			return result, e
		}
		var from model.Account
		if e := checkAccountOwnership(tx, actor, in.FromAccountID); e != nil {
			return result, e
		}
		if e := tx.First(&from, in.FromAccountID).Error; e != nil {
			return result, e
		}
		if !from.IsCash() || from.Status != model.StatusActive {
			return result, ErrTxnAccountInvalid
		}
		op.PlanID = &p.ID
		if e := tx.Model(op).Update("plan_id", p.ID).Error; e != nil {
			return result, e
		}
		if in.FeeAmount.IsPositive() {
			feeAccount := in.FeeAccountID
			if feeAccount == 0 {
				feeAccount = from.ID
			}
			feeCategory := in.FeeCategoryID
			if feeCategory == 0 {
				feeCategory = p.FeeCategoryID
			}
			if e := checkAccountOwnership(tx, actor, feeAccount); e != nil {
				return result, e
			}
			if e := checkCategoryOwnership(tx, actor, feeCategory); e != nil {
				return result, e
			}
			var a model.Account
			var cat model.Category
			if e := tx.First(&a, feeAccount).Error; e != nil {
				return result, e
			}
			if a.ID != p.AccountID && (!a.IsCash() || a.Status != model.StatusActive) {
				return result, ErrTxnAccountInvalid
			}
			if e := tx.First(&cat, feeCategory).Error; e != nil {
				return result, e
			}
			var n int64
			if e := tx.Model(&model.Category{}).Where("parent_id = ?", cat.ID).Count(&n).Error; e != nil {
				return result, e
			}
			if cat.Kind != model.KindExpense || n > 0 {
				return result, ErrTxnCategoryInvalid
			}
			t := model.Transaction{Kind: model.TxnExpense, Amount: in.FeeAmount, AccountID: feeAccount, CategoryID: &feeCategory, EntryDate: date, PeriodKey: period, CreatedBy: actor.ID, Source: model.SourceManual, LiabilityOperationID: &op.ID, Note: "分期提前结清手续费"}
			if e := repository.NewTransactionRepo(tx).Create(&t); e != nil {
				return result, e
			}
			result.TransactionIDs = append(result.TransactionIDs, t.ID)
		}
		d, e = detailAt(tx, p.AccountID, s.Now())
		if e != nil {
			return result, e
		}
		goal := map[int64]decimal.Decimal{}
		total := decimal.Zero
		for _, x := range d.Items {
			if x.PlanID != nil && *x.PlanID == p.ID && x.Remaining.IsPositive() {
				goal[x.ID] = x.Remaining
				total = total.Add(x.Remaining)
			}
		}
		take := func(amount decimal.Decimal) []liability.Target {
			targets := []liability.Target{}
			for _, x := range d.Items {
				left := goal[x.ID]
				n := decimal.Min(left, amount)
				if !n.IsPositive() {
					continue
				}
				targets = append(targets, liability.Target{DebtItemID: x.ID, Amount: n})
				goal[x.ID] = left.Sub(n)
				amount = amount.Sub(n)
			}
			return targets
		}
		for _, credit := range d.Credits {
			amount := decimal.Min(total, credit.Remaining)
			if !amount.IsPositive() {
				continue
			}
			var source model.Transaction
			if credit.SourceTxnID == nil {
				return result, liability.ErrConflict
			}
			if e := tx.First(&source, *credit.SourceTxnID).Error; e != nil {
				return result, e
			}
			if !CanModifyTransaction(actor, source.CreatedBy) {
				return result, ErrTxnForbidden
			}
			targets := []liability.Target{}
			for _, old := range d.Allocations {
				if old.CreditEventID == credit.ID {
					targets = append(targets, liability.Target{DebtItemID: old.DebtItemID, Amount: old.Amount})
				}
			}
			targets = append(targets, take(amount)...)
			encoded := jsonText(targets)
			if e := tx.Model(&credit).Updates(map[string]any{"mode": "settlement", "targets": encoded}).Error; e != nil {
				return result, e
			}
			result.CreditSignatures[credit.ID] = "settlement:" + encoded
			total = total.Sub(amount)
		}
		if total.IsPositive() {
			targets := take(total)
			to := p.AccountID
			t := model.Transaction{Kind: model.TxnTransfer, Amount: total, AccountID: from.ID, TransferAccountID: &to, EntryDate: date, PeriodKey: period, CreatedBy: actor.ID, Source: model.SourceManual, LiabilityOperationID: &op.ID, Note: "分期提前结清本金及已发生费用"}
			if e := repository.NewTransactionRepo(tx).Create(&t); e != nil {
				return result, e
			}
			if e := tx.Model(&model.LiabilityCreditEvent{}).Where("account_id = ? AND source_id = ?", to, t.ID).Updates(map[string]any{"mode": "settlement", "targets": jsonText(targets)}).Error; e != nil {
				return result, e
			}
			result.TransactionIDs = append(result.TransactionIDs, t.ID)
		}
		if e := liability.Reallocate(tx, p.AccountID); e != nil {
			return result, e
		}
		check, e := detailAt(tx, p.AccountID, s.Now())
		if e != nil {
			return result, e
		}
		for _, x := range check.Items {
			if x.PlanID != nil && *x.PlanID == p.ID && x.Remaining.IsPositive() {
				return result, liability.ErrInvariant
			}
		}
		rule, e := liability.RuleForDate(tx, p.AccountID, in.EntryDate)
		if e != nil {
			return result, e
		}
		issued := s.Now()
		settlementBill := model.LiabilityStatement{AccountID: p.AccountID, RuleID: rule.ID, Kind: "settlement", BillKey: fmt.Sprintf("settlement:%d", op.ID), CycleStart: in.EntryDate, CycleEnd: in.EntryDate, DueDate: in.EntryDate, IssuedAt: &issued}
		for _, x := range check.Items {
			if x.PlanID == nil || *x.PlanID != p.ID {
				continue
			}
			for _, b := range check.Statements {
				if b.ID == x.StatementID && b.IssuedAt == nil {
					settlementBill.SnapshotAmount = settlementBill.SnapshotAmount.Add(x.Amount)
				}
			}
		}
		if e := tx.Create(&settlementBill).Error; e != nil {
			return result, e
		}
		for _, x := range check.Items {
			if x.PlanID == nil || *x.PlanID != p.ID {
				continue
			}
			for _, b := range check.Statements {
				if b.ID == x.StatementID && b.IssuedAt == nil {
					if e := tx.Model(&model.LiabilityDebtItem{}).Where("id = ?", x.ID).Update("statement_id", settlementBill.ID).Error; e != nil {
						return result, e
					}
				}
			}
		}
		if e := tx.Model(&model.InstallmentTerm{}).Where("plan_id = ? AND fee_txn_id IS NULL", p.ID).Updates(map[string]any{"fee_status": "cancelled", "blocked_reason": ""}).Error; e != nil {
			return result, e
		}
		if e := tx.Model(&model.InstallmentTerm{}).Where("plan_id = ?", p.ID).Update("status", "settled").Error; e != nil {
			return result, e
		}
		p.Status = "settled"
		p.Version++
		if e := tx.Model(&p).Updates(map[string]any{"status": p.Status, "version": p.Version}).Error; e != nil {
			return result, e
		}
		return result, nil
	})
}

type ReverseInput struct {
	RequestKey string `json:"request_key"`
	Reason     string `json:"reason"`
}

func (s *LiabilityService) Reverse(actor *model.User, id int64, in ReverseInput) (LiabilityResult, error) {
	var prior model.LiabilityOperation
	if e := s.db.First(&prior, id).Error; e != nil {
		return LiabilityResult{}, e
	}
	return s.operation(actor, prior.AccountID, "reverse", in.RequestKey, in.Reason, in, func(tx *gorm.DB, op *model.LiabilityOperation) (LiabilityResult, error) {
		result := LiabilityResult{TransactionIDs: []int64{}}
		if e := tx.First(&prior, id).Error; e != nil {
			return result, e
		}
		if prior.ReversedAt != nil || strings.TrimSpace(in.Reason) == "" {
			return result, liability.ErrConflict
		}
		if !CanModifyTransaction(actor, prior.ActorID) {
			return result, ErrTxnForbidden
		}
		if prior.Kind != "settle" && prior.Kind != "repayment" {
			return result, liability.ErrManaged
		}
		var original LiabilityResult
		var before liability.Detail
		if e := json.Unmarshal([]byte(prior.ResultJSON), &original); e != nil {
			return result, e
		}
		if e := json.Unmarshal([]byte(prior.BeforeJSON), &before); e != nil {
			return result, e
		}
		var p model.InstallmentPlan
		if prior.Kind == "settle" {
			if original.PlanID == 0 {
				return result, liability.ErrConflict
			}
			if e := tx.First(&p, original.PlanID).Error; e != nil {
				return result, e
			}
			if p.Version != original.Version || p.Status != "settled" {
				return result, liability.ErrConflict
			}
			if !CanModifyTransaction(actor, p.CreatedBy) {
				return result, ErrTxnForbidden
			}
			result.PlanID = p.ID
		}
		inner := s.txns.bound(tx)
		for _, txnID := range original.TransactionIDs {
			var t model.Transaction
			if e := tx.First(&t, txnID).Error; e != nil {
				return result, e
			}
			if e := inner.checkPostingOwnership(actor, &t); e != nil {
				return result, e
			}
			if !CanModifyTransaction(actor, t.CreatedBy) {
				return result, ErrTxnForbidden
			}
			if e := inner.guardOpen(t.PeriodKey); e != nil {
				return result, e
			}
			has, e := repository.NewTransactionRepo(tx).HasRefunds(t.ID)
			if e != nil {
				return result, e
			}
			if has {
				return result, ErrTxnRefundsExist
			}
			result.ChangedPeriods = append(result.ChangedPeriods, t.PeriodKey)
		}
		for creditID, signature := range original.CreditSignatures {
			var c model.LiabilityCreditEvent
			if e := tx.First(&c, creditID).Error; e != nil {
				return result, e
			}
			if c.Mode+":"+c.Targets != signature {
				return result, liability.ErrConflict
			}
			var source model.Transaction
			if c.SourceTxnID == nil {
				return result, liability.ErrConflict
			}
			if e := tx.First(&source, *c.SourceTxnID).Error; e != nil {
				return result, e
			}
			if !CanModifyTransaction(actor, source.CreatedBy) {
				return result, ErrTxnForbidden
			}
		}
		for _, txnID := range original.TransactionIDs {
			if e := repository.NewTransactionRepo(tx).WithActor(actor.ID).Delete(txnID); e != nil {
				return result, e
			}
		}
		if prior.Kind == "settle" {
			found := false
			for _, old := range before.Items {
				if old.PlanID != nil && *old.PlanID == p.ID {
					if e := tx.Model(&model.LiabilityDebtItem{}).Where("id = ? AND plan_id = ?", old.ID, p.ID).Update("statement_id", old.StatementID).Error; e != nil {
						return result, e
					}
				}
			}
			for _, old := range before.Plans {
				if old.ID != p.ID {
					continue
				}
				found = true
				for _, term := range old.Terms {
					if e := tx.Model(&model.InstallmentTerm{}).Where("id = ? AND plan_id = ?", term.ID, p.ID).Updates(map[string]any{"status": term.Status, "fee_status": term.FeeStatus, "blocked_reason": term.BlockedReason}).Error; e != nil {
						return result, e
					}
				}
				p.Status = old.Status
				p.Version++
				if e := tx.Model(&p).Updates(map[string]any{"status": p.Status, "version": p.Version}).Error; e != nil {
					return result, e
				}
			}
			if !found {
				return result, liability.ErrConflict
			}
			for _, old := range before.Credits {
				if _, ok := original.CreditSignatures[old.ID]; ok {
					var c model.LiabilityCreditEvent
					if e := tx.First(&c, old.ID).Error; e != nil {
						return result, e
					}
					if !c.Active || !c.Amount.Equal(old.Amount) {
						return result, liability.ErrConflict
					}
					if e := tx.Model(&c).Updates(map[string]any{"mode": old.Mode, "targets": old.Targets}).Error; e != nil {
						return result, e
					}
				}
			}
		}
		now := s.Now()
		return result, tx.Model(&prior).Update("reversed_at", now).Error
	})
}

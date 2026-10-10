package service

import (
	"errors"
	"fmt"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"jizhang/internal/liability"
	"jizhang/internal/model"
	"jizhang/internal/repository"
	"strings"
	"time"
)

type InstallmentInput struct {
	AccountID          int64           `json:"account_id"`
	SourceTxnID        int64           `json:"source_txn_id"`
	Amount             decimal.Decimal `json:"amount"`
	EntryDate          string          `json:"entry_date"`
	CategoryID         int64           `json:"category_id"`
	Note               string          `json:"note"`
	Platform           string          `json:"platform"`
	TermCount          int             `json:"term_count"`
	FirstStatementDate string          `json:"first_statement_date"`
	FeeMode            string          `json:"fee_mode"`
	FeeInput           decimal.Decimal `json:"fee_input"`
	FeeAccountID       int64           `json:"fee_account_id"`
	FeeCategoryID      int64           `json:"fee_category_id"`
	FeeDate            string          `json:"fee_date"`
	FeeDates           []string        `json:"fee_dates"`
	RequestKey         string          `json:"request_key"`
	Reason             string          `json:"reason"`
}

func (s *LiabilityService) CreatePlan(actor *model.User, in InstallmentInput) (LiabilityResult, error) {
	return s.operation(actor, in.AccountID, "create_plan", in.RequestKey, in.Reason, in, func(tx *gorm.DB, op *model.LiabilityOperation) (LiabilityResult, error) {
		result := LiabilityResult{TransactionIDs: []int64{}}
		var source *model.Transaction
		inner := s.txns.bound(tx)
		if in.SourceTxnID > 0 {
			var t model.Transaction
			if e := tx.First(&t, in.SourceTxnID).Error; e != nil {
				return result, e
			}
			source = &t
			if !CanModifyTransaction(actor, t.CreatedBy) {
				return result, ErrTxnForbidden
			}
		} else {
			date, e := liability.Parse(in.EntryDate)
			if e != nil {
				return result, e
			}
			var e2 error
			source, e2 = inner.create(actor, TxnInput{Kind: model.TxnExpense, Amount: in.Amount, AccountID: in.AccountID, EntryDate: date, CategoryID: in.CategoryID, Note: in.Note, Platform: in.Platform})
			if e2 != nil {
				return result, e2
			}
			result.TransactionIDs = append(result.TransactionIDs, source.ID)
		}
		if source.AccountID != in.AccountID || source.Kind != model.TxnExpense || source.IsVoucher() || source.InstallmentPlanID != nil || source.LiabilityTermID != nil || source.LiabilityOperationID != nil {
			return result, liability.ErrManaged
		}
		var item model.LiabilityDebtItem
		if e := tx.Where("source_id = ? AND account_id = ? AND active = ? AND plan_id IS NULL", source.ID, in.AccountID, true).First(&item).Error; e != nil {
			return result, e
		}
		items := []model.LiabilityDebtItem{item}
		if _, e := liability.FillAmounts(tx, items, nil); e != nil {
			return result, e
		}
		principal := items[0].Remaining
		if in.SourceTxnID > 0 && !principal.Equal(item.Amount) && strings.TrimSpace(in.Reason) == "" {
			return result, requestConflict("已有还款或退款，请填写未还本金核对原因")
		}
		parts, e := liability.Spread(principal, in.TermCount, true)
		if e != nil {
			return result, e
		}
		if !liability.ValidAmount(in.FeeInput, true) {
			return result, liability.ErrInvalid
		}
		feeMode := in.FeeMode
		if feeMode == "" {
			feeMode = "per_term"
		}
		feeParts := make([]decimal.Decimal, in.TermCount)
		switch feeMode {
		case "per_term":
			if !liability.ValidAmount(in.FeeInput.Mul(decimal.NewFromInt(int64(in.TermCount))), true) {
				return result, liability.ErrInvalid
			}
			for i := range feeParts {
				feeParts[i] = in.FeeInput
			}
		case "total_spread":
			feeParts, e = liability.Spread(in.FeeInput, in.TermCount, false)
			if e != nil {
				return result, e
			}
		case "upfront":
			feeParts[0] = in.FeeInput
		default:
			return result, liability.ErrInvalid
		}
		feeAccount := in.FeeAccountID
		if feeAccount == 0 {
			feeAccount = in.AccountID
		}
		feeCategory := in.FeeCategoryID
		if feeCategory == 0 && source.CategoryID != nil {
			feeCategory = *source.CategoryID
		}
		var fa model.Account
		var fc model.Category
		if e := tx.First(&fa, feeAccount).Error; e != nil {
			return result, e
		}
		if fa.Status != model.StatusActive || (fa.Type != model.AccountAsset && fa.ID != in.AccountID) {
			return result, ErrTxnAccountInvalid
		}
		if e := tx.First(&fc, feeCategory).Error; e != nil {
			return result, e
		}
		var children int64
		if e := tx.Model(&model.Category{}).Where("parent_id = ?", fc.ID).Count(&children).Error; e != nil {
			return result, e
		}
		if fc.Kind != model.KindExpense || children > 0 {
			return result, ErrTxnCategoryInvalid
		}
		first := in.FirstStatementDate
		if first == "" {
			first = liability.Date(source.EntryDate)
		}
		if _, e := liability.Parse(first); e != nil {
			return result, e
		}
		bill, e := liability.StatementFor(tx, in.AccountID, first)
		if e != nil {
			return result, e
		}
		if bill.CycleEnd < liability.Date(source.EntryDate) {
			return result, liability.ErrInvalid
		}
		p := model.InstallmentPlan{SourceTxnID: source.ID, AccountID: in.AccountID, Principal: principal, TermCount: in.TermCount, FeeMode: feeMode, FeeInput: in.FeeInput, FeeAccountID: feeAccount, FeeCategoryID: feeCategory, CreatedBy: source.CreatedBy, Version: 1, Status: "active"}
		if e := tx.Create(&p).Error; e != nil {
			return result, e
		}
		result.PlanID = p.ID
		for i, amount := range parts {
			if i > 0 {
				end, _ := liability.Parse(bill.CycleEnd)
				bill, e = liability.StatementFor(tx, in.AccountID, liability.Date(end.AddDate(0, 0, 1)))
				if e != nil {
					return result, e
				}
			}
			feeDate := bill.CycleEnd
			if len(in.FeeDates) > 0 {
				if len(in.FeeDates) != in.TermCount {
					return result, liability.ErrInvalid
				}
				feeDate = in.FeeDates[i]
			}
			if feeMode == "upfront" && i == 0 {
				feeDate = in.FeeDate
				if feeDate == "" {
					feeDate = liability.Date(source.EntryDate)
				}
			}
			if _, e := liability.Parse(feeDate); e != nil {
				return result, e
			}
			if feeDate < liability.Date(source.EntryDate) {
				return result, liability.ErrInvalid
			}
			feeStatus := "scheduled"
			if feeParts[i].IsZero() {
				feeStatus = "none"
			}
			term := model.InstallmentTerm{PlanID: p.ID, Number: i + 1, StatementID: bill.ID, Principal: amount, FeeAmount: feeParts[i], FeeDate: feeDate, Status: "scheduled", FeeStatus: feeStatus}
			if e := tx.Create(&term).Error; e != nil {
				return result, e
			}
			debt := model.LiabilityDebtItem{AccountID: in.AccountID, SourceTxnID: &source.ID, SourceID: source.ID, ItemKey: fmt.Sprintf("term:%d", term.ID), Kind: "principal", StatementID: bill.ID, PlanID: &p.ID, TermID: &term.ID, Amount: amount, Active: true}
			if e := tx.Create(&debt).Error; e != nil {
				return result, e
			}
		}
		// Already paid/refunded principal stays in the original item and history.
		keep := item.Amount.Sub(principal)
		if e := tx.Model(&item).Updates(map[string]any{"amount": keep, "active": keep.IsPositive()}).Error; e != nil {
			return result, e
		}
		if e := tx.Model(source).Update("installment_plan_id", p.ID).Error; e != nil {
			return result, e
		}
		var dueTerms []model.InstallmentTerm
		if e := tx.Where("plan_id = ? AND fee_date <= ? AND fee_amount > 0", p.ID, liability.Date(s.Now())).Find(&dueTerms).Error; e != nil {
			return result, e
		}
		for _, term := range dueTerms {
			feeID, e := s.postFee(tx, &p, &term, op.ID)
			if e != nil {
				return result, e
			}
			result.TransactionIDs = append(result.TransactionIDs, feeID)
		}
		return result, nil
	})
}

func (s *LiabilityService) postFee(tx *gorm.DB, p *model.InstallmentPlan, term *model.InstallmentTerm, operationID int64) (int64, error) {
	if p.Status == "needs_reconcile" {
		return 0, requestConflict("请先核对退款后的分期和未来手续费")
	}
	if term.FeeTxnID != nil {
		return *term.FeeTxnID, nil
	}
	if !term.FeeAmount.IsPositive() || term.FeeStatus == "cancelled" {
		return 0, nil
	}
	date, e := liability.Parse(term.FeeDate)
	if e != nil {
		return 0, e
	}
	inner := s.txns.bound(tx)
	period := inner.deps.Periods.KeyForDate(date)
	if e := inner.guardOpen(period); e != nil {
		return 0, e
	}
	var a model.Account
	var cat model.Category
	if e := tx.First(&a, p.FeeAccountID).Error; e != nil {
		return 0, e
	}
	if a.Status != model.StatusActive && a.ID != p.AccountID {
		return 0, ErrTxnAccountInvalid
	}
	if a.Type != model.AccountAsset && a.ID != p.AccountID {
		return 0, ErrTxnAccountInvalid
	}
	if e := tx.First(&cat, p.FeeCategoryID).Error; e != nil {
		return 0, e
	}
	var count int64
	if e := tx.Model(&model.Category{}).Where("parent_id = ?", cat.ID).Count(&count).Error; e != nil {
		return 0, e
	}
	if count > 0 || cat.Kind != model.KindExpense {
		return 0, ErrTxnCategoryInvalid
	}
	t := model.Transaction{Kind: model.TxnExpense, Amount: term.FeeAmount, AccountID: p.FeeAccountID, CategoryID: &p.FeeCategoryID, EntryDate: date, PeriodKey: period, Source: model.SourceLiabilityAuto, CreatedBy: p.CreatedBy, LiabilityTermID: &term.ID, Note: fmt.Sprintf("分期 %d 第 %d 期手续费", p.ID, term.Number)}
	if operationID > 0 {
		t.LiabilityOperationID = &operationID
	}
	if e := repository.NewTransactionRepo(tx).Create(&t); e != nil {
		return 0, e
	}
	if p.FeeAccountID == p.AccountID {
		if e := tx.Model(&model.LiabilityDebtItem{}).Where("source_id = ? AND account_id = ?", t.ID, p.AccountID).Updates(map[string]any{"plan_id": p.ID, "term_id": term.ID, "kind": "fee", "statement_id": term.StatementID}).Error; e != nil {
			return 0, e
		}
	}
	if e := tx.Model(term).Updates(map[string]any{"fee_txn_id": t.ID, "fee_status": "posted", "blocked_reason": ""}).Error; e != nil {
		return 0, e
	}
	p.Version++
	if e := tx.Model(p).Update("version", p.Version).Error; e != nil {
		return 0, e
	}
	return t.ID, nil
}

func (s *LiabilityService) ensureFees(now time.Time) (int, error) {
	var terms []model.InstallmentTerm
	if e := s.db.Where("fee_txn_id IS NULL AND fee_status IN ? AND fee_date <= ? AND fee_amount > 0", []string{"scheduled", "blocked"}, liability.Date(now)).Find(&terms).Error; e != nil {
		return 0, e
	}
	generated := 0
	for _, candidate := range terms {
		var feeID int64
		var period string
		e := repository.LedgerWrite(s.db, func(tx *gorm.DB) error {
			var term model.InstallmentTerm
			var p model.InstallmentPlan
			if e := tx.First(&term, candidate.ID).Error; e != nil {
				return e
			}
			if term.FeeTxnID != nil || term.FeeStatus == "cancelled" {
				return nil
			}
			if e := tx.First(&p, term.PlanID).Error; e != nil {
				return e
			}
			if p.Status == "settled" {
				return nil
			}
			op := model.LiabilityOperation{ActorID: 0, RequestKey: fmt.Sprintf("fee:%d", term.ID), Kind: "fee", AccountID: p.AccountID, PlanID: &p.ID, BeforeJSON: jsonText(term), ResultJSON: "{}"}
			if e := tx.Create(&op).Error; e != nil {
				return e
			}
			var e error
			feeID, e = s.postFee(tx, &p, &term, op.ID)
			if e != nil {
				return e
			}
			if e := liability.Reallocate(tx, p.AccountID); e != nil {
				return e
			}
			var a model.Account
			if e := tx.First(&a, p.AccountID).Error; e != nil {
				return e
			}
			if e := liability.CheckInvariant(tx, a); e != nil {
				return e
			}
			var t model.Transaction
			if e := tx.First(&t, feeID).Error; e != nil {
				return e
			}
			period = t.PeriodKey
			op.ResultJSON = jsonText(LiabilityResult{OperationID: op.ID, PlanID: p.ID, Version: p.Version, TransactionIDs: []int64{feeID}})
			return tx.Save(&op).Error
		})
		if e != nil {
			if !errors.Is(e, ErrTxnClosedPeriod) && !errors.Is(e, ErrTxnAccountInvalid) && !errors.Is(e, ErrTxnCategoryInvalid) && !errors.Is(e, liability.ErrInvalid) && !errors.Is(e, liability.ErrConflict) {
				return generated, e
			}
			if updateErr := repository.LedgerWrite(s.db, func(tx *gorm.DB) error {
				return tx.Model(&model.InstallmentTerm{}).Where("id = ? AND fee_txn_id IS NULL AND fee_status <> ?", candidate.ID, "cancelled").Updates(map[string]any{"fee_status": "blocked", "blocked_reason": e.Error()}).Error
			}); updateErr != nil {
				return generated, updateErr
			}
			continue
		}
		if feeID > 0 {
			generated++
			s.txns.fireWrite()
			s.txns.firePeriods(period)
		}
	}
	return generated, nil
}

type TermChange struct {
	ID                 int64            `json:"id"`
	StatementID        int64            `json:"statement_id"`
	RemainingPrincipal *decimal.Decimal `json:"remaining_principal"`
	FeeAmount          *decimal.Decimal `json:"fee_amount"`
	FeeDate            string           `json:"fee_date"`
	FeeTxnID           *int64           `json:"fee_txn_id"`
}
type InstallmentUpdate struct {
	RequestKey  string       `json:"request_key"`
	Reason      string       `json:"reason"`
	Version     int          `json:"version"`
	RefundTxnID int64        `json:"refund_txn_id"`
	Terms       []TermChange `json:"terms"`
}

func (s *LiabilityService) UpdatePlan(actor *model.User, id int64, in InstallmentUpdate, refund bool) (LiabilityResult, error) {
	var p model.InstallmentPlan
	if e := s.db.First(&p, id).Error; e != nil {
		return LiabilityResult{}, e
	}
	kind := "adjust_plan"
	if refund {
		kind = "refund_adjustment"
	}
	return s.operation(actor, p.AccountID, kind, in.RequestKey, in.Reason, in, func(tx *gorm.DB, op *model.LiabilityOperation) (LiabilityResult, error) {
		result := LiabilityResult{PlanID: id, TransactionIDs: []int64{}}
		if e := tx.First(&p, id).Error; e != nil {
			return result, e
		}
		if !CanModifyTransaction(actor, p.CreatedBy) {
			return result, ErrTxnForbidden
		}
		if p.Version != in.Version || p.Status == "settled" || strings.TrimSpace(in.Reason) == "" {
			return result, liability.ErrConflict
		}
		if refund {
			var r model.Transaction
			if e := tx.First(&r, in.RefundTxnID).Error; e != nil {
				return result, e
			}
			if r.Kind != model.TxnRefund || r.RelatedTxnID == nil || *r.RelatedTxnID != p.SourceTxnID {
				return result, liability.ErrInvalid
			}
		}
		p.Status = "active"
		detail, e := detailAt(tx, p.AccountID, s.Now())
		if e != nil {
			return result, e
		}
		originals := map[int64]model.LiabilityDebtItem{}
		before := decimal.Zero
		for _, d := range detail.Items {
			if d.PlanID != nil && *d.PlanID == id && d.Kind == "principal" && d.TermID != nil {
				originals[*d.TermID] = d
				before = before.Add(d.Remaining)
			}
		}
		expected := before
		seen := map[int64]bool{}
		for _, change := range in.Terms {
			if seen[change.ID] {
				return result, liability.ErrInvalid
			}
			seen[change.ID] = true
			var term model.InstallmentTerm
			if e := tx.First(&term, change.ID).Error; e != nil {
				return result, e
			}
			if term.PlanID != id {
				return result, liability.ErrInvalid
			}
			d, ok := originals[term.ID]
			if !ok {
				return result, liability.ErrInvalid
			}
			var bill model.LiabilityStatement
			if e := tx.First(&bill, term.StatementID).Error; e != nil {
				return result, e
			}
			if change.RemainingPrincipal != nil {
				if !liability.ValidAmount(*change.RemainingPrincipal, true) || (!refund && bill.IssuedAt != nil) {
					return result, liability.ErrManaged
				}
				if bill.IssuedAt != nil && !d.Remaining.IsPositive() && change.RemainingPrincipal.IsPositive() {
					return result, liability.ErrConflict
				}
				expected = expected.Sub(d.Remaining).Add(*change.RemainingPrincipal)
				amount := d.Amount.Sub(d.Remaining).Add(*change.RemainingPrincipal)
				term.Principal = amount
				if e := tx.Model(&d).Update("amount", amount).Error; e != nil {
					return result, e
				}
			}
			if change.StatementID > 0 && change.StatementID != term.StatementID {
				var target model.LiabilityStatement
				if e := tx.First(&target, change.StatementID).Error; e != nil {
					return result, e
				}
				if target.AccountID != p.AccountID || bill.IssuedAt != nil || target.IssuedAt != nil {
					return result, liability.ErrInvalid
				}
				term.StatementID = target.ID
				if e := tx.Model(&d).Update("statement_id", target.ID).Error; e != nil {
					return result, e
				}
			}
			if change.FeeAmount != nil || change.FeeDate != "" {
				if term.FeeTxnID != nil {
					return result, liability.ErrManaged
				}
				if change.FeeAmount != nil {
					if !liability.ValidAmount(*change.FeeAmount, true) {
						return result, liability.ErrInvalid
					}
					term.FeeAmount = *change.FeeAmount
				}
				if change.FeeDate != "" {
					if _, e := liability.Parse(change.FeeDate); e != nil {
						return result, e
					}
					term.FeeDate = change.FeeDate
				}
				term.FeeStatus = "scheduled"
				if term.FeeAmount.IsZero() {
					term.FeeStatus = "none"
				}
				term.BlockedReason = ""
			}
			if change.FeeTxnID != nil {
				if term.FeeTxnID != nil {
					return result, liability.ErrManaged
				}
				var t model.Transaction
				if e := tx.First(&t, *change.FeeTxnID).Error; e != nil {
					return result, e
				}
				if !CanModifyTransaction(actor, t.CreatedBy) {
					return result, ErrTxnForbidden
				}
				if t.Kind != model.TxnExpense || t.IsVoucher() || t.InstallmentPlanID != nil || t.LiabilityTermID != nil || t.LiabilityOperationID != nil || t.AccountID != p.FeeAccountID || !t.Amount.Equal(term.FeeAmount) || liability.Date(t.EntryDate) != term.FeeDate {
					return result, liability.ErrInvalid
				}
				term.FeeTxnID = &t.ID
				term.FeeStatus = "posted"
				term.BlockedReason = ""
				if e := tx.Model(&t).Update("liability_term_id", term.ID).Error; e != nil {
					return result, e
				}
				if t.AccountID == p.AccountID {
					if e := tx.Model(&model.LiabilityDebtItem{}).Where("source_id = ? AND account_id = ?", t.ID, p.AccountID).Updates(map[string]any{"plan_id": p.ID, "term_id": term.ID, "kind": "fee", "statement_id": term.StatementID}).Error; e != nil {
						return result, e
					}
				}
			}
			if e := tx.Save(&term).Error; e != nil {
				return result, e
			}
			if term.FeeTxnID == nil && term.FeeAmount.IsPositive() && term.FeeDate <= liability.Date(s.Now()) {
				feeID, e := s.postFee(tx, &p, &term, op.ID)
				if e != nil {
					return result, e
				}
				result.TransactionIDs = append(result.TransactionIDs, feeID)
			}
		}
		if !expected.Equal(before) {
			return result, requestConflict("调整后的各期未还本金合计必须等于真实剩余本金")
		}
		p.Status = "active"
		p.Version++
		if e := tx.Save(&p).Error; e != nil {
			return result, e
		}
		return result, nil
	})
}

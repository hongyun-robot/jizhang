package service

import (
	"encoding/json"
	"gorm.io/gorm"
	"jizhang/internal/liability"
	"jizhang/internal/model"
	"jizhang/internal/repository"
	"strings"
)

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
		if prior.Kind != "settle" && prior.Kind != "repayment" && prior.Kind != "installment_repayment" && prior.Kind != "allocation" {
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
		if len(original.TermIDs) > 0 {
			return s.reverseTermPayment(tx, actor, &prior, original, before)
		}
		if prior.Kind == "allocation" {
			return result, liability.ErrManaged
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
			if prior.Kind == "repayment" && original.RepaymentScope != unbilledOrdinary {
				if e := inner.guardSettlementFunds(&t); e != nil {
					return result, e
				}
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

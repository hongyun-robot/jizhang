package service

import (
	"encoding/json"
	"reflect"

	"gorm.io/gorm"

	"jizhang/internal/liability"
	"jizhang/internal/model"
	"jizhang/internal/repository"
)

func (s *LiabilityService) reverseTermPayment(tx *gorm.DB, actor *model.User, prior *model.LiabilityOperation, original LiabilityResult, before liability.Detail) (LiabilityResult, error) {
	result := LiabilityResult{PlanID: original.PlanID, TermIDs: original.TermIDs, TransactionIDs: []int64{}}
	expectedPlans := original.PlanSnapshots
	if len(expectedPlans) == 0 && original.PlanSnapshot != nil {
		expectedPlans = []model.InstallmentPlan{*original.PlanSnapshot}
	}
	if len(expectedPlans) == 0 {
		return result, liability.ErrConflict
	}
	planIDs := map[int64]bool{}
	for _, expected := range expectedPlans {
		var current model.InstallmentPlan
		if err := tx.First(&current, expected.ID).Error; err != nil {
			return result, err
		}
		if !CanModifyTransaction(actor, current.CreatedBy) {
			return result, ErrTxnForbidden
		}
		if current.Status != expected.Status {
			return result, requestConflict("计划存在后续核对，请先处理相关操作")
		}
		planIDs[current.ID] = true
		result.PlanIDs = append(result.PlanIDs, current.ID)
	}
	var successors []model.LiabilityOperation
	if err := tx.Where("account_id = ? AND id > ? AND reversed_at IS NULL AND kind IN ?", prior.AccountID, prior.ID, []string{"installment_repayment", "settle", "repayment", "allocation", "adjust_plan", "refund_adjustment"}).Find(&successors).Error; err != nil {
		return result, err
	}
	for _, successor := range successors {
		if successor.PlanID != nil && planIDs[*successor.PlanID] {
			return result, requestConflict("请先撤销依赖此计划的后续操作")
		}
		var recorded LiabilityResult
		if err := json.Unmarshal([]byte(successor.ResultJSON), &recorded); err != nil {
			return result, err
		}
		for _, id := range recorded.PlanIDs {
			if planIDs[id] {
				return result, requestConflict("请先撤销依赖此计划的后续操作")
			}
		}
	}
	selected := map[int64]bool{}
	for _, id := range original.TermIDs {
		selected[id] = true
	}
	for _, expectedPlan := range expectedPlans {
		for _, expected := range expectedPlan.Terms {
			if !selected[expected.ID] {
				continue
			}
			var current model.InstallmentTerm
			if err := tx.First(&current, expected.ID).Error; err != nil {
				return result, err
			}
			if current.Status != expected.Status || current.FeeStatus != expected.FeeStatus || !reflect.DeepEqual(current.FeeTxnID, expected.FeeTxnID) || !current.Principal.Equal(expected.Principal) || !current.FeeAmount.Equal(expected.FeeAmount) || current.FeeDate != expected.FeeDate {
				return result, requestConflict("期次或费用已变化，请先处理相关操作")
			}
		}
	}
	inner := s.txns.bound(tx)
	for _, id := range original.TransactionIDs {
		var source model.Transaction
		if err := tx.First(&source, id).Error; err != nil {
			return result, err
		}
		if !CanModifyTransaction(actor, source.CreatedBy) {
			return result, ErrTxnForbidden
		}
		if err := inner.checkPostingOwnership(actor, &source); err != nil {
			return result, err
		}
		if err := inner.guardOpen(source.PeriodKey); err != nil {
			return result, err
		}
		refunds, err := repository.NewTransactionRepo(tx).HasRefunds(id)
		if err != nil {
			return result, err
		}
		if refunds {
			return result, ErrTxnRefundsExist
		}
		result.ChangedPeriods = append(result.ChangedPeriods, source.PeriodKey)
	}
	for _, id := range original.ManagedSourceIDs {
		found := false
		for _, source := range before.Transactions {
			if source.ID != id {
				continue
			}
			found = true
			if err := inner.guardOpen(source.PeriodKey); err != nil {
				return result, err
			}
			if err := tx.Model(&model.Transaction{}).Where("id = ?", id).Update("liability_operation_id", source.LiabilityOperationID).Error; err != nil {
				return result, err
			}
		}
		if !found {
			return result, liability.ErrConflict
		}
	}
	for id, expected := range original.CreditSignatures {
		var credit model.LiabilityCreditEvent
		if err := tx.First(&credit, id).Error; err != nil {
			return result, err
		}
		if !credit.Active || credit.Mode+":"+credit.Targets != expected {
			return result, requestConflict("资金已被后续操作使用，请先处理后续操作")
		}
		var source model.Transaction
		if err := tx.First(&source, credit.SourceID).Error; err != nil {
			return result, err
		}
		if !CanModifyTransaction(actor, source.CreatedBy) {
			return result, ErrTxnForbidden
		}
		if err := inner.checkPostingOwnership(actor, &source); err != nil {
			return result, err
		}
		if err := inner.guardOpen(source.PeriodKey); err != nil {
			return result, err
		}
	}
	for _, id := range original.TransactionIDs {
		if err := repository.NewTransactionRepo(tx).WithActor(actor.ID).Delete(id); err != nil {
			return result, err
		}
	}
	for _, old := range before.Items {
		if old.TermID != nil && selected[*old.TermID] {
			if err := tx.Model(&model.LiabilityDebtItem{}).Where("id = ?", old.ID).Update("statement_id", old.StatementID).Error; err != nil {
				return result, err
			}
		}
	}
	found := map[int64]bool{}
	for _, old := range before.Plans {
		if !planIDs[old.ID] {
			continue
		}
		found[old.ID] = true
		for _, term := range old.Terms {
			if !selected[term.ID] {
				continue
			}
			if err := tx.Model(&model.InstallmentTerm{}).Where("id = ? AND plan_id = ?", term.ID, old.ID).Updates(map[string]any{"status": term.Status, "fee_status": term.FeeStatus, "fee_txn_id": term.FeeTxnID, "fee_date": term.FeeDate, "blocked_reason": term.BlockedReason}).Error; err != nil {
				return result, err
			}
		}
		if err := tx.Model(&model.InstallmentPlan{}).Where("id = ?", old.ID).Updates(map[string]any{"status": old.Status, "version": gorm.Expr("version + 1")}).Error; err != nil {
			return result, err
		}
	}
	if len(found) != len(planIDs) {
		return result, liability.ErrConflict
	}
	for _, old := range before.Credits {
		if _, changed := original.CreditSignatures[old.ID]; !changed {
			continue
		}
		var current model.LiabilityCreditEvent
		if err := tx.First(&current, old.ID).Error; err != nil {
			return result, err
		}
		if !current.Active || !current.Amount.Equal(old.Amount) {
			return result, liability.ErrConflict
		}
		if err := tx.Model(&current).Updates(map[string]any{"mode": old.Mode, "targets": old.Targets}).Error; err != nil {
			return result, err
		}
	}
	if err := tx.Model(prior).Update("reversed_at", s.Now()).Error; err != nil {
		return result, err
	}
	return result, nil
}

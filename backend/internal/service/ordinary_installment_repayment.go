package service

import (
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"jizhang/internal/liability"
	"jizhang/internal/model"
)

type ordinaryPlanPayment struct {
	Plan  model.InstallmentPlan
	Terms []InstallmentRepaymentTerm
}

type preparedRepayment struct {
	Service *LiabilityService
	Op      *model.LiabilityOperation
	Before  *liability.Detail
	Parts   []ordinaryPlanPayment
	Targets []liability.Target
	Result  LiabilityResult
	OwnOp   bool
}

func (s *TransactionService) liabilityClock() time.Time {
	if s.liabilityNow != nil {
		return s.liabilityNow()
	}
	return time.Now()
}

// Ordinary interfaces can pay a complete issued term, but never infer a partial
// term or park its remainder. The explicit plan interface handles future terms.
func (s *LiabilityService) prepareOrdinaryRepayment(tx *gorm.DB, actor *model.User, account, from int64, amount decimal.Decimal, date, mode string, requested []liability.Target, op *model.LiabilityOperation) (*preparedRepayment, error) {
	d, err := detailAt(tx, account, s.Now())
	if err != nil {
		return nil, err
	}
	if len(d.Plans) == 0 {
		return nil, nil
	}
	bills := map[int64]model.LiabilityStatement{}
	plans := map[int64]model.InstallmentPlan{}
	terms := map[int64]model.InstallmentTerm{}
	for _, b := range d.Statements {
		bills[b.ID] = b
	}
	for _, p := range d.Plans {
		plans[p.ID] = p
		for _, term := range p.Terms {
			terms[term.ID] = term
		}
	}
	items := append([]model.LiabilityDebtItem(nil), d.Items...)
	sort.Slice(items, func(i, j int) bool {
		if bills[items[i].StatementID].DueDate != bills[items[j].StatementID].DueDate {
			return bills[items[i].StatementID].DueDate < bills[items[j].StatementID].DueDate
		}
		return items[i].ID < items[j].ID
	})
	budgets := requested
	if mode != "manual" {
		if len(requested) != 0 {
			return nil, liability.ErrInvalid
		}
		budgets = []liability.Target{{Amount: amount}}
	} else {
		sum := decimal.Zero
		for _, target := range budgets {
			if target.DebtItemID != 0 || target.StatementID == 0 || bills[target.StatementID].AccountID != account || bills[target.StatementID].IssuedAt == nil || !liability.ValidAmount(target.Amount, false) {
				return nil, liability.ErrInvalid
			}
			sum = sum.Add(target.Amount)
		}
		if !sum.Equal(amount) {
			return nil, requestConflict("手动分配必须使用全部还款金额")
		}
	}
	selected := map[int64][]InstallmentRepaymentTerm{}
	seen := map[int64]bool{}
	targets := []liability.Target{}
	for _, budget := range budgets {
		left := budget.Amount
		for _, item := range items {
			if !left.IsPositive() || !item.Remaining.IsPositive() || bills[item.StatementID].IssuedAt == nil || (budget.StatementID != 0 && item.StatementID != budget.StatementID) {
				continue
			}
			if item.TermID == nil {
				take := decimal.Min(left, item.Remaining)
				targets = append(targets, liability.Target{DebtItemID: item.ID, Amount: take})
				left = left.Sub(take)
				continue
			}
			if seen[*item.TermID] {
				continue
			}
			seen[*item.TermID] = true
			term := terms[*item.TermID]
			quote := termQuote(d, term)
			if !quote.Principal.IsPositive() || left.LessThan(quote.TotalAmount) {
				return nil, requestConflict("分期必须整期偿还本金和手续费，请从分期还款入口核对")
			}
			selected[term.PlanID] = append(selected[term.PlanID], quote)
			left = left.Sub(quote.TotalAmount)
		}
		if left.IsPositive() {
			return nil, requestConflict("还款金额必须完整对应可偿付欠款，未来分期请从分期入口操作")
		}
	}
	if len(selected) == 0 {
		return nil, nil
	}
	prepared := &preparedRepayment{Service: s, Op: op, Before: d, Targets: targets, Result: LiabilityResult{TransactionIDs: []int64{}, TermIDs: []int64{}, CreditSignatures: map[int64]string{}}}
	planIDs := []int64{}
	for id := range selected {
		planIDs = append(planIDs, id)
	}
	sort.Slice(planIDs, func(i, j int) bool { return planIDs[i] < planIDs[j] })
	for _, id := range planIDs {
		p := plans[id]
		ids := []int64{}
		for _, term := range selected[id] {
			ids = append(ids, term.ID)
		}
		input := InstallmentRepaymentInput{TermIDs: ids, FromAccountID: from, EntryDate: date}
		quote, _, p, err := s.repaymentPreviewAt(tx, actor, id, input)
		if err != nil {
			return nil, err
		}
		prepared.Parts = append(prepared.Parts, ordinaryPlanPayment{Plan: p, Terms: quote.Terms})
		prepared.Result.PlanIDs = append(prepared.Result.PlanIDs, id)
		prepared.Result.TermIDs = append(prepared.Result.TermIDs, ids...)
	}
	if len(planIDs) == 1 {
		prepared.Result.PlanID = planIDs[0]
	}
	if op == nil {
		prepared.OwnOp = true
		prepared.Op = &model.LiabilityOperation{ActorID: actor.ID, RequestKey: "ordinary:" + uuid.NewString(), Kind: "installment_repayment", AccountID: account, BeforeJSON: jsonText(operationSnapshot(d)), ResultJSON: "{}", Reason: "普通转账整期还款"}
		if err := tx.Create(prepared.Op).Error; err != nil {
			return nil, err
		}
	}
	if prepared.Result.PlanID > 0 {
		id := prepared.Result.PlanID
		prepared.Op.PlanID = &id
		if err := tx.Model(prepared.Op).Update("plan_id", id).Error; err != nil {
			return nil, err
		}
	}
	selectedIDs := map[int64]bool{}
	for _, id := range prepared.Result.TermIDs {
		selectedIDs[id] = true
	}
	if err := s.fixRepaymentSources(tx, actor, d, selectedIDs, &prepared.Result); err != nil {
		return nil, err
	}
	for _, item := range d.Items {
		if item.TermID != nil && selectedIDs[*item.TermID] && item.Remaining.IsPositive() {
			prepared.Targets = append(prepared.Targets, liability.Target{DebtItemID: item.ID, Amount: item.Remaining})
		}
	}
	for _, part := range prepared.Parts {
		p := part.Plan
		for _, term := range p.Terms {
			if !selectedIDs[term.ID] || term.FeeTxnID != nil || term.FeeStatus == "cancelled" || !term.FeeAmount.IsPositive() {
				continue
			}
			id, err := s.postFeeAt(tx, &p, &term, prepared.Op.ID, date, p.AccountID)
			if err != nil {
				return nil, err
			}
			prepared.Result.TransactionIDs = append(prepared.Result.TransactionIDs, id)
			var fee model.LiabilityDebtItem
			if err := tx.Where("source_id = ? AND account_id = ?", id, account).First(&fee).Error; err != nil {
				return nil, err
			}
			prepared.Targets = append(prepared.Targets, liability.Target{DebtItemID: fee.ID, Amount: term.FeeAmount})
		}
	}
	return prepared, nil
}

func (p *preparedRepayment) finish(tx *gorm.DB, actor *model.User, source *model.Transaction, newSource bool) (LiabilityResult, error) {
	result := p.Result
	if newSource {
		result.TransactionIDs = append(result.TransactionIDs, source.ID)
	} else {
		result.ManagedSourceIDs = append(result.ManagedSourceIDs, source.ID)
	}
	if err := tx.Model(source).Update("liability_operation_id", p.Op.ID).Error; err != nil {
		return result, err
	}
	source.LiabilityOperationID = &p.Op.ID
	var credit model.LiabilityCreditEvent
	if err := tx.Where("source_id = ? AND account_id = ? AND active = ?", source.ID, p.Op.AccountID, true).First(&credit).Error; err != nil {
		return result, err
	}
	targets := jsonText(p.Targets)
	if err := tx.Model(&credit).Updates(map[string]any{"mode": "settlement", "targets": targets}).Error; err != nil {
		return result, err
	}
	result.CreditSignatures[credit.ID] = "settlement:" + targets
	selected := map[int64]bool{}
	for _, id := range result.TermIDs {
		selected[id] = true
	}
	if err := p.Service.movePrepaidItems(tx, p.Op, p.Before, selected, liability.Date(source.EntryDate)); err != nil {
		return result, err
	}
	if err := tx.Model(&model.InstallmentTerm{}).Where("id IN ?", result.TermIDs).Update("status", "settled").Error; err != nil {
		return result, err
	}
	if err := liability.Reallocate(tx, p.Op.AccountID); err != nil {
		return result, err
	}
	d, err := detailAt(tx, p.Op.AccountID, p.Service.Now())
	if err != nil {
		return result, err
	}
	for _, part := range p.Parts {
		for _, plan := range d.Plans {
			if plan.ID != part.Plan.ID {
				continue
			}
			remaining := decimal.Zero
			for _, term := range plan.Terms {
				quote := termQuote(d, term)
				if selected[term.ID] && !quote.TotalAmount.IsZero() {
					return result, liability.ErrInvariant
				}
				remaining = remaining.Add(quote.TotalAmount)
			}
			status := "active"
			if remaining.IsZero() {
				status = "settled"
			}
			if err := tx.Model(&model.InstallmentPlan{}).Where("id = ?", plan.ID).Updates(map[string]any{"status": status, "version": gorm.Expr("version + 1")}).Error; err != nil {
				return result, err
			}
			plan.Status = status
			plan.Version++
			result.PlanSnapshots = append(result.PlanSnapshots, plan)
			if plan.ID == result.PlanID {
				result.PlanSnapshot = &plan
				result.Version = plan.Version
			}
		}
	}
	result.OperationID = p.Op.ID
	if p.OwnOp {
		for _, id := range result.TransactionIDs {
			var transaction model.Transaction
			if err := tx.First(&transaction, id).Error; err != nil {
				return result, err
			}
			result.PostingSnapshots = append(result.PostingSnapshots, transaction)
		}
		if err := tx.Model(p.Op).Update("result_json", jsonText(result)).Error; err != nil {
			return result, err
		}
	}
	return result, nil
}

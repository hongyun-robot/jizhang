package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"jizhang/internal/liability"
	"jizhang/internal/model"
	"jizhang/internal/repository"
)

const combinedRepayment = "all"

type CombinedRepaymentTerm struct {
	PlanID int64 `json:"plan_id"`
	InstallmentRepaymentTerm
}

// Planning is read-only; the existing term-payment writer applies the whole
// payment in one ledger transaction after its quote has been checked.
func (s *LiabilityService) combinedPreviewAt(tx *gorm.DB, actor *model.User, in LiabilityRepaymentInput) (OrdinaryRepaymentPreview, *preparedRepayment, error) {
	out := OrdinaryRepaymentPreview{Scope: combinedRepayment, Amount: in.Amount, Bills: []OrdinaryRepaymentBill{}, Allocations: []liability.AdvanceAllocation{}, Installments: []CombinedRepaymentTerm{}}
	if !CanCreateTransaction(actor) {
		return out, nil, ErrTxnForbidden
	}
	if in.Scope != combinedRepayment || !liability.ValidAmount(in.Amount, false) || (in.Mode != "" && in.Mode != "auto") || len(in.Targets) != 0 {
		return out, nil, liability.ErrInvalid
	}
	if err := checkAccountOwnership(tx, actor, in.AccountID); err != nil {
		return out, nil, err
	}
	if err := checkRepaymentAccount(tx, actor, in.FromAccountID); err != nil {
		return out, nil, err
	}
	date, err := liability.Parse(in.EntryDate)
	if err != nil || in.EntryDate > liability.Date(s.Now()) {
		return out, nil, liability.ErrInvalid
	}
	if err := s.txns.bound(tx).guardOpen(s.txns.bound(tx).deps.Periods.KeyForDate(date)); err != nil {
		return out, nil, err
	}
	d, err := detailAt(tx, in.AccountID, s.Now())
	if err != nil {
		return out, nil, err
	}
	if !d.Account.BillingEnabled {
		return out, nil, liability.ErrSetup
	}
	bills := map[int64]model.LiabilityStatement{}
	sources := map[int64]model.Transaction{}
	for _, b := range d.Statements {
		bills[b.ID] = b
	}
	for _, source := range d.Transactions {
		sources[source.ID] = source
	}
	type candidate struct {
		bill   int64
		item   model.LiabilityDebtItem
		plan   int64
		term   InstallmentRepaymentTerm
		amount decimal.Decimal
	}
	candidates := []candidate{}
	for _, item := range d.Items {
		source := sources[item.SourceID]
		if item.TermID == nil && item.PlanID == nil && item.Active && item.Remaining.IsPositive() && source.ID > 0 && item.SourceTxnID != nil && *item.SourceTxnID == source.ID && liability.Date(source.EntryDate) <= in.EntryDate {
			candidates = append(candidates, candidate{bill: item.StatementID, item: item, amount: item.Remaining})
		}
	}
	for _, plan := range d.Plans {
		if source := sources[plan.SourceTxnID]; source.ID == 0 || liability.Date(source.EntryDate) > in.EntryDate {
			continue
		}
		for _, term := range plan.Terms {
			quote := termQuote(d, term)
			if quote.TotalAmount.IsPositive() {
				candidates = append(candidates, candidate{bill: term.StatementID, plan: plan.ID, term: quote, amount: quote.TotalAmount})
			}
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		x, y := bills[a.bill], bills[b.bill]
		if x.DueDate != y.DueDate {
			return x.DueDate < y.DueDate
		}
		if x.CycleEnd != y.CycleEnd {
			return x.CycleEnd < y.CycleEnd
		}
		if a.plan == 0 || b.plan == 0 {
			if a.plan != b.plan {
				return a.plan == 0
			}
			if sources[a.item.SourceID].EntryDate != sources[b.item.SourceID].EntryDate {
				return sources[a.item.SourceID].EntryDate.Before(sources[b.item.SourceID].EntryDate)
			}
			return a.item.ID < b.item.ID
		}
		if a.plan != b.plan {
			return a.plan < b.plan
		}
		return a.term.Number < b.term.Number
	})
	capacity := map[int64]decimal.Decimal{}
	for _, c := range candidates {
		out.AvailableAmount = out.AvailableAmount.Add(c.amount)
		capacity[c.bill] = capacity[c.bill].Add(c.amount)
	}
	seenBills := map[int64]bool{}
	for _, c := range candidates {
		if seenBills[c.bill] {
			continue
		}
		seenBills[c.bill] = true
		b := bills[c.bill]
		out.Bills = append(out.Bills, OrdinaryRepaymentBill{b.ID, b.CycleStart, b.CycleEnd, b.DueDate, capacity[b.ID]})
	}
	if in.Amount.GreaterThan(out.AvailableAmount) {
		return out, nil, requestConflict(fmt.Sprintf("本次最多可还 %s 元（包含未付分期手续费）", out.AvailableAmount.StringFixed(2)))
	}
	prepared := &preparedRepayment{Service: s, Before: d, EntryDate: in.EntryDate, CreditMode: liability.CombinedRepayment, Result: LiabilityResult{RepaymentScope: combinedRepayment, AllocationMode: "auto", TransactionIDs: []int64{}, TermIDs: []int64{}, CreditSignatures: map[int64]string{}}}
	selected := map[int64][]int64{}
	left := in.Amount
	for _, c := range candidates {
		if !left.IsPositive() {
			break
		}
		if c.plan == 0 {
			take := decimal.Min(left, c.amount)
			prepared.Targets = append(prepared.Targets, liability.Target{DebtItemID: c.item.ID, Amount: take})
			out.Allocations = append(out.Allocations, liability.AdvanceAllocation{StatementID: c.bill, DebtItemID: c.item.ID, SourceID: c.item.SourceID, Amount: take})
			out.OrdinaryAmount = out.OrdinaryAmount.Add(take)
			left = left.Sub(take)
			continue
		}
		if left.LessThan(c.amount) {
			return out, nil, requestConflict(fmt.Sprintf("分期第 %d 期须连同手续费整期偿还，本次可改为 %s 元", c.term.Number, in.Amount.Add(c.amount.Sub(left)).StringFixed(2)))
		}
		selected[c.plan] = append(selected[c.plan], c.term.ID)
		out.Installments = append(out.Installments, CombinedRepaymentTerm{PlanID: c.plan, InstallmentRepaymentTerm: c.term})
		out.FeeAmount = out.FeeAmount.Add(c.term.OutstandingFees).Add(c.term.ScheduledFees)
		left = left.Sub(c.amount)
	}
	if !left.IsZero() {
		return out, nil, liability.ErrConflict
	}
	planIDs := []int64{}
	for id := range selected {
		planIDs = append(planIDs, id)
	}
	sort.Slice(planIDs, func(i, j int) bool { return planIDs[i] < planIDs[j] })
	for _, id := range planIDs {
		quote, _, plan, err := s.repaymentPreviewAt(tx, actor, id, InstallmentRepaymentInput{TermIDs: selected[id], FromAccountID: in.FromAccountID, EntryDate: in.EntryDate})
		if err != nil {
			return out, nil, err
		}
		prepared.Parts = append(prepared.Parts, ordinaryPlanPayment{Plan: plan, Terms: quote.Terms})
		prepared.Result.PlanIDs = append(prepared.Result.PlanIDs, id)
		prepared.Result.TermIDs = append(prepared.Result.TermIDs, selected[id]...)
	}
	encoded, err := json.Marshal(struct {
		Actor       int64
		From        int64
		Date        string
		Items       []model.LiabilityDebtItem
		Plans       []model.InstallmentPlan
		Sources     []model.Transaction
		Credits     []model.LiabilityCreditEvent
		Allocations []model.LiabilityAllocation
		Preview     OrdinaryRepaymentPreview
	}{actor.ID, in.FromAccountID, in.EntryDate, d.Items, d.Plans, d.Transactions, d.Credits, d.Allocations, out})
	if err != nil {
		return out, nil, err
	}
	sum := sha256.Sum256(encoded)
	out.QuoteSignature = hex.EncodeToString(sum[:])
	return out, prepared, nil
}

func (s *LiabilityService) repayCombined(tx *gorm.DB, actor *model.User, in LiabilityRepaymentInput, op *model.LiabilityOperation) (LiabilityResult, error) {
	quote, prepared, err := s.combinedPreviewAt(tx, actor, in)
	if err != nil {
		return LiabilityResult{}, err
	}
	if in.QuoteSignature == "" || quote.QuoteSignature != in.QuoteSignature {
		return LiabilityResult{}, requestConflict("欠款或分期费用已变化，请重新核对还款金额")
	}
	prepared.Op = op
	if err := prepared.begin(tx, actor); err != nil {
		return LiabilityResult{}, err
	}
	date, _ := liability.Parse(in.EntryDate)
	transfer := model.Transaction{Kind: model.TxnTransfer, AccountID: in.FromAccountID, TransferAccountID: &in.AccountID, Amount: in.Amount, EntryDate: date, PeriodKey: s.txns.bound(tx).deps.Periods.KeyForDate(date), CreatedBy: actor.ID, Source: model.SourceManual, Note: in.Reason, LiabilityOperationID: &op.ID}
	if err := repository.NewTransactionRepo(tx).Create(&transfer); err != nil {
		return LiabilityResult{}, err
	}
	return prepared.finish(tx, actor, &transfer, true)
}

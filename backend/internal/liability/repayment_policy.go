package liability

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"jizhang/internal/model"
)

type UnallocatedFund struct {
	AccountID int64           `json:"account_id"`
	SourceID  int64           `json:"source_id"`
	Amount    decimal.Decimal `json:"amount"`
}

type TermProblem struct {
	AccountID int64  `json:"account_id"`
	PlanID    int64  `json:"plan_id"`
	TermID    int64  `json:"term_id"`
	Number    int    `json:"number"`
	Reason    string `json:"reason"`
}

type RepaymentAudit struct {
	Funds    []UnallocatedFund `json:"unallocated_funds"`
	Problems []TermProblem     `json:"term_problems"`
}

type termPayment struct {
	PlanID    int64
	Number    int
	Principal decimal.Decimal
	Fee       decimal.Decimal
	FutureFee decimal.Decimal
	Payments  map[int64]decimal.Decimal
}

type RepaymentState struct {
	Terms           map[int64]termPayment
	Funds           []UnallocatedFund
	OperationID     int64
	ManagedPayments map[int64]bool
}

// RepaymentSnapshot only reads persisted sources and allocations. In particular,
// it never reallocates historical funds while checking whether a cutover is safe.
func RepaymentSnapshot(db *gorm.DB) (RepaymentState, error) {
	out := RepaymentState{Terms: map[int64]termPayment{}, Funds: []UnallocatedFund{}, ManagedPayments: map[int64]bool{}}
	if !db.Migrator().HasTable(&model.LiabilityCreditEvent{}) {
		return out, nil
	}
	var accounts []model.Account
	if err := db.Where("billing_enabled = ?", true).Find(&accounts).Error; err != nil {
		return out, err
	}
	details, err := loadDetails(db, accounts, time.Now(), false)
	if err != nil {
		return out, err
	}
	if err := db.Model(&model.LiabilityOperation{}).Select("COALESCE(MAX(id), 0)").Scan(&out.OperationID).Error; err != nil {
		return out, err
	}
	operationIDs := []int64{}
	for _, d := range details {
		for _, source := range d.Transactions {
			if source.LiabilityOperationID != nil {
				operationIDs = append(operationIDs, *source.LiabilityOperationID)
			}
		}
	}
	managed := map[int64]bool{}
	if len(operationIDs) > 0 {
		var operations []model.LiabilityOperation
		if err := db.Select("id, kind").Where("id IN ?", operationIDs).Find(&operations).Error; err != nil {
			return out, err
		}
		for _, operation := range operations {
			managed[operation.ID] = operation.Kind == "installment_repayment" || operation.Kind == "settle" || operation.Kind == "repayment" || operation.Kind == "allocation"
		}
	}
	for _, account := range accounts {
		d := details[account.ID]
		for _, operation := range d.Operations {
			if operation.ID > out.OperationID {
				out.OperationID = operation.ID
			}
		}
		transactions := map[int64]model.Transaction{}
		credits := map[int64]model.LiabilityCreditEvent{}
		items := map[int64]model.LiabilityDebtItem{}
		for _, t := range d.Transactions {
			transactions[t.ID] = t
		}
		for _, c := range d.Credits {
			credits[c.ID] = c
			if source := transactions[c.SourceID]; source.LiabilityOperationID != nil {
				out.ManagedPayments[c.ID] = managed[*source.LiabilityOperationID]
			}
			if !c.Remaining.IsZero() {
				out.Funds = append(out.Funds, UnallocatedFund{AccountID: account.ID, SourceID: c.SourceID, Amount: c.Remaining})
			}
		}
		for _, p := range d.Plans {
			for _, t := range p.Terms {
				x := termPayment{PlanID: p.ID, Number: t.Number, Principal: t.Remaining, Payments: map[int64]decimal.Decimal{}}
				if t.FeeTxnID == nil && t.FeeStatus != "cancelled" {
					x.FutureFee = t.FeeAmount
				}
				out.Terms[t.ID] = x
			}
		}
		for _, item := range d.Items {
			items[item.ID] = item
			if item.TermID != nil && item.Kind == "fee" {
				x, exists := out.Terms[*item.TermID]
				if !exists {
					return out, fmt.Errorf("%w：费用期次关联缺失，请核对原记录", ErrInvariant)
				}
				x.Fee = x.Fee.Add(item.Remaining)
				out.Terms[*item.TermID] = x
			}
		}
		for _, allocation := range d.Allocations {
			item := items[allocation.DebtItemID]
			if item.TermID == nil || transactions[credits[allocation.CreditEventID].SourceID].Kind == model.TxnRefund {
				continue
			}
			x, exists := out.Terms[*item.TermID]
			if !exists {
				return out, fmt.Errorf("%w：偿付期次关联缺失，请核对原记录", ErrInvariant)
			}
			x.Payments[allocation.CreditEventID] = x.Payments[allocation.CreditEventID].Add(allocation.Amount)
			out.Terms[*item.TermID] = x
		}
	}
	sort.Slice(out.Funds, func(i, j int) bool {
		if out.Funds[i].AccountID != out.Funds[j].AccountID {
			return out.Funds[i].AccountID < out.Funds[j].AccountID
		}
		return out.Funds[i].SourceID < out.Funds[j].SourceID
	})
	return out, nil
}

func AuditRepayments(db *gorm.DB) (RepaymentAudit, error) {
	out := RepaymentAudit{Funds: []UnallocatedFund{}, Problems: []TermProblem{}}
	state, err := RepaymentSnapshot(db)
	if err != nil {
		return out, err
	}
	out.Funds = state.Funds
	var plans []model.InstallmentPlan
	if !db.Migrator().HasTable(&model.InstallmentPlan{}) {
		return out, nil
	}
	if err := db.Order("id").Find(&plans).Error; err != nil {
		return out, err
	}
	for _, p := range plans {
		if err := db.Where("plan_id = ?", p.ID).Order("number").Find(&p.Terms).Error; err != nil {
			return out, err
		}
		for _, t := range p.Terms {
			x, ok := state.Terms[t.ID]
			if !ok {
				continue
			}
			reason := ""
			if x.Principal.IsZero() && x.Fee.Add(x.FutureFee).IsPositive() {
				reason = "本金已清但手续费未付，请核对原记录"
			} else if t.FeeStatus == "blocked" {
				reason = t.BlockedReason
			} else if p.Status == "needs_reconcile" {
				reason = "退款后的期次尚未核对"
			}
			if reason != "" {
				out.Problems = append(out.Problems, TermProblem{AccountID: p.AccountID, PlanID: p.ID, TermID: t.ID, Number: t.Number, Reason: reason})
			}
		}
	}
	return out, nil
}

func CheckRepaymentCutover(db *gorm.DB) error {
	state, err := RepaymentSnapshot(db)
	if err != nil {
		return err
	}
	if len(state.Funds) > 0 {
		return fmt.Errorf("%w：存在历史未分配资金，请运行 liability-audit 并按真实资金核对后再切换", ErrConflict)
	}
	return nil
}

// ValidateRepaymentWrite is called at the outer financial transaction boundary,
// after every fee, transfer and target has been persisted as one operation.
func ValidateRepaymentWrite(db *gorm.DB, before RepaymentState) error {
	after, err := RepaymentSnapshot(db)
	if err != nil {
		return err
	}
	if len(after.Funds) > 0 {
		return fmt.Errorf("%w：资金必须当次完整分配，不支持超额还款、转入或留下余款", ErrConflict)
	}
	restored := map[int64]bool{}
	var reversals []model.LiabilityOperation
	if db.Migrator().HasTable(&model.LiabilityOperation{}) {
		if err := db.Where("id > ? AND kind = ?", before.OperationID, "reverse").Find(&reversals).Error; err != nil {
			return err
		}
		for _, reversal := range reversals {
			var result struct {
				TermIDs []int64 `json:"term_ids"`
			}
			if err := json.Unmarshal([]byte(reversal.ResultJSON), &result); err != nil {
				return err
			}
			for _, id := range result.TermIDs {
				restored[id] = true
			}
		}
	}
	selected := map[int64]map[int]bool{}
	for id, term := range after.Terms {
		old := before.Terms[id]
		oldPaid, paid := decimal.Zero, decimal.Zero
		for _, amount := range old.Payments {
			oldPaid = oldPaid.Add(amount)
		}
		for _, amount := range term.Payments {
			paid = paid.Add(amount)
		}
		if paid.LessThan(oldPaid) && paid.IsPositive() && term.Principal.Add(term.Fee).Add(term.FutureFee).IsPositive() && !restored[id] {
			return fmt.Errorf("%w：不能通过修改或删除留下分期部分偿付，请使用专门撤销入口", ErrConflict)
		}
		increased := false
		for credit, amount := range term.Payments {
			if amount.GreaterThan(old.Payments[credit]) {
				if !after.ManagedPayments[credit] {
					return fmt.Errorf("%w：分期偿付须经过完整期次核对，请从还款入口操作", ErrConflict)
				}
				increased = true
				break
			}
		}
		if !increased {
			continue
		}
		if term.Principal.Add(term.Fee).Add(term.FutureFee).IsPositive() {
			return fmt.Errorf("%w：分期必须连同未付手续费整期还清，请从分期还款入口操作", ErrConflict)
		}
		if old.Principal.Add(old.Fee).Add(old.FutureFee).IsPositive() {
			if old.Principal.IsZero() {
				return fmt.Errorf("%w：不支持单独还手续费，请核对该期记录", ErrConflict)
			}
			if selected[term.PlanID] == nil {
				selected[term.PlanID] = map[int]bool{}
			}
			selected[term.PlanID][term.Number] = true
		}
	}
	for plan, numbers := range selected {
		pending := []termPayment{}
		for _, term := range before.Terms {
			if term.PlanID == plan && term.Principal.Add(term.Fee).Add(term.FutureFee).IsPositive() {
				pending = append(pending, term)
			}
		}
		sort.Slice(pending, func(i, j int) bool { return pending[i].Number < pending[j].Number })
		gap := false
		for _, term := range pending {
			if !numbers[term.Number] {
				gap = true
			} else if gap {
				return fmt.Errorf("%w：请从最早未还期开始连续还款", ErrConflict)
			}
		}
	}
	return nil
}

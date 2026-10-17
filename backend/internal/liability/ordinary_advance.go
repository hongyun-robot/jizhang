package liability

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/shopspring/decimal"

	"jizhang/internal/model"
)

const OrdinaryAdvance = "ordinary_advance"

// OrdinaryPrincipal excludes non-consumption funding and every managed fee/term.
// It is also used after issuance: issuance does not invalidate an earlier payment.
func OrdinaryPrincipal(item model.LiabilityDebtItem, source model.Transaction) bool {
	effect, _ := source.Effects()
	return item.Active && item.Kind == "principal" && item.PlanID == nil && item.TermID == nil &&
		item.SourceTxnID != nil && *item.SourceTxnID == source.ID && source.ID == item.SourceID &&
		source.AccountID == item.AccountID && source.Kind == model.TxnExpense && effect.IsNegative() &&
		source.InstallmentPlanID == nil && source.LiabilityTermID == nil && source.LiabilityOperationID == nil
}

type AdvanceAllocation struct {
	StatementID int64           `json:"statement_id"`
	DebtItemID  int64           `json:"debt_item_id"`
	SourceID    int64           `json:"source_id"`
	Amount      decimal.Decimal `json:"amount"`
}

// OrdinaryCandidates reads already materialized debt; it never creates a bill.
func OrdinaryCandidates(d *Detail, date string) []model.LiabilityDebtItem {
	bills := map[int64]model.LiabilityStatement{}
	sources := map[int64]model.Transaction{}
	for _, b := range d.Statements {
		bills[b.ID] = b
	}
	for _, t := range d.Transactions {
		sources[t.ID] = t
	}
	items := []model.LiabilityDebtItem{}
	for _, item := range d.Items {
		b, ok := bills[item.StatementID]
		t := sources[item.SourceID]
		if ok && b.AccountID == d.Account.ID && b.Kind == "monthly" && b.IssuedAt == nil &&
			item.Remaining.IsPositive() && OrdinaryPrincipal(item, t) && Date(t.EntryDate) <= date {
			items = append(items, item)
		}
	}
	sort.Slice(items, func(i, j int) bool {
		a, b := bills[items[i].StatementID], bills[items[j].StatementID]
		if a.DueDate != b.DueDate {
			return a.DueDate < b.DueDate
		}
		if a.CycleEnd != b.CycleEnd {
			return a.CycleEnd < b.CycleEnd
		}
		if a.ID != b.ID {
			return a.ID < b.ID
		}
		x, y := sources[items[i].SourceID], sources[items[j].SourceID]
		if Date(x.EntryDate) != Date(y.EntryDate) {
			return Date(x.EntryDate) < Date(y.EntryDate)
		}
		if x.ID != y.ID {
			return x.ID < y.ID
		}
		return items[i].ID < items[j].ID
	})
	return items
}

func AdvanceConflict() error {
	return fmt.Errorf("%w：普通提前还款关联消费已变化，请先核对或撤销对应提前还款", ErrConflict)
}

// ValidateAdvanceAllocations guards even writes that don't call Reallocate,
// including conversion of a paid ordinary purchase into installments.
func ValidateAdvanceAllocations(d *Detail) error {
	items := map[int64]model.LiabilityDebtItem{}
	sources := map[int64]model.Transaction{}
	for _, item := range d.Items {
		items[item.ID] = item
	}
	for _, t := range d.Transactions {
		sources[t.ID] = t
	}
	for _, c := range d.Credits {
		if c.Mode != OrdinaryAdvance {
			continue
		}
		var targets []Target
		if err := json.Unmarshal([]byte(c.Targets), &targets); err != nil {
			return AdvanceConflict()
		}
		expected := map[int64]decimal.Decimal{}
		sum := decimal.Zero
		for _, target := range targets {
			item := items[target.DebtItemID]
			if target.StatementID != 0 || !ValidAmount(target.Amount, false) ||
				!OrdinaryPrincipal(item, sources[item.SourceID]) || Date(sources[item.SourceID].EntryDate) > c.EntryDate {
				return AdvanceConflict()
			}
			expected[item.ID] = expected[item.ID].Add(target.Amount)
			sum = sum.Add(target.Amount)
		}
		if !sum.Equal(c.Amount) {
			return AdvanceConflict()
		}
		actual := map[int64]decimal.Decimal{}
		for _, a := range d.Allocations {
			if a.CreditEventID == c.ID {
				actual[a.DebtItemID] = actual[a.DebtItemID].Add(a.Amount)
			}
		}
		if len(actual) != len(expected) {
			return AdvanceConflict()
		}
		for id, amount := range expected {
			if !actual[id].Equal(amount) || items[id].Remaining.IsNegative() {
				return AdvanceConflict()
			}
		}
	}
	return nil
}

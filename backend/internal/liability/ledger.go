package liability

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"jizhang/internal/model"
	"sort"
	"time"
)

type Target struct {
	StatementID int64           `json:"statement_id"`
	DebtItemID  int64           `json:"debt_item_id,omitempty"`
	Amount      decimal.Decimal `json:"amount"`
}

func RuleForDate(db *gorm.DB, account int64, date string) (model.LiabilityBillRule, error) {
	var rule model.LiabilityBillRule
	e := db.Where("account_id = ? AND effective_from <= ?", account, date).Order("effective_from DESC, version DESC").First(&rule).Error
	if errors.Is(e, gorm.ErrRecordNotFound) {
		e = ErrSetup
	}
	return rule, e
}
func StatementFor(db *gorm.DB, account int64, date string) (model.LiabilityStatement, error) {
	var bill model.LiabilityStatement
	rule, e := RuleForDate(db, account, date)
	if e != nil {
		return bill, e
	}
	start, end, due, e := Range(rule, date)
	if e != nil {
		return bill, e
	}
	key := fmt.Sprintf("monthly:%d:%s", account, end)
	e = db.Where("bill_key = ?", key).First(&bill).Error
	if errors.Is(e, gorm.ErrRecordNotFound) {
		bill = model.LiabilityStatement{AccountID: account, RuleID: rule.ID, Kind: "monthly", BillKey: key, CycleStart: start, CycleEnd: end, DueDate: due}
		e = db.Create(&bill).Error
	}
	return bill, e
}

// RecordChange runs inside the financial transaction. No after-commit accounting.
func RecordChange(db *gorm.DB, before, after *model.Transaction, actors ...int64) error {
	ids := map[int64]bool{}
	for _, t := range []*model.Transaction{before, after} {
		if t != nil {
			ids[t.AccountID] = true
			if t.TransferAccountID != nil {
				ids[*t.TransferAccountID] = true
			}
		}
	}
	for id := range ids {
		var a model.Account
		if e := db.First(&a, id).Error; e != nil {
			return e
		}
		if !a.BillingEnabled {
			continue
		}
		effect := decimal.Zero
		if after != nil {
			if after.AccountID == id {
				effect, _ = after.Effects()
			}
			if after.Kind == model.TxnTransfer && after.TransferAccountID != nil && *after.TransferAccountID == id {
				effect = effect.Add(after.Amount)
			}
		}
		source := before
		if after != nil {
			source = after
		}
		key := fmt.Sprintf("txn:%d:%d", source.ID, id)
		var item model.LiabilityDebtItem
		e := db.Where("item_key = ?", key).First(&item).Error
		if e != nil && !errors.Is(e, gorm.ErrRecordNotFound) {
			return e
		}
		managedPrincipal := after != nil && after.InstallmentPlanID != nil && after.AccountID == id && effect.IsNegative()
		if !managedPrincipal {
			if effect.IsNegative() {
				bill, e := StatementFor(db, id, Date(after.EntryDate))
				if e != nil {
					return e
				}
				if item.ID == 0 {
					item = model.LiabilityDebtItem{AccountID: id, SourceID: source.ID, ItemKey: key, Kind: "principal", StatementID: bill.ID}
				}
				// Manual bill assignment survives changes to the amount, but not to source date/account.
				if !item.ManualAssignment || before == nil || Date(before.EntryDate) != Date(after.EntryDate) || before.AccountID != after.AccountID {
					item.StatementID = bill.ID
					item.ManualAssignment = false
				}
				item.SourceTxnID = &after.ID
				item.Amount = effect.Abs()
				item.Active = true
				if after.LiabilityTermID != nil {
					item.Kind = "fee"
					item.TermID = after.LiabilityTermID
				}
				if after.LiabilityOperationID != nil && after.Kind == model.TxnExpense {
					var op model.LiabilityOperation
					if e := db.First(&op, *after.LiabilityOperationID).Error; e != nil {
						return e
					}
					if op.PlanID != nil {
						item.PlanID = op.PlanID
						item.Kind = "fee"
					}
				}
				if e := db.Save(&item).Error; e != nil {
					return e
				}
				if e := db.Model(&model.LiabilityStatement{}).Where("id = ?", item.StatementID).Update("reconciled_at", nil).Error; e != nil {
					return e
				}
			} else if item.ID != 0 {
				if e := db.Model(&item).Updates(map[string]any{"active": false, "source_txn_id": nil}).Error; e != nil {
					return e
				}
			}
		}
		var credit model.LiabilityCreditEvent
		e = db.Where("account_id = ? AND source_id = ?", id, source.ID).First(&credit).Error
		if e != nil && !errors.Is(e, gorm.ErrRecordNotFound) {
			return e
		}
		if effect.IsPositive() {
			if credit.ID == 0 {
				credit = model.LiabilityCreditEvent{AccountID: id, SourceID: source.ID, Mode: "auto", Targets: "[]"}
			}
			credit.SourceTxnID = &after.ID
			credit.Amount = effect
			credit.EntryDate = Date(after.EntryDate)
			credit.Active = true
			credit.RelatedSourceID = 0
			if after.RelatedTxnID != nil {
				credit.RelatedSourceID = *after.RelatedTxnID
			}
			if e := db.Save(&credit).Error; e != nil {
				return e
			}
		} else if credit.ID != 0 {
			if e := db.Model(&credit).Updates(map[string]any{"active": false, "source_txn_id": nil}).Error; e != nil {
				return e
			}
		}
		refundSources := map[int64]bool{}
		for _, posting := range []*model.Transaction{before, after} {
			if posting != nil && posting.AccountID == id && posting.Kind == model.TxnRefund && posting.RelatedTxnID != nil {
				refundSources[*posting.RelatedTxnID] = true
			}
		}
		for sourceID := range refundSources {
			if e := db.Model(&model.InstallmentPlan{}).Where("source_txn_id = ? AND account_id = ?", sourceID, id).Updates(map[string]any{"status": gorm.Expr("CASE WHEN status = 'settled' THEN 'settled' ELSE 'needs_reconcile' END"), "version": gorm.Expr("version + 1")}).Error; e != nil {
				return e
			}
		}
		if e := Reallocate(db, id); e != nil {
			return e
		}
		if e := CheckInvariant(db, a); e != nil {
			return e
		}
		actor := source.CreatedBy
		if len(actors) > 0 && actors[0] > 0 {
			actor = actors[0]
		}
		reason := "流水入账"
		if before != nil {
			reason = "流水修改"
		}
		if after == nil {
			reason = "流水删除"
		}
		op := model.LiabilityOperation{ActorID: actor, RequestKey: fmt.Sprintf("posting:%d:%d:%d", source.ID, id, time.Now().UnixNano()), Kind: "posting", AccountID: id, Reason: reason, BeforeJSON: stringJSON(before), ResultJSON: stringJSON(after)}
		if e := db.Create(&op).Error; e != nil {
			return e
		}
		if e := RefreshReminders(db, time.Now(), false); e != nil {
			return e
		}
	}
	return nil
}

func stringJSON(v any) string { raw, _ := json.Marshal(v); return string(raw) }

func FillAmounts(db *gorm.DB, items []model.LiabilityDebtItem, credits []model.LiabilityCreditEvent) ([]model.LiabilityAllocation, error) {
	debtIds := make([]int64, 0, len(items))
	for i := range items {
		items[i].Remaining = items[i].Amount
		debtIds = append(debtIds, items[i].ID)
	}
	creditIds := make([]int64, 0, len(credits))
	for i := range credits {
		credits[i].Remaining = credits[i].Amount
		creditIds = append(creditIds, credits[i].ID)
	}
	var all []model.LiabilityAllocation
	if len(debtIds) == 0 && len(creditIds) == 0 {
		return all, nil
	}
	if e := db.Where("active = ? AND (debt_item_id IN ? OR credit_event_id IN ?)", true, debtIds, creditIds).Find(&all).Error; e != nil {
		return nil, e
	}
	dm := map[int64]int{}
	cm := map[int64]int{}
	for i, x := range items {
		dm[x.ID] = i
	}
	for i, x := range credits {
		cm[x.ID] = i
	}
	for _, x := range all {
		if i, ok := dm[x.DebtItemID]; ok {
			items[i].Remaining = items[i].Remaining.Sub(x.Amount)
		}
		if i, ok := cm[x.CreditEventID]; ok {
			credits[i].Remaining = credits[i].Remaining.Sub(x.Amount)
		}
	}
	return all, nil
}

// Recompute only changed allocations; durable manual targets are never replaced by FIFO.
func Reallocate(db *gorm.DB, account int64) error {
	var items []model.LiabilityDebtItem
	var credits []model.LiabilityCreditEvent
	var bills []model.LiabilityStatement
	if e := db.Where("account_id = ? AND active = ?", account, true).Find(&items).Error; e != nil {
		return e
	}
	if e := db.Where("account_id = ?", account).Order("entry_date, id").Find(&credits).Error; e != nil {
		return e
	}
	// Reserve a settlement's explicit item targets before automatic FIFO.
	// This also repairs older allocations clipped after a bill move, using
	// the same recorded funds and targets without changing any transaction.
	sort.SliceStable(credits, func(i, j int) bool {
		return credits[i].Mode == "settlement" && credits[j].Mode != "settlement"
	})
	if e := db.Where("account_id = ?", account).Find(&bills).Error; e != nil {
		return e
	}
	bm := map[int64]model.LiabilityStatement{}
	for _, b := range bills {
		bm[b.ID] = b
	}
	sort.SliceStable(items, func(i, j int) bool {
		a, b := bm[items[i].StatementID], bm[items[j].StatementID]
		if a.DueDate != b.DueDate {
			return a.DueDate < b.DueDate
		}
		return items[i].ID < items[j].ID
	})
	remaining := map[int64]decimal.Decimal{}
	for _, d := range items {
		remaining[d.ID] = d.Amount
	}
	desired := map[int64][]model.LiabilityAllocation{}
	for _, c := range credits {
		if !c.Active {
			continue
		}
		left := c.Amount
		allocate := func(d model.LiabilityDebtItem, limit decimal.Decimal) {
			n := decimal.Min(left, decimal.Min(remaining[d.ID], limit))
			if !n.IsPositive() {
				return
			}
			desired[c.ID] = append(desired[c.ID], model.LiabilityAllocation{CreditEventID: c.ID, DebtItemID: d.ID, Amount: n, Active: true})
			remaining[d.ID] = remaining[d.ID].Sub(n)
			left = left.Sub(n)
		}
		if c.Mode == "manual" || c.Mode == "settlement" {
			var targets []Target
			if e := json.Unmarshal([]byte(c.Targets), &targets); e != nil {
				return e
			}
			for _, target := range targets {
				limit := target.Amount
				for _, d := range items {
					if target.DebtItemID != 0 && target.DebtItemID != d.ID {
						continue
					}
					if target.StatementID != 0 && target.StatementID != d.StatementID {
						continue
					}
					oldLeft := left
					allocate(d, limit)
					limit = limit.Sub(oldLeft.Sub(left))
					if !limit.IsPositive() {
						break
					}
				}
			}
			continue
		}
		if c.Mode == "hold" {
			continue
		}
		if c.RelatedSourceID > 0 {
			for _, d := range items {
				if d.SourceID == c.RelatedSourceID {
					allocate(d, left)
				}
			}
		}
		for _, d := range items {
			b := bm[d.StatementID]
			// Future installment payments use explicit term targets. There is no
			// unused funding that may silently pay a later purchase or bill.
			if b.IssuedAt != nil {
				allocate(d, left)
			}
		}
	}
	var old []model.LiabilityAllocation
	ids := make([]int64, 0, len(credits))
	for _, c := range credits {
		ids = append(ids, c.ID)
	}
	if len(ids) > 0 {
		if e := db.Where("credit_event_id IN ? AND active = ?", ids, true).Find(&old).Error; e != nil {
			return e
		}
	}
	prior := map[int64][]model.LiabilityAllocation{}
	for _, x := range old {
		prior[x.CreditEventID] = append(prior[x.CreditEventID], x)
	}
	signature := func(xs []model.LiabilityAllocation) string {
		sums := map[int64]decimal.Decimal{}
		for _, x := range xs {
			sums[x.DebtItemID] = sums[x.DebtItemID].Add(x.Amount)
		}
		keys := make([]int64, 0, len(sums))
		for k := range sums {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
		s := ""
		for _, k := range keys {
			s += fmt.Sprintf("%d:%s;", k, sums[k].StringFixed(2))
		}
		return s
	}
	for _, c := range credits {
		if signature(prior[c.ID]) == signature(desired[c.ID]) {
			continue
		}
		now := time.Now()
		if e := db.Model(&model.LiabilityAllocation{}).Where("credit_event_id = ? AND active = ?", c.ID, true).Updates(map[string]any{"active": false, "voided_at": now}).Error; e != nil {
			return e
		}
		if len(desired[c.ID]) > 0 {
			if e := db.Create(desired[c.ID]).Error; e != nil {
				return e
			}
		}
	}
	return nil
}

func CheckInvariant(db *gorm.DB, a model.Account) error {
	var txns []model.Transaction
	if e := db.Where("account_id = ? OR transfer_account_id = ?", a.ID, a.ID).Find(&txns).Error; e != nil {
		return e
	}
	balance := a.OpeningBalance
	for _, t := range txns {
		if t.AccountID == a.ID {
			x, _ := t.Effects()
			balance = balance.Add(x)
		}
		if t.Kind == model.TxnTransfer && t.TransferAccountID != nil && *t.TransferAccountID == a.ID {
			balance = balance.Add(t.Amount)
		}
	}
	var items []model.LiabilityDebtItem
	var credits []model.LiabilityCreditEvent
	if e := db.Where("account_id = ? AND active = ?", a.ID, true).Find(&items).Error; e != nil {
		return e
	}
	if e := db.Where("account_id = ? AND active = ?", a.ID, true).Find(&credits).Error; e != nil {
		return e
	}
	if _, e := FillAmounts(db, items, credits); e != nil {
		return e
	}
	computed := decimal.Zero
	for _, c := range credits {
		if c.Remaining.IsNegative() {
			return ErrInvariant
		}
		computed = computed.Add(c.Remaining)
	}
	for _, d := range items {
		if d.Remaining.IsNegative() {
			return ErrInvariant
		}
		computed = computed.Sub(d.Remaining)
	}
	if !balance.Round(2).Equal(computed.Round(2)) {
		return fmt.Errorf("%w: %s / %s", ErrInvariant, balance, computed)
	}
	return nil
}

func TargetsJSON(db *gorm.DB, account int64, amount decimal.Decimal, targets []Target, settlement bool) (string, error) {
	sum := decimal.Zero
	var items []model.LiabilityDebtItem
	var credits []model.LiabilityCreditEvent
	if e := db.Where("account_id = ? AND active = ?", account, true).Find(&items).Error; e != nil {
		return "", e
	}
	if _, e := FillAmounts(db, items, credits); e != nil {
		return "", e
	}
	used := map[int64]decimal.Decimal{}
	for _, x := range targets {
		if !ValidAmount(x.Amount, false) || (x.StatementID == 0 && (!settlement || x.DebtItemID == 0)) {
			return "", ErrInvalid
		}
		capacity := decimal.Zero
		for _, d := range items {
			if x.StatementID != 0 && d.StatementID != x.StatementID {
				continue
			}
			if x.DebtItemID != 0 && d.ID != x.DebtItemID {
				continue
			}
			if !settlement {
				var bill model.LiabilityStatement
				if e := db.First(&bill, d.StatementID).Error; e != nil {
					return "", e
				}
				if bill.IssuedAt == nil {
					return "", ErrInvalid
				}
			}
			capacity = capacity.Add(d.Remaining.Sub(used[d.ID]))
		}
		if x.Amount.GreaterThan(capacity) {
			return "", ErrConflict
		}
		sum = sum.Add(x.Amount)
		left := x.Amount
		for _, d := range items {
			if x.StatementID != 0 && d.StatementID != x.StatementID {
				continue
			}
			if x.DebtItemID != 0 && d.ID != x.DebtItemID {
				continue
			}
			n := decimal.Min(left, d.Remaining.Sub(used[d.ID]))
			used[d.ID] = used[d.ID].Add(n)
			left = left.Sub(n)
		}
	}
	if sum.GreaterThan(amount) {
		return "", ErrInvalid
	}
	raw, e := json.Marshal(targets)
	return string(raw), e
}

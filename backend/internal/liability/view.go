package liability

import (
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"jizhang/internal/model"
	"time"
)

type Summary struct {
	AccountID       int64           `json:"account_id"`
	Name            string          `json:"name"`
	LiabilityType   string          `json:"liability_type"`
	BillingEnabled  bool            `json:"billing_enabled"`
	Balance         decimal.Decimal `json:"balance"`
	TotalDebt       decimal.Decimal `json:"total_debt"`
	CurrentDue      decimal.Decimal `json:"current_due"`
	DueDate         string          `json:"due_date"`
	Overdue         decimal.Decimal `json:"overdue"`
	Unbilled        decimal.Decimal `json:"unbilled"`
	FuturePrincipal decimal.Decimal `json:"future_principal"`
	PendingCredit   decimal.Decimal `json:"pending_credit"`
	Surplus         decimal.Decimal `json:"surplus"`
	ForecastFees    decimal.Decimal `json:"forecast_fees"`
}
type Detail struct {
	Account      model.Account                `json:"account"`
	Summary      Summary                      `json:"summary"`
	Rules        []model.LiabilityBillRule    `json:"rules"`
	Statements   []model.LiabilityStatement   `json:"statements"`
	Items        []model.LiabilityDebtItem    `json:"items"`
	Credits      []model.LiabilityCreditEvent `json:"credits"`
	Allocations  []model.LiabilityAllocation  `json:"allocations"`
	Plans        []model.InstallmentPlan      `json:"plans"`
	Operations   []model.LiabilityOperation   `json:"operations"`
	Transactions []model.Transaction          `json:"transactions"`
}

// Load uses a fixed set of batch reads rather than queries per account or bill.
func Load(db *gorm.DB, accounts []model.Account, now time.Time) (map[int64]*Detail, error) {
	out := map[int64]*Detail{}
	ids := []int64{}
	for _, a := range accounts {
		ids = append(ids, a.ID)
		out[a.ID] = &Detail{Account: a, Summary: Summary{AccountID: a.ID, Name: a.Name, LiabilityType: a.LiabilityType, BillingEnabled: a.BillingEnabled, Balance: a.Balance, TotalDebt: decimal.Max(a.Balance.Neg(), decimal.Zero), Surplus: decimal.Max(a.Balance, decimal.Zero)}, Rules: []model.LiabilityBillRule{}, Statements: []model.LiabilityStatement{}, Items: []model.LiabilityDebtItem{}, Credits: []model.LiabilityCreditEvent{}, Allocations: []model.LiabilityAllocation{}, Plans: []model.InstallmentPlan{}, Operations: []model.LiabilityOperation{}, Transactions: []model.Transaction{}}
	}
	if len(ids) == 0 {
		return out, nil
	}
	var rules []model.LiabilityBillRule
	var bills []model.LiabilityStatement
	var items []model.LiabilityDebtItem
	var credits []model.LiabilityCreditEvent
	var plans []model.InstallmentPlan
	var ops []model.LiabilityOperation
	queries := []struct {
		value any
		order string
	}{{&rules, "effective_from, version"}, {&bills, "cycle_end DESC"}, {&items, "id"}, {&credits, "entry_date, id"}, {&plans, "id DESC"}, {&ops, "id DESC"}}
	for _, q := range queries {
		query := db.Where("account_id IN ?", ids).Order(q.order)
		if q.value == any(&ops) {
			query = query.Limit(100)
		}
		if e := query.Find(q.value).Error; e != nil {
			return nil, e
		}
	}
	for _, r := range rules {
		out[r.AccountID].Rules = append(out[r.AccountID].Rules, r)
	}
	allocations, e := FillAmounts(db, items, credits)
	if e != nil {
		return nil, e
	}
	planIds := []int64{}
	for _, p := range plans {
		planIds = append(planIds, p.ID)
	}
	terms := []model.InstallmentTerm{}
	if len(planIds) > 0 {
		if e := db.Where("plan_id IN ?", planIds).Order("number").Find(&terms).Error; e != nil {
			return nil, e
		}
	}
	sources := map[int64]bool{}
	for _, d := range items {
		sources[d.SourceID] = true
	}
	for _, c := range credits {
		sources[c.SourceID] = true
	}
	for _, t := range terms {
		if t.FeeTxnID != nil {
			sources[*t.FeeTxnID] = true
		}
	}
	sourceIds := []int64{}
	for id := range sources {
		sourceIds = append(sourceIds, id)
	}
	txns := []model.Transaction{}
	opIds := []int64{}
	for _, op := range ops {
		opIds = append(opIds, op.ID)
	}
	if len(sourceIds) > 0 || len(opIds) > 0 {
		if e := db.Where("id IN ? OR liability_operation_id IN ?", sourceIds, opIds).Find(&txns).Error; e != nil {
			return nil, e
		}
	}
	parentIDs := make([]int64, 0, len(txns))
	for _, t := range txns {
		parentIDs = append(parentIDs, t.ID)
	}
	var refunds []model.Transaction
	if len(parentIDs) > 0 {
		if e := db.Where("kind = ? AND related_txn_id IN ?", model.TxnRefund, parentIDs).Find(&refunds).Error; e != nil {
			return nil, e
		}
	}
	refunded := map[int64]decimal.Decimal{}
	for _, r := range refunds {
		if r.RelatedTxnID != nil {
			refunded[*r.RelatedTxnID] = refunded[*r.RelatedTxnID].Add(r.Amount)
		}
	}
	for i := range txns {
		txns[i].RefundedAmount = refunded[txns[i].ID]
	}
	present := map[int64]bool{}
	for _, t := range txns {
		present[t.ID] = true
	}
	for _, r := range refunds {
		if !present[r.ID] {
			txns = append(txns, r)
		}
	}
	tm := map[int64]model.Transaction{}
	for _, t := range txns {
		t.FillEffects()
		tm[t.ID] = t
	}
	bm := map[int64]*model.LiabilityStatement{}
	for i := range bills {
		bm[bills[i].ID] = &bills[i]
	}
	dm := map[int64]model.LiabilityDebtItem{}
	cm := map[int64]model.LiabilityCreditEvent{}
	for _, d := range items {
		dm[d.ID] = d
		if !d.Active {
			continue
		}
		profile := out[d.AccountID]
		profile.Items = append(profile.Items, d)
		b := bm[d.StatementID]
		if b == nil {
			continue
		}
		b.Amount = b.Amount.Add(d.Amount)
		b.Remaining = b.Remaining.Add(d.Remaining)
		if b.IssuedAt == nil {
			profile.Summary.Unbilled = profile.Summary.Unbilled.Add(d.Remaining)
			if d.PlanID != nil && d.Kind == "principal" {
				profile.Summary.FuturePrincipal = profile.Summary.FuturePrincipal.Add(d.Remaining)
			}
		}
	}
	for _, c := range credits {
		cm[c.ID] = c
		if c.Active {
			out[c.AccountID].Credits = append(out[c.AccountID].Credits, c)
			out[c.AccountID].Summary.PendingCredit = out[c.AccountID].Summary.PendingCredit.Add(c.Remaining)
		}
	}
	for _, x := range allocations {
		d := dm[x.DebtItemID]
		c := cm[x.CreditEventID]
		if !d.Active || !c.Active {
			continue
		}
		b := bm[d.StatementID]
		if b == nil {
			continue
		}
		if tm[c.SourceID].Kind == model.TxnRefund {
			b.Amount = b.Amount.Sub(x.Amount)
		} else {
			b.PaidAmount = b.PaidAmount.Add(x.Amount)
		}
		out[d.AccountID].Allocations = append(out[d.AccountID].Allocations, x)
	}
	today := Date(now)
	for _, b := range bills {
		profile := out[b.AccountID]
		profile.Statements = append(profile.Statements, b)
		if b.IssuedAt == nil || !b.Remaining.IsPositive() {
			continue
		}
		if b.DueDate < today {
			profile.Summary.Overdue = profile.Summary.Overdue.Add(b.Remaining)
			continue
		}
		if profile.Summary.DueDate == "" || b.DueDate < profile.Summary.DueDate {
			profile.Summary.DueDate = b.DueDate
			profile.Summary.CurrentDue = b.Remaining
		} else if b.DueDate == profile.Summary.DueDate {
			profile.Summary.CurrentDue = profile.Summary.CurrentDue.Add(b.Remaining)
		}
	}
	termMap := map[int64][]model.InstallmentTerm{}
	for _, t := range terms {
		for _, d := range items {
			if d.Active && d.TermID != nil && *d.TermID == t.ID && d.Kind == "principal" {
				t.Remaining = t.Remaining.Add(d.Remaining)
			}
		}
		termMap[t.PlanID] = append(termMap[t.PlanID], t)
	}
	for _, p := range plans {
		p.Terms = termMap[p.ID]
		if p.Terms == nil {
			p.Terms = []model.InstallmentTerm{}
		}
		out[p.AccountID].Plans = append(out[p.AccountID].Plans, p)
		for _, term := range p.Terms {
			if term.FeeTxnID == nil && term.FeeStatus != "cancelled" {
				out[p.AccountID].Summary.ForecastFees = out[p.AccountID].Summary.ForecastFees.Add(term.FeeAmount)
			}
		}
	}
	for _, op := range ops {
		out[op.AccountID].Operations = append(out[op.AccountID].Operations, op)
	}
	for _, profile := range out {
		set := map[int64]bool{}
		for _, d := range profile.Items {
			set[d.SourceID] = true
		}
		for _, c := range profile.Credits {
			set[c.SourceID] = true
		}
		for _, p := range profile.Plans {
			set[p.SourceTxnID] = true
			for _, term := range p.Terms {
				if term.FeeTxnID != nil {
					set[*term.FeeTxnID] = true
				}
			}
		}
		for _, t := range txns {
			linked := false
			if t.LiabilityOperationID != nil {
				for _, op := range profile.Operations {
					if op.ID == *t.LiabilityOperationID {
						linked = true
					}
				}
			}
			if set[t.ID] || linked || (t.RelatedTxnID != nil && set[*t.RelatedTxnID]) {
				t.LiabilityAccountID = &profile.Account.ID
				profile.Transactions = append(profile.Transactions, t)
			}
		}
	}
	return out, nil
}

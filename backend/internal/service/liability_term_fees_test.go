package service

import (
	"testing"

	"github.com/google/uuid"
	"jizhang/internal/model"
)

func TestLiabilityTermFeesRepaidWithPrincipal(t *testing.T) {
	e := newLiabilityEnv(t)
	mustOK(t, e.db.Model(&model.Account{}).Where("id = ?", e.card).Update("principal_remainder", "first").Error)
	r, err := e.svcL.CreatePlan(adminUser(), InstallmentInput{AccountID: e.card, Amount: dec("998.33"), EntryDate: "2026-10-15", CategoryID: e.category, TermCount: 3, FeeMode: "per_term", FeeInput: dec("0.08"), FeeAccountID: e.card, FeeCategoryID: e.category, RequestKey: uuid.NewString()})
	mustOK(t, err)
	d := e.state(t)
	if !d.Summary.TotalDebt.Equal(dec("998.33")) || !d.Summary.ForecastFees.Equal(dec("0.24")) {
		t.Fatal("future fees must stay forecasts", d.Summary)
	}
	for i, term := range []struct {
		date, payment, remaining string
	}{
		{"2026-11-01", "332.87", "665.54"},
		{"2026-12-01", "332.85", "332.77"},
		{"2027-01-01", "332.85", "0"},
	} {
		e.now = vdate(t, term.date)
		_, err = e.svcL.EnsureDue(e.now)
		mustOK(t, err)
		d = e.state(t)
		if !d.Summary.CurrentDue.Equal(dec(term.payment)) {
			t.Fatalf("term %d must bill principal plus 0.08 fee: %v", i+1, d.Summary)
		}
		p, err := e.svcL.Plan(adminUser(), r.PlanID)
		mustOK(t, err)
		feeFound := false
		for _, item := range d.Items {
			if item.TermID != nil && *item.TermID == p.Terms[i].ID && item.Kind == "fee" {
				feeFound = item.AccountID == e.card && item.StatementID == p.Terms[i].StatementID && item.Amount.Equal(dec("0.08"))
			}
		}
		if !feeFound {
			t.Fatal("fee is not in the principal's liability bill")
		}
		e.pay(t, term.payment, term.date)
		d = e.state(t)
		if !d.Summary.CurrentDue.IsZero() || !d.Summary.TotalDebt.Equal(dec(term.remaining)) {
			t.Fatal("one transfer must pay principal and fee together", d.Summary)
		}
		for _, item := range d.Items {
			if item.TermID != nil && *item.TermID == p.Terms[i].ID && !item.Remaining.IsZero() {
				t.Fatal("fee or principal was left unpaid", item)
			}
		}
	}
	expenses := dec("0")
	feeCount := 0
	for _, txn := range d.Transactions {
		if txn.Kind == model.TxnExpense && txn.AccountID == e.card {
			expenses = expenses.Add(txn.Amount)
			if txn.LiabilityTermID != nil {
				feeCount++
			}
		}
	}
	if feeCount != 3 || !expenses.Equal(dec("998.57")) || !d.Summary.ForecastFees.IsZero() {
		t.Fatal("principal or fee was counted twice", feeCount, expenses, d.Summary)
	}
}

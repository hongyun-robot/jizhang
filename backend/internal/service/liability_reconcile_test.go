package service

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"jizhang/internal/liability"
	"jizhang/internal/model"
)

func TestLiabilityReconcileMovedExpenseChecksAdjustedAmount(t *testing.T) {
	e := newLiabilityEnv(t)
	_, err := e.svcL.Settings(adminUser(), e.card, LiabilitySettingsInput{LiabilityType: "huabei", StatementDay: 1, RepaymentDay: 8, RepaymentMonthOffset: 0})
	mustOK(t, err)
	e.now = vdate(t, "2026-11-05")
	_, err = e.svcL.EnsureDue(e.now)
	mustOK(t, err)
	source := e.spend(t, "3", "2026-11-01")
	future, err := liability.StatementFor(e.db, e.card, "2026-11-02")
	mustOK(t, err)
	before := e.state(t)
	var item model.LiabilityDebtItem
	for _, d := range before.Items {
		if d.SourceID == source.ID {
			item = d
		}
	}
	if item.ID == 0 || item.StatementID == future.ID || future.DueDate != "2026-12-08" {
		t.Fatal("invalid reproduction setup", item, future)
	}
	ref := dec("3")
	in := LiabilityReconcileInput{RequestKey: uuid.NewString(), Reason: "11月消费调整至12月还款", ReferenceAmount: &ref, Complete: true}
	in.Moves = append(in.Moves, struct {
		ItemID      int64 `json:"item_id"`
		StatementID int64 `json:"statement_id"`
	}{ItemID: item.ID, StatementID: future.ID})
	_, err = e.svcL.Reconcile(adminUser(), item.StatementID, in)
	if !errors.Is(err, liability.ErrConflict) {
		t.Fatalf("mismatched reference accepted: %v", err)
	}
	after := e.state(t)
	if after.Items[0].StatementID != item.StatementID || len(after.Operations) != len(before.Operations) || !after.Summary.TotalDebt.Equal(before.Summary.TotalDebt) {
		t.Fatal("failed reconciliation partly committed", after)
	}
	conflict := err
	var originalBill model.LiabilityStatement
	for _, bill := range before.Statements {
		if bill.ID == item.StatementID {
			originalBill = bill
		}
	}
	ref = dec("0")
	in.RequestKey = uuid.NewString()
	result, err := e.svcL.Reconcile(adminUser(), item.StatementID, in)
	mustOK(t, err)
	again, err := e.svcL.Reconcile(adminUser(), item.StatementID, in)
	mustOK(t, err)
	if result.OperationID != again.OperationID {
		t.Fatal("retry duplicated reconciliation")
	}
	after = e.state(t)
	if after.Items[0].StatementID != future.ID || !after.Summary.CurrentDue.IsZero() || !after.Summary.Unbilled.Equal(dec("3")) || !after.Summary.TotalDebt.Equal(before.Summary.TotalDebt) {
		t.Fatal("expense was not moved intact", after)
	}
	for _, bill := range after.Statements {
		if bill.ID == item.StatementID && (!bill.Amount.IsZero() || bill.ReconciledAt == nil || !bill.SnapshotAmount.Equal(originalBill.SnapshotAmount)) {
			t.Fatal("original bill was not reconciled correctly", bill)
		}
	}
	var stored model.Transaction
	mustOK(t, e.db.First(&stored, source.ID).Error)
	if liability.Date(stored.EntryDate) != "2026-11-01" || stored.PeriodKey != source.PeriodKey || !stored.Amount.Equal(source.Amount) {
		t.Fatal("moving a bill changed the expense", stored)
	}
	t.Log("reference=3, complete=true: rejected and rolled back; reference=0, complete=true: moved to December successfully")
	if !strings.Contains(conflict.Error(), "调整后本账单金额为 ¥0.00") {
		t.Fatalf("conflict must explain the adjusted amount: %v", conflict)
	}
}

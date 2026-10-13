package service

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"jizhang/internal/liability"
	"jizhang/internal/model"
)

func TestLiabilitySettlementPreservesEarlierRepayment(t *testing.T) {
	e := newLiabilityEnv(t)
	created, err := e.svcL.CreatePlan(adminUser(), InstallmentInput{AccountID: e.card, Amount: dec("200"), EntryDate: "2026-10-15", CategoryID: e.category, TermCount: 2, FeeInput: dec("0"), RequestKey: uuid.NewString()})
	mustOK(t, err)
	e.now = vdate(t, "2026-11-01")
	_, err = e.svcL.EnsureDue(e.now)
	mustOK(t, err)
	historicalPayment(t, e, "20", "2026-11-01")
	before := e.state(t)
	plan, err := e.svcL.Plan(adminUser(), created.PlanID)
	mustOK(t, err)
	settled, err := e.svcL.Settle(adminUser(), plan.ID, settlementRequest(t, e, plan.ID))
	mustOK(t, err)
	assertPaid := func() {
		t.Helper()
		d := e.state(t)
		if !d.Summary.TotalDebt.IsZero() || !d.Summary.CurrentDue.IsZero() || !d.Summary.Overdue.IsZero() || !d.Summary.PendingCredit.IsZero() {
			t.Fatal("settlement moved an earlier payment and stranded credit", d.Summary)
		}
		for _, term := range d.Plans[0].Terms {
			if !term.Remaining.IsZero() {
				t.Fatal("settled term still appears unpaid", term)
			}
		}
	}
	assertPaid()
	if err := e.txn.Delete(adminUser(), before.Credits[0].SourceID); !errors.Is(err, liability.ErrManaged) {
		t.Fatal("settlement-linked earlier payment can be deleted directly", err)
	}
	if _, err := e.svcL.Allocate(adminUser(), before.Credits[0].SourceID, LiabilityAllocationInput{RequestKey: uuid.NewString(), Mode: "hold"}); !errors.Is(err, liability.ErrManaged) {
		t.Fatal("settlement-linked earlier payment can be reassigned directly", err)
	}
	e.now = vdate(t, "2027-01-15")
	_, err = e.svcL.EnsureDue(e.now)
	mustOK(t, err)
	assertPaid()
	_, err = e.svcL.Reverse(adminUser(), settled.OperationID, ReverseInput{RequestKey: uuid.NewString(), Reason: "核对分配后撤销结清"})
	mustOK(t, err)
	after := e.state(t)
	if !after.Summary.TotalDebt.Equal(dec("180")) || !after.Summary.PendingCredit.IsZero() || after.Credits[0].Mode != before.Credits[0].Mode || after.Credits[0].Targets != before.Credits[0].Targets {
		t.Fatal("reversal did not restore the previous payment policy", after.Summary, after.Credits)
	}
	var expenses, transfers int64
	mustOK(t, e.db.Model(&model.Transaction{}).Where("account_id = ? AND kind = ?", e.card, model.TxnExpense).Count(&expenses).Error)
	mustOK(t, e.db.Model(&model.Transaction{}).Where("transfer_account_id = ? AND kind = ?", e.card, model.TxnTransfer).Count(&transfers).Error)
	if expenses != 1 || transfers != 1 {
		t.Fatal("settlement or reversal duplicated financial postings", expenses, transfers)
	}
}

func TestLiabilityLegacySettlementTargetsTakePriorityWithoutNewMoney(t *testing.T) {
	e := newLiabilityEnv(t)
	created, err := e.svcL.CreatePlan(adminUser(), InstallmentInput{AccountID: e.card, Amount: dec("200"), EntryDate: "2026-10-15", CategoryID: e.category, TermCount: 2, FeeInput: dec("0"), RequestKey: uuid.NewString()})
	mustOK(t, err)
	e.now = vdate(t, "2026-11-01")
	_, err = e.svcL.EnsureDue(e.now)
	mustOK(t, err)
	historicalPayment(t, e, "20", "2026-11-01")
	prior := e.state(t)
	plan, err := e.svcL.Plan(adminUser(), created.PlanID)
	mustOK(t, err)
	_, err = e.svcL.Settle(adminUser(), plan.ID, settlementRequest(t, e, plan.ID))
	mustOK(t, err)
	// Pre-fix settlements left the original paid credit in automatic mode.
	mustOK(t, e.db.Model(&model.LiabilityCreditEvent{}).Where("id = ?", prior.Credits[0].ID).Updates(map[string]any{"mode": "auto", "targets": "[]"}).Error)
	var postingsBefore, postingsAfter int64
	mustOK(t, e.db.Model(&model.Transaction{}).Count(&postingsBefore).Error)
	mustOK(t, liability.Reallocate(e.db, e.card))
	d := e.state(t)
	if !d.Summary.TotalDebt.IsZero() || !d.Summary.CurrentDue.IsZero() || !d.Summary.PendingCredit.IsZero() || !d.Plans[0].Terms[0].Remaining.IsZero() {
		t.Fatal("legacy target priorities still strand recorded funds", d.Summary, d.Plans)
	}
	mustOK(t, e.db.Model(&model.Transaction{}).Count(&postingsAfter).Error)
	if postingsBefore != postingsAfter || d.Credits[0].Mode != "auto" {
		t.Fatal("repair changed original funding or added a posting", postingsBefore, postingsAfter, d.Credits)
	}
}

func TestLiabilitySettlementRejectsExcessAndHold(t *testing.T) {
	e := newLiabilityEnv(t)
	created, err := e.svcL.CreatePlan(adminUser(), InstallmentInput{AccountID: e.card, Amount: dec("200"), EntryDate: "2026-10-15", CategoryID: e.category, TermCount: 2, FeeInput: dec("0"), RequestKey: uuid.NewString()})
	mustOK(t, err)
	e.now = vdate(t, "2026-11-01")
	_, err = e.svcL.EnsureDue(e.now)
	mustOK(t, err)
	_, err = e.svcL.Repay(adminUser(), LiabilityRepaymentInput{AccountID: e.card, FromAccountID: e.account, Amount: dec("100"), EntryDate: "2026-11-01", Mode: "hold", RequestKey: uuid.NewString()})
	if !errors.Is(err, liability.ErrConflict) {
		t.Fatal("hold request accepted", err)
	}
	_, err = e.svcL.Repay(adminUser(), LiabilityRepaymentInput{AccountID: e.card, FromAccountID: e.account, Amount: dec("150"), EntryDate: "2026-11-01", RequestKey: uuid.NewString()})
	if !errors.Is(err, liability.ErrConflict) {
		t.Fatal("excess request accepted", err)
	}
	e.pay(t, "100", "2026-11-01")
	plan, err := e.svcL.Plan(adminUser(), created.PlanID)
	mustOK(t, err)
	_, err = e.svcL.Settle(adminUser(), plan.ID, settlementRequest(t, e, plan.ID))
	mustOK(t, err)
	d := e.state(t)
	if !d.Summary.PendingCredit.IsZero() || !d.Summary.TotalDebt.IsZero() {
		t.Fatal("settlement left unused funds", d.Summary, d.Credits)
	}
	e.spend(t, "20", "2026-11-02")
	d = e.state(t)
	if !d.Summary.PendingCredit.IsZero() || !d.Summary.TotalDebt.Equal(dec("20")) || !d.Summary.Unbilled.Equal(dec("20")) {
		t.Fatal("new expense was incorrectly prepaid", d.Summary)
	}
}

func TestLiabilitySettlementPinningChecksPaymentActorAndRollsBack(t *testing.T) {
	e := newLiabilityEnv(t)
	created, err := e.svcL.CreatePlan(adminUser(), InstallmentInput{AccountID: e.card, Amount: dec("200"), EntryDate: "2026-10-15", CategoryID: e.category, TermCount: 2, FeeInput: dec("0"), RequestKey: uuid.NewString()})
	mustOK(t, err)
	e.now = vdate(t, "2026-11-01")
	_, err = e.svcL.EnsureDue(e.now)
	mustOK(t, err)
	historicalPayment(t, e, "20", "2026-11-01")
	// Legacy shared-account history may have a payment created by another user.
	mustOK(t, e.db.Model(&model.Transaction{}).Where("kind = ? AND transfer_account_id = ?", model.TxnTransfer, e.card).Update("created_by", 3).Error)
	plan, err := e.svcL.Plan(adminUser(), created.PlanID)
	mustOK(t, err)
	actor := &model.User{ID: 1, Role: model.RoleMember}
	_, err = e.svcL.Settle(actor, plan.ID, settlementRequest(t, e, plan.ID))
	if !errors.Is(err, ErrTxnForbidden) {
		t.Fatal("member changed another creator's payment allocation", err)
	}
	d := e.state(t)
	if !d.Summary.TotalDebt.Equal(dec("180")) || d.Plans[0].Status != "active" || len(d.Transactions) != 2 || d.Credits[0].Mode != "auto" {
		t.Fatal("forbidden pinning left a partial settlement", d.Summary, d.Transactions, d.Credits)
	}
}

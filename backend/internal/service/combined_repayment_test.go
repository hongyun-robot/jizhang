package service

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"jizhang/internal/liability"
	"jizhang/internal/model"
)

func TestCombinedRepaymentSecondTermAndOrdinary(t *testing.T) {
	e := newLiabilityEnv(t)
	id := repaymentPlan(t, e)
	first, _ := quotedRepayment(t, e, id, 1)
	_, err := e.svcL.RepayInstallment(adminUser(), id, first)
	mustOK(t, err)
	e.spend(t, "8.28", "2026-10-15")
	in := advanceInput(e, "1028.28", "2026-10-15")
	in.Scope = "all"
	before := e.state(t)
	q, err := e.svcL.OrdinaryRepaymentPreview(adminUser(), in)
	mustOK(t, err)
	if !q.Amount.Equal(dec("1028.28")) || !reflect.DeepEqual(before, e.state(t)) {
		t.Fatal("combined preview must quote the payment without moving money", q)
	}
	in.QuoteSignature = q.QuoteSignature
	r, err := e.svcL.Repay(adminUser(), in)
	mustOK(t, err)
	d := e.state(t)
	if !d.Summary.TotalDebt.Equal(dec("1000")) || d.Plans[0].Terms[1].Status != "settled" || d.Plans[0].Terms[2].Status != "scheduled" {
		t.Fatal("ordinary consumption and second term must be repaid together", d.Summary, d.Plans)
	}
	transfers := 0
	for _, transaction := range d.Transactions {
		if transaction.Kind == model.TxnTransfer {
			transfers++
		}
	}
	if transfers != 2 || len(r.TermIDs) != 1 || len(r.TransactionIDs) != 2 {
		t.Fatal("combined payment must create one transfer and one fee", r, transfers)
	}
	again, err := e.svcL.Repay(adminUser(), in)
	mustOK(t, err)
	if again.OperationID != r.OperationID {
		t.Fatal("retry duplicated the combined payment")
	}
	_, err = e.svcL.Reverse(adminUser(), r.OperationID, ReverseInput{RequestKey: uuid.NewString(), Reason: "撤销合并还款"})
	mustOK(t, err)
	if !e.state(t).Summary.TotalDebt.Equal(dec("2008.28")) || e.state(t).Plans[0].Terms[1].FeeTxnID != nil {
		t.Fatal("reversal must restore ordinary debt, installment principal and its fee plan")
	}
}

// Uses only TestLiabilityMySQL's randomly named, migrated database.
func checkCombinedRepaymentMySQL(t *testing.T, db *gorm.DB) {
	e := &liabilityEnv{voucherEnv: seedVoucherEnv(t, db), now: vdate(t, "2026-10-15")}
	e.svcL = NewLiabilityService(db, e.txn)
	e.svcL.Now = func() time.Time { return e.now }
	configureSettingsTestAccount(t, e, 31, 10, 1)
	repaymentPlan(t, e)
	e.spend(t, "8.28", "2026-10-15")
	in := advanceInput(e, "1028.28", "2026-10-15")
	in.Scope = combinedRepayment
	in = quoteAdvance(t, e, in)
	start := make(chan struct{})
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() { <-start; _, err := e.svcL.Repay(adminUser(), in); results <- err }()
	}
	close(start)
	for i := 0; i < 2; i++ {
		mustOK(t, <-results)
	}
	if !e.state(t).Summary.TotalDebt.Equal(dec("2000")) || len(e.state(t).Credits) != 1 {
		t.Fatal("same attempt duplicated combined funds")
	}
	in.Amount, in.RequestKey = dec("1020"), uuid.NewString()
	in = quoteAdvance(t, e, in)
	start, results = make(chan struct{}), make(chan error, 2)
	for i := 0; i < 2; i++ {
		request := in
		request.RequestKey = uuid.NewString()
		go func() { <-start; _, err := e.svcL.Repay(adminUser(), request); results <- err }()
	}
	close(start)
	succeeded := 0
	for i := 0; i < 2; i++ {
		if err := <-results; err == nil {
			succeeded++
		} else if !errors.Is(err, liability.ErrConflict) {
			t.Fatal(err)
		}
	}
	if succeeded != 1 || !e.state(t).Summary.TotalDebt.Equal(dec("1000")) || len(e.state(t).Credits) != 2 {
		t.Fatal("stale concurrent quote moved money twice")
	}
	mustOK(t, liability.Reallocate(db, e.card))
	if !e.state(t).Summary.TotalDebt.Equal(dec("1000")) {
		t.Fatal("persisted combined mode lost allocations")
	}
}

func TestCombinedRepaymentOrdinaryOnlyAndFixedTargets(t *testing.T) {
	e := newLiabilityEnv(t)
	source := e.spend(t, "100", "2026-10-10")
	in := advanceInput(e, "30", "2026-10-15")
	in.Scope = combinedRepayment
	in = quoteAdvance(t, e, in)
	r, err := e.svcL.Repay(adminUser(), in)
	mustOK(t, err)
	e.spend(t, "20", "2026-10-09")
	if !e.state(t).Summary.TotalDebt.Equal(dec("90")) {
		t.Fatal("payment drifted after a backdated purchase")
	}
	_, err = e.txn.Update(adminUser(), source.ID, TxnInput{Kind: model.TxnExpense, AccountID: e.card, CategoryID: e.category, Amount: dec("100"), EntryDate: vdate(t, "2026-10-16")})
	if !errors.Is(err, liability.ErrConflict) {
		t.Fatal("paid consumption moved after its payment", err)
	}
	_, err = e.svcL.Allocate(adminUser(), r.TransactionIDs[0], LiabilityAllocationInput{RequestKey: uuid.NewString(), Mode: "auto"})
	if !errors.Is(err, liability.ErrManaged) {
		t.Fatal("combined payment was reallocated", err)
	}
	_, err = e.svcL.Reverse(adminUser(), r.OperationID, ReverseInput{RequestKey: uuid.NewString(), Reason: "撤销普通消费合并还款"})
	mustOK(t, err)
	if !e.state(t).Summary.TotalDebt.Equal(dec("120")) {
		t.Fatal("ordinary-only reversal lost debt")
	}
}

func TestCombinedRepaymentAllPlansAndFees(t *testing.T) {
	e := newLiabilityEnv(t)
	repaymentPlan(t, e)
	repaymentPlan(t, e)
	e.spend(t, "8.28", "2026-10-15")
	in := advanceInput(e, "6128.28", "2026-10-15")
	in.Scope = combinedRepayment
	in = quoteAdvance(t, e, in)
	r, err := e.svcL.Repay(adminUser(), in)
	mustOK(t, err)
	if len(r.PlanIDs) != 2 || len(r.TermIDs) != 6 || len(r.TransactionIDs) != 7 || !e.state(t).Summary.TotalDebt.IsZero() {
		t.Fatal(r, e.state(t).Summary)
	}
	for _, plan := range e.state(t).Plans {
		if plan.Status != "settled" {
			t.Fatal("plan not settled", plan)
		}
	}
	_, err = e.svcL.Reverse(adminUser(), r.OperationID, ReverseInput{RequestKey: uuid.NewString(), Reason: "撤销全部还清"})
	mustOK(t, err)
	if !e.state(t).Summary.TotalDebt.Equal(dec("6008.28")) || !e.state(t).Summary.ForecastFees.Equal(dec("120")) {
		t.Fatal("multi-plan reversal lost principal or fees")
	}
}

func TestCombinedRepaymentExactCents(t *testing.T) {
	e := newLiabilityEnv(t)
	_, err := e.svcL.CreatePlan(adminUser(), InstallmentInput{AccountID: e.card, Amount: dec("100.01"), EntryDate: "2026-10-15", CategoryID: e.category, TermCount: 3, FeeMode: "per_term", FeeInput: dec("0.08"), FeeAccountID: e.card, FeeCategoryID: e.category, RequestKey: uuid.NewString()})
	mustOK(t, err)
	e.spend(t, "8.28", "2026-10-15")
	in := advanceInput(e, "108.53", "2026-10-15")
	in.Scope = combinedRepayment
	q, err := e.svcL.OrdinaryRepaymentPreview(adminUser(), in)
	mustOK(t, err)
	if !q.AvailableAmount.Equal(dec("108.53")) || !q.FeeAmount.Equal(dec("0.24")) || !q.OrdinaryAmount.Equal(dec("8.28")) {
		t.Fatal("lost cents in principal remainder or per-term fees", q)
	}
	in.QuoteSignature = q.QuoteSignature
	_, err = e.svcL.Repay(adminUser(), in)
	mustOK(t, err)
	if !e.state(t).Summary.TotalDebt.IsZero() {
		t.Fatal("cent remainder left unpaid")
	}
}

func TestCombinedRepaymentIssuedAndUnbilled(t *testing.T) {
	e := newLiabilityEnv(t)
	repaymentPlan(t, e)
	e.spend(t, "200", "2026-10-15")
	e.now = vdate(t, "2026-11-01")
	_, err := e.svcL.EnsureDue(e.now)
	mustOK(t, err)
	e.spend(t, "8.28", "2026-11-01")
	in := advanceInput(e, "1228.28", "2026-11-01")
	in.Scope = combinedRepayment
	q, err := e.svcL.OrdinaryRepaymentPreview(adminUser(), in)
	mustOK(t, err)
	if !q.OrdinaryAmount.Equal(dec("208.28")) || !q.FeeAmount.Equal(dec("20")) || len(q.Installments) != 1 {
		t.Fatal(q)
	}
	in.QuoteSignature = q.QuoteSignature
	r, err := e.svcL.Repay(adminUser(), in)
	mustOK(t, err)
	if len(r.TransactionIDs) != 1 || !e.state(t).Summary.TotalDebt.Equal(dec("2000")) {
		t.Fatal("posted fee was charged twice or debt allocation was incomplete", r, e.state(t).Summary)
	}
}

func TestCombinedRepaymentRejectsPartialAndStale(t *testing.T) {
	e := newLiabilityEnv(t)
	repaymentPlan(t, e)
	e.spend(t, "8.28", "2026-10-15")
	before := e.state(t)
	for _, amount := range []string{"0", "500", "3068.29"} {
		in := advanceInput(e, amount, "2026-10-15")
		in.Scope = combinedRepayment
		if _, err := e.svcL.OrdinaryRepaymentPreview(adminUser(), in); err == nil {
			t.Fatal("invalid or partial payment quoted", amount)
		}
	}
	if !reflect.DeepEqual(before, e.state(t)) {
		t.Fatal("failed preview changed money")
	}
	in := advanceInput(e, "1028.28", "2026-10-15")
	in.Scope = combinedRepayment
	in = quoteAdvance(t, e, in)
	e.spend(t, "1", "2026-10-15")
	before = e.state(t)
	if _, err := e.svcL.Repay(adminUser(), in); !errors.Is(err, liability.ErrConflict) {
		t.Fatal("stale quote accepted", err)
	}
	if !reflect.DeepEqual(before, e.state(t)) {
		t.Fatal("stale payment partially posted")
	}
	in.EntryDate = "2026-10-16"
	if _, err := e.svcL.OrdinaryRepaymentPreview(adminUser(), in); err == nil {
		t.Fatal("future payment accepted")
	}
}

func TestCombinedRepaymentRollsBackFeesWhenTransferFails(t *testing.T) {
	e := newLiabilityEnv(t)
	repaymentPlan(t, e)
	e.spend(t, "8.28", "2026-10-15")
	in := advanceInput(e, "1028.28", "2026-10-15")
	in.Scope = combinedRepayment
	in = quoteAdvance(t, e, in)
	before := e.state(t)
	mustOK(t, e.db.Callback().Create().Before("gorm:create").Register("fail_combined_transfer", func(tx *gorm.DB) {
		if transaction, ok := tx.Statement.Dest.(*model.Transaction); ok && transaction.Kind == model.TxnTransfer {
			tx.AddError(errors.New("test transfer failure"))
		}
	}))
	if _, err := e.svcL.Repay(adminUser(), in); err == nil {
		t.Fatal("failed bank transfer was accepted")
	}
	if !reflect.DeepEqual(before, e.state(t)) {
		t.Fatal("failed transfer left a fee, payment or operation")
	}
}

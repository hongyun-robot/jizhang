package service

import (
	"errors"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"jizhang/internal/model"
	"jizhang/internal/repository"
	"testing"
	"time"
)

func TestLiabilityCrossCycleAcceptance(t *testing.T) {
	e := newLiabilityEnv(t)
	check := func(debt, issued, unbilled, credit, forecast string) {
		t.Helper()
		d := e.state(t)
		if !d.Summary.TotalDebt.Equal(dec(debt)) || !d.Summary.CurrentDue.Equal(dec(issued)) || !d.Summary.Unbilled.Equal(dec(unbilled)) || !d.Summary.PendingCredit.Equal(dec(credit)) || !d.Summary.ForecastFees.Equal(dec(forecast)) {
			t.Fatal(d.Summary)
		}
		var postings []model.Transaction
		mustOK(t, e.db.Find(&postings).Error)
		balance := dec("0")
		for _, p := range postings {
			if p.AccountID == e.card {
				effect, _ := p.Effects()
				balance = balance.Add(effect)
			}
			if p.Kind == model.TxnTransfer && p.TransferAccountID != nil && *p.TransferAccountID == e.card {
				balance = balance.Add(p.Amount)
			}
		}
		if !balance.Equal(d.Account.Balance) {
			t.Fatal("independent ledger sum differs", balance, d.Account.Balance)
		}
	}
	e.spend(t, "500", "2026-10-15")
	created, err := e.svcL.CreatePlan(adminUser(), InstallmentInput{AccountID: e.card, Amount: dec("3000"), EntryDate: "2026-10-15", CategoryID: e.category, TermCount: 3, FeeMode: "per_term", FeeInput: dec("20"), FeeAccountID: e.card, FeeCategoryID: e.category, RequestKey: uuid.NewString()})
	mustOK(t, err)
	check("3500", "0", "3500", "0", "60")
	e.expense(t, "2026-10", "3500")
	e.now = vdate(t, "2026-10-31")
	_, err = e.svcL.EnsureDue(e.now)
	mustOK(t, err)
	check("3520", "0", "3520", "0", "40")
	e.expense(t, "2026-10", "3520")
	e.now = vdate(t, "2026-11-01")
	_, err = e.svcL.EnsureDue(e.now)
	mustOK(t, err)
	check("3520", "1520", "2000", "0", "40")
	e.pay(t, "1520", "2026-11-09")
	check("2000", "0", "2000", "0", "40")
	e.expense(t, "2026-11", "0")
	p, err := e.svcL.Plan(adminUser(), created.PlanID)
	mustOK(t, err)
	refund, err := e.txn.Create(adminUser(), TxnInput{Kind: model.TxnRefund, AccountID: e.card, RelatedTxnID: p.SourceTxnID, Amount: dec("500"), EntryDate: vdate(t, "2026-11-10")})
	mustOK(t, err)
	check("1500", "0", "1500", "0", "40")
	e.expense(t, "2026-11", "-500")
	p, err = e.svcL.Plan(adminUser(), p.ID)
	mustOK(t, err)
	if p.Status != "needs_reconcile" {
		t.Fatal(p.Status)
	}
	_, err = e.svcL.UpdatePlan(adminUser(), p.ID, InstallmentUpdate{Version: p.Version, RequestKey: uuid.NewString(), Reason: "按平台确认剩余本金1500，费用不退", RefundTxnID: refund.ID}, true)
	mustOK(t, err)
	p, err = e.svcL.Plan(adminUser(), p.ID)
	mustOK(t, err)
	e.now = vdate(t, "2026-11-10")
	settled, err := e.svcL.Settle(adminUser(), p.ID, settlementRequest(t, e, p.ID))
	mustOK(t, err)
	check("0", "0", "0", "0", "0")
	e.expense(t, "2026-10", "3520")
	e.expense(t, "2026-11", "-460")
	e.balance(t, "-2060")
	mustClose(t, e.closing, adminUser(), "2026-11")
	_, err = e.svcL.Reverse(adminUser(), settled.OperationID, ReverseInput{RequestKey: uuid.NewString(), Reason: "尝试撤销封账记录"})
	if !errors.Is(err, ErrTxnClosedPeriod) {
		t.Fatal(err)
	}
	check("0", "0", "0", "0", "0")
}

func TestLiabilityVoucherAndRecurringShareProjection(t *testing.T) {
	e := newLiabilityEnv(t)
	in := e.input(t, "100", 5, "")
	in.AccountID = e.card
	v, err := e.svc.Create(adminUser(), in)
	mustOK(t, err)
	if !e.state(t).Summary.TotalDebt.Equal(dec("100")) {
		t.Fatal("voucher purchase missing principal")
	}
	e.expense(t, "2026-10", "0")
	redeemed := e.op(t, v.ID, model.VoucherRedeem, "2026-10-16", 1)
	if !e.state(t).Summary.TotalDebt.Equal(dec("100")) {
		t.Fatal("redemption added debt")
	}
	e.expense(t, "2026-10", "20")
	mustOK(t, e.svc.Undo(adminUser(), v.ID, redeemed.ID))
	e.expense(t, "2026-10", "0")
	writtenOff := e.op(t, v.ID, model.VoucherWriteoff, "2026-10-16", 0)
	if !e.state(t).Summary.TotalDebt.Equal(dec("100")) {
		t.Fatal("writeoff added debt")
	}
	mustOK(t, e.svc.Undo(adminUser(), v.ID, writtenOff.ID))
	refunded := e.op(t, v.ID, model.VoucherRefund, "2026-10-17", 0)
	if !e.state(t).Summary.TotalDebt.IsZero() {
		t.Fatal("voucher refund did not reduce debt")
	}
	mustOK(t, e.svc.Undo(adminUser(), v.ID, refunded.ID))
	if !e.state(t).Summary.TotalDebt.Equal(dec("100")) {
		t.Fatal("voucher reversal did not restore debt")
	}
	mustOK(t, e.db.AutoMigrate(&model.RecurringRule{}))
	rule := model.RecurringRule{Amount: dec("25"), Freq: model.FreqMonthly, AccountID: e.card, CategoryID: e.category, StartDate: vdate(t, "2026-10-18"), NextRunDate: vdate(t, "2026-10-18"), Status: model.RuleEnabled, CreatedBy: 1}
	mustOK(t, e.db.Create(&rule).Error)
	repo := repository.NewTransactionRepo(e.db)
	recurring := NewRecurringService(RuleDeps{Rules: repository.NewRecurringRepo(e.db), Dates: repo, Txns: e.txn, Accounts: repository.NewAccountRepo(e.db), Cats: repository.NewCategoryRepo(e.db)})
	e.svcL.BeforeEnsure = func(now time.Time) error { _, err := recurring.EnsureDue(now); return err }
	e.now = vdate(t, "2026-11-01")
	_, err = e.svcL.EnsureDue(e.now)
	mustOK(t, err)
	_, err = e.svcL.EnsureDue(e.now)
	mustOK(t, err)
	d := e.state(t)
	if !d.Summary.CurrentDue.Equal(dec("125")) {
		t.Fatal("recurring not in bill before issue", d.Summary)
	}
	var n int64
	mustOK(t, e.db.Model(&model.Transaction{}).Where("rule_id = ?", rule.ID).Count(&n).Error)
	if n != 1 {
		t.Fatal("duplicate recurring money", n)
	}
}

func TestLiabilitySettlementFailureAndRefundRevision(t *testing.T) {
	e := newLiabilityEnv(t)
	r, err := e.svcL.CreatePlan(adminUser(), InstallmentInput{AccountID: e.card, Amount: dec("3000"), EntryDate: "2026-10-15", CategoryID: e.category, TermCount: 3, FeeMode: "per_term", FeeInput: dec("0"), FeeAccountID: e.card, FeeCategoryID: e.category, RequestKey: uuid.NewString()})
	mustOK(t, err)
	p, err := e.svcL.Plan(adminUser(), r.PlanID)
	mustOK(t, err)
	refund, err := e.txn.Create(adminUser(), TxnInput{Kind: model.TxnRefund, AccountID: e.card, RelatedTxnID: p.SourceTxnID, Amount: dec("500"), EntryDate: e.now})
	mustOK(t, err)
	p, err = e.svcL.Plan(adminUser(), p.ID)
	mustOK(t, err)
	_, err = e.svcL.UpdatePlan(adminUser(), p.ID, InstallmentUpdate{Version: p.Version, RequestKey: uuid.NewString(), Reason: "确认退款", RefundTxnID: refund.ID}, true)
	mustOK(t, err)
	mustOK(t, e.txn.Delete(adminUser(), refund.ID))
	p, err = e.svcL.Plan(adminUser(), p.ID)
	mustOK(t, err)
	if p.Status != "needs_reconcile" || !e.state(t).Summary.TotalDebt.Equal(dec("3000")) {
		t.Fatal("refund deletion did not request new reconciliation", p)
	}
	e2 := newLiabilityEnv(t)
	r, err = e2.svcL.CreatePlan(adminUser(), InstallmentInput{AccountID: e2.card, Amount: dec("100"), EntryDate: "2026-10-15", CategoryID: e2.category, TermCount: 3, FeeMode: "per_term", FeeInput: dec("5"), FeeAccountID: e2.card, FeeCategoryID: e2.category, RequestKey: uuid.NewString()})
	mustOK(t, err)
	p, err = e2.svcL.Plan(adminUser(), r.PlanID)
	mustOK(t, err)
	fail := errors.New("settlement expense failure")
	calls := 0
	e2.txn.OnWrite = func() { calls++ }
	mustOK(t, e2.db.Callback().Create().Before("gorm:create").Register("settlement_failure", func(db *gorm.DB) {
		if t, ok := db.Statement.Dest.(*model.Transaction); ok && t.Kind == model.TxnExpense && t.LiabilityOperationID != nil {
			db.AddError(fail)
		}
	}))
	e2.now = vdate(t, "2026-10-16")
	_, err = e2.svcL.Settle(adminUser(), p.ID, settlementRequest(t, e2, p.ID))
	mustOK(t, e2.db.Callback().Create().Remove("settlement_failure"))
	if !errors.Is(err, fail) {
		t.Fatal(err)
	}
	d := e2.state(t)
	if !d.Summary.TotalDebt.Equal(dec("100")) || d.Plans[0].Status != "active" || calls != 0 {
		t.Fatal("partial settlement", d, calls)
	}
	e2.balance(t, "1000")
}

package service

import (
	"errors"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"jizhang/internal/liability"
	"jizhang/internal/model"
	"jizhang/internal/repository"
	"testing"
	"time"
)

type liabilityEnv struct {
	*voucherEnv
	svcL *LiabilityService
	card int64
	now  time.Time
}

func TestLiabilitySettleAndReverse(t *testing.T) {
	e := newLiabilityEnv(t)
	r, err := e.svcL.CreatePlan(adminUser(), InstallmentInput{AccountID: e.card, Amount: dec("3000"), EntryDate: "2026-10-15", CategoryID: e.category, TermCount: 3, FeeMode: "per_term", FeeInput: dec("20"), FeeAccountID: e.card, FeeCategoryID: e.category, RequestKey: uuid.NewString()})
	mustOK(t, err)
	e.now = vdate(t, "2026-11-01")
	_, err = e.svcL.EnsureDue(e.now)
	mustOK(t, err)
	e.pay(t, "1020", "2026-11-09")
	p, err := e.svcL.Plan(adminUser(), r.PlanID)
	mustOK(t, err)
	in := settlementRequest(t, e, p.ID)
	preview, err := e.svcL.SettlementPreview(adminUser(), p.ID, in)
	mustOK(t, err)
	if !preview.ScheduledFees.Equal(dec("40")) || !preview.TransferAmount.Equal(dec("2040")) {
		t.Fatal(preview)
	}
	wrong := in
	wrong.Principal = dec("1900")
	_, err = e.svcL.Settle(adminUser(), p.ID, wrong)
	if !errors.Is(err, liability.ErrConflict) {
		t.Fatal(err)
	}
	settled, err := e.svcL.Settle(adminUser(), p.ID, in)
	mustOK(t, err)
	again, err := e.svcL.Settle(adminUser(), p.ID, in)
	mustOK(t, err)
	if again.OperationID != settled.OperationID {
		t.Fatal("duplicated settlement")
	}
	d := e.state(t)
	if !d.Summary.TotalDebt.IsZero() || !d.Summary.ForecastFees.IsZero() || d.Plans[0].Status != "settled" {
		t.Fatal(d.Summary, d.Plans)
	}
	feeVisible := false
	for _, x := range d.Transactions {
		if x.LiabilityOperationID != nil && *x.LiabilityOperationID == settled.OperationID && x.Kind == model.TxnExpense && x.Amount.Equal(dec("20")) {
			feeVisible = true
		}
	}
	if !feeVisible {
		t.Fatal("settlement asset fee missing from detail")
	}
	_, err = e.svcL.Reverse(adminUser(), settled.OperationID, ReverseInput{RequestKey: uuid.NewString(), Reason: "核对后撤销结清"})
	mustOK(t, err)
	d = e.state(t)
	if !d.Summary.TotalDebt.Equal(dec("2000")) || !d.Summary.ForecastFees.Equal(dec("40")) || d.Plans[0].Status != "active" {
		t.Fatal(d.Summary, d.Plans)
	}
}

func TestLiabilitySettlementHasNoFutureBilling(t *testing.T) {
	e := newLiabilityEnv(t)
	r, err := e.svcL.CreatePlan(adminUser(), InstallmentInput{AccountID: e.card, Amount: dec("3000"), EntryDate: "2026-10-15", CategoryID: e.category, TermCount: 3, FeeMode: "per_term", FeeInput: dec("20"), FeeAccountID: e.card, FeeCategoryID: e.category, RequestKey: uuid.NewString()})
	mustOK(t, err)
	e.now = vdate(t, "2026-11-01")
	_, err = e.svcL.EnsureDue(e.now)
	mustOK(t, err)
	e.pay(t, "1020", "2026-11-09")
	p, err := e.svcL.Plan(adminUser(), r.PlanID)
	mustOK(t, err)
	_, err = e.svcL.Settle(adminUser(), p.ID, settlementRequest(t, e, p.ID))
	mustOK(t, err)
	e.now = vdate(t, "2027-01-01")
	_, err = e.svcL.EnsureDue(e.now)
	mustOK(t, err)
	d := e.state(t)
	if !d.Summary.TotalDebt.IsZero() {
		t.Fatal(d.Summary)
	}
	for _, b := range d.Statements {
		if b.Kind == "monthly" && b.CycleEnd >= "2026-11-30" && !b.Amount.IsZero() {
			t.Fatal("settled principal billed again", b)
		}
	}
}

func TestLiabilityPostingFaultRollsBack(t *testing.T) {
	e := newLiabilityEnv(t)
	hooks := 0
	e.txn.OnWrite = func() { hooks++ }
	injected := errors.New("fee projection fault")
	mustOK(t, e.db.Callback().Create().Before("gorm:create").Register("liability_fee_fault", func(db *gorm.DB) {
		if tx, ok := db.Statement.Dest.(*model.Transaction); ok && tx.LiabilityTermID != nil {
			db.AddError(injected)
		}
	}))
	_, err := e.svcL.CreatePlan(adminUser(), InstallmentInput{AccountID: e.card, Amount: dec("3000"), EntryDate: "2026-10-15", CategoryID: e.category, TermCount: 3, FeeMode: "upfront", FeeInput: dec("60"), FeeDate: "2026-10-15", FeeAccountID: e.card, FeeCategoryID: e.category, RequestKey: uuid.NewString()})
	mustOK(t, e.db.Callback().Create().Remove("liability_fee_fault"))
	if !errors.Is(err, injected) {
		t.Fatal(err)
	}
	d := e.state(t)
	if !d.Summary.TotalDebt.IsZero() || len(d.Plans) != 0 || len(d.Transactions) != 0 || hooks != 0 {
		t.Fatal("partial financial state exposed", d, hooks)
	}
}

func TestLiabilityRuleChangesFreezeAndRemainContinuous(t *testing.T) {
	e := newLiabilityEnv(t)
	e.spend(t, "500", "2026-10-15")
	e.now = vdate(t, "2026-11-01")
	_, err := e.svcL.EnsureDue(e.now)
	mustOK(t, err)
	before := e.state(t)
	var old model.LiabilityStatement
	for _, b := range before.Statements {
		if b.CycleEnd == "2026-10-31" {
			old = b
		}
	}
	_, err = e.svcL.Settings(adminUser(), e.card, LiabilitySettingsInput{LiabilityType: "huabei", StatementDay: 20, RepaymentDay: 25, RepaymentMonthOffset: 0})
	mustOK(t, err)
	e.now = vdate(t, "2027-01-01")
	_, err = e.svcL.EnsureDue(e.now)
	mustOK(t, err)
	d := e.state(t)
	for _, b := range d.Statements {
		if b.ID == old.ID && (b.CycleStart != old.CycleStart || b.CycleEnd != old.CycleEnd || b.DueDate != old.DueDate) {
			t.Fatal("rewrote history", b)
		}
		if b.CycleEnd == "2026-12-20" && (b.CycleStart != "2026-12-01" || b.DueDate != "2026-12-25") {
			t.Fatal("rule transition gap", b)
		}
	}
}

func newLiabilityEnv(t *testing.T) *liabilityEnv {
	e := newVoucherEnv(t)
	mustOK(t, e.db.AutoMigrate(append(model.LiabilityModels(), &model.Notification{})...))
	mustOK(t, e.db.AutoMigrate(&model.User{}))
	mustOK(t, e.db.Create(&[]model.User{{ID: 1, Username: "admin", Role: model.RoleAdmin, LiabilityAccountsInitialized: true}, {ID: 2, Username: "viewer", Role: model.RoleViewer, LiabilityAccountsInitialized: true}, {ID: 3, Username: "member", Role: model.RoleMember, LiabilityAccountsInitialized: true}}).Error)
	mustOK(t, e.db.Create(&[]model.LiabilityAccountType{{Code: "credit_card", Name: "信用卡"}, {Code: "huabei", Name: "花呗", Sort: 1}, {Code: "meituan_monthly", Name: "美团月付", Sort: 2}}).Error)
	owner := int64(1)
	card := model.Account{Name: "花呗", Type: model.AccountLiability, OwnerID: &owner, Status: model.StatusActive}
	mustOK(t, e.db.Create(&card).Error)
	env := &liabilityEnv{voucherEnv: e, card: card.ID, now: vdate(t, "2026-10-15")}
	env.svcL = NewLiabilityService(e.db, e.txn)
	env.svcL.Now = func() time.Time { return env.now }
	_, err := env.svcL.Settings(adminUser(), card.ID, LiabilitySettingsInput{LiabilityType: "huabei", StatementDay: 31, RepaymentDay: 10, RepaymentMonthOffset: 1})
	mustOK(t, err)
	return env
}
func (e *liabilityEnv) state(t *testing.T) *liability.Detail {
	t.Helper()
	d, err := e.svcL.Detail(adminUser(), e.card)
	mustOK(t, err)
	mustOK(t, liability.CheckInvariant(e.db, d.Account))
	return d
}
func (e *liabilityEnv) spend(t *testing.T, amount, date string) *model.Transaction {
	t.Helper()
	x, err := e.txn.Create(adminUser(), TxnInput{Kind: model.TxnExpense, AccountID: e.card, Amount: dec(amount), EntryDate: vdate(t, date), CategoryID: e.category})
	mustOK(t, err)
	return x
}
func (e *liabilityEnv) pay(t *testing.T, amount, date string) LiabilityResult {
	t.Helper()
	e.now = vdate(t, date)
	r, err := e.svcL.Repay(adminUser(), LiabilityRepaymentInput{AccountID: e.card, FromAccountID: e.account, Amount: dec(amount), EntryDate: date, RequestKey: uuid.NewString()})
	mustOK(t, err)
	return r
}
func TestLiabilityBillingAndNewConsumption(t *testing.T) {
	e := newLiabilityEnv(t)
	e.spend(t, "500", "2026-10-15")
	e.now = vdate(t, "2026-11-01")
	_, err := e.svcL.EnsureDue(e.now)
	mustOK(t, err)
	d := e.state(t)
	if !d.Summary.CurrentDue.Equal(dec("500")) || d.Summary.DueDate != "2026-11-10" {
		t.Fatal(d.Summary)
	}
	e.pay(t, "300", "2026-11-09")
	if !e.state(t).Summary.CurrentDue.Equal(dec("200")) {
		t.Fatal("partial repayment")
	}
	e.pay(t, "200", "2026-11-10")
	e.spend(t, "200", "2026-11-12")
	e.now = vdate(t, "2026-11-13")
	_, err = e.svcL.EnsureDue(e.now)
	mustOK(t, err)
	d = e.state(t)
	if !d.Summary.TotalDebt.Equal(dec("200")) || !d.Summary.Unbilled.Equal(dec("200")) || !d.Summary.Overdue.IsZero() || !d.Summary.CurrentDue.IsZero() {
		t.Fatal(d.Summary)
	}
}
func TestLiabilityPrepaymentAndRetry(t *testing.T) {
	e := newLiabilityEnv(t)
	e.spend(t, "500", "2026-10-15")
	e.now = vdate(t, "2026-11-01")
	_, err := e.svcL.EnsureDue(e.now)
	mustOK(t, err)
	req := LiabilityRepaymentInput{AccountID: e.card, FromAccountID: e.account, Amount: dec("600"), EntryDate: "2026-11-01", RequestKey: uuid.NewString()}
	_, err = e.svcL.Repay(adminUser(), req)
	if !errors.Is(err, liability.ErrConflict) {
		t.Fatal("excess repayment accepted", err)
	}
	req.Amount = dec("500")
	first, err := e.svcL.Repay(adminUser(), req)
	mustOK(t, err)
	second, err := e.svcL.Repay(adminUser(), req)
	mustOK(t, err)
	if first.OperationID != second.OperationID {
		t.Fatal("retry duplicated")
	}
	req.Amount = dec("601")
	_, err = e.svcL.Repay(adminUser(), req)
	if !errors.Is(err, liability.ErrConflict) {
		t.Fatal(err)
	}
	e.spend(t, "80", "2026-11-02")
	d := e.state(t)
	if !d.Summary.Balance.Equal(dec("-80")) || !d.Summary.PendingCredit.IsZero() {
		t.Fatal(d.Summary)
	}
}
func TestLiabilityInstallmentsAndManualFees(t *testing.T) {
	e := newLiabilityEnv(t)
	r, err := e.svcL.CreatePlan(adminUser(), InstallmentInput{AccountID: e.card, Amount: dec("3000"), EntryDate: "2026-10-15", CategoryID: e.category, TermCount: 3, FeeMode: "per_term", FeeInput: dec("20"), FeeAccountID: e.card, FeeCategoryID: e.category, RequestKey: uuid.NewString()})
	mustOK(t, err)
	d := e.state(t)
	if !d.Summary.TotalDebt.Equal(dec("3000")) || !d.Summary.ForecastFees.Equal(dec("60")) {
		t.Fatal(d.Summary)
	}
	e.now = vdate(t, "2026-11-01")
	_, err = e.svcL.EnsureDue(e.now)
	mustOK(t, err)
	_, err = e.svcL.EnsureDue(e.now)
	mustOK(t, err)
	d = e.state(t)
	if !d.Summary.CurrentDue.Equal(dec("1020")) || !d.Summary.TotalDebt.Equal(dec("3020")) || !d.Summary.ForecastFees.Equal(dec("40")) {
		t.Fatal(d.Summary)
	}
	e.pay(t, "1020", "2026-11-09")
	p, err := e.svcL.Plan(adminUser(), r.PlanID)
	mustOK(t, err)
	err = e.txn.Delete(adminUser(), p.SourceTxnID)
	if !errors.Is(err, liability.ErrManaged) {
		t.Fatal("managed source unprotected", err)
	}
	_, err = e.txn.Create(adminUser(), TxnInput{Kind: model.TxnRefund, Amount: dec("500"), AccountID: e.card, RelatedTxnID: p.SourceTxnID, EntryDate: vdate(t, "2026-11-10")})
	mustOK(t, err)
	d = e.state(t)
	if !d.Summary.TotalDebt.Equal(dec("1500")) || d.Plans[0].Status != "needs_reconcile" {
		t.Fatal(d.Summary, d.Plans)
	}
}
func TestLiabilityUpfrontFeeAndAtomicFailure(t *testing.T) {
	e := newLiabilityEnv(t)
	r, err := e.svcL.CreatePlan(adminUser(), InstallmentInput{AccountID: e.card, Amount: dec("100"), EntryDate: "2026-10-15", CategoryID: e.category, TermCount: 3, FeeMode: "upfront", FeeInput: dec("60"), FeeAccountID: e.account, FeeCategoryID: e.category, FeeDate: "2026-10-15", RequestKey: uuid.NewString()})
	mustOK(t, err)
	d := e.state(t)
	if !d.Summary.TotalDebt.Equal(dec("100")) || !d.Summary.ForecastFees.IsZero() || len(r.TransactionIDs) != 2 {
		t.Fatal(d.Summary, r)
	}
	p := d.Plans[0]
	if !p.Terms[2].Principal.Equal(dec("33.34")) {
		t.Fatal(p)
	}
	acc, err := repository.NewAccountRepo(e.db).List()
	mustOK(t, err)
	for _, a := range acc {
		if a.ID == e.account && !a.Balance.Equal(dec("940")) {
			t.Fatal(a.Balance)
		}
	}
	_, err = e.svcL.CreatePlan(&model.User{ID: 99, Role: model.RoleViewer}, InstallmentInput{AccountID: e.card, RequestKey: uuid.NewString()})
	if !errors.Is(err, ErrTxnForbidden) {
		t.Fatal(err)
	}
}

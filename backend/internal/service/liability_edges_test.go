package service

import (
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"jizhang/internal/liability"
	"jizhang/internal/model"
	"jizhang/internal/repository"
	"testing"
)

func TestLiabilityAccountSetupAndHistoryGuards(t *testing.T) {
	e := newLiabilityEnv(t)
	svc := NewAccountService(repository.NewAccountRepo(e.db))
	day := 31
	due := 10
	offset := 1
	_, err := svc.Create(CreateAccountInput{Name: "invalid asset", Type: model.AccountAsset, RepaymentDay: &due})
	if !errors.Is(err, liability.ErrInvalid) {
		t.Fatal(err)
	}
	_, err = svc.Create(CreateAccountInput{Name: "nonzero debt", Type: model.AccountLiability, OpeningBalance: dec("-10")})
	if !errors.Is(err, liability.ErrSetup) {
		t.Fatal(err)
	}
	pending, err := svc.Create(CreateAccountInput{Name: "pending", Type: model.AccountLiability})
	mustOK(t, err)
	_, err = e.txn.Create(adminUser(), TxnInput{Kind: model.TxnExpense, Amount: dec("10"), AccountID: pending.ID, CategoryID: e.category, EntryDate: e.now})
	if !errors.Is(err, liability.ErrSetup) {
		t.Fatal(err)
	}
	enabled, err := svc.Create(CreateAccountInput{Name: "configured", Type: model.AccountLiability, LiabilityType: "meituan_monthly", StatementDay: &day, RepaymentDay: &due, RepaymentMonthOffset: &offset})
	mustOK(t, err)
	if !enabled.BillingEnabled {
		t.Fatal(enabled)
	}
	e.spend(t, "100", "2026-10-15")
	_, err = svc.Update(e.card, CreateAccountInput{Name: "change type", Type: model.AccountAsset}, model.StatusActive)
	if !errors.Is(err, ErrAccountHasTransactions) {
		t.Fatal(err)
	}
	err = svc.Delete(e.card)
	if !errors.Is(err, ErrAccountHasTransactions) {
		t.Fatal(err)
	}
	_, err = e.svcL.Settings(&model.User{ID: 3, Role: model.RoleMember}, e.card, LiabilitySettingsInput{LiabilityType: "huabei", StatementDay: 31, RepaymentDay: 10, RepaymentMonthOffset: 1})
	if !errors.Is(err, ErrTxnForbidden) {
		t.Fatal(err)
	}
}

func TestLiabilityOrdinaryMutationAndAllFundDirections(t *testing.T) {
	e := newLiabilityEnv(t)
	original := e.spend(t, "100", "2026-10-15")
	e.now = vdate(t, "2026-11-01")
	_, err := e.svcL.EnsureDue(e.now)
	mustOK(t, err)
	_, err = e.txn.Update(adminUser(), original.ID, TxnInput{Kind: model.TxnExpense, Amount: dec("120"), AccountID: e.card, CategoryID: e.category, EntryDate: original.EntryDate})
	mustOK(t, err)
	d := e.state(t)
	if !d.Summary.TotalDebt.Equal(dec("120")) {
		t.Fatal(d.Summary)
	}
	snapshot := false
	for _, b := range d.Statements {
		if b.SnapshotAmount.Equal(dec("100")) {
			snapshot = true
		}
	}
	if !snapshot {
		t.Fatal("snapshot overwritten")
	}
	mustOK(t, e.txn.Delete(adminUser(), original.ID))
	if !e.state(t).Summary.TotalDebt.IsZero() {
		t.Fatal("delete left debt")
	}
	cat := model.Category{OwnerID: testOwner(1), Name: "credit-income", Kind: model.KindIncome}
	mustOK(t, e.db.Create(&cat).Error)
	_, err = e.txn.Create(adminUser(), TxnInput{Kind: model.TxnIncome, Amount: dec("50"), AccountID: e.card, CategoryID: cat.ID, EntryDate: vdate(t, "2026-11-02")})
	if !errors.Is(err, liability.ErrConflict) {
		t.Fatal("income without actual debt accepted", err)
	}
	_, err = e.txn.Create(adminUser(), TxnInput{Kind: model.TxnTransfer, Amount: dec("30"), AccountID: e.card, TransferAccountID: e.account, EntryDate: vdate(t, "2026-11-02")})
	mustOK(t, err)
	d = e.state(t)
	if !d.Summary.Balance.Equal(dec("-30")) {
		t.Fatal(d.Summary)
	}
	cat2 := model.Category{OwnerID: testOwner(1), Name: "actual asset expense", Kind: model.KindExpense}
	mustOK(t, e.db.Create(&cat2).Error)
	_, err = e.txn.Create(adminUser(), TxnInput{Kind: model.TxnExpense, Amount: dec("5"), AccountID: e.account, CategoryID: cat2.ID, EntryDate: e.now})
	mustOK(t, err)
	audited := false
	for _, op := range d.Operations {
		if op.Kind == "posting" && op.Reason == "流水修改" {
			var previous model.Transaction
			mustOK(t, json.Unmarshal([]byte(op.BeforeJSON), &previous))
			if previous.Amount.Equal(dec("100")) && op.ActorID == 1 {
				audited = true
			}
		}
	}
	if !audited {
		t.Fatal("source revision missing")
	}
}

func TestLiabilityManualAllocationAndReallocation(t *testing.T) {
	e := newLiabilityEnv(t)
	e.spend(t, "300", "2026-10-15")
	e.spend(t, "500", "2026-11-02")
	e.now = vdate(t, "2026-12-01")
	_, err := e.svcL.EnsureDue(e.now)
	mustOK(t, err)
	d := e.state(t)
	var november int64
	for _, b := range d.Statements {
		if b.CycleEnd == "2026-11-30" {
			november = b.ID
		}
	}
	r, err := e.svcL.Repay(adminUser(), LiabilityRepaymentInput{AccountID: e.card, FromAccountID: e.account, Amount: dec("400"), EntryDate: "2026-12-01", RequestKey: uuid.NewString(), Mode: "manual", Targets: []liability.Target{{StatementID: november, Amount: dec("400")}}})
	mustOK(t, err)
	d = e.state(t)
	if !d.Summary.Overdue.Equal(dec("300")) || !d.Summary.PendingCredit.IsZero() || !d.Summary.CurrentDue.Equal(dec("100")) {
		t.Fatal("manual intent changed", d.Summary)
	}
	_, err = e.svcL.Allocate(adminUser(), r.TransactionIDs[0], LiabilityAllocationInput{RequestKey: uuid.NewString(), Reason: "改为按到期日偿还", Mode: "auto"})
	mustOK(t, err)
	d = e.state(t)
	if !d.Summary.Overdue.IsZero() || !d.Summary.PendingCredit.IsZero() || !d.Summary.CurrentDue.Equal(dec("400")) {
		t.Fatal(d.Summary)
	}
	before := d.Summary.Balance
	_, err = e.svcL.Repay(adminUser(), LiabilityRepaymentInput{AccountID: e.card, FromAccountID: e.account, Amount: dec("100"), EntryDate: "2026-12-01", RequestKey: uuid.NewString(), Mode: "manual", Targets: []liability.Target{{StatementID: november, Amount: dec("101")}}})
	if !errors.Is(err, liability.ErrInvalid) {
		t.Fatal(err)
	}
	if !e.state(t).Summary.Balance.Equal(before) {
		t.Fatal("invalid allocation partly posted")
	}
	mustOK(t, e.txn.Delete(adminUser(), r.TransactionIDs[0]))
	if !e.state(t).Summary.TotalDebt.Equal(dec("800")) {
		t.Fatal("deleted repayment still allocated")
	}
}

func TestLiabilityReferenceAndAssignmentPreserveLedgerDate(t *testing.T) {
	e := newLiabilityEnv(t)
	source := e.spend(t, "480", "2026-10-15")
	e.now = vdate(t, "2026-11-01")
	_, err := e.svcL.EnsureDue(e.now)
	mustOK(t, err)
	d := e.state(t)
	var october int64
	for _, b := range d.Statements {
		if b.CycleEnd == "2026-10-31" {
			october = b.ID
		}
	}
	ref := dec("500")
	_, err = e.svcL.Reconcile(adminUser(), october, LiabilityReconcileInput{RequestKey: uuid.NewString(), ReferenceAmount: &ref, Complete: true})
	if !errors.Is(err, liability.ErrConflict) {
		t.Fatal(err)
	}
	_, err = e.svcL.Reconcile(adminUser(), october, LiabilityReconcileInput{RequestKey: uuid.NewString(), ReferenceAmount: &ref, Complete: false})
	mustOK(t, err)
	if !e.state(t).Summary.TotalDebt.Equal(dec("480")) {
		t.Fatal("reference rewrote balance")
	}
	e.spend(t, "20", "2026-10-20")
	_, err = e.svcL.Reconcile(adminUser(), october, LiabilityReconcileInput{RequestKey: uuid.NewString(), ReferenceAmount: &ref, Complete: true})
	mustOK(t, err)
	future, err := liability.StatementFor(e.db, e.card, "2026-11-02")
	mustOK(t, err)
	d = e.state(t)
	var itemID int64
	for _, x := range d.Items {
		if x.SourceID == source.ID {
			itemID = x.ID
		}
	}
	moves := LiabilityReconcileInput{RequestKey: uuid.NewString(), Reason: "按平台入账期调整", Moves: []struct {
		ItemID      int64 `json:"item_id"`
		StatementID int64 `json:"statement_id"`
	}{{ItemID: itemID, StatementID: future.ID}}}
	_, err = e.svcL.Reconcile(adminUser(), october, moves)
	mustOK(t, err)
	stored, err := repository.NewTransactionRepo(e.db).GetByID(source.ID)
	mustOK(t, err)
	if liability.Date(stored.EntryDate) != "2026-10-15" || stored.PeriodKey != "2026-10" || !e.state(t).Summary.TotalDebt.Equal(dec("500")) {
		t.Fatal(stored, e.state(t).Summary)
	}
	_, err = e.txn.Update(adminUser(), source.ID, TxnInput{Kind: model.TxnExpense, Amount: dec("490"), AccountID: e.card, CategoryID: e.category, EntryDate: source.EntryDate})
	mustOK(t, err)
	d = e.state(t)
	for _, x := range d.Items {
		if x.SourceID == source.ID && x.StatementID != future.ID {
			t.Fatal("explicit assignment lost")
		}
	}
}

func TestLiabilityFeeBlockingRecoveryAndManualLink(t *testing.T) {
	e := newLiabilityEnv(t)
	r, err := e.svcL.CreatePlan(adminUser(), InstallmentInput{AccountID: e.card, Amount: dec("3000"), EntryDate: "2026-10-15", CategoryID: e.category, TermCount: 3, FeeMode: "per_term", FeeInput: dec("20"), FeeAccountID: e.card, FeeCategoryID: e.category, RequestKey: uuid.NewString()})
	mustOK(t, err)
	mustClose(t, e.closing, adminUser(), "2026-10")
	e.now = vdate(t, "2026-11-01")
	_, err = e.svcL.EnsureDue(e.now)
	mustOK(t, err)
	d := e.state(t)
	if d.Plans[0].Terms[0].FeeStatus != "blocked" || !d.Summary.TotalDebt.Equal(dec("3000")) {
		t.Fatal(d)
	}
	mustOK(t, e.closing.Reopen(adminUser(), "2026-10"))
	_, err = e.svcL.EnsureDue(e.now)
	mustOK(t, err)
	d = e.state(t)
	if !d.Summary.TotalDebt.Equal(dec("3020")) || d.Plans[0].Terms[0].FeeStatus != "posted" {
		t.Fatal(d.Summary)
	}
	p, err := e.svcL.Plan(adminUser(), r.PlanID)
	mustOK(t, err)
	feeID := p.Terms[0].FeeTxnID
	if feeID == nil {
		t.Fatal("no fee")
	}
	err = e.txn.Delete(adminUser(), *feeID)
	if !errors.Is(err, liability.ErrManaged) {
		t.Fatal(err)
	}
	e2 := newLiabilityEnv(t)
	r2, err := e2.svcL.CreatePlan(adminUser(), InstallmentInput{AccountID: e2.card, Amount: dec("100"), EntryDate: "2026-10-15", CategoryID: e2.category, TermCount: 3, FeeMode: "per_term", FeeInput: dec("20"), FeeAccountID: e2.account, FeeCategoryID: e2.category, RequestKey: uuid.NewString()})
	mustOK(t, err)
	fee, err := e2.txn.Create(adminUser(), TxnInput{Kind: model.TxnExpense, Amount: dec("20"), AccountID: e2.account, CategoryID: e2.category, EntryDate: vdate(t, "2026-10-20")})
	mustOK(t, err)
	p2, err := e2.svcL.Plan(adminUser(), r2.PlanID)
	mustOK(t, err)
	_, err = e2.svcL.UpdatePlan(adminUser(), p2.ID, InstallmentUpdate{RequestKey: uuid.NewString(), Reason: "关联已发生的真实费用", Version: p2.Version, Terms: []TermChange{{ID: p2.Terms[0].ID, FeeDate: "2026-10-20", FeeTxnID: &fee.ID}}}, false)
	mustOK(t, err)
	e2.now = vdate(t, "2026-11-01")
	_, err = e2.svcL.EnsureDue(e2.now)
	mustOK(t, err)
	state := e2.state(t)
	if !state.Summary.TotalDebt.Equal(dec("100")) || state.Plans[0].Terms[0].FeeTxnID == nil || *state.Plans[0].Terms[0].FeeTxnID != fee.ID {
		t.Fatal("manual fee duplicated", state)
	}
}

func TestLiabilityRoleBoundsAndRefundToDifferentAccount(t *testing.T) {
	e := newLiabilityEnv(t)
	source := e.spend(t, "500", "2026-10-15")
	viewer := &model.User{ID: 2, Role: model.RoleViewer}
	member := &model.User{ID: 3, Role: model.RoleMember}
	_, err := e.svcL.Detail(viewer, e.card)
	if !errors.Is(err, ErrTxnForbidden) {
		t.Fatal("foreign account visible", err)
	}
	_, err = e.svcL.Repay(viewer, LiabilityRepaymentInput{AccountID: e.card, RequestKey: uuid.NewString()})
	if !errors.Is(err, ErrTxnForbidden) {
		t.Fatal(err)
	}
	_, err = e.svcL.CreatePlan(member, InstallmentInput{AccountID: e.card, SourceTxnID: source.ID, TermCount: 3, FeeMode: "per_term", FeeInput: dec("0"), FeeCategoryID: e.category, RequestKey: uuid.NewString()})
	if !errors.Is(err, ErrTxnForbidden) {
		t.Fatal(err)
	}
	_, err = e.txn.Create(adminUser(), TxnInput{Kind: model.TxnRefund, Amount: dec("200"), AccountID: e.account, RelatedTxnID: source.ID, EntryDate: vdate(t, "2026-11-02")})
	mustOK(t, err)
	if !e.state(t).Summary.TotalDebt.Equal(dec("500")) {
		t.Fatal("asset refund incorrectly repaid debt")
	}
	_, err = e.txn.Create(adminUser(), TxnInput{Kind: model.TxnRefund, Amount: dec("600"), AccountID: e.card, RelatedTxnID: source.ID, EntryDate: vdate(t, "2026-11-03")})
	if !errors.Is(err, liability.ErrConflict) {
		t.Fatal("excess liability refund accepted", err)
	}
	_, err = e.txn.Create(adminUser(), TxnInput{Kind: model.TxnRefund, Amount: dec("600"), AccountID: e.account, RelatedTxnID: source.ID, EntryDate: vdate(t, "2026-11-03")})
	mustOK(t, err)
	d := e.state(t)
	if !d.Summary.TotalDebt.Equal(dec("500")) || !d.Summary.PendingCredit.IsZero() {
		t.Fatal("asset refund changed original debt", d.Summary)
	}
}

func TestLiabilityReminderResolveAndReactivate(t *testing.T) {
	e := newLiabilityEnv(t)
	e.spend(t, "500", "2026-10-15")
	e.now = vdate(t, "2026-11-01")
	_, err := e.svcL.EnsureDue(e.now)
	mustOK(t, err)
	svc := NewNotificationService(NotifDeps{Notifs: repository.NewNotificationRepo(e.db), Accounts: repository.NewAccountRepo(e.db)})
	mustOK(t, svc.EvaluateRepayment(vdate(t, "2026-11-06")))
	rows, unread, err := svc.List()
	mustOK(t, err)
	if len(rows) != 0 || unread != 0 {
		t.Fatal("too early reminder")
	}
	e.now = vdate(t, "2026-11-07")
	mustOK(t, svc.EvaluateRepayment(e.now))
	rows, unread, err = svc.List()
	mustOK(t, err)
	if len(rows) != 1 || unread != 1 {
		t.Fatal(rows, unread)
	}
	id := rows[0].ID
	_, err = svc.MarkRead(id)
	mustOK(t, err)
	partial := e.pay(t, "200", "2026-11-07")
	mustOK(t, svc.EvaluateRepayment(e.now))
	rows, unread, err = svc.List()
	mustOK(t, err)
	var p RepaymentNotifyPayload
	mustOK(t, json.Unmarshal([]byte(rows[0].Payload), &p))
	if rows[0].ID != id || unread != 0 || p.Debt != "300.00" || p.InitialDebt != "500.00" {
		t.Fatal(rows, p, unread)
	}
	full := e.pay(t, "300", "2026-11-07")
	mustOK(t, svc.EvaluateRepayment(e.now))
	rows, unread, err = svc.List()
	mustOK(t, err)
	if len(rows) != 1 || rows[0].ResolvedAt == nil || unread != 0 {
		t.Fatal(rows, unread)
	}
	e.spend(t, "200", "2026-11-08")
	e.now = vdate(t, "2026-11-11")
	mustOK(t, svc.EvaluateRepayment(e.now))
	if !e.state(t).Summary.Overdue.IsZero() {
		t.Fatal("new unbilled debt made old bill overdue")
	}
	mustOK(t, e.txn.Delete(adminUser(), full.TransactionIDs[0]))
	mustOK(t, svc.EvaluateRepayment(e.now))
	rows, unread, err = svc.List()
	mustOK(t, err)
	if len(rows) != 1 || rows[0].ID != id || rows[0].ResolvedAt != nil || unread != 1 {
		t.Fatal("not reactivated once", rows, unread)
	}
	_, err = svc.MarkRead(id)
	mustOK(t, err)
	mustOK(t, svc.EvaluateRepayment(e.now))
	_, unread, err = svc.List()
	mustOK(t, err)
	if unread != 0 {
		t.Fatal("daily evaluation reset read state")
	}
	_ = partial
}

func TestLiabilityMetadataDoesNotInvalidateMoneyCaches(t *testing.T) {
	e := newLiabilityEnv(t)
	calls := 0
	periods := map[string]bool{}
	e.txn.OnWrite = func() { calls++ }
	e.txn.OnWritePeriod = func(keys ...string) {
		for _, k := range keys {
			periods[k] = true
		}
	}
	source := e.spend(t, "480", "2026-10-15")
	if calls != 1 || !periods["2026-10"] {
		t.Fatal(calls, periods)
	}
	calls = 0
	periods = map[string]bool{}
	e.now = vdate(t, "2026-11-01")
	_, err := e.svcL.EnsureDue(e.now)
	mustOK(t, err)
	if calls != 0 {
		t.Fatal("issuing bill changed cash caches")
	}
	d := e.state(t)
	bill := d.Statements[0]
	future, err := liability.StatementFor(e.db, e.card, "2026-11-02")
	mustOK(t, err)
	var item int64
	for _, x := range d.Items {
		if x.SourceID == source.ID {
			item = x.ID
		}
	}
	_, err = e.svcL.Reconcile(adminUser(), bill.ID, LiabilityReconcileInput{RequestKey: uuid.NewString(), Reason: "只调整归属", Moves: []struct {
		ItemID      int64 `json:"item_id"`
		StatementID int64 `json:"statement_id"`
	}{{item, future.ID}}})
	mustOK(t, err)
	if calls != 0 || len(periods) != 0 {
		t.Fatal("metadata invalidated financial aggregates")
	}
	_, err = e.svcL.CreatePlan(adminUser(), InstallmentInput{AccountID: e.card, Amount: dec("100"), EntryDate: "2026-11-01", CategoryID: e.category, TermCount: 3, FeeMode: "upfront", FeeInput: dec("10"), FeeDate: "2026-11-01", FeeAccountID: e.card, FeeCategoryID: e.category, RequestKey: uuid.NewString()})
	mustOK(t, err)
	if calls != 1 || !periods["2026-11"] {
		t.Fatal(calls, periods)
	}
}

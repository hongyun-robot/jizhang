package service

import (
	"errors"
	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"jizhang/internal/model"
	"jizhang/internal/repository"
	"strings"
	"testing"
	"time"
)

type voucherEnv struct {
	db       *gorm.DB
	svc      *VoucherService
	txn      *TransactionService
	account  int64
	category int64
	closing  *ClosingService
}

func newVoucherEnv(t *testing.T) *voucherEnv {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	mustOK(t, err)
	sqlDB, err := db.DB()
	mustOK(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	mustOK(t, db.AutoMigrate(&model.User{}, &model.Setting{}, &model.Account{}, &model.Category{}, &model.Transaction{}, &model.PeriodClosing{}, &model.AuditLog{}, &model.Voucher{}, &model.VoucherEvent{}, &model.VoucherPlatform{}, &model.VoucherPlatformTemplate{}))
	mustOK(t, db.Create(&[]model.User{
		{ID: 1, Username: "fixture-admin", Role: model.RoleAdmin, Status: model.StatusActive},
		{ID: 2, Username: "fixture-viewer", Role: model.RoleViewer, Status: model.StatusActive},
		{ID: 3, Username: "fixture-member", Role: model.RoleMember, Status: model.StatusActive},
		{ID: 4, Username: "fixture-recorder", Role: model.RoleRecorder, Status: model.StatusActive},
	}).Error)
	return seedVoucherEnv(t, db)
}
func seedVoucherEnv(t *testing.T, db *gorm.DB) *voucherEnv {
	t.Helper()
	periods := NewPeriodService(repository.NewSettingRepo(db))
	acc := model.Account{OwnerID: testOwner(1), Name: "券测试账户", Type: model.AccountAsset, Status: model.StatusActive, OpeningBalance: dec("1000")}
	mustOK(t, db.Create(&acc).Error)
	cat := model.Category{OwnerID: testOwner(1), Name: "券测试餐饮", Kind: model.KindExpense}
	mustOK(t, db.Create(&cat).Error)
	txn := NewTransactionService(TxnDeps{Txns: repository.NewTransactionRepo(db), Accounts: repository.NewAccountRepo(db), Cats: repository.NewCategoryRepo(db), Closings: repository.NewClosingRepo(db), Periods: periods})
	svc := NewVoucherService(db, txn)
	svc.now = func() time.Time { return vdate(t, "2026-12-02") }
	closing := NewClosingService(ClosingDeps{Closings: repository.NewClosingRepo(db), Audits: repository.NewAuditRepo(db), Periods: periods, Stats: repository.NewStatsRepo(db)})
	closing.now = svc.now
	return &voucherEnv{db: db, svc: svc, txn: txn, account: acc.ID, category: cat.ID, closing: closing}
}
func vdate(t *testing.T, s string) time.Time {
	t.Helper()
	d, e := ParseVoucherDate(s)
	mustOK(t, e)
	return d
}
func (e *voucherEnv) input(t *testing.T, amount string, units int64, expiry string) VoucherInput {
	t.Helper()
	in := VoucherInput{Name: "抖音餐券", Platform: "抖音", PaidAmount: dec(amount), PurchaseDate: vdate(t, "2026-10-10"), AccountID: e.account, CategoryID: e.category, TotalUnits: units, RequestKey: uuid.NewString()}
	if expiry != "" {
		d := vdate(t, expiry)
		in.ExpiresOn = &d
		in.AutoRefundEnabled = true
	}
	return in
}
func (e *voucherEnv) buy(t *testing.T, amount string, units int64, expiry string) *model.Voucher {
	t.Helper()
	v, err := e.svc.Create(adminUser(), e.input(t, amount, units, expiry))
	mustOK(t, err)
	return v
}
func (e *voucherEnv) op(t *testing.T, id int64, action model.VoucherAction, date string, units int64) *model.VoucherEvent {
	t.Helper()
	row, err := e.svc.Operate(adminUser(), id, action, VoucherOperation{EntryDate: vdate(t, date), Units: units, RequestKey: uuid.NewString()})
	mustOK(t, err)
	return row
}
func (e *voucherEnv) balance(t *testing.T, want string) {
	t.Helper()
	rows, err := repository.NewAccountRepo(e.db).List()
	mustOK(t, err)
	for _, a := range rows {
		if a.ID == e.account {
			if !a.Balance.Equal(dec(want)) {
				t.Fatalf("balance=%s want %s", a.Balance, want)
			}
			return
		}
	}
	t.Fatal("account missing")
}
func (e *voucherEnv) expense(t *testing.T, period, want string) {
	t.Helper()
	row, err := repository.NewStatsRepo(e.db).SumByPeriod(period)
	mustOK(t, err)
	if !row.Expense.Equal(dec(want)) {
		t.Fatalf("%s expense=%s want %s", period, row.Expense, want)
	}
	roots, err := repository.NewBudgetRepo(e.db).ConsumptionByRoot(period)
	mustOK(t, err)
	if !roots[e.category].Equal(dec(want)) {
		t.Fatalf("budget=%s want %s", roots[e.category], want)
	}
}

func TestVoucherLifecycleAggregates(t *testing.T) {
	e := newVoucherEnv(t)
	v := e.buy(t, "100", 5, "")
	e.balance(t, "900")
	e.expense(t, "2026-10", "0")
	list, err := e.svc.List(adminUser(), "", "", 1, 20)
	mustOK(t, err)
	if !list.UnusedAmount.Equal(dec("100")) {
		t.Fatal(list)
	}
	e.op(t, v.ID, model.VoucherRedeem, "2026-11-02", 2)
	e.balance(t, "900")
	e.expense(t, "2026-11", "40")
	e.op(t, v.ID, model.VoucherRefund, "2026-11-15", 0)
	e.balance(t, "960")
	e.expense(t, "2026-11", "40")
	detail, err := e.svc.Detail(adminUser(), v.ID)
	mustOK(t, err)
	if detail.RemainingUnits != 0 || !detail.RemainingAmount.IsZero() || detail.Settlement != "refunded" {
		t.Fatal(detail)
	}
	stats := repository.NewStatsRepo(e.db)
	daily, err := stats.DailyTotals(vdate(t, "2026-11-01"), vdate(t, "2026-11-30"))
	mustOK(t, err)
	total := dec("0")
	for _, r := range daily {
		total = total.Add(r.Expense)
	}
	if !total.Equal(dec("40")) {
		t.Fatal(total)
	}
	category, err := stats.CategoryTotals(vdate(t, "2026-11-01"), vdate(t, "2026-11-30"))
	mustOK(t, err)
	total = dec("0")
	for _, r := range category {
		total = total.Add(r.Total)
	}
	if !total.Equal(dec("40")) {
		t.Fatal(total)
	}
	periods, err := stats.PeriodTotals(2026)
	mustOK(t, err)
	total = dec("0")
	for _, r := range periods {
		total = total.Add(r.Expense)
	}
	if !total.Equal(dec("40")) {
		t.Fatal(total)
	}
	nets, err := stats.DailyAccountNets(vdate(t, "2026-11-30"))
	mustOK(t, err)
	total = dec("0")
	for _, r := range nets {
		total = total.Add(r.Net)
	}
	if !total.Equal(dec("-40")) {
		t.Fatal(total)
	}
	preview, err := e.closing.Preview("2026-11")
	mustOK(t, err)
	if !preview.Expense.Equal(dec("40")) {
		t.Fatal(preview)
	}
	_, err = e.svc.Operate(adminUser(), v.ID, model.VoucherRefund, VoucherOperation{EntryDate: vdate(t, "2026-11-15"), RequestKey: uuid.NewString()})
	if !errors.Is(err, ErrVoucherSettled) {
		t.Fatal(err)
	}
}

func TestVoucherAllocationUndoAndWriteoff(t *testing.T) {
	e := newVoucherEnv(t)
	v := e.buy(t, "10", 3, "")
	first := e.op(t, v.ID, model.VoucherRedeem, "2026-10-11", 1)
	second := e.op(t, v.ID, model.VoucherRedeem, "2026-11-01", 1)
	if !first.Amount.Equal(dec("3.33")) || !second.Amount.Equal(dec("3.33")) {
		t.Fatal("allocation")
	}
	if err := e.svc.Undo(adminUser(), v.ID, first.ID); !errors.Is(err, ErrVoucherDependent) {
		t.Fatal(err)
	}
	loss := e.op(t, v.ID, model.VoucherWriteoff, "2026-11-02", 0)
	if !loss.Amount.Equal(dec("3.34")) {
		t.Fatal(loss)
	}
	e.expense(t, "2026-11", "6.67")
	e.balance(t, "990")
	mustOK(t, e.svc.Undo(adminUser(), v.ID, loss.ID))
	mustOK(t, e.svc.Undo(adminUser(), v.ID, loss.ID))
	e.expense(t, "2026-11", "3.33")
	refund := e.op(t, v.ID, model.VoucherRefund, "2026-11-03", 0)
	e.balance(t, "993.34")
	mustOK(t, e.svc.Undo(adminUser(), v.ID, refund.ID))
	e.balance(t, "990")
	mustOK(t, e.svc.Undo(adminUser(), v.ID, second.ID))
	e.expense(t, "2026-11", "0")
	d, err := e.svc.Detail(adminUser(), v.ID)
	mustOK(t, err)
	if d.RemainingUnits != 2 || !d.RemainingAmount.Equal(dec("6.67")) || d.Events[2].VoidedAt == nil {
		t.Fatal(d)
	}
	z := e.buy(t, "0.01", 2, "")
	e.op(t, z.ID, model.VoucherRedeem, "2026-11-01", 1)
	var count int64
	mustOK(t, e.db.Model(&model.Transaction{}).Where("purpose = ?", model.PurposeVoucherRedemption).Count(&count).Error)
	if count != 1 {
		t.Fatal("zero-cost unit generated a transaction")
	}
	e.op(t, z.ID, model.VoucherRedeem, "2026-11-01", 1)
}

func TestVoucherAutomaticBoundaryCatchupAndPause(t *testing.T) {
	e := newVoucherEnv(t)
	v := e.buy(t, "100", 5, "2026-10-31")
	e.op(t, v.ID, model.VoucherRedeem, "2026-10-31", 2)
	before, err := time.Parse(time.RFC3339, "2026-10-31T15:59:59Z")
	mustOK(t, err)
	r, err := e.svc.EnsureDue(before)
	mustOK(t, err)
	if r.Generated != 0 {
		t.Fatal(r)
	}
	r, err = e.svc.EnsureDue(vdate(t, "2026-12-02"))
	mustOK(t, err)
	if r.Generated != 1 {
		t.Fatal(r)
	}
	e.balance(t, "960")
	d, err := e.svc.Detail(adminUser(), v.ID)
	mustOK(t, err)
	auto := d.Events[len(d.Events)-1]
	if voucherDay(auto.EntryDate) != "2026-11-01" || auto.Source != "auto" || auto.CreatedBy != adminUser().ID {
		t.Fatal(auto)
	}
	r, err = e.svc.EnsureDue(vdate(t, "2026-12-02"))
	mustOK(t, err)
	if r.Generated != 0 {
		t.Fatal(r)
	}
	mustOK(t, e.svc.Undo(adminUser(), v.ID, auto.ID))
	e.balance(t, "900")
	r, err = e.svc.EnsureDue(vdate(t, "2026-12-02"))
	mustOK(t, err)
	if r.Generated != 0 {
		t.Fatal("suspended refund reran")
	}
	in := e.input(t, "100", 5, "2026-10-31")
	in.ResumeAutoRefund = true
	_, err = e.svc.Update(adminUser(), v.ID, in)
	mustOK(t, err)
	r, err = e.svc.EnsureDue(vdate(t, "2026-12-02"))
	mustOK(t, err)
	if r.Generated != 1 {
		t.Fatal(r)
	}
	e.balance(t, "960")
}

func TestVoucherClosedPeriodFailureRecovery(t *testing.T) {
	e := newVoucherEnv(t)
	v := e.buy(t, "89", 1, "2026-10-31")
	mustClose(t, e.closing, adminUser(), "2026-10")
	mustClose(t, e.closing, adminUser(), "2026-11")
	r, err := e.svc.EnsureDue(vdate(t, "2026-12-02"))
	mustOK(t, err)
	if r.Failed != 1 || r.Generated != 0 {
		t.Fatal(r)
	}
	e.balance(t, "911")
	d, err := e.svc.Detail(adminUser(), v.ID)
	mustOK(t, err)
	if d.Status != "failed" || d.LastError == "" || !d.RemainingAmount.Equal(dec("89")) {
		t.Fatal(d)
	}
	mustOK(t, e.closing.Reopen(adminUser(), "2026-11"))
	r, err = e.svc.EnsureDue(vdate(t, "2026-12-02"))
	mustOK(t, err)
	if r.Generated != 1 {
		t.Fatal(r)
	}
	e.balance(t, "1000")
	d, err = e.svc.Detail(adminUser(), v.ID)
	mustOK(t, err)
	last := d.Events[len(d.Events)-1]
	mustClose(t, e.closing, adminUser(), "2026-11")
	if err := e.svc.Undo(adminUser(), v.ID, last.ID); !errors.Is(err, ErrTxnClosedPeriod) {
		t.Fatal(err)
	}
}

func TestVoucherIdempotencyProtectionAndPermissions(t *testing.T) {
	e := newVoucherEnv(t)
	in := e.input(t, "100", 5, "")
	v, err := e.svc.Create(adminUser(), in)
	mustOK(t, err)
	again, err := e.svc.Create(adminUser(), in)
	mustOK(t, err)
	if again.ID != v.ID {
		t.Fatal("duplicate voucher")
	}
	e.balance(t, "900")
	in.Name = "different"
	_, err = e.svc.Create(adminUser(), in)
	if !errors.Is(err, ErrVoucherKeyConflict) {
		t.Fatal(err)
	}
	op := VoucherOperation{EntryDate: vdate(t, "2026-11-01"), Units: 2, RequestKey: uuid.NewString()}
	event, err := e.svc.Operate(adminUser(), v.ID, model.VoucherRedeem, op)
	mustOK(t, err)
	dup, err := e.svc.Operate(adminUser(), v.ID, model.VoucherRedeem, op)
	mustOK(t, err)
	if dup.ID != event.ID {
		t.Fatal("duplicate event")
	}
	e.expense(t, "2026-11", "40")
	op.Units = 1
	_, err = e.svc.Operate(adminUser(), v.ID, model.VoucherRedeem, op)
	if !errors.Is(err, ErrVoucherKeyConflict) {
		t.Fatal(err)
	}
	member := &model.User{ID: 3, Role: model.RoleMember}
	viewer := &model.User{ID: 4, Role: model.RoleViewer}
	if err := e.svc.Undo(member, v.ID, event.ID); !errors.Is(err, ErrTxnForbidden) {
		t.Fatal(err)
	}
	if _, err := e.svc.Create(viewer, e.input(t, "89", 1, "")); !errors.Is(err, ErrTxnForbidden) {
		t.Fatal(err)
	}
	if _, err := e.svc.Detail(viewer, v.ID); !errors.Is(err, ErrTxnForbidden) {
		t.Fatal(err)
	}
	rows, _, err := e.txn.List(adminUser(), repository.TxnFilter{})
	mustOK(t, err)
	for _, row := range rows {
		if !row.IsVoucher() || row.VoucherID == nil {
			t.Fatal(row)
		}
		if err := e.txn.Delete(adminUser(), row.ID); !errors.Is(err, ErrTxnVoucherManaged) {
			t.Fatal(err)
		}
		if _, err := e.txn.Update(adminUser(), row.ID, expenseIn(t, e.account, e.category, "1", "2026-11-01")); !errors.Is(err, ErrTxnVoucherManaged) {
			t.Fatal(err)
		}
	}
	var purchase model.Transaction
	mustOK(t, e.db.Where("purpose = ?", model.PurposeVoucherPurchase).First(&purchase).Error)
	_, err = e.txn.Create(adminUser(), TxnInput{Kind: model.TxnRefund, Amount: dec("100"), RelatedTxnID: purchase.ID, EntryDate: vdate(t, "2026-11-01")})
	if !errors.Is(err, ErrTxnVoucherManaged) {
		t.Fatal(err)
	}
}

func TestVoucherModifyDeleteAndCustomPeriod(t *testing.T) {
	e := newVoucherEnv(t)
	mustOK(t, e.txn.deps.Periods.SetSettlementDay(26))
	in := e.input(t, "89", 1, "")
	in.PurchaseDate = vdate(t, "2026-10-25")
	v, err := e.svc.Create(adminUser(), in)
	mustOK(t, err)
	e.op(t, v.ID, model.VoucherRedeem, "2026-10-26", 1)
	e.expense(t, "2026-10", "0")
	e.expense(t, "2026-11", "89")
	in.PaidAmount = dec("90")
	if _, err := e.svc.Update(adminUser(), v.ID, in); !errors.Is(err, ErrVoucherUsed) {
		t.Fatal(err)
	}
	if err := e.svc.Delete(adminUser(), v.ID); !errors.Is(err, ErrVoucherUsed) {
		t.Fatal(err)
	}
	unused := e.buy(t, "10", 1, "")
	update := e.input(t, "20", 2, "")
	_, err = e.svc.Update(adminUser(), unused.ID, update)
	mustOK(t, err)
	e.balance(t, "891")
	mustOK(t, e.svc.Delete(adminUser(), unused.ID))
	e.balance(t, "911")
}

func TestVoucherRollbackHooksAndClosingAudit(t *testing.T) {
	e := newVoucherEnv(t)
	hooks := 0
	e.txn.OnWrite = func() { hooks++ }
	mustOK(t, e.db.Callback().Create().Before("gorm:create").Register("voucher_projection_failure", func(db *gorm.DB) {
		if db.Statement.Table == "transactions" {
			db.AddError(errors.New("projection failure"))
		}
	}))
	_, err := e.svc.Create(adminUser(), e.input(t, "100", 1, ""))
	if err == nil {
		t.Fatal("expected rollback")
	}
	mustOK(t, e.db.Callback().Create().Remove("voucher_projection_failure"))
	var vouchers, events int64
	mustOK(t, e.db.Model(&model.Voucher{}).Count(&vouchers).Error)
	mustOK(t, e.db.Model(&model.VoucherEvent{}).Count(&events).Error)
	if vouchers != 0 || events != 0 || hooks != 0 {
		t.Fatal(vouchers, events, hooks)
	}
	e.balance(t, "1000")
	mustOK(t, e.db.Callback().Create().Before("gorm:create").Register("audit_failure", func(db *gorm.DB) {
		if db.Statement.Table == "audit_logs" {
			db.AddError(errors.New("audit failure"))
		}
	}))
	_, err = e.closing.Close(adminUser(), "2026-10")
	if err == nil {
		t.Fatal("expected failed audit")
	}
	closed, err := repository.NewClosingRepo(e.db).IsClosed("2026-10")
	mustOK(t, err)
	if closed {
		t.Fatal("closing survived failed audit")
	}
	mustOK(t, e.db.Callback().Create().Remove("audit_failure"))
}

func TestVoucherListSummaryAndCSV(t *testing.T) {
	e := newVoucherEnv(t)
	first := e.buy(t, "89", 1, "")
	e.buy(t, "10", 1, "2026-10-31")
	list, err := e.svc.List(adminUser(), "", "", 1, 1)
	mustOK(t, err)
	if len(list.Items) != 1 || list.Total != 2 || !list.UnusedAmount.Equal(dec("99")) {
		t.Fatal(list)
	}
	list, err = e.svc.List(adminUser(), "", "expired", 1, 1)
	mustOK(t, err)
	if list.Total != 1 || !list.UnusedAmount.Equal(dec("99")) {
		t.Fatal(list)
	}
	e.op(t, first.ID, model.VoucherRedeem, "2026-11-01", 1)
	data, err := e.txn.ExportCSV(adminUser(), repository.TxnFilter{})
	mustOK(t, err)
	text := string(data)
	if !strings.HasPrefix(text, "\ufeff") || !strings.Contains(text, "用途,券编号,余额影响,消费影响") || !strings.Contains(text, "购券,") || !strings.Contains(text, "券核销,") || !strings.Contains(text, "0.00,89.00") {
		t.Fatal(text)
	}
}

func TestVoucherInvalidDatesAndDisabledRefund(t *testing.T) {
	e := newVoucherEnv(t)
	v := e.buy(t, "100", 2, "2026-10-31")
	for _, day := range []string{"2026-10-09", "2026-11-01"} {
		_, err := e.svc.Operate(adminUser(), v.ID, model.VoucherRedeem, VoucherOperation{EntryDate: vdate(t, day), Units: 1, RequestKey: uuid.NewString()})
		if err == nil {
			t.Fatal(day)
		}
	}
	mustOK(t, e.db.Model(&model.Account{}).Where("id = ?", e.account).Update("status", model.StatusDisabled).Error)
	r, err := e.svc.EnsureDue(vdate(t, "2026-12-02"))
	mustOK(t, err)
	if r.Failed != 1 {
		t.Fatal(r)
	}
	mustOK(t, e.db.Model(&model.Account{}).Where("id = ?", e.account).Update("status", model.StatusActive).Error)
	r, err = e.svc.EnsureDue(vdate(t, "2026-12-02"))
	mustOK(t, err)
	if r.Generated != 1 {
		t.Fatal(r)
	}
	e.balance(t, "1000")
}

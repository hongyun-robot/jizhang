package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"jizhang/internal/model"
	"jizhang/internal/repository"
	"jizhang/internal/wealthdata"
)

func newCashEnv(t *testing.T) *wealthEnv {
	t.Helper()
	e := newWealthEnv(t)
	mustOK(t, e.db.AutoMigrate(&model.WealthCashLink{}, &model.WealthCashFlow{}, &model.WealthCashYield{}, &model.WealthCashAudit{}))
	e.txn.liabilityNow = func() time.Time { return e.now }
	return e
}
func enableCash(t *testing.T, e *wealthEnv, start, policy string) WealthCashReceipt {
	t.Helper()
	r, err := e.svc.ConfigureCash(context.Background(), e.users[0], e.banks[0], WealthCashInput{RequestKey: uuid.NewString(), Active: true, ProductID: 1, StartDate: start, Policy: policy})
	mustOK(t, err)
	return r
}
func TestWealthCashLedgerFastWithdrawalAndEditableYield(t *testing.T) {
	e := newCashEnv(t)
	ctx := context.Background()
	enableCash(t, e, "2026-10-12", "unit_nav")
	e.quote(t, "2026-10-12", "1")
	e.quote(t, "2026-10-13", "1.01")
	e.quote(t, "2026-10-14", "1.02")
	category := model.Category{Name: "现金支出", Kind: model.KindExpense, OwnerID: &e.users[0].ID}
	mustOK(t, e.db.Create(&category).Error)
	expense, err := e.txn.Create(e.users[0], TxnInput{Kind: model.TxnExpense, AccountID: e.banks[0], CategoryID: category.ID, Amount: dec("1000"), EntryDate: fday("2026-10-13")})
	mustOK(t, err)
	if len(expense.WealthCashFlows) != 1 || expense.WealthCashFlows[0].TransferMode != "quick" {
		t.Fatal("expense must default to quick", expense)
	}
	mustOK(t, e.svc.ensureCashYields(ctx))
	view, err := e.svc.CashAccount(e.users[0], e.banks[0])
	mustOK(t, err)
	if len(view.Yields) != 2 || view.Yields[0].Amount.String() != "40" || e.bank(t, 0) != "4080.00" {
		t.Fatal(view, e.bank(t, 0))
	}
	var count int64
	mustOK(t, e.db.Model(&model.Transaction{}).Where("purpose = ?", model.PurposeWealthCashYield).Count(&count).Error)
	if count != 2 {
		t.Fatal(count)
	}
	mustOK(t, e.svc.ensureCashYields(ctx))
	again, err := e.svc.CashAccount(e.users[0], e.banks[0])
	mustOK(t, err)
	if len(again.Audits) != len(view.Audits) {
		t.Fatal("unchanged retry generated audit")
	}
	row := view.Yields[0]
	in := WealthCashYieldInput{RequestKey: uuid.NewString(), ExpectedRevision: row.Revision, Amount: "45.00", Note: "按实际到账调整"}
	receipt, err := e.svc.EditCashYield(ctx, e.users[0], row.ID, in)
	mustOK(t, err)
	replay, err := e.svc.EditCashYield(ctx, e.users[0], row.ID, in)
	mustOK(t, err)
	if replay != receipt {
		t.Fatal("lost idempotency")
	}
	e.quote(t, "2026-10-13", "1.011")
	mustOK(t, e.svc.ensureCashYields(ctx))
	corrected, err := e.svc.CashAccount(e.users[0], e.banks[0])
	mustOK(t, err)
	if !corrected.Yields[0].ManualOverride || corrected.Yields[0].Amount.String() != "45" || corrected.Yields[0].CalculatedAmount.String() != "44" {
		t.Fatal(corrected.Yields)
	}
	if _, err = e.txn.Update(e.users[0], *row.TxnID, TxnInput{}); !errors.Is(err, ErrWealthManaged) {
		t.Fatal("ordinary editor changed yield", err)
	}
	if _, err = e.svc.CashAccount(e.users[1], e.banks[0]); !errors.Is(err, ErrTxnForbidden) {
		t.Fatal("cross owner", err)
	}
	if _, err = e.svc.EditCashYield(ctx, e.users[2], row.ID, in); !errors.Is(err, ErrTxnForbidden) {
		t.Fatal("viewer wrote yield", err)
	}
	txns, _, err := e.txn.List(e.users[0], repository.TxnFilter{})
	mustOK(t, err)
	for _, row := range txns {
		if row.WealthCashYieldID != nil {
			if !row.ExpenseEffect.IsZero() {
				t.Fatal("yield polluted consumption", row)
			}
		}
	}
	daily, err := repository.NewStatsRepo(e.db).ForOwner(e.users[0].ID).DailyTotals(fday("2026-10-12"), fday("2026-10-16"))
	mustOK(t, err)
	if len(daily) != 1 || !daily[0].Income.IsZero() || daily[0].Expense.String() != "1000" {
		t.Fatal("yield leaked into living statistics", daily)
	}
}

func TestWealthCashPendingLIFORefundAndMissingDates(t *testing.T) {
	link := model.WealthCashLink{StartDate: fday("2026-10-12"), Policy: "yulibao_20250715"}
	quotes := []model.WealthNAV{{NAVDate: fday("2026-10-12"), UnitNAV: dec("1")}, {NAVDate: fday("2026-10-13"), UnitNAV: dec("1.01")}, {NAVDate: fday("2026-10-14"), UnitNAV: dec("1.02")}}
	flows := []repository.CashMovement{
		{TxnID: 1, Date: fday("2026-10-13"), Kind: model.TxnIncome, Net: dec("1000"), Metadata: model.WealthCashFlow{ApplicationTime: "10:00:00"}},
		{TxnID: 2, Date: fday("2026-10-13"), Kind: model.TxnExpense, Net: dec("-800"), Metadata: model.WealthCashFlow{ApplicationTime: "11:00:00", TransferMode: "quick"}},
		{TxnID: 3, Date: fday("2026-10-14"), Kind: model.TxnRefund, Net: dec("100")},
	}
	plans, state, err := cashYieldPlans(link, dec("5000"), flows, quotes, fday("2026-10-16"), func(_ time.Time, v decimal.Decimal) decimal.Decimal { return v })
	mustOK(t, err)
	if len(plans) != 2 || plans[0].Calculated.String() != "50" || plans[1].Calculated.String() != "51.98" || state.free.String() != "100" {
		t.Fatal(plans, state)
	}
	late := flows[:1]
	late[0].Metadata.ApplicationTime = "15:01:00"
	plans, state, err = cashYieldPlans(link, dec("5000"), late, quotes, fday("2026-10-16"), func(_ time.Time, v decimal.Decimal) decimal.Decimal { return v })
	mustOK(t, err)
	if plans[1].Calculated.String() != "50" || state.confirmed.String() != "5100" {
		t.Fatal("unconfirmed transfer earned prematurely", plans, state)
	}
	if len(plans) != 2 {
		t.Fatal("invented undisclosed-date ledger returns")
	}
}

func TestWealthCashNegativeZeroAndClosedPeriod(t *testing.T) {
	e := newCashEnv(t)
	enableCash(t, e, "2026-10-12", "unit_nav")
	e.quote(t, "2026-10-12", "1")
	e.quote(t, "2026-10-13", "0.99")
	e.quote(t, "2026-10-14", "0.99")
	mustOK(t, e.svc.ensureCashYields(context.Background()))
	v, err := e.svc.CashAccount(e.users[0], e.banks[0])
	mustOK(t, err)
	if len(v.Yields) != 2 || v.Yields[0].Amount.String() != "-50" || v.Yields[1].TxnID != nil || e.bank(t, 0) != "4950.00" {
		t.Fatal(v)
	}
	_, err = e.txn.Create(e.users[0], TxnInput{Kind: model.TxnRefund, Amount: dec("1"), AccountID: e.banks[0], RelatedTxnID: *v.Yields[0].TxnID, EntryDate: fday("2026-10-14")})
	if !errors.Is(err, ErrWealthManaged) {
		t.Fatal("loss bypassed managed yield editor through refund", err)
	}
	closing := model.PeriodClosing{PeriodKey: e.txn.deps.Periods.KeyForDate(fday("2026-10-13")), Status: model.ClosingClosed, ClosedBy: e.users[0].ID}
	mustOK(t, e.db.Create(&closing).Error)
	e.quote(t, "2026-10-13", "0.98")
	mustOK(t, e.svc.ensureCashYields(context.Background()))
	v, err = e.svc.CashAccount(e.users[0], e.banks[0])
	mustOK(t, err)
	if v.Link.SyncStatus != "blocked_closed" || e.bank(t, 0) != "4950.00" || v.Yields[0].CalculatedAmount.String() != "-100" {
		t.Fatal(v, e.bank(t, 0))
	}
	_, err = e.svc.EditCashYield(context.Background(), e.users[0], v.Yields[0].ID, WealthCashYieldInput{RequestKey: uuid.NewString(), ExpectedRevision: v.Yields[0].Revision, Amount: "-99"})
	if !errors.Is(err, ErrTxnClosedPeriod) {
		t.Fatal(err)
	}
	mustOK(t, e.db.Delete(&closing).Error)
	mustOK(t, e.svc.ensureCashYields(context.Background()))
	v, err = e.svc.CashAccount(e.users[0], e.banks[0])
	mustOK(t, err)
	if v.Link.SyncStatus == "blocked_closed" {
		t.Fatal(v.Link)
	}
}

func TestWealthCashSchedulerTracksOnlyLinkedWalletAndSourceFailure(t *testing.T) {
	e := newCashEnv(t)
	product := addCmbcwmProduct(t, e)
	_, err := e.svc.ConfigureCash(context.Background(), e.users[0], e.banks[0], WealthCashInput{RequestKey: uuid.NewString(), Active: true, ProductID: product.ID, StartDate: "2026-10-14"})
	mustOK(t, err)
	e.market.quotes = []wealthdata.NAV{{Date: fday("2026-10-14"), Value: dec("1.068000"), SourceCode: product.Code, FetchedAt: e.now}, {Date: fday("2026-10-15"), Value: dec("1.068033"), SourceCode: product.Code, FetchedAt: e.now}}
	invalidated := map[int64]int{}
	e.svc.OnWrite = func(owner int64) { invalidated[owner]++ }
	mustOK(t, e.svc.Ensure(context.Background()))
	v, err := e.svc.CashAccount(e.users[0], e.banks[0])
	mustOK(t, err)
	if v.Link.Policy != "yulibao_20250715" || len(v.Yields) != 1 || e.bank(t, 0) != "5000.15" || invalidated[e.users[0].ID] < 1 {
		t.Fatal(v, invalidated)
	}
	if e.bank(t, 1) != "5000.00" {
		t.Fatal("unlinked ordinary account changed")
	}
	var n int64
	mustOK(t, e.db.Model(&model.WealthPosition{}).Count(&n).Error)
	if n != 0 {
		t.Fatal("wallet created duplicate investment position")
	}
	before := len(v.Audits)
	e.market.failure = wealthdata.ErrUnavailable
	e.now = e.now.Add(24 * time.Hour)
	mustOK(t, e.svc.Ensure(context.Background()))
	v, err = e.svc.CashAccount(e.users[0], e.banks[0])
	mustOK(t, err)
	if e.bank(t, 0) != "5000.15" || len(v.Audits) != before || v.Product.LastError == "" {
		t.Fatal("failed source changed ledger or lost error", v)
	}
}

func TestWealthCashDividendsRequireReviewAndOrdinaryRightsEndDate(t *testing.T) {
	e := newCashEnv(t)
	enableCash(t, e, "2026-10-12", "unit_nav")
	_, err := e.svc.SavePublicNAVs(1, []wealthdata.NAV{{Date: fday("2026-10-12"), Value: dec("1"), TotalValue: decimalPointer(dec("1")), SourceCode: "Y0408Ux", FetchedAt: e.now}, {Date: fday("2026-10-13"), Value: dec("0.95"), TotalValue: decimalPointer(dec("1.01")), SourceCode: "Y0408Ux", FetchedAt: e.now}})
	mustOK(t, err)
	mustOK(t, e.svc.ensureCashYields(context.Background()))
	v, err := e.svc.CashAccount(e.users[0], e.banks[0])
	mustOK(t, err)
	if v.Link.SyncStatus != "dividend_review" || v.Yields[0].TxnID != nil || e.bank(t, 0) != "5000.00" {
		t.Fatal("ex-dividend movement posted as loss", v)
	}
	link := model.WealthCashLink{StartDate: fday("2026-10-12"), Policy: "unit_nav"}
	end := fday("2026-10-13")
	quotes := []model.WealthNAV{{NAVDate: fday("2026-10-12"), UnitNAV: dec("1")}, {NAVDate: fday("2026-10-13"), UnitNAV: dec("1.01")}, {NAVDate: fday("2026-10-14"), UnitNAV: dec("1.02")}}
	flows := []repository.CashMovement{{Date: fday("2026-10-14"), Net: dec("-1000"), Metadata: model.WealthCashFlow{TransferMode: "ordinary", RightsEndDate: &end}}}
	plans, _, err := cashYieldPlans(link, dec("5000"), flows, quotes, e.now, func(_ time.Time, v decimal.Decimal) decimal.Decimal { return v })
	mustOK(t, err)
	if plans[0].Calculated.String() != "50" || plans[1].Calculated.String() != "40.1" {
		t.Fatal("ordinary lost the documented rights date", plans)
	}
	stats, err := repository.NewStatsRepo(e.db).ForOwner(e.users[0].ID).DailyTotals(fday("2026-10-12"), fday("2026-10-15"))
	mustOK(t, err)
	if len(stats) != 0 {
		t.Fatal("yield entered living statistics", stats)
	}
}

func TestWealthCashAutomaticPostingRollsBackWithAuditFailure(t *testing.T) {
	e := newCashEnv(t)
	enableCash(t, e, "2026-10-12", "unit_nav")
	e.quote(t, "2026-10-12", "1")
	e.quote(t, "2026-10-13", "1.01")
	failure := errors.New("isolated audit storage failure")
	mustOK(t, e.db.Callback().Create().Before("gorm:create").Register("fail_cash_audit", func(db *gorm.DB) {
		if db.Statement.Table == "wealth_cash_audits" {
			db.AddError(failure)
		}
	}))
	if err := e.svc.ensureCashYields(context.Background()); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	view, err := e.svc.CashAccount(e.users[0], e.banks[0])
	mustOK(t, err)
	if len(view.Yields) != 0 || len(view.Audits) != 1 || e.bank(t, 0) != "5000.00" {
		t.Fatal("partial automatic financial write", view)
	}
	mustOK(t, e.db.Callback().Create().Remove("fail_cash_audit"))
	mustOK(t, e.svc.ensureCashYields(context.Background()))
	if e.bank(t, 0) != "5050.00" {
		t.Fatal(e.bank(t, 0))
	}
}

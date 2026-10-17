package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"jizhang/internal/model"
	"jizhang/internal/repository"
	"jizhang/internal/suyin"
	"jizhang/internal/wealthdata"
	"strings"
	"testing"
	"time"
)

type wealthEnv struct {
	db       *gorm.DB
	svc      *WealthService
	txn      *TransactionService
	market   *wealthTestMarket
	accounts *repository.AccountRepo
	users    []*model.User
	wealths  []int64
	banks    []int64
	now      time.Time
}

func newWealthEnv(t *testing.T) *wealthEnv {
	t.Helper()
	db, e := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	mustOK(t, e)
	pool, e := db.DB()
	mustOK(t, e)
	pool.SetMaxOpenConns(1)
	t.Cleanup(func() { pool.Close() })
	mustOK(t, db.AutoMigrate(&model.User{}, &model.Setting{}, &model.Account{}, &model.Category{}, &model.Transaction{}, &model.PeriodClosing{}, &model.AuditLog{}, &model.WealthProduct{}, &model.WealthPosition{}, &model.WealthEvent{}, &model.WealthMutation{}, &model.WealthNAV{}, &model.WealthNAVRevision{}, &model.WealthValuation{}, &model.WealthAudit{}, &model.WealthManualNAV{}, &model.WealthManualAudit{}, &model.WealthSeries{}, &model.WealthSyncSlot{}))
	r := repository.NewAccountRepo(db)
	periods := NewPeriodService(repository.NewSettingRepo(db))
	txn := NewTransactionService(TxnDeps{Txns: repository.NewTransactionRepo(db), Accounts: r, Cats: repository.NewCategoryRepo(db), Closings: repository.NewClosingRepo(db), Periods: periods})
	m := &wealthTestMarket{}
	s := NewWealthService(db, txn, m)
	env := &wealthEnv{db: db, svc: s, txn: txn, market: m, accounts: r, now: fday("2026-10-16")}
	s.Now = func() time.Time { return env.now }
	s.BindAccounts(r)
	product := model.WealthProduct{ID: 1, Issuer: "suyin", RegistrationCode: "Z7003125000238", Code: "J07241", Name: "苏银理财恒源日申周赎1号A", Currency: "CNY", ValuationBasis: "cny_wealth_unit_nav", InceptionDate: fday("2025-08-29"), SourceAdapter: "suyin", SourceCode: "Y0408Ux", AdmissionStatus: "enabled", EvidenceVersion: "isolated-test-fixture"}
	mustOK(t, db.Create(&product).Error)
	for i, role := range []model.Role{model.RoleAdmin, model.RoleMember, model.RoleViewer, model.RoleRecorder} {
		u := &model.User{ID: int64(i + 1), Username: fmt.Sprintf("wealth-user-%d", i), PasswordHash: "hash", Role: role, Status: model.StatusActive}
		mustOK(t, db.Create(u).Error)
		env.users = append(env.users, u)
		owner := u.ID
		bank := model.Account{Name: "银行卡", Type: model.AccountAsset, AssetClass: "cash", OwnerID: &owner, OpeningBalance: dec("5000"), Status: model.StatusActive}
		wealth := model.Account{Name: "银行理财", Type: model.AccountAsset, AssetClass: "wealth", OwnerID: &owner, Status: model.StatusActive}
		mustOK(t, db.Create(&bank).Error)
		mustOK(t, db.Create(&wealth).Error)
		env.banks = append(env.banks, bank.ID)
		env.wealths = append(env.wealths, wealth.ID)
	}
	return env
}
func wealthOpening(shares, cost, date string) WealthInput {
	return WealthInput{RequestKey: uuid.NewString(), Code: "J07241", Shares: shares, CostPrice: cost, EffectiveDate: date}
}
func (e *wealthEnv) opening(t *testing.T, who int, shares, cost, date string) WealthReceipt {
	t.Helper()
	r, err := e.svc.CreateOpening(context.Background(), e.users[who], e.wealths[who], wealthOpening(shares, cost, date))
	mustOK(t, err)
	return r
}
func (e *wealthEnv) bank(t *testing.T, who int) string {
	t.Helper()
	rows, err := e.accounts.ForOwner(e.users[who].ID).List()
	mustOK(t, err)
	for _, a := range rows {
		if a.ID == e.banks[who] {
			return a.Balance.StringFixed(2)
		}
	}
	t.Fatal("bank missing")
	return ""
}

type wealthTestMarket struct {
	quotes  []suyin.NAV
	failure error
}

func (m *wealthTestMarket) Configured() bool { return true }
func (m *wealthTestMarket) NAVs(context.Context, string, string) ([]suyin.NAV, error) {
	return m.quotes, m.failure
}
func (m *wealthTestMarket) Fetch(ctx context.Context, product wealthdata.Product, window string) ([]wealthdata.NAV, error) {
	return m.NAVs(ctx, product.Code, window)
}
func (e *wealthEnv) quote(t *testing.T, date, value string) {
	t.Helper()
	_, err := e.svc.SavePublicNAVs(1, []suyin.NAV{{Date: fday(date), Value: dec(value), FetchedAt: e.now, SourceCode: "Y0408Ux"}})
	mustOK(t, err)
}

func TestWealthOpeningWithoutPriceAndOwnerBoundaries(t *testing.T) {
	e := newWealthEnv(t)
	ctx := context.Background()
	in := wealthOpening("1000", "1", "2026-10-10")
	r, err := e.svc.CreateOpening(ctx, e.users[0], e.wealths[0], in)
	mustOK(t, err)
	v, err := e.svc.Position(e.users[0], r.PositionID)
	mustOK(t, err)
	if v.Shares.String() != "1000" || v.Cost.String() != "1000" || v.MarketValue != nil || v.TotalPnL != nil || v.ValuationStatus != "no_price" || e.bank(t, 0) != "5000.00" {
		t.Fatal(v)
	}
	a, err := e.svc.Account(e.users[0], e.wealths[0])
	mustOK(t, err)
	raw, err := json.Marshal(a.Account)
	mustOK(t, err)
	if a.Complete || a.TotalValue != nil || !strings.Contains(string(raw), `"balance":null`) {
		t.Fatal(string(raw), a)
	}
	prior, err := e.svc.CreateOpening(ctx, e.users[0], e.wealths[0], in)
	mustOK(t, err)
	if prior != r {
		t.Fatal(prior, r)
	}
	in.CostPrice = "2"
	_, err = e.svc.CreateOpening(ctx, e.users[0], e.wealths[0], in)
	if !errors.Is(err, ErrWealthConflict) {
		t.Fatal(err)
	}
	for _, user := range e.users[1:] {
		if _, err = e.svc.Position(user, r.PositionID); !errors.Is(err, ErrTxnForbidden) {
			t.Fatal(user.Role, err)
		}
	}
	values, err := e.svc.ValuesForDates(e.db, []int64{e.wealths[0]}, []time.Time{fday("2026-10-09"), fday("2026-10-10")})
	mustOK(t, err)
	if !values["2026-10-09"][e.wealths[0]].Complete || values["2026-10-10"][e.wealths[0]].Complete {
		t.Fatal(values)
	}
	_, err = e.svc.AmendOpening(ctx, e.users[0], r.PositionID, wealthOpening("500", "0", "2026-10-10"))
	mustOK(t, err)
	v, err = e.svc.Position(e.users[0], r.PositionID)
	mustOK(t, err)
	if !v.Cost.IsZero() || v.HoldingReturn != nil {
		t.Fatal(v)
	}
}

func TestWealthCrossDayBuyAndFeeConservation(t *testing.T) {
	e := newWealthEnv(t)
	e.quote(t, "2026-10-12", "1")
	ctx := context.Background()
	buy := WealthInput{RequestKey: uuid.NewString(), Code: "J07241", Shares: "990", Amount: "1000", Fee: "10", TradeNAV: "1", CashDate: "2026-10-10", EffectiveDate: "2026-10-12", CashAccountID: e.banks[0]}
	r, err := e.svc.FirstBuy(ctx, e.users[0], e.wealths[0], buy)
	mustOK(t, err)
	again, err := e.svc.FirstBuy(ctx, e.users[0], e.wealths[0], buy)
	mustOK(t, err)
	if again != r || e.bank(t, 0) != "4000.00" {
		t.Fatal(again, r, e.bank(t, 0))
	}
	values, err := e.svc.ValuesForDates(e.db, []int64{e.wealths[0]}, []time.Time{fday("2026-10-09"), fday("2026-10-10"), fday("2026-10-12")})
	mustOK(t, err)
	for date, want := range map[string]string{"2026-10-09": "0", "2026-10-10": "1000", "2026-10-12": "990"} {
		got := values[date][e.wealths[0]]
		if !got.Complete || got.Value.String() != want {
			t.Fatal(date, got)
		}
	}
	v, err := e.svc.Position(e.users[0], r.PositionID)
	mustOK(t, err)
	if v.Cost.String() != "1000" || v.TotalPnL == nil || v.TotalPnL.String() != "-10" || v.Fees.String() != "10" {
		t.Fatal(v)
	}
	rows, err := e.svc.Events(e.users[0], r.PositionID)
	mustOK(t, err)
	if len(rows) != 1 {
		t.Fatal(rows)
	}
	var txns []model.Transaction
	mustOK(t, e.db.Find(&txns).Error)
	if len(txns) != 1 || txns[0].Purpose != model.PurposeWealthPurchase || !txns[0].IsWealthManaged() {
		t.Fatal(txns)
	}
	_, expense := txns[0].Effects()
	if !expense.IsZero() {
		t.Fatal("investment counted as living expense")
	}
	_, err = e.txn.Update(e.users[0], txns[0].ID, TxnInput{Kind: model.TxnTransfer, Amount: dec("1"), AccountID: e.banks[0], TransferAccountID: e.wealths[0], EntryDate: fday("2026-10-10")})
	if !errors.Is(err, ErrWealthManaged) {
		t.Fatal(err)
	}
	if err = e.txn.Delete(e.users[0], txns[0].ID); !errors.Is(err, ErrWealthManaged) {
		t.Fatal(err)
	}
}

func TestWealthRedemptionSettlementAndDependentUndo(t *testing.T) {
	e := newWealthEnv(t)
	ctx := context.Background()
	r := e.opening(t, 0, "100", "1.2", "2026-10-09")
	_, err := e.svc.Operate(ctx, e.users[0], r.PositionID, WealthInput{RequestKey: uuid.NewString(), Kind: "buy", Shares: "100", Amount: "110", TradeNAV: "1.1", EffectiveDate: "2026-10-12", CashDate: "2026-10-10", CashAccountID: e.banks[0]})
	mustOK(t, err)
	sale, err := e.svc.Operate(ctx, e.users[0], r.PositionID, WealthInput{RequestKey: uuid.NewString(), Kind: "redeem", Shares: "100", Amount: "130", Fee: "2", EffectiveDate: "2026-10-13"})
	mustOK(t, err)
	v, err := e.svc.Position(e.users[0], r.PositionID)
	mustOK(t, err)
	if v.Shares.String() != "100" || v.Cost.String() != "115" || v.PendingFunds.String() != "128" || v.RealizedPnL.String() != "13" || e.bank(t, 0) != "4890.00" {
		t.Fatal(v)
	}
	settled, err := e.svc.Operate(ctx, e.users[0], r.PositionID, WealthInput{RequestKey: uuid.NewString(), Kind: "settlement", SourceEventID: sale.EventID, EffectiveDate: "2026-10-15", CashAccountID: e.banks[0]})
	mustOK(t, err)
	if e.bank(t, 0) != "5018.00" {
		t.Fatal(e.bank(t, 0))
	}
	if _, err = e.svc.Undo(e.users[0], r.PositionID, sale.EventID, uuid.NewString()); !errors.Is(err, ErrWealthHistory) {
		t.Fatal(err)
	}
	_, err = e.svc.Operate(ctx, e.users[0], r.PositionID, WealthInput{RequestKey: uuid.NewString(), Kind: "settlement", SourceEventID: sale.EventID, EffectiveDate: "2026-10-16", CashAccountID: e.banks[0]})
	if !errors.Is(err, ErrWealthConflict) {
		t.Fatal(err)
	}
	_, err = e.svc.Undo(e.users[0], r.PositionID, settled.EventID, uuid.NewString())
	mustOK(t, err)
	_, err = e.svc.Undo(e.users[0], r.PositionID, sale.EventID, uuid.NewString())
	mustOK(t, err)
	v, err = e.svc.Position(e.users[0], r.PositionID)
	mustOK(t, err)
	if v.Shares.String() != "200" || v.Cost.String() != "230" || !v.RealizedPnL.IsZero() {
		t.Fatal(v)
	}
	_, err = e.svc.Operate(ctx, e.users[0], r.PositionID, WealthInput{RequestKey: uuid.NewString(), Kind: "redeem", Shares: "200", Amount: "260", Fee: "2", EffectiveDate: "2026-10-16"})
	mustOK(t, err)
	v, err = e.svc.Position(e.users[0], r.PositionID)
	mustOK(t, err)
	a, err := e.svc.Account(e.users[0], e.wealths[0])
	mustOK(t, err)
	if !v.Shares.IsZero() || !v.Cost.IsZero() || a.TotalValue == nil || a.TotalValue.String() != "258" || v.RealizedPnL.String() != "28" {
		t.Fatal(v, a)
	}
}

func TestWealthRightsDoNotDoubleCountDividends(t *testing.T) {
	e := newWealthEnv(t)
	ctx := context.Background()
	r := e.opening(t, 0, "1000", "1.2", "2026-10-09")
	e.quote(t, "2026-10-09", "1.2")
	e.quote(t, "2026-10-12", "1.1")
	_, err := e.svc.Operate(ctx, e.users[0], r.PositionID, WealthInput{RequestKey: uuid.NewString(), Kind: "cash_dividend", Amount: "100", EffectiveDate: "2026-10-12", RightsDate: "2026-10-12"})
	mustOK(t, err)
	v, err := e.svc.Position(e.users[0], r.PositionID)
	mustOK(t, err)
	if v.Cost.String() != "1200" || v.Dividends.String() != "100" || v.PendingFunds.String() != "100" || v.TotalPnL == nil || !v.TotalPnL.IsZero() || v.DailyPnL == nil || !v.DailyPnL.IsZero() {
		t.Fatal(v)
	}
	r2 := e.opening(t, 1, "1000", "1.2", "2026-10-09")
	_, err = e.svc.Operate(ctx, e.users[1], r2.PositionID, WealthInput{RequestKey: uuid.NewString(), Kind: "reinvest", Amount: "100", Shares: "100", EffectiveDate: "2026-10-13", RightsDate: "2026-10-12"})
	mustOK(t, err)
	v, err = e.svc.Position(e.users[1], r2.PositionID)
	mustOK(t, err)
	if v.Shares.String() != "1100" || v.Cost.String() != "1300" || v.Dividends.String() != "100" || !v.PendingFunds.IsZero() || e.bank(t, 1) != "5000.00" {
		t.Fatal(v)
	}
}

func TestWealthInputRejectionsAndAtomicRollback(t *testing.T) {
	e := newWealthEnv(t)
	ctx := context.Background()
	valid := WealthInput{RequestKey: uuid.NewString(), Code: "J07241", Kind: "buy", Shares: "990", Amount: "1000", Fee: "10", TradeNAV: "1", CashDate: "2026-10-10", EffectiveDate: "2026-10-12", CashAccountID: e.banks[0]}
	for _, mutate := range []func(*WealthInput){func(i *WealthInput) { i.CashDate = "2026-10-13" }, func(i *WealthInput) { i.EffectiveDate = "2026-10-17" }, func(i *WealthInput) { i.Shares = "" }, func(i *WealthInput) { i.Shares = "NaN" }, func(i *WealthInput) { i.Shares = "0.000000001" }, func(i *WealthInput) { i.Amount = "1000.001" }, func(i *WealthInput) { i.Fee = "1000" }, func(i *WealthInput) { i.TradeNAV = "2" }, func(i *WealthInput) { i.EffectiveDate = "2026-02-30" }} {
		i := valid
		i.RequestKey = uuid.NewString()
		mutate(&i)
		if _, err := e.svc.FirstBuy(ctx, e.users[0], e.wealths[0], i); err == nil {
			t.Fatal("invalid accepted", i)
		}
	}
	injected := errors.New("injected persistence failure")
	mustOK(t, e.db.Callback().Create().Before("gorm:create").Register("wealth_test_failure", func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Table == "wealth_mutations" {
			tx.AddError(injected)
		}
	}))
	_, err := e.svc.FirstBuy(ctx, e.users[0], e.wealths[0], valid)
	if !errors.Is(err, injected) {
		t.Fatal(err)
	}
	mustOK(t, e.db.Callback().Create().Remove("wealth_test_failure"))
	for _, table := range []string{"wealth_events", "wealth_positions", "transactions", "wealth_audits", "wealth_mutations"} {
		var n int64
		mustOK(t, e.db.Table(table).Count(&n).Error)
		if n != 0 {
			t.Fatal("partial financial commit", table, n)
		}
	}
	if e.bank(t, 0) != "5000.00" {
		t.Fatal(e.bank(t, 0))
	}
}

func TestWealthZeroCostAndAmountLimitsDoNotUseOlderPrice(t *testing.T) {
	e := newWealthEnv(t)
	e.quote(t, "2026-10-09", "1")
	r := e.opening(t, 0, "1000000000", "0", "2026-10-09")
	v, err := e.svc.Position(e.users[0], r.PositionID)
	mustOK(t, err)
	if v.HoldingReturn != nil || v.MarketValue.String() != "1000000000" {
		t.Fatal(v)
	}
	e.quote(t, "2026-10-10", "20")
	v, err = e.svc.Position(e.users[0], r.PositionID)
	mustOK(t, err)
	if v.MarketValue != nil || v.ValuationStatus != "range_error" {
		t.Fatal("overflow used older price", v)
	}
	e2 := newWealthEnv(t)
	e2.quote(t, "2026-10-09", "2")
	_, err = e2.svc.CreateOpening(context.Background(), e2.users[0], e2.wealths[0], wealthOpening("9999999999.99", "0", "2026-10-09"))
	if !errors.Is(err, ErrWealthInvalid) {
		t.Fatal("known aggregate overflow accepted", err)
	}
	_, err = e2.svc.CreateOpening(context.Background(), e2.users[0], e2.wealths[0], wealthOpening("1", "", "2026-10-09"))
	if !errors.Is(err, ErrWealthInvalid) {
		t.Fatal("missing cost silently became zero", err)
	}
}

func TestWealthLifecycleRolesAndClosedConfirmationOpenSettlement(t *testing.T) {
	e := newWealthEnv(t)
	ctx := context.Background()
	r := e.opening(t, 0, "200", "1.15", "2026-10-09")
	viewer := e.opening(t, 2, "1", "0", "2026-10-09")
	_, err := e.svc.Operate(ctx, e.users[2], viewer.PositionID, WealthInput{RequestKey: uuid.NewString(), Kind: "redeem", Shares: "1", Amount: "1", EffectiveDate: "2026-10-10"})
	if !errors.Is(err, ErrTxnForbidden) {
		t.Fatal(err)
	}
	sale, err := e.svc.Operate(ctx, e.users[0], r.PositionID, WealthInput{RequestKey: uuid.NewString(), Kind: "redeem", Shares: "100", Amount: "130", Fee: "2", EffectiveDate: "2026-10-13"})
	mustOK(t, err)
	if _, err = e.svc.AmendOpening(ctx, e.users[0], r.PositionID, wealthOpening("300", "1", "2026-10-09")); !errors.Is(err, ErrWealthHistory) {
		t.Fatal(err)
	}
	mustOK(t, e.db.Model(&model.Account{}).Where("id = ?", e.wealths[0]).Update("status", model.StatusDisabled).Error)
	e.now = fday("2026-11-02")
	closing := NewClosingService(ClosingDeps{Closings: repository.NewClosingRepo(e.db), Audits: repository.NewAuditRepo(e.db), Periods: e.txn.deps.Periods, Stats: repository.NewStatsRepo(e.db)})
	closing.now = e.svc.Now
	_, err = closing.Close(e.users[0], "2026-10")
	mustOK(t, err)
	_, err = e.svc.Operate(ctx, e.users[0], r.PositionID, WealthInput{RequestKey: uuid.NewString(), Kind: "redeem", Shares: "1", Amount: "1", EffectiveDate: "2026-10-14", CashDate: "2026-11-01", CashAccountID: e.banks[0]})
	if !errors.Is(err, ErrTxnClosedPeriod) {
		t.Fatal("open cash date bypassed closed confirmation", err)
	}
	_, err = e.svc.Operate(ctx, e.users[0], r.PositionID, WealthInput{RequestKey: uuid.NewString(), Kind: "settlement", SourceEventID: sale.EventID, EffectiveDate: "2026-11-01", CashAccountID: e.banks[0]})
	mustOK(t, err)
	if e.bank(t, 0) != "5128.00" {
		t.Fatal(e.bank(t, 0))
	}
	_, err = e.svc.Operate(ctx, e.users[0], r.PositionID, WealthInput{RequestKey: uuid.NewString(), Kind: "buy", Shares: "1", Amount: "1", TradeNAV: "1", CashDate: "2026-11-01", EffectiveDate: "2026-11-01", CashAccountID: e.banks[0]})
	if !errors.Is(err, ErrTxnAccountInvalid) {
		t.Fatal(err)
	}
	_, err = e.svc.Undo(e.users[0], r.PositionID, sale.EventID, uuid.NewString())
	if !errors.Is(err, ErrWealthHistory) {
		t.Fatal(err)
	}
}

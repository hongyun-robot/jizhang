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
	"jizhang/internal/fuyao"
	"jizhang/internal/model"
	"jizhang/internal/repository"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fundTestMarket struct {
	mu        sync.Mutex
	windows   []string
	quotes    []fuyao.NAV
	dividends []fuyao.Dividend
	failure   error
	calls     atomic.Int64
}

func (f *fundTestMarket) Configured() bool { return true }
func (f *fundTestMarket) Search(_ context.Context, q string) ([]fuyao.Instrument, error) {
	code := strings.TrimSuffix(q, ".OF") + ".OF"
	return []fuyao.Instrument{{THSCode: code, Ticker: strings.TrimSuffix(code, ".OF"), Name: "测试基金", AssetType: "fund-otc", Currency: "CNY"}}, f.failure
}
func (f *fundTestMarket) Profile(_ context.Context, c string) (fuyao.Profile, error) {
	return fuyao.Profile{THSCode: c, Ticker: strings.TrimSuffix(c, ".OF"), Name: "测试基金"}, f.failure
}
func (f *fundTestMarket) NAVs(_ context.Context, _ string, window string) ([]fuyao.NAV, error) {
	f.calls.Add(1)
	f.mu.Lock()
	f.windows = append(f.windows, window)
	f.mu.Unlock()
	return f.quotes, f.failure
}
func (f *fundTestMarket) Dividends(context.Context, string) ([]fuyao.Dividend, error) {
	return f.dividends, f.failure
}

type fundEnv struct {
	db       *gorm.DB
	svc      *FundService
	txn      *TransactionService
	market   *fundTestMarket
	accounts *repository.AccountRepo
	users    []*model.User
	funds    []int64
	banks    []int64
	now      time.Time
}

func newFundEnv(t *testing.T) *fundEnv {
	t.Helper()
	db, e := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	mustOK(t, e)
	pool, e := db.DB()
	mustOK(t, e)
	pool.SetMaxOpenConns(1)
	t.Cleanup(func() { pool.Close() })
	mustOK(t, db.AutoMigrate(&model.User{}, &model.Setting{}, &model.Account{}, &model.Category{}, &model.Transaction{}, &model.PeriodClosing{}, &model.AuditLog{}, &model.FundInstrument{}, &model.FundPosition{}, &model.FundEvent{}, &model.FundMutation{}, &model.FundNAV{}, &model.FundNAVRevision{}, &model.FundValuation{}, &model.FundAudit{}, &model.FundDividend{}))
	r := repository.NewAccountRepo(db)
	periods := NewPeriodService(repository.NewSettingRepo(db))
	txn := NewTransactionService(TxnDeps{Txns: repository.NewTransactionRepo(db), Accounts: r, Cats: repository.NewCategoryRepo(db), Closings: repository.NewClosingRepo(db), Periods: periods})
	m := &fundTestMarket{quotes: []fuyao.NAV{{Date: fday("2026-10-09"), Value: dec("1.2305")}, {Date: fday("2026-10-12"), Value: dec("1.1946")}}}
	s := NewFundService(db, txn, m)
	env := &fundEnv{db: db, svc: s, txn: txn, market: m, accounts: r, now: fday("2026-10-16")}
	s.Now = func() time.Time { return env.now }
	s.BindAccounts(r)
	for i, role := range []model.Role{model.RoleAdmin, model.RoleMember, model.RoleViewer, model.RoleRecorder} {
		u := &model.User{ID: int64(i + 1), Username: fmt.Sprintf("fund-user-%d", i), PasswordHash: "hash", Role: role, Status: model.StatusActive}
		mustOK(t, db.Create(u).Error)
		env.users = append(env.users, u)
		owner := u.ID
		bank := model.Account{Name: "银行卡", Type: model.AccountAsset, AssetClass: "cash", OwnerID: &owner, OpeningBalance: dec("5000"), Status: model.StatusActive}
		fund := model.Account{Name: "基金", Type: model.AccountAsset, AssetClass: "fund", OwnerID: &owner, Status: model.StatusActive}
		mustOK(t, db.Create(&bank).Error)
		mustOK(t, db.Create(&fund).Error)
		env.banks = append(env.banks, bank.ID)
		env.funds = append(env.funds, fund.ID)
	}
	return env
}
func fundOpening(shares, cost, date string) FundInput {
	return FundInput{RequestKey: uuid.NewString(), Code: "023564", Shares: shares, CostPrice: cost, EffectiveDate: date, ValuationBasis: "cny_otc_unit_nav", BasisConfirmed: true}
}
func (e *fundEnv) opening(t *testing.T, who int, shares, cost, date string) FundReceipt {
	t.Helper()
	r, err := e.svc.CreateOpening(context.Background(), e.users[who], e.funds[who], fundOpening(shares, cost, date))
	mustOK(t, err)
	return r
}
func (e *fundEnv) bank(t *testing.T, who int) string {
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

func TestFundOpeningDailyOwnershipAndRetry(t *testing.T) {
	e := newFundEnv(t)
	in := fundOpening("5028.62", "1.4119", "2026-10-09")
	r, err := e.svc.CreateOpening(context.Background(), e.users[0], e.funds[0], in)
	mustOK(t, err)
	v, err := e.svc.Position(e.users[0], r.PositionID)
	mustOK(t, err)
	if v.MarketValue == nil || v.MarketValue.StringFixed(2) != "6007.19" || v.HoldingPnL == nil || v.HoldingPnL.StringFixed(2) != "-1092.72" || v.DailyPnL == nil || v.DailyPnL.StringFixed(2) != "-180.53" {
		t.Fatal(v)
	}
	if e.bank(t, 0) != "5000.00" {
		t.Fatal("opening debited bank")
	}
	again, err := e.svc.CreateOpening(context.Background(), e.users[0], e.funds[0], in)
	mustOK(t, err)
	if again != r {
		t.Fatal("retry duplicated", again, r)
	}
	in.CostPrice = "1.5"
	_, err = e.svc.CreateOpening(context.Background(), e.users[0], e.funds[0], in)
	if !errors.Is(err, ErrFundConflict) {
		t.Fatal(err)
	}
	for _, user := range e.users[1:] {
		if _, err = e.svc.Position(user, r.PositionID); !errors.Is(err, ErrTxnForbidden) {
			t.Fatal(user.Role, err)
		}
	}
	var n int64
	mustOK(t, e.db.Model(&model.FundPosition{}).Count(&n).Error)
	if n != 1 {
		t.Fatal("seeded or duplicate positions", n)
	}
}
func TestFundFirstBuyRedemptionPendingSettlementAndUndo(t *testing.T) {
	e := newFundEnv(t)
	e.market.quotes = []fuyao.NAV{{Date: fday("2026-10-09"), Value: dec("1")}, {Date: fday("2026-10-12"), Value: dec("1.2")}}
	buy := FundInput{RequestKey: uuid.NewString(), Code: "023564", Shares: "990", Amount: "1000", Fee: "10", TradeNAV: "1", EffectiveDate: "2026-10-09", CashDate: "2026-10-09", CashAccountID: e.banks[0], ValuationBasis: "cny_otc_unit_nav", BasisConfirmed: true}
	r, err := e.svc.FirstBuy(context.Background(), e.users[0], e.funds[0], buy)
	mustOK(t, err)
	if e.bank(t, 0) != "4000.00" {
		t.Fatal("buy funds")
	}
	redeem := FundInput{RequestKey: uuid.NewString(), Kind: "redeem", Shares: "495", Amount: "594", Fee: "2", TradeNAV: "1.2", EffectiveDate: "2026-10-12"}
	sale, err := e.svc.Operate(context.Background(), e.users[0], r.PositionID, redeem)
	mustOK(t, err)
	v, err := e.svc.Position(e.users[0], r.PositionID)
	mustOK(t, err)
	if v.Shares.String() != "495" || v.Cost.String() != "500" || v.RealizedPnL.String() != "92" || v.PendingFunds.String() != "592" || v.TotalPnL == nil || v.TotalPnL.String() != "186" {
		t.Fatal(v)
	}
	if _, err = e.svc.Undo(e.users[0], r.PositionID, r.EventID, uuid.NewString()); !errors.Is(err, ErrFundHistory) {
		t.Fatal("early undo", err)
	}
	settlement := FundInput{RequestKey: uuid.NewString(), Kind: "settlement", SourceEventID: sale.EventID, EffectiveDate: "2026-10-15", CashAccountID: e.banks[0]}
	cash, err := e.svc.Operate(context.Background(), e.users[0], r.PositionID, settlement)
	mustOK(t, err)
	v, err = e.svc.Position(e.users[0], r.PositionID)
	mustOK(t, err)
	if !v.PendingFunds.IsZero() || e.bank(t, 0) != "4592.00" {
		t.Fatal(v, e.bank(t, 0))
	}
	settlement.RequestKey = uuid.NewString()
	if _, err = e.svc.Operate(context.Background(), e.users[0], r.PositionID, settlement); !errors.Is(err, ErrFundConflict) {
		t.Fatal("duplicate settlement", err)
	}
	_, err = e.svc.Undo(e.users[0], r.PositionID, cash.EventID, uuid.NewString())
	mustOK(t, err)
	if e.bank(t, 0) != "4000.00" {
		t.Fatal("settlement undo")
	}
	_, err = e.svc.Undo(e.users[0], r.PositionID, sale.EventID, uuid.NewString())
	mustOK(t, err)
	v, err = e.svc.Position(e.users[0], r.PositionID)
	mustOK(t, err)
	if v.Shares.String() != "990" || v.Cost.String() != "1000" {
		t.Fatal(v)
	}
}
func TestFundReadonlyOpeningAndCrossAccountCash(t *testing.T) {
	e := newFundEnv(t)
	r := e.opening(t, 2, "100", "1", "2026-10-09")
	in := FundInput{RequestKey: uuid.NewString(), Kind: "buy", Shares: "100", Amount: "100", TradeNAV: "1", EffectiveDate: "2026-10-12", CashDate: "2026-10-12", CashAccountID: e.banks[2]}
	_, err := e.svc.Operate(context.Background(), e.users[2], r.PositionID, in)
	if !errors.Is(err, ErrTxnForbidden) {
		t.Fatal("readonly funds", err)
	}
	r = e.opening(t, 0, "100", "1", "2026-10-09")
	in.RequestKey = uuid.NewString()
	in.CashAccountID = e.banks[1]
	_, err = e.svc.Operate(context.Background(), e.users[0], r.PositionID, in)
	if !errors.Is(err, ErrTxnForbidden) {
		t.Fatal("foreign cash", err)
	}
	if e.bank(t, 0) != "5000.00" {
		t.Fatal("partial commit")
	}
}
func TestFundDividendReviewAndBusinessDuplicate(t *testing.T) {
	e := newFundEnv(t)
	e.market.quotes = []fuyao.NAV{{Date: fday("2026-10-09"), Value: dec("1.2")}, {Date: fday("2026-10-12"), Value: dec("1.1")}}
	e.market.dividends = []fuyao.Dividend{{Key: "rights-12", RegistrationDate: fday("2026-10-09"), ExDate: fday("2026-10-12"), PerTenCash: dec("1")}}
	r := e.opening(t, 0, "1000", "1.2", "2026-10-09")
	v, err := e.svc.Position(e.users[0], r.PositionID)
	mustOK(t, err)
	if v.DailyStatus != "dividend_review" || v.DailyPnL != nil {
		t.Fatal(v)
	}
	d := FundInput{RequestKey: uuid.NewString(), Kind: "cash_dividend", Amount: "100", EffectiveDate: "2026-10-12", RightsDate: "2026-10-12", RegistrationDate: "2026-10-09"}
	_, err = e.svc.Operate(context.Background(), e.users[0], r.PositionID, d)
	mustOK(t, err)
	v, err = e.svc.Position(e.users[0], r.PositionID)
	mustOK(t, err)
	if v.DailyPnL == nil || !v.DailyPnL.IsZero() || v.Cost.String() != "1200" || v.PendingFunds.String() != "100" || v.TotalPnL == nil || !v.TotalPnL.IsZero() {
		t.Fatal(v)
	}
	d.RequestKey = uuid.NewString()
	_, err = e.svc.Operate(context.Background(), e.users[0], r.PositionID, d)
	if !errors.Is(err, ErrFundConflict) {
		t.Fatal("duplicate rights", err)
	}
}
func TestFundClosedDateRollsBackAndReplayStillWorks(t *testing.T) {
	e := newFundEnv(t)
	r := e.opening(t, 0, "100", "1", "2026-10-09")
	mustOK(t, e.db.Create(&model.PeriodClosing{PeriodKey: "2026-10"}).Error)
	in := FundInput{RequestKey: uuid.NewString(), Kind: "redeem", Shares: "50", Amount: "50", TradeNAV: "1", EffectiveDate: "2026-10-12", CashDate: "2026-10-15", CashAccountID: e.banks[0]}
	_, err := e.svc.Operate(context.Background(), e.users[0], r.PositionID, in)
	if !errors.Is(err, ErrTxnClosedPeriod) {
		t.Fatal(err)
	}
	v, err := e.svc.Position(e.users[0], r.PositionID)
	mustOK(t, err)
	if v.Shares.String() != "100" || e.bank(t, 0) != "5000.00" {
		t.Fatal(v)
	}
	_, err = e.svc.Undo(e.users[0], r.PositionID, r.EventID, uuid.NewString())
	if !errors.Is(err, ErrTxnClosedPeriod) {
		t.Fatal(err)
	}
}
func TestFundQuoteFailurePreservesPreviousValueAndRevision(t *testing.T) {
	e := newFundEnv(t)
	r := e.opening(t, 0, "100", "1", "2026-10-09")
	e.now = e.now.Add(2 * time.Minute)
	e.market.failure = fuyao.ErrRateLimited
	_, err := e.svc.Refresh(context.Background(), e.users[0], e.funds[0])
	mustOK(t, err)
	v, err := e.svc.Position(e.users[0], r.PositionID)
	mustOK(t, err)
	if v.MarketValue == nil || v.MarketValue.String() != "119.46" || v.SourceStatus != "rate_limited" {
		t.Fatal(v)
	}
	e.market.failure = nil
	e.now = e.now.Add(10 * time.Minute)
	e.market.quotes[1].Value = dec("1.2")
	_, err = e.svc.Refresh(context.Background(), e.users[0], e.funds[0])
	mustOK(t, err)
	var n int64
	mustOK(t, e.db.Model(&model.FundNAVRevision{}).Count(&n).Error)
	if n != 1 {
		t.Fatal(n)
	}
	v, err = e.svc.Position(e.users[0], r.PositionID)
	mustOK(t, err)
	if v.MarketValue.String() != "120" {
		t.Fatal(v)
	}
}

func TestFundTotalAssetsHistoryAndLifeStatistics(t *testing.T) {
	e := newFundEnv(t)
	e.opening(t, 0, "5028.62", "1.4119", "2026-10-09")
	owner := e.users[0].ID
	liability := model.Account{Name: "欠款", Type: model.AccountLiability, OwnerID: &owner, OpeningBalance: dec("-1000"), Status: model.StatusActive}
	mustOK(t, e.db.Create(&liability).Error)
	rows, err := e.accounts.ForOwner(owner).List()
	mustOK(t, err)
	total := dec("0")
	for _, a := range rows {
		total = total.Add(a.Balance)
	}
	if total.StringFixed(2) != "10007.19" {
		t.Fatal(total)
	}
	stats := NewStatsService(repository.NewStatsRepo(e.db), e.accounts, repository.NewCategoryRepo(e.db), NewPeriodService(repository.NewSettingRepo(e.db)), nil).ForUser(e.users[0])
	body, _, err := stats.Assets("2026-10-08", "2026-10-12")
	mustOK(t, err)
	var result assetResponse
	mustOK(t, json.Unmarshal(body, &result))
	if result.EndingTotal == nil || result.EndingTotal.StringFixed(2) != "10007.19" {
		t.Fatal(string(body))
	}
	if result.Points[0].Total == nil || result.Points[0].Total.StringFixed(2) != "4000.00" {
		t.Fatal("pre-baseline assets", string(body))
	}
	other := stats.ForUser(e.users[1])
	body, _, err = other.Assets("2026-10-09", "2026-10-12")
	mustOK(t, err)
	mustOK(t, json.Unmarshal(body, &result))
	if result.EndingTotal.StringFixed(2) != "5000.00" {
		t.Fatal("other person's fund leaked", string(body))
	}
	life, err := repository.NewStatsRepo(e.db).ForOwner(owner).SumByPeriod("2026-10")
	mustOK(t, err)
	if !life.Income.IsZero() || !life.Expense.IsZero() {
		t.Fatal(life)
	}
}

func TestFundAmendDisabledAndPaymentGuards(t *testing.T) {
	e := newFundEnv(t)
	r := e.opening(t, 0, "100", "1", "2026-10-13")
	change := fundOpening("200", "1.25", "2026-10-09")
	_, err := e.svc.AmendOpening(context.Background(), e.users[0], r.PositionID, change)
	mustOK(t, err)
	p, err := e.svc.Position(e.users[0], r.PositionID)
	mustOK(t, err)
	if p.Shares.String() != "200" || p.Cost.String() != "250" {
		t.Fatal(p)
	}
	_, err = e.txn.Create(e.users[0], TxnInput{Kind: model.TxnTransfer, AccountID: e.banks[0], TransferAccountID: e.funds[0], Amount: dec("10"), EntryDate: fday("2026-10-12")})
	if !errors.Is(err, ErrFundManaged) {
		t.Fatal(err)
	}
	mustOK(t, e.db.Model(&model.Account{}).Where("id = ?", e.funds[0]).Update("status", model.StatusDisabled).Error)
	buy := FundInput{RequestKey: uuid.NewString(), Kind: "buy", Shares: "10", Amount: "10", TradeNAV: "1", EffectiveDate: "2026-10-12", CashDate: "2026-10-12", CashAccountID: e.banks[0]}
	_, err = e.svc.Operate(context.Background(), e.users[0], r.PositionID, buy)
	if !errors.Is(err, ErrTxnAccountInvalid) {
		t.Fatal(err)
	}
	accountSvc := NewAccountService(e.accounts)
	if err = accountSvc.DeleteFor(e.users[0], e.funds[0]); !errors.Is(err, ErrAccountHasTransactions) {
		t.Fatal(err)
	}
	sell := FundInput{RequestKey: uuid.NewString(), Kind: "redeem", Shares: "10", Amount: "10", TradeNAV: "1", EffectiveDate: "2026-10-12", CashDate: "2026-10-12", CashAccountID: e.banks[0]}
	receipt, err := e.svc.Operate(context.Background(), e.users[0], r.PositionID, sell)
	mustOK(t, err)
	var tx model.Transaction
	mustOK(t, e.db.Where("fund_event_id = ?", receipt.EventID).First(&tx).Error)
	if err = e.txn.Delete(e.users[0], tx.ID); !errors.Is(err, ErrFundManaged) {
		t.Fatal("generic deletion", err)
	}
}

func TestFundHistoricalMissingQuoteIsNotZero(t *testing.T) {
	e := newFundEnv(t)
	e.opening(t, 0, "100", "1", "2026-09-01")
	stats := NewStatsService(repository.NewStatsRepo(e.db), e.accounts, repository.NewCategoryRepo(e.db), NewPeriodService(repository.NewSettingRepo(e.db)), nil).ForUser(e.users[0])
	b, _, err := stats.Assets("2026-09-01", "2026-09-02")
	mustOK(t, err)
	var result assetResponse
	mustOK(t, json.Unmarshal(b, &result))
	if result.EndingTotal != nil || result.Points[0].Total != nil {
		t.Fatal(string(b))
	}
}

func TestFundReinvestmentZeroCostMissingPriceAndBasis(t *testing.T) {
	e := newFundEnv(t)
	e.market.quotes = []fuyao.NAV{{Date: fday("2026-10-09"), Value: dec("1.2")}, {Date: fday("2026-10-12"), Value: dec("1.1")}}
	r := e.opening(t, 0, "1000", "1.2", "2026-10-09")
	in := FundInput{RequestKey: uuid.NewString(), Kind: "reinvest", Shares: "90.90909091", Amount: "100", Fee: "0", TradeNAV: "1.1", RightsDate: "2026-10-12", EffectiveDate: "2026-10-12"}
	_, err := e.svc.Operate(context.Background(), e.users[0], r.PositionID, in)
	mustOK(t, err)
	v, err := e.svc.Position(e.users[0], r.PositionID)
	mustOK(t, err)
	if v.Cost.String() != "1300" || v.Dividends.String() != "100" || v.TotalPnL == nil || !v.TotalPnL.IsZero() || e.bank(t, 0) != "5000.00" {
		t.Fatal(v)
	}
	gift := e.opening(t, 1, "100", "0", "2026-10-09")
	v, err = e.svc.Position(e.users[1], gift.PositionID)
	mustOK(t, err)
	if v.HoldingReturn != nil {
		t.Fatal("zero denominator")
	}
	bad := fundOpening("100", "1", "2026-10-09")
	bad.BasisConfirmed = false
	_, err = e.svc.CreateOpening(context.Background(), e.users[2], e.funds[2], bad)
	if !errors.Is(err, ErrFundCode) {
		t.Fatal(err)
	}
	e.market.failure = fuyao.ErrUnavailable
	fresh := fundOpening("100", "1", "2026-10-09")
	fresh.Code = "012345"
	_, err = e.svc.CreateOpening(context.Background(), e.users[2], e.funds[2], fresh)
	if err == nil {
		t.Fatal("missing price accepted")
	}
	var n int64
	mustOK(t, e.db.Model(&model.FundPosition{}).Where("owner_id = ?", e.users[2].ID).Count(&n).Error)
	if n != 0 {
		t.Fatal("partial missing-price holding")
	}
}

func TestFundSyncWindowsNoNewDateAndOwnAccountIsolation(t *testing.T) {
	e := newFundEnv(t)
	first := e.opening(t, 0, "100", "1", "2026-10-09")
	second := e.opening(t, 1, "200", "1", "2026-10-09")
	if e.market.calls.Load() != 1 || len(e.market.windows) != 1 || e.market.windows[0] != "fyear" {
		t.Fatal(e.market.calls.Load(), e.market.windows)
	}
	e.now = e.now.Add(2 * time.Minute)
	_, err := e.svc.Refresh(context.Background(), e.users[0], e.funds[0])
	mustOK(t, err)
	if len(e.market.windows) != 2 || e.market.windows[1] != "month" {
		t.Fatal(e.market.windows)
	}
	for _, r := range []FundReceipt{first, second} {
		var n int64
		mustOK(t, e.db.Model(&model.FundValuation{}).Where("position_id = ?", r.PositionID).Count(&n).Error)
		if n != 2 {
			t.Fatal("duplicate valuation date", n)
		}
	}
	other, err := e.svc.Accounts(e.users[1])
	mustOK(t, err)
	if len(other) != 1 || len(other[0].Positions) != 1 || other[0].Positions[0].Shares.String() != "200" {
		t.Fatal(other)
	}
	e.now = e.now.Add(40 * 24 * time.Hour)
	_, err = e.svc.Refresh(context.Background(), e.users[0], e.funds[0])
	mustOK(t, err)
	if e.market.windows[len(e.market.windows)-1] != "fyear" {
		t.Fatal(e.market.windows)
	}
}

func TestFundInvestmentFeesExcludedFromAllLifeTotalsAndCashDates(t *testing.T) {
	e := newFundEnv(t)
	e.market.quotes = []fuyao.NAV{{Date: fday("2026-10-09"), Value: dec("1")}, {Date: fday("2026-10-12"), Value: dec("1")}}
	buy := FundInput{RequestKey: uuid.NewString(), Code: "023564", Shares: "990", Amount: "1000", Fee: "10", TradeNAV: "1", EffectiveDate: "2026-10-12", CashDate: "2026-10-10", CashAccountID: e.banks[0], ValuationBasis: "cny_otc_unit_nav", BasisConfirmed: true}
	r, err := e.svc.FirstBuy(context.Background(), e.users[0], e.funds[0], buy)
	mustOK(t, err)
	values, err := e.svc.ValuesForDates(e.db, []int64{e.funds[0]}, []time.Time{fday("2026-10-10"), fday("2026-10-12")})
	mustOK(t, err)
	if values["2026-10-10"][e.funds[0]].Value.String() != "1000" || values["2026-10-12"][e.funds[0]].Value.String() != "990" {
		t.Fatal(values)
	}
	stats := repository.NewStatsRepo(e.db).ForOwner(e.users[0].ID)
	totals, err := stats.SumByPeriod("2026-10")
	mustOK(t, err)
	if !totals.Income.IsZero() || !totals.Expense.IsZero() {
		t.Fatal(totals)
	}
	category, err := stats.CategoryTotals(fday("2026-10-01"), fday("2026-10-31"))
	mustOK(t, err)
	if len(category) != 0 {
		t.Fatal(category)
	}
	var tx model.Transaction
	mustOK(t, e.db.Where("fund_event_id = ?", r.EventID).First(&tx).Error)
	bank, expense := tx.Effects()
	if bank.String() != "-1000" || !expense.IsZero() {
		t.Fatal(bank, expense)
	}
}

func TestFundFinancialWriteFailureRollsBackEveryProjection(t *testing.T) {
	e := newFundEnv(t)
	mustOK(t, e.db.Exec(`CREATE TRIGGER fail_fund_cash BEFORE INSERT ON transactions WHEN NEW.fund_event_id IS NOT NULL BEGIN SELECT RAISE(ABORT,'forced cash failure'); END`).Error)
	in := FundInput{RequestKey: uuid.NewString(), Code: "023564", Shares: "100", Amount: "120", Fee: "0.54", TradeNAV: "1.1946", EffectiveDate: "2026-10-12", CashDate: "2026-10-12", CashAccountID: e.banks[0], ValuationBasis: "cny_otc_unit_nav", BasisConfirmed: true}
	_, err := e.svc.FirstBuy(context.Background(), e.users[0], e.funds[0], in)
	if err == nil {
		t.Fatal("forced failure ignored")
	}
	for _, table := range []any{&model.FundPosition{}, &model.FundEvent{}, &model.FundMutation{}} {
		var n int64
		mustOK(t, e.db.Model(table).Count(&n).Error)
		if n != 0 {
			t.Fatal("partial financial resource", n)
		}
	}
	if e.bank(t, 0) != "5000.00" {
		t.Fatal("cash committed")
	}
}

func TestFundCommittedCashSurvivesSnapshotFailureAndClosedPositionRetries(t *testing.T) {
	e := newFundEnv(t)
	mustOK(t, e.db.Exec(`CREATE TRIGGER fail_fund_snapshot BEFORE INSERT ON fund_valuations BEGIN SELECT RAISE(ABORT,'forced snapshot failure'); END`).Error)
	in := FundInput{RequestKey: uuid.NewString(), Code: "023564", Shares: "100", Amount: "120", Fee: "0.54", TradeNAV: "1.1946", EffectiveDate: "2026-10-12", CashDate: "2026-10-12", CashAccountID: e.banks[0], ValuationBasis: "cny_otc_unit_nav", BasisConfirmed: true}
	r, err := e.svc.FirstBuy(context.Background(), e.users[0], e.funds[0], in)
	mustOK(t, err)
	again, err := e.svc.FirstBuy(context.Background(), e.users[0], e.funds[0], in)
	mustOK(t, err)
	if again != r || e.bank(t, 0) != "4880.00" {
		t.Fatal("snapshot failure duplicated cash", again, r, e.bank(t, 0))
	}
	_, err = e.svc.Valuations(e.users[0], r.PositionID, "", "")
	if err == nil {
		t.Fatal("stale history silently published")
	}
	_, err = e.svc.Operate(context.Background(), e.users[0], r.PositionID, FundInput{RequestKey: uuid.NewString(), Kind: "redeem", Shares: "100", Amount: "119.46", Fee: "1", TradeNAV: "1.1946", EffectiveDate: "2026-10-12", CashDate: "2026-10-12", CashAccountID: e.banks[0]})
	mustOK(t, err)
	p, err := e.svc.repo.Position(r.PositionID, false)
	mustOK(t, err)
	if !p.RebuildPending || e.bank(t, 0) != "4998.46" {
		t.Fatal("repair state or committed cash lost", p, e.bank(t, 0))
	}
	mustOK(t, e.db.Exec("DROP TRIGGER fail_fund_snapshot").Error)
	e.market.failure = fuyao.ErrUnavailable
	calls := e.market.calls.Load()
	result := e.svc.Ensure(context.Background())
	if result.Failed != 0 || e.market.calls.Load() != calls {
		t.Fatal("zero-share repair depended on quote provider", result)
	}
	p, err = e.svc.repo.Position(r.PositionID, false)
	mustOK(t, err)
	rows, err := e.svc.Valuations(e.users[0], r.PositionID, "", "")
	mustOK(t, err)
	if p.RebuildPending || len(rows) != 1 || rows[0].EventVersion != p.EventVersion || !rows[0].Shares.IsZero() || !rows[0].Cost.IsZero() {
		t.Fatal("persisted retry did not rebuild current event version", p, rows)
	}
}

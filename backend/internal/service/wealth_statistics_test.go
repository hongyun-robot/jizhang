package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
	"jizhang/internal/cache"
	"jizhang/internal/model"
	"jizhang/internal/repository"
	"jizhang/internal/suyin"
)

func TestWealthUnknownKnownSubtotalAndScopedStatisticsCache(t *testing.T) {
	e := newWealthEnv(t)
	ctx := context.Background()
	a := e.opening(t, 0, "200", "1.15", "2026-10-09")
	e.opening(t, 1, "100", "1", "2026-10-09")
	_, err := e.svc.Operate(ctx, e.users[0], a.PositionID, WealthInput{RequestKey: uuid.NewString(), Kind: "redeem", Shares: "100", Amount: "130", Fee: "2", EffectiveDate: "2026-10-10"})
	mustOK(t, err)
	rows, err := e.accounts.ForOwner(e.users[0].ID).List()
	mustOK(t, err)
	for _, row := range rows {
		if row.ID == e.wealths[0] && (row.ValuationStatus != "no_price" || row.KnownBalance == nil || row.KnownBalance.String() != "128") {
			t.Fatal(row)
		}
	}
	redis := miniredis.RunT(t)
	client := redisClient(redis)
	defer client.Close()
	stats := NewStatsService(repository.NewStatsRepo(e.db), e.accounts, repository.NewCategoryRepo(e.db), e.txn.deps.Periods, cache.NewStatsCache(client))
	e.svc.OnWrite = stats.InvalidateOwner
	get := func(who int) (assetResponse, bool) {
		t.Helper()
		raw, hit, err := stats.ForUser(e.users[who]).Assets("2026-10-09", "2026-10-16")
		mustOK(t, err)
		var result assetResponse
		mustOK(t, json.Unmarshal(raw, &result))
		return result, hit
	}
	first, _ := get(0)
	if first.EndingTotal != nil || first.Points[len(first.Points)-1].KnownSubtotal.String() != "5128" {
		t.Fatal(first)
	}
	_, _ = get(1)
	_, hit := get(1)
	if !hit {
		t.Fatal("cache not primed")
	}
	_, err = e.svc.WriteManualNAV(e.users[0], 1, WealthManualInput{RequestKey: uuid.NewString(), NAVDate: "2026-10-10", UnitNAV: "1.3", SourceNote: "本人银行核对"})
	mustOK(t, err)
	changed, hit := get(0)
	if hit || changed.EndingTotal == nil || changed.EndingTotal.String() != "5258" {
		t.Fatal(changed, hit)
	}
	unchanged, hit := get(1)
	if !hit || unchanged.EndingTotal != nil {
		t.Fatal("overlay invalidated another owner or leaked price", unchanged, hit)
	}
	e.quote(t, "2026-10-10", "1.3")
	_, hit = get(0)
	if hit {
		t.Fatal("automatic source switch kept old owner cache")
	}
	other, hit := get(1)
	if hit || other.EndingTotal == nil || other.EndingTotal.String() != "5130" {
		t.Fatal(other, hit)
	}
}

func redisClient(server *miniredis.Miniredis) *redis.Client {
	return redis.NewClient(&redis.Options{Addr: server.Addr()})
}

func TestWealthDerivedFailureAndBackdatedSuccessorsAreAtomic(t *testing.T) {
	e := newWealthEnv(t)
	e.quote(t, "2026-10-09", "1")
	ctx := context.Background()
	injected := errors.New("derived valuation failure")
	mustOK(t, e.db.Callback().Create().Before("gorm:create").Register("wealth_derived_failure", func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Table == "wealth_valuations" {
			tx.AddError(injected)
		}
	}))
	input := WealthInput{RequestKey: uuid.NewString(), Code: "J07241", Shares: "100", Amount: "100", TradeNAV: "1", EffectiveDate: "2026-10-09", CashDate: "2026-10-09", CashAccountID: e.banks[0]}
	receipt, err := e.svc.FirstBuy(ctx, e.users[0], e.wealths[0], input)
	mustOK(t, err)
	var p model.WealthPosition
	mustOK(t, e.db.First(&p, receipt.PositionID).Error)
	if !p.RebuildPending || e.bank(t, 0) != "4900.00" {
		t.Fatal(p)
	}
	again, err := e.svc.FirstBuy(ctx, e.users[0], e.wealths[0], input)
	mustOK(t, err)
	if again != receipt {
		t.Fatal(again)
	}
	mustOK(t, e.db.Callback().Create().Remove("wealth_derived_failure"))
	mustOK(t, e.svc.RebuildPosition(receipt.PositionID))
	mustOK(t, e.db.First(&p, receipt.PositionID).Error)
	if p.RebuildPending || e.bank(t, 0) != "4900.00" {
		t.Fatal(p)
	}
	_, err = e.svc.Operate(ctx, e.users[0], p.ID, WealthInput{RequestKey: uuid.NewString(), Kind: "redeem", Shares: "100", Amount: "100", EffectiveDate: "2026-10-12"})
	mustOK(t, err)
	// A backdated disposal would make the later valid disposal oversell.
	_, err = e.svc.Operate(ctx, e.users[0], p.ID, WealthInput{RequestKey: uuid.NewString(), Kind: "redeem", Shares: "1", Amount: "1", EffectiveDate: "2026-10-10"})
	if err == nil {
		t.Fatal("invalid successor accepted")
	}
	var n int64
	mustOK(t, e.db.Model(&model.WealthEvent{}).Count(&n).Error)
	if n != 2 {
		t.Fatal("backdate partly committed", n)
	}
}

func TestWealthFeesAndRightsExcludedFromLivingTotalsAndClosedFacts(t *testing.T) {
	e := newWealthEnv(t)
	ctx := context.Background()
	r, err := e.svc.FirstBuy(ctx, e.users[0], e.wealths[0], WealthInput{RequestKey: uuid.NewString(), Code: "J07241", Shares: "990", Amount: "1000", Fee: "10", TradeNAV: "1", EffectiveDate: "2026-10-09", CashDate: "2026-10-09", CashAccountID: e.banks[0]})
	mustOK(t, err)
	_, err = e.svc.Operate(ctx, e.users[0], r.PositionID, WealthInput{RequestKey: uuid.NewString(), Kind: "cash_dividend", Amount: "100", EffectiveDate: "2026-10-10", RightsDate: "2026-10-10", CashDate: "2026-10-10", CashAccountID: e.banks[0]})
	mustOK(t, err)
	_, err = e.svc.Operate(ctx, e.users[0], r.PositionID, WealthInput{RequestKey: uuid.NewString(), Kind: "reinvest", Amount: "100", Shares: "100", EffectiveDate: "2026-10-11", RightsDate: "2026-10-11"})
	mustOK(t, err)
	_, err = e.svc.Operate(ctx, e.users[0], r.PositionID, WealthInput{RequestKey: uuid.NewString(), Kind: "redeem", Amount: "120", Fee: "2", Shares: "100", EffectiveDate: "2026-10-12", CashDate: "2026-10-13", CashAccountID: e.banks[0]})
	mustOK(t, err)
	stats := repository.NewStatsRepo(e.db).ForOwner(e.users[0].ID)
	totals, err := stats.SumByPeriod("2026-10")
	mustOK(t, err)
	if !totals.Income.IsZero() || !totals.Expense.IsZero() {
		t.Fatal(totals)
	}
	categories, err := stats.CategoryTotals(fday("2026-10-01"), fday("2026-10-31"))
	mustOK(t, err)
	if len(categories) != 0 {
		t.Fatal(categories)
	}
	closing := NewClosingService(ClosingDeps{Closings: repository.NewClosingRepo(e.db), Audits: repository.NewAuditRepo(e.db), Periods: e.txn.deps.Periods, Stats: repository.NewStatsRepo(e.db)})
	e.now = fday("2026-11-02")
	closing.now = e.svc.Now
	_, err = closing.Close(e.users[0], "2026-10")
	mustOK(t, err)
	before, err := e.svc.Position(e.users[0], r.PositionID)
	mustOK(t, err)
	cash := e.bank(t, 0)
	_, err = e.svc.WriteManualNAV(e.users[0], 1, WealthManualInput{RequestKey: uuid.NewString(), NAVDate: "2026-10-12", UnitNAV: "1.1", SourceNote: "已封账日期的真实净值"})
	mustOK(t, err)
	after, err := e.svc.Position(e.users[0], r.PositionID)
	mustOK(t, err)
	if !before.Shares.Equal(after.Shares) || !before.Cost.Equal(after.Cost) || !before.RealizedPnL.Equal(after.RealizedPnL) || !before.Dividends.Equal(after.Dividends) || cash != e.bank(t, 0) {
		t.Fatal("valuation changed closed financial facts")
	}
	totals, err = stats.SumByPeriod("2026-10")
	mustOK(t, err)
	if !totals.Income.IsZero() || !totals.Expense.IsZero() {
		t.Fatal(totals)
	}
}

func TestWealthMultiProductCompletenessAndUnitNAVOnly(t *testing.T) {
	e := newWealthEnv(t)
	e.opening(t, 0, "100", "1", "2026-10-09")
	e.quote(t, "2026-10-10", "1.02")
	product := model.WealthProduct{Issuer: "fixture", RegistrationCode: "fixture-second", Code: "FIXTURE2", Name: "隔离测试产品", Currency: "CNY", ValuationBasis: "cny_wealth_unit_nav", InceptionDate: fday("2025-08-29"), SourceAdapter: "fixture", SourceCode: "fixture-2", AdmissionStatus: "enabled", EvidenceVersion: "isolated-test-only"}
	mustOK(t, e.db.Create(&product).Error)
	p := model.WealthPosition{OwnerID: e.users[0].ID, AccountID: e.wealths[0], ProductID: product.ID, ValuationBasis: "cny_wealth_unit_nav", BasisConfirmedBy: e.users[0].ID, BasisConfirmedAt: e.now, EventVersion: 1}
	mustOK(t, e.db.Create(&p).Error)
	event := model.WealthEvent{OwnerID: p.OwnerID, AccountID: p.AccountID, PositionID: p.ID, Kind: "opening", Shares: dec("10"), Amount: dec("10"), EffectiveDate: fday("2026-10-09"), RightsPositionID: p.ID, CreatedBy: p.OwnerID}
	mustOK(t, e.db.Create(&event).Error)
	account, err := e.svc.Account(e.users[0], e.wealths[0])
	mustOK(t, err)
	if account.Complete || account.TotalValue != nil || account.KnownSubtotal.String() != "102" {
		t.Fatal(account)
	}
	cumulative := dec("99")
	_, err = e.svc.SavePublicNAVs(product.ID, []suyin.NAV{{Date: fday("2026-10-11"), Value: dec("1.1"), TotalValue: &cumulative, FetchedAt: e.now, SourceCode: "fixture-2"}})
	mustOK(t, err)
	account, err = e.svc.Account(e.users[0], e.wealths[0])
	mustOK(t, err)
	if !account.Complete || account.TotalValue.String() != "113" || account.Positions[1].MarketValue.String() != "11" {
		t.Fatal("cumulative NAV used for valuation", account)
	}
}

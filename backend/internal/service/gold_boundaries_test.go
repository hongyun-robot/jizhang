package service

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"jizhang/internal/ccb"
	"jizhang/internal/model"
	"testing"
	"time"
)

func TestGoldOpeningFeesZeroCostAndLifecycle(t *testing.T) {
	g := newGoldEnv(t)
	for _, kind := range []string{"total", "unit"} {
		in := GoldInput{RequestKey: uuid.NewString(), Kind: "opening", Grams: "10", CostMode: kind, Cost: "8000"}
		if kind == "unit" {
			in.Cost = "800"
		}
		e, err := normalizeGold(in, g.now)
		mustOK(t, err)
		if e.CashAmount.String() != "8000" {
			t.Fatal(e)
		}
	}
	fee := "10"
	g.write(t, GoldInput{Kind: "buy", Grams: "10", CashAmount: "8010", Fee: &fee, CashAccountID: g.banks[0]})
	v, e := g.gold.Account(g.users[0], g.ids[0])
	mustOK(t, e)
	if v.Cost.String() != "8010" || v.Fees.String() != "10" || v.CostPrice.String() != "801" {
		t.Fatal(v)
	}
	mustOK(t, g.db.Model(&model.Account{}).Where("id = ?", g.ids[0]).Update("status", model.StatusDisabled).Error)
	in := GoldInput{RequestKey: uuid.NewString(), Kind: "buy", Grams: "1", CashAmount: "800", CashAccountID: g.banks[0]}
	if _, e = g.gold.Write(g.users[0], g.ids[0], in, false); !errors.Is(e, ErrTxnAccountInvalid) {
		t.Fatal(e)
	}
	in.Kind = "sell"
	in.Grams = "10"
	in.CashAmount = "9000"
	in.RequestKey = uuid.NewString()
	_, e = g.gold.Write(g.users[0], g.ids[0], in, false)
	mustOK(t, e)
	v, e = g.gold.Account(g.users[0], g.ids[0])
	mustOK(t, e)
	if v.MarketValue == nil || !v.MarketValue.IsZero() || !v.Cost.IsZero() {
		t.Fatal(v)
	}
	if e = validateAccountInput(CreateAccountInput{Name: "黄金", Type: model.AccountAsset, AssetClass: "gold", OpeningBalance: dec("1")}); e == nil {
		t.Fatal("cash opening allowed")
	}
	if e = NewAccountService(g.accounts).Delete(g.ids[0]); !errors.Is(e, ErrAccountHasTransactions) {
		t.Fatal(e)
	}
}
func TestGoldClosedDatesSuccessorsAndUndo(t *testing.T) {
	g := newGoldEnv(t)
	opening := g.write(t, GoldInput{Kind: "opening", Grams: "10", CostMode: "total", Cost: "8000", TradeDate: "2026-09-01"})
	buy := g.write(t, GoldInput{Kind: "buy", Grams: "1", CashAmount: "900", CashAccountID: g.banks[0], TradeDate: "2026-10-10"})
	if _, e := g.gold.Undo(g.users[0], g.ids[0], opening.EventID, uuid.NewString()); !errors.Is(e, ErrGoldHistory) {
		t.Fatal(e)
	}
	mustOK(t, g.db.Create(&model.PeriodClosing{PeriodKey: "2026-10", Status: model.ClosingClosed, ClosedBy: 1, ClosedAt: g.now}).Error)
	if _, e := g.gold.Undo(g.users[0], g.ids[0], buy.EventID, uuid.NewString()); !errors.Is(e, ErrTxnClosedPeriod) {
		t.Fatal(e)
	}
	in := GoldInput{RequestKey: uuid.NewString(), Kind: "buy", Grams: "1", CashAmount: "900", CashAccountID: g.banks[0], TradeDate: "2026-09-15"}
	if _, e := g.gold.Write(g.users[0], g.ids[0], in, false); !errors.Is(e, ErrTxnClosedPeriod) {
		t.Fatal(e)
	}
	var count int64
	mustOK(t, g.db.Model(&model.GoldEvent{}).Count(&count).Error)
	if count != 2 {
		t.Fatal("partial history persisted")
	}
	// Quotes remain independent of the closed trade date.
	_, e := g.gold.Refresh(context.Background(), g.users[0])
	mustOK(t, e)
}
func TestGoldQuoteRevisionsAndFailureTimeBoundary(t *testing.T) {
	g := newGoldEnv(t)
	first, e := g.gold.Refresh(context.Background(), g.users[0])
	mustOK(t, e)
	g.now = g.now.Add(time.Minute)
	g.marketGold.q.Sell = dec("894")
	revised, e := g.gold.Refresh(context.Background(), g.users[0])
	mustOK(t, e)
	if revised.Quote.RevisionOf == nil || *revised.Quote.RevisionOf != first.Quote.ID {
		t.Fatal("revision lost")
	}
	g.now = g.now.Add(time.Minute)
	g.marketGold.q.At = g.marketGold.q.At.Add(-time.Minute)
	stale, e := g.gold.Refresh(context.Background(), g.users[0])
	mustOK(t, e)
	if stale.Quote.ID != revised.Quote.ID || stale.Error == "" {
		t.Fatal("old quote replaced current")
	}
	g.now = revised.LastSuccessAt.Add(2*time.Hour - time.Second)
	stale, e = g.gold.QuoteView()
	mustOK(t, e)
	if stale.Status == "update_error" {
		t.Fatal("early alarm")
	}
	g.now = g.now.Add(time.Second)
	stale, e = g.gold.QuoteView()
	mustOK(t, e)
	if stale.Status != "update_error" {
		t.Fatal("missing alarm")
	}
}
func TestGoldNoInitialPriceFailureAlarm(t *testing.T) {
	g := newGoldEnv(t)
	g.marketGold.failure = ccb.ErrUnavailable
	_, e := g.gold.Refresh(context.Background(), g.users[0])
	mustOK(t, e)
	g.now = g.now.Add(2 * time.Hour)
	v, e := g.gold.QuoteView()
	mustOK(t, e)
	if v.Status != "update_error" || v.Quote != nil {
		t.Fatal(v)
	}
}
func TestGoldNoWorkerActivityAndUndoCashOwnership(t *testing.T) {
	g := newGoldEnv(t)
	first := g.write(t, GoldInput{Kind: "buy", Grams: "1", CashAmount: "800", CashAccountID: g.banks[0]})
	_, e := g.gold.Refresh(context.Background(), g.users[0])
	mustOK(t, e)
	g.now = g.now.Add(2 * time.Hour)
	view, e := g.gold.QuoteView()
	mustOK(t, e)
	if view.Status != "update_error" {
		t.Fatal("missing stale cache alarm")
	}
	owner := g.users[1].ID
	mustOK(t, g.db.Model(&model.Account{}).Where("id = ?", g.banks[0]).Update("owner_id", owner).Error)
	if _, e = g.gold.Undo(g.users[0], g.ids[0], first.EventID, uuid.NewString()); !errors.Is(e, ErrTxnForbidden) {
		t.Fatal("undo crossed resource ownership", e)
	}
	owner = g.users[0].ID
	mustOK(t, g.db.Model(&model.Account{}).Where("id = ?", g.banks[0]).Updates(map[string]any{"owner_id": owner, "status": model.StatusDisabled}).Error)
	_, e = g.gold.Undo(g.users[0], g.ids[0], first.EventID, uuid.NewString())
	mustOK(t, e)
}
func TestGoldOpeningAmendmentDependency(t *testing.T) {
	g := newGoldEnv(t)
	g.write(t, GoldInput{Kind: "opening", Grams: "10", CostMode: "unit", Cost: "800"})
	in := GoldInput{RequestKey: uuid.NewString(), Kind: "opening", Grams: "10", CostMode: "total", Cost: "8100"}
	_, e := g.gold.Write(g.users[0], g.ids[0], in, true)
	mustOK(t, e)
	g.write(t, GoldInput{Kind: "buy", Grams: "1", CashAmount: "900", CashAccountID: g.banks[0]})
	in.RequestKey = uuid.NewString()
	if _, e = g.gold.Write(g.users[0], g.ids[0], in, true); !errors.Is(e, ErrGoldHistory) {
		t.Fatal(e)
	}
}
func TestGoldBackdatingCannotChangeClosedCarryWithoutTrades(t *testing.T) {
	g := newGoldEnv(t)
	mustOK(t, g.db.Create(&model.PeriodClosing{PeriodKey: "2026-09", Status: model.ClosingClosed, ClosedBy: 1, ClosedAt: g.now}).Error)
	_, e := g.gold.Write(g.users[0], g.ids[0], GoldInput{RequestKey: uuid.NewString(), Kind: "opening", Grams: "1", CostMode: "total", Cost: "800", TradeDate: "2026-08-01"}, false)
	if !errors.Is(e, ErrTxnClosedPeriod) {
		t.Fatal("backdated opening changed closed carry", e)
	}
}
func TestGoldInputPrecisionAndDefaultDateRetry(t *testing.T) {
	g := newGoldEnv(t)
	in := GoldInput{RequestKey: uuid.NewString(), Kind: "buy", Grams: "1", CashAmount: "800", CashAccountID: g.banks[0]}
	first, e := g.gold.Write(g.users[0], g.ids[0], in, false)
	mustOK(t, e)
	g.now = g.now.Add(24 * time.Hour)
	again, e := g.gold.Write(g.users[0], g.ids[0], in, false)
	mustOK(t, e)
	if first != again {
		t.Fatal("default date changed retry identity")
	}
	in.RequestKey = uuid.NewString()
	in.CashAmount = "800.000"
	if _, e := g.gold.Write(g.users[0], g.ids[0], in, false); !errors.Is(e, ErrGoldInvalid) {
		t.Fatal("excess zero precision accepted", e)
	}
	in = GoldInput{RequestKey: uuid.NewString(), Kind: "opening", Grams: "9999999999", CostMode: "unit", Cost: "9999999999"}
	if _, e := normalizeGold(in, g.now); e == nil {
		t.Fatal("overflow accepted")
	}
}

type blockedGoldMarket struct {
	entered, release chan struct{}
	quote            ccb.Quote
}

func (b *blockedGoldMarket) Quote(context.Context) (ccb.Quote, error) {
	close(b.entered)
	<-b.release
	return b.quote, nil
}
func TestGoldLeaseFencingRejectsLateWorker(t *testing.T) {
	g := newGoldEnv(t)
	b := &blockedGoldMarket{make(chan struct{}), make(chan struct{}), g.marketGold.q}
	g.gold.market = b
	done := make(chan error, 1)
	go func() { _, e := g.gold.Refresh(context.Background(), g.users[0]); done <- e }()
	<-b.entered
	g.now = g.now.Add(61 * time.Second)
	fresh := NewGoldService(g.db, g.txn, g.marketGold)
	fresh.Now = g.gold.Now
	g.marketGold.q.At = g.now
	g.marketGold.q.Sell = dec("910")
	v, e := fresh.Refresh(context.Background(), g.users[0])
	mustOK(t, e)
	close(b.release)
	mustOK(t, <-done)
	last, e := g.gold.QuoteView()
	mustOK(t, e)
	if last.Quote.ID != v.Quote.ID || last.Quote.SellPrice.String() != "910" {
		t.Fatal("expired worker overwrote current quote")
	}
}

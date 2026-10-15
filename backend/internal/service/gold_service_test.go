package service

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"jizhang/internal/ccb"
	"jizhang/internal/model"
	"jizhang/internal/repository"
	"sync/atomic"
	"testing"
	"time"
)

type goldTestMarket struct {
	q       ccb.Quote
	failure error
	calls   atomic.Int64
}

func (m *goldTestMarket) Quote(context.Context) (ccb.Quote, error) {
	m.calls.Add(1)
	return m.q, m.failure
}

type goldEnv struct {
	*fundEnv
	gold       *GoldService
	marketGold *goldTestMarket
	ids        []int64
}

func newGoldEnv(t *testing.T) *goldEnv {
	f := newFundEnv(t)
	mustOK(t, f.db.AutoMigrate(&model.GoldPosition{}, &model.GoldEvent{}, &model.GoldMutation{}, &model.GoldAudit{}, &model.GoldQuoteObservation{}, &model.GoldQuoteSync{}, &model.GoldValuation{}))
	f.now = time.Date(2026, 10, 16, 12, 0, 0, 0, ccb.Shanghai)
	m := &goldTestMarket{q: ccb.Quote{Buy: dec("899.04"), Sell: dec("893.04"), At: f.now}}
	s := NewGoldService(f.db, f.txn, m)
	s.Now = func() time.Time { return f.now }
	s.BindAccounts(f.accounts)
	e := &goldEnv{fundEnv: f, gold: s, marketGold: m}
	for _, u := range f.users {
		owner := u.ID
		a := model.Account{Name: "建行积存金", OwnerID: &owner, Type: model.AccountAsset, AssetClass: "gold", Status: model.StatusActive}
		mustOK(t, f.db.Create(&a).Error)
		e.ids = append(e.ids, a.ID)
	}
	return e
}
func (g *goldEnv) write(t *testing.T, in GoldInput) GoldReceipt {
	t.Helper()
	if in.RequestKey == "" {
		in.RequestKey = uuid.NewString()
	}
	r, e := g.gold.Write(g.users[0], g.ids[0], in, false)
	mustOK(t, e)
	return r
}
func TestGoldAccountingNoQuoteAndCash(t *testing.T) {
	g := newGoldEnv(t)
	r := g.write(t, GoldInput{Kind: "opening", Grams: "10", CostMode: "unit", Cost: "800", TradeDate: "2026-10-14"})
	v, e := g.gold.Account(g.users[0], g.ids[0])
	mustOK(t, e)
	if v.MarketValue != nil || v.Cost.String() != "8000" || g.bank(t, 0) != "5000.00" {
		t.Fatal(v)
	}
	accounts, e := g.accounts.ForOwner(1).List()
	mustOK(t, e)
	for _, a := range accounts {
		if a.ID == g.ids[0] {
			b, _ := json.Marshal(a)
			var m map[string]any
			mustOK(t, json.Unmarshal(b, &m))
			if m["balance"] != nil {
				t.Fatal(string(b))
			}
		}
	}
	_, e = g.gold.Refresh(context.Background(), g.users[0])
	mustOK(t, e)
	v, e = g.gold.Account(g.users[0], g.ids[0])
	mustOK(t, e)
	if v.MarketValue.String() != "8930.4" || v.HoldingPnL.String() != "930.4" {
		t.Fatal(v)
	}
	g.write(t, GoldInput{Kind: "buy", Grams: "10", CashAmount: "9000", CashAccountID: g.banks[0], TradeDate: "2026-10-15"})
	key := uuid.NewString()
	fee := "10"
	sell := GoldInput{RequestKey: key, Kind: "sell", Grams: "5", CashAmount: "4500", Fee: &fee, CashAccountID: g.banks[0], TradeDate: "2026-10-16"}
	r, e = g.gold.Write(g.users[0], g.ids[0], sell, false)
	mustOK(t, e)
	again, e := g.gold.Write(g.users[0], g.ids[0], sell, false)
	mustOK(t, e)
	if again != r {
		t.Fatal("retry changed receipt")
	}
	v, e = g.gold.Account(g.users[0], g.ids[0])
	mustOK(t, e)
	if v.Grams.String() != "15" || v.Cost.String() != "12750" || v.RealizedPnL.String() != "250" || g.bank(t, 0) != "500.00" {
		t.Fatal(v, g.bank(t, 0))
	}
	sell.CashAmount = "4501"
	if _, e = g.gold.Write(g.users[0], g.ids[0], sell, false); !errors.Is(e, ErrGoldConflict) {
		t.Fatal(e)
	}
	_, e = g.gold.Undo(g.users[0], g.ids[0], r.EventID, uuid.NewString())
	mustOK(t, e)
	v, e = g.gold.Account(g.users[0], g.ids[0])
	mustOK(t, e)
	if v.Grams.String() != "20" || v.Cost.String() != "17000" || !v.RealizedPnL.IsZero() {
		t.Fatal(v)
	}
}
func TestGoldBoundariesAndRollback(t *testing.T) {
	g := newGoldEnv(t)
	r := g.write(t, GoldInput{Kind: "buy", Grams: "10", CashAmount: "8010", CashAccountID: g.banks[0]})
	if g.bank(t, 0) != "-3010.00" {
		t.Fatal(g.bank(t, 0))
	}
	for _, u := range []*model.User{g.users[1], g.users[2]} {
		if _, e := g.gold.Account(u, g.ids[0]); !errors.Is(e, ErrTxnForbidden) {
			t.Fatal(e)
		}
	}
	in := GoldInput{RequestKey: uuid.NewString(), Kind: "buy", Grams: "1", CashAmount: "800", CashAccountID: g.banks[2]}
	if _, e := g.gold.Write(g.users[2], g.ids[2], in, false); !errors.Is(e, ErrTxnForbidden) {
		t.Fatal(e)
	}
	in.CashAccountID = g.banks[1]
	if _, e := g.gold.Write(g.users[0], g.ids[0], in, false); !errors.Is(e, ErrTxnForbidden) {
		t.Fatal(e)
	}
	in.CashAccountID = g.banks[0]
	in.Kind = "sell"
	in.Grams = "11"
	if _, e := g.gold.Write(g.users[0], g.ids[0], in, false); e == nil {
		t.Fatal("oversell")
	}
	var n int64
	mustOK(t, g.db.Model(&model.GoldEvent{}).Count(&n).Error)
	if n != 1 {
		t.Fatal("partial event", n)
	}
	if e := g.txn.Delete(g.users[0], 1); !errors.Is(e, ErrGoldManaged) {
		t.Fatal(e)
	}
	_, e := g.gold.Undo(g.users[0], g.ids[0], r.EventID, uuid.NewString())
	mustOK(t, e)
	if yes, e := g.accounts.HasTransactions(g.ids[0]); e != nil || !yes {
		t.Fatal("history lost", e)
	}
	for _, in := range []GoldInput{{Kind: "opening", Grams: "1.000000001", CostMode: "total", Cost: "0"}, {Kind: "buy", Grams: "NaN", CashAmount: "1", CashAccountID: 1}, {Kind: "buy", Grams: "1", CashAmount: "800", CashAccountID: 1, TradeDate: "2026-10-15", CashDate: "2026-10-16"}} {
		in.RequestKey = uuid.NewString()
		if _, e := normalizeGold(in, g.now); e == nil {
			t.Fatal(in)
		}
	}
}
func TestGoldScheduleCacheHistoryAndProjectionRecovery(t *testing.T) {
	g := newGoldEnv(t)
	g.write(t, GoldInput{Kind: "opening", Grams: "10", CostMode: "total", Cost: "8000", TradeDate: "2026-10-14"})
	mustOK(t, g.gold.Ensure(context.Background()))
	mustOK(t, g.gold.Ensure(context.Background()))
	if g.marketGold.calls.Load() != 1 {
		t.Fatal("same hour repeated")
	}
	for i := 0; i < 3; i++ {
		_, e := g.gold.QuoteView()
		mustOK(t, e)
	}
	if g.marketGold.calls.Load() != 1 {
		t.Fatal("GET fetched")
	}
	_, e := g.gold.Refresh(context.Background(), g.users[0])
	mustOK(t, e)
	if g.marketGold.calls.Load() != 1 {
		t.Fatal("cooldown ignored")
	}
	g.now = g.now.Add(31 * time.Second)
	_, e = g.gold.Refresh(context.Background(), g.users[0])
	mustOK(t, e)
	if g.marketGold.calls.Load() != 2 {
		t.Fatal("manual did not bypass hour")
	}
	values, e := g.gold.ValuesForDates(g.db, []int64{g.ids[0]}, []time.Time{fday("2026-10-14"), fday("2026-10-16")})
	mustOK(t, e)
	if values["2026-10-14"][g.ids[0]].Complete || !values["2026-10-16"][g.ids[0]].Complete {
		t.Fatal(values)
	}
	g.now = g.now.Add(2 * time.Hour)
	g.marketGold.failure = ccb.ErrUnavailable
	v, e := g.gold.Refresh(context.Background(), g.users[0])
	mustOK(t, e)
	if v.Status != "update_error" || v.Quote == nil {
		t.Fatal(v)
	}
	g.marketGold.failure = nil
	g.now = g.now.Add(time.Minute)
	v, e = g.gold.Refresh(context.Background(), g.users[0])
	mustOK(t, e)
	if v.Status == "update_error" || v.Quote == nil || !v.Quote.QuoteAt.Equal(g.marketGold.q.At) {
		t.Fatal(v)
	}
	mustOK(t, g.db.Exec(`CREATE TRIGGER fail_gold_projection BEFORE INSERT ON gold_valuations BEGIN SELECT RAISE(ABORT,'forced projection failure'); END`).Error)
	r := g.write(t, GoldInput{Kind: "buy", Grams: "1", CashAmount: "800", CashAccountID: g.banks[0]})
	if r.EventID == 0 {
		t.Fatal("receipt lost")
	}
	p, e := repository.NewGoldRepo(g.db).Position(g.ids[0])
	mustOK(t, e)
	if !p.RebuildPending {
		t.Fatal("lost retry marker")
	}
	mustOK(t, g.db.Exec(`DROP TRIGGER fail_gold_projection`).Error)
	_, e = g.gold.Valuations(g.users[0], g.ids[0], "", "")
	mustOK(t, e)
	p, e = repository.NewGoldRepo(g.db).Position(g.ids[0])
	mustOK(t, e)
	if p.RebuildPending {
		t.Fatal("rebuild not recovered")
	}
}

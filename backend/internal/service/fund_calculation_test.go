package service

import (
	"jizhang/internal/model"
	"testing"
	"time"
)

func fday(s string) time.Time {
	d, e := time.Parse("2006-01-02", s)
	if e != nil {
		panic(e)
	}
	return d
}
func fptr(s string) *time.Time { d := fday(s); return &d }
func TestFundCostsAndSettlement(t *testing.T) {
	events := []model.FundEvent{{ID: 1, Kind: "opening", Shares: dec("100"), Amount: dec("120"), EffectiveDate: fday("2026-10-09")}, {ID: 2, Kind: "buy", Shares: dec("100"), Amount: dec("110"), Fee: dec("0"), EffectiveDate: fday("2026-10-12"), CashDate: fptr("2026-10-10")}}
	s, e := replayFund(events, fday("2026-10-10"))
	mustOK(t, e)
	if s.Shares.String() != "100" || s.Cash.String() != "110" {
		t.Fatal(s)
	}
	s, e = replayFund(events, fday("2026-10-12"))
	mustOK(t, e)
	if s.Shares.String() != "200" || s.Cost.String() != "230" || !s.Cash.IsZero() {
		t.Fatal(s)
	}
	events = append(events, model.FundEvent{ID: 3, Kind: "redeem", Shares: dec("100"), Amount: dec("130"), Fee: dec("2"), EffectiveDate: fday("2026-10-13"), CashDate: fptr("2026-10-15")})
	s, e = replayFund(events, fday("2026-10-13"))
	mustOK(t, e)
	if s.Shares.String() != "100" || s.Cost.String() != "115" || s.Realized.String() != "13" || s.Cash.String() != "128" {
		t.Fatal(s)
	}
	s, e = replayFund(events, fday("2026-10-15"))
	mustOK(t, e)
	if !s.Cash.IsZero() {
		t.Fatal(s)
	}
	events = append(events, model.FundEvent{ID: 4, Kind: "redeem", Shares: dec("100"), Amount: dec("130"), EffectiveDate: fday("2026-10-16")})
	s, e = replayFund(events, fday("2026-10-16"))
	mustOK(t, e)
	if !s.Shares.IsZero() || !s.Cost.IsZero() {
		t.Fatal("full redemption tail", s)
	}
}
func TestFundDailyUsesHistoricalShares(t *testing.T) {
	events := []model.FundEvent{{Kind: "opening", Shares: dec("5028.62"), Amount: dec("7099.908578"), EffectiveDate: fday("2026-10-09")}}
	navs := []model.FundNAV{{NAVDate: fday("2026-10-09"), UnitNAV: dec("1.2305")}, {NAVDate: fday("2026-10-12"), UnitNAV: dec("1.1946")}}
	p, status, e := fundDaily(events, navs, 1, nil)
	mustOK(t, e)
	if p == nil || p.String() != "-180.53" || status != "ready" {
		t.Fatal(p, status)
	}
	events[0].EffectiveDate = fday("2026-10-13")
	p, status, e = fundDaily(events, navs, 1, nil)
	mustOK(t, e)
	if p != nil || status != "no_history" {
		t.Fatal(p, status)
	}
	events = []model.FundEvent{{Kind: "opening", Shares: dec("100"), Amount: dec("100"), EffectiveDate: fday("2026-10-09")}, {Kind: "buy", Shares: dec("100"), Amount: dec("120"), EffectiveDate: fday("2026-10-12")}}
	navs[0].UnitNAV = dec("1")
	navs[1].UnitNAV = dec("1.2")
	p, _, e = fundDaily(events, navs, 1, nil)
	mustOK(t, e)
	if p == nil || p.String() != "20" {
		t.Fatal("final quantity applied to whole interval", p)
	}
}
func TestFundDividendAndFeeNotDoubleCounted(t *testing.T) {
	events := []model.FundEvent{{Kind: "opening", Shares: dec("1000"), Amount: dec("1200"), EffectiveDate: fday("2026-10-09")}, {Kind: "cash_dividend", Amount: dec("100"), EffectiveDate: fday("2026-10-12"), RightsDate: fptr("2026-10-12"), CashDate: fptr("2026-10-15")}}
	navs := []model.FundNAV{{NAVDate: fday("2026-10-09"), UnitNAV: dec("1.2")}, {NAVDate: fday("2026-10-12"), UnitNAV: dec("1.1")}}
	p, _, e := fundDaily(events, navs, 1, nil)
	mustOK(t, e)
	if p == nil || !p.IsZero() {
		t.Fatal(p)
	}
	s, e := replayFund(events, fday("2026-10-12"))
	mustOK(t, e)
	if s.Cash.String() != "100" || s.Cost.String() != "1200" || s.Dividends.String() != "100" {
		t.Fatal(s)
	}
	events[1].Kind = "reinvest"
	events[1].Shares = dec("100")
	events[1].TradeNAV = dec("1")
	events[1].CashDate = nil
	s, e = replayFund(events, fday("2026-10-12"))
	mustOK(t, e)
	if s.Shares.String() != "1100" || s.Cost.String() != "1300" || !s.Cash.IsZero() {
		t.Fatal(s)
	}
	events = []model.FundEvent{{Kind: "buy", Shares: dec("990"), Amount: dec("1000"), Fee: dec("10"), EffectiveDate: fday("2026-10-12"), CashDate: fptr("2026-10-12")}}
	navs[0].UnitNAV = dec("1")
	navs[1].UnitNAV = dec("1")
	p, _, e = fundDaily(events, navs, 1, nil)
	mustOK(t, e)
	if p == nil || p.String() != "-10" {
		t.Fatal(p)
	}
}

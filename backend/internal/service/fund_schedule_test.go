package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/robfig/cron/v3"

	"jizhang/internal/fuyao"
	"jizhang/internal/model"
)

func fundClock(value string) time.Time {
	t, err := time.ParseInLocation("2006-01-02 15:04:05", value, fuyao.Shanghai)
	if err != nil {
		panic(err)
	}
	return t
}

func TestFundAutomaticRunClockBoundaries(t *testing.T) {
	for _, tc := range []struct {
		now, target, slot string
		morning           bool
	}{
		{"2026-10-12 18:59:59", "", "", false},
		{"2026-10-12 19:00:00", "2026-10-12", "19:00", false},
		{"2026-10-12 19:29:59", "2026-10-12", "19:00", false},
		{"2026-10-12 19:30:00", "2026-10-12", "19:30", false},
		{"2026-10-12 22:59:59", "2026-10-12", "22:30", false},
		{"2026-10-12 23:00:00", "2026-10-12", "23:00", false},
		{"2026-10-12 23:01:00", "", "", false},
		{"2026-10-13 03:59:59", "", "", false},
		{"2026-10-13 04:00:00", "2026-10-12", "04:00", true},
		{"2026-10-13 04:29:59", "2026-10-12", "04:00", true},
		{"2026-10-13 04:30:00", "", "", false},
		{"2026-10-17 04:00:00", "2026-10-16", "04:00", true},
		{"2026-10-17 19:00:00", "", "", false},
		{"2026-10-18 04:00:00", "", "", false},
		{"2026-10-19 04:00:00", "", "", false},
	} {
		t.Run(tc.now, func(t *testing.T) {
			// Call with UTC to ensure the host timezone cannot alter the window.
			run, active := fundRunAt(fundClock(tc.now).UTC())
			if active != (tc.target != "") {
				t.Fatal(run, active)
			}
			if active && (run.Morning != tc.morning || run.TargetDate.Format("2006-01-02") != tc.target || run.SlotStart.Format("15:04") != tc.slot) {
				t.Fatal(run)
			}
		})
	}
}

func TestFundCronProducesEveningFinalAndMorningChecks(t *testing.T) {
	for _, tc := range []struct{ schedule, from, next string }{
		{FundEveningCron, "2026-10-12 18:59:59", "2026-10-12 19:00:00"},
		{FundEveningCron, "2026-10-12 19:00:00", "2026-10-12 19:30:00"},
		{FundEveningCron, "2026-10-16 22:30:00", "2026-10-19 19:00:00"},
		{FundLastEveningCron, "2026-10-16 22:59:59", "2026-10-16 23:00:00"},
		{FundMorningCron, "2026-10-16 23:00:00", "2026-10-17 04:00:00"},
	} {
		schedule, err := cron.ParseStandard(tc.schedule)
		mustOK(t, err)
		if next := schedule.Next(fundClock(tc.from)); !next.Equal(fundClock(tc.next)) {
			t.Fatal(tc, next)
		}
	}
}

func TestFundAutomaticNoNewQuoteEndsAt23AndMorningAttemptsOnce(t *testing.T) {
	e := newFundEnv(t)
	e.opening(t, 0, "100", "1", "2026-10-09")
	initial := e.market.calls.Load()
	for _, clock := range []string{"19:00:00", "19:30:00", "20:00:00", "20:30:00", "21:00:00", "21:30:00", "22:00:00", "22:30:00", "23:00:00"} {
		e.now = fundClock("2026-10-16 " + clock)
		before := e.market.calls.Load()
		if result := e.svc.Ensure(context.Background()); result.Failed != 0 {
			t.Fatal(result)
		}
		e.now = e.now.Add(10 * time.Second)
		e.svc.Ensure(context.Background())
		if e.market.calls.Load() != before+1 {
			t.Fatal("missing or duplicate half-hour fetch", clock, e.market.calls.Load(), before)
		}
	}
	if e.market.calls.Load() != initial+9 {
		t.Fatal("evening attempts")
	}
	for _, now := range []string{"2026-10-16 23:01:00", "2026-10-17 00:00:00", "2026-10-17 03:59:59"} {
		e.now = fundClock(now)
		e.svc.Ensure(context.Background())
	}
	if e.market.calls.Load() != initial+9 {
		t.Fatal("out-of-window network call")
	}
	e.market.failure = fuyao.ErrRateLimited
	e.now = fundClock("2026-10-17 04:00:00")
	if result := e.svc.Ensure(context.Background()); result.Failed != 1 {
		t.Fatal(result)
	}
	e.now = fundClock("2026-10-17 04:10:00")
	e.svc = NewFundService(e.db, e.txn, e.market)
	e.svc.Now = func() time.Time { return e.now }
	e.svc.Ensure(context.Background())
	if e.market.calls.Load() != initial+10 {
		t.Fatal("morning failure repeated after restart")
	}
	var item model.FundInstrument
	mustOK(t, e.db.First(&item).Error)
	if !fundSameDay(item.MorningAttemptedFor, fday("2026-10-16")) {
		t.Fatal("morning target was fetch day rather than yesterday", item)
	}
}

type fundScheduleMarket struct {
	*fundTestMarket
	mu     sync.Mutex
	byCode map[string][]fuyao.NAV
	count  map[string]int
}

func (m *fundScheduleMarket) NAVs(_ context.Context, code, _ string) ([]fuyao.NAV, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.count[code]++
	rows, exists := m.byCode[code]
	if !exists {
		rows = m.quotes
	}
	return append([]fuyao.NAV(nil), rows...), nil
}

func TestFundAutomaticPerCodeStopRestartAndIndependentMorningRevision(t *testing.T) {
	e := newFundEnv(t)
	m := &fundScheduleMarket{fundTestMarket: e.market, byCode: map[string][]fuyao.NAV{}, count: map[string]int{}}
	e.svc.market = m
	first := e.opening(t, 0, "100", "1", "2026-10-09")
	in := fundOpening("10", "1", "2026-10-09")
	in.Code = "012345"
	_, err := e.svc.CreateOpening(context.Background(), e.users[0], e.funds[0], in)
	mustOK(t, err)
	// Equal NAV on a newer date is still a new daily quote.
	m.byCode["023564.OF"] = append(append([]fuyao.NAV(nil), m.quotes...), fuyao.NAV{Date: fday("2026-10-16"), Value: dec("1.1946")})
	e.now = fundClock("2026-10-16 19:00:00")
	e.svc.Ensure(context.Background())
	fast, slow := m.count["023564.OF"], m.count["012345.OF"]
	e.now = fundClock("2026-10-16 19:30:00")
	e.svc = NewFundService(e.db, e.txn, m)
	e.svc.Now = func() time.Time { return e.now }
	e.svc.Ensure(context.Background())
	if m.count["023564.OF"] != fast || m.count["012345.OF"] != slow+1 {
		t.Fatal("completion was global or lost on restart", m.count)
	}
	m.byCode["012345.OF"] = append(append([]fuyao.NAV(nil), m.quotes...), fuyao.NAV{Date: fday("2026-10-16"), Value: dec("1.2")})
	e.now = fundClock("2026-10-16 20:00:00")
	e.svc.Ensure(context.Background())
	slow = m.count["012345.OF"]
	e.now = fundClock("2026-10-16 23:00:00")
	e.svc.Ensure(context.Background())
	if m.count["023564.OF"] != fast || m.count["012345.OF"] != slow {
		t.Fatal("completed code fetched again", m.count)
	}
	m.byCode["023564.OF"][2].Value = dec("1.3")
	// The morning correction targets yesterday; an upstream today row cannot
	// shift this run's daily valuation date into today.
	m.byCode["023564.OF"] = append(m.byCode["023564.OF"], fuyao.NAV{Date: fday("2026-10-17"), Value: dec("9")})
	e.now = fundClock("2026-10-17 04:00:00")
	e.svc.Ensure(context.Background())
	if m.count["023564.OF"] != fast+1 || m.count["012345.OF"] != slow+1 {
		t.Fatal("morning skipped evening completion", m.count)
	}
	e.now = fundClock("2026-10-17 04:20:00")
	e.svc.Ensure(context.Background())
	if m.count["023564.OF"] != fast+1 || m.count["012345.OF"] != slow+1 {
		t.Fatal("morning repeated", m.count)
	}
	v, err := e.svc.Position(e.users[0], first.PositionID)
	mustOK(t, err)
	if v.NAVDate != "2026-10-16" || v.MarketValue.String() != "130" || e.bank(t, 0) != "5000.00" {
		t.Fatal("morning revision changed dates or cash", v)
	}
}

func TestFundCachedTodayAndManualRefreshOutsideSchedule(t *testing.T) {
	e := newFundEnv(t)
	e.market.quotes = append(e.market.quotes, fuyao.NAV{Date: fday("2026-10-16"), Value: dec("1.2")})
	r := e.opening(t, 0, "100", "1", "2026-10-09")
	before := e.market.calls.Load()
	e.now = fundClock("2026-10-16 19:00:00")
	e.svc.Ensure(context.Background())
	e.now = fundClock("2026-10-16 20:00:00")
	e.svc.Ensure(context.Background())
	if e.market.calls.Load() != before {
		t.Fatal("already-current quote fetched")
	}
	e.now = fundClock("2026-10-17 12:00:00")
	e.svc.Ensure(context.Background())
	if e.market.calls.Load() != before {
		t.Fatal("weekend daytime fetched")
	}
	e.market.quotes[2].Value = dec("1.25")
	_, err := e.svc.Refresh(context.Background(), e.users[0], e.funds[0])
	mustOK(t, err)
	v, err := e.svc.Position(e.users[0], r.PositionID)
	mustOK(t, err)
	if e.market.calls.Load() != before+1 || v.MarketValue.String() != "125" {
		t.Fatal("explicit manual refresh was suppressed", v)
	}
}

func TestFundEveningStopsOnlyAfterValuationCompletes(t *testing.T) {
	e := newFundEnv(t)
	r := e.opening(t, 0, "100", "1", "2026-10-09")
	mustOK(t, e.db.Exec(`CREATE TRIGGER fail_evening_snapshot BEFORE INSERT ON fund_valuations BEGIN SELECT RAISE(ABORT,'forced snapshot failure'); END`).Error)
	e.market.quotes = append(e.market.quotes, fuyao.NAV{Date: fday("2026-10-16"), Value: dec("1.2")})
	e.now = fundClock("2026-10-16 19:00:00")
	e.svc.Ensure(context.Background())
	var item model.FundInstrument
	mustOK(t, e.db.First(&item).Error)
	if item.EveningCompletedFor != nil {
		t.Fatal("marked complete while valuation failed")
	}
	calls := e.market.calls.Load()
	mustOK(t, e.db.Exec("DROP TRIGGER fail_evening_snapshot").Error)
	e.now = fundClock("2026-10-16 19:10:00")
	e.svc.Ensure(context.Background())
	mustOK(t, e.db.First(&item).Error)
	rows, err := e.svc.Valuations(e.users[0], r.PositionID, "", "")
	mustOK(t, err)
	if !fundSameDay(item.EveningCompletedFor, fday("2026-10-16")) || rows[0].MarketValue.String() != "120" || e.market.calls.Load() != calls {
		t.Fatal("cached calculation recovery required another quote fetch", item, rows)
	}
}

func TestFundFullyRedeemedStillCompletesFinalIntervalAndMorning(t *testing.T) {
	e := newFundEnv(t)
	e.now = fundClock("2026-10-13 10:00:00")
	r := e.opening(t, 0, "100", "1", "2026-10-09")
	_, err := e.svc.Operate(context.Background(), e.users[0], r.PositionID, FundInput{RequestKey: "final-sale", Kind: "redeem", Shares: "100", Amount: "120", TradeNAV: "1.2", EffectiveDate: "2026-10-13", CashDate: "2026-10-13", CashAccountID: e.banks[0]})
	mustOK(t, err)
	rows, err := e.svc.repo.Tracked()
	mustOK(t, err)
	if len(rows) != 1 {
		t.Fatal("final interval unsubscribed before its NAV", rows)
	}
	e.market.quotes = append(e.market.quotes, fuyao.NAV{Date: fday("2026-10-13"), Value: dec("1.2")})
	e.now = fundClock("2026-10-13 19:00:00")
	calls := e.market.calls.Load()
	e.svc.Ensure(context.Background())
	history, err := e.svc.Valuations(e.users[0], r.PositionID, "", "")
	mustOK(t, err)
	if e.market.calls.Load() != calls+1 || history[0].NAVDate.Format("2006-01-02") != "2026-10-13" || history[0].DailyPnL == nil || history[0].DailyPnL.String() != "0.54" {
		t.Fatal("final sale interval missing or wrong", history)
	}
	e.now = fundClock("2026-10-14 04:00:00")
	e.svc.Ensure(context.Background())
	if e.market.calls.Load() != calls+2 {
		t.Fatal("closed holding missed independent morning check")
	}
	rows, err = e.svc.repo.TrackedMorning(fday("2026-10-13"))
	mustOK(t, err)
	if len(rows) != 0 {
		t.Fatal("fully settled history did not unsubscribe after morning", rows)
	}
	if e.bank(t, 0) != "5120.00" {
		t.Fatal("quote completion changed cash")
	}
}

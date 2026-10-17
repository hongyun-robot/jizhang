package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"jizhang/internal/model"
	"jizhang/internal/repository"
)

func orderInput(kind, date string) WealthInput {
	return WealthInput{RequestKey: uuid.NewString(), Kind: kind, Code: "J07241", EffectiveDate: date}
}
func (e *wealthEnv) subscription(t *testing.T, amount, date, clock string) WealthReceipt {
	t.Helper()
	in := orderInput("subscribe", date)
	in.Amount = amount
	in.TradeDate = date
	in.ApplicationTime = clock
	in.ExpectedDate = "2026-10-19"
	in.CashAccountID = e.banks[0]
	r, err := e.svc.Subscribe(context.Background(), e.users[0], e.wealths[0], in)
	mustOK(t, err)
	return r
}
func (e *wealthEnv) order(t *testing.T, position int64, in WealthInput) WealthReceipt {
	t.Helper()
	r, err := e.svc.Operate(context.Background(), e.users[0], position, in)
	mustOK(t, err)
	return r
}
func TestWealthPaidOrderConfirmationPreservesHistoryAndExactTradeDate(t *testing.T) {
	e := newWealthEnv(t)
	r := e.subscription(t, "1000", "2026-10-16", "09:00:56")
	v, err := e.svc.Position(e.users[0], r.PositionID)
	mustOK(t, err)
	if e.bank(t, 0) != "4000.00" || v.PendingFunds.String() != "1000" || !v.Shares.IsZero() || v.DailyPnL != nil {
		t.Fatal(v, e.bank(t, 0))
	}
	e.now = fday("2026-10-19")
	e.quote(t, "2026-10-15", "1.02")
	in := orderInput("confirm_buy", "2026-10-19")
	in.SourceEventID = r.EventID
	in.Shares = "1000"
	if _, err = e.svc.Operate(context.Background(), e.users[0], r.PositionID, in); !errors.Is(err, ErrWealthPrice) {
		t.Fatal("wrong-date fallback", err)
	}
	e.quote(t, "2026-10-16", "1")
	confirmed := e.order(t, r.PositionID, in)
	retried := e.order(t, r.PositionID, in)
	if confirmed.EventID != retried.EventID {
		t.Fatal("duplicate confirmation")
	}
	v, err = e.svc.Position(e.users[0], r.PositionID)
	mustOK(t, err)
	if !v.PendingFunds.IsZero() || v.Shares.String() != "1000" || e.bank(t, 0) != "4000.00" || v.DailyPnL != nil {
		t.Fatal(v, e.bank(t, 0))
	}
	historical, err := e.svc.ValuesForDates(e.db, []int64{e.wealths[0]}, []time.Time{fday("2026-10-16")})
	mustOK(t, err)
	if historical["2026-10-16"][e.wealths[0]].Value.String() != "1000" {
		t.Fatal(historical)
	}
	rows, err := e.svc.Events(e.users[0], r.PositionID)
	mustOK(t, err)
	if orderData(rows[1]).NAVDate != "2026-10-16" || orderData(rows[1]).NAVSource != "automatic" {
		t.Fatal(rows)
	}
	cancel := orderInput("cancel_buy", "2026-10-19")
	cancel.SourceEventID = r.EventID
	if _, err = e.svc.Operate(context.Background(), e.users[0], r.PositionID, cancel); !errors.Is(err, ErrWealthConflict) {
		t.Fatal(err)
	}
}
func TestWealthOrderCancellationPartialRefundAndUndoDependencies(t *testing.T) {
	e := newWealthEnv(t)
	r := e.subscription(t, "800", "2026-10-16", "09:00:56")
	in := orderInput("cancel_buy", "2026-10-16")
	in.SourceEventID = r.EventID
	cancelled := e.order(t, r.PositionID, in)
	v, err := e.svc.Position(e.users[0], r.PositionID)
	mustOK(t, err)
	if e.bank(t, 0) != "4200.00" || v.Orders[0].RefundDue.String() != "800" {
		t.Fatal(v)
	}
	in = orderInput("refund_buy", "2026-10-16")
	in.SourceEventID = cancelled.EventID
	in.Amount = "300"
	in.CashAccountID = e.banks[0]
	paid := e.order(t, r.PositionID, in)
	_ = e.order(t, r.PositionID, in)
	if e.bank(t, 0) != "4500.00" {
		t.Fatal(e.bank(t, 0))
	}
	if _, err = e.svc.Undo(e.users[0], r.PositionID, cancelled.EventID, uuid.NewString()); !errors.Is(err, ErrWealthHistory) {
		t.Fatal(err)
	}
	in.RequestKey = uuid.NewString()
	in.Amount = "501"
	if _, err = e.svc.Operate(context.Background(), e.users[0], r.PositionID, in); !errors.Is(err, ErrWealthConflict) {
		t.Fatal(err)
	}
	in.RequestKey = uuid.NewString()
	in.Amount = "500"
	last := e.order(t, r.PositionID, in)
	v, err = e.svc.Position(e.users[0], r.PositionID)
	mustOK(t, err)
	if e.bank(t, 0) != "5000.00" || !v.PendingFunds.IsZero() || v.Orders[0].Status != "refunded" {
		t.Fatal(v)
	}
	_, err = e.svc.Undo(e.users[0], r.PositionID, last.EventID, uuid.NewString())
	mustOK(t, err)
	if e.bank(t, 0) != "4500.00" {
		t.Fatal(e.bank(t, 0))
	}
	_ = paid
}
func TestWealthOrderBankOverrideRequiresReasonAndDoesNotChangePublicQuote(t *testing.T) {
	e := newWealthEnv(t)
	r := e.subscription(t, "100", "2026-10-16", "09:00:00")
	e.quote(t, "2026-10-16", "1")
	in := orderInput("confirm_buy", "2026-10-16")
	in.SourceEventID = r.EventID
	in.Shares = "90.91"
	in.TradeNAV = "1.1"
	if _, err := e.svc.Operate(context.Background(), e.users[0], r.PositionID, in); !errors.Is(err, ErrWealthInvalid) {
		t.Fatal(err)
	}
	in.AdjustmentReason = "银行成交确认 1.1"
	e.order(t, r.PositionID, in)
	var n model.WealthNAV
	mustOK(t, e.db.First(&n).Error)
	if n.UnitNAV.String() != "1" {
		t.Fatal("trade edited public NAV")
	}
}
func TestWealthZhoulibaoLIFOPendingFailedPrincipalAndConfirmedAllocation(t *testing.T) {
	e := newWealthEnv(t)
	old := e.opening(t, 0, "100", "1", "2026-10-09")
	first := e.subscription(t, "100", "2026-10-15", "10:00:00")
	second := e.subscription(t, "200", "2026-10-15", "09:00:00")
	// Backfilled in reverse application order: the 10:00 bank order is newer.
	today := e.subscription(t, "300", "2026-10-16", "08:00:00")
	e.quote(t, "2026-10-15", "1")
	in := orderInput("request_redeem", "2026-10-16")
	in.TradeDate = "2026-10-16"
	in.PreviousTradeDate = "2026-10-15"
	in.Amount = "350"
	request := e.order(t, old.PositionID, in)
	rows, err := e.svc.Events(e.users[0], old.PositionID)
	mustOK(t, err)
	requestEvent := orderEvent(rows, request.EventID)
	a := orderData(*requestEvent).Allocations
	if len(a) != 3 || a[0].SourceID != first.EventID || a[1].SourceID != second.EventID || a[2].SourceID != 0 || a[2].Shares.String() != "50" {
		t.Fatal(a)
	}
	normal := orderInput("redeem", "2026-10-16")
	normal.Amount = "60"
	normal.Shares = "60"
	if _, err = e.svc.Operate(context.Background(), e.users[0], old.PositionID, normal); !errors.Is(err, ErrWealthConflict) {
		t.Fatal(err)
	}
	confirm := orderInput("confirm_buy", "2026-10-16")
	confirm.SourceEventID = first.EventID
	confirm.Shares = "100"
	e.order(t, old.PositionID, confirm)
	fail := orderInput("cancel_buy", "2026-10-16")
	fail.SourceEventID = second.EventID
	e.order(t, old.PositionID, fail)
	v, err := e.svc.Position(e.users[0], old.PositionID)
	mustOK(t, err)
	if v.Orders[1].RefundDue.String() != "0" {
		t.Fatal(v.Orders)
	}
	confirmOut := orderInput("confirm_redeem", "2026-10-16")
	confirmOut.SourceEventID = request.EventID
	confirmOut.Shares = "150"
	confirmOut.Amount = "355"
	out := e.order(t, old.PositionID, confirmOut)
	settlement := orderInput("settlement", "2026-10-16")
	settlement.SourceEventID = out.EventID
	settlement.CashAccountID = e.banks[0]
	e.order(t, old.PositionID, settlement)
	v, err = e.svc.Position(e.users[0], old.PositionID)
	mustOK(t, err)
	if v.Shares.String() != "50" || v.PendingFunds.String() != "300" || v.RealizedPnL.String() != "5" || e.bank(t, 0) != "4755.00" {
		t.Fatal(v, e.bank(t, 0))
	}
	if v.Orders[2].EventID != today.EventID || v.Orders[2].Status != "pending" {
		t.Fatal(v.Orders)
	}
}
func TestWealthZhoulibaoFailedOrderRefundExcludesReservedPrincipal(t *testing.T) {
	e := newWealthEnv(t)
	root := e.subscription(t, "100", "2026-10-15", "09:00:00")
	in := orderInput("request_redeem", "2026-10-16")
	in.TradeDate = "2026-10-16"
	in.PreviousTradeDate = "2026-10-15"
	in.Amount = "40"
	out := e.order(t, root.PositionID, in)
	in = orderInput("cancel_buy", "2026-10-16")
	in.SourceEventID = root.EventID
	cancel := e.order(t, root.PositionID, in)
	in = orderInput("refund_buy", "2026-10-16")
	in.SourceEventID = cancel.EventID
	in.CashAccountID = e.banks[0]
	in.Amount = "60"
	e.order(t, root.PositionID, in)
	in = orderInput("confirm_redeem", "2026-10-16")
	in.SourceEventID = out.EventID
	in.Shares = "0"
	in.Amount = "40"
	confirmed := e.order(t, root.PositionID, in)
	in = orderInput("settlement", "2026-10-16")
	in.SourceEventID = confirmed.EventID
	in.CashAccountID = e.banks[0]
	e.order(t, root.PositionID, in)
	v, err := e.svc.Position(e.users[0], root.PositionID)
	mustOK(t, err)
	if e.bank(t, 0) != "5000.00" || !v.RealizedPnL.IsZero() || !v.PendingFunds.IsZero() {
		t.Fatal(v, e.bank(t, 0))
	}
}

func TestWealthOrderClosedPaidPeriodCanConfirmInOpenPeriodButCannotBackdate(t *testing.T) {
	e := newWealthEnv(t)
	r := e.subscription(t, "100", "2026-10-16", "09:00:00")
	e.quote(t, "2026-10-16", "1")
	e.now = fday("2026-11-02")
	closing := NewClosingService(ClosingDeps{Closings: repository.NewClosingRepo(e.db), Audits: repository.NewAuditRepo(e.db), Periods: e.txn.deps.Periods, Stats: repository.NewStatsRepo(e.db)})
	closing.now = e.svc.Now
	_, err := closing.Close(e.users[0], "2026-10")
	mustOK(t, err)
	in := orderInput("confirm_buy", "2026-10-19")
	in.SourceEventID = r.EventID
	in.Shares = "100"
	if _, err = e.svc.Operate(context.Background(), e.users[0], r.PositionID, in); !errors.Is(err, ErrTxnClosedPeriod) {
		t.Fatal(err)
	}
	in.EffectiveDate = "2026-11-01"
	e.order(t, r.PositionID, in)
	if e.bank(t, 0) != "4900.00" {
		t.Fatal(e.bank(t, 0))
	}
}
func TestWealthZhoulibaoRoundingNeverReservesMoreThanConfirmedRoot(t *testing.T) {
	e := newWealthEnv(t)
	root := e.subscription(t, "0.03", "2026-10-15", "09:00:00")
	for range 3 {
		in := orderInput("request_redeem", "2026-10-16")
		in.TradeDate = "2026-10-16"
		in.PreviousTradeDate = "2026-10-15"
		in.Amount = "0.01"
		e.order(t, root.PositionID, in)
	}
	e.quote(t, "2026-10-15", "1.5")
	in := orderInput("confirm_buy", "2026-10-16")
	in.SourceEventID = root.EventID
	in.Shares = "0.02"
	e.order(t, root.PositionID, in)
	rows, err := e.svc.Events(e.users[0], root.PositionID)
	mustOK(t, err)
	total := dec("0")
	for _, o := range wealthOrderViews(rows) {
		if o.Kind == "request_redeem" {
			total = total.Add(o.SuggestedShares)
		}
	}
	if total.String() != "0.02" {
		t.Fatal(total)
	}
}

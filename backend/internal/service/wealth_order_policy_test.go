package service

import "testing"

func TestWealthOrderInsufficientBalanceChoice(t *testing.T) {
	e := newWealthEnv(t)
	p := e.opening(t, 0, "100", "1", "2026-10-09")
	e.quote(t, "2026-10-15", "1")
	in := orderInput("request_redeem", "2026-10-16")
	in.TradeDate = "2026-10-16"
	in.PreviousTradeDate = "2026-10-15"
	in.Amount = "150"
	cancelled := e.order(t, p.PositionID, in)
	events, err := e.svc.Events(e.users[0], p.PositionID)
	mustOK(t, err)
	if !orderData(*orderEvent(events, cancelled.EventID)).CancelledForInsufficient {
		t.Fatal("insufficient cancellation not saved")
	}
	in.RequestKey = "all-available"
	in.InsufficientPolicy = "all"
	full := e.order(t, p.PositionID, in)
	events, err = e.svc.Events(e.users[0], p.PositionID)
	mustOK(t, err)
	data := orderData(*orderEvent(events, full.EventID))
	if data.AllocatedAmount.String() != "100" || len(data.Allocations) != 1 || data.Allocations[0].Shares.String() != "100" {
		t.Fatal(data)
	}
}

func TestWealthOrderCurrentDayPendingOnlyCannotRedeemEvenWithoutQuote(t *testing.T) {
	e := newWealthEnv(t)
	p := e.subscription(t, "100", "2026-10-16", "09:00:00")
	in := orderInput("request_redeem", "2026-10-16")
	in.TradeDate = "2026-10-16"
	in.PreviousTradeDate = "2026-10-15"
	in.Amount = "100"
	e.order(t, p.PositionID, in)
	v, err := e.svc.Position(e.users[0], p.PositionID)
	mustOK(t, err)
	if v.Orders[1].Status != "cancelled" || v.PendingFunds.String() != "100" || e.bank(t, 0) != "4900.00" {
		t.Fatal(v)
	}
}

package service

import (
	"sort"
	"time"

	"github.com/shopspring/decimal"
	"jizhang/internal/model"
	"jizhang/internal/repository"
)

type cashYieldPlan struct {
	Date         time.Time
	PreviousDate time.Time
	NAV          decimal.Decimal
	PreviousNAV  decimal.Decimal
	Calculated   decimal.Decimal
	Accepted     decimal.Decimal
}

type cashPendingLot struct {
	amount  decimal.Decimal
	confirm time.Time
}

type cashExposure struct {
	confirmed decimal.Decimal
	free      decimal.Decimal
	pending   []cashPendingLot
}

func (e *cashExposure) withdraw(amount decimal.Decimal) {
	// Refund money is available cash, not an automatic new subscription.
	fromCash := decimal.Min(e.free, amount)
	e.free = e.free.Sub(fromCash)
	amount = amount.Sub(fromCash)
	// Latest applications are last in the slice: C, then B, both LIFO.
	for i := len(e.pending) - 1; i >= 0 && amount.IsPositive(); i-- {
		part := decimal.Min(amount, e.pending[i].amount)
		e.pending[i].amount = e.pending[i].amount.Sub(part)
		amount = amount.Sub(part)
	}
	e.confirmed = e.confirmed.Sub(amount)
}

func cashTradeDate(date time.Time, clock string, quotes []model.WealthNAV, next bool) time.Time {
	for _, quote := range quotes {
		day := wealthDay(quote.NAVDate)
		if day.Before(date) {
			continue
		}
		if day.Equal(date) && (next || clock >= "15:00:00") {
			continue
		}
		return day
	}
	return time.Time{} // no unsupported holiday or confirmation assertion
}

func cashConfirmDate(flow repository.CashMovement, policy string, quotes []model.WealthNAV) time.Time {
	if flow.Metadata.ConfirmationDate != nil {
		return wealthDay(*flow.Metadata.ConfirmationDate)
	}
	if policy != "yulibao_20250715" {
		return wealthDay(flow.Date).AddDate(0, 0, 1)
	}
	clock := flow.Metadata.ApplicationTime
	if clock == "" {
		clock = "12:00:00"
	}
	trade := cashTradeDate(wealthDay(flow.Date), clock, quotes, false)
	if trade.IsZero() {
		return time.Time{}
	}
	// T+1 is an explicit estimate. A provided bank confirmation date wins.
	return cashTradeDate(trade, "00:00:00", quotes, true)
}

// cashYieldPlans estimates daily capital exposure inside each disclosed NAV
// interval. Linear interpolation is internal only: no synthetic public NAV or
// undisclosed-date transaction is ever written. Fast withdrawals lose the
// corresponding day's rights; unconfirmed subscriptions earn nothing.
func cashYieldPlans(link model.WealthCashLink, initial decimal.Decimal, flows []repository.CashMovement, quotes []model.WealthNAV, now time.Time, accept func(time.Time, decimal.Decimal) decimal.Decimal) ([]cashYieldPlan, cashExposure, error) {
	start, today := wealthDay(link.StartDate), wealthToday(now)
	state := cashExposure{confirmed: initial}
	type action struct {
		date    time.Time
		flow    repository.CashMovement
		confirm time.Time
	}
	actions := []action{}
	for _, flow := range flows {
		flow.Date = wealthDay(flow.Date)
		if flow.Metadata.ApplicationTime == "" {
			flow.Metadata.ApplicationTime = "12:00:00"
		}
		if !flow.Date.After(start) {
			continue
		}
		at := flow.Date
		if flow.Net.IsNegative() && flow.Metadata.TransferMode == "ordinary" && flow.Metadata.RightsEndDate != nil {
			at = wealthDay(*flow.Metadata.RightsEndDate).AddDate(0, 0, 1)
			if !at.After(start) {
				at = start.AddDate(0, 0, 1)
			}
		}
		actions = append(actions, action{date: at, flow: flow, confirm: cashConfirmDate(flow, link.Policy, quotes)})
	}
	sort.SliceStable(actions, func(i, j int) bool {
		if !actions[i].date.Equal(actions[j].date) {
			return actions[i].date.Before(actions[j].date)
		}
		a, b := actions[i].flow.Metadata.ApplicationTime, actions[j].flow.Metadata.ApplicationTime
		if a != b {
			return a < b
		}
		return actions[i].flow.TxnID < actions[j].flow.TxnID
	})
	index := 0
	process := func(day time.Time) {
		for i := range state.pending {
			lot := &state.pending[i]
			if !lot.confirm.IsZero() && !lot.confirm.After(day) {
				state.confirmed = state.confirmed.Add(lot.amount)
				lot.amount = decimal.Zero
			}
		}
		for index < len(actions) && !actions[index].date.After(day) {
			a := actions[index]
			index++
			if a.flow.Net.IsNegative() {
				state.withdraw(a.flow.Net.Abs())
			} else if a.flow.Kind == model.TxnRefund {
				state.free = state.free.Add(a.flow.Net)
			} else if !a.confirm.IsZero() && !a.confirm.After(day) {
				state.confirmed = state.confirmed.Add(a.flow.Net)
			} else {
				state.pending = append(state.pending, cashPendingLot{amount: a.flow.Net, confirm: a.confirm})
			}
		}
	}
	plans := []cashYieldPlan{}
	var previous *model.WealthNAV
	currentDay := start
	for i := range quotes {
		quote := quotes[i]
		date := wealthDay(quote.NAVDate)
		if date.After(today) {
			break
		}
		if !date.After(start) {
			previous = &quotes[i]
			continue
		}
		if previous == nil {
			for d := currentDay.AddDate(0, 0, 1); !d.After(date); d = d.AddDate(0, 0, 1) {
				process(d)
			}
			currentDay = date
			previous = &quotes[i]
			continue
		}
		left := wealthDay(previous.NAVDate)
		span := int64(date.Sub(left).Hours() / 24)
		if span <= 0 || !previous.UnitNAV.IsPositive() || !quote.UnitNAV.IsPositive() {
			return nil, state, ErrWealthInvalid
		}
		priceAt := func(day time.Time) decimal.Decimal {
			days := int64(day.Sub(left).Hours() / 24)
			return previous.UnitNAV.Add(quote.UnitNAV.Sub(previous.UnitNAV).Mul(decimal.NewFromInt(days)).DivRound(decimal.NewFromInt(span), 16))
		}
		price := priceAt(currentDay)
		gain := decimal.Zero
		for d := currentDay.AddDate(0, 0, 1); !d.After(date); d = d.AddDate(0, 0, 1) {
			process(d)
			next := priceAt(d)
			change := decimal.Max(state.confirmed, decimal.Zero).Mul(next.DivRound(price, 16).Sub(decimal.NewFromInt(1)))
			state.confirmed = state.confirmed.Add(change)
			gain = gain.Add(change)
			price = next
		}
		calculated := gain.Round(2)
		if calculated.Abs().GreaterThan(maxAmount) {
			return nil, state, ErrWealthInvalid
		}
		accepted := accept(date, calculated)
		state.confirmed = state.confirmed.Add(accepted.Sub(gain))
		plans = append(plans, cashYieldPlan{Date: date, PreviousDate: left, NAV: quote.UnitNAV, PreviousNAV: previous.UnitNAV, Calculated: calculated, Accepted: accepted})
		currentDay = date
		previous = &quotes[i]
	}
	for day := currentDay.AddDate(0, 0, 1); !day.After(today); day = day.AddDate(0, 0, 1) {
		process(day)
	}
	return plans, state, nil
}

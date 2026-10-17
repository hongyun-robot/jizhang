package service

import (
	"github.com/shopspring/decimal"
	"jizhang/internal/fuyao"
	"jizhang/internal/model"
	"regexp"
	"sort"
	"strings"
	"time"
)

var wealthNumberPattern = regexp.MustCompile(`^[0-9]+(?:\.[0-9]{1,8})?$`)

// wealthDay normalizes SQL DATE values without applying a second timezone conversion.
func wealthDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}
func wealthToday(t time.Time) time.Time { return wealthDay(t.In(fuyao.Shanghai)) }
func wealthDate(s string, now time.Time) (time.Time, error) {
	d, e := time.Parse("2006-01-02", s)
	if e != nil || d.Format("2006-01-02") != s || d.Year() < 2000 || d.Year() > 2100 || d.After(wealthToday(now)) {
		return time.Time{}, ErrWealthInvalid
	}
	return d, nil
}
func wealthNumber(s string, zero bool, places int) (decimal.Decimal, error) {
	s = strings.TrimSpace(s)
	if s == "" && zero {
		s = "0"
	}
	if !wealthNumberPattern.MatchString(s) {
		return decimal.Zero, ErrWealthInvalid
	}
	parts := strings.Split(s, ".")
	if len(parts) == 2 && len(parts[1]) > places {
		return decimal.Zero, ErrWealthInvalid
	}
	d, e := decimal.NewFromString(s)
	if e != nil || d.GreaterThan(maxAmount) || !d.Round(int32(places)).Equal(d) || (!zero && !d.IsPositive()) {
		return decimal.Zero, ErrWealthInvalid
	}
	return d, nil
}

type wealthState struct {
	Shares    decimal.Decimal
	Cost      decimal.Decimal
	Cash      decimal.Decimal
	Realized  decimal.Decimal
	Dividends decimal.Decimal
	Fees      decimal.Decimal
	BasisDate *time.Time
	Imported  bool
}

// replayWealth applies economic effects separately from bank settlement effects.
// No write or external quote request is performed during valuation.
func replayWealth(events []model.WealthEvent, at time.Time) (wealthState, error) {
	at = wealthDay(at)
	s := wealthState{}
	rows := append([]model.WealthEvent(nil), events...)
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := wealthDay(rows[i].EffectiveDate), wealthDay(rows[j].EffectiveDate)
		if a.Equal(b) {
			return rows[i].ID < rows[j].ID
		}
		return a.Before(b)
	})
	parents := map[int64]model.WealthEvent{}
	for _, e := range rows {
		parents[e.ID] = e
	}
	for _, e := range rows {
		if e.VoidedAt != nil {
			continue
		}
		effective := wealthDay(e.EffectiveDate)
		active := !effective.After(at)
		if (e.Kind == "opening" || e.Kind == "buy" || e.Kind == "confirm_buy") && (s.BasisDate == nil || effective.Before(*s.BasisDate)) {
			d := effective
			s.BasisDate = &d
			s.Imported = e.Kind == "opening"
		}
		rights := effective
		if e.RightsDate != nil {
			rights = wealthDay(*e.RightsDate)
		}
		cashActive := e.CashDate != nil && !wealthDay(*e.CashDate).After(at)
		switch e.Kind {
		case "subscribe":
			if cashActive {
				s.Cash = s.Cash.Add(e.Amount)
			}
		case "cancel_buy", "request_redeem", "cancel_redeem":
		case "refund_buy":
			if cashActive {
				s.Cash = s.Cash.Sub(e.Amount)
			}
		case "opening":
			if active {
				s.Shares = s.Shares.Add(e.Shares)
				s.Cost = s.Cost.Add(e.Amount)
			}
		case "buy", "confirm_buy":
			if cashActive {
				s.Cash = s.Cash.Add(e.Amount)
			}
			if active {
				s.Shares = s.Shares.Add(e.Shares)
				s.Cost = s.Cost.Add(e.Amount)
				s.Cash = s.Cash.Sub(e.Amount)
				s.Fees = s.Fees.Add(e.Fee)
			}
		case "redeem", "confirm_redeem":
			net := e.Amount.Sub(e.Fee)
			if e.Kind == "confirm_redeem" {
				net = net.Sub(orderData(e).FailedPrincipal)
			}
			if active {
				if (!e.Shares.IsPositive() && !(e.Kind == "confirm_redeem" && orderData(e).FailedPrincipal.IsPositive())) || e.Shares.GreaterThan(s.Shares) {
					return s, ErrWealthInvalid
				}
				cost := s.Cost
				if e.Shares.IsZero() {
					cost = decimal.Zero
				} else if !e.Shares.Equal(s.Shares) {
					cost = s.Cost.Mul(e.Shares).DivRound(s.Shares, 16)
				}
				s.Shares = s.Shares.Sub(e.Shares)
				s.Cost = s.Cost.Sub(cost)
				if s.Shares.IsZero() {
					s.Cost = decimal.Zero
				}
				s.Realized = s.Realized.Add(net.Sub(cost))
				s.Fees = s.Fees.Add(e.Fee)
				s.Cash = s.Cash.Add(net)
			}
			if cashActive {
				s.Cash = s.Cash.Sub(net)
			}
		case "cash_dividend":
			if !rights.After(at) {
				s.Cash = s.Cash.Add(e.Amount)
				s.Dividends = s.Dividends.Add(e.Amount)
			}
			if cashActive {
				s.Cash = s.Cash.Sub(e.Amount)
			}
		case "reinvest":
			if !rights.After(at) {
				s.Cash = s.Cash.Add(e.Amount)
				s.Dividends = s.Dividends.Add(e.Amount)
			}
			if active {
				s.Shares = s.Shares.Add(e.Shares)
				s.Cost = s.Cost.Add(e.Amount)
				s.Cash = s.Cash.Sub(e.Amount)
				s.Fees = s.Fees.Add(e.Fee)
			}
		case "settlement":
			if e.SourceEventID == nil {
				return s, ErrWealthInvalid
			}
			p, ok := parents[*e.SourceEventID]
			if !ok || p.VoidedAt != nil {
				return s, ErrWealthInvalid
			}
			if active {
				if p.Kind == "buy" {
					s.Cash = s.Cash.Add(e.Amount)
				} else {
					s.Cash = s.Cash.Sub(e.Amount)
				}
			}
		default:
			return s, ErrWealthInvalid
		}
		if s.Shares.IsNegative() || s.Shares.GreaterThan(maxAmount) || s.Cost.IsNegative() || s.Cost.GreaterThan(maxAmount) {
			return s, ErrWealthInvalid
		}
	}
	return s, nil
}

func wealthEventsFor(rows []model.WealthEvent, id int64) []model.WealthEvent {
	out := []model.WealthEvent{}
	for _, e := range rows {
		if e.PositionID == id {
			out = append(out, e)
		}
	}
	return out
}
func wealthPriceAt(rows []model.WealthQuote, at time.Time, shares decimal.Decimal) (*model.WealthQuote, bool) {
	for i := len(rows) - 1; i >= 0; i-- {
		if rows[i].NAVDate.After(wealthDay(at)) {
			continue
		}
		n := rows[i]
		if shares.Mul(n.UnitNAV).GreaterThan(maxAmount) {
			return nil, true
		}
		return &n, false
	}
	return nil, false
}
func wealthDaily(events []model.WealthEvent, navs []model.WealthQuote, index int) (*decimal.Decimal, string, error) {
	if index <= 0 {
		return nil, "no_history", nil
	}
	cur, prev := navs[index], navs[index-1]
	a, e := replayWealth(events, prev.NAVDate)
	if e != nil {
		return nil, "invalid", e
	}
	b, e := replayWealth(events, cur.NAVDate)
	if e != nil {
		return nil, "invalid", e
	}
	if b.BasisDate == nil || b.BasisDate.After(cur.NAVDate) || (b.Imported && b.BasisDate.After(prev.NAVDate)) {
		return nil, "no_history", nil
	}
	pnl := b.Shares.Mul(cur.UnitNAV).Sub(a.Shares.Mul(prev.UnitNAV))
	within := func(d time.Time) bool { d = wealthDay(d); return d.After(prev.NAVDate) && !d.After(cur.NAVDate) }
	for _, v := range events {
		if v.VoidedAt != nil {
			continue
		}
		if within(v.EffectiveDate) {
			switch v.Kind {
			case "buy", "confirm_buy", "reinvest":
				pnl = pnl.Sub(v.Amount)
			case "redeem", "confirm_redeem":
				pnl = pnl.Add(v.Amount.Sub(v.Fee).Sub(orderData(v).FailedPrincipal))
			}
		}
		if (v.Kind == "cash_dividend" || v.Kind == "reinvest") && v.RightsDate != nil && within(*v.RightsDate) {
			pnl = pnl.Add(v.Amount)
		}
	}
	pnl = pnl.Round(2)
	return &pnl, "ready", nil
}

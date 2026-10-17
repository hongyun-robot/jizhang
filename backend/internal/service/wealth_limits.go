package service

import (
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"jizhang/internal/model"
	"time"
)

// Validate each changed boundary and successor against the same owner NAV
// sequence used by reads. Missing prices never prevent truthful bookkeeping.
func (s *WealthService) accountRange(tx *gorm.DB, p *model.WealthPosition, events []model.WealthEvent, change model.WealthEvent) error {
	start := wealthDay(change.EffectiveDate)
	for _, date := range []*time.Time{change.CashDate, change.RightsDate} {
		if date != nil && wealthDay(*date).Before(start) {
			start = wealthDay(*date)
		}
	}
	data, err := s.repo.At(tx).LoadAccounts([]int64{p.AccountID})
	if err != nil {
		return err
	}
	combined := []model.WealthEvent{}
	for _, e := range data.Events {
		if e.PositionID != p.ID {
			combined = append(combined, e)
		}
	}
	data.Events = append(combined, events...)
	dates := map[string]time.Time{}
	today := wealthToday(s.Now())
	dates[today.Format("2006-01-02")] = today
	for _, e := range data.Events {
		if e.VoidedAt != nil {
			continue
		}
		for _, date := range []*time.Time{&e.EffectiveDate, e.CashDate, e.RightsDate} {
			if date != nil {
				day := wealthDay(*date)
				dates[day.Format("2006-01-02")] = day
			}
		}
	}
	for _, day := range dates {
		if day.Before(start) {
			continue
		}
		cost, known := decimal.Zero, decimal.Zero
		for _, position := range data.Positions {
			state, err := replayWealth(wealthEventsFor(data.Events, position.ID), day)
			if err != nil {
				return err
			}
			cost = cost.Add(state.Cost)
			known = known.Add(state.Cash)
			quote, overflow := wealthPriceAt(wealthQuotes(data, position.OwnerID, position.ProductID), day, state.Shares)
			if overflow {
				return ErrWealthInvalid
			}
			if quote != nil {
				known = known.Add(state.Shares.Mul(quote.UnitNAV))
			}
		}
		if cost.GreaterThan(maxAmount) || known.GreaterThan(maxAmount) || known.IsNegative() {
			return ErrWealthInvalid
		}
	}
	return nil
}

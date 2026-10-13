package service

import (
	"encoding/json"
	"errors"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"jizhang/internal/model"
	"jizhang/internal/repository"
	"time"
)

type FundPositionView struct {
	model.FundPosition
	Code            string           `json:"code"`
	Ticker          string           `json:"ticker"`
	Name            string           `json:"name"`
	SourceName      string           `json:"source_name"`
	Shares          decimal.Decimal  `json:"shares"`
	Cost            decimal.Decimal  `json:"cost"`
	CostPrice       decimal.Decimal  `json:"cost_price"`
	CostEstimated   bool             `json:"cost_estimated"`
	MarketValue     *decimal.Decimal `json:"market_value"`
	UnitNAV         *decimal.Decimal `json:"unit_nav"`
	NAVDate         string           `json:"nav_date"`
	PendingFunds    decimal.Decimal  `json:"pending_funds"`
	HoldingPnL      *decimal.Decimal `json:"holding_pnl"`
	HoldingReturn   *decimal.Decimal `json:"holding_return"`
	RealizedPnL     decimal.Decimal  `json:"realized_pnl"`
	Dividends       decimal.Decimal  `json:"dividends"`
	Fees            decimal.Decimal  `json:"fees"`
	TotalPnL        *decimal.Decimal `json:"total_pnl"`
	DailyPnL        *decimal.Decimal `json:"daily_pnl"`
	DailyStart      string           `json:"daily_start"`
	DailyEnd        string           `json:"daily_end"`
	DailyStatus     string           `json:"daily_status"`
	ValuationStatus string           `json:"valuation_status"`
	SourceStatus    string           `json:"source_status"`
	SourceError     string           `json:"source_error,omitempty"`
	EffectiveDate   string           `json:"effective_date"`
	CanAmend        bool             `json:"can_amend"`
}
type FundAccountView struct {
	Account      model.Account      `json:"account"`
	Positions    []FundPositionView `json:"positions"`
	MarketValue  *decimal.Decimal   `json:"market_value"`
	PendingFunds decimal.Decimal    `json:"pending_funds"`
	TotalValue   *decimal.Decimal   `json:"total_value"`
	TotalPnL     *decimal.Decimal   `json:"total_pnl"`
	Complete     bool               `json:"complete"`
	Configured   bool               `json:"configured"`
}

func decimalPointer(d decimal.Decimal) *decimal.Decimal { v := d; return &v }

func (s *FundService) valuePosition(data repository.FundData, p model.FundPosition, at time.Time) (FundPositionView, error) {
	rows := fundEventsFor(data.Events, p.ID)
	state, e := replayFund(rows, at)
	if e != nil {
		return FundPositionView{}, e
	}
	v := FundPositionView{FundPosition: p, Shares: state.Shares, Cost: state.Cost.Round(2), PendingFunds: state.Cash.Round(2), RealizedPnL: state.Realized.Round(2), Dividends: state.Dividends.Round(2), Fees: state.Fees.Round(2), CostEstimated: state.Imported, DailyStatus: "no_history", ValuationStatus: "ready"}
	if state.BasisDate != nil {
		v.EffectiveDate = state.BasisDate.Format("2006-01-02")
	}
	if state.Shares.IsPositive() {
		v.CostPrice = state.Cost.DivRound(state.Shares, 8)
	}
	for _, i := range data.Instruments {
		if i.ID == p.InstrumentID {
			v.Code = i.THSCode
			v.Ticker = i.Ticker
			v.SourceName = i.Name
			v.Name = i.Name
			v.SourceStatus = i.SyncStatus
			v.SourceError = i.LastError
			break
		}
	}
	if p.DisplayName != "" {
		v.Name = p.DisplayName
	}
	navs := fundNAVsFor(data.NAVs, p.InstrumentID)
	price, fallback := fundPriceAt(navs, at, state.Shares)
	market := decimal.Zero
	if price == nil && state.Shares.IsPositive() {
		v.ValuationStatus = "no_price"
		return v, nil
	}
	if price != nil {
		market = state.Shares.Mul(price.UnitNAV)
		v.UnitNAV = decimalPointer(price.UnitNAV)
		v.NAVDate = price.NAVDate.Format("2006-01-02")
	}
	if fallback {
		v.ValuationStatus = "range_error"
	}
	v.MarketValue = decimalPointer(market.Round(2))
	v.HoldingPnL = decimalPointer(market.Sub(state.Cost).Round(2))
	if state.Cost.IsPositive() {
		v.HoldingReturn = decimalPointer(market.Sub(state.Cost).DivRound(state.Cost, 12).Mul(decimal.NewFromInt(100)).Round(4))
	}
	v.TotalPnL = decimalPointer(market.Sub(state.Cost).Add(state.Realized).Add(state.Dividends).Round(2))
	if price != nil {
		for i := range navs {
			if navs[i].NAVDate.Equal(price.NAVDate) {
				divs := []model.FundDividend{}
				for _, d := range data.Dividends {
					if d.InstrumentID == p.InstrumentID {
						divs = append(divs, d)
					}
				}
				daily, status, e := fundDaily(rows, navs, i, divs)
				if e != nil {
					return v, e
				}
				v.DailyPnL = daily
				v.DailyStatus = status
				v.DailyEnd = v.NAVDate
				if i > 0 {
					v.DailyStart = navs[i-1].NAVDate.Format("2006-01-02")
				}
				break
			}
		}
	}
	active := []model.FundEvent{}
	for _, event := range rows {
		if event.VoidedAt == nil {
			active = append(active, event)
		}
	}
	if len(active) == 1 && active[0].Kind == "opening" {
		v.CanAmend = true
	}
	return v, nil
}
func (s *FundService) valueAccounts(db *gorm.DB, accounts []model.Account, at time.Time) ([]FundAccountView, error) {
	ids := []int64{}
	for _, a := range accounts {
		ids = append(ids, a.ID)
	}
	data, e := repository.NewFundRepo(db).LoadAccounts(ids)
	if e != nil {
		return nil, e
	}
	result := make([]FundAccountView, 0, len(accounts))
	for _, a := range accounts {
		v := FundAccountView{Account: a, Positions: []FundPositionView{}, Complete: true, Configured: s.Configured()}
		market, total, pnl := decimal.Zero, decimal.Zero, decimal.Zero
		for _, p := range data.Positions {
			if p.AccountID != a.ID {
				continue
			}
			pv, e := s.valuePosition(data, p, at)
			if e != nil {
				return nil, e
			}
			v.Positions = append(v.Positions, pv)
			if pv.CanAmend {
				for _, event := range data.Events {
					if event.PositionID == p.ID && event.Kind == "opening" && event.VoidedAt == nil && s.openDates(db, event) != nil {
						v.Positions[len(v.Positions)-1].CanAmend = false
					}
				}
			}
			v.PendingFunds = v.PendingFunds.Add(pv.PendingFunds)
			if pv.MarketValue == nil || pv.TotalPnL == nil {
				v.Complete = false
			} else {
				market = market.Add(*pv.MarketValue)
				pnl = pnl.Add(*pv.TotalPnL)
			}
		}
		if v.Complete {
			v.MarketValue = decimalPointer(market.Round(2))
			total = market.Add(v.PendingFunds).Round(2)
			v.TotalValue = &total
			v.TotalPnL = decimalPointer(pnl.Round(2))
			v.Account.Balance = total
		}
		result = append(result, v)
	}
	return result, nil
}
func (s *FundService) Accounts(actor *model.User) ([]FundAccountView, error) {
	if e := fundActor(actor); e != nil {
		return nil, e
	}
	accounts := []model.Account{}
	e := s.repo.DB().Where("owner_id = ? AND type = ? AND asset_class = ?", actor.ID, model.AccountAsset, "fund").Order("sort,id").Find(&accounts).Error
	if e != nil {
		return nil, e
	}
	return s.valueAccounts(s.repo.DB(), accounts, fundToday(s.Now()))
}
func (s *FundService) Account(actor *model.User, id int64) (FundAccountView, error) {
	a, e := s.account(s.repo.DB(), actor, id)
	if e != nil {
		return FundAccountView{}, e
	}
	v, e := s.valueAccounts(s.repo.DB(), []model.Account{*a}, fundToday(s.Now()))
	if e != nil {
		return FundAccountView{}, e
	}
	return v[0], nil
}
func (s *FundService) Position(actor *model.User, id int64) (FundPositionView, error) {
	p, e := s.position(s.repo.DB(), actor, id, false)
	if e != nil {
		return FundPositionView{}, e
	}
	data, e := s.repo.LoadAccounts([]int64{p.AccountID})
	if e != nil {
		return FundPositionView{}, e
	}
	v, e := s.valuePosition(data, *p, fundToday(s.Now()))
	if e == nil && v.CanAmend {
		for _, event := range data.Events {
			if event.PositionID == p.ID && event.Kind == "opening" && event.VoidedAt == nil && s.openDates(s.repo.DB(), event) != nil {
				v.CanAmend = false
			}
		}
	}
	return v, e
}
func (s *FundService) DividendHints(actor *model.User, id int64) ([]model.FundDividend, error) {
	p, e := s.position(s.repo.DB(), actor, id, false)
	if e != nil {
		return nil, e
	}
	return s.repo.Dividends(p.InstrumentID)
}
func (s *FundService) Valuations(actor *model.User, id int64, start, end string) ([]model.FundValuation, error) {
	p, err := s.position(s.repo.DB(), actor, id, false)
	if err != nil {
		return nil, err
	}
	if p.RebuildPending {
		if e := s.RebuildPosition(id); e != nil {
			return nil, e
		}
	}
	if _, e := s.position(s.repo.DB(), actor, id, false); e != nil {
		return nil, e
	}
	q := s.repo.DB().Where("position_id = ?", id)
	var st, en time.Time
	var e error
	if start != "" {
		st, e = fundDate(start, s.Now())
		if e != nil {
			return nil, e
		}
		q = q.Where("nav_date >= ?", st)
	}
	if end != "" {
		en, e = fundDate(end, s.Now())
		if e != nil {
			return nil, e
		}
		q = q.Where("nav_date <= ?", en)
	}
	if !st.IsZero() && !en.IsZero() && st.After(en) {
		return nil, ErrFundInvalid
	}
	rows := []model.FundValuation{}
	e = q.Order("nav_date DESC").Limit(2000).Find(&rows).Error
	return rows, e
}
func (s *FundService) RebuildPosition(id int64) error {
	return s.repo.DB().Transaction(func(tx *gorm.DB) error {
		p, e := repository.NewFundRepo(tx).Position(id, true)
		if e != nil {
			return e
		}
		if p == nil {
			return ErrFundMissing
		}
		data, e := repository.NewFundRepo(tx).LoadAccounts([]int64{p.AccountID})
		if e != nil {
			return e
		}
		events := fundEventsFor(data.Events, id)
		navs := fundNAVsFor(data.NAVs, p.InstrumentID)
		state, e := replayFund(events, fundToday(s.Now()))
		if e != nil {
			return e
		}
		if state.BasisDate == nil {
			if e = tx.Where("position_id = ?", id).Delete(&model.FundValuation{}).Error; e != nil {
				return e
			}
			return tx.Model(p).Update("rebuild_pending", false).Error
		}
		if e = tx.Where("position_id = ? AND nav_date < ?", id, *state.BasisDate).Delete(&model.FundValuation{}).Error; e != nil {
			return e
		}
		divs := []model.FundDividend{}
		for _, d := range data.Dividends {
			if d.InstrumentID == p.InstrumentID {
				divs = append(divs, d)
			}
		}
		for i, n := range navs {
			if n.NAVDate.Before(*state.BasisDate) {
				continue
			}
			asof, e := replayFund(events, n.NAVDate)
			if e != nil {
				return e
			}
			mv := asof.Shares.Mul(n.UnitNAV)
			if mv.GreaterThan(maxAmount) {
				continue
			}
			daily, status, e := fundDaily(events, navs, i, divs)
			if e != nil {
				return e
			}
			v := model.FundValuation{PositionID: id, NAVDate: n.NAVDate, EventVersion: p.EventVersion, QuoteRevision: n.Revision, Shares: asof.Shares, Cost: asof.Cost, MarketValue: mv.Round(2), DailyPnL: daily, Status: status, UpdatedAt: s.Now()}
			if i > 0 {
				d := navs[i-1].NAVDate
				v.PreviousNAVDate = &d
				v.PreviousQuoteRevision = navs[i-1].Revision
			}
			var old model.FundValuation
			e = tx.Where("position_id = ? AND nav_date = ?", id, n.NAVDate).First(&old).Error
			if e != nil && !errors.Is(e, gorm.ErrRecordNotFound) {
				return e
			}
			if e == nil && old.EventVersion == v.EventVersion && old.QuoteRevision == v.QuoteRevision && old.PreviousQuoteRevision == v.PreviousQuoteRevision && old.Status == v.Status {
				continue
			}
			if e == nil {
				before, _ := json.Marshal(old)
				after, _ := json.Marshal(v)
				if e = tx.Create(&model.FundAudit{OwnerID: p.OwnerID, PositionID: id, ActorID: 0, Action: "revalue", BeforeJSON: string(before), AfterJSON: string(after)}).Error; e != nil {
					return e
				}
			}
			if e = tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "position_id"}, {Name: "nav_date"}}, DoUpdates: clause.AssignmentColumns([]string{"previous_nav_date", "event_version", "quote_revision", "previous_quote_revision", "shares", "cost", "market_value", "daily_pn_l", "status", "updated_at"})}).Create(&v).Error; e != nil {
				return e
			}
		}
		return tx.Model(p).Update("rebuild_pending", false).Error
	})
}

// ValuesForDates is the shared current/history read model. IDs are already owner-scoped by AccountRepo.
func (s *FundService) ValuesForDates(db *gorm.DB, ids []int64, dates []time.Time) (map[string]map[int64]repository.FundAccountValue, error) {
	result := map[string]map[int64]repository.FundAccountValue{}
	data, e := repository.NewFundRepo(db).LoadAccounts(ids)
	if e != nil {
		return nil, e
	}
	for _, d := range dates {
		key := fundDay(d).Format("2006-01-02")
		result[key] = map[int64]repository.FundAccountValue{}
		for _, id := range ids {
			result[key][id] = repository.FundAccountValue{Complete: true, Status: "ready"}
		}
		for _, p := range data.Positions {
			v, e := s.valuePosition(data, p, d)
			if e != nil {
				return nil, e
			}
			a := result[key][p.AccountID]
			if v.MarketValue == nil {
				a.Complete = false
				a.Status = "no_price"
			} else {
				a.Value = a.Value.Add(*v.MarketValue).Add(v.PendingFunds)
			}
			if v.ValuationStatus != "ready" {
				a.Status = v.ValuationStatus
			}
			if a.NAVDate == "" || (v.NAVDate != "" && v.NAVDate < a.NAVDate) {
				a.NAVDate = v.NAVDate
			}
			result[key][p.AccountID] = a
		}
	}
	return result, nil
}

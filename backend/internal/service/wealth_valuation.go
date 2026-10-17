package service

import (
	"encoding/json"
	"errors"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"jizhang/internal/model"
	"jizhang/internal/repository"
	"jizhang/internal/wealthdata"
	"sort"
	"time"
)

func wealthQuotes(data repository.WealthData, owner, product int64) []model.WealthQuote {
	byDate := map[string]model.WealthQuote{}
	for _, n := range data.ManualNAVs {
		if n.OwnerID == owner && n.ProductID == product && !n.Withdrawn {
			date := wealthDay(n.NAVDate)
			byDate[date.Format("2006-01-02")] = model.WealthQuote{ProductID: product, NAVDate: date, UnitNAV: n.UnitNAV, Source: "manual", RecordID: n.ID, Revision: n.Revision}
		}
	}
	for _, n := range data.NAVs {
		if n.ProductID == product {
			date := wealthDay(n.NAVDate)
			byDate[date.Format("2006-01-02")] = model.WealthQuote{ProductID: product, NAVDate: date, UnitNAV: n.UnitNAV, TotalNAV: n.TotalNAV, Source: "automatic", RecordID: n.ID, Revision: n.Revision}
		}
	}
	rows := make([]model.WealthQuote, 0, len(byDate))
	for _, n := range byDate {
		rows = append(rows, n)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].NAVDate.Before(rows[j].NAVDate) })
	return rows
}
func wealthSeriesVersion(data repository.WealthData, owner, product int64) int64 {
	for _, s := range data.Series {
		if s.OwnerID == owner && s.ProductID == product {
			return s.Version
		}
	}
	return 0
}

type WealthPositionView struct {
	model.WealthPosition
	Product         model.WealthProduct `json:"product"`
	Quote           *model.WealthQuote  `json:"quote"`
	SeriesVersion   int64               `json:"series_version"`
	RebuildPending  bool                `json:"rebuild_pending"`
	Code            string              `json:"code"`
	Ticker          string              `json:"ticker"`
	Name            string              `json:"name"`
	SourceName      string              `json:"source_name"`
	OrderPolicy     string              `json:"order_policy"`
	Shares          decimal.Decimal     `json:"shares"`
	Cost            decimal.Decimal     `json:"cost"`
	CostPrice       decimal.Decimal     `json:"cost_price"`
	CostEstimated   bool                `json:"cost_estimated"`
	MarketValue     *decimal.Decimal    `json:"market_value"`
	UnitNAV         *decimal.Decimal    `json:"unit_nav"`
	NAVDate         string              `json:"nav_date"`
	PendingFunds    decimal.Decimal     `json:"pending_funds"`
	HoldingPnL      *decimal.Decimal    `json:"holding_pnl"`
	HoldingReturn   *decimal.Decimal    `json:"holding_return"`
	RealizedPnL     decimal.Decimal     `json:"realized_pnl"`
	Dividends       decimal.Decimal     `json:"dividends"`
	Fees            decimal.Decimal     `json:"fees"`
	TotalPnL        *decimal.Decimal    `json:"total_pnl"`
	DailyPnL        *decimal.Decimal    `json:"daily_pnl"`
	DailyStart      string              `json:"daily_start"`
	DailyEnd        string              `json:"daily_end"`
	DailyStatus     string              `json:"daily_status"`
	ValuationStatus string              `json:"valuation_status"`
	SourceStatus    string              `json:"source_status"`
	SourceError     string              `json:"source_error,omitempty"`
	EffectiveDate   string              `json:"effective_date"`
	CanAmend        bool                `json:"can_amend"`
	OpeningNAV      *decimal.Decimal    `json:"opening_nav,omitempty"`
	OpeningNAVDate  string              `json:"opening_nav_date,omitempty"`
	Orders          []WealthOrderView   `json:"orders"`
}
type WealthAccountView struct {
	Account       model.Account        `json:"account"`
	Positions     []WealthPositionView `json:"positions"`
	MarketValue   *decimal.Decimal     `json:"market_value"`
	PendingFunds  decimal.Decimal      `json:"pending_funds"`
	TotalValue    *decimal.Decimal     `json:"total_value"`
	TotalPnL      *decimal.Decimal     `json:"total_pnl"`
	Complete      bool                 `json:"complete"`
	Configured    bool                 `json:"configured"`
	KnownSubtotal decimal.Decimal      `json:"known_subtotal"`
}

func (s *WealthService) valuePosition(data repository.WealthData, p model.WealthPosition, at time.Time) (WealthPositionView, error) {
	rows := wealthEventsFor(data.Events, p.ID)
	state, e := replayWealth(rows, at)
	if e != nil {
		return WealthPositionView{}, e
	}
	v := WealthPositionView{WealthPosition: p, Shares: state.Shares, Cost: state.Cost.Round(2), PendingFunds: state.Cash.Round(2), RealizedPnL: state.Realized.Round(2), Dividends: state.Dividends.Round(2), Fees: state.Fees.Round(2), CostEstimated: state.Imported, DailyStatus: "no_history", ValuationStatus: "ready"}
	v.Orders = wealthOrderViews(rows)
	for _, event := range rows {
		if event.Kind == "opening" && event.VoidedAt == nil {
			data := orderData(event)
			if value, err := decimal.NewFromString(data.OpeningNAV); err == nil && value.IsPositive() {
				v.OpeningNAV = &value
				v.OpeningNAVDate = data.NAVDate
			}
			break
		}
	}
	v.SeriesVersion = wealthSeriesVersion(data, p.OwnerID, p.ProductID)
	v.RebuildPending = p.RebuildPending
	if state.BasisDate != nil {
		v.EffectiveDate = state.BasisDate.Format("2006-01-02")
	}
	if state.Shares.IsPositive() {
		v.CostPrice = state.Cost.DivRound(state.Shares, 8)
	}
	for _, i := range data.Products {
		if i.ID == p.ProductID {
			v.Product = i
			v.Code = i.Code
			v.Ticker = i.Code
			v.SourceName = i.Name
			if product, ok := wealthdata.Catalog(i.Code); ok {
				v.OrderPolicy = product.OrderPolicy
			}
			v.Name = i.Name
			v.SourceStatus = i.SyncStatus
			v.SourceError = i.LastError
			break
		}
	}
	if p.DisplayName != "" {
		v.Name = p.DisplayName
	}
	navs := wealthQuotes(data, p.OwnerID, p.ProductID)
	price, fallback := wealthPriceAt(navs, at, state.Shares)
	market := decimal.Zero
	if price == nil && state.Shares.IsPositive() {
		v.ValuationStatus = "no_price"
		if fallback {
			v.ValuationStatus = "range_error"
		}
		return v, nil
	}
	if price != nil {
		market = state.Shares.Mul(price.UnitNAV)
		v.Quote = price
		v.SeriesVersion = wealthSeriesVersion(data, p.OwnerID, p.ProductID)
		v.RebuildPending = p.RebuildPending
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
				daily, status, e := wealthDaily(rows, navs, i)
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
	active := []model.WealthEvent{}
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
func (s *WealthService) valueAccounts(db *gorm.DB, accounts []model.Account, at time.Time) ([]WealthAccountView, error) {
	ids := []int64{}
	for _, a := range accounts {
		ids = append(ids, a.ID)
	}
	data, e := repository.NewWealthRepo(db).LoadAccounts(ids)
	if e != nil {
		return nil, e
	}
	result := make([]WealthAccountView, 0, len(accounts))
	for _, a := range accounts {
		v := WealthAccountView{Account: a, Positions: []WealthPositionView{}, Complete: true, Configured: s.Configured()}
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
			if !s.configuredFor(pv.Code) {
				v.Configured = false
			}
			if a.Status != model.StatusActive {
				v.Positions[len(v.Positions)-1].CanAmend = false
			}
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
		v.KnownSubtotal = market.Add(v.PendingFunds).Round(2)
		if !v.Complete {
			v.Account.ValuationStatus = "no_price"
			v.Account.KnownBalance = decimalPointer(v.KnownSubtotal)
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
func (s *WealthService) Accounts(actor *model.User) ([]WealthAccountView, error) {
	if e := wealthActor(actor); e != nil {
		return nil, e
	}
	accounts, e := s.repo.Accounts(actor.ID)
	if e != nil {
		return nil, e
	}
	return s.valueAccounts(s.repo.DB(), accounts, wealthToday(s.Now()))
}
func (s *WealthService) Account(actor *model.User, id int64) (WealthAccountView, error) {
	a, e := s.account(s.repo.DB(), actor, id)
	if e != nil {
		return WealthAccountView{}, e
	}
	v, e := s.valueAccounts(s.repo.DB(), []model.Account{*a}, wealthToday(s.Now()))
	if e != nil {
		return WealthAccountView{}, e
	}
	return v[0], nil
}
func (s *WealthService) Position(actor *model.User, id int64) (WealthPositionView, error) {
	p, e := s.position(s.repo.DB(), actor, id, false)
	if e != nil {
		return WealthPositionView{}, e
	}
	data, e := s.repo.LoadAccounts([]int64{p.AccountID})
	if e != nil {
		return WealthPositionView{}, e
	}
	v, e := s.valuePosition(data, *p, wealthToday(s.Now()))
	if e == nil && v.CanAmend {
		a, err := s.account(s.repo.DB(), actor, p.AccountID)
		if err != nil {
			return v, err
		}
		if a.Status != model.StatusActive {
			v.CanAmend = false
		}
	}
	if e == nil && v.CanAmend {
		for _, event := range data.Events {
			if event.PositionID == p.ID && event.Kind == "opening" && event.VoidedAt == nil && s.openDates(s.repo.DB(), event) != nil {
				v.CanAmend = false
			}
		}
	}
	return v, e
}
func (s *WealthService) Valuations(actor *model.User, id int64, start, end string) ([]model.WealthValuation, error) {
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
	var st, en time.Time
	var e error
	if start != "" {
		st, e = wealthDate(start, s.Now())
		if e != nil {
			return nil, e
		}
	}
	if end != "" {
		en, e = wealthDate(end, s.Now())
		if e != nil {
			return nil, e
		}
	}
	if !st.IsZero() && !en.IsZero() && st.After(en) {
		return nil, ErrWealthInvalid
	}
	return s.repo.Valuations(actor.ID, id, st, en)
}
func (s *WealthService) RebuildPosition(id int64) error {
	// Identity is immutable. Read it before starting the snapshot transaction so
	// waiting for the product lock cannot retain a pre-quote MVCC snapshot.
	var metadata model.WealthPosition
	if e := s.repo.DB().First(&metadata, id).Error; e != nil {
		return e
	}
	return s.repo.DB().Transaction(func(tx *gorm.DB) error {
		if _, e := repository.NewWealthRepo(tx).Product(metadata.ProductID, true); e != nil {
			return e
		}
		p, e := repository.NewWealthRepo(tx).Position(metadata.OwnerID, id, true)
		if e != nil {
			return e
		}
		if p == nil {
			return ErrWealthMissing
		}
		data, e := repository.NewWealthRepo(tx).LoadAccounts([]int64{p.AccountID})
		if e != nil {
			return e
		}
		events := wealthEventsFor(data.Events, id)
		navs := wealthQuotes(data, p.OwnerID, p.ProductID)
		state, e := replayWealth(events, wealthToday(s.Now()))
		if e != nil {
			return e
		}
		if state.BasisDate == nil {
			if e = tx.Where("position_id = ?", id).Delete(&model.WealthValuation{}).Error; e != nil {
				return e
			}
			return tx.Model(p).Update("rebuild_pending", false).Error
		}
		validDates := map[string]bool{}
		for _, n := range navs {
			if !n.NAVDate.Before(*state.BasisDate) {
				validDates[n.NAVDate.Format("2006-01-02")] = true
			}
		}
		var oldSnapshots []model.WealthValuation
		if e = tx.Where("position_id = ?", id).Find(&oldSnapshots).Error; e != nil {
			return e
		}
		for _, old := range oldSnapshots {
			if validDates[wealthDay(old.NAVDate).Format("2006-01-02")] {
				continue
			}
			before, _ := json.Marshal(old)
			if e = tx.Create(&model.WealthAudit{OwnerID: p.OwnerID, PositionID: id, ActorID: 0, Action: "valuation_remove", BeforeJSON: string(before), AfterJSON: "null"}).Error; e != nil {
				return e
			}
			if e = tx.Delete(&old).Error; e != nil {
				return e
			}
		}
		for i, n := range navs {
			if n.NAVDate.Before(*state.BasisDate) {
				continue
			}
			asof, e := replayWealth(events, n.NAVDate)
			if e != nil {
				return e
			}
			mv := asof.Shares.Mul(n.UnitNAV)
			if mv.GreaterThan(maxAmount) {
				continue
			}
			daily, status, e := wealthDaily(events, navs, i)
			if e != nil {
				return e
			}
			v := model.WealthValuation{PositionID: id, NAVDate: n.NAVDate, EventVersion: p.EventVersion, SeriesVersion: wealthSeriesVersion(data, p.OwnerID, p.ProductID), QuoteSource: n.Source, QuoteRecordID: n.RecordID, QuoteRevision: n.Revision, Shares: asof.Shares, Cost: asof.Cost, MarketValue: mv.Round(2), DailyPnL: daily, Status: status, UpdatedAt: s.Now()}
			if i > 0 {
				d := navs[i-1].NAVDate
				v.PreviousNAVDate = &d
				v.PreviousQuoteRevision = navs[i-1].Revision
				v.PreviousQuoteSource = navs[i-1].Source
				v.PreviousQuoteRecordID = navs[i-1].RecordID
			}
			var old model.WealthValuation
			lower, upper := repository.WealthDateBounds(n.NAVDate)
			e = tx.Where("position_id = ? AND nav_date >= ? AND nav_date < ?", id, lower, upper).First(&old).Error
			if e != nil && !errors.Is(e, gorm.ErrRecordNotFound) {
				return e
			}
			if e == nil && old.EventVersion == v.EventVersion && old.SeriesVersion == v.SeriesVersion && old.QuoteSource == v.QuoteSource && old.QuoteRecordID == v.QuoteRecordID && old.PreviousQuoteSource == v.PreviousQuoteSource && old.PreviousQuoteRecordID == v.PreviousQuoteRecordID && old.QuoteRevision == v.QuoteRevision && old.PreviousQuoteRevision == v.PreviousQuoteRevision && old.Status == v.Status {
				continue
			}
			if e == nil {
				before, _ := json.Marshal(old)
				after, _ := json.Marshal(v)
				if e = tx.Create(&model.WealthAudit{OwnerID: p.OwnerID, PositionID: id, ActorID: 0, Action: "revalue", BeforeJSON: string(before), AfterJSON: string(after)}).Error; e != nil {
					return e
				}
			}
			if e = tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "position_id"}, {Name: "nav_date"}}, DoUpdates: clause.AssignmentColumns([]string{"previous_nav_date", "event_version", "series_version", "quote_source", "quote_record_id", "previous_quote_source", "previous_quote_record_id", "quote_revision", "previous_quote_revision", "shares", "cost", "market_value", "daily_pn_l", "status", "updated_at"})}).Create(&v).Error; e != nil {
				return e
			}
		}
		return tx.Model(p).Update("rebuild_pending", false).Error
	})
}

// ValuesForDates is the shared current/history read model. IDs are already owner-scoped by AccountRepo.
func (s *WealthService) ValuesForDates(db *gorm.DB, ids []int64, dates []time.Time) (map[string]map[int64]repository.FundAccountValue, error) {
	result := map[string]map[int64]repository.FundAccountValue{}
	data, e := repository.NewWealthRepo(db).LoadAccounts(ids)
	if e != nil {
		return nil, e
	}
	for _, d := range dates {
		key := wealthDay(d).Format("2006-01-02")
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
				a.Value = a.Value.Add(*v.MarketValue)
			}
			a.Value = a.Value.Add(v.PendingFunds)
			if v.ValuationStatus != "ready" && a.Status != "no_price" {
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

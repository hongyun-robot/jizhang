package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"jizhang/internal/ccb"
	"jizhang/internal/model"
	"jizhang/internal/repository"
)

type GoldService struct {
	repo    *repository.GoldRepo
	txns    *TransactionService
	market  ccb.Market
	Now     func() time.Time
	OnWrite func()
}

func NewGoldService(db *gorm.DB, txns *TransactionService, market ccb.Market) *GoldService {
	return &GoldService{repository.NewGoldRepo(db), txns, market, time.Now, nil}
}
func (s *GoldService) changed() {
	if s.OnWrite != nil {
		s.OnWrite()
	}
}
func (s *GoldService) BindAccounts(r *repository.AccountRepo) {
	r.GoldValues = s.ValuesForDates
	r.Now = func() time.Time { return s.Now().In(ccb.Shanghai) }
}
func (s *GoldService) account(db *gorm.DB, actor *model.User, id int64) (*model.Account, error) {
	if fundActor(actor) != nil {
		return nil, ErrTxnForbidden
	}
	a, e := repository.NewAccountRepo(db).GetByID(id)
	if e != nil {
		return nil, e
	}
	if a == nil {
		return nil, ErrGoldMissing
	}
	if !ownsAccount(actor, *a) {
		return nil, ErrTxnForbidden
	}
	if !a.IsGold() {
		return nil, ErrGoldInvalid
	}
	return a, nil
}
func (s *GoldService) openDate(db *gorm.DB, d time.Time) error {
	inner := s.txns.bound(db)
	return inner.guardOpen(inner.deps.Periods.KeyForDate(fundDay(d)))
}

// A backdated opening can change a closed period even when that period has no trades.
func (s *GoldService) guardClosedHistory(tx *gorm.DB, before, after []model.GoldEvent) error {
	closed, e := repository.NewClosingRepo(tx).List()
	if e != nil {
		return e
	}
	periods := s.txns.bound(tx).deps.Periods
	for _, period := range closed {
		start, end, ok := periods.Range(period.PeriodKey)
		if !ok {
			return ErrTxnClosedPeriod
		}
		for d := start; d.Before(end); d = d.AddDate(0, 0, 1) {
			a, e := replayGold(before, d)
			if e != nil {
				return e
			}
			b, e := replayGold(after, d)
			if e != nil {
				return e
			}
			if !a.Grams.Equal(b.Grams) || !a.Cost.Equal(b.Cost) || !a.Realized.Equal(b.Realized) {
				return ErrTxnClosedPeriod
			}
		}
	}
	return nil
}

type GoldReceipt struct {
	AccountID int64 `json:"account_id"`
	EventID   int64 `json:"event_id"`
}

func goldDigest(action string, id int64, e any) string {
	b, _ := json.Marshal([]any{action, id, e})
	v := sha256.Sum256(b)
	return hex.EncodeToString(v[:])
}
func (s *GoldService) mutation(db *gorm.DB, actor *model.User, accountID int64, key, hash string) (*GoldReceipt, error) {
	var m model.GoldMutation
	err := db.Where("owner_id = ? AND request_key = ?", actor.ID, key).First(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if m.PayloadHash != hash || m.AccountID != accountID {
		return nil, ErrGoldConflict
	}
	return &GoldReceipt{m.AccountID, m.EventID}, nil
}
func (s *GoldService) audit(tx *gorm.DB, actor *model.User, id int64, e model.GoldEvent, action string, before any, original *GoldInput) error {
	b, _ := json.Marshal(before)
	a, _ := json.Marshal(e)
	if original != nil {
		a, _ = json.Marshal(struct {
			Event model.GoldEvent `json:"event"`
			Input GoldInput       `json:"input"`
		}{e, *original})
	}
	return tx.Create(&model.GoldAudit{OwnerID: actor.ID, AccountID: id, EventID: e.ID, ActorID: actor.ID, Action: action, BeforeJSON: string(b), AfterJSON: string(a)}).Error
}

// Write registers a completed event. amend replaces an opening only before any active successor.
func (s *GoldService) Write(actor *model.User, id int64, in GoldInput, amend bool) (GoldReceipt, error) {
	if _, e := s.account(s.repo.DB(), actor, id); e != nil {
		return GoldReceipt{}, e
	}
	e, err := normalizeGold(in, s.Now())
	if err != nil {
		return GoldReceipt{}, err
	}
	action := e.Kind
	if amend {
		action = "amend"
		if e.Kind != "opening" {
			return GoldReceipt{}, ErrGoldInvalid
		}
	}
	fingerprint := e
	if in.TradeDate == "" {
		fingerprint.TradeDate = time.Time{}
	}
	hash := goldDigest(action, id, fingerprint)
	var out GoldReceipt
	changed := false
	err = repository.LedgerWrite(s.repo.DB(), func(tx *gorm.DB) error {
		a, er := s.account(tx, actor, id)
		if er != nil {
			return er
		}
		prior, er := s.mutation(tx, actor, id, in.RequestKey, hash)
		if er != nil {
			return er
		}
		if prior != nil {
			out = *prior
			return nil
		}
		if e.Kind != "opening" && !CanCreateTransaction(actor) {
			return ErrTxnForbidden
		}
		if a.Status != model.StatusActive && (e.Kind == "opening" || e.Kind == "buy") {
			return ErrTxnAccountInvalid
		}
		if er = s.openDate(tx, e.TradeDate); er != nil {
			return er
		}
		r := repository.NewGoldRepo(tx)
		rows, er := r.Events(id)
		if er != nil {
			return er
		}
		var old *model.GoldEvent
		if e.Kind == "opening" {
			for i := range rows {
				if rows[i].VoidedAt == nil {
					if rows[i].Kind != "opening" || old != nil {
						return ErrGoldHistory
					}
					old = &rows[i]
				}
			}
			if amend && old == nil {
				return ErrGoldMissing
			}
			if !amend && old != nil {
				return ErrGoldConflict
			}
		}
		if old != nil {
			if er = s.openDate(tx, old.TradeDate); er != nil {
				return er
			}
			before := *old
			e.ID = old.ID
			e.CreatedBy = old.CreatedBy
			e.OwnerID = actor.ID
			e.AccountID = id
			e.CreatedAt = old.CreatedAt
			if er = tx.Save(&e).Error; er != nil {
				return er
			}
			if er = s.audit(tx, actor, id, e, action, before, &in); er != nil {
				return er
			}
		} else {
			e.OwnerID = actor.ID
			e.AccountID = id
			e.CreatedBy = actor.ID
			if er = tx.Create(&e).Error; er != nil {
				return er
			}
			if er = s.audit(tx, actor, id, e, action, nil, &in); er != nil {
				return er
			}
		}
		// Replay every successor before committing a historical insertion. A closed successor cannot be re-costed.
		for _, later := range rows {
			if later.VoidedAt == nil && (fundDay(later.TradeDate).After(fundDay(e.TradeDate)) || fundDay(later.TradeDate).Equal(fundDay(e.TradeDate)) && later.ID > e.ID) {
				if er = s.openDate(tx, later.TradeDate); er != nil {
					return er
				}
			}
		}
		all, er := r.Events(id)
		if er != nil {
			return er
		}
		state, replayErr := replayGold(all, s.Now().In(ccb.Shanghai))
		if replayErr != nil {
			return replayErr
		}
		currentQuote, quoteErr := r.Observation(s.Now(), false)
		if quoteErr != nil {
			return quoteErr
		}
		if currentQuote != nil && state.Grams.Mul(currentQuote.SellPrice).GreaterThan(maxAmount) {
			return ErrGoldInvalid
		}
		if er = s.guardClosedHistory(tx, rows, all); er != nil {
			return er
		}
		if e.CashAccountID != nil {
			cash, er := repository.NewAccountRepo(tx).GetByID(*e.CashAccountID)
			if er != nil {
				return er
			}
			if cash == nil {
				return ErrTxnAccountInvalid
			}
			if !ownsAccount(actor, *cash) {
				return ErrTxnForbidden
			}
			if !cash.IsCash() || cash.Status != model.StatusActive {
				return ErrTxnAccountInvalid
			}
			from, to := id, cash.ID
			purpose := model.PurposeGoldRedemption
			if e.Kind == "buy" {
				from, to = to, from
				purpose = model.PurposeGoldPurchase
			}
			t := model.Transaction{Kind: model.TxnTransfer, Amount: e.CashAmount, AccountID: from, TransferAccountID: &to, EntryDate: e.TradeDate, PeriodKey: s.txns.bound(tx).deps.Periods.KeyForDate(e.TradeDate), CreatedBy: actor.ID, Source: model.SourceManual, Purpose: purpose, GoldEventID: &e.ID, Note: "建设银行积存金 " + e.Note}
			if er = repository.NewTransactionRepo(tx).Create(&t); er != nil {
				return er
			}
		}
		p, er := r.Position(id)
		if er != nil {
			return er
		}
		if p == nil {
			p = &model.GoldPosition{OwnerID: actor.ID, AccountID: id, ProductKey: ccb.ProductKey, EventVersion: 1, RebuildPending: true}
			er = tx.Create(p).Error
		} else {
			er = tx.Model(p).Updates(map[string]any{"event_version": p.EventVersion + 1, "rebuild_pending": true}).Error
		}
		if er != nil {
			return er
		}
		if er = tx.Create(&model.GoldMutation{OwnerID: actor.ID, RequestKey: in.RequestKey, PayloadHash: hash, AccountID: id, EventID: e.ID}).Error; er != nil {
			return er
		}
		out = GoldReceipt{id, e.ID}
		changed = true
		return nil
	})
	if err == nil && changed {
		s.changed()
		_ = s.Rebuild(id)
	}
	return out, err
}
func (s *GoldService) Undo(actor *model.User, id, eventID int64, key string) (GoldReceipt, error) {
	if _, e := s.account(s.repo.DB(), actor, id); e != nil {
		return GoldReceipt{}, e
	}
	if key == "" || len(key) > 128 {
		return GoldReceipt{}, ErrGoldInvalid
	}
	hash := goldDigest("undo", id, eventID)
	var out GoldReceipt
	changed := false
	err := repository.LedgerWrite(s.repo.DB(), func(tx *gorm.DB) error {
		if _, e := s.account(tx, actor, id); e != nil {
			return e
		}
		prior, e := s.mutation(tx, actor, id, key, hash)
		if e != nil {
			return e
		}
		if prior != nil {
			out = *prior
			return nil
		}
		if !CanCreateTransaction(actor) {
			return ErrTxnForbidden
		}
		rows, e := repository.NewGoldRepo(tx).Events(id)
		if e != nil {
			return e
		}
		var target *model.GoldEvent
		for i := range rows {
			if rows[i].VoidedAt == nil {
				target = &rows[i]
			}
		}
		if target == nil || target.ID != eventID {
			return ErrGoldHistory
		}
		if !CanModifyTransaction(actor, target.CreatedBy) {
			return ErrTxnForbidden
		}
		if target.CashAccountID != nil {
			cash, e := repository.NewAccountRepo(tx).GetByID(*target.CashAccountID)
			if e != nil {
				return e
			}
			if cash == nil || !ownsAccount(actor, *cash) {
				return ErrTxnForbidden
			}
			if !cash.IsCash() {
				return ErrTxnAccountInvalid
			}
		}
		if e = s.openDate(tx, target.TradeDate); e != nil {
			return e
		}
		now := s.Now()
		before := *target
		beforeRows := append([]model.GoldEvent(nil), rows...)
		target.VoidedAt = &now
		if e = s.guardClosedHistory(tx, beforeRows, rows); e != nil {
			return e
		}
		if e = tx.Save(target).Error; e != nil {
			return e
		}
		if e = tx.Where("gold_event_id = ?", eventID).Delete(&model.Transaction{}).Error; e != nil {
			return e
		}
		if e = tx.Model(&model.GoldPosition{}).Where("account_id = ?", id).Updates(map[string]any{"event_version": gorm.Expr("event_version + 1"), "rebuild_pending": true}).Error; e != nil {
			return e
		}
		if e = s.audit(tx, actor, id, *target, "undo", before, nil); e != nil {
			return e
		}
		if e = tx.Create(&model.GoldMutation{OwnerID: actor.ID, RequestKey: key, PayloadHash: hash, AccountID: id, EventID: eventID}).Error; e != nil {
			return e
		}
		out = GoldReceipt{id, eventID}
		changed = true
		return nil
	})
	if err == nil && changed {
		s.changed()
		_ = s.Rebuild(id)
	}
	return out, err
}

type GoldAccount struct {
	Account       model.Account     `json:"account"`
	Grams         decimal.Decimal   `json:"grams"`
	Cost          decimal.Decimal   `json:"cost"`
	CostPrice     *decimal.Decimal  `json:"cost_price"`
	MarketValue   *decimal.Decimal  `json:"market_value"`
	HoldingPnL    *decimal.Decimal  `json:"holding_pnl"`
	HoldingReturn *decimal.Decimal  `json:"holding_return"`
	RealizedPnL   decimal.Decimal   `json:"realized_pnl"`
	TotalPnL      *decimal.Decimal  `json:"total_pnl"`
	Fees          decimal.Decimal   `json:"fees"`
	CostEstimated bool              `json:"cost_estimated"`
	CanAmend      bool              `json:"can_amend"`
	EventVersion  int64             `json:"event_version"`
	Status        string            `json:"status"`
	Quote         GoldQuoteView     `json:"quote"`
	Events        []model.GoldEvent `json:"events"`
}

func goldPnL(st goldState, quote *model.GoldQuoteObservation) (*decimal.Decimal, *decimal.Decimal, *decimal.Decimal) {
	if !st.Grams.IsZero() && quote == nil {
		return nil, nil, nil
	}
	m := decimal.Zero
	if quote != nil {
		m = st.Grams.Mul(quote.SellPrice).Round(2)
	}
	if m.GreaterThan(maxAmount) {
		return nil, nil, nil
	}
	h := m.Sub(st.Cost).Round(2)
	t := h.Add(st.Realized).Round(2)
	return &m, &h, &t
}
func (s *GoldService) Account(actor *model.User, id int64) (GoldAccount, error) {
	var out GoldAccount
	err := s.repo.DB().Transaction(func(tx *gorm.DB) error {
		bound := *s
		bound.repo = repository.NewGoldRepo(tx)
		var e error
		out, e = bound.accountView(actor, id)
		return e
	})
	return out, err
}
func (s *GoldService) accountView(actor *model.User, id int64) (GoldAccount, error) {
	a, e := s.account(s.repo.DB(), actor, id)
	if e != nil {
		return GoldAccount{}, e
	}
	rows, e := s.repo.Events(id)
	if e != nil {
		return GoldAccount{}, e
	}
	st, e := replayGold(rows, s.Now().In(ccb.Shanghai))
	if e != nil {
		return GoldAccount{}, e
	}
	q, e := s.QuoteView()
	if e != nil {
		return GoldAccount{}, e
	}
	m, h, t := goldPnL(st, q.Quote)
	v := GoldAccount{Account: *a, Grams: st.Grams, Cost: st.Cost, RealizedPnL: st.Realized.Round(2), MarketValue: m, HoldingPnL: h, TotalPnL: t, Fees: st.Fees, CostEstimated: st.Imported, Status: "ok", Quote: q, Events: rows}
	if !st.Grams.IsZero() {
		c := st.Cost.DivRound(st.Grams, 8)
		v.CostPrice = &c
	}
	if h != nil && !st.Cost.IsZero() {
		p := h.DivRound(st.Cost, 8)
		v.HoldingReturn = &p
	}
	if m == nil {
		v.Status = "no_price"
		v.Account.ValuationStatus = "no_price"
	} else {
		v.Account.Balance = *m
	}
	active := 0
	for _, row := range rows {
		if row.VoidedAt == nil {
			active++
			v.CanAmend = row.Kind == "opening"
		}
	}
	v.CanAmend = v.CanAmend && active == 1
	p, e := s.repo.Position(id)
	if e != nil {
		return v, e
	}
	if p != nil {
		v.EventVersion = p.EventVersion
		if p.RebuildPending {
			v.Status = "rebuild_pending"
			if m == nil {
				v.Status = "no_price"
			}
		}
	}
	return v, nil
}
func (s *GoldService) Accounts(actor *model.User) ([]GoldAccount, error) {
	if fundActor(actor) != nil {
		return nil, ErrTxnForbidden
	}
	var accts []model.Account
	if e := s.repo.DB().Where("owner_id = ? AND asset_class = ?", actor.ID, "gold").Order("sort,id").Find(&accts).Error; e != nil {
		return nil, e
	}
	out := []GoldAccount{}
	for _, a := range accts {
		v, e := s.Account(actor, a.ID)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, nil
}
func (s *GoldService) Audits(actor *model.User, id int64) ([]model.GoldAudit, error) {
	if _, e := s.account(s.repo.DB(), actor, id); e != nil {
		return nil, e
	}
	rows := []model.GoldAudit{}
	e := s.repo.DB().Where("account_id = ? AND owner_id = ?", id, actor.ID).Order("id DESC").Find(&rows).Error
	return rows, e
}
func (s *GoldService) ValuesForDates(db *gorm.DB, ids []int64, dates []time.Time) (map[string]map[int64]repository.FundAccountValue, error) {
	out := map[string]map[int64]repository.FundAccountValue{}
	if len(dates) == 0 || len(ids) == 0 {
		return out, nil
	}
	r := repository.NewGoldRepo(db)
	now := s.Now().In(ccb.Shanghai)
	start := dates[0]
	for _, d := range dates {
		if d.Before(start) {
			start = d
		}
	}
	start = time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, ccb.Shanghai)
	observations, e := r.Observations(start, now)
	if e != nil {
		return nil, e
	}
	byDay := map[string]*model.GoldQuoteObservation{}
	for i := range observations {
		q := &observations[i]
		day := q.FetchedAt.In(ccb.Shanghai)
		end := time.Date(day.Year(), day.Month(), day.Day(), 23, 59, 59, 999999999, ccb.Shanghai)
		if !q.QuoteAt.After(end) {
			byDay[day.Format("2006-01-02")] = q
		}
	}
	current, e := r.Observation(now, false)
	if e != nil {
		return nil, e
	}
	for _, id := range ids {
		rows, e := r.Events(id)
		if e != nil {
			return nil, e
		}
		for _, d := range dates {
			key := d.Format("2006-01-02")
			if out[key] == nil {
				out[key] = map[int64]repository.FundAccountValue{}
			}
			q := byDay[key]
			if key == now.Format("2006-01-02") {
				q = current
			}
			st, e := replayGold(rows, d)
			if e != nil {
				return nil, e
			}
			m, _, _ := goldPnL(st, q)
			v := repository.FundAccountValue{Complete: m != nil, Status: "ok"}
			if m != nil {
				v.Value = *m
			} else {
				v.Status = "no_price"
			}
			if q != nil {
				v.NAVDate = q.QuoteAt.In(ccb.Shanghai).Format("2006-01-02 15:04:05")
				quoteID := q.ID
				v.ObservationID = &quoteID
			}
			out[key][id] = v
		}
	}
	return out, nil
}
func (s *GoldService) Rebuild(id int64) error {
	return s.repo.DB().Transaction(func(tx *gorm.DB) error {
		// Lock the position so a late projection cannot clear a newer event version.
		var p model.GoldPosition
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("account_id = ?", id).First(&p).Error; e != nil {
			return e
		}
		r := repository.NewGoldRepo(tx)
		rows, e := r.Events(id)
		if e != nil {
			return e
		}
		if len(rows) == 0 {
			return nil
		}
		dates := []time.Time{}
		start := fundDay(rows[0].TradeDate)
		today := fundToday(s.Now())
		for d := start; !d.After(today); d = d.AddDate(0, 0, 1) {
			dates = append(dates, d)
		}
		values, e := s.ValuesForDates(tx, []int64{id}, dates)
		if e != nil {
			return e
		}
		if e = tx.Where("account_id = ?", id).Delete(&model.GoldValuation{}).Error; e != nil {
			return e
		}
		projections := make([]model.GoldValuation, 0, len(dates))
		for _, d := range dates {
			st, e := replayGold(rows, d)
			if e != nil {
				return e
			}
			v := values[d.Format("2006-01-02")][id]
			row := model.GoldValuation{AccountID: id, Date: d, EventVersion: p.EventVersion, ObservationID: v.ObservationID, Grams: st.Grams, Cost: st.Cost, RealizedPnL: st.Realized, Status: v.Status}
			if v.Complete {
				m := v.Value
				row.MarketValue = &m
			}
			projections = append(projections, row)
		}
		if len(projections) > 0 {
			if e = tx.CreateInBatches(&projections, 200).Error; e != nil {
				return e
			}
		}
		return tx.Model(&p).Update("rebuild_pending", false).Error
	})
}
func (s *GoldService) Valuations(actor *model.User, id int64, start, end string) ([]model.GoldValuation, error) {
	if _, e := s.account(s.repo.DB(), actor, id); e != nil {
		return nil, e
	}
	if p, e := s.repo.Position(id); e != nil {
		return nil, e
	} else if p != nil {
		if e = s.Rebuild(id); e != nil {
			return nil, e
		}
	}
	q := s.repo.DB().Where("account_id = ?", id)
	for _, pair := range []struct{ value, op string }{{start, ">="}, {end, "<="}} {
		if pair.value != "" {
			d, e := fundDate(pair.value, s.Now())
			if e != nil {
				return nil, ErrGoldInvalid
			}
			q = q.Where("date "+pair.op+" ?", d)
		}
	}
	rows := []model.GoldValuation{}
	e := q.Order("date").Find(&rows).Error
	if e != nil {
		return nil, e
	}
	ids := []int64{}
	for _, row := range rows {
		if row.ObservationID != nil {
			ids = append(ids, *row.ObservationID)
		}
	}
	if len(ids) > 0 {
		var observations []model.GoldQuoteObservation
		if e = s.repo.DB().Where("id IN ?", ids).Find(&observations).Error; e != nil {
			return nil, e
		}
		byID := map[int64]model.GoldQuoteObservation{}
		for _, obs := range observations {
			byID[obs.ID] = obs
		}
		for i := range rows {
			if rows[i].ObservationID != nil {
				if obs, ok := byID[*rows[i].ObservationID]; ok {
					at, fetch := obs.QuoteAt, obs.FetchedAt
					rows[i].QuoteAt = &at
					rows[i].FetchedAt = &fetch
				}
			}
		}
	}
	return rows, e
}

// Isolate a stable public product from all personal identity and holdings data.
func goldSlot(now time.Time) string { return now.In(ccb.Shanghai).Format("2006010215") }

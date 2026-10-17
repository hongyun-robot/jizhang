package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"jizhang/internal/fuyao"
	"jizhang/internal/model"
	"jizhang/internal/repository"
	"jizhang/internal/wealthdata"
	"strings"
	"time"
	"unicode/utf8"
)

type WealthService struct {
	repo    *repository.WealthRepo
	txns    *TransactionService
	market  wealthdata.MarketData
	Now     func() time.Time
	OnWrite func(owner int64)
}

func NewWealthService(db *gorm.DB, txns *TransactionService, market wealthdata.MarketData) *WealthService {
	return &WealthService{repo: repository.NewWealthRepo(db), txns: txns, market: market, Now: time.Now}
}
func (s *WealthService) changed(owner int64) {
	if s.OnWrite != nil {
		s.OnWrite(owner)
	}
}
func (s *WealthService) Configured() bool { return s.market != nil && s.market.Configured() }

func (s *WealthService) configuredFor(code string) bool {
	if source, ok := s.market.(interface{ ConfiguredFor(string) bool }); ok {
		return source.ConfiguredFor(code)
	}
	return s.Configured()
}

func (s *WealthService) BindAccounts(accounts *repository.AccountRepo) {
	accounts.WealthValues = s.ValuesForDates
	accounts.Now = func() time.Time { return s.Now().In(fuyao.Shanghai) }
}

type WealthInput struct {
	RequestKey         string  `json:"request_key"`
	Kind               string  `json:"kind"`
	Code               string  `json:"code"`
	DisplayName        string  `json:"display_name"`
	Shares             string  `json:"shares"`
	CostPrice          string  `json:"cost_price"`
	TotalCost          *string `json:"total_cost,omitempty"`
	OpeningNAV         *string `json:"opening_nav,omitempty"`
	Amount             string  `json:"amount"`
	Fee                string  `json:"fee"`
	TradeNAV           string  `json:"trade_nav"`
	EffectiveDate      string  `json:"effective_date"`
	CashDate           string  `json:"cash_date"`
	RightsDate         string  `json:"rights_date"`
	RegistrationDate   string  `json:"registration_date"`
	CashAccountID      int64   `json:"cash_account_id"`
	SourceEventID      int64   `json:"source_event_id"`
	Note               string  `json:"note"`
	TradeDate          string  `json:"trade_date"`
	NAVDate            string  `json:"nav_date"`
	ExpectedDate       string  `json:"expected_date"`
	PreviousTradeDate  string  `json:"previous_trade_date"`
	AdjustmentReason   string  `json:"adjustment_reason"`
	ApplicationTime    string  `json:"application_time"`
	InsufficientPolicy string  `json:"insufficient_policy"`
}
type WealthReceipt struct {
	AccountID  int64 `json:"account_id"`
	PositionID int64 `json:"position_id"`
	EventID    int64 `json:"event_id"`
}

func wealthActor(actor *model.User) error {
	if !CanView(actor) || actor.Status == model.StatusDisabled {
		return ErrTxnForbidden
	}
	return nil
}
func (s *WealthService) account(db *gorm.DB, actor *model.User, id int64) (*model.Account, error) {
	if e := wealthActor(actor); e != nil {
		return nil, e
	}
	a, e := repository.NewAccountRepo(db).GetByID(id)
	if e != nil {
		return nil, e
	}
	if a == nil {
		return nil, ErrWealthMissing
	}
	if !ownsAccount(actor, *a) {
		return nil, ErrTxnForbidden
	}
	if !a.IsWealth() {
		return nil, ErrWealthCode
	}
	return a, nil
}
func (s *WealthService) position(db *gorm.DB, actor *model.User, id int64, lock bool) (*model.WealthPosition, error) {
	if e := wealthActor(actor); e != nil {
		return nil, e
	}
	p, e := repository.NewWealthRepo(db).Position(actor.ID, id, lock)
	if e != nil {
		return nil, e
	}
	if p == nil {
		var n int64
		if err := db.Model(&model.WealthPosition{}).Where("id = ?", id).Count(&n).Error; err != nil {
			return nil, err
		}
		if n > 0 {
			return nil, ErrTxnForbidden
		}
		return nil, ErrWealthMissing
	}
	if p.OwnerID != actor.ID {
		return nil, ErrTxnForbidden
	}
	if _, e = s.account(db, actor, p.AccountID); e != nil {
		return nil, e
	}
	return p, nil
}

// Catalog is public metadata, restricted to products admitted for new holdings.
func (s *WealthService) Catalog(actor *model.User) ([]model.WealthProduct, error) {
	if err := wealthActor(actor); err != nil {
		return nil, err
	}
	return s.repo.Catalog()
}
func wealthCanonicalCode(code string) (string, error) {
	code = strings.ToUpper(strings.TrimSpace(code))
	if _, ok := wealthdata.Catalog(code); !ok {
		return "", ErrWealthCode
	}
	return code, nil
}
func (s *WealthService) product(code string) (*model.WealthProduct, error) {
	full, err := wealthCanonicalCode(code)
	if err != nil {
		return nil, err
	}
	row, err := s.repo.ProductForCode(full)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, ErrWealthCode
	}
	return row, nil
}
func normalizeWealthInput(in WealthInput, now time.Time) (WealthInput, model.WealthEvent, error) {
	var e model.WealthEvent
	in.RequestKey = strings.TrimSpace(in.RequestKey)
	if in.RequestKey == "" || len(in.RequestKey) > 128 || utf8.RuneCountInString(strings.TrimSpace(in.DisplayName)) > 128 || utf8.RuneCountInString(in.Note) > 255 {
		return in, e, ErrWealthInvalid
	}
	in.DisplayName = strings.TrimSpace(in.DisplayName)
	if in.Kind != "opening" && (in.TotalCost != nil || in.OpeningNAV != nil) {
		return in, e, ErrWealthInvalid
	}
	if wealthOrderKind(in.Kind) {
		return normalizeWealthOrder(in, now)
	}
	if in.Kind != "settlement" && in.SourceEventID != 0 {
		return in, e, ErrWealthInvalid
	}
	if in.Kind == "opening" && in.EffectiveDate == "" {
		in.EffectiveDate = wealthToday(now).Format("2006-01-02")
	}
	d, err := wealthDate(in.EffectiveDate, now)
	if err != nil {
		return in, e, err
	}
	e.Kind = in.Kind
	e.EffectiveDate = d
	e.Note = in.Note
	for _, x := range []struct {
		value  string
		target **time.Time
	}{{in.CashDate, &e.CashDate}, {in.RightsDate, &e.RightsDate}, {in.RegistrationDate, &e.RegistrationDate}} {
		if x.value != "" {
			v, err := wealthDate(x.value, now)
			if err != nil {
				return in, e, err
			}
			*x.target = &v
		}
	}
	q, err := wealthNumber(in.Shares, in.Kind == "cash_dividend" || in.Kind == "settlement", 8)
	if err != nil {
		return in, e, err
	}
	e.Shares = q
	in.Shares = q.String()
	f, err := wealthNumber(in.Fee, true, 2)
	if err != nil {
		return in, e, err
	}
	e.Fee = f
	in.Fee = f.String()
	switch in.Kind {
	case "opening":
		if !f.IsZero() || e.CashDate != nil || in.CashAccountID != 0 || e.RightsDate != nil {
			return in, e, ErrWealthInvalid
		}
		var price decimal.Decimal
		if in.TotalCost != nil {
			if strings.TrimSpace(*in.TotalCost) == "" || strings.TrimSpace(in.CostPrice) != "" {
				return in, e, ErrWealthInvalid
			}
			total, err := wealthNumber(*in.TotalCost, true, 2)
			if err != nil {
				return in, e, err
			}
			price = total.DivRound(q, 8)
			if price.GreaterThan(maxAmount) {
				return in, e, ErrWealthInvalid
			}
			e.Amount = total
			canonical := total.String()
			in.TotalCost = &canonical
		} else {
			if strings.TrimSpace(in.CostPrice) == "" {
				return in, e, ErrWealthInvalid
			}
			var err error
			price, err = wealthNumber(in.CostPrice, true, 8)
			if err != nil {
				return in, e, err
			}
			e.Amount = q.Mul(price)
			in.CostPrice = price.String()
		}
		e.TradeNAV = price
		if in.OpeningNAV != nil {
			nav, err := wealthNumber(*in.OpeningNAV, false, 8)
			if err != nil {
				return in, e, err
			}
			canonical := nav.String()
			in.OpeningNAV = &canonical
			putOrderData(&e, WealthOrderData{NAVDate: e.EffectiveDate.Format("2006-01-02"), OpeningNAV: canonical, NAVSource: "user_input"})
		}
		in.Amount = e.Amount.String()
		in.TradeNAV = "0"
	case "buy", "redeem", "reinvest":
		amount, err := wealthNumber(in.Amount, false, 2)
		if err != nil {
			return in, e, err
		}
		nav, err := wealthNumber(in.TradeNAV, in.Kind != "buy" && in.TradeNAV == "", 8)
		if err != nil {
			return in, e, err
		}
		e.Amount = amount
		e.TradeNAV = nav
		in.Amount = amount.String()
		in.TradeNAV = nav.String()
		principal := amount
		if in.Kind != "redeem" {
			principal = amount.Sub(f)
			if !principal.IsPositive() {
				return in, e, ErrWealthInvalid
			}
		} else if f.GreaterThan(amount) {
			return in, e, ErrWealthInvalid
		}
		if nav.IsPositive() && q.Mul(nav).Sub(principal).Abs().GreaterThan(decimal.RequireFromString("0.01")) {
			return in, e, ErrWealthInvalid
		}
		if in.Kind == "reinvest" {
			if e.RightsDate == nil || !f.IsZero() {
				return in, e, ErrWealthInvalid
			}
			if e.RightsDate.After(e.EffectiveDate) || e.CashDate != nil || in.CashAccountID != 0 {
				return in, e, ErrWealthInvalid
			}
		}
	case "cash_dividend":
		amount, err := wealthNumber(in.Amount, false, 2)
		if err != nil || !f.IsZero() || !q.IsZero() || e.RightsDate == nil {
			return in, e, ErrWealthInvalid
		}
		e.Amount = amount
		in.Amount = amount.String()
		if !e.EffectiveDate.Equal(*e.RightsDate) {
			return in, e, ErrWealthInvalid
		}
	case "settlement":
		if in.SourceEventID <= 0 || !q.IsZero() || !f.IsZero() || e.RightsDate != nil || e.RegistrationDate != nil || (e.CashDate != nil && !e.CashDate.Equal(e.EffectiveDate)) {
			return in, e, ErrWealthInvalid
		}
		e.SourceEventID = &in.SourceEventID
		e.SettlementKey = &in.SourceEventID
		e.CashDate = &e.EffectiveDate
	default:
		return in, e, ErrWealthInvalid
	}
	if e.Amount.GreaterThan(maxAmount) {
		return in, e, ErrWealthInvalid
	}
	if in.Kind == "buy" && (e.CashDate == nil || e.CashDate.After(e.EffectiveDate)) {
		return in, e, ErrWealthInvalid
	}
	if (in.Kind == "buy" || in.Kind == "redeem" || in.Kind == "opening") && (e.RightsDate != nil || e.RegistrationDate != nil) {
		return in, e, ErrWealthInvalid
	}
	if e.CashDate == nil && in.CashAccountID != 0 {
		return in, e, ErrWealthInvalid
	}
	if (in.Kind == "redeem" || in.Kind == "cash_dividend") && e.CashDate != nil {
		start := e.EffectiveDate
		if e.RightsDate != nil {
			start = *e.RightsDate
		}
		if e.CashDate.Before(start) {
			return in, e, ErrWealthInvalid
		}
	}
	if e.RegistrationDate != nil && e.RightsDate != nil && e.RegistrationDate.After(*e.RightsDate) {
		return in, e, ErrWealthInvalid
	}
	if e.CashDate != nil && in.CashAccountID <= 0 {
		return in, e, ErrWealthInvalid
	}
	if in.CashAccountID > 0 {
		e.CashAccountID = &in.CashAccountID
	}
	return in, e, nil
}
func wealthHash(action string, accountID, positionID int64, in WealthInput) string {
	in.RequestKey = ""
	b, _ := json.Marshal(struct {
		Action                string
		AccountID, PositionID int64
		Input                 WealthInput
	}{action, accountID, positionID, in})
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func wealthReceipt(m *model.WealthMutation) WealthReceipt {
	return WealthReceipt{AccountID: 0, PositionID: m.PositionID, EventID: m.EventID}
}

func (s *WealthService) CreateOpening(ctx context.Context, actor *model.User, accountID int64, in WealthInput) (WealthReceipt, error) {
	in.Kind = "opening"
	return s.record(ctx, actor, accountID, 0, "opening", in)
}
func (s *WealthService) FirstBuy(ctx context.Context, actor *model.User, accountID int64, in WealthInput) (WealthReceipt, error) {
	in.Kind = "buy"
	return s.record(ctx, actor, accountID, 0, "first_buy", in)
}
func (s *WealthService) Subscribe(ctx context.Context, actor *model.User, accountID int64, in WealthInput) (WealthReceipt, error) {
	in.Kind = "subscribe"
	return s.record(ctx, actor, accountID, 0, "subscription", in)
}
func (s *WealthService) Operate(ctx context.Context, actor *model.User, positionID int64, in WealthInput) (WealthReceipt, error) {
	p, e := s.position(s.repo.DB(), actor, positionID, false)
	if e != nil {
		return WealthReceipt{}, e
	}
	if in.Kind == "opening" {
		return WealthReceipt{}, ErrWealthInvalid
	}
	return s.record(ctx, actor, p.AccountID, positionID, "event", in)
}
func (s *WealthService) AmendOpening(ctx context.Context, actor *model.User, positionID int64, in WealthInput) (WealthReceipt, error) {
	p, e := s.position(s.repo.DB(), actor, positionID, false)
	if e != nil {
		return WealthReceipt{}, e
	}
	in.Kind = "opening"
	return s.record(ctx, actor, p.AccountID, positionID, "amend", in)
}

func (s *WealthService) record(ctx context.Context, actor *model.User, accountID, positionID int64, action string, in WealthInput) (WealthReceipt, error) {
	a, err := s.account(s.repo.DB(), actor, accountID)
	if err != nil {
		return WealthReceipt{}, err
	}
	if positionID > 0 {
		p, e := s.position(s.repo.DB(), actor, positionID, false)
		if e != nil {
			return WealthReceipt{}, e
		}
		var item model.WealthProduct
		if e = s.repo.DB().First(&item, p.ProductID).Error; e != nil {
			return WealthReceipt{}, e
		}
		if in.Code != "" {
			code, e := wealthCanonicalCode(in.Code)
			if e != nil || code != item.Code {
				return WealthReceipt{}, ErrWealthCode
			}
		}
		in.Code = item.Code
	}
	full, e := wealthCanonicalCode(in.Code)
	if e != nil {
		return WealthReceipt{}, e
	}
	in.Code = full
	dateDefaulted := in.EffectiveDate == ""
	normal, event, e := normalizeWealthInput(in, s.Now())
	if e != nil {
		return WealthReceipt{}, e
	}
	hashInput := normal
	if dateDefaulted {
		hashInput.EffectiveDate = ""
	}
	hash := wealthHash(action, accountID, positionID, hashInput)
	if prior, e := s.repo.Mutation(actor.ID, normal.RequestKey); e != nil {
		return WealthReceipt{}, e
	} else if prior != nil {
		if prior.PayloadHash != hash {
			return WealthReceipt{}, ErrWealthConflict
		}
		r := wealthReceipt(prior)
		r.AccountID = accountID
		return r, nil
	}
	if event.Kind != "opening" && !CanCreateTransaction(actor) {
		return WealthReceipt{}, ErrTxnForbidden
	}
	if (event.Kind == "opening" || event.Kind == "buy" || event.Kind == "subscribe") && a.Status != model.StatusActive {
		return WealthReceipt{}, ErrTxnAccountInvalid
	}
	item, err := s.product(full)
	if err != nil {
		return WealthReceipt{}, err
	}
	var result WealthReceipt
	wrote := false
	err = repository.LedgerWrite(s.repo.DB(), func(tx *gorm.DB) error {
		r := s.repo.At(tx)
		currentProduct, e := r.Product(item.ID, true)
		if e != nil {
			return e
		}
		if currentProduct == nil {
			return ErrWealthCode
		}
		if ((event.Kind == "opening" && action != "amend") || event.Kind == "buy" || event.Kind == "subscribe") && currentProduct.AdmissionStatus != "enabled" {
			return ErrWealthCode
		}
		for _, date := range []*time.Time{&event.EffectiveDate, event.CashDate, event.RightsDate, event.RegistrationDate} {
			if date != nil && wealthDay(*date).Before(wealthDay(currentProduct.InceptionDate)) {
				return ErrWealthInvalid
			}
		}
		currentAccount, e := s.account(tx, actor, accountID)
		if e != nil {
			return e
		}
		prior, e := r.Mutation(actor.ID, normal.RequestKey)
		if e != nil {
			return e
		}
		if prior != nil {
			if prior.PayloadHash != hash {
				return ErrWealthConflict
			}
			result = wealthReceipt(prior)
			result.AccountID = accountID
			return nil
		}
		if (event.Kind == "opening" || event.Kind == "buy" || event.Kind == "subscribe") && currentAccount.Status != model.StatusActive {
			return ErrTxnAccountInvalid
		}
		var p *model.WealthPosition
		if positionID > 0 {
			p, e = s.position(tx, actor, positionID, true)
		} else {
			p, e = r.PositionForCode(actor.ID, accountID, item.ID)
		}
		if e != nil {
			return e
		}
		newPosition := p == nil
		if newPosition {

			p = &model.WealthPosition{OwnerID: actor.ID, AccountID: accountID, ProductID: item.ID, DisplayName: normal.DisplayName, ValuationBasis: "cny_wealth_unit_nav", BasisConfirmedBy: actor.ID, BasisConfirmedAt: s.Now(), EventVersion: 1}
			if e = tx.Create(p).Error; e != nil {
				return e
			}
		}
		rows, e := r.Events(actor.ID, p.ID)
		if e != nil {
			return e
		}
		active := []model.WealthEvent{}
		for _, v := range rows {
			if v.VoidedAt == nil {
				active = append(active, v)
			}
		}
		if action == "opening" && len(active) > 0 {
			return ErrWealthConflict
		}
		if action != "amend" {
			for _, v := range active {
				if v.Kind == "opening" {
					for _, date := range []*time.Time{&event.EffectiveDate, event.CashDate, event.RightsDate} {
						if date != nil && wealthDay(*date).Before(wealthDay(v.EffectiveDate)) {
							return ErrWealthHistory
						}
					}
					break
				}
			}
		}
		if e = s.checkCash(tx, actor, event.CashAccountID); e != nil {
			return e
		}
		event.OwnerID = actor.ID
		event.AccountID = accountID
		event.PositionID = p.ID
		event.RightsPositionID = p.ID
		event.CreatedBy = actor.ID
		if event.Kind == "opening" && normal.OpeningNAV != nil {
			data, e := r.LoadAccounts([]int64{p.AccountID})
			if e != nil {
				return e
			}
			meta := orderData(event)
			for _, quote := range wealthQuotes(data, actor.ID, p.ProductID) {
				if quote.NAVDate.Format("2006-01-02") == meta.NAVDate && quote.UnitNAV.String() == meta.OpeningNAV {
					meta.NAVSource = quote.Source
					break
				}
			}
			putOrderData(&event, meta)
		}
		if e = s.prepareWealthOrder(tx, actor, p, normal, &event, active); e != nil {
			return e
		}
		var before model.WealthEvent
		if action == "amend" {
			if len(active) != 1 || active[0].Kind != "opening" {
				return ErrWealthHistory
			}
			before = active[0]
			event.ID = before.ID
			event.CreatedAt = before.CreatedAt
		}
		if event.Kind == "settlement" {
			var source *model.WealthEvent
			for i := range active {
				if active[i].ID == normal.SourceEventID {
					source = &active[i]
					break
				}
			}
			if source == nil {
				if err := s.foreignEvent(tx, actor, normal.SourceEventID); err != nil {
					return err
				}
				return ErrWealthMissing
			}
			if source.CashDate != nil || !(source.Kind == "redeem" || source.Kind == "cash_dividend" || source.Kind == "confirm_redeem") {
				return ErrWealthConflict
			}
			start := source.EffectiveDate
			if source.RightsDate != nil {
				start = *source.RightsDate
			}
			if event.EffectiveDate.Before(wealthDay(start)) {
				return ErrWealthInvalid
			}
			event.Amount = source.Amount
			if source.Kind == "redeem" || source.Kind == "confirm_redeem" {
				event.Amount = source.Amount.Sub(source.Fee)
			}
			if !event.Amount.IsPositive() || event.CashAccountID == nil {
				return ErrWealthInvalid
			}
			for _, existing := range active {
				if existing.SourceEventID != nil && *existing.SourceEventID == source.ID {
					return ErrWealthConflict
				}
			}
		}
		if event.Kind == "cash_dividend" || event.Kind == "reinvest" {
			key := event.RightsDate.Format("2006-01-02")
			event.CorporateKey = &key
		}
		checkRows := append([]model.WealthEvent(nil), rows...)
		if action == "amend" {
			for i := range checkRows {
				if checkRows[i].ID == event.ID {
					checkRows[i] = event
				}
			}
		} else {
			event.ID = 1 << 62
			checkRows = append(checkRows, event)
		}
		// A backdated insertion may change later cost allocations, including closed periods.
		for _, v := range checkRows {
			if v.VoidedAt != nil {
				continue
			}
			if action == "amend" || !v.EffectiveDate.Before(event.EffectiveDate) {
				if e = s.openDates(tx, v); e != nil {
					return e
				}
			}
		}
		if action == "amend" {
			if e = s.openDates(tx, before); e != nil {
				return e
			}
		}
		state, e := replayWealth(checkRows, wealthToday(s.Now()))
		if e != nil {
			return e
		}
		if e = validateWealthReservations(checkRows, wealthToday(s.Now())); e != nil {
			return e
		}
		checkedDates := map[string]bool{}
		for _, v := range checkRows {
			date := wealthDay(v.EffectiveDate)
			key := date.Format("2006-01-02")
			if !checkedDates[key] {
				if _, e = replayWealth(checkRows, date); e != nil {
					return e
				}
				checkedDates[key] = true
				if e = validateWealthReservations(checkRows, date); e != nil {
					return e
				}
			}
		}
		if state.Cash.IsNegative() || state.Cash.GreaterThan(maxAmount) {
			return ErrWealthInvalid
		}
		if e = s.accountRange(tx, p, checkRows, event); e != nil {
			return e
		}
		if action == "amend" {
			if e = tx.Save(&event).Error; e != nil {
				return e
			}
		} else {
			event.ID = 0
			if e = tx.Create(&event).Error; e != nil {
				if event.CorporateKey != nil || event.SettlementKey != nil {
					return ErrWealthConflict
				}
				return e
			}
		}
		if e = s.projectWealthCash(tx, actor, p, item, &event, active); e != nil {
			return e
		}
		if !newPosition {
			if e = tx.Model(p).Updates(map[string]any{"event_version": p.EventVersion + 1, "rebuild_pending": true, "display_name": func() string {
				if action == "amend" {
					return normal.DisplayName
				}
				return p.DisplayName
			}()}).Error; e != nil {
				return e
			}
		}
		m := model.WealthMutation{OwnerID: actor.ID, RequestKey: normal.RequestKey, PayloadHash: hash, Action: action, PositionID: p.ID, EventID: event.ID}
		if e = tx.Create(&m).Error; e != nil {
			return e
		}
		{
			b, _ := json.Marshal(before)
			after, _ := json.Marshal(event)
			if e = tx.Create(&model.WealthAudit{OwnerID: actor.ID, PositionID: p.ID, EventID: event.ID, ActorID: actor.ID, Action: action, BeforeJSON: string(b), AfterJSON: string(after)}).Error; e != nil {
				return e
			}
		}
		result = WealthReceipt{accountID, p.ID, event.ID}
		wrote = true
		return nil
	})
	if err == nil && wrote {
		s.changed(actor.ID)
		_ = s.RebuildPosition(result.PositionID)
	}
	return result, err
}

func (s *WealthService) checkCash(tx *gorm.DB, actor *model.User, id *int64) error {
	if id == nil {
		return nil
	}
	a, e := repository.NewAccountRepo(tx).GetByID(*id)
	if e != nil {
		return e
	}
	if a == nil {
		return ErrTxnAccountInvalid
	}
	if !ownsAccount(actor, *a) {
		return ErrTxnForbidden
	}
	if !a.IsCash() || a.Status != model.StatusActive {
		return ErrTxnAccountInvalid
	}
	return nil
}
func (s *WealthService) openDates(tx *gorm.DB, e model.WealthEvent) error {
	inner := s.txns.bound(tx)
	for _, d := range []*time.Time{&e.EffectiveDate, e.CashDate, e.RightsDate} {
		if d != nil {
			if err := inner.guardOpen(inner.deps.Periods.KeyForDate(wealthDay(*d))); err != nil {
				return err
			}
		}
	}
	return nil
}
func (s *WealthService) projectWealthCash(tx *gorm.DB, actor *model.User, p *model.WealthPosition, item *model.WealthProduct, e *model.WealthEvent, previous []model.WealthEvent) error {
	if e.CashDate == nil || e.CashAccountID == nil {
		return nil
	}
	amount := e.Amount
	purpose := model.TxnPurpose("wealth_dividend")
	from, to := p.AccountID, *e.CashAccountID
	switch e.Kind {
	case "buy", "subscribe":
		purpose = model.TxnPurpose("wealth_purchase")
		from, to = to, from
	case "redeem", "confirm_redeem":
		purpose = model.TxnPurpose("wealth_redemption")
		amount = amount.Sub(e.Fee)
	case "cash_dividend":
	case "refund_buy":
		purpose = model.TxnPurpose("wealth_redemption")
	case "settlement":
		for _, v := range previous {
			if e.SourceEventID != nil && v.ID == *e.SourceEventID {
				if v.Kind == "buy" {
					purpose = model.TxnPurpose("wealth_purchase")
					from, to = to, from
				} else if v.Kind == "redeem" || v.Kind == "confirm_redeem" {
					purpose = model.TxnPurpose("wealth_redemption")
				}
				break
			}
		}
	default:
		return nil
	}
	if amount.IsZero() {
		return nil
	}
	if e := s.checkCash(tx, actor, e.CashAccountID); e != nil {
		return e
	}
	t := model.Transaction{Kind: model.TxnTransfer, Amount: amount, AccountID: from, TransferAccountID: &to, EntryDate: *e.CashDate, PeriodKey: s.txns.bound(tx).deps.Periods.KeyForDate(*e.CashDate), Note: item.Code + " 理财资金", CreatedBy: actor.ID, Source: model.SourceManual, Purpose: purpose, WealthEventID: &e.ID}
	if e.Kind == "subscribe" {
		t.Note = item.Code + " 已付款待确认申购"
	}
	if e.Kind == "refund_buy" {
		t.Note = item.Code + " 申购取消或失败退款"
	}
	return repository.NewTransactionRepo(tx).Create(&t)
}

func (s *WealthService) Undo(actor *model.User, positionID, eventID int64, key string) (WealthReceipt, error) {
	p, err := s.position(s.repo.DB(), actor, positionID, false)
	if err != nil {
		return WealthReceipt{}, err
	}
	key = strings.TrimSpace(key)
	if key == "" || len(key) > 128 {
		return WealthReceipt{}, ErrWealthInvalid
	}
	hash := wealthHash("undo", p.AccountID, positionID, WealthInput{SourceEventID: eventID})
	var result WealthReceipt
	changed := false
	err = repository.LedgerWrite(s.repo.DB(), func(tx *gorm.DB) error {
		r := s.repo.At(tx)
		if _, e := r.Product(p.ProductID, true); e != nil {
			return e
		}
		p, e := s.position(tx, actor, positionID, true)
		if e != nil {
			return e
		}
		if prior, e := r.Mutation(actor.ID, key); e != nil {
			return e
		} else if prior != nil {
			if prior.PayloadHash != hash {
				return ErrWealthConflict
			}
			result = wealthReceipt(prior)
			result.AccountID = p.AccountID
			return nil
		}
		if !CanCreateTransaction(actor) {
			return ErrTxnForbidden
		}
		rows, e := r.Events(actor.ID, positionID)
		if e != nil {
			return e
		}
		var target *model.WealthEvent
		for i := range rows {
			if rows[i].ID == eventID {
				target = &rows[i]
				break
			}
		}
		if target == nil {
			if err := s.foreignEvent(tx, actor, eventID); err != nil {
				return err
			}
			return ErrWealthMissing
		}
		if target.OwnerID != actor.ID || !CanModifyTransaction(actor, target.CreatedBy) {
			return ErrTxnForbidden
		}
		if target.VoidedAt != nil {
			return ErrWealthConflict
		}
		for _, v := range rows {
			if v.VoidedAt == nil && ((v.SourceEventID != nil && *v.SourceEventID == eventID) || wealthOrderDepends(v, eventID)) {
				return ErrWealthHistory
			}
		}
		for _, v := range rows {
			if v.VoidedAt == nil && (wealthDay(v.EffectiveDate).After(wealthDay(target.EffectiveDate)) || (wealthDay(v.EffectiveDate).Equal(wealthDay(target.EffectiveDate)) && v.ID > target.ID)) {
				return ErrWealthHistory
			}
		}
		if e = s.openDates(tx, *target); e != nil {
			return e
		}
		now := s.Now()
		before, _ := json.Marshal(target)
		if e = tx.Model(target).Updates(map[string]any{"voided_at": now, "corporate_key": nil, "settlement_key": nil}).Error; e != nil {
			return e
		}
		if e = tx.Where("wealth_event_id = ?", eventID).Delete(&model.Transaction{}).Error; e != nil {
			return e
		}
		if e = tx.Model(p).Updates(map[string]any{"event_version": p.EventVersion + 1, "rebuild_pending": true}).Error; e != nil {
			return e
		}
		if e = tx.Create(&model.WealthAudit{OwnerID: actor.ID, PositionID: positionID, EventID: eventID, ActorID: actor.ID, Action: "undo", BeforeJSON: string(before), AfterJSON: "{}"}).Error; e != nil {
			return e
		}
		if e = tx.Create(&model.WealthMutation{OwnerID: actor.ID, RequestKey: key, PayloadHash: hash, Action: "undo", PositionID: positionID, EventID: eventID}).Error; e != nil {
			return e
		}
		result = WealthReceipt{p.AccountID, positionID, eventID}
		changed = true
		return nil
	})
	if err == nil && changed {
		s.changed(actor.ID)
		_ = s.RebuildPosition(positionID)
	}
	return result, err
}

func (s *WealthService) foreignEvent(tx *gorm.DB, actor *model.User, id int64) error {
	var owner struct{ OwnerID int64 }
	if err := tx.Model(&model.WealthEvent{}).Select("owner_id").Where("id = ?", id).Scan(&owner).Error; err != nil {
		return err
	}
	if owner.OwnerID > 0 && owner.OwnerID != actor.ID {
		return ErrTxnForbidden
	}
	return nil
}

func (s *WealthService) Events(actor *model.User, id int64) ([]model.WealthEvent, error) {
	if _, e := s.position(s.repo.DB(), actor, id, false); e != nil {
		return nil, e
	}
	return s.repo.Events(actor.ID, id)
}
func (s *WealthService) Audits(actor *model.User, id int64) ([]model.WealthAudit, error) {
	if _, e := s.position(s.repo.DB(), actor, id, false); e != nil {
		return nil, e
	}
	return s.repo.Audits(actor.ID, id)
}

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
	"strings"
	"time"
	"unicode/utf8"
)

type FundService struct {
	repo    *repository.FundRepo
	txns    *TransactionService
	market  fuyao.MarketData
	Now     func() time.Time
	OnWrite func()
}

func NewFundService(db *gorm.DB, txns *TransactionService, market fuyao.MarketData) *FundService {
	return &FundService{repo: repository.NewFundRepo(db), txns: txns, market: market, Now: time.Now}
}
func (s *FundService) changed() {
	if s.OnWrite != nil {
		s.OnWrite()
	}
}
func (s *FundService) Configured() bool { return s.market != nil && s.market.Configured() }

func (s *FundService) BindAccounts(accounts *repository.AccountRepo) {
	accounts.FundValues = s.ValuesForDates
	accounts.Now = func() time.Time { return s.Now().In(fuyao.Shanghai) }
}

type FundInput struct {
	RequestKey       string `json:"request_key"`
	Kind             string `json:"kind"`
	Code             string `json:"code"`
	DisplayName      string `json:"display_name"`
	ValuationBasis   string `json:"valuation_basis"`
	BasisConfirmed   bool   `json:"basis_confirmed"`
	Shares           string `json:"shares"`
	CostPrice        string `json:"cost_price"`
	Amount           string `json:"amount"`
	Fee              string `json:"fee"`
	TradeNAV         string `json:"trade_nav"`
	EffectiveDate    string `json:"effective_date"`
	CashDate         string `json:"cash_date"`
	RightsDate       string `json:"rights_date"`
	RegistrationDate string `json:"registration_date"`
	CashAccountID    int64  `json:"cash_account_id"`
	SourceEventID    int64  `json:"source_event_id"`
	Note             string `json:"note"`
}
type FundReceipt struct {
	AccountID  int64 `json:"account_id"`
	PositionID int64 `json:"position_id"`
	EventID    int64 `json:"event_id"`
}

func fundActor(actor *model.User) error {
	if !CanView(actor) || actor.Status == model.StatusDisabled {
		return ErrTxnForbidden
	}
	return nil
}
func (s *FundService) account(db *gorm.DB, actor *model.User, id int64) (*model.Account, error) {
	if e := fundActor(actor); e != nil {
		return nil, e
	}
	a, e := repository.NewAccountRepo(db).GetByID(id)
	if e != nil {
		return nil, e
	}
	if a == nil {
		return nil, ErrFundMissing
	}
	if !ownsAccount(actor, *a) {
		return nil, ErrTxnForbidden
	}
	if !a.IsFund() {
		return nil, ErrFundCode
	}
	return a, nil
}
func (s *FundService) position(db *gorm.DB, actor *model.User, id int64, lock bool) (*model.FundPosition, error) {
	if e := fundActor(actor); e != nil {
		return nil, e
	}
	p, e := repository.NewFundRepo(db).Position(id, lock)
	if e != nil {
		return nil, e
	}
	if p == nil {
		return nil, ErrFundMissing
	}
	if p.OwnerID != actor.ID {
		return nil, ErrTxnForbidden
	}
	if _, e = s.account(db, actor, p.AccountID); e != nil {
		return nil, e
	}
	return p, nil
}
func (s *FundService) Search(ctx context.Context, actor *model.User, q string) ([]fuyao.Instrument, error) {
	if e := fundActor(actor); e != nil {
		return nil, e
	}
	if s.market == nil {
		return nil, fuyao.ErrUnconfigured
	}
	return s.market.Search(ctx, q)
}
func (s *FundService) instrument(ctx context.Context, code string) (*model.FundInstrument, error) {
	full, e := fundCanonicalCode(code)
	if e != nil {
		return nil, e
	}
	old, e := s.repo.Instrument(full)
	if e != nil {
		return nil, e
	}
	if old != nil {
		if old.AssetType != "fund-otc" {
			return nil, ErrFundCode
		}
		return old, nil
	}
	if s.market == nil {
		return nil, fuyao.ErrUnconfigured
	}
	rows, e := s.market.Search(ctx, strings.TrimSuffix(full, ".OF"))
	if e != nil {
		return nil, e
	}
	var found *fuyao.Instrument
	for i := range rows {
		if rows[i].THSCode == full && rows[i].AssetType == "fund-otc" {
			found = &rows[i]
			break
		}
	}
	if found == nil {
		return nil, ErrFundCode
	}
	p, e := s.market.Profile(ctx, full)
	if e != nil {
		return nil, e
	}
	row := model.FundInstrument{THSCode: full, Ticker: found.Ticker, Name: p.Name, AssetType: found.AssetType, Manager: p.Manager, Management: p.Management}
	if e = s.repo.SaveInstrument(&row); e != nil {
		return nil, e
	}
	return s.repo.Instrument(full)
}
func normalizeFundInput(in FundInput, now time.Time) (FundInput, model.FundEvent, error) {
	var e model.FundEvent
	in.RequestKey = strings.TrimSpace(in.RequestKey)
	if in.RequestKey == "" || len(in.RequestKey) > 128 || utf8.RuneCountInString(strings.TrimSpace(in.DisplayName)) > 128 || utf8.RuneCountInString(in.Note) > 255 {
		return in, e, ErrFundInvalid
	}
	in.DisplayName = strings.TrimSpace(in.DisplayName)
	if in.Kind == "opening" && in.EffectiveDate == "" {
		in.EffectiveDate = fundToday(now).Format("2006-01-02")
	}
	d, err := fundDate(in.EffectiveDate, now)
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
			v, err := fundDate(x.value, now)
			if err != nil {
				return in, e, err
			}
			*x.target = &v
		}
	}
	q, err := fundNumber(in.Shares, in.Kind == "cash_dividend" || in.Kind == "settlement", 8)
	if err != nil {
		return in, e, err
	}
	e.Shares = q
	in.Shares = q.String()
	f, err := fundNumber(in.Fee, true, 2)
	if err != nil {
		return in, e, err
	}
	e.Fee = f
	in.Fee = f.String()
	switch in.Kind {
	case "opening":
		if !f.IsZero() || e.CashDate != nil || in.CashAccountID != 0 || e.RightsDate != nil {
			return in, e, ErrFundInvalid
		}
		price, err := fundNumber(in.CostPrice, true, 8)
		if err != nil {
			return in, e, err
		}
		e.TradeNAV = price
		e.Amount = q.Mul(price)
		in.CostPrice = price.String()
		in.Amount = e.Amount.String()
		in.TradeNAV = "0"
	case "buy", "redeem", "reinvest":
		amount, err := fundNumber(in.Amount, false, 2)
		if err != nil {
			return in, e, err
		}
		nav, err := fundNumber(in.TradeNAV, false, 8)
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
				return in, e, ErrFundInvalid
			}
		} else if f.GreaterThan(amount) {
			return in, e, ErrFundInvalid
		}
		if q.Mul(nav).Sub(principal).Abs().GreaterThan(decimal.RequireFromString("0.01")) {
			return in, e, ErrFundInvalid
		}
		if in.Kind == "reinvest" {
			if e.RightsDate == nil {
				return in, e, ErrFundInvalid
			}
			if e.RightsDate.After(e.EffectiveDate) || e.CashDate != nil || in.CashAccountID != 0 {
				return in, e, ErrFundInvalid
			}
		}
	case "cash_dividend":
		amount, err := fundNumber(in.Amount, true, 2)
		if err != nil || !f.IsZero() || e.RightsDate == nil {
			return in, e, ErrFundInvalid
		}
		e.Amount = amount
		in.Amount = amount.String()
		if !e.EffectiveDate.Equal(*e.RightsDate) {
			return in, e, ErrFundInvalid
		}
	case "settlement":
		if in.SourceEventID <= 0 || !q.IsZero() || !f.IsZero() {
			return in, e, ErrFundInvalid
		}
		e.SourceEventID = &in.SourceEventID
		e.SettlementKey = &in.SourceEventID
		e.CashDate = &e.EffectiveDate
	default:
		return in, e, ErrFundInvalid
	}
	if e.Amount.GreaterThan(maxAmount) {
		return in, e, ErrFundInvalid
	}
	if in.Kind == "buy" && e.CashDate == nil {
		return in, e, ErrFundInvalid
	}
	if (in.Kind == "redeem" || in.Kind == "cash_dividend") && e.CashDate != nil {
		start := e.EffectiveDate
		if e.RightsDate != nil {
			start = *e.RightsDate
		}
		if e.CashDate.Before(start) {
			return in, e, ErrFundInvalid
		}
	}
	if e.RegistrationDate != nil && e.RightsDate != nil && e.RegistrationDate.After(*e.RightsDate) {
		return in, e, ErrFundInvalid
	}
	if e.CashDate != nil && in.CashAccountID <= 0 {
		return in, e, ErrFundInvalid
	}
	if in.CashAccountID > 0 {
		e.CashAccountID = &in.CashAccountID
	}
	return in, e, nil
}
func fundHash(action string, accountID, positionID int64, in FundInput) string {
	in.RequestKey = ""
	b, _ := json.Marshal(struct {
		Action                string
		AccountID, PositionID int64
		Input                 FundInput
	}{action, accountID, positionID, in})
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func fundReceipt(m *model.FundMutation) FundReceipt {
	return FundReceipt{AccountID: 0, PositionID: m.PositionID, EventID: m.EventID}
}

func (s *FundService) CreateOpening(ctx context.Context, actor *model.User, accountID int64, in FundInput) (FundReceipt, error) {
	in.Kind = "opening"
	return s.record(ctx, actor, accountID, 0, "opening", in)
}
func (s *FundService) FirstBuy(ctx context.Context, actor *model.User, accountID int64, in FundInput) (FundReceipt, error) {
	in.Kind = "buy"
	return s.record(ctx, actor, accountID, 0, "first_buy", in)
}
func (s *FundService) Operate(ctx context.Context, actor *model.User, positionID int64, in FundInput) (FundReceipt, error) {
	p, e := s.position(s.repo.DB(), actor, positionID, false)
	if e != nil {
		return FundReceipt{}, e
	}
	if in.Kind == "opening" {
		return FundReceipt{}, ErrFundInvalid
	}
	return s.record(ctx, actor, p.AccountID, positionID, "event", in)
}
func (s *FundService) AmendOpening(ctx context.Context, actor *model.User, positionID int64, in FundInput) (FundReceipt, error) {
	p, e := s.position(s.repo.DB(), actor, positionID, false)
	if e != nil {
		return FundReceipt{}, e
	}
	in.Kind = "opening"
	return s.record(ctx, actor, p.AccountID, positionID, "amend", in)
}

func (s *FundService) record(ctx context.Context, actor *model.User, accountID, positionID int64, action string, in FundInput) (FundReceipt, error) {
	a, err := s.account(s.repo.DB(), actor, accountID)
	if err != nil {
		return FundReceipt{}, err
	}
	if positionID > 0 {
		p, e := s.position(s.repo.DB(), actor, positionID, false)
		if e != nil {
			return FundReceipt{}, e
		}
		var item model.FundInstrument
		if e = s.repo.DB().First(&item, p.InstrumentID).Error; e != nil {
			return FundReceipt{}, e
		}
		if in.Code != "" {
			code, e := fundCanonicalCode(in.Code)
			if e != nil || code != item.THSCode {
				return FundReceipt{}, ErrFundCode
			}
		}
		in.Code = item.THSCode
	}
	full, e := fundCanonicalCode(in.Code)
	if e != nil {
		return FundReceipt{}, e
	}
	in.Code = full
	dateDefaulted := in.EffectiveDate == ""
	normal, event, e := normalizeFundInput(in, s.Now())
	if e != nil {
		return FundReceipt{}, e
	}
	hashInput := normal
	if dateDefaulted {
		hashInput.EffectiveDate = ""
	}
	hash := fundHash(action, accountID, positionID, hashInput)
	if prior, e := s.repo.Mutation(actor.ID, normal.RequestKey); e != nil {
		return FundReceipt{}, e
	} else if prior != nil {
		if prior.PayloadHash != hash {
			return FundReceipt{}, ErrFundConflict
		}
		r := fundReceipt(prior)
		r.AccountID = accountID
		return r, nil
	}
	if event.Kind != "opening" && !CanCreateTransaction(actor) {
		return FundReceipt{}, ErrTxnForbidden
	}
	if (event.Kind == "opening" || event.Kind == "buy") && a.Status != model.StatusActive {
		return FundReceipt{}, ErrTxnAccountInvalid
	}
	item, err := s.instrument(ctx, full)
	if err != nil {
		return FundReceipt{}, err
	}
	if _, err = s.repo.NAVAt(item.ID, fundToday(s.Now())); err != nil {
		return FundReceipt{}, err
	}
	if price, e := s.repo.NAVAt(item.ID, fundToday(s.Now())); e != nil {
		return FundReceipt{}, e
	} else if price == nil {
		if e = s.syncInstrument(ctx, item.ID, true); e != nil {
			return FundReceipt{}, e
		}
		price, e = s.repo.NAVAt(item.ID, fundToday(s.Now()))
		if e != nil || price == nil {
			return FundReceipt{}, ErrFundPrice
		}
	}
	var result FundReceipt
	wrote := false
	err = repository.LedgerWrite(s.repo.DB(), func(tx *gorm.DB) error {
		r := s.repo.At(tx)
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
				return ErrFundConflict
			}
			result = fundReceipt(prior)
			result.AccountID = accountID
			return nil
		}
		if (event.Kind == "opening" || event.Kind == "buy") && currentAccount.Status != model.StatusActive {
			return ErrTxnAccountInvalid
		}
		var p *model.FundPosition
		if positionID > 0 {
			p, e = s.position(tx, actor, positionID, true)
		} else {
			p, e = r.PositionForCode(accountID, item.ID)
		}
		if e != nil {
			return e
		}
		newPosition := p == nil
		if newPosition {
			if !normal.BasisConfirmed || normal.ValuationBasis != "cny_otc_unit_nav" {
				return ErrFundCode
			}
			p = &model.FundPosition{OwnerID: actor.ID, AccountID: accountID, InstrumentID: item.ID, DisplayName: normal.DisplayName, ValuationBasis: normal.ValuationBasis, BasisConfirmedBy: actor.ID, BasisConfirmedAt: s.Now(), EventVersion: 1}
			if e = tx.Create(p).Error; e != nil {
				return e
			}
		}
		rows, e := r.Events(p.ID)
		if e != nil {
			return e
		}
		active := []model.FundEvent{}
		for _, v := range rows {
			if v.VoidedAt == nil {
				active = append(active, v)
			}
		}
		if action == "opening" && len(active) > 0 {
			return ErrFundConflict
		}
		if action != "amend" {
			for _, v := range active {
				if v.Kind == "opening" {
					for _, date := range []*time.Time{&event.EffectiveDate, event.CashDate, event.RightsDate} {
						if date != nil && fundDay(*date).Before(fundDay(v.EffectiveDate)) {
							return ErrFundHistory
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
		var before model.FundEvent
		if action == "amend" {
			if len(active) != 1 || active[0].Kind != "opening" {
				return ErrFundHistory
			}
			before = active[0]
			event.ID = before.ID
			event.CreatedAt = before.CreatedAt
		}
		if event.Kind == "settlement" {
			var source *model.FundEvent
			for i := range active {
				if active[i].ID == normal.SourceEventID {
					source = &active[i]
					break
				}
			}
			if source == nil {
				return ErrFundMissing
			}
			if source.CashDate != nil || !(source.Kind == "buy" || source.Kind == "redeem" || source.Kind == "cash_dividend") {
				return ErrFundConflict
			}
			start := source.EffectiveDate
			if source.RightsDate != nil {
				start = *source.RightsDate
			}
			if event.EffectiveDate.Before(fundDay(start)) {
				return ErrFundInvalid
			}
			event.Amount = source.Amount
			if source.Kind == "redeem" {
				event.Amount = source.Amount.Sub(source.Fee)
			}
			if !event.Amount.IsPositive() || event.CashAccountID == nil {
				return ErrFundInvalid
			}
		}
		if event.Kind == "cash_dividend" || event.Kind == "reinvest" {
			key := event.RightsDate.Format("2006-01-02")
			event.CorporateKey = &key
		}
		checkRows := append([]model.FundEvent(nil), rows...)
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
		if _, e = replayFund(checkRows, fundToday(s.Now())); e != nil {
			return e
		}
		for _, v := range checkRows {
			if _, e = replayFund(checkRows, fundDay(v.EffectiveDate)); e != nil {
				return e
			}
		}
		state, e := replayFund(checkRows, fundToday(s.Now()))
		if e != nil {
			return e
		}
		price, e := r.NAVAt(item.ID, fundToday(s.Now()))
		if e != nil || price == nil {
			return ErrFundPrice
		}
		if state.Shares.Mul(price.UnitNAV).GreaterThan(maxAmount) {
			return ErrFundInvalid
		}
		if action == "amend" {
			if e = tx.Save(&event).Error; e != nil {
				return e
			}
		} else {
			event.ID = 0
			if e = tx.Create(&event).Error; e != nil {
				if event.CorporateKey != nil || event.SettlementKey != nil {
					return ErrFundConflict
				}
				return e
			}
		}
		if e = s.projectFundCash(tx, actor, p, item, &event, active); e != nil {
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
		m := model.FundMutation{OwnerID: actor.ID, RequestKey: normal.RequestKey, PayloadHash: hash, Action: action, PositionID: p.ID, EventID: event.ID}
		if e = tx.Create(&m).Error; e != nil {
			return e
		}
		if action == "amend" {
			b, _ := json.Marshal(before)
			after, _ := json.Marshal(event)
			if e = tx.Create(&model.FundAudit{OwnerID: actor.ID, PositionID: p.ID, EventID: event.ID, ActorID: actor.ID, Action: "amend", BeforeJSON: string(b), AfterJSON: string(after)}).Error; e != nil {
				return e
			}
		}
		result = FundReceipt{accountID, p.ID, event.ID}
		wrote = true
		return nil
	})
	if err == nil && wrote {
		s.changed()
		_ = s.RebuildPosition(result.PositionID)
	}
	return result, err
}

func (s *FundService) checkCash(tx *gorm.DB, actor *model.User, id *int64) error {
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
func (s *FundService) openDates(tx *gorm.DB, e model.FundEvent) error {
	inner := s.txns.bound(tx)
	for _, d := range []*time.Time{&e.EffectiveDate, e.CashDate, e.RightsDate} {
		if d != nil {
			if err := inner.guardOpen(inner.deps.Periods.KeyForDate(fundDay(*d))); err != nil {
				return err
			}
		}
	}
	return nil
}
func (s *FundService) projectFundCash(tx *gorm.DB, actor *model.User, p *model.FundPosition, item *model.FundInstrument, e *model.FundEvent, previous []model.FundEvent) error {
	if e.CashDate == nil || e.CashAccountID == nil {
		return nil
	}
	amount := e.Amount
	purpose := model.TxnPurpose("fund_dividend")
	from, to := p.AccountID, *e.CashAccountID
	switch e.Kind {
	case "buy":
		purpose = model.TxnPurpose("fund_purchase")
		from, to = to, from
	case "redeem":
		purpose = model.TxnPurpose("fund_redemption")
		amount = amount.Sub(e.Fee)
	case "cash_dividend":
	case "settlement":
		for _, v := range previous {
			if e.SourceEventID != nil && v.ID == *e.SourceEventID {
				if v.Kind == "buy" {
					purpose = model.TxnPurpose("fund_purchase")
					from, to = to, from
				} else if v.Kind == "redeem" {
					purpose = model.TxnPurpose("fund_redemption")
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
	t := model.Transaction{Kind: model.TxnTransfer, Amount: amount, AccountID: from, TransferAccountID: &to, EntryDate: *e.CashDate, PeriodKey: s.txns.bound(tx).deps.Periods.KeyForDate(*e.CashDate), Note: item.THSCode + " 基金资金", CreatedBy: actor.ID, Source: model.SourceManual, Purpose: purpose, FundEventID: &e.ID}
	return repository.NewTransactionRepo(tx).Create(&t)
}

func (s *FundService) Undo(actor *model.User, positionID, eventID int64, key string) (FundReceipt, error) {
	p, err := s.position(s.repo.DB(), actor, positionID, false)
	if err != nil {
		return FundReceipt{}, err
	}
	key = strings.TrimSpace(key)
	if key == "" || len(key) > 128 {
		return FundReceipt{}, ErrFundInvalid
	}
	hash := fundHash("undo", p.AccountID, positionID, FundInput{SourceEventID: eventID})
	var result FundReceipt
	changed := false
	err = repository.LedgerWrite(s.repo.DB(), func(tx *gorm.DB) error {
		r := s.repo.At(tx)
		p, e := s.position(tx, actor, positionID, true)
		if e != nil {
			return e
		}
		if prior, e := r.Mutation(actor.ID, key); e != nil {
			return e
		} else if prior != nil {
			if prior.PayloadHash != hash {
				return ErrFundConflict
			}
			result = fundReceipt(prior)
			result.AccountID = p.AccountID
			return nil
		}
		if !CanCreateTransaction(actor) {
			return ErrTxnForbidden
		}
		rows, e := r.Events(positionID)
		if e != nil {
			return e
		}
		var target *model.FundEvent
		for i := range rows {
			if rows[i].ID == eventID {
				target = &rows[i]
				break
			}
		}
		if target == nil {
			return ErrFundMissing
		}
		if target.OwnerID != actor.ID || !CanModifyTransaction(actor, target.CreatedBy) {
			return ErrTxnForbidden
		}
		if target.VoidedAt != nil {
			return ErrFundConflict
		}
		for _, v := range rows {
			if v.VoidedAt == nil && (fundDay(v.EffectiveDate).After(fundDay(target.EffectiveDate)) || (fundDay(v.EffectiveDate).Equal(fundDay(target.EffectiveDate)) && v.ID > target.ID)) {
				return ErrFundHistory
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
		if e = tx.Where("fund_event_id = ?", eventID).Delete(&model.Transaction{}).Error; e != nil {
			return e
		}
		if e = tx.Model(p).Updates(map[string]any{"event_version": p.EventVersion + 1, "rebuild_pending": true}).Error; e != nil {
			return e
		}
		if e = tx.Create(&model.FundAudit{OwnerID: actor.ID, PositionID: positionID, EventID: eventID, ActorID: actor.ID, Action: "undo", BeforeJSON: string(before), AfterJSON: "{}"}).Error; e != nil {
			return e
		}
		if e = tx.Create(&model.FundMutation{OwnerID: actor.ID, RequestKey: key, PayloadHash: hash, Action: "undo", PositionID: positionID, EventID: eventID}).Error; e != nil {
			return e
		}
		result = FundReceipt{p.AccountID, positionID, eventID}
		changed = true
		return nil
	})
	if err == nil && changed {
		s.changed()
		_ = s.RebuildPosition(positionID)
	}
	return result, err
}

func (s *FundService) Events(actor *model.User, id int64) ([]model.FundEvent, error) {
	if _, e := s.position(s.repo.DB(), actor, id, false); e != nil {
		return nil, e
	}
	return s.repo.Events(id)
}
func (s *FundService) Audits(actor *model.User, id int64) ([]model.FundAudit, error) {
	if _, e := s.position(s.repo.DB(), actor, id, false); e != nil {
		return nil, e
	}
	rows := []model.FundAudit{}
	e := s.repo.DB().Where("position_id = ? AND owner_id = ?", id, actor.ID).Order("id DESC").Find(&rows).Error
	return rows, e
}

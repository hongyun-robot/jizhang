package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"jizhang/internal/model"
	"jizhang/internal/repository"
)

type WealthCashInput struct {
	RequestKey       string `json:"request_key"`
	ExpectedRevision int64  `json:"expected_revision"`
	ProductID        int64  `json:"product_id"`
	StartDate        string `json:"start_date"`
	Active           bool   `json:"active"`
	Policy           string `json:"policy"`
}
type WealthCashYieldInput struct {
	RequestKey       string  `json:"request_key"`
	ExpectedRevision int64   `json:"expected_revision"`
	Amount           string  `json:"amount"`
	Balance          *string `json:"balance,omitempty"`
	Note             string  `json:"note"`
}
type WealthCashReceipt struct {
	AccountID int64 `json:"account_id"`
	LinkID    int64 `json:"link_id"`
	YieldID   int64 `json:"yield_id,omitempty"`
	Revision  int64 `json:"revision"`
}
type WealthCashView struct {
	Account          model.Account           `json:"account"`
	Link             *model.WealthCashLink   `json:"link"`
	Product          *model.WealthProduct    `json:"product"`
	Yields           []model.WealthCashYield `json:"yields"`
	Audits           []model.WealthCashAudit `json:"audits"`
	UnitNAV          *decimal.Decimal        `json:"unit_nav"`
	NAVDate          string                  `json:"nav_date"`
	EstimatedShares  *decimal.Decimal        `json:"estimated_shares"`
	ConfirmedAmount  decimal.Decimal         `json:"confirmed_amount"`
	PendingAmount    decimal.Decimal         `json:"pending_amount"`
	UninvestedAmount decimal.Decimal         `json:"uninvested_amount"`
	TotalYield       decimal.Decimal         `json:"total_yield"`
}

func (s *WealthService) cashAccount(db *gorm.DB, actor *model.User, id int64, lock bool) (*model.Account, error) {
	if err := wealthActor(actor); err != nil {
		return nil, err
	}
	var account model.Account
	query := db.Where("id = ?", id)
	if lock {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	if err := query.First(&account).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrWealthMissing
		}
		return nil, err
	}
	if account.OwnerID == nil || *account.OwnerID != actor.ID {
		return nil, ErrTxnForbidden
	}
	if !account.IsCash() {
		return nil, ErrTxnAccountInvalid
	}
	return &account, nil
}

func cashHash(action string, id int64, input any) string {
	raw, _ := json.Marshal(struct {
		Action string
		ID     int64
		Input  any
	}{action, id, input})
	hash := sha256.Sum256(raw)
	return hex.EncodeToString(hash[:])
}
func cashKey(key string) bool { return key != "" && len(key) <= 128 && strings.TrimSpace(key) == key }
func cashReplay(audit *model.WealthCashAudit, hash string) (WealthCashReceipt, error) {
	var receipt WealthCashReceipt
	if audit.PayloadHash != hash {
		return receipt, ErrWealthConflict
	}
	return receipt, json.Unmarshal([]byte(audit.AfterJSON), &receipt)
}
func (s *WealthService) cashAudit(tx *gorm.DB, link model.WealthCashLink, yield *int64, action, key, hash string, before, after any, receipt WealthCashReceipt) error {
	return cashAuditAt(tx, s.Now(), link, yield, action, key, hash, before, after, receipt)
}

func cashAuditAt(tx *gorm.DB, now time.Time, link model.WealthCashLink, yield *int64, action, key, hash string, before, after any, receipt WealthCashReceipt) error {
	old, _ := json.Marshal(before)
	next, _ := json.Marshal(struct {
		WealthCashReceipt
		State any `json:"state"`
	}{receipt, after})
	var request *string
	if key != "" {
		request = &key
	}
	return tx.Create(&model.WealthCashAudit{OwnerID: link.OwnerID, LinkID: link.ID, YieldID: yield, Action: action, RequestKey: request, PayloadHash: hash, BeforeJSON: string(old), AfterJSON: string(next), CreatedBy: link.OwnerID, CreatedAt: now}).Error
}

func (s *WealthService) ConfigureCash(ctx context.Context, actor *model.User, accountID int64, in WealthCashInput) (WealthCashReceipt, error) {
	var receipt WealthCashReceipt
	if err := wealthActor(actor); err != nil {
		return receipt, err
	}
	if !CanCreateTransaction(actor) {
		return receipt, ErrTxnForbidden
	}
	if !cashKey(in.RequestKey) || in.ExpectedRevision < 0 {
		return receipt, ErrWealthInvalid
	}
	payload := in
	payload.RequestKey = ""
	hash := cashHash("cash_config", accountID, payload)
	err := repository.LedgerWrite(s.repo.DB(), func(tx *gorm.DB) error {
		r := repository.NewWealthCashRepo(tx)
		if !r.Available() {
			return ErrWealthCashSetup
		}
		account, err := s.cashAccount(tx, actor, accountID, true)
		if err != nil {
			return err
		}
		if prior, e := r.AuditForRequest(actor.ID, in.RequestKey); e != nil {
			return e
		} else if prior != nil {
			receipt, err = cashReplay(prior, hash)
			return err
		}
		link, err := r.Link(actor.ID, accountID, true)
		if err != nil {
			return err
		}
		var before any
		if link == nil {
			if in.ExpectedRevision != 0 || !in.Active {
				return ErrWealthConflict
			}
			link = &model.WealthCashLink{OwnerID: actor.ID, AccountID: accountID, CreatedAt: s.Now()}
		} else {
			if link.Revision != in.ExpectedRevision {
				return ErrWealthConflict
			}
			before = *link
		}
		if in.Active {
			if account.Status != model.StatusActive {
				return ErrTxnAccountInvalid
			}
			setup, err := validateCashSetup(tx, WealthCashSetupInput{ProductID: in.ProductID, StartDate: in.StartDate, Policy: in.Policy}, s.Now())
			if err != nil {
				return err
			}
			product, date, policy := setup.product, setup.date, setup.policy
			if link.ID > 0 && (link.ProductID != product.ID || !wealthDay(link.StartDate).Equal(date) || link.Policy != policy || !link.Active) {
				var last model.WealthCashYield
				err = tx.Where("account_id = ?", accountID).Order("nav_date DESC").First(&last).Error
				if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
					return err
				}
				if err == nil && date.Before(wealthDay(last.NAVDate)) {
					return ErrWealthConflict
				}
			}
			link.ProductID, link.StartDate, link.Policy = product.ID, date, policy
			account.AssetClass = "wealth_cash"
		} else {
			account.AssetClass = "cash"
		}
		link.Active, link.Revision, link.UpdatedAt = in.Active, link.Revision+1, s.Now()
		link.SyncStatus, link.LastError = "pending", ""
		link.CalculationHash = ""
		if err = tx.Save(link).Error; err != nil {
			return err
		}
		if err = tx.Model(account).Update("asset_class", account.AssetClass).Error; err != nil {
			return err
		}
		receipt = WealthCashReceipt{AccountID: accountID, LinkID: link.ID, Revision: link.Revision}
		return s.cashAudit(tx, *link, nil, "configuration", in.RequestKey, hash, before, *link, receipt)
	})
	if err == nil {
		s.changed(actor.ID)
	}
	return receipt, err
}

func (s *WealthService) CashAccounts(actor *model.User) ([]WealthCashView, error) {
	if err := wealthActor(actor); err != nil {
		return nil, err
	}
	links, err := repository.NewWealthCashRepo(s.repo.DB()).Links(actor.ID, true)
	if err != nil {
		return nil, err
	}
	rows := []WealthCashView{}
	for _, link := range links {
		view, err := s.CashAccount(actor, link.AccountID)
		if err != nil {
			return nil, err
		}
		rows = append(rows, view)
	}
	return rows, nil
}

func (s *WealthService) RefreshCash(ctx context.Context, actor *model.User, accountID int64) (WealthCashView, error) {
	view, err := s.CashAccount(actor, accountID)
	if err != nil {
		return view, err
	}
	if !CanCreateTransaction(actor) {
		return view, ErrTxnForbidden
	}
	if view.Link == nil || !view.Link.Active {
		return view, ErrWealthCashSetup
	}
	if _, err = s.RefreshProduct(ctx, actor, view.Link.ProductID); err != nil {
		return view, err
	}
	changed := false
	err = repository.LedgerWrite(s.repo.DB(), func(tx *gorm.DB) error {
		account, e := s.cashAccount(tx, actor, accountID, true)
		if e != nil {
			return e
		}
		link, e := repository.NewWealthCashRepo(tx).Link(actor.ID, accountID, true)
		if e != nil {
			return e
		}
		if link == nil || !link.Active {
			return ErrWealthConflict
		}
		changed, e = s.reconcileCashAt(tx, *account, link)
		return e
	})
	if err != nil {
		return view, err
	}
	if changed {
		s.changed(actor.ID)
	}
	return s.CashAccount(actor, accountID)
}
func (s *WealthService) CashAccount(actor *model.User, accountID int64) (WealthCashView, error) {
	var view WealthCashView
	account, err := s.cashAccount(s.repo.DB(), actor, accountID, false)
	if err != nil {
		return view, err
	}
	r := repository.NewWealthCashRepo(s.repo.DB())
	view.Account = *account
	view.Yields = []model.WealthCashYield{}
	view.Audits = []model.WealthCashAudit{}
	view.Account.Balance, err = r.Balance(*account, wealthToday(s.Now()))
	if err != nil {
		return view, err
	}
	if !r.Available() {
		return view, nil
	}
	view.Link, err = r.Link(actor.ID, accountID, false)
	if err != nil || view.Link == nil {
		return view, err
	}
	view.Product, err = s.repo.Product(view.Link.ProductID, false)
	if err != nil {
		return view, err
	}
	view.Yields, err = r.Yields(actor.ID, accountID)
	if err != nil {
		return view, err
	}
	view.Audits, err = r.Audits(actor.ID, view.Link.ID)
	if err != nil {
		return view, err
	}
	for _, row := range view.Yields {
		view.TotalYield = view.TotalYield.Add(row.Amount)
	}
	quotes, err := s.repo.NAVs(view.Link.ProductID)
	if err != nil {
		return view, err
	}
	for _, quote := range quotes {
		if !wealthDay(quote.NAVDate).After(wealthToday(s.Now())) {
			view.UnitNAV = decimalPointer(quote.UnitNAV)
			view.NAVDate = wealthDay(quote.NAVDate).Format("2006-01-02")
		}
	}
	if view.Link.Active {
		initial, err := r.Balance(*account, wealthDay(view.Link.StartDate))
		if err != nil {
			return view, err
		}
		flows, err := r.Movements(accountID, wealthToday(s.Now()))
		if err != nil {
			return view, err
		}
		accepted := map[string]decimal.Decimal{}
		for _, row := range view.Yields {
			accepted[wealthDay(row.NAVDate).Format("2006-01-02")] = row.Amount
		}
		_, exposure, err := cashYieldPlans(*view.Link, initial, flows, quotes, s.Now(), func(date time.Time, calculated decimal.Decimal) decimal.Decimal {
			if amount, ok := accepted[date.Format("2006-01-02")]; ok {
				return amount
			}
			return decimal.Zero
		})
		if err != nil {
			return view, err
		}
		view.ConfirmedAmount = exposure.confirmed.Round(2)
		view.UninvestedAmount = exposure.free.Round(2)
		for _, lot := range exposure.pending {
			view.PendingAmount = view.PendingAmount.Add(lot.amount)
		}
		if view.UnitNAV != nil && view.ConfirmedAmount.IsPositive() {
			view.EstimatedShares = decimalPointer(view.ConfirmedAmount.DivRound(*view.UnitNAV, 8))
		}
	}
	return view, nil
}

// postCashYield changes one owned, open-period cash fact. The metadata remains
// available even when the adopted amount is zero and no zero-value txn exists.
func (s *WealthService) postCashYield(tx *gorm.DB, row *model.WealthCashYield) error {
	if row.Amount.IsZero() {
		if row.TxnID != nil {
			id := *row.TxnID
			if err := tx.Model(row).Update("txn_id", nil).Error; err != nil {
				return err
			}
			if err := tx.Where("id = ? AND wealth_cash_yield_id = ? AND created_by = ? AND account_id = ?", id, row.ID, row.OwnerID, row.AccountID).Delete(&model.Transaction{}).Error; err != nil {
				return err
			}
			row.TxnID = nil
		}
		return nil
	}
	kind := model.TxnIncome
	if row.Amount.IsNegative() {
		kind = model.TxnExpense
	}
	values := map[string]any{"kind": kind, "amount": row.Amount.Abs(), "account_id": row.AccountID, "entry_date": row.NAVDate, "period_key": s.txns.bound(tx).deps.Periods.KeyForDate(wealthDay(row.NAVDate)), "note": row.Note, "updated_at": s.Now()}
	if row.TxnID == nil {
		values["source"] = model.SourceWealthAuto
		values["purpose"] = model.PurposeWealthCashYield
		values["created_by"] = row.OwnerID
		values["wealth_cash_yield_id"] = row.ID
		values["created_at"] = s.Now()
		values["platform"] = ""
		if err := tx.Table("transactions").Create(values).Error; err != nil {
			return err
		}
		var transaction model.Transaction
		if err := tx.Where("wealth_cash_yield_id = ? AND account_id = ? AND created_by = ?", row.ID, row.AccountID, row.OwnerID).First(&transaction).Error; err != nil {
			return err
		}
		row.TxnID = &transaction.ID
	} else {
		result := tx.Table("transactions").Where("id = ? AND wealth_cash_yield_id = ? AND account_id = ? AND created_by = ?", *row.TxnID, row.ID, row.AccountID, row.OwnerID).Updates(values)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			var count int64
			if err := tx.Table("transactions").Where("id = ? AND wealth_cash_yield_id = ? AND account_id = ? AND created_by = ?", *row.TxnID, row.ID, row.AccountID, row.OwnerID).Count(&count).Error; err != nil {
				return err
			}
			if count != 1 {
				return ErrWealthConflict
			}
		}
	}
	return nil
}

func (s *WealthService) reconcileCashAt(tx *gorm.DB, account model.Account, link *model.WealthCashLink) (bool, error) {
	r := repository.NewWealthCashRepo(tx)
	quotes, err := s.repo.At(tx).NAVs(link.ProductID)
	if err != nil {
		return false, err
	}
	flows, err := r.Movements(account.ID, wealthToday(s.Now()))
	if err != nil {
		return false, err
	}
	initial, err := r.Balance(account, wealthDay(link.StartDate))
	if err != nil {
		return false, err
	}
	old, err := r.Yields(link.OwnerID, account.ID)
	if err != nil {
		return false, err
	}
	periods, err := repository.NewClosingRepo(tx).List()
	if err != nil {
		return false, err
	}
	overrides := []model.WealthCashYield{}
	for _, row := range old {
		if row.ManualOverride {
			overrides = append(overrides, row)
		}
	}
	type quoteInput struct {
		Date  time.Time
		Value decimal.Decimal
		Total *decimal.Decimal
	}
	quoteInputs := make([]quoteInput, 0, len(quotes))
	for _, quote := range quotes {
		quoteInputs = append(quoteInputs, quoteInput{wealthDay(quote.NAVDate), quote.UnitNAV, quote.TotalNAV})
	}
	inputHash := cashHash("cash_calculation", link.Revision, struct {
		Initial   decimal.Decimal
		Flows     []repository.CashMovement
		Quotes    []quoteInput
		Overrides []model.WealthCashYield
		Closed    []model.PeriodClosing
	}{initial, flows, quoteInputs, overrides, periods})
	if link.CalculationHash == inputHash {
		return false, nil
	}
	byDate := map[string]model.WealthCashYield{}
	for _, row := range old {
		byDate[wealthDay(row.NAVDate).Format("2006-01-02")] = row
	}
	covered := func(date time.Time) bool {
		for _, row := range old {
			if row.ManualOverride && row.ManualStartDate != nil && date.After(wealthDay(*row.ManualStartDate)) && date.Before(wealthDay(row.NAVDate)) {
				return true
			}
		}
		return false
	}
	closed := map[string]bool{}
	review := map[string]bool{}
	for i := 1; i < len(quotes); i++ {
		a, b := quotes[i-1], quotes[i]
		if a.TotalNAV != nil && b.TotalNAV != nil && !a.TotalNAV.Sub(a.UnitNAV).Equal(b.TotalNAV.Sub(b.UnitNAV)) {
			review[wealthDay(b.NAVDate).Format("2006-01-02")] = true
		}
	}
	for _, quote := range quotes {
		day := wealthDay(quote.NAVDate)
		if !day.After(wealthDay(link.StartDate)) {
			continue
		}
		key := s.txns.bound(tx).deps.Periods.KeyForDate(day)
		if _, ok := closed[key]; !ok {
			closed[key], err = repository.NewClosingRepo(tx).IsClosed(key)
			if err != nil {
				return false, err
			}
		}
	}
	plans, _, err := cashYieldPlans(*link, initial, flows, quotes, s.Now(), func(date time.Time, calculated decimal.Decimal) decimal.Decimal {
		prior, exists := byDate[date.Format("2006-01-02")]
		if exists && prior.ManualOverride {
			return prior.Amount
		}
		if closed[s.txns.bound(tx).deps.Periods.KeyForDate(date)] || review[date.Format("2006-01-02")] {
			if exists {
				return prior.Amount
			}
			return decimal.Zero
		}
		if covered(date) {
			return decimal.Zero
		}
		return calculated
	})
	if err != nil {
		return false, err
	}
	changed, blocked, dividendReview := false, false, false
	for _, plan := range plans {
		row, exists := byDate[plan.Date.Format("2006-01-02")]
		status := "applied"
		if covered(plan.Date) && !row.ManualOverride {
			status = "manual_interval"
		}
		if review[plan.Date.Format("2006-01-02")] && !row.ManualOverride {
			status = "dividend_review"
			dividendReview = true
		}
		if closed[s.txns.bound(tx).deps.Periods.KeyForDate(plan.Date)] && !row.ManualOverride && !covered(plan.Date) && !plan.Calculated.Equal(plan.Accepted) {
			status = "blocked_closed"
			blocked = true
		}
		if exists && row.CalculatedAmount.Equal(plan.Calculated) && row.Amount.Equal(plan.Accepted) && row.UnitNAV.Equal(plan.NAV) && row.PreviousNAV.Equal(plan.PreviousNAV) && wealthDay(row.PreviousNAVDate).Equal(plan.PreviousDate) && row.Status == status {
			continue
		}
		before := row
		row.LinkID, row.OwnerID, row.AccountID, row.ProductID = link.ID, link.OwnerID, account.ID, link.ProductID
		row.NAVDate, row.PreviousNAVDate, row.UnitNAV, row.PreviousNAV = plan.Date, plan.PreviousDate, plan.NAV, plan.PreviousNAV
		row.CalculatedAmount, row.Amount, row.Revision, row.Status = plan.Calculated, plan.Accepted, row.Revision+1, status
		row.UpdatedAt = s.Now()
		if !exists {
			row.CreatedAt = s.Now()
			row.Note = fmt.Sprintf("理财零钱净值区间收益 %s → %s", plan.PreviousDate.Format("2006-01-02"), plan.Date.Format("2006-01-02"))
		}
		if !exists {
			if err = tx.Create(&row).Error; err != nil {
				return false, err
			}
		}
		if !closed[s.txns.bound(tx).deps.Periods.KeyForDate(plan.Date)] {
			if err = s.postCashYield(tx, &row); err != nil {
				return false, err
			}
		}
		if err = tx.Save(&row).Error; err != nil {
			return false, err
		}
		id := row.ID
		if err = s.cashAudit(tx, *link, &id, "automatic", "", "", before, row, WealthCashReceipt{AccountID: account.ID, LinkID: link.ID, YieldID: row.ID, Revision: row.Revision}); err != nil {
			return false, err
		}
		changed = true
	}
	now := s.Now()
	link.LastReconciledAt = &now
	link.LastError = ""
	link.SyncStatus = "no_new_yield"
	if len(plans) == 0 {
		link.SyncStatus = "waiting_nav"
	} else if changed {
		link.SyncStatus = "updated"
	}
	if blocked {
		link.SyncStatus = "blocked_closed"
		link.LastError = "封账区间有待核对收益，保留原流水；可在重开后重新计算"
	}
	if dividendReview && !blocked {
		link.SyncStatus = "dividend_review"
		link.LastError = "累计净值与单位净值差额变化，需核对分红；保留原入账金额，可按实际收益修改"
	}
	link.CalculationHash = inputHash
	if err = tx.Save(link).Error; err != nil {
		return false, err
	}
	return changed, nil
}

func (s *WealthService) ensureCashYields(ctx context.Context) error {
	links, err := repository.NewWealthCashRepo(s.repo.DB()).Links(0, true)
	if err != nil {
		return err
	}
	var first error
	for _, candidate := range links {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		changed := false
		err = repository.LedgerWrite(s.repo.DB(), func(tx *gorm.DB) error {
			var owner model.User
			if e := tx.First(&owner, candidate.OwnerID).Error; e != nil {
				return e
			}
			if owner.Status != model.StatusActive || !CanCreateTransaction(&owner) {
				return nil
			}
			account, e := s.cashAccount(tx, &owner, candidate.AccountID, true)
			if e != nil {
				return e
			}
			link, e := repository.NewWealthCashRepo(tx).Link(owner.ID, account.ID, true)
			if e != nil {
				return e
			}
			if link == nil || !link.Active || !account.IsWealthCash() {
				return nil
			}
			changed, e = s.reconcileCashAt(tx, *account, link)
			return e
		})
		if err != nil {
			if first == nil {
				first = err
			}
		} else if changed {
			s.changed(candidate.OwnerID)
		}
	}
	return first
}

func (s *WealthService) CashYield(actor *model.User, id int64) (model.WealthCashYield, error) {
	var row model.WealthCashYield
	if err := wealthActor(actor); err != nil {
		return row, err
	}
	err := s.repo.DB().Where("id = ? AND owner_id = ?", id, actor.ID).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return row, ErrWealthMissing
	}
	return row, err
}

var cashYieldNumber = regexp.MustCompile(`^-?\d+(\.\d{1,2})?$`)

func (s *WealthService) EditCashYield(ctx context.Context, actor *model.User, id int64, in WealthCashYieldInput) (WealthCashReceipt, error) {
	var receipt WealthCashReceipt
	if err := wealthActor(actor); err != nil {
		return receipt, err
	}
	if !CanCreateTransaction(actor) {
		return receipt, ErrTxnForbidden
	}
	value := in.Amount
	if in.Balance != nil {
		if in.Amount != "" {
			return receipt, ErrWealthInvalid
		}
		value = *in.Balance
	}
	if !cashKey(in.RequestKey) || !cashYieldNumber.MatchString(value) || len(value) > 16 || utf8.RuneCountInString(in.Note) > 255 {
		return receipt, ErrWealthInvalid
	}
	amount, err := decimal.NewFromString(value)
	if err != nil || amount.Abs().GreaterThan(maxAmount) {
		return receipt, ErrWealthInvalid
	}
	if in.Balance == nil {
		in.Amount = amount.String()
	} else {
		normalized := amount.String()
		in.Balance = &normalized
	}
	payload := in
	payload.RequestKey = ""
	hash := cashHash("cash_yield", id, payload)
	err = repository.LedgerWrite(s.repo.DB(), func(tx *gorm.DB) error {
		var row model.WealthCashYield
		if e := tx.Where("id = ? AND owner_id = ?", id, actor.ID).First(&row).Error; e != nil {
			if errors.Is(e, gorm.ErrRecordNotFound) {
				return ErrWealthMissing
			}
			return e
		}
		account, e := s.cashAccount(tx, actor, row.AccountID, true)
		if e != nil {
			return e
		}
		r := repository.NewWealthCashRepo(tx)
		if prior, e := r.AuditForRequest(actor.ID, in.RequestKey); e != nil {
			return e
		} else if prior != nil {
			receipt, e = cashReplay(prior, hash)
			return e
		}
		link, e := r.Link(actor.ID, account.ID, true)
		if e != nil {
			return e
		}
		if link == nil {
			return ErrWealthMissing
		}
		if e = tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, id).Error; e != nil {
			return e
		}
		if row.Revision != in.ExpectedRevision {
			return ErrWealthConflict
		}
		if e = s.txns.bound(tx).guardOpen(s.txns.bound(tx).deps.Periods.KeyForDate(wealthDay(row.NAVDate))); e != nil {
			return e
		}
		before := row
		if in.Balance != nil {
			current, e := r.Balance(*account, wealthToday(s.Now()))
			if e != nil {
				return e
			}
			amount = row.Amount.Add(amount.Sub(current))
			if amount.Abs().GreaterThan(maxAmount) {
				return ErrWealthInvalid
			}
		}
		if row.ManualStartDate == nil {
			start := row.PreviousNAVDate
			row.ManualStartDate = &start
		}
		row.Amount, row.Note, row.ManualOverride, row.Revision, row.Status, row.UpdatedAt = amount, in.Note, true, row.Revision+1, "applied", s.Now()
		if e = s.postCashYield(tx, &row); e != nil {
			return e
		}
		if e = tx.Save(&row).Error; e != nil {
			return e
		}
		receipt = WealthCashReceipt{AccountID: row.AccountID, LinkID: link.ID, YieldID: row.ID, Revision: row.Revision}
		if e = s.cashAudit(tx, *link, &id, "manual", in.RequestKey, hash, before, row, receipt); e != nil {
			return e
		}
		if link.Active {
			_, e = s.reconcileCashAt(tx, *account, link)
		}
		return e
	})
	if err == nil {
		s.changed(actor.ID)
	}
	return receipt, err
}

package service

import (
	"time"

	"gorm.io/gorm"
	"jizhang/internal/model"
	"jizhang/internal/repository"
	"jizhang/internal/wealthdata"
)

type WealthCashSetupInput struct {
	ProductID int64  `json:"product_id"`
	StartDate string `json:"start_date"`
	Policy    string `json:"policy,omitempty"`
}
type cashSetup struct {
	product *model.WealthProduct
	date    time.Time
	policy  string
}

// New accounts and legacy account linking use the same controlled identities,
// dates and policy defaults. Fetching NAVs is not part of account creation.
func validateCashSetup(tx *gorm.DB, in WealthCashSetupInput, now time.Time) (cashSetup, error) {
	var result cashSetup
	product, err := repository.NewWealthRepo(tx).Product(in.ProductID, true)
	if err != nil {
		return result, err
	}
	if product == nil || product.AdmissionStatus != "enabled" {
		return result, ErrWealthCode
	}
	identity, known := wealthdata.Catalog(product.Code)
	if !known || identity.Issuer != product.Issuer || identity.RegistrationCode != product.RegistrationCode || identity.Adapter != product.SourceAdapter || identity.SourceCode != product.SourceCode || identity.Currency != product.Currency || identity.ValuationBasis != product.ValuationBasis {
		return result, ErrWealthCode
	}
	date, err := wealthDate(in.StartDate, now)
	if err != nil || date.Before(wealthDay(product.InceptionDate)) {
		return result, ErrWealthInvalid
	}
	policy := in.Policy
	if policy == "" {
		policy = identity.DefaultCashPolicy
		if policy == "" {
			policy = "unit_nav"
		}
	}
	if policy != "unit_nav" && policy != "yulibao_20250715" {
		return result, ErrWealthInvalid
	}
	return cashSetup{product: product, date: date, policy: policy}, nil
}

func (s *AccountService) cashClock() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Called inside AccountService's existing ledger transaction and request-key
// reservation. A caller sees one creation, never a partially configured account.
func (s *AccountService) createCashAt(tx *gorm.DB, in CreateAccountInput) (*model.Account, error) {
	if in.Type != model.AccountAsset {
		return nil, ErrInvalidAccountType
	}
	if in.WealthCash == nil {
		return nil, ErrWealthCashSetup
	}
	if in.RequestKey == "" || in.ActorID <= 0 {
		return nil, ErrAccountCreateConflict
	}
	if in.OpeningBalance.IsNegative() {
		return nil, ErrInvalidAmount
	}
	if !repository.NewWealthCashRepo(tx).Available() {
		return nil, ErrWealthCashSetup
	}
	var actor model.User
	if err := tx.First(&actor, in.ActorID).Error; err != nil {
		return nil, ErrTxnForbidden
	}
	if err := wealthActor(&actor); err != nil {
		return nil, err
	}
	if !CanCreateTransaction(&actor) {
		return nil, ErrTxnForbidden
	}
	if audit, err := repository.NewWealthCashRepo(tx).AuditForRequest(actor.ID, in.RequestKey); err != nil {
		return nil, err
	} else if audit != nil {
		return nil, ErrAccountCreateConflict
	}
	now := s.cashClock()
	setup, err := validateCashSetup(tx, *in.WealthCash, now)
	if err != nil {
		return nil, err
	}
	ordinary := in
	ordinary.AssetClass = "cash"
	ordinary.WealthCash = nil
	account, err := s.createAt(tx, ordinary)
	if err != nil {
		return nil, err
	}
	account.AssetClass = "wealth_cash"
	account.Balance = account.OpeningBalance
	account.HasFinancialHistory = true
	if err = tx.Model(account).Update("asset_class", account.AssetClass).Error; err != nil {
		return nil, err
	}
	link := model.WealthCashLink{OwnerID: actor.ID, AccountID: account.ID, ProductID: setup.product.ID, Policy: setup.policy, StartDate: setup.date, Active: true, Revision: 1, SyncStatus: "pending", CreatedAt: now, UpdatedAt: now}
	if err = tx.Create(&link).Error; err != nil {
		return nil, err
	}
	payload := in
	payload.RequestKey = ""
	receipt := WealthCashReceipt{AccountID: account.ID, LinkID: link.ID, Revision: link.Revision}
	if err = cashAuditAt(tx, now, link, nil, "creation", in.RequestKey, cashHash("cash_create", account.ID, payload), nil, link, receipt); err != nil {
		return nil, err
	}
	return account, nil
}

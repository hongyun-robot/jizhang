package service

import (
	"gorm.io/gorm"
	"jizhang/internal/model"
	"jizhang/internal/repository"
)

func ownsLiability(actor *model.User, a model.Account) bool {
	return actor != nil && a.Type == model.AccountLiability && a.OwnerID != nil && *a.OwnerID == actor.ID
}
func ownsAccount(actor *model.User, a model.Account) bool {
	return CanView(actor) && a.OwnerID != nil && *a.OwnerID == actor.ID
}
func checkAccountOwnership(db *gorm.DB, actor *model.User, ids ...int64) error {
	if !CanView(actor) {
		return ErrTxnForbidden
	}
	for _, id := range ids {
		if id == 0 {
			continue
		}
		var a model.Account
		if err := db.First(&a, id).Error; err != nil {
			return err
		}
		if !ownsAccount(actor, a) {
			return ErrTxnForbidden
		}
	}
	return nil
}
func (s *AccountService) EnsurePersonalLiabilities(actor *model.User) error {
	if !CanView(actor) {
		return ErrTxnForbidden
	}
	changed := false
	err := repository.LedgerWrite(s.repo.DB(), func(tx *gorm.DB) error {
		var user model.User
		if err := tx.First(&user, actor.ID).Error; err != nil {
			return err
		}
		if user.LiabilityAccountsInitialized {
			return nil
		}
		var owned int64
		if err := tx.Model(&model.Account{}).Where("type = ? AND owner_id = ?", model.AccountLiability, actor.ID).Count(&owned).Error; err != nil {
			return err
		}
		if owned == 0 {
			// On a fresh database the migration-created zero accounts await the first
			// user. Adopt these identities once rather than duplicating their templates.
			var unclaimed []model.Account
			if err := tx.Where("type = ? AND owner_id IS NULL", model.AccountLiability).Find(&unclaimed).Error; err != nil {
				return err
			}
			if len(unclaimed) > 0 {
				for _, a := range unclaimed {
					var n int64
					if err := tx.Model(&model.Transaction{}).Where("account_id = ? OR transfer_account_id = ?", a.ID, a.ID).Count(&n).Error; err != nil {
						return err
					}
					if n > 0 || !a.OpeningBalance.IsZero() {
						return ErrAccountHasTransactions
					}
					if err := tx.Model(&a).Update("owner_id", actor.ID).Error; err != nil {
						return err
					}
					changed = true
				}
			}
		}
		var templates []model.LiabilityAccountType
		if err := tx.Where("default_account = ?", true).Order("sort,code").Find(&templates).Error; err != nil {
			return err
		}
		for _, t := range templates {
			var count int64
			if err := tx.Model(&model.Account{}).Where("type = ? AND owner_id = ? AND liability_type = ?", model.AccountLiability, actor.ID, t.Code).Count(&count).Error; err != nil {
				return err
			}
			if count > 0 {
				continue
			}
			owner := actor.ID
			a := model.Account{Name: t.Name, Type: model.AccountLiability, OwnerID: &owner, LiabilityType: t.Code, Status: model.StatusActive, Sort: 4 + t.Sort}
			if err := tx.Create(&a).Error; err != nil {
				return err
			}
			changed = true
		}
		return tx.Model(&user).Update("liability_accounts_initialized", true).Error
	})
	if err == nil && changed {
		s.fireWrite()
	}
	return err
}
func (s *AccountService) ListFor(actor *model.User) ([]model.Account, error) {
	if err := initializeCatalog(s.repo.DB(), actor, true); err != nil {
		return nil, err
	}
	if err := s.EnsurePersonalLiabilities(actor); err != nil {
		return nil, err
	}
	rows, err := s.repo.ForOwner(actor.ID).List()
	if err != nil {
		return nil, err
	}
	out := []model.Account{}
	for _, a := range rows {
		if ownsAccount(actor, a) {
			out = append(out, a)
		}
	}
	return out, nil
}
func (s *AccountService) CreateFor(actor *model.User, in CreateAccountInput) (*model.Account, error) {
	if !CanView(actor) {
		return nil, ErrTxnForbidden
	}
	if in.AssetClass == "wealth_cash" && in.Type != model.AccountAsset {
		return nil, ErrInvalidAccountType
	}
	if in.WealthCash != nil && in.AssetClass != "wealth_cash" {
		return nil, ErrWealthInvalid
	}
	if in.Type == model.AccountLiability {
		if err := s.EnsurePersonalLiabilities(actor); err != nil {
			return nil, err
		}
	}
	in.ActorID = actor.ID
	return s.Create(in)
}
func (s *AccountService) UpdateFor(actor *model.User, id int64, in CreateAccountInput, status model.UserStatus) (*model.Account, error) {
	a, err := s.repo.GetByID(id)
	if err != nil {
		return nil, err
	}
	if a == nil {
		return nil, ErrAccountNotFound
	}
	if !ownsAccount(actor, *a) {
		return nil, ErrTxnForbidden
	}
	in.ActorID = actor.ID
	return s.Update(id, in, status)
}
func (s *AccountService) DeleteFor(actor *model.User, id int64) error {
	a, err := s.repo.GetByID(id)
	if err != nil {
		return err
	}
	if a == nil {
		return ErrAccountNotFound
	}
	if !ownsAccount(actor, *a) {
		return ErrTxnForbidden
	}
	return s.Delete(id)
}
func (s *TransactionService) checkInputOwnership(actor *model.User, in TxnInput) error {
	if e := checkCategoryOwnership(s.deps.Accounts.DB(), actor, in.CategoryID); e != nil {
		return e
	}
	if e := checkAccountOwnership(s.deps.Accounts.DB(), actor, in.AccountID, in.TransferAccountID); e != nil {
		return e
	}
	if in.RelatedTxnID > 0 {
		t, e := s.deps.Txns.GetByID(in.RelatedTxnID)
		if e != nil {
			return e
		}
		if t != nil {
			return s.checkPostingOwnership(actor, t)
		}
	}
	return nil
}
func (s *TransactionService) checkPostingOwnership(actor *model.User, t *model.Transaction) error {
	ids := []int64{t.AccountID}
	if t.TransferAccountID != nil {
		ids = append(ids, *t.TransferAccountID)
	}
	if t.LiabilityOperationID != nil {
		var op model.LiabilityOperation
		if e := s.deps.Accounts.DB().First(&op, *t.LiabilityOperationID).Error; e != nil {
			return e
		}
		ids = append(ids, op.AccountID)
	}
	if t.LiabilityTermID != nil {
		var term model.InstallmentTerm
		if e := s.deps.Accounts.DB().First(&term, *t.LiabilityTermID).Error; e != nil {
			return e
		}
		var plan model.InstallmentPlan
		if e := s.deps.Accounts.DB().First(&plan, term.PlanID).Error; e != nil {
			return e
		}
		ids = append(ids, plan.AccountID)
	}
	return checkAccountOwnership(s.deps.Accounts.DB(), actor, ids...)
}
func (s *TransactionService) personalFilter(actor *model.User, f repository.TxnFilter) repository.TxnFilter {
	f.PersonalOwnerID = actor.ID
	if p, ok := s.deps.Txns.(interface{ DB() *gorm.DB }); ok {
		f.IncludeLiabilityLinks = p.DB().Migrator().HasTable(&model.LiabilityOperation{})
		f.LegacyTransfers = p.DB().Migrator().HasTable("catalog_before_transactions")
	}
	return f
}

package service

import (
	"context"
	"fmt"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"jizhang/internal/model"
	"jizhang/internal/repository"
)

func cashYieldCategoryName(account model.Account) string {
	name := []rune("收益-" + strings.TrimSpace(account.Name))
	if len(name) <= 64 {
		return string(name)
	}
	suffix := fmt.Sprintf("-%d", account.ID)
	return string(name[:64-len([]rune(suffix))]) + suffix
}

// The caller holds LedgerWrite, so category creation is serialized with normal
// directory edits and commits atomically with the yield transaction or audit.
func (s *WealthService) cashYieldCategory(tx *gorm.DB, account model.Account, kind model.CategoryKind, preferred *int64) (*model.Category, error) {
	if account.OwnerID == nil || *account.OwnerID <= 0 {
		return nil, ErrTxnForbidden
	}
	owner := *account.OwnerID
	if category, err := repository.NewWealthCashRepo(tx).YieldCategory(owner, account.ID, kind, preferred); err != nil || category != nil {
		return category, err
	}
	var person model.User
	if err := tx.First(&person, owner).Error; err != nil {
		return nil, err
	}
	// A first system-created category must not make later personal-directory
	// initialization mistake an otherwise empty directory for a custom one.
	if !person.CategoriesInitialized {
		if err := initializeCatalog(tx, &person, false); err != nil {
			return nil, err
		}
	}
	repo := repository.NewCategoryRepo(tx).ForOwner(owner)
	ensure := func(name string, parent *int64) (*model.Category, error) {
		category, err := repo.FindNamed(kind, name, parent)
		if err != nil || category != nil {
			return category, err
		}
		position, err := repository.CategoryOrder(tx, owner, kind, parent).NextPosition()
		if err != nil {
			return nil, err
		}
		return NewCategoryService(repository.NewCategoryRepo(tx)).Create(CreateCategoryInput{OwnerID: owner, Kind: kind, Name: name, ParentID: parent, Sort: position})
	}
	root, err := ensure("理财", nil)
	if err != nil {
		return nil, err
	}
	return ensure(cashYieldCategoryName(account), &root.ID)
}

// Legacy classification is metadata-only, including in closed periods: never
// repost a cash fact, recalculate a yield, or change transaction timestamps.
func (s *WealthService) backfillCashYieldCategories(ctx context.Context) error {
	accounts, err := repository.NewWealthCashRepo(s.repo.DB()).UncategorizedYieldAccounts()
	if err != nil {
		return err
	}
	var first error
	for _, candidate := range accounts {
		if err := ctx.Err(); err != nil {
			return err
		}
		changed := false
		err := repository.LedgerWrite(s.repo.DB().WithContext(ctx), func(tx *gorm.DB) error {
			var owner model.User
			if err := tx.First(&owner, candidate.OwnerID).Error; err != nil {
				return err
			}
			if owner.Status != model.StatusActive || !CanView(&owner) {
				return nil
			}
			account, err := s.cashAccount(tx, &owner, candidate.AccountID, true)
			if err != nil {
				return err
			}
			repo := repository.NewWealthCashRepo(tx)
			link, err := repo.Link(owner.ID, account.ID, true)
			if err != nil {
				return err
			}
			if link == nil {
				return ErrWealthMissing
			}
			rows, err := repo.UncategorizedYields(owner.ID, account.ID)
			if err != nil {
				return err
			}
			for _, row := range rows {
				var transaction model.Transaction
				if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&transaction, *row.TxnID).Error; err != nil {
					return err
				}
				kind := model.KindIncome
				if transaction.Kind == model.TxnExpense {
					kind = model.KindExpense
				}
				category, err := s.cashYieldCategory(tx, *account, kind, row.TxnID)
				if err != nil {
					return err
				}
				// MySQL has ON UPDATE CURRENT_TIMESTAMP on transactions.updated_at.
				if err := tx.Model(&model.Transaction{}).Where("id = ? AND category_id IS NULL", transaction.ID).UpdateColumns(map[string]any{"category_id": category.ID, "updated_at": transaction.UpdatedAt}).Error; err != nil {
					return err
				}
				before := map[string]any{"transaction_id": transaction.ID, "category_id": nil}
				after := map[string]any{"transaction_id": transaction.ID, "category_id": category.ID}
				if err := s.cashAudit(tx, *link, &row.ID, "category_assignment", "", "", before, after, WealthCashReceipt{AccountID: account.ID, LinkID: link.ID, YieldID: row.ID, Revision: row.Revision}); err != nil {
					return err
				}
				changed = true
			}
			return nil
		})
		if err != nil && first == nil {
			first = err
		} else if err == nil && changed {
			s.changed(candidate.OwnerID)
		}
	}
	return first
}

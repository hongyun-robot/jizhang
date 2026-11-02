package repository

import (
	"errors"

	"gorm.io/gorm"
	"jizhang/internal/model"
)

type CashYieldCategoryAccount struct {
	OwnerID   int64
	AccountID int64
}

func (r *WealthCashRepo) uncategorizedYields() *gorm.DB {
	return r.db.Model(&model.WealthCashYield{}).
		Joins("JOIN transactions t ON t.id = wealth_cash_yields.txn_id AND t.wealth_cash_yield_id = wealth_cash_yields.id AND t.account_id = wealth_cash_yields.account_id AND t.created_by = wealth_cash_yields.owner_id").
		Where("t.purpose = ? AND t.source = ? AND t.category_id IS NULL AND t.kind IN ?", model.PurposeWealthCashYield, model.SourceWealthAuto, []model.TxnKind{model.TxnIncome, model.TxnExpense})
}

func (r *WealthCashRepo) UncategorizedYieldAccounts() ([]CashYieldCategoryAccount, error) {
	rows := []CashYieldCategoryAccount{}
	if !r.Available() {
		return rows, nil
	}
	err := r.uncategorizedYields().Distinct("wealth_cash_yields.owner_id", "wealth_cash_yields.account_id").
		Order("wealth_cash_yields.owner_id, wealth_cash_yields.account_id").Scan(&rows).Error
	return rows, err
}

func (r *WealthCashRepo) UncategorizedYields(owner, account int64) ([]model.WealthCashYield, error) {
	rows := []model.WealthCashYield{}
	err := r.uncategorizedYields().Select("wealth_cash_yields.*").
		Where("wealth_cash_yields.owner_id = ? AND wealth_cash_yields.account_id = ?", owner, account).
		Order("wealth_cash_yields.nav_date, wealth_cash_yields.id").Find(&rows).Error
	return rows, err
}

// Reuse a valid category already assigned to this owner's wallet. This also
// preserves category IDs after the owner renames the account or category.
func (r *WealthCashRepo) YieldCategory(owner, account int64, kind model.CategoryKind, preferred *int64) (*model.Category, error) {
	query := func() *gorm.DB {
		return r.db.Model(&model.Category{}).Select("categories.*").
			Joins("JOIN transactions t ON t.category_id = categories.id").
			Where("t.account_id = ? AND t.created_by = ? AND t.purpose = ? AND categories.owner_id = ? AND categories.kind = ?", account, owner, model.PurposeWealthCashYield, owner, kind).
			Where("NOT EXISTS (SELECT 1 FROM categories child WHERE child.parent_id = categories.id)").
			Where("(categories.parent_id IS NULL OR EXISTS (SELECT 1 FROM categories parent WHERE parent.id = categories.parent_id AND parent.owner_id = ? AND parent.kind = ?))", owner, kind)
	}
	var category model.Category
	if preferred != nil {
		err := query().Where("t.id = ?", *preferred).Take(&category).Error
		if err == nil {
			return &category, nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
	}
	err := query().Order("t.entry_date DESC, t.id DESC").Take(&category).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &category, err
}

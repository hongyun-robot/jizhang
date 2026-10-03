package repository

import (
	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"jizhang/internal/model"
)

// BudgetRepo budgets 表数据访问。
type BudgetRepo struct {
	db *gorm.DB
}

func NewBudgetRepo(db *gorm.DB) *BudgetRepo { return &BudgetRepo{db: db} }

// List 某账期全部预算（总预算 category_id 为 NULL）。
func (r *BudgetRepo) List(periodKey string) ([]model.Budget, error) {
	var list []model.Budget
	err := r.db.Where("period_key = ?", periodKey).Order("id").Find(&list).Error
	return list, err
}

// Replace 整期重建配置（配置页整单提交）：事务内删旧插新。
func (r *BudgetRepo) Replace(periodKey string, bs []model.Budget) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("period_key = ?", periodKey).Delete(&model.Budget{}).Error; err != nil {
			return err
		}
		for i := range bs {
			if err := tx.Create(&bs[i]).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// rootConsumeRow 一级分类消耗行。
type rootConsumeRow struct {
	RootID int64
	Total  decimal.Decimal
}

// ConsumptionByRoot 某账期支出按一级分类上卷汇总（子分类并入父级）。
// 只计 kind='expense'：收入与转账天然排除（specs/budgets「预算消耗计算」）。
func (r *BudgetRepo) ConsumptionByRoot(periodKey string) (map[int64]decimal.Decimal, error) {
	const sql = `
		SELECT COALESCE(p.id, c.id) AS root_id, SUM(t.amount) AS total
		FROM transactions t
		LEFT JOIN categories c ON c.id = t.category_id
		LEFT JOIN categories p ON p.id = c.parent_id
		WHERE t.kind = 'expense' AND t.period_key = ?
		  AND COALESCE(p.id, c.id) IS NOT NULL
		GROUP BY COALESCE(p.id, c.id)`
	var rows []rootConsumeRow
	if err := r.db.Raw(sql, periodKey).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[int64]decimal.Decimal, len(rows))
	for _, row := range rows {
		out[row.RootID] = row.Total
	}
	return out, nil
}

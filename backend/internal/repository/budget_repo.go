package repository

import (
	"strings"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"jizhang/internal/model"
)

// BudgetRepo budgets 表数据访问。
type BudgetRepo struct {
	db      *gorm.DB
	ownerID int64
}

func NewBudgetRepo(db *gorm.DB) *BudgetRepo         { return &BudgetRepo{db: db} }
func (r *BudgetRepo) ForOwner(id int64) *BudgetRepo { return &BudgetRepo{db: r.db, ownerID: id} }
func (r *BudgetRepo) scoped(q *gorm.DB) *gorm.DB {
	return q.Where("owner_id = ?", r.ownerID)
}

// Owners 后台评估只遍历实际配置过预算的账号。
func (r *BudgetRepo) Owners(periodKey string) ([]int64, error) {
	var owners []int64
	err := r.db.Model(&model.Budget{}).Where("period_key = ?", periodKey).Distinct("owner_id").Pluck("owner_id", &owners).Error
	return owners, err
}

// List 某账期全部预算（总预算 category_id 为 NULL）。
func (r *BudgetRepo) List(periodKey string) ([]model.Budget, error) {
	var list []model.Budget
	err := r.scoped(r.db.Where("period_key = ?", periodKey)).Order("id").Find(&list).Error
	return list, err
}

// Replace 整期重建配置（配置页整单提交）：事务内删旧插新。
func (r *BudgetRepo) Replace(periodKey string, bs []model.Budget) error {
	if r.ownerID <= 0 {
		return gorm.ErrInvalidData
	}
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := r.scoped(tx.Where("period_key = ?", periodKey)).Delete(&model.Budget{}).Error; err != nil {
			return err
		}
		for i := range bs {
			bs[i].OwnerID = r.ownerID
			bs[i].PeriodKey = periodKey
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
// 消耗 = 同账期支出 − 同账期退款（退款按到账日落账期、按归属分类回吐，specs/budgets
// 「预算消耗计算」）；收入与转账天然排除，净额可为负（前端按「本月已退回」展示）。
// 无分类退款（root 为 NULL）不冲减任何分类预算。
func (r *BudgetRepo) ConsumptionByRoot(periodKey string) (map[int64]decimal.Decimal, error) {
	sql := `
		SELECT COALESCE(p.id, c.id) AS root_id,
		       SUM(` + expenseEffectSQL("t") + `) AS total
		FROM transactions t
		LEFT JOIN categories c ON c.id = t.category_id
		LEFT JOIN categories p ON p.id = c.parent_id
		WHERE t.kind IN ('expense', 'refund') AND t.period_key = ?
		  AND COALESCE(p.id, c.id) IS NOT NULL
		GROUP BY COALESCE(p.id, c.id)`
	var rows []rootConsumeRow
	args := []any{periodKey}
	if r.ownerID > 0 {
		sql = strings.Replace(sql, "GROUP BY", "AND t.account_id IN (SELECT id FROM accounts WHERE owner_id = ?) GROUP BY", 1)
		args = append(args, r.ownerID)
	}
	if err := r.db.Raw(sql, args...).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[int64]decimal.Decimal, len(rows))
	for _, row := range rows {
		out[row.RootID] = row.Total
	}
	return out, nil
}

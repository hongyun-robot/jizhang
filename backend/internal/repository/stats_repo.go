package repository

import (
	"fmt"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"jizhang/internal/model"
)

// StatsRepo 统计查询专用数据访问（只读）。
type StatsRepo struct {
	db *gorm.DB
}

func NewStatsRepo(db *gorm.DB) *StatsRepo { return &StatsRepo{db: db} }

// DailyRow 逐日收支聚合（不含转账）。
type DailyRow struct {
	D       time.Time
	Income  decimal.Decimal
	Expense decimal.Decimal
}

// DailyTotals 区间内逐日收入/支出（转账不计入，支出为退款冲减后净额，对照 statistics spec 口径）。
func (r *StatsRepo) DailyTotals(start, end time.Time) ([]DailyRow, error) {
	sql := `
		SELECT entry_date AS d,
		       SUM(CASE WHEN kind = 'income'  THEN amount ELSE 0 END) AS income,
		       SUM(` + expenseEffectSQL("") + `) AS expense
		FROM transactions
		WHERE kind IN ('income', 'expense', 'refund')
		  AND entry_date >= ? AND entry_date <= ?
		GROUP BY entry_date
		ORDER BY entry_date`
	var rows []DailyRow
	err := r.db.Raw(sql, start, end).Scan(&rows).Error
	return rows, err
}

// CategoryRow 一级分类聚合行（root_id 为 0 表示未分类）。
type CategoryRow struct {
	RootID int64
	Kind   model.TxnKind
	Total  decimal.Decimal
}

// CategoryTotals 区间内按一级分类聚合收入/支出（子分类上卷到一级）。
// 退款并入 expense 口径按负值累计：净额可为负且 MUST 原样返回（specs/statistics 分类占比，
// 口径过滤由图表层执行）；无分类退款归入 root_id=0（未分类）。
func (r *StatsRepo) CategoryTotals(start, end time.Time) ([]CategoryRow, error) {
	sql := `
		SELECT COALESCE(p.id, c.id) AS root_id,
		       CASE WHEN t.kind = 'refund' THEN 'expense' ELSE t.kind END AS kind,
		       SUM(CASE WHEN t.kind = 'income' THEN t.amount ELSE ` + expenseEffectSQL("t") + ` END) AS total
		FROM transactions t
		LEFT JOIN categories c ON c.id = t.category_id
		LEFT JOIN categories p ON p.id = c.parent_id
		WHERE t.kind IN ('income', 'expense', 'refund')
		  AND t.entry_date >= ? AND t.entry_date <= ?
		GROUP BY COALESCE(p.id, c.id), CASE WHEN t.kind = 'refund' THEN 'expense' ELSE t.kind END`
	var rows []CategoryRow
	err := r.db.Raw(sql, start, end).Scan(&rows).Error
	return rows, err
}

// PeriodRow 账期聚合行。
type PeriodRow struct {
	PeriodKey string
	Income    decimal.Decimal
	Expense   decimal.Decimal
}

// PeriodTotals 指定年份各账期的收入/支出（period_key 前缀走索引，支出含退款冲减）。
func (r *StatsRepo) PeriodTotals(year int) ([]PeriodRow, error) {
	sql := `
		SELECT period_key,
		       SUM(CASE WHEN kind = 'income'  THEN amount ELSE 0 END) AS income,
		       SUM(` + expenseEffectSQL("") + `) AS expense
		FROM transactions
		WHERE kind IN ('income', 'expense', 'refund') AND period_key LIKE ?
		GROUP BY period_key`
	var rows []PeriodRow
	err := r.db.Raw(sql, fmt.Sprintf("%04d-%%", year)).Scan(&rows).Error
	return rows, err
}

// SumByPeriod 按物化 period_key 汇总收入/支出（转账不计，支出含退款冲减）。
// 封账预览用：与 guardOpen 的锁口径（同 period_key）严格一致，改结算日不影响历史合计。
func (r *StatsRepo) SumByPeriod(periodKey string) (PeriodRow, error) {
	sql := `
		SELECT COALESCE(SUM(CASE WHEN kind = 'income'  THEN amount END), 0) AS income,
		       COALESCE(SUM(` + expenseEffectSQL("") + `), 0) AS expense
		FROM transactions
		WHERE kind IN ('income', 'expense', 'refund') AND period_key = ?`
	var row PeriodRow
	err := r.db.Raw(sql, periodKey).Scan(&row).Error
	row.PeriodKey = periodKey
	return row, err
}

// AssetNetRow 某账户某日的净额（转账双边各一行）。
type AssetNetRow struct {
	Aid int64
	D   time.Time
	Net decimal.Decimal
}

// DailyAccountNets 截至 end 的逐账户逐日净额（含转账双边），按日排序。
func (r *StatsRepo) DailyAccountNets(end time.Time) ([]AssetNetRow, error) {
	sql := `
		SELECT aid, d, SUM(v) AS net FROM (
			SELECT account_id AS aid, entry_date AS d,
			       ` + balanceEffectSQL("") + ` AS v
			FROM transactions
			WHERE kind <> 'transfer' AND entry_date <= ?
			UNION ALL
			SELECT account_id AS aid, entry_date AS d, -amount AS v
			FROM transactions
			WHERE kind = 'transfer' AND entry_date <= ?
			UNION ALL
			SELECT transfer_account_id AS aid, entry_date AS d, amount AS v
			FROM transactions
			WHERE kind = 'transfer' AND transfer_account_id IS NOT NULL AND entry_date <= ?
		) x
		WHERE aid IS NOT NULL
		GROUP BY aid, d
		ORDER BY d`
	var rows []AssetNetRow
	err := r.db.Raw(sql, end, end, end).Scan(&rows).Error
	return rows, err
}

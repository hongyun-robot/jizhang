package repository

import (
	"errors"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"jizhang/internal/model"
)

// TransactionRepo transactions 表数据访问。
type TransactionRepo struct {
	db *gorm.DB
}

func NewTransactionRepo(db *gorm.DB) *TransactionRepo { return &TransactionRepo{db: db} }

func (r *TransactionRepo) DB() *gorm.DB { return r.db }

// TxnFilter 列表过滤条件（specs/transactions 查询场景）。
type TxnFilter struct {
	PeriodKey    string        // 精确归期
	StartDate    *string       // YYYY-MM-DD 起（含）
	EndDate      *string       // YYYY-MM-DD 止（含）
	Kind         model.TxnKind // 限定类型
	AccountID    int64
	CategoryID   int64
	AmountMin    *string // 金额区间下界（含）
	AmountMax    *string // 金额区间上界（含）
	Keyword      string  // 备注或平台 / 商家模糊
	RelatedTxnID int64   // 定位关联到该流水的退款（>0 时过滤）
	Page         int     // 从 1 开始
	PageSize     int
}

// applyFilter 组装筛选条件（List / ListAll 共用，导出与列表同一套筛选语义）。
func applyFilter(q *gorm.DB, f TxnFilter) *gorm.DB {
	if f.PeriodKey != "" {
		q = q.Where("period_key = ?", f.PeriodKey)
	}
	if f.StartDate != nil {
		q = q.Where("entry_date >= ?", *f.StartDate)
	}
	if f.EndDate != nil {
		q = q.Where("entry_date <= ?", *f.EndDate)
	}
	if f.Kind != "" {
		q = q.Where("kind = ?", f.Kind)
	}
	if f.AccountID > 0 {
		q = q.Where("account_id = ? OR transfer_account_id = ?", f.AccountID, f.AccountID)
	}
	if f.CategoryID > 0 {
		// 分类筛选含子分类：选一级时返回其下全部子分类的流水（specs/transactions 组合搜索）
		q = q.Where("category_id IN (SELECT id FROM categories WHERE id = ? OR parent_id = ?)",
			f.CategoryID, f.CategoryID)
	}
	if f.AmountMin != nil && *f.AmountMin != "" {
		q = q.Where("amount >= ?", *f.AmountMin)
	}
	if f.AmountMax != nil && *f.AmountMax != "" {
		q = q.Where("amount <= ?", *f.AmountMax)
	}
	if f.Keyword != "" {
		like := "%" + f.Keyword + "%"
		q = q.Where("(note LIKE ? OR platform LIKE ?)", like, like)
	}
	if f.RelatedTxnID > 0 {
		// 关联退款定位（specs/refunds 原支出「已退」跳转）
		q = q.Where("related_txn_id = ?", f.RelatedTxnID)
	}
	return q
}

// List 按条件分页查询，返回总数与当前页（按发生日期倒序）。
func (r *TransactionRepo) List(f TxnFilter) ([]model.Transaction, int64, error) {
	q := applyFilter(r.db.Model(&model.Transaction{}), f)

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	page := f.Page
	if page < 1 {
		page = 1
	}
	size := f.PageSize
	if size < 1 || size > 100 {
		size = 20
	}

	var list []model.Transaction
	err := q.Order("entry_date DESC, id DESC").
		Offset((page - 1) * size).
		Limit(size).
		Find(&list).Error
	return list, total, err
}

// ListAll 同筛选条件不分页，返回全部命中（CSV 导出用，排序与 List 一致）。
func (r *TransactionRepo) ListAll(f TxnFilter) ([]model.Transaction, error) {
	q := applyFilter(r.db.Model(&model.Transaction{}), f)
	var list []model.Transaction
	err := q.Order("entry_date DESC, id DESC").Find(&list).Error
	return list, err
}

// GetByID 按主键查询。
func (r *TransactionRepo) GetByID(id int64) (*model.Transaction, error) {
	var t model.Transaction
	err := r.db.First(&t, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &t, err
}

// Create 新建流水（period_key 由 service 写入前物化）。
func (r *TransactionRepo) Create(t *model.Transaction) error { return r.db.Create(t).Error }

// Update 保存修改。
func (r *TransactionRepo) Update(t *model.Transaction) error { return r.db.Save(t).Error }

// Delete 硬删除流水。
func (r *TransactionRepo) Delete(id int64) error {
	return r.db.Delete(&model.Transaction{}, id).Error
}

// ExistsByRuleDate 周期幂等检查：同一模板同一归属日唯一（uk_txn_rule_date）。
// 半开区间 [date, date+1) 同时兼容 MySQL DATE 与 sqlite 文本存储的比较语义。
func (r *TransactionRepo) ExistsByRuleDate(ruleID int64, date time.Time) (bool, error) {
	var n int64
	start := date.Format("2006-01-02")
	end := date.AddDate(0, 0, 1).Format("2006-01-02")
	err := r.db.Model(&model.Transaction{}).
		Where("rule_id = ? AND entry_date >= ? AND entry_date < ?", ruleID, start, end).
		Count(&n).Error
	return n > 0, err
}

// ListByPeriod 取某归期全部流水（封账/统计用）。
func (r *TransactionRepo) ListByPeriod(periodKey string) ([]model.Transaction, error) {
	var list []model.Transaction
	err := r.db.Where("period_key = ?", periodKey).Order("entry_date, id").Find(&list).Error
	return list, err
}

// HasRefunds 该流水是否已被退款关联（原支出删除约束）。
func (r *TransactionRepo) HasRefunds(txnID int64) (bool, error) {
	var n int64
	err := r.db.Model(&model.Transaction{}).
		Where("kind = ? AND related_txn_id = ?", model.TxnRefund, txnID).
		Count(&n).Error
	return n > 0, err
}

// RefundedTotals 按原支出 id 聚合关联退款累计金额（列表「已退 ¥x」标记）。
func (r *TransactionRepo) RefundedTotals(ids []int64) (map[int64]decimal.Decimal, error) {
	out := make(map[int64]decimal.Decimal, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	type row struct {
		RelatedTxnID int64
		Total        decimal.Decimal
	}
	var rows []row
	err := r.db.Model(&model.Transaction{}).
		Select("related_txn_id, SUM(amount) AS total").
		Where("kind = ? AND related_txn_id IN ?", model.TxnRefund, ids).
		Group("related_txn_id").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, x := range rows {
		out[x.RelatedTxnID] = x.Total
	}
	return out, nil
}

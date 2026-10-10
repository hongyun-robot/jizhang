package repository

import (
	"errors"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"jizhang/internal/model"
)

// AccountRepo accounts 表数据访问。
type AccountRepo struct {
	db *gorm.DB
}

func NewAccountRepo(db *gorm.DB) *AccountRepo { return &AccountRepo{db: db} }

func (r *AccountRepo) DB() *gorm.DB { return r.db }

// netRow 中间聚合结果。
type netRow struct {
	Aid int64
	Net decimal.Decimal
}

// List 返回全部账户并附余额：opening + 收入 + 退款 − 支出 − 转出 + 转入。
func (r *AccountRepo) List() ([]model.Account, error) {
	var accounts []model.Account
	if err := r.db.Order("sort, id").Find(&accounts).Error; err != nil {
		return nil, err
	}

	// 流水净额：资产/负债共用同一口径（信用卡支出=余额变负，还款=转入回正，退款=资金回流）
	netSQL := `
		SELECT aid, SUM(v) AS net FROM (
			SELECT account_id AS aid,
			       ` + balanceEffectSQL("") + ` AS v
			FROM transactions
			UNION ALL
			SELECT transfer_account_id AS aid, amount AS v
			FROM transactions
			WHERE kind = 'transfer' AND transfer_account_id IS NOT NULL
		) x
		WHERE aid IS NOT NULL
		GROUP BY aid`
	var rows []netRow
	if err := r.db.Raw(netSQL).Scan(&rows).Error; err != nil {
		return nil, err
	}
	netByID := make(map[int64]decimal.Decimal, len(rows))
	for _, row := range rows {
		netByID[row.Aid] = row.Net
	}
	for i := range accounts {
		accounts[i].Balance = accounts[i].OpeningBalance.Add(netByID[accounts[i].ID]).Round(2)
	}
	historySQL := `SELECT DISTINCT aid FROM (SELECT account_id AS aid FROM transactions UNION ALL SELECT transfer_account_id AS aid FROM transactions WHERE transfer_account_id IS NOT NULL`
	if r.db.Migrator().HasTable(&model.LiabilityStatement{}) {
		historySQL += ` UNION ALL SELECT account_id AS aid FROM liability_statements UNION ALL SELECT account_id AS aid FROM liability_operations WHERE kind <> 'settings'`
	}
	historySQL += `) h`
	var history []struct{ Aid int64 }
	if e := r.db.Raw(historySQL).Scan(&history).Error; e != nil {
		return nil, e
	}
	seen := map[int64]bool{}
	for _, x := range history {
		seen[x.Aid] = true
	}
	for i := range accounts {
		accounts[i].HasFinancialHistory = seen[accounts[i].ID]
	}
	return accounts, nil
}

// GetByID 按 ID 查询（不含余额字段）。
func (r *AccountRepo) GetByID(id int64) (*model.Account, error) {
	var a model.Account
	err := r.db.First(&a, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &a, err
}

// Create 新建账户。
func (r *AccountRepo) Create(a *model.Account) error { return r.db.Create(a).Error }

// Update 保存修改。
func (r *AccountRepo) Update(a *model.Account) error { return r.db.Save(a).Error }

// HasTransactions 该账户是否仍被流水引用（有流水不可删）。
func (r *AccountRepo) HasTransactions(id int64) (bool, error) {
	var n int64
	err := r.db.Table("transactions").
		Where("account_id = ? OR transfer_account_id = ?", id, id).Count(&n).Error
	return n > 0, err
}

// Delete 删除账户（无流水时）。
func (r *AccountRepo) Delete(id int64) error {
	return r.db.Delete(&model.Account{}, id).Error
}

// Count 账户总数（种子判断）。
func (r *AccountRepo) Count() (int64, error) {
	var n int64
	err := r.db.Model(&model.Account{}).Count(&n).Error
	return n, err
}

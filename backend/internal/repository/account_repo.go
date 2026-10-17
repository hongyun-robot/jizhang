package repository

import (
	"errors"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"jizhang/internal/model"
)

// AccountRepo accounts 表数据访问。
type AccountRepo struct {
	db           *gorm.DB
	ownerID      int64
	accountType  model.AccountType
	FundValues   func(*gorm.DB, []int64, []time.Time) (map[string]map[int64]FundAccountValue, error)
	GoldValues   func(*gorm.DB, []int64, []time.Time) (map[string]map[int64]FundAccountValue, error)
	WealthValues func(*gorm.DB, []int64, []time.Time) (map[string]map[int64]FundAccountValue, error)
	Now          func() time.Time
}

type FundAccountValue struct {
	Value         decimal.Decimal
	Complete      bool
	Status        string
	NAVDate       string
	ObservationID *int64
}

func NewAccountRepo(db *gorm.DB) *AccountRepo { return &AccountRepo{db: db} }

func (r *AccountRepo) DB() *gorm.DB { return r.db }
func (r *AccountRepo) ForOwner(id int64) *AccountRepo {
	return &AccountRepo{db: r.db, ownerID: id, accountType: r.accountType, FundValues: r.FundValues, GoldValues: r.GoldValues, WealthValues: r.WealthValues, Now: r.Now}
}

// ForType lets liability operations load their balances without valuing
// unrelated investment accounts inside a financial transaction.
func (r *AccountRepo) ForType(kind model.AccountType) *AccountRepo {
	return &AccountRepo{db: r.db, ownerID: r.ownerID, accountType: kind, FundValues: r.FundValues, GoldValues: r.GoldValues, WealthValues: r.WealthValues, Now: r.Now}
}

// netRow 中间聚合结果。
type netRow struct {
	Aid int64
	Net decimal.Decimal
}

// List 返回全部账户并附余额：opening + 收入 + 退款 − 支出 − 转出 + 转入。
func (r *AccountRepo) List() ([]model.Account, error) {
	var rows []model.Account
	err := r.db.Transaction(func(tx *gorm.DB) error {
		bound := &AccountRepo{db: tx, ownerID: r.ownerID, accountType: r.accountType, FundValues: r.FundValues, GoldValues: r.GoldValues, WealthValues: r.WealthValues, Now: r.Now}
		var e error
		rows, e = bound.list()
		return e
	})
	return rows, err
}

// Metadata loads ownership and display fields without computing balances.
func (r *AccountRepo) Metadata() ([]model.Account, error) {
	var accounts []model.Account
	q := r.db
	if r.ownerID > 0 {
		q = q.Where("owner_id = ?", r.ownerID)
	}
	if r.accountType != "" {
		q = q.Where("type = ?", r.accountType)
	}
	if err := q.Order("sort, id").Find(&accounts).Error; err != nil {
		return nil, err
	}
	return accounts, nil
}

func (r *AccountRepo) list() ([]model.Account, error) {
	accounts, err := r.Metadata()
	if err != nil {
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
	if r.db.Migrator().HasTable(&model.FundPosition{}) {
		historySQL = `SELECT DISTINCT aid FROM (` + historySQL + ` UNION ALL SELECT account_id AS aid FROM fund_positions) fh`
	}
	if r.db.Migrator().HasTable(&model.WealthPosition{}) {
		historySQL = `SELECT DISTINCT aid FROM (` + historySQL + ` UNION ALL SELECT account_id AS aid FROM wealth_positions) wh`
	}
	if r.db.Migrator().HasTable(&model.WealthCashLink{}) {
		historySQL = `SELECT DISTINCT aid FROM (` + historySQL + ` UNION ALL SELECT account_id AS aid FROM wealth_cash_links) wc`
	}
	var history []struct{ Aid int64 }
	if r.db.Migrator().HasTable(&model.GoldPosition{}) {
		historySQL = `SELECT DISTINCT aid FROM (` + historySQL + ` UNION ALL SELECT account_id AS aid FROM gold_positions) gh`
	}
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
	fundIDs := []int64{}
	for _, a := range accounts {
		if a.IsFund() {
			fundIDs = append(fundIDs, a.ID)
		}
	}
	if len(fundIDs) > 0 {
		if r.FundValues == nil {
			return nil, errors.New("基金估值服务未初始化")
		}
		now := time.Now()
		if r.Now != nil {
			now = r.Now()
		}
		values, e := r.FundValues(r.db, fundIDs, []time.Time{now})
		if e != nil {
			return nil, e
		}
		key := now.Format("2006-01-02")
		for i := range accounts {
			if accounts[i].IsFund() {
				v := values[key][accounts[i].ID]
				accounts[i].Balance = v.Value.Round(2)
				accounts[i].ValuationStatus = v.Status
				accounts[i].NAVDate = v.NAVDate
			}
		}
	}
	goldIDs := []int64{}
	for _, a := range accounts {
		if a.IsGold() {
			goldIDs = append(goldIDs, a.ID)
		}
	}
	if len(goldIDs) > 0 {
		if r.GoldValues == nil {
			return nil, errors.New("黄金估值服务未初始化")
		}
		now := time.Now()
		if r.Now != nil {
			now = r.Now()
		}
		values, e := r.GoldValues(r.db, goldIDs, []time.Time{now})
		if e != nil {
			return nil, e
		}
		for i := range accounts {
			if accounts[i].IsGold() {
				v := values[now.Format("2006-01-02")][accounts[i].ID]
				accounts[i].Balance = v.Value.Round(2)
				accounts[i].ValuationStatus = v.Status
				accounts[i].NAVDate = v.NAVDate
			}
		}
	}
	wealthIDs := []int64{}
	for _, a := range accounts {
		if a.IsWealth() {
			wealthIDs = append(wealthIDs, a.ID)
		}
	}
	if len(wealthIDs) > 0 {
		if r.WealthValues == nil {
			return nil, errors.New("理财估值服务未初始化")
		}
		now := time.Now()
		if r.Now != nil {
			now = r.Now()
		}
		values, e := r.WealthValues(r.db, wealthIDs, []time.Time{now})
		if e != nil {
			return nil, e
		}
		for i := range accounts {
			if accounts[i].IsWealth() {
				v := values[now.Format("2006-01-02")][accounts[i].ID]
				accounts[i].Balance = v.Value.Round(2)
				if !v.Complete {
					known := v.Value.Round(2)
					accounts[i].KnownBalance = &known
				}
				accounts[i].ValuationStatus = v.Status
				accounts[i].NAVDate = v.NAVDate
			}
		}
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
	if r.db.Migrator().HasTable(&model.WealthCashLink{}) {
		var count int64
		if err := r.db.Model(&model.WealthCashLink{}).Where("account_id = ?", id).Count(&count).Error; err != nil { return false, err }
		if count > 0 { return true, nil }
	}
	if r.db.Migrator().HasTable(&model.WealthPosition{}) {
		var n int64
		if err := r.db.Model(&model.WealthPosition{}).Where("account_id = ?", id).Count(&n).Error; err != nil {
			return false, err
		}
		if n > 0 {
			return true, nil
		}
	}

	if r.db.Migrator().HasTable(&model.GoldPosition{}) {
		var n int64
		if e := r.db.Model(&model.GoldPosition{}).Where("account_id = ?", id).Count(&n).Error; e != nil {
			return false, e
		}
		if n > 0 {
			return true, nil
		}
	}
	var n int64
	err := r.db.Table("transactions").
		Where("account_id = ? OR transfer_account_id = ?", id, id).Count(&n).Error
	if err == nil && n == 0 && r.db.Migrator().HasTable(&model.FundPosition{}) {
		err = r.db.Model(&model.FundPosition{}).Where("account_id = ?", id).Count(&n).Error
	}
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

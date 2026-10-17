package repository

import (
	"errors"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"jizhang/internal/model"
)

type WealthCashRepo struct{ db *gorm.DB }

func NewWealthCashRepo(db *gorm.DB) *WealthCashRepo { return &WealthCashRepo{db: db} }
func (r *WealthCashRepo) Available() bool           { return r.db.Migrator().HasTable(&model.WealthCashLink{}) }
func (r *WealthCashRepo) Link(owner, account int64, lock bool) (*model.WealthCashLink, error) {
	var row model.WealthCashLink
	query := r.db.Where("owner_id = ? AND account_id = ?", owner, account)
	if lock {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	err := query.First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &row, err
}
func (r *WealthCashRepo) Links(owner int64, active bool) ([]model.WealthCashLink, error) {
	rows := []model.WealthCashLink{}
	if !r.Available() {
		return rows, nil
	}
	query := r.db.Model(&model.WealthCashLink{})
	if owner > 0 {
		query = query.Where("owner_id = ?", owner)
	}
	if active {
		query = query.Where("active = ?", true)
	}
	err := query.Order("id").Find(&rows).Error
	return rows, err
}
func (r *WealthCashRepo) Yields(owner, account int64) ([]model.WealthCashYield, error) {
	rows := []model.WealthCashYield{}
	err := r.db.Where("owner_id = ? AND account_id = ?", owner, account).Order("nav_date,id").Find(&rows).Error
	return rows, err
}
func (r *WealthCashRepo) AuditForRequest(owner int64, key string) (*model.WealthCashAudit, error) {
	var row model.WealthCashAudit
	err := r.db.Where("owner_id = ? AND request_key = ?", owner, key).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &row, err
}
func (r *WealthCashRepo) Audits(owner, link int64) ([]model.WealthCashAudit, error) {
	rows := []model.WealthCashAudit{}
	err := r.db.Where("owner_id = ? AND link_id = ?", owner, link).Order("id DESC").Find(&rows).Error
	return rows, err
}

type CashNet struct {
	Date time.Time
	Net  decimal.Decimal
}

type CashMovement struct {
	TxnID      int64
	Date       time.Time
	RecordedAt time.Time
	Kind       model.TxnKind
	Net        decimal.Decimal
	Metadata   model.WealthCashFlow `gorm:"-"`
}

func (r *WealthCashRepo) Movements(account int64, end time.Time) ([]CashMovement, error) {
	rows := []CashMovement{}
	query := `SELECT id AS txn_id,entry_date AS date,created_at AS recorded_at,kind,` + balanceEffectSQL("") + ` AS net
 FROM transactions WHERE account_id = ? AND entry_date <= ? AND purpose<>'wealth_cash_yield'
 UNION ALL SELECT id AS txn_id,entry_date AS date,created_at AS recorded_at,kind,amount AS net
 FROM transactions WHERE transfer_account_id = ? AND entry_date <= ? AND kind='transfer' AND purpose<>'wealth_cash_yield'
 ORDER BY date,recorded_at,txn_id`
	if err := r.db.Raw(query, account, end, account, end).Scan(&rows).Error; err != nil {
		return nil, err
	}
	metadata := []model.WealthCashFlow{}
	if err := r.db.Where("account_id = ?", account).Find(&metadata).Error; err != nil {
		return nil, err
	}
	byID := map[int64]model.WealthCashFlow{}
	for _, row := range metadata {
		byID[row.TxnID] = row
	}
	for i := range rows {
		rows[i].Metadata = byID[rows[i].TxnID]
	}
	return rows, nil
}

// CashNets mirrors ordinary account effects. Excluding this module's own
// interest avoids treating compounding interest as a second external deposit.
func (r *WealthCashRepo) CashNets(account int64, end time.Time, includeYield bool) ([]CashNet, error) {
	filter := ""
	if !includeYield {
		filter = " AND purpose <> 'wealth_cash_yield'"
	}
	query := `SELECT d AS date,SUM(v) AS net FROM (
 SELECT entry_date AS d,` + balanceEffectSQL("") + ` AS v FROM transactions WHERE account_id = ? AND entry_date <= ?` + filter + `
 UNION ALL SELECT entry_date AS d,amount AS v FROM transactions WHERE kind='transfer' AND transfer_account_id = ? AND entry_date <= ?` + filter + `
 ) cash_rows GROUP BY d ORDER BY d`
	rows := []CashNet{}
	err := r.db.Raw(query, account, end, account, end).Scan(&rows).Error
	return rows, err
}
func (r *WealthCashRepo) Balance(account model.Account, date time.Time) (decimal.Decimal, error) {
	rows, err := r.CashNets(account.ID, date, true)
	value := account.OpeningBalance
	for _, row := range rows {
		value = value.Add(row.Net)
	}
	return value.Round(2), err
}

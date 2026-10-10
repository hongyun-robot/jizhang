package repository

import (
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"jizhang/internal/model"
)

const ledgerGuardKey = "ledger_write_guard"

// LedgerWrite 所有财务写入和封账先锁同一行，校验与提交都在锁内。
func LedgerWrite(db *gorm.DB, fn func(*gorm.DB) error) error {
	return db.Transaction(func(tx *gorm.DB) error {
		seed := model.Setting{Key: ledgerGuardKey, Value: "1"}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&seed).Error; err != nil {
			return err
		}
		var guard model.Setting
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&guard, "`key` = ?", ledgerGuardKey).Error; err != nil {
			return err
		}
		return fn(tx)
	})
}

package service

import (
	"gorm.io/gorm"

	"jizhang/internal/model"
)

func checkRepaymentAccount(tx *gorm.DB, actor *model.User, id int64) error {
	if err := checkAccountOwnership(tx, actor, id); err != nil {
		return err
	}
	var account model.Account
	if err := tx.First(&account, id).Error; err != nil {
		return err
	}
	if account.Type != model.AccountAsset || account.Status != model.StatusActive {
		return ErrTxnAccountInvalid
	}
	// The separate fund-account change may introduce this column. Its market
	// value is not spendable cash; older schemas have only ordinary assets.
	if tx.Migrator().HasColumn(&model.Account{}, "asset_kind") {
		var kind struct{ AssetKind string }
		if err := tx.Model(&model.Account{}).Select("asset_kind").Where("id = ?", id).Scan(&kind).Error; err != nil {
			return err
		}
		if kind.AssetKind == "fund" {
			return ErrTxnAccountInvalid
		}
	}
	return nil
}

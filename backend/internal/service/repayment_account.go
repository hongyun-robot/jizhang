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
	if !account.IsCash() || account.Status != model.StatusActive {
		return ErrTxnAccountInvalid
	}
	return nil
}

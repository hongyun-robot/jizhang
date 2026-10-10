package liability

import (
	"errors"
	"gorm.io/gorm"
	"jizhang/internal/model"
)

func AccountTypes(db *gorm.DB) ([]model.LiabilityAccountType, error) {
	rows := []model.LiabilityAccountType{}
	err := db.Order("sort, code").Find(&rows).Error
	return rows, err
}
func ValidateAccountType(db *gorm.DB, code string) error {
	var count int64
	if err := db.Model(&model.LiabilityAccountType{}).Where("code = ?", code).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return ErrInvalid
	}
	return nil
}
func DefaultAccountType(db *gorm.DB) (string, error) {
	var row model.LiabilityAccountType
	err := db.Order("sort, code").First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", ErrInvalid
	}
	return row.Code, err
}

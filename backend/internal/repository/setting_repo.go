package repository

import (
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"jizhang/internal/model"
)

// SettingRepo settings 表数据访问。
type SettingRepo struct {
	db *gorm.DB
}

func NewSettingRepo(db *gorm.DB) *SettingRepo { return &SettingRepo{db: db} }

// Get 读取配置；不存在返回空串。
func (r *SettingRepo) Get(key string) (string, error) {
	var s model.Setting
	err := r.db.First(&s, "`key` = ?", key).Error
	if err == gorm.ErrRecordNotFound {
		return "", nil
	}
	return s.Value, err
}

// Set 写入配置（upsert）。
func (r *SettingRepo) Set(key, value string) error {
	s := model.Setting{Key: key, Value: value}
	return r.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value"}),
	}).Create(&s).Error
}

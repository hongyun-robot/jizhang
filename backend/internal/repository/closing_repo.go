package repository

import (
	"errors"

	"gorm.io/gorm"

	"jizhang/internal/model"
)

// ClosingRepo period_closings 表数据访问。
type ClosingRepo struct {
	db *gorm.DB
}

func NewClosingRepo(db *gorm.DB) *ClosingRepo { return &ClosingRepo{db: db} }

func (r *ClosingRepo) DB() *gorm.DB { return r.db }

// IsClosed 某归期是否已封账。
func (r *ClosingRepo) IsClosed(periodKey string) (bool, error) {
	var n int64
	err := r.db.Model(&model.PeriodClosing{}).Where("period_key = ?", periodKey).Count(&n).Error
	return n > 0, err
}

// IsClosedAny 给定多个归期中任意一个被封（改动涉及跨期时用）。
func (r *ClosingRepo) IsClosedAny(keys ...string) (bool, error) {
	if len(keys) == 0 {
		return false, nil
	}
	var n int64
	err := r.db.Model(&model.PeriodClosing{}).Where("period_key IN ?", keys).Count(&n).Error
	return n > 0, err
}

// Close 记录封账。
func (r *ClosingRepo) Close(c *model.PeriodClosing) error { return r.db.Create(c).Error }

// Reopen 删除封账记录（管理员重开）。
func (r *ClosingRepo) Reopen(periodKey string) error {
	return r.db.Where("period_key = ?", periodKey).Delete(&model.PeriodClosing{}).Error
}

// Get 按归期取封账记录。
func (r *ClosingRepo) Get(periodKey string) (*model.PeriodClosing, error) {
	var c model.PeriodClosing
	err := r.db.Where("period_key = ?", periodKey).First(&c).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &c, err
}

// ListByPeriodPrefix 按前缀列记录（列出全部封账期）。
func (r *ClosingRepo) List() ([]model.PeriodClosing, error) {
	var list []model.PeriodClosing
	err := r.db.Order("period_key DESC").Find(&list).Error
	return list, err
}

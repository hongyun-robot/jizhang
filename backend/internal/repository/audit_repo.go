package repository

import (
	"gorm.io/gorm"

	"jizhang/internal/model"
)

// AuditRepo 审计日志数据访问。
type AuditRepo struct {
	db *gorm.DB
}

func NewAuditRepo(db *gorm.DB) *AuditRepo { return &AuditRepo{db: db} }

// Create 记录一条审计日志。
func (r *AuditRepo) Create(a *model.AuditLog) error { return r.db.Create(a).Error }

// List 按时间倒序分页（最新在前）。
func (r *AuditRepo) List(page, pageSize int) ([]model.AuditLog, int64, error) {
	var total int64
	if err := r.db.Model(&model.AuditLog{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []model.AuditLog
	err := r.db.Order("id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&list).Error
	return list, total, err
}

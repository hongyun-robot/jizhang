package repository

import (
	"gorm.io/gorm"
	"jizhang/internal/model"
)

// LiabilityRepo reads durable resource identities. Financial projection writes
// are coordinated by internal/liability within LedgerWrite.
type LiabilityRepo struct{ db *gorm.DB }

func NewLiabilityRepo(db *gorm.DB) *LiabilityRepo { return &LiabilityRepo{db: db} }
func (r *LiabilityRepo) Plan(id int64) (model.InstallmentPlan, error) {
	var p model.InstallmentPlan
	e := r.db.First(&p, id).Error
	return p, e
}
func (r *LiabilityRepo) Statements(account int64) ([]model.LiabilityStatement, error) {
	out := []model.LiabilityStatement{}
	e := r.db.Where("account_id = ?", account).Order("cycle_end DESC").Find(&out).Error
	return out, e
}
func (r *LiabilityRepo) Rules(account int64) ([]model.LiabilityBillRule, error) {
	out := []model.LiabilityBillRule{}
	e := r.db.Where("account_id = ?", account).Order("version").Find(&out).Error
	return out, e
}

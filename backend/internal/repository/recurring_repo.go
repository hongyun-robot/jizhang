package repository

import (
	"errors"
	"time"

	"gorm.io/gorm"

	"jizhang/internal/model"
)

// RecurringRepo recurring_rules 表数据访问。
type RecurringRepo struct {
	db *gorm.DB
}

func NewRecurringRepo(db *gorm.DB) *RecurringRepo { return &RecurringRepo{db: db} }

// List 全部模板（新的在前）。
func (r *RecurringRepo) List() ([]model.RecurringRule, error) {
	var rs []model.RecurringRule
	err := r.db.Order("id DESC").Find(&rs).Error
	return rs, err
}

// ListByCreator 单个创建者的模板（列表按属主隔离，specs/recurring-entries）。
func (r *RecurringRepo) ListByCreator(creatorID int64) ([]model.RecurringRule, error) {
	var rs []model.RecurringRule
	err := r.db.Where("created_by = ?", creatorID).Order("id DESC").Find(&rs).Error
	return rs, err
}

// ListDueBefore 截止某日（不含）之前到期的启用模板（生成扫描范围）。
// 用严格小于次日的半开区间，规避 DATE 与文本存储的字符串比较边界。
func (r *RecurringRepo) ListDueBefore(beforeExclusive string) ([]model.RecurringRule, error) {
	var rs []model.RecurringRule
	err := r.db.
		Where("status = ? AND next_run_date < ?", model.RuleEnabled, beforeExclusive).
		Order("next_run_date").Find(&rs).Error
	return rs, err
}

// GetByID 按主键查询（未找到返回 (nil, nil)）。
func (r *RecurringRepo) GetByID(id int64) (*model.RecurringRule, error) {
	var m model.RecurringRule
	err := r.db.First(&m, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// Create 新建模板（next_run_date = start_date，service 层规整）。
func (r *RecurringRepo) Create(m *model.RecurringRule) error { return r.db.Create(m).Error }

// Update 保存修改（整行）。
func (r *RecurringRepo) Update(m *model.RecurringRule) error { return r.db.Save(m).Error }

// Advance 只推进 next_run_date 一列：生成循环与管理端编辑并发时不互相覆盖整行。
func (r *RecurringRepo) Advance(id int64, nextRun time.Time) error {
	return r.db.Model(&model.RecurringRule{}).
		Where("id = ?", id).
		Update("next_run_date", nextRun).Error
}

// Delete 硬删除模板；已生成流水保留（rule_id 悬挂指针无害，source 仍标记为自动生成）。
func (r *RecurringRepo) Delete(id int64) error {
	return r.db.Delete(&model.RecurringRule{}, id).Error
}

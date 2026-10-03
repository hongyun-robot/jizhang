package repository

import (
	"errors"

	"gorm.io/gorm"

	"jizhang/internal/model"
)

// CategoryRepo categories 表数据访问。
type CategoryRepo struct {
	db *gorm.DB
}

func NewCategoryRepo(db *gorm.DB) *CategoryRepo { return &CategoryRepo{db: db} }

// List 全部分类（收支两套树由 service 组装）。
func (r *CategoryRepo) List() ([]model.Category, error) {
	var list []model.Category
	err := r.db.Order("kind, sort, id").Find(&list).Error
	return list, err
}

// GetByID 按 ID 查询。
func (r *CategoryRepo) GetByID(id int64) (*model.Category, error) {
	var c model.Category
	err := r.db.First(&c, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &c, err
}

// Create 新建分类。
func (r *CategoryRepo) Create(c *model.Category) error { return r.db.Create(c).Error }

// Update 保存修改。
func (r *CategoryRepo) Update(c *model.Category) error { return r.db.Save(c).Error }

// Delete 删除分类。
func (r *CategoryRepo) Delete(id int64) error { return r.db.Delete(&model.Category{}, id).Error }

// CountChildren 子分类数量（有子分类不可删）。
func (r *CategoryRepo) CountChildren(parentID int64) (int64, error) {
	var n int64
	err := r.db.Model(&model.Category{}).Where("parent_id = ?", parentID).Count(&n).Error
	return n, err
}

// CountTransactions 被流水引用数量（被引用不可删）。
func (r *CategoryRepo) CountTransactions(id int64) (int64, error) {
	var n int64
	err := r.db.Table("transactions").Where("category_id = ?", id).Count(&n).Error
	return n, err
}

// Count 分类总数（种子判断）。
func (r *CategoryRepo) Count() (int64, error) {
	var n int64
	err := r.db.Model(&model.Category{}).Count(&n).Error
	return n, err
}

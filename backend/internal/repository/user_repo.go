package repository

import (
	"errors"

	"gorm.io/gorm"

	"jizhang/internal/model"
)

// UserRepo users 表数据访问。
type UserRepo struct {
	db *gorm.DB
}

func NewUserRepo(db *gorm.DB) *UserRepo { return &UserRepo{db: db} }

// Create 新建用户。
func (r *UserRepo) Create(u *model.User) error { return r.db.Create(u).Error }

// GetByUsername 按用户名查询（含停用账号，供登录给出明确提示）。
func (r *UserRepo) GetByUsername(username string) (*model.User, error) {
	var u model.User
	err := r.db.Where("username = ?", username).First(&u).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &u, err
}

// GetByID 按主键查询（auth 中间件每次调用，校验停用状态）。
func (r *UserRepo) GetByID(id int64) (*model.User, error) {
	var u model.User
	err := r.db.First(&u, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &u, err
}

// Update 保存修改。
func (r *UserRepo) Update(u *model.User) error { return r.db.Save(u).Error }

// List 全部用户（管理页）。
func (r *UserRepo) List() ([]model.User, error) {
	var users []model.User
	err := r.db.Order("id").Find(&users).Error
	return users, err
}

// CountActiveAdmins 统计启用中的管理员数量（保底最后一个管理员）。
func (r *UserRepo) CountActiveAdmins() (int64, error) {
	var n int64
	err := r.db.Model(&model.User{}).
		Where("role = ? AND status = ?", model.RoleAdmin, model.StatusActive).Count(&n).Error
	return n, err
}

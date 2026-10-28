package service

import (
	"context"
	"errors"

	"golang.org/x/crypto/bcrypt"

	"jizhang/internal/model"
)

// 用户管理业务错误。
var (
	ErrUserNotFound  = errors.New("用户不存在")
	ErrUsernameTaken = errors.New("用户名已占用")
	ErrLastAdmin     = errors.New("必须保留至少一个启用状态的管理员")
	ErrInvalidRole   = errors.New("无效的角色")
	ErrWeakPassword  = errors.New("密码至少 6 位")
	ErrInvalidStatus = errors.New("无效的账号状态")
)

// UserManageStore 用户管理所需数据访问。
type UserManageStore interface {
	UserStore
	Create(u *model.User) error
	Update(u *model.User) error
	List() ([]model.User, error)
	CountActiveAdmins() (int64, error)
}

// SessionRevoker 吊销用户全部在线会话（AuthService 实现）。
type SessionRevoker interface {
	RevokeAll(ctx context.Context, uid int64) error
}

var validRoles = map[model.Role]bool{
	model.RoleAdmin:    true,
	model.RoleRecorder: true,
	model.RoleMember:   true,
	model.RoleViewer:   true,
}

var validStatuses = map[model.UserStatus]bool{
	model.StatusActive:   true,
	model.StatusDisabled: true,
}

// UserService 用户管理（仅管理员调用，入口由路由角色中间件把守）。
type UserService struct {
	store    UserManageStore
	sessions SessionRevoker
}

func NewUserService(store UserManageStore, sessions SessionRevoker) *UserService {
	return &UserService{store: store, sessions: sessions}
}

// List 全部用户。
func (s *UserService) List() ([]model.User, error) { return s.store.List() }

// Create 建号。
func (s *UserService) Create(username, password string, role model.Role) (*model.User, error) {
	if !validRoles[role] {
		return nil, ErrInvalidRole
	}
	if len(password) < 6 {
		return nil, ErrWeakPassword
	}
	existing, err := s.store.GetByUsername(username)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, ErrUsernameTaken
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	u := &model.User{
		Username:     username,
		PasswordHash: string(hash),
		Role:         role,
		Status:       model.StatusActive,
	}
	if err := s.store.Create(u); err != nil {
		return nil, err
	}
	return u, nil
}

// guardAdmin 保护最后一个启用管理员：当 target 即将失去 admin 身份或被停用时拦截。
func (s *UserService) guardAdmin(target *model.User, removingAdmin bool) error {
	if !removingAdmin || target.Role != model.RoleAdmin || target.Status != model.StatusActive {
		return nil
	}
	n, err := s.store.CountActiveAdmins()
	if err != nil {
		return err
	}
	if n <= 1 {
		return ErrLastAdmin
	}
	return nil
}

// SetRole 调整角色；降级最后一个管理员被拒绝。
func (s *UserService) SetRole(targetID int64, role model.Role) (*model.User, error) {
	if !validRoles[role] {
		return nil, ErrInvalidRole
	}
	u, err := s.store.GetByID(targetID)
	if err != nil {
		return nil, err
	}
	if u == nil {
		return nil, ErrUserNotFound
	}
	if err := s.guardAdmin(u, role != model.RoleAdmin); err != nil {
		return nil, err
	}
	u.Role = role
	if err := s.store.Update(u); err != nil {
		return nil, err
	}
	if err := s.sessions.RevokeAll(context.Background(), u.ID); err != nil {
		return nil, err
	}
	return u, nil
}

// SetStatus 停用/恢复；停用最后一个管理员被拒绝，停用即踢下线。
func (s *UserService) SetStatus(targetID int64, status model.UserStatus) (*model.User, error) {
	if !validStatuses[status] {
		return nil, ErrInvalidStatus
	}
	u, err := s.store.GetByID(targetID)
	if err != nil {
		return nil, err
	}
	if u == nil {
		return nil, ErrUserNotFound
	}
	if err := s.guardAdmin(u, status == model.StatusDisabled); err != nil {
		return nil, err
	}
	u.Status = status
	if err := s.store.Update(u); err != nil {
		return nil, err
	}
	if err := s.sessions.RevokeAll(context.Background(), u.ID); err != nil {
		return nil, err
	}
	return u, nil
}

// ResetPassword 重置密码并吊销该用户全部会话。
func (s *UserService) ResetPassword(targetID int64, newPassword string) error {
	if len(newPassword) < 6 {
		return ErrWeakPassword
	}
	u, err := s.store.GetByID(targetID)
	if err != nil {
		return err
	}
	if u == nil {
		return ErrUserNotFound
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	u.PasswordHash = string(hash)
	if err := s.store.Update(u); err != nil {
		return err
	}
	return s.sessions.RevokeAll(context.Background(), u.ID)
}

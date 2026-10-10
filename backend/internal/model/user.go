package model

import "time"

// Role 四档角色。
type Role string

const (
	RoleAdmin    Role = "admin"    // 管理员：全部权限
	RoleRecorder Role = "recorder" // 记账员：记账并可改删全部流水
	RoleMember   Role = "member"   // 普通成员：记账，仅改删自己创建的
	RoleViewer   Role = "viewer"   // 只读：仅查看与统计
)

// UserStatus 账号状态。
type UserStatus string

const (
	StatusActive   UserStatus = "active"
	StatusDisabled UserStatus = "disabled"
)

// User 系统用户（单账本共享，不开放注册）。
type User struct {
	ID                           int64      `gorm:"primaryKey" json:"id"`
	Username                     string     `gorm:"size:64;uniqueIndex" json:"username"`
	PasswordHash                 string     `gorm:"size:255" json:"-"`
	Role                         Role       `json:"role"`
	Status                       UserStatus `json:"status"`
	LiabilityAccountsInitialized bool       `json:"-"`
	CreatedAt                    time.Time  `json:"created_at"`
	UpdatedAt                    time.Time  `json:"updated_at"`
}

func (User) TableName() string { return "users" }

// IsActive 账号是否可用。
func (u *User) IsActive() bool { return u.Status == StatusActive }

// IsAdmin 是否管理员。
func (u *User) IsAdmin() bool { return u.Role == RoleAdmin }

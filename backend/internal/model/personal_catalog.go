package model

import "time"

type AccountTemplate struct {
	Code string `gorm:"primaryKey"`
	Name string
	Sort int
}
type CategoryTemplate struct {
	ID       int64 `gorm:"primaryKey"`
	ParentID *int64
	Kind     CategoryKind
	Name     string
	Sort     int
}
type AccountCreateRequest struct {
	OwnerID    int64  `gorm:"primaryKey"`
	RequestKey string `gorm:"primaryKey;size:128"`
	Digest     string `gorm:"size:64"`
	AccountID  *int64
	CreatedAt  time.Time
}

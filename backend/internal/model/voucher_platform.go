package model

import "time"

// VoucherPlatform 是个人名称目录；券和流水仍保存平台文本快照。
type VoucherPlatform struct {
	ID        int64     `gorm:"primaryKey" json:"id"`
	Sort      int       `gorm:"not null;default:0" json:"sort"`
	OwnerID   int64     `gorm:"not null;uniqueIndex:uk_voucher_platform_owner_name,priority:1" json:"-"`
	Name      string    `gorm:"size:128;not null;uniqueIndex:uk_voucher_platform_owner_name,priority:2" json:"name"`
	CreatedAt time.Time `json:"-"`
}

type VoucherPlatformTemplate struct {
	Name string `gorm:"primaryKey;size:128"`
}

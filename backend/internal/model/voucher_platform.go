package model

import "time"

// VoucherPlatform 是共享名称目录；券仍保存平台文本，目录不参与账务。
type VoucherPlatform struct {
	ID        int64     `gorm:"primaryKey" json:"id"`
	Name      string    `gorm:"size:128;not null;uniqueIndex:uk_voucher_platform_name" json:"name"`
	CreatedAt time.Time `json:"-"`
}

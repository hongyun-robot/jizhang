package model

import "time"

// Setting 键值配置。
type Setting struct {
	Key       string    `gorm:"primaryKey;size:64" json:"key"`
	Value     string    `gorm:"size:255" json:"value"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (Setting) TableName() string { return "settings" }

// 设置键名。
const (
	// SettingSettlementDay 结算日 1-28，缺省 1（自然月）。
	SettingSettlementDay = "settlement_day"
)

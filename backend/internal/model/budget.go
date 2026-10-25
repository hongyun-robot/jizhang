package model

import (
	"time"

	"github.com/shopspring/decimal"
)

// Budget 账期预算：category_id 为 NULL 表示总预算，否则为一级分类预算
// 每个登录账号独立配置（specs/budgets，schema 见 migrations/000022_personal_budgets）。
type Budget struct {
	ID         int64           `gorm:"primaryKey" json:"id"`
	OwnerID    int64           `gorm:"not null;index" json:"owner_id"`
	PeriodKey  string          `gorm:"type:char(7);index" json:"period_key"`
	CategoryID *int64          `json:"category_id"`
	Amount     decimal.Decimal `gorm:"type:decimal(12,2)" json:"amount"`
	CreatedAt  time.Time       `json:"created_at"`
	UpdatedAt  time.Time       `json:"updated_at"`
}

func (Budget) TableName() string { return "budgets" }

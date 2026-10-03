package model

import (
	"time"

	"github.com/shopspring/decimal"
)

// RuleFreq 周期模板频率。
type RuleFreq string

const (
	FreqDaily   RuleFreq = "daily"
	FreqWeekly  RuleFreq = "weekly"
	FreqMonthly RuleFreq = "monthly"
	FreqYearly  RuleFreq = "yearly"
)

// RuleStatus 模板状态：启用 / 暂停（软开关，不删数据）。
type RuleStatus string

const (
	RuleEnabled RuleStatus = "enabled"
	RulePaused  RuleStatus = "paused"
)

// RecurringRule 周期记账模板（specs/recurring-entries）：
// 固定金额 + 固定频率，按 start_date 起算，next_run_date 指向下一次应生成的归属日。
type RecurringRule struct {
	ID          int64           `gorm:"primaryKey" json:"id"`
	Amount      decimal.Decimal `gorm:"type:decimal(12,2)" json:"amount"`
	Freq        RuleFreq        `json:"freq"`
	AccountID   int64           `json:"account_id"`
	CategoryID  int64           `json:"category_id"`
	Note        string          `gorm:"size:255" json:"note"`
	StartDate   time.Time       `gorm:"type:date" json:"start_date"`
	NextRunDate time.Time       `gorm:"type:date" json:"next_run_date"`
	Status      RuleStatus      `json:"status"`
	CreatedBy   int64           `json:"created_by"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

func (RecurringRule) TableName() string { return "recurring_rules" }

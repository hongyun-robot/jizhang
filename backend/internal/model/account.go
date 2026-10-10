package model

import (
	"time"

	"github.com/shopspring/decimal"
)

// AccountType 账户类型。
type AccountType string

const (
	AccountAsset     AccountType = "asset"     // 资产：现金/微信/银行卡…
	AccountLiability AccountType = "liability" // 负债：信用卡（余额为负=欠款）
)

// Account 资金账户。余额不冗余，由流水聚合推导（设计 D4）。
type Account struct {
	ID                  int64           `gorm:"primaryKey" json:"id"`
	Name                string          `gorm:"size:64" json:"name"`
	Type                AccountType     `json:"type"`
	OwnerID             *int64          `gorm:"index" json:"owner_id"`
	OpeningBalance      decimal.Decimal `gorm:"type:decimal(12,2)" json:"opening_balance"`
	RepaymentDay        *int            `gorm:"type:tinyint" json:"repayment_day"` // 信用卡还款日 1-28
	LiabilityType       string          `gorm:"size:32" json:"liability_type"`
	BillingEnabled      bool            `json:"billing_enabled"`
	Status              UserStatus      `json:"status"`
	Sort                int             `json:"sort"`
	Balance             decimal.Decimal `gorm:"-" json:"balance"` // 聚合计算，见 repository
	HasFinancialHistory bool            `gorm:"-" json:"has_financial_history"`
	CreatedAt           time.Time       `json:"created_at"`
	UpdatedAt           time.Time       `json:"updated_at"`
}

func (Account) TableName() string { return "accounts" }

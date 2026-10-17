package model

import (
	"encoding/json"
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
	ID                  int64            `gorm:"primaryKey" json:"id"`
	Name                string           `gorm:"size:64" json:"name"`
	Type                AccountType      `json:"type"`
	AssetClass          string           `gorm:"size:16" json:"asset_class,omitempty"`
	OwnerID             *int64           `gorm:"index" json:"owner_id"`
	OpeningBalance      decimal.Decimal  `gorm:"type:decimal(12,2)" json:"opening_balance"`
	RepaymentDay        *int             `gorm:"type:tinyint" json:"repayment_day"` // 信用卡还款日 1-28
	LiabilityType       string           `gorm:"size:32" json:"liability_type"`
	BillingEnabled      bool             `json:"billing_enabled"`
	PrincipalRemainder  string           `gorm:"size:8;not null;default:last" json:"principal_remainder"`
	Status              UserStatus       `json:"status"`
	Sort                int              `json:"sort"`
	Balance             decimal.Decimal  `gorm:"-" json:"balance"` // 聚合计算，见 repository
	KnownBalance        *decimal.Decimal `gorm:"-" json:"known_balance,omitempty"`
	ValuationStatus     string           `gorm:"-" json:"valuation_status,omitempty"`
	NAVDate             string           `gorm:"-" json:"nav_date,omitempty"`
	HasFinancialHistory bool             `gorm:"-" json:"has_financial_history"`
	CreatedAt           time.Time        `json:"created_at"`
	UpdatedAt           time.Time        `json:"updated_at"`
}

func (Account) TableName() string { return "accounts" }

func (a Account) IsFund() bool       { return a.Type == AccountAsset && a.AssetClass == "fund" }
func (a Account) IsGold() bool       { return a.Type == AccountAsset && a.AssetClass == "gold" }
func (a Account) IsWealth() bool     { return a.Type == AccountAsset && a.AssetClass == "wealth" }
func (a Account) IsWealthCash() bool { return a.Type == AccountAsset && a.AssetClass == "wealth_cash" }
func (a Account) IsInvestment() bool { return a.IsFund() || a.IsGold() || a.IsWealth() }

// Unknown gold value is explicitly null on the wire; retain exact decimal internally.
func (a Account) MarshalJSON() ([]byte, error) {
	type plain Account
	if ((a.IsGold() || a.IsWealth()) && a.ValuationStatus == "no_price") || (a.IsWealth() && a.ValuationStatus == "range_error") {
		return json.Marshal(struct {
			plain
			Balance *decimal.Decimal `json:"balance"`
		}{plain(a), nil})
	}
	return json.Marshal(plain(a))
}
func (a Account) IsCash() bool {
	return a.Type == AccountAsset && (a.AssetClass == "" || a.AssetClass == "cash" || a.AssetClass == "wealth_cash")
}

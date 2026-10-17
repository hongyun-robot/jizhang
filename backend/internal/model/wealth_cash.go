package model

import (
	"github.com/shopspring/decimal"
	"time"
)

// WealthCashLink adds NAV-based estimated interest to the same payment account.
// It is never a second position or a second balance included in total assets.
type WealthCashLink struct {
	ID               int64      `gorm:"primaryKey" json:"id"`
	OwnerID          int64      `gorm:"not null;index" json:"owner_id"`
	AccountID        int64      `gorm:"uniqueIndex;not null" json:"account_id"`
	ProductID        int64      `gorm:"not null;index" json:"product_id"`
	Policy           string     `gorm:"size:32;not null" json:"policy"`
	StartDate        time.Time  `gorm:"type:date;not null" json:"start_date"`
	Active           bool       `gorm:"not null" json:"active"`
	Revision         int64      `gorm:"not null" json:"revision"`
	SyncStatus       string     `gorm:"size:32;not null" json:"sync_status"`
	CalculationHash  string     `gorm:"size:64;not null;default:''" json:"-"`
	LastError        string     `gorm:"size:255" json:"last_error,omitempty"`
	LastReconciledAt *time.Time `json:"last_reconciled_at,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

// WealthCashFlow records the user-supplied timing of the existing cash fact.
// Missing bank confirmation is an estimate, never a real bank order.
type WealthCashFlow struct {
	ID               int64      `gorm:"primaryKey" json:"id"`
	TxnID            int64      `gorm:"uniqueIndex:uk_cash_flow,priority:1;not null" json:"txn_id"`
	AccountID        int64      `gorm:"uniqueIndex:uk_cash_flow,priority:2;not null" json:"account_id"`
	OwnerID          int64      `gorm:"not null" json:"owner_id"`
	ApplicationTime  string     `gorm:"size:8;not null" json:"application_time"`
	TransferMode     string     `gorm:"size:16;not null" json:"transfer_mode"`
	ConfirmationDate *time.Time `gorm:"type:date" json:"confirmation_date,omitempty"`
	RightsEndDate    *time.Time `gorm:"type:date" json:"rights_end_date,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

type WealthCashYield struct {
	ID               int64           `gorm:"primaryKey" json:"id"`
	LinkID           int64           `gorm:"index;not null" json:"link_id"`
	OwnerID          int64           `gorm:"not null" json:"owner_id"`
	AccountID        int64           `gorm:"uniqueIndex:uk_cash_yield,priority:1;not null" json:"account_id"`
	ProductID        int64           `gorm:"not null" json:"product_id"`
	NAVDate          time.Time       `gorm:"type:date;uniqueIndex:uk_cash_yield,priority:2;not null" json:"nav_date"`
	PreviousNAVDate  time.Time       `gorm:"type:date;not null" json:"previous_nav_date"`
	UnitNAV          decimal.Decimal `gorm:"type:decimal(20,8);not null" json:"unit_nav"`
	PreviousNAV      decimal.Decimal `gorm:"type:decimal(20,8);not null" json:"previous_nav"`
	CalculatedAmount decimal.Decimal `gorm:"type:decimal(12,2);not null" json:"calculated_amount"`
	Amount           decimal.Decimal `gorm:"type:decimal(12,2);not null" json:"amount"`
	ManualOverride   bool            `gorm:"not null" json:"manual_override"`
	ManualStartDate  *time.Time      `gorm:"type:date" json:"manual_start_date,omitempty"`
	TxnID            *int64          `gorm:"uniqueIndex" json:"txn_id"`
	Revision         int64           `gorm:"not null" json:"revision"`
	Status           string          `gorm:"size:32;not null" json:"status"`
	Note             string          `gorm:"size:255" json:"note"`
	CreatedAt        time.Time       `json:"created_at"`
	UpdatedAt        time.Time       `json:"updated_at"`
}

type WealthCashAudit struct {
	ID          int64     `gorm:"primaryKey" json:"id"`
	OwnerID     int64     `gorm:"uniqueIndex:uk_cash_audit_request,priority:1;not null" json:"owner_id"`
	LinkID      int64     `gorm:"index;not null" json:"link_id"`
	YieldID     *int64    `json:"yield_id,omitempty"`
	Action      string    `gorm:"size:32;not null" json:"action"`
	RequestKey  *string   `gorm:"size:128;uniqueIndex:uk_cash_audit_request,priority:2" json:"request_key,omitempty"`
	PayloadHash string    `gorm:"size:64" json:"-"`
	BeforeJSON  string    `gorm:"type:longtext;not null" json:"before"`
	AfterJSON   string    `gorm:"type:longtext;not null" json:"after"`
	CreatedBy   int64     `gorm:"not null" json:"created_by"`
	CreatedAt   time.Time `json:"created_at"`
}

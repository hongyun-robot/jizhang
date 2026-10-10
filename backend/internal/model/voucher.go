package model

import (
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"time"
)

type Voucher struct {
	ID                  int64           `gorm:"primaryKey" json:"id"`
	Name                string          `gorm:"size:128" json:"name"`
	Platform            string          `gorm:"size:128" json:"platform"`
	Note                string          `gorm:"size:255" json:"note"`
	PaidAmount          decimal.Decimal `gorm:"type:decimal(12,2)" json:"paid_amount"`
	PurchaseDate        time.Time       `gorm:"type:date" json:"purchase_date"`
	AccountID           int64           `json:"account_id"`
	CategoryID          int64           `json:"category_id"`
	TotalUnits          int64           `json:"total_units"`
	ExpiresOn           *time.Time      `gorm:"type:date;index:idx_voucher_due,priority:3" json:"expires_on"`
	AutoRefundEnabled   bool            `gorm:"index:idx_voucher_due,priority:1" json:"auto_refund_enabled"`
	AutoRefundSuspended bool            `gorm:"index:idx_voucher_due,priority:2" json:"auto_refund_suspended"`
	ScheduleVersion     int64           `json:"schedule_version"`
	Revision            int64           `json:"-"`
	CreatedBy           int64           `gorm:"uniqueIndex:uk_voucher_create,priority:1" json:"created_by"`
	CreateRequestKey    string          `gorm:"size:64;uniqueIndex:uk_voucher_create,priority:2" json:"-"`
	CreatePayloadHash   string          `gorm:"size:64" json:"-"`
	LastError           string          `gorm:"size:255" json:"last_error"`
	LastAttemptAt       *time.Time      `json:"last_attempt_at"`
	CreatedAt           time.Time       `json:"created_at"`
	UpdatedAt           time.Time       `json:"updated_at"`
	DeletedAt           gorm.DeletedAt  `gorm:"index" json:"-"`
}

type VoucherAction string

const (
	VoucherPurchase VoucherAction = "purchase"
	VoucherRedeem   VoucherAction = "redeem"
	VoucherRefund   VoucherAction = "refund"
	VoucherWriteoff VoucherAction = "writeoff"
)

type VoucherEvent struct {
	ID              int64           `gorm:"primaryKey" json:"id"`
	VoucherID       int64           `gorm:"uniqueIndex:uk_voucher_event_request,priority:1;index:idx_voucher_event_active,priority:1" json:"voucher_id"`
	Action          VoucherAction   `gorm:"size:16" json:"action"`
	Units           int64           `json:"units"`
	Amount          decimal.Decimal `gorm:"type:decimal(12,2)" json:"amount"`
	EntryDate       time.Time       `gorm:"type:date" json:"entry_date"`
	PeriodKey       string          `gorm:"size:7" json:"period_key"`
	AccountID       int64           `json:"account_id"`
	CategoryID      int64           `json:"category_id"`
	CreatedBy       int64           `json:"created_by"`
	Source          string          `gorm:"size:16" json:"source"`
	RequestKey      string          `gorm:"size:64;uniqueIndex:uk_voucher_event_request,priority:2" json:"-"`
	PayloadHash     string          `gorm:"size:64" json:"-"`
	ScheduleVersion *int64          `json:"schedule_version"`
	VoidedAt        *time.Time      `gorm:"index:idx_voucher_event_active,priority:2" json:"voided_at"`
	VoidedBy        *int64          `json:"voided_by"`
	CreatedAt       time.Time       `json:"created_at"`
}

// VoucherCost 连续份额成本，最后一份承接余数；调用方先校验数量范围。
func VoucherCost(paid decimal.Decimal, total, settled, units int64) decimal.Decimal {
	if total <= 0 || units <= 0 || settled < 0 || units > total-settled {
		return decimal.Zero
	}
	fen := paid.Shift(2).IntPart()
	cost := (fen / total) * units
	if settled+units == total {
		cost += fen % total
	}
	return decimal.New(cost, -2)
}

package model

import (
	"time"

	"github.com/shopspring/decimal"
)

// TxnKind 流水类型：支出/收入/转账/退款（转账不进统计与预算，退款按负支出冲减）。
type TxnKind string

const (
	TxnExpense  TxnKind = "expense"
	TxnIncome   TxnKind = "income"
	TxnTransfer TxnKind = "transfer"
	TxnRefund   TxnKind = "refund"
)

// TxnSource 来源：手记 / 周期模板补齐。
type TxnSource string

const (
	SourceManual    TxnSource = "manual"
	SourceRecurring TxnSource = "recurring"
)

// Transaction 一条流水（金额恒为正数，方向由 kind 表达）。
type Transaction struct {
	ID                int64           `gorm:"primaryKey" json:"id"`
	Kind              TxnKind         `json:"kind"`
	Amount            decimal.Decimal `gorm:"type:decimal(12,2)" json:"amount"`
	AccountID         int64           `json:"account_id"`
	TransferAccountID *int64          `json:"transfer_account_id"`
	CategoryID        *int64          `json:"category_id"`
	EntryDate         time.Time       `gorm:"type:date" json:"entry_date"`
	PeriodKey         string          `gorm:"type:char(7);index" json:"period_key"`
	Note              string          `gorm:"size:255" json:"note"`
	CreatedBy         int64           `json:"created_by"`
	Source            TxnSource       `json:"source"`
	RuleID            *int64          `json:"rule_id"`
	RelatedTxnID      *int64          `gorm:"index" json:"related_txn_id"` // 关联原支出流水（退款用，见 design D2）
	// RefundedAmount 该支出被关联退款累计的金额（列表查询时聚合填充，不落库）。
	RefundedAmount decimal.Decimal `gorm:"-" json:"refunded_amount"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

func (Transaction) TableName() string { return "transactions" }

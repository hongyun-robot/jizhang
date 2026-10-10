package model

import (
	"time"

	"github.com/shopspring/decimal"
)

// TxnKind 流水类型：支出/收入/转账/退款；普通退款按负支出处理，券用途另定资金与消费影响。
type TxnKind string

const (
	TxnExpense  TxnKind = "expense"
	TxnIncome   TxnKind = "income"
	TxnTransfer TxnKind = "transfer"
	TxnRefund   TxnKind = "refund"
)

// TxnSource 来源：手记 / 周期模板补齐 / 券到期自动退款。
type TxnSource string

const (
	SourceManual        TxnSource = "manual"
	SourceRecurring     TxnSource = "recurring"
	SourceVoucherAuto   TxnSource = "voucher_auto"
	SourceLiabilityAuto TxnSource = "liability_auto"
)

// Transaction 一条流水（金额恒正，资金与消费方向由 kind 和 purpose 共同表达）。
type Transaction struct {
	ID                   int64           `gorm:"primaryKey" json:"id"`
	Kind                 TxnKind         `json:"kind"`
	Amount               decimal.Decimal `gorm:"type:decimal(12,2)" json:"amount"`
	AccountID            int64           `json:"account_id"`
	TransferAccountID    *int64          `json:"transfer_account_id"`
	CategoryID           *int64          `json:"category_id"`
	EntryDate            time.Time       `gorm:"type:date" json:"entry_date"`
	PeriodKey            string          `gorm:"type:char(7);index" json:"period_key"`
	Note                 string          `gorm:"size:255" json:"note"`
	Platform             string          `gorm:"size:128;not null;default:''" json:"platform"`
	CreatedBy            int64           `json:"created_by"`
	Source               TxnSource       `json:"source"`
	RuleID               *int64          `json:"rule_id"`
	RelatedTxnID         *int64          `gorm:"index" json:"related_txn_id"` // 关联原支出流水（退款用，见 design D2）
	Purpose              TxnPurpose      `gorm:"size:32;not null;default:regular" json:"purpose"`
	VoucherEventID       *int64          `gorm:"uniqueIndex" json:"voucher_event_id"`
	VoucherID            *int64          `gorm:"-" json:"voucher_id"`
	InstallmentPlanID    *int64          `json:"installment_plan_id"`
	LiabilityTermID      *int64          `json:"liability_term_id"`
	LiabilityOperationID *int64          `json:"liability_operation_id"`
	LiabilityAccountID   *int64          `gorm:"-" json:"liability_account_id"`
	BalanceEffect        decimal.Decimal `gorm:"-" json:"balance_effect"`
	ExpenseEffect        decimal.Decimal `gorm:"-" json:"expense_effect"`
	// RefundedAmount 该支出被关联退款累计的金额（列表查询时聚合填充，不落库）。
	RefundedAmount decimal.Decimal `gorm:"-" json:"refunded_amount"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

func (Transaction) TableName() string { return "transactions" }

package model

import (
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"time"
)

type LiabilityBillRule struct {
	ID                   int64     `gorm:"primaryKey" json:"id"`
	AccountID            int64     `gorm:"uniqueIndex:uk_bill_rule_version" json:"account_id"`
	Version              int       `gorm:"uniqueIndex:uk_bill_rule_version" json:"version"`
	StatementDay         int       `json:"statement_day"`
	RepaymentDay         int       `json:"repayment_day"`
	RepaymentMonthOffset int       `json:"repayment_month_offset"`
	EffectiveFrom        string    `gorm:"type:date;index" json:"effective_from"`
	CreatedAt            time.Time `json:"created_at"`
}

type LiabilityStatement struct {
	ID              int64            `gorm:"primaryKey" json:"id"`
	AccountID       int64            `gorm:"index" json:"account_id"`
	Kind            string           `gorm:"size:32;default:monthly" json:"kind"`
	BillKey         string           `gorm:"size:128;uniqueIndex" json:"-"`
	RuleID          int64            `json:"rule_id"`
	CycleStart      string           `gorm:"type:date" json:"cycle_start"`
	CycleEnd        string           `gorm:"type:date;index" json:"cycle_end"`
	DueDate         string           `gorm:"type:date;index" json:"due_date"`
	IssuedAt        *time.Time       `json:"issued_at"`
	SnapshotAmount  decimal.Decimal  `gorm:"type:decimal(12,2)" json:"snapshot_amount"`
	ReferenceAmount *decimal.Decimal `gorm:"type:decimal(12,2)" json:"reference_amount"`
	ReconciledAt    *time.Time       `json:"reconciled_at"`
	CreatedAt       time.Time        `json:"created_at"`
	Amount          decimal.Decimal  `gorm:"-" json:"amount"`
	PaidAmount      decimal.Decimal  `gorm:"-" json:"paid_amount"`
	Remaining       decimal.Decimal  `gorm:"-" json:"remaining"`
}

type LiabilityDebtItem struct {
	ID               int64           `gorm:"primaryKey" json:"id"`
	AccountID        int64           `gorm:"index" json:"account_id"`
	SourceTxnID      *int64          `gorm:"index" json:"source_txn_id"`
	SourceID         int64           `json:"source_id"`
	ItemKey          string          `gorm:"size:128;uniqueIndex" json:"item_key"`
	Kind             string          `gorm:"size:32" json:"kind"`
	StatementID      int64           `gorm:"index" json:"statement_id"`
	PlanID           *int64          `gorm:"index" json:"plan_id"`
	TermID           *int64          `json:"term_id"`
	Amount           decimal.Decimal `gorm:"type:decimal(12,2)" json:"amount"`
	Active           bool            `json:"active"`
	ManualAssignment bool            `json:"manual_assignment"`
	CreatedAt        time.Time       `json:"created_at"`
	UpdatedAt        time.Time       `json:"updated_at"`
	Remaining        decimal.Decimal `gorm:"-" json:"remaining"`
}

type LiabilityCreditEvent struct {
	ID              int64           `gorm:"primaryKey" json:"id"`
	AccountID       int64           `gorm:"uniqueIndex:uk_credit_source;index" json:"account_id"`
	SourceTxnID     *int64          `json:"source_txn_id"`
	SourceID        int64           `gorm:"uniqueIndex:uk_credit_source" json:"source_id"`
	Amount          decimal.Decimal `gorm:"type:decimal(12,2)" json:"amount"`
	EntryDate       string          `gorm:"type:date" json:"entry_date"`
	RelatedSourceID int64           `json:"related_source_id"`
	Mode            string          `gorm:"size:32" json:"mode"`
	Targets         string          `gorm:"type:text" json:"targets"`
	Active          bool            `json:"active"`
	CreatedAt       time.Time       `json:"created_at"`
	Remaining       decimal.Decimal `gorm:"-" json:"remaining"`
}

type LiabilityAllocation struct {
	ID            int64           `gorm:"primaryKey" json:"id"`
	CreditEventID int64           `gorm:"index" json:"credit_event_id"`
	DebtItemID    int64           `gorm:"index" json:"debt_item_id"`
	Amount        decimal.Decimal `gorm:"type:decimal(12,2)" json:"amount"`
	Active        bool            `json:"active"`
	CreatedAt     time.Time       `json:"created_at"`
	VoidedAt      *time.Time      `json:"voided_at"`
}

type InstallmentPlan struct {
	ID            int64             `gorm:"primaryKey" json:"id"`
	SourceTxnID   int64             `gorm:"uniqueIndex" json:"source_txn_id"`
	AccountID     int64             `gorm:"index" json:"account_id"`
	Principal     decimal.Decimal   `gorm:"type:decimal(12,2)" json:"principal"`
	TermCount     int               `json:"term_count"`
	FeeMode       string            `gorm:"size:32" json:"fee_mode"`
	FeeInput      decimal.Decimal   `gorm:"type:decimal(12,2)" json:"fee_input"`
	FeeAccountID  int64             `json:"fee_account_id"`
	FeeCategoryID int64             `json:"fee_category_id"`
	CreatedBy     int64             `json:"created_by"`
	Version       int               `json:"version"`
	Status        string            `gorm:"size:32;index" json:"status"`
	CreatedAt     time.Time         `json:"created_at"`
	UpdatedAt     time.Time         `json:"updated_at"`
	Terms         []InstallmentTerm `gorm:"-" json:"terms"`
}

type InstallmentTerm struct {
	ID            int64           `gorm:"primaryKey" json:"id"`
	PlanID        int64           `gorm:"uniqueIndex:uk_installment_term;index" json:"plan_id"`
	Number        int             `gorm:"uniqueIndex:uk_installment_term" json:"number"`
	StatementID   int64           `json:"statement_id"`
	Principal     decimal.Decimal `gorm:"type:decimal(12,2)" json:"principal"`
	FeeAmount     decimal.Decimal `gorm:"type:decimal(12,2)" json:"fee_amount"`
	FeeDate       string          `gorm:"type:date;index" json:"fee_date"`
	FeeTxnID      *int64          `gorm:"uniqueIndex" json:"fee_txn_id"`
	Status        string          `gorm:"size:32" json:"status"`
	FeeStatus     string          `gorm:"size:32;index" json:"fee_status"`
	BlockedReason string          `gorm:"size:255" json:"blocked_reason"`
	Remaining     decimal.Decimal `gorm:"-" json:"remaining"`
}

type LiabilityOperation struct {
	ID         int64      `gorm:"primaryKey" json:"id"`
	ActorID    int64      `gorm:"uniqueIndex:uk_liability_request" json:"actor_id"`
	RequestKey string     `gorm:"size:128;uniqueIndex:uk_liability_request" json:"request_key"`
	Digest     string     `gorm:"size:64" json:"-"`
	Kind       string     `gorm:"size:32" json:"kind"`
	AccountID  int64      `gorm:"index" json:"account_id"`
	PlanID     *int64     `json:"plan_id"`
	Reason     string     `gorm:"size:255" json:"reason"`
	BeforeJSON string     `gorm:"type:longtext" json:"before_json"`
	ResultJSON string     `gorm:"type:longtext" json:"result_json"`
	ReversedAt *time.Time `json:"reversed_at"`
	CreatedAt  time.Time  `json:"created_at"`
}

func LiabilityModels() []any {
	return []any{&LiabilityAccountType{}, &LiabilityBillRule{}, &LiabilityStatement{}, &LiabilityDebtItem{}, &LiabilityCreditEvent{}, &LiabilityAllocation{}, &InstallmentPlan{}, &InstallmentTerm{}, &LiabilityOperation{}}
}

// MySQL parseTime and SQLite DATE both scan dates as RFC3339 strings. Public
// billing dates are calendar dates; normalize at the persistence boundary.
func calendarDate(s string) string {
	if len(s) > 10 {
		return s[:10]
	}
	return s
}
func (r *LiabilityBillRule) AfterFind(*gorm.DB) error {
	r.EffectiveFrom = calendarDate(r.EffectiveFrom)
	return nil
}
func (b *LiabilityStatement) AfterFind(*gorm.DB) error {
	b.CycleStart = calendarDate(b.CycleStart)
	b.CycleEnd = calendarDate(b.CycleEnd)
	b.DueDate = calendarDate(b.DueDate)
	return nil
}
func (c *LiabilityCreditEvent) AfterFind(*gorm.DB) error {
	c.EntryDate = calendarDate(c.EntryDate)
	return nil
}
func (t *InstallmentTerm) AfterFind(*gorm.DB) error { t.FeeDate = calendarDate(t.FeeDate); return nil }

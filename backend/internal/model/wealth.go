package model

import (
	"github.com/shopspring/decimal"
	"time"
)

// WealthProduct contains public identity and controlled admission, never holdings.
type WealthProduct struct {
	ID               int64      `gorm:"primaryKey" json:"id"`
	Issuer           string     `gorm:"size:32;uniqueIndex:uk_wealth_identity,priority:1;not null" json:"issuer"`
	RegistrationCode string     `gorm:"size:64;uniqueIndex:uk_wealth_identity,priority:2;not null" json:"registration_code"`
	Code             string     `gorm:"size:32;uniqueIndex:uk_wealth_identity,priority:3;not null" json:"code"`
	Name             string     `gorm:"size:128;not null" json:"name"`
	Currency         string     `gorm:"size:3;not null" json:"currency"`
	ValuationBasis   string     `gorm:"size:32;not null" json:"valuation_basis"`
	InceptionDate    time.Time  `gorm:"type:date;not null" json:"inception_date"`
	SourceAdapter    string     `gorm:"size:32;not null" json:"source_adapter"`
	SourceCode       string     `gorm:"size:64;not null" json:"source_code"`
	AdmissionStatus  string     `gorm:"size:32;not null" json:"admission_status"`
	EvidenceVersion  string     `gorm:"size:128;not null" json:"evidence_version"`
	SyncStatus       string     `gorm:"size:32;not null;default:pending" json:"sync_status"`
	LastError        string     `gorm:"size:255" json:"last_error,omitempty"`
	LastAttempt      *time.Time `json:"last_attempt,omitempty"`
	LastSuccess      *time.Time `json:"last_success,omitempty"`
	NextAttempt      *time.Time `json:"next_attempt,omitempty"`
	LeaseUntil       *time.Time `json:"-"`
	LeaseToken       string     `gorm:"size:64" json:"-"`
	FailureCount     int        `gorm:"not null;default:0" json:"-"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

type WealthPosition struct {
	ID               int64     `gorm:"primaryKey" json:"id"`
	OwnerID          int64     `gorm:"index;not null" json:"owner_id"`
	AccountID        int64     `gorm:"uniqueIndex:uk_wealth_position,priority:1;not null" json:"account_id"`
	ProductID        int64     `gorm:"uniqueIndex:uk_wealth_position,priority:2;not null" json:"product_id"`
	DisplayName      string    `gorm:"size:128" json:"display_name"`
	ValuationBasis   string    `gorm:"size:32;not null" json:"valuation_basis"`
	BasisConfirmedBy int64     `gorm:"not null" json:"basis_confirmed_by"`
	BasisConfirmedAt time.Time `json:"basis_confirmed_at"`
	EventVersion     int64     `gorm:"not null;default:1" json:"event_version"`
	RebuildPending   bool      `gorm:"not null;default:true;index" json:"-"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type WealthEvent struct {
	ID               int64           `gorm:"primaryKey" json:"id"`
	OwnerID          int64           `gorm:"index;not null" json:"owner_id"`
	AccountID        int64           `gorm:"index;not null" json:"account_id"`
	PositionID       int64           `gorm:"index;not null" json:"position_id"`
	Kind             string          `gorm:"size:24;not null" json:"kind"`
	Shares           decimal.Decimal `gorm:"type:decimal(24,8);not null" json:"shares"`
	Amount           decimal.Decimal `gorm:"type:decimal(36,16);not null" json:"amount"`
	Fee              decimal.Decimal `gorm:"type:decimal(12,2);not null" json:"fee"`
	TradeNAV         decimal.Decimal `gorm:"type:decimal(20,8);not null" json:"trade_nav"`
	EffectiveDate    time.Time       `gorm:"type:date;not null;index" json:"effective_date"`
	CashDate         *time.Time      `gorm:"type:date" json:"cash_date,omitempty"`
	RightsDate       *time.Time      `gorm:"type:date" json:"rights_date,omitempty"`
	RegistrationDate *time.Time      `gorm:"type:date" json:"registration_date,omitempty"`
	CashAccountID    *int64          `gorm:"index" json:"cash_account_id,omitempty"`
	SourceEventID    *int64          `gorm:"index" json:"source_event_id,omitempty"`
	SettlementKey    *int64          `gorm:"uniqueIndex" json:"-"`
	CorporateKey     *string         `gorm:"size:128;uniqueIndex:uk_wealth_rights,priority:2" json:"corporate_key,omitempty"`
	RightsPositionID int64           `gorm:"uniqueIndex:uk_wealth_rights,priority:1;not null" json:"-"`
	Note             string          `gorm:"size:255" json:"note"`
	OrderJSON        string          `gorm:"type:longtext;not null;default:''" json:"order_json,omitempty"`
	CreatedBy        int64           `gorm:"not null" json:"created_by"`
	VoidedAt         *time.Time      `json:"voided_at,omitempty"`
	CreatedAt        time.Time       `json:"created_at"`
}

type WealthMutation struct {
	ID          int64  `gorm:"primaryKey"`
	OwnerID     int64  `gorm:"uniqueIndex:uk_wealth_mutation,priority:1;not null"`
	RequestKey  string `gorm:"size:128;uniqueIndex:uk_wealth_mutation,priority:2;not null"`
	PayloadHash string `gorm:"size:64;not null"`
	Action      string `gorm:"size:32;not null"`
	PositionID  int64
	EventID     int64
	ResultJSON  string `gorm:"type:longtext;not null"`
	CreatedAt   time.Time
}

type WealthNAV struct {
	ID        int64            `gorm:"primaryKey" json:"id"`
	ProductID int64            `gorm:"uniqueIndex:uk_wealth_nav,priority:1;not null" json:"product_id"`
	NAVDate   time.Time        `gorm:"type:date;uniqueIndex:uk_wealth_nav,priority:2;not null" json:"nav_date"`
	UnitNAV   decimal.Decimal  `gorm:"type:decimal(20,8);not null" json:"unit_nav"`
	TotalNAV  *decimal.Decimal `gorm:"type:decimal(20,8)" json:"total_nav"`
	Revision  int64            `gorm:"not null;default:1" json:"revision"`
	FetchedAt time.Time        `json:"fetched_at"`
}

type WealthNAVRevision struct {
	ID         int64            `gorm:"primaryKey"`
	ProductID  int64            `gorm:"index;not null"`
	NAVDate    time.Time        `gorm:"type:date;not null"`
	UnitNAV    decimal.Decimal  `gorm:"type:decimal(20,8);not null"`
	TotalNAV   *decimal.Decimal `gorm:"type:decimal(20,8)"`
	Revision   int64            `gorm:"not null"`
	RecordedAt time.Time
}

type WealthValuation struct {
	ID                    int64            `gorm:"primaryKey" json:"id"`
	PositionID            int64            `gorm:"uniqueIndex:uk_wealth_valuation,priority:1;not null" json:"position_id"`
	NAVDate               time.Time        `gorm:"type:date;uniqueIndex:uk_wealth_valuation,priority:2;not null" json:"nav_date"`
	PreviousNAVDate       *time.Time       `gorm:"type:date" json:"previous_nav_date,omitempty"`
	EventVersion          int64            `gorm:"not null" json:"event_version"`
	SeriesVersion         int64            `gorm:"not null" json:"series_version"`
	QuoteSource           string           `gorm:"size:16;not null" json:"quote_source"`
	QuoteRecordID         int64            `gorm:"not null" json:"quote_record_id"`
	PreviousQuoteSource   string           `gorm:"size:16;not null" json:"previous_quote_source"`
	PreviousQuoteRecordID int64            `gorm:"not null" json:"previous_quote_record_id"`
	QuoteRevision         int64            `gorm:"not null" json:"quote_revision"`
	PreviousQuoteRevision int64            `gorm:"not null" json:"previous_quote_revision"`
	Shares                decimal.Decimal  `gorm:"type:decimal(24,8);not null" json:"shares"`
	Cost                  decimal.Decimal  `gorm:"type:decimal(36,16);not null" json:"cost"`
	MarketValue           decimal.Decimal  `gorm:"type:decimal(12,2);not null" json:"market_value"`
	DailyPnL              *decimal.Decimal `gorm:"type:decimal(12,2)" json:"daily_pnl"`
	Status                string           `gorm:"size:32;not null" json:"status"`
	UpdatedAt             time.Time        `json:"updated_at"`
}

type WealthAudit struct {
	ID         int64     `gorm:"primaryKey" json:"id"`
	OwnerID    int64     `gorm:"index;not null" json:"owner_id"`
	PositionID int64     `gorm:"index;not null" json:"position_id"`
	EventID    int64     `gorm:"index" json:"event_id"`
	ActorID    int64     `gorm:"not null" json:"actor_id"`
	Action     string    `gorm:"size:32;not null" json:"action"`
	BeforeJSON string    `gorm:"type:longtext" json:"before"`
	AfterJSON  string    `gorm:"type:longtext" json:"after"`
	CreatedAt  time.Time `json:"created_at"`
}

// Personal overlays are keyed by owner/product/date. Automatic quotes are separate.
type WealthManualNAV struct {
	ID         int64           `gorm:"primaryKey" json:"id"`
	OwnerID    int64           `gorm:"uniqueIndex:uk_wealth_manual,priority:1;not null" json:"owner_id"`
	ProductID  int64           `gorm:"uniqueIndex:uk_wealth_manual,priority:2;not null" json:"product_id"`
	NAVDate    time.Time       `gorm:"type:date;uniqueIndex:uk_wealth_manual,priority:3;not null" json:"nav_date"`
	UnitNAV    decimal.Decimal `gorm:"type:decimal(20,8);not null" json:"unit_nav"`
	Revision   int64           `gorm:"not null" json:"revision"`
	SourceNote string          `gorm:"size:255;not null" json:"source_note"`
	Withdrawn  bool            `gorm:"not null" json:"withdrawn"`
	CreatedAt  time.Time       `json:"created_at"`
	UpdatedAt  time.Time       `json:"updated_at"`
}

type WealthManualAudit struct {
	ID          int64     `gorm:"primaryKey" json:"id"`
	OwnerID     int64     `gorm:"index;not null" json:"owner_id"`
	ProductID   int64     `gorm:"not null" json:"product_id"`
	ManualNAVID int64     `gorm:"index;not null" json:"manual_nav_id"`
	ActorID     int64     `gorm:"not null" json:"actor_id"`
	Action      string    `gorm:"size:32;not null" json:"action"`
	BeforeJSON  string    `gorm:"type:longtext;not null" json:"before"`
	AfterJSON   string    `gorm:"type:longtext;not null" json:"after"`
	CreatedAt   time.Time `json:"created_at"`
}

type WealthSeries struct {
	OwnerID   int64 `gorm:"primaryKey" json:"owner_id"`
	ProductID int64 `gorm:"primaryKey" json:"product_id"`
	Version   int64 `gorm:"not null" json:"version"`
}

type WealthSyncSlot struct {
	ID          int64      `gorm:"primaryKey" json:"id"`
	ProductID   int64      `gorm:"uniqueIndex:uk_wealth_slot,priority:1;not null" json:"product_id"`
	Slot        time.Time  `gorm:"uniqueIndex:uk_wealth_slot,priority:2;not null" json:"slot"`
	Status      string     `gorm:"size:24;not null" json:"status"`
	LeaseToken  string     `gorm:"size:64" json:"-"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	MergedInto  *time.Time `json:"merged_into,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

// WealthQuote identity includes source and record ID, not just a local revision.
type WealthQuote struct {
	ProductID int64            `json:"product_id"`
	NAVDate   time.Time        `json:"nav_date"`
	UnitNAV   decimal.Decimal  `json:"unit_nav"`
	TotalNAV  *decimal.Decimal `json:"total_nav"`
	Source    string           `json:"source"`
	RecordID  int64            `json:"record_id"`
	Revision  int64            `json:"revision"`
}

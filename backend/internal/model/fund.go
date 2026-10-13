package model

import (
	"github.com/shopspring/decimal"
	"time"
)

// FundInstrument 仅保存公开行情元数据，不包含个人持仓。
type FundInstrument struct {
	ID                  int64      `gorm:"primaryKey" json:"id"`
	THSCode             string     `gorm:"size:32;uniqueIndex;not null" json:"thscode"`
	Ticker              string     `gorm:"size:16;not null" json:"ticker"`
	Name                string     `gorm:"size:128" json:"name"`
	AssetType           string     `gorm:"size:32" json:"asset_type"`
	Manager             string     `gorm:"size:128" json:"manager"`
	Management          string     `gorm:"size:128" json:"management"`
	SyncStatus          string     `gorm:"size:32;not null;default:pending" json:"sync_status"`
	LastError           string     `gorm:"size:255" json:"last_error,omitempty"`
	LastAttempt         *time.Time `json:"last_attempt,omitempty"`
	NextAttempt         *time.Time `json:"next_attempt,omitempty"`
	LeaseUntil          *time.Time `json:"-"`
	FailureCount        int        `gorm:"not null;default:0" json:"-"`
	HistoryLoaded       bool       `gorm:"not null;default:false" json:"-"`
	EveningCompletedFor *time.Time `gorm:"type:date" json:"-"`
	MorningAttemptedFor *time.Time `gorm:"type:date" json:"-"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

type FundPosition struct {
	ID               int64     `gorm:"primaryKey" json:"id"`
	OwnerID          int64     `gorm:"index;not null" json:"owner_id"`
	AccountID        int64     `gorm:"uniqueIndex:uk_fund_position,priority:1;not null" json:"account_id"`
	InstrumentID     int64     `gorm:"uniqueIndex:uk_fund_position,priority:2;not null" json:"instrument_id"`
	DisplayName      string    `gorm:"size:128" json:"display_name"`
	ValuationBasis   string    `gorm:"size:32;not null" json:"valuation_basis"`
	BasisConfirmedBy int64     `gorm:"not null" json:"basis_confirmed_by"`
	BasisConfirmedAt time.Time `json:"basis_confirmed_at"`
	EventVersion     int64     `gorm:"not null;default:1" json:"event_version"`
	RebuildPending   bool      `gorm:"not null;default:true;index" json:"-"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type FundEvent struct {
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
	CorporateKey     *string         `gorm:"size:128;uniqueIndex:uk_fund_rights,priority:2" json:"corporate_key,omitempty"`
	RightsPositionID int64           `gorm:"uniqueIndex:uk_fund_rights,priority:1;not null" json:"-"`
	Note             string          `gorm:"size:255" json:"note"`
	CreatedBy        int64           `gorm:"not null" json:"created_by"`
	VoidedAt         *time.Time      `json:"voided_at,omitempty"`
	CreatedAt        time.Time       `json:"created_at"`
}

type FundMutation struct {
	ID          int64  `gorm:"primaryKey"`
	OwnerID     int64  `gorm:"uniqueIndex:uk_fund_mutation,priority:1;not null"`
	RequestKey  string `gorm:"size:128;uniqueIndex:uk_fund_mutation,priority:2;not null"`
	PayloadHash string `gorm:"size:64;not null"`
	Action      string `gorm:"size:32;not null"`
	PositionID  int64
	EventID     int64
	CreatedAt   time.Time
}

type FundNAV struct {
	ID           int64           `gorm:"primaryKey" json:"id"`
	InstrumentID int64           `gorm:"uniqueIndex:uk_fund_nav,priority:1;not null" json:"instrument_id"`
	NAVDate      time.Time       `gorm:"type:date;uniqueIndex:uk_fund_nav,priority:2;not null" json:"nav_date"`
	UnitNAV      decimal.Decimal `gorm:"type:decimal(20,8);not null" json:"unit_nav"`
	Revision     int64           `gorm:"not null;default:1" json:"revision"`
	FetchedAt    time.Time       `json:"fetched_at"`
}

type FundNAVRevision struct {
	ID           int64           `gorm:"primaryKey"`
	InstrumentID int64           `gorm:"index;not null"`
	NAVDate      time.Time       `gorm:"type:date;not null"`
	UnitNAV      decimal.Decimal `gorm:"type:decimal(20,8);not null"`
	Revision     int64           `gorm:"not null"`
	RecordedAt   time.Time
}

type FundValuation struct {
	ID                    int64            `gorm:"primaryKey" json:"id"`
	PositionID            int64            `gorm:"uniqueIndex:uk_fund_valuation,priority:1;not null" json:"position_id"`
	NAVDate               time.Time        `gorm:"type:date;uniqueIndex:uk_fund_valuation,priority:2;not null" json:"nav_date"`
	PreviousNAVDate       *time.Time       `gorm:"type:date" json:"previous_nav_date,omitempty"`
	EventVersion          int64            `gorm:"not null" json:"event_version"`
	QuoteRevision         int64            `gorm:"not null" json:"quote_revision"`
	PreviousQuoteRevision int64            `gorm:"not null" json:"previous_quote_revision"`
	Shares                decimal.Decimal  `gorm:"type:decimal(24,8);not null" json:"shares"`
	Cost                  decimal.Decimal  `gorm:"type:decimal(36,16);not null" json:"cost"`
	MarketValue           decimal.Decimal  `gorm:"type:decimal(12,2);not null" json:"market_value"`
	DailyPnL              *decimal.Decimal `gorm:"type:decimal(12,2)" json:"daily_pnl"`
	Status                string           `gorm:"size:32;not null" json:"status"`
	UpdatedAt             time.Time        `json:"updated_at"`
}

type FundAudit struct {
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

type FundDividend struct {
	ID               int64           `gorm:"primaryKey" json:"id"`
	InstrumentID     int64           `gorm:"uniqueIndex:uk_fund_dividend,priority:1;not null" json:"instrument_id"`
	SourceKey        string          `gorm:"size:128;uniqueIndex:uk_fund_dividend,priority:2;not null" json:"source_key"`
	RegistrationDate time.Time       `gorm:"type:date;not null" json:"registration_date"`
	ExDate           time.Time       `gorm:"type:date;not null" json:"ex_date"`
	PaymentDate      *time.Time      `gorm:"type:date" json:"payment_date,omitempty"`
	PerTenCash       decimal.Decimal `gorm:"type:decimal(20,8);not null" json:"per_ten_cash"`
}

package model

import (
	"github.com/shopspring/decimal"
	"time"
)

type GoldPosition struct {
	ID             int64     `gorm:"primaryKey" json:"id"`
	OwnerID        int64     `gorm:"index;not null" json:"owner_id"`
	AccountID      int64     `gorm:"uniqueIndex;not null" json:"account_id"`
	ProductKey     string    `gorm:"size:64;not null" json:"product_key"`
	EventVersion   int64     `gorm:"not null;default:1" json:"event_version"`
	RebuildPending bool      `gorm:"not null;default:true;index" json:"rebuild_pending"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}
type GoldEvent struct {
	ID            int64            `gorm:"primaryKey" json:"id"`
	OwnerID       int64            `gorm:"index;not null" json:"owner_id"`
	AccountID     int64            `gorm:"index;not null" json:"account_id"`
	Kind          string           `gorm:"size:16;not null" json:"kind"`
	Grams         decimal.Decimal  `gorm:"type:decimal(24,8);not null" json:"grams"`
	CashAmount    decimal.Decimal  `gorm:"type:decimal(36,16);not null" json:"cash_amount"`
	Fee           *decimal.Decimal `gorm:"type:decimal(12,2)" json:"fee"`
	TradeDate     time.Time        `gorm:"type:date;not null;index" json:"trade_date"`
	CashAccountID *int64           `gorm:"index" json:"cash_account_id,omitempty"`
	Note          string           `gorm:"size:255" json:"note"`
	CreatedBy     int64            `gorm:"not null" json:"created_by"`
	VoidedAt      *time.Time       `json:"voided_at,omitempty"`
	CreatedAt     time.Time        `json:"created_at"`
}
type GoldMutation struct {
	ID          int64  `gorm:"primaryKey"`
	OwnerID     int64  `gorm:"uniqueIndex:uk_gold_mutation,priority:1;not null"`
	RequestKey  string `gorm:"size:128;uniqueIndex:uk_gold_mutation,priority:2;not null"`
	PayloadHash string `gorm:"size:64;not null"`
	AccountID   int64
	EventID     int64
	CreatedAt   time.Time
}
type GoldAudit struct {
	ID         int64     `gorm:"primaryKey" json:"id"`
	OwnerID    int64     `gorm:"index;not null" json:"owner_id"`
	AccountID  int64     `gorm:"index;not null" json:"account_id"`
	EventID    int64     `json:"event_id"`
	ActorID    int64     `json:"actor_id"`
	Action     string    `gorm:"size:32" json:"action"`
	BeforeJSON string    `gorm:"type:longtext" json:"before"`
	AfterJSON  string    `gorm:"type:longtext" json:"after"`
	CreatedAt  time.Time `json:"created_at"`
}
type GoldQuoteObservation struct {
	ID         int64           `gorm:"primaryKey" json:"id"`
	ProductKey string          `gorm:"size:64;index;not null" json:"product_key"`
	BuyPrice   decimal.Decimal `gorm:"type:decimal(20,8);not null" json:"buy_price"`
	SellPrice  decimal.Decimal `gorm:"type:decimal(20,8);not null" json:"sell_price"`
	QuoteAt    time.Time       `gorm:"precision:3;not null" json:"quote_at"`
	FetchedAt  time.Time       `gorm:"precision:3;index;not null" json:"fetched_at"`
	AttemptKey string          `gorm:"size:64;uniqueIndex;not null" json:"-"`
	RevisionOf *int64          `json:"revision_of,omitempty"`
}
type GoldQuoteSync struct {
	ProductKey      string `gorm:"size:64;primaryKey"`
	ObservationID   *int64
	LastAttemptAt   *time.Time
	LastSuccessAt   *time.Time
	FirstAttemptAt  *time.Time
	ScheduledSlot   string `gorm:"size:16"`
	LeaseToken      string `gorm:"size:64"`
	LeaseUntil      *time.Time
	NextRefreshAt   *time.Time
	LastError       string `gorm:"size:255"`
	SourceUnchanged bool   `gorm:"not null;default:false"`
}
type GoldValuation struct {
	ID            int64            `gorm:"primaryKey" json:"id"`
	AccountID     int64            `gorm:"uniqueIndex:uk_gold_valuation,priority:1;not null" json:"account_id"`
	Date          time.Time        `gorm:"type:date;uniqueIndex:uk_gold_valuation,priority:2;not null" json:"date"`
	EventVersion  int64            `json:"event_version"`
	ObservationID *int64           `json:"observation_id"`
	QuoteAt       *time.Time       `gorm:"-" json:"quote_at"`
	FetchedAt     *time.Time       `gorm:"-" json:"fetched_at"`
	Grams         decimal.Decimal  `gorm:"type:decimal(24,8)" json:"grams"`
	Cost          decimal.Decimal  `gorm:"type:decimal(36,16)" json:"cost"`
	RealizedPnL   decimal.Decimal  `gorm:"type:decimal(36,16)" json:"realized_pnl"`
	MarketValue   *decimal.Decimal `gorm:"type:decimal(12,2)" json:"market_value"`
	Status        string           `gorm:"size:32" json:"status"`
}

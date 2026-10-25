package model

// TransactionDisplayOrder is a personal view preference, separate from the journal.
type TransactionDisplayOrder struct {
	OwnerID       int64 `gorm:"primaryKey;autoIncrement:false"`
	TransactionID int64 `gorm:"primaryKey;autoIncrement:false"`
	Position      int64
}

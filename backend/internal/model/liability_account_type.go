package model

// LiabilityAccountType is the database catalog used for validation and display.
type LiabilityAccountType struct {
	Code           string `gorm:"primaryKey;size:32" json:"code"`
	Name           string `gorm:"size:64;not null" json:"name"`
	Sort           int    `json:"sort"`
	DefaultAccount bool   `json:"default_account"`
}

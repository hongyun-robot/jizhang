package model

import "time"

// CatalogOrderRequest remembers successful saves so retries cannot replay an old move.
type CatalogOrderRequest struct {
	ID         int64  `gorm:"primaryKey"`
	OwnerID    int64  `gorm:"uniqueIndex:uk_catalog_order_request,priority:1"`
	Scope      string `gorm:"size:64;uniqueIndex:uk_catalog_order_request,priority:2"`
	RequestKey string `gorm:"size:128;uniqueIndex:uk_catalog_order_request,priority:3"`
	Digest     string `gorm:"size:64"`
	CreatedAt  time.Time
}

package model

import "time"

// ClosingStatus 封账状态（当前只有 closed；reopen = 删除记录）。
type ClosingStatus string

const (
	ClosingClosed ClosingStatus = "closed"
)

// PeriodClosing 账期封账记录：period_key 存在即已封（硬锁）。
type PeriodClosing struct {
	ID        int64         `gorm:"primaryKey" json:"id"`
	PeriodKey string        `gorm:"type:char(7);uniqueIndex" json:"period_key"`
	Status    ClosingStatus `json:"status"`
	ClosedBy  int64         `json:"closed_by"`
	ClosedAt  time.Time     `json:"closed_at"`
}

func (PeriodClosing) TableName() string { return "period_closings" }

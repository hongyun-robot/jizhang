package model

import "time"

// 站内提醒类型（schema ENUM，migrations/000001_init；任务 8.2 先落预算超支）。
const (
	NotifBudgetOverdue    = "budget_overdue"
	NotifRepaymentDue     = "repayment_due"
	NotifRecurringPatched = "recurring_patched"
)

// Notification 站内提醒：家庭共享账本为全局一条，read_at 为空即未读。
// Payload 存 JSON 字符串，内含 dedup_key（如 budget_overdue:2026-10:cat:1），
// 「同账期同预算不重复提醒」在 service 层按它去重。
type Notification struct {
	OwnerID     *int64     `json:"-"`
	ID          int64      `gorm:"primaryKey" json:"id"`
	Kind        string     `gorm:"type:varchar(32)" json:"kind"`
	Payload     string     `gorm:"type:varchar(1000)" json:"payload"`
	ReadAt      *time.Time `json:"read_at"`
	StatementID *int64     `json:"statement_id"`
	DedupKey    *string    `gorm:"size:128;uniqueIndex" json:"dedup_key"`
	ResolvedAt  *time.Time `json:"resolved_at"`
	CreatedAt   time.Time  `json:"created_at"`
}

func (Notification) TableName() string { return "notifications" }

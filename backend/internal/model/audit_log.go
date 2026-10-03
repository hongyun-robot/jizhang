package model

import "time"

// AuditLog 审计日志：封账/解封等动作留痕（specs/billing-periods「解封与审计日志」）。
// Detail 存操作时快照（period_key、username），后续改名不影响历史可读性。
type AuditLog struct {
	ID        int64     `gorm:"primaryKey" json:"id"`
	UserID    int64     `json:"user_id"`
	Action    string    `gorm:"type:varchar(32)" json:"action"`
	Detail    string    `gorm:"type:varchar(255)" json:"detail"`
	CreatedAt time.Time `json:"created_at"`
}

func (AuditLog) TableName() string { return "audit_logs" }

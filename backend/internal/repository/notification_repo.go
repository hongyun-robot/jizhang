package repository

import (
	"errors"
	"time"

	"gorm.io/gorm"

	"jizhang/internal/model"
)

// NotificationRepo notifications 表数据访问。
type NotificationRepo struct {
	db      *gorm.DB
	ownerID int64
}

func (r *NotificationRepo) ForOwner(id int64) *NotificationRepo {
	return &NotificationRepo{db: r.db, ownerID: id}
}
func (r *NotificationRepo) visible(q *gorm.DB) *gorm.DB {
	if r.ownerID > 0 {
		return q.Where("kind <> ? OR owner_id = ?", model.NotifRepaymentDue, r.ownerID)
	}
	return q
}

func NewNotificationRepo(db *gorm.DB) *NotificationRepo { return &NotificationRepo{db: db} }

// List 按类型取提醒（kind 为空取全部），新的在前。
// 去重键在 payload JSON 里，service 层解析比对（表小；避免 SQL JSON 函数的方言差异）。
func (r *NotificationRepo) List(kind string) ([]model.Notification, error) {
	q := r.visible(r.db).Order("id DESC")
	if kind != "" {
		q = q.Where("kind = ?", kind)
	}
	var list []model.Notification
	err := q.Find(&list).Error
	return list, err
}

// Create 写入提醒。
func (r *NotificationRepo) Create(n *model.Notification) error {
	return r.db.Create(n).Error
}

// GetByID 按 ID 查询（不存在返回 nil, nil）。
func (r *NotificationRepo) GetByID(id int64) (*model.Notification, error) {
	var n model.Notification
	err := r.visible(r.db).First(&n, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &n, err
}

// UnreadCount 未读条数（铃铛红点数字，任务 10.2）。
func (r *NotificationRepo) UnreadCount() (int64, error) {
	var n int64
	err := r.visible(r.db.Model(&model.Notification{})).Where("read_at IS NULL AND resolved_at IS NULL").Count(&n).Error
	return n, err
}

// MarkRead 单条标为已读（幂等：已读的不再更新）。
func (r *NotificationRepo) MarkRead(id int64) error {
	return r.visible(r.db.Model(&model.Notification{})).
		Where("id = ? AND read_at IS NULL", id).
		Update("read_at", time.Now()).Error
}

// MarkAllRead 全部标为已读，返回本次更新条数。
func (r *NotificationRepo) MarkAllRead() (int64, error) {
	res := r.visible(r.db.Model(&model.Notification{})).
		Where("read_at IS NULL AND resolved_at IS NULL").
		Update("read_at", time.Now())
	return res.RowsAffected, res.Error
}

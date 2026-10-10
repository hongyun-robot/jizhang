package service

import (
	"errors"
	"gorm.io/gorm"
	"jizhang/internal/liability"
	"jizhang/internal/model"
	"jizhang/internal/repository"
	"time"
)

var ErrNotifNotFound = errors.New("提醒不存在")

const repaymentLeadDays = 3

type NotifDeps struct {
	Notifs   *repository.NotificationRepo
	Accounts *repository.AccountRepo
}
type NotificationService struct{ deps NotifDeps }

func NewNotificationService(deps NotifDeps) *NotificationService {
	return &NotificationService{deps: deps}
}

type RepaymentNotifyPayload struct {
	DedupKey      string `json:"dedup_key"`
	AccountID     int64  `json:"account_id"`
	AccountName   string `json:"account_name"`
	StatementID   int64  `json:"statement_id"`
	Debt          string `json:"debt"`
	InitialDebt   string `json:"initial_debt"`
	RepaymentDate string `json:"repayment_date"`
	Days          int    `json:"days"`
	Overdue       bool   `json:"overdue"`
	Resolved      bool   `json:"resolved"`
}

func (s *NotificationService) EvaluateRepayment(now time.Time) error {
	db := s.deps.Accounts.DB()
	if !db.Migrator().HasTable(&model.LiabilityStatement{}) {
		return nil
	}
	return repository.LedgerWrite(db, func(tx *gorm.DB) error { return liability.RefreshReminders(tx, now, true) })
}
func (s *NotificationService) List() ([]model.Notification, int64, error) {
	list, e := s.deps.Notifs.List("")
	if e != nil {
		return nil, 0, e
	}
	n, e := s.deps.Notifs.UnreadCount()
	return list, n, e
}
func (s *NotificationService) ForUser(actor *model.User) *NotificationService {
	return &NotificationService{deps: NotifDeps{Notifs: s.deps.Notifs.ForOwner(actor.ID), Accounts: s.deps.Accounts}}
}
func (s *NotificationService) MarkRead(id int64) (int64, error) {
	n, e := s.deps.Notifs.GetByID(id)
	if e != nil {
		return 0, e
	}
	if n == nil {
		return 0, ErrNotifNotFound
	}
	if e := s.deps.Notifs.MarkRead(id); e != nil {
		return 0, e
	}
	return s.deps.Notifs.UnreadCount()
}
func (s *NotificationService) MarkAllRead() (int64, error) {
	if _, e := s.deps.Notifs.MarkAllRead(); e != nil {
		return 0, e
	}
	return s.deps.Notifs.UnreadCount()
}

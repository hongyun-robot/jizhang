package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"jizhang/internal/model"
	"jizhang/internal/repository"
)

// 提醒业务错误（任务 10.2）。
var ErrNotifNotFound = errors.New("提醒不存在")

// 还款提醒的提前天数（任务 10.1「还款日提前 3 天提醒」：含还款日当天共 4 个触发日）。
const repaymentLeadDays = 3

// NotifDeps 提醒服务依赖。
type NotifDeps struct {
	Notifs   *repository.NotificationRepo
	Accounts *repository.AccountRepo
}

// NotificationService 站内提醒：还款日评估（10.1）+ 读写接口（10.2）。
// 预算超支提醒在 BudgetService.Evaluate（8.2）、周期补齐提醒在 RecurringService.EnsureDueLazy（9.3/10.1）。
type NotificationService struct {
	deps NotifDeps
}

func NewNotificationService(deps NotifDeps) *NotificationService {
	return &NotificationService{deps: deps}
}

// RepaymentNotifyPayload 临近还款日提醒内容（spec：账户名、欠款金额与还款日）。
type RepaymentNotifyPayload struct {
	DedupKey      string `json:"dedup_key"`
	AccountID     int64  `json:"account_id"`
	AccountName   string `json:"account_name"`
	Debt          string `json:"debt"`
	RepaymentDate string `json:"repayment_date"`
	Days          int    `json:"days"`    // 距还款日天数（已过为负）
	Overdue       bool   `json:"overdue"` // 已过还款日仍未还清
}

// EvaluateRepayment 评估信用卡还款日（任务 10.1，设计 D7 每日 08:00 cron）：
// 负债账户配置了还款日且余额为负（有欠款）才提醒——
//   - 距本月还款日 <= repaymentLeadDays 天；
//   - 或已过本月还款日仍未还清（逾期同样需要核对，按月去重不骚扰）。
//
// 同账户同月只提醒一次（payload.dedup_key = repayment_due:{id}:{YYYY-MM}），重复评估不重复。
func (s *NotificationService) EvaluateRepayment(now time.Time) error {
	accounts, err := s.deps.Accounts.List()
	if err != nil {
		return err
	}
	notifs, err := s.deps.Notifs.List(model.NotifRepaymentDue)
	if err != nil {
		return err
	}
	existing := make(map[string]bool, len(notifs))
	for _, n := range notifs {
		var p RepaymentNotifyPayload
		if json.Unmarshal([]byte(n.Payload), &p) == nil && p.DedupKey != "" {
			existing[p.DedupKey] = true
		}
	}

	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	for i := range accounts {
		a := &accounts[i]
		if a.Type != model.AccountLiability || a.RepaymentDay == nil ||
			a.Status != model.StatusActive || !a.Balance.IsNegative() {
			continue // 只看有欠款的信用卡（无欠款不提醒）
		}
		due := time.Date(today.Year(), today.Month(), *a.RepaymentDay, 0, 0, 0, 0, today.Location())
		days := int(due.Sub(today).Hours() / 24)
		overdue := days < 0
		if !overdue && days > repaymentLeadDays {
			continue // 未进入提前提醒窗口
		}
		dedupKey := fmt.Sprintf("repayment_due:%d:%s", a.ID, due.Format("2006-01"))
		if existing[dedupKey] {
			continue
		}
		payload, err := json.Marshal(RepaymentNotifyPayload{
			DedupKey:      dedupKey,
			AccountID:     a.ID,
			AccountName:   a.Name,
			Debt:          a.Balance.Neg().StringFixed(2),
			RepaymentDate: due.Format(dayFmt),
			Days:          days,
			Overdue:       overdue,
		})
		if err != nil {
			return err
		}
		if err := s.deps.Notifs.Create(&model.Notification{
			Kind:    model.NotifRepaymentDue,
			Payload: string(payload),
		}); err != nil {
			return err
		}
		existing[dedupKey] = true
	}
	return nil
}

// List 全部提醒（新的在前）+ 未读数（任务 10.2，铃铛与列表共用一次请求）。
func (s *NotificationService) List() ([]model.Notification, int64, error) {
	list, err := s.deps.Notifs.List("")
	if err != nil {
		return nil, 0, err
	}
	unread, err := s.deps.Notifs.UnreadCount()
	if err != nil {
		return nil, 0, err
	}
	return list, unread, nil
}

// MarkRead 单条标为已读；返回最新未读数。
func (s *NotificationService) MarkRead(id int64) (int64, error) {
	n, err := s.deps.Notifs.GetByID(id)
	if err != nil {
		return 0, err
	}
	if n == nil {
		return 0, ErrNotifNotFound
	}
	if err := s.deps.Notifs.MarkRead(id); err != nil {
		return 0, err
	}
	return s.deps.Notifs.UnreadCount()
}

// MarkAllRead 全部标为已读（红点归零，已读仍在列表可回看）。
func (s *NotificationService) MarkAllRead() (int64, error) {
	if _, err := s.deps.Notifs.MarkAllRead(); err != nil {
		return 0, err
	}
	return s.deps.Notifs.UnreadCount()
}

package service

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/shopspring/decimal"

	"jizhang/internal/model"
	"jizhang/internal/repository"
)

// 封账业务错误（specs/billing-periods 任务 7.2）。
var (
	ErrClosingInvalidPeriod = errors.New("账期格式无效")
	ErrClosingNotEnded      = errors.New("该账期尚未结束，无法封账")
	ErrClosingAlreadyClosed = errors.New("该账期已封账")
	ErrClosingNotClosed     = errors.New("该账期未封账")
)

// 审计动作（封账与解封必留痕）。
const (
	AuditActionClose  = "period.close"
	AuditActionReopen = "period.reopen"
)

// ClosingDeps 封账服务依赖。
type ClosingDeps struct {
	Closings *repository.ClosingRepo
	Audits   *repository.AuditRepo
	Periods  *PeriodService
	Stats    *repository.StatsRepo
}

// ClosingService 账期封账/解封 + 审计日志。
type ClosingService struct {
	deps ClosingDeps
	now  func() time.Time // 可注入（测试）
}

func NewClosingService(d ClosingDeps) *ClosingService {
	return &ClosingService{deps: d, now: time.Now}
}

// ClosingPreview 封账确认展示：区间 + 该期收支结余。
// 合计按物化 period_key 汇总（与封账锁同口径），区间按当前结算日换算展示。
type ClosingPreview struct {
	PeriodKey string          `json:"period_key"`
	StartDate string          `json:"start_date"`
	EndDate   string          `json:"end_date"`
	Closed    bool            `json:"closed"`
	Ended     bool            `json:"ended"`
	Income    decimal.Decimal `json:"income"`
	Expense   decimal.Decimal `json:"expense"`
	Balance   decimal.Decimal `json:"balance"`
}

// Preview 某账期的封账预览（全部角色可读，确认弹窗数据源）。
func (s *ClosingService) Preview(periodKey string) (*ClosingPreview, error) {
	start, end, ok := s.deps.Periods.Range(periodKey)
	if !ok {
		return nil, ErrClosingInvalidPeriod
	}
	sum, err := s.deps.Stats.SumByPeriod(periodKey)
	if err != nil {
		return nil, err
	}
	closed, err := s.deps.Closings.IsClosed(periodKey)
	if err != nil {
		return nil, err
	}
	return &ClosingPreview{
		PeriodKey: periodKey,
		StartDate: start.Format("2006-01-02"),
		EndDate:   end.AddDate(0, 0, -1).Format("2006-01-02"),
		Closed:    closed,
		Ended:     !end.After(s.now()),
		Income:    sum.Income,
		Expense:   sum.Expense,
		Balance:   sum.Income.Sub(sum.Expense),
	}, nil
}

// Close 封账（仅管理员，路由再加中间件门禁）：仅已结束账期；写封账记录 + 审计留痕。
func (s *ClosingService) Close(actor *model.User, periodKey string) (*model.PeriodClosing, error) {
	_, end, ok := s.deps.Periods.Range(periodKey)
	if !ok {
		return nil, ErrClosingInvalidPeriod
	}
	if end.After(s.now()) {
		return nil, ErrClosingNotEnded
	}
	closed, err := s.deps.Closings.IsClosed(periodKey)
	if err != nil {
		return nil, err
	}
	if closed {
		return nil, ErrClosingAlreadyClosed
	}
	rec := &model.PeriodClosing{
		PeriodKey: periodKey,
		Status:    model.ClosingClosed,
		ClosedBy:  actor.ID,
		ClosedAt:  s.now(),
	}
	if err := s.deps.Closings.Close(rec); err != nil {
		return nil, err
	}
	if err := s.audit(actor, AuditActionClose, periodKey); err != nil {
		_ = s.deps.Closings.Reopen(periodKey) // 审计失败补偿：回滚封账，保证留痕
		return nil, err
	}
	return rec, nil
}

// Reopen 解封（仅管理员）：删除封账记录 + 审计留痕。
func (s *ClosingService) Reopen(actor *model.User, periodKey string) error {
	if _, _, ok := s.deps.Periods.Range(periodKey); !ok {
		return ErrClosingInvalidPeriod
	}
	closed, err := s.deps.Closings.IsClosed(periodKey)
	if err != nil {
		return err
	}
	if !closed {
		return ErrClosingNotClosed
	}
	if err := s.deps.Closings.Reopen(periodKey); err != nil {
		return err
	}
	if err := s.audit(actor, AuditActionReopen, periodKey); err != nil {
		_ = s.deps.Closings.Close(&model.PeriodClosing{
			PeriodKey: periodKey, Status: model.ClosingClosed, ClosedBy: actor.ID, ClosedAt: s.now(),
		})
		return err
	}
	return nil
}

// List 已封账账期列表（设置页状态展示）。
func (s *ClosingService) List() ([]model.PeriodClosing, error) {
	return s.deps.Closings.List()
}

// AuditItem 审计日志行（detail 快照解析后）。
type AuditItem struct {
	ID        int64     `json:"id"`
	UserID    int64     `json:"user_id"`
	Username  string    `json:"username"`
	Action    string    `json:"action"`
	PeriodKey string    `json:"period_key"`
	CreatedAt time.Time `json:"created_at"`
}

// AuditLogs 分页审计日志（管理员查看）。
func (s *ClosingService) AuditLogs(page, pageSize int) ([]AuditItem, int64, error) {
	logs, total, err := s.deps.Audits.List(page, pageSize)
	if err != nil {
		return nil, 0, err
	}
	items := make([]AuditItem, 0, len(logs))
	for _, l := range logs {
		var d struct {
			PeriodKey string `json:"period_key"`
			Username  string `json:"username"`
		}
		_ = json.Unmarshal([]byte(l.Detail), &d)
		items = append(items, AuditItem{
			ID:        l.ID,
			UserID:    l.UserID,
			Username:  d.Username,
			Action:    l.Action,
			PeriodKey: d.PeriodKey,
			CreatedAt: l.CreatedAt,
		})
	}
	return items, total, nil
}

// audit 写审计日志（detail 含账期与操作人名快照）。
func (s *ClosingService) audit(actor *model.User, action, periodKey string) error {
	detail, _ := json.Marshal(map[string]string{
		"period_key": periodKey,
		"username":   actor.Username,
	})
	return s.deps.Audits.Create(&model.AuditLog{
		UserID: actor.ID,
		Action: action,
		Detail: string(detail),
	})
}

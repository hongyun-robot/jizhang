package service

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"jizhang/internal/model"
	"jizhang/internal/repository"
)

// notifEnv 提醒测试环境（sqlite 内存库跑真实仓储；账户余额由 opening_balance 推导）。
type notifEnv struct {
	t       *testing.T
	db      *gorm.DB
	svc     *NotificationService
	accRepo *repository.AccountRepo
	notifs  *repository.NotificationRepo
}

func newNotifEnv(t *testing.T) *notifEnv {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	// AccountRepo.List 会聚合 transactions，即使无流水也要建表
	if err := db.AutoMigrate(&model.Account{}, &model.Transaction{}, &model.Notification{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	accRepo := repository.NewAccountRepo(db)
	notifs := repository.NewNotificationRepo(db)
	return &notifEnv{
		t:       t,
		db:      db,
		svc:     NewNotificationService(NotifDeps{Notifs: notifs, Accounts: accRepo}),
		accRepo: accRepo,
		notifs:  notifs,
	}
}

// creditCard 建一张信用卡（还款日 + 期初余额，负数即欠款）。
func (e *notifEnv) creditCard(name string, opening string, day int, status model.UserStatus) *model.Account {
	e.t.Helper()
	a := &model.Account{
		Name:           name,
		Type:           model.AccountLiability,
		OpeningBalance: dec(opening),
		Status:         status,
		RepaymentDay:   &day,
	}
	mustOK(e.t, e.accRepo.Create(a))
	return a
}

// repaymentPayload 取最新一条还款提醒并解码。
func (e *notifEnv) repaymentPayload() RepaymentNotifyPayload {
	e.t.Helper()
	list, err := e.notifs.List(model.NotifRepaymentDue)
	mustOK(e.t, err)
	if len(list) != 1 {
		e.t.Fatalf("还款提醒条数 = %d，want 1", len(list))
	}
	var p RepaymentNotifyPayload
	if err := json.Unmarshal([]byte(list[0].Payload), &p); err != nil {
		e.t.Fatalf("解码 payload: %v", err)
	}
	return p
}

// 任务 10.1 核心约束：无欠款不提醒（配置了还款日但已还清/从未欠款）。
func TestRepaymentNoDebtNoReminder(t *testing.T) {
	env := newNotifEnv(t)
	env.creditCard("已还清", "0", 10, model.StatusActive)
	env.creditCard("有溢额", "300", 10, model.StatusActive)
	// 负债但未配置还款日 → 不提醒
	noDay := &model.Account{Name: "白条", Type: model.AccountLiability,
		OpeningBalance: dec("-200"), Status: model.StatusActive}
	mustOK(t, env.accRepo.Create(noDay))

	// 落在提醒窗口内（还款日 10 号，评估日 8 号）
	mustOK(t, env.svc.EvaluateRepayment(mustDate(t, "2026-10-08")))
	list, err := env.notifs.List("")
	mustOK(t, err)
	if len(list) != 0 {
		t.Errorf("无欠款生成了 %d 条提醒，want 0", len(list))
	}
}

// 距还款日 <= 3 天且有欠款 → 生成一条，含账户名、欠款金额与还款日；重复评估按月去重。
func TestRepaymentWithinLeadDays(t *testing.T) {
	env := newNotifEnv(t)
	cc := env.creditCard("招行信用卡", "-500", 10, model.StatusActive)

	mustOK(t, env.svc.EvaluateRepayment(mustDate(t, "2026-10-08")))
	p := env.repaymentPayload()
	if p.AccountName != "招行信用卡" || p.AccountID != cc.ID {
		t.Errorf("账户 = %s/%d，want 招行信用卡/%d", p.AccountName, p.AccountID, cc.ID)
	}
	if p.Debt != "500.00" {
		t.Errorf("欠款 = %s，want 500.00", p.Debt)
	}
	if p.RepaymentDate != "2026-10-10" {
		t.Errorf("还款日 = %s，want 2026-10-10", p.RepaymentDate)
	}
	if p.Days != 2 || p.Overdue {
		t.Errorf("Days/Overdue = %d/%v，want 2/false", p.Days, p.Overdue)
	}

	// 次日再评估：同账户同月去重，仍 1 条
	mustOK(t, env.svc.EvaluateRepayment(mustDate(t, "2026-10-09")))
	list, err := env.notifs.List(model.NotifRepaymentDue)
	mustOK(t, err)
	if len(list) != 1 {
		t.Errorf("重复评估后条数 = %d，want 1", len(list))
	}
}

// 未进入窗口不提醒；提前 3 天当天恰好触发。
func TestRepaymentOutsideWindowNoReminder(t *testing.T) {
	env := newNotifEnv(t)
	env.creditCard("信用卡", "-800", 10, model.StatusActive)

	mustOK(t, env.svc.EvaluateRepayment(mustDate(t, "2026-10-01"))) // 距 9 天
	list, err := env.notifs.List("")
	mustOK(t, err)
	if len(list) != 0 {
		t.Errorf("窗口外生成了 %d 条提醒，want 0", len(list))
	}

	mustOK(t, env.svc.EvaluateRepayment(mustDate(t, "2026-10-07"))) // 恰好提前 3 天
	p := env.repaymentPayload()
	if p.Days != 3 {
		t.Errorf("提前 3 天触发的 Days = %d，want 3", p.Days)
	}
}

// 已过还款日仍未还清 → 逾期提醒（Overdue=true）；下月窗口是新的去重键，可再提醒。
func TestRepaymentOverdueReminder(t *testing.T) {
	env := newNotifEnv(t)
	env.creditCard("信用卡", "-500", 10, model.StatusActive)

	mustOK(t, env.svc.EvaluateRepayment(mustDate(t, "2026-10-12")))
	p := env.repaymentPayload()
	if !p.Overdue || p.Days != -2 {
		t.Errorf("Overdue/Days = %v/%d，want true/-2", p.Overdue, p.Days)
	}
	if p.RepaymentDate != "2026-10-10" {
		t.Errorf("还款日 = %s，want 2026-10-10", p.RepaymentDate)
	}

	// 同月重复评估仍 1 条；下月还款窗口生成新一条
	mustOK(t, env.svc.EvaluateRepayment(mustDate(t, "2026-10-15")))
	mustOK(t, env.svc.EvaluateRepayment(mustDate(t, "2026-11-08")))
	list, err := env.notifs.List(model.NotifRepaymentDue)
	mustOK(t, err)
	if len(list) != 2 {
		t.Errorf("跨月后条数 = %d，want 2", len(list))
	}
}

// 已停用的信用卡与资产账户不参与还款提醒。
func TestRepaymentSkipsDisabledAndAsset(t *testing.T) {
	env := newNotifEnv(t)
	env.creditCard("停用信用卡", "-999", 10, model.StatusDisabled)
	asset := &model.Account{Name: "现金", Type: model.AccountAsset,
		OpeningBalance: dec("-100"), Status: model.StatusActive}
	mustOK(t, env.accRepo.Create(asset))

	mustOK(t, env.svc.EvaluateRepayment(mustDate(t, "2026-10-08")))
	list, err := env.notifs.List("")
	mustOK(t, err)
	if len(list) != 0 {
		t.Errorf("生成了 %d 条提醒，want 0", len(list))
	}
}

// 单条已读幂等、未读不存在报错、全部已读后列表仍可回看（任务 10.2 / specs/notifications）。
func TestNotificationReadFlow(t *testing.T) {
	env := newNotifEnv(t)
	mustOK(t, env.notifs.Create(&model.Notification{Kind: model.NotifBudgetOverdue, Payload: `{"dedup_key":"budget_overdue:2026-10:total"}`}))
	mustOK(t, env.notifs.Create(&model.Notification{Kind: model.NotifRepaymentDue, Payload: `{"dedup_key":"repayment_due:1:2026-10"}`}))

	list, unread, err := env.svc.List()
	mustOK(t, err)
	if len(list) != 2 || unread != 2 {
		t.Fatalf("list/unread = %d/%d，want 2/2", len(list), unread)
	}
	if list[0].ID < list[1].ID {
		t.Error("列表未按新→旧排序")
	}

	// 单条已读：未读 2→1，已读行仍在列表
	unread, err = env.svc.MarkRead(list[0].ID)
	mustOK(t, err)
	if unread != 1 {
		t.Errorf("单条已读后 unread = %d，want 1", unread)
	}
	again, err := env.svc.MarkRead(list[0].ID) // 幂等
	mustOK(t, err)
	if again != 1 {
		t.Errorf("重复已读后 unread = %d，want 1", again)
	}
	rows, unread, err := env.svc.List()
	mustOK(t, err)
	if len(rows) != 2 || unread != 1 {
		t.Errorf("已读行被移出列表或未读数错：len=%d unread=%d", len(rows), unread)
	}

	// 不存在的 ID
	_, err = env.svc.MarkRead(9999)
	if !errors.Is(err, ErrNotifNotFound) {
		t.Errorf("不存在的提醒 err = %v，want ErrNotifNotFound", err)
	}

	// 全部已读：红点归零，历史仍可回看
	unread, err = env.svc.MarkAllRead()
	mustOK(t, err)
	if unread != 0 {
		t.Errorf("全部已读后 unread = %d，want 0", unread)
	}
	rows, unread, err = env.svc.List()
	mustOK(t, err)
	if len(rows) != 2 || unread != 0 {
		t.Errorf("全部已读后 len=%d unread=%d，want 2/0", len(rows), unread)
	}
	for _, n := range rows {
		if n.ReadAt == nil {
			t.Error("标记已读后仍有未读行")
			break
		}
	}
}

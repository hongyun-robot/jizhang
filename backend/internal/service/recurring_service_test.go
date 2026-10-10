package service

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"jizhang/internal/model"
	"jizhang/internal/repository"
)

// recurringEnv 周期记账测试环境（sqlite 内存库跑真实仓储 + 真实生成链路）。
type recurringEnv struct {
	t        *testing.T
	db       *gorm.DB
	rules    *RecurringService
	ruleRepo *repository.RecurringRepo
	txnRepo  *repository.TransactionRepo
	accRepo  *repository.AccountRepo
	catRepo  *repository.CategoryRepo
	notifs   *repository.NotificationRepo
	userRepo *repository.UserRepo
}

func newRecurringEnv(t *testing.T) *recurringEnv {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.Setting{}, &model.Account{}, &model.Category{}, &model.Transaction{},
		&model.PeriodClosing{}, &model.RecurringRule{}, &model.Notification{}, &model.User{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	periods := NewPeriodService(&memSettings{m: map[string]string{"settlement_day": "1"}})
	accRepo := repository.NewAccountRepo(db)
	catRepo := repository.NewCategoryRepo(db)
	txnRepo := repository.NewTransactionRepo(db)
	ruleRepo := repository.NewRecurringRepo(db)
	notifs := repository.NewNotificationRepo(db)
	userRepo := repository.NewUserRepo(db)
	txn := NewTransactionService(TxnDeps{
		Txns:     txnRepo,
		Accounts: accRepo,
		Cats:     catRepo,
		Closings: repository.NewClosingRepo(db),
		Periods:  periods,
	})
	rules := NewRecurringService(RuleDeps{
		Rules:    ruleRepo,
		Dates:    txnRepo,
		Txns:     txn,
		Accounts: accRepo,
		Cats:     catRepo,
		Notifs:   notifs,
		Users:    userRepo,
	})
	return &recurringEnv{t: t, db: db, rules: rules, ruleRepo: ruleRepo,
		txnRepo: txnRepo, accRepo: accRepo, catRepo: catRepo, notifs: notifs, userRepo: userRepo}
}

// seed 启用账户 + 一级/末级支出分类（餐饮{午餐}）。
func (e *recurringEnv) seed() (model.Account, int64, int64) {
	e.t.Helper()
	acc := model.Account{Name: "银行卡", Type: model.AccountAsset, Status: model.StatusActive}
	mustOK(e.t, e.accRepo.Create(&acc))
	root := model.Category{Kind: model.KindExpense, Name: "餐饮"}
	mustOK(e.t, e.catRepo.Create(&root))
	leaf := model.Category{Kind: model.KindExpense, Name: "午餐", ParentID: &root.ID}
	mustOK(e.t, e.catRepo.Create(&leaf))
	return acc, root.ID, leaf.ID
}

// newRule 创建一个默认模板（每月 amount，首次 start）。
func (e *recurringEnv) newRule(amount, start string) *model.RecurringRule {
	e.t.Helper()
	acc, _, leaf := e.seed()
	r, err := e.rules.Create(adminUser(), RuleInput{
		Amount:     dec(amount),
		Freq:       model.FreqMonthly,
		AccountID:  acc.ID,
		CategoryID: leaf,
		StartDate:  mustDate(e.t, start),
	})
	mustOK(e.t, err)
	return r
}

// ruleTxns 该模板生成的全部流水（按归属日升序）。
func (e *recurringEnv) ruleTxns(ruleID int64) []model.Transaction {
	e.t.Helper()
	var ts []model.Transaction
	mustOK(e.t, e.db.Where("rule_id = ?", ruleID).Order("entry_date").Find(&ts).Error)
	return ts
}

// TestRecurringNextRun 任务 9.1：四种频率的下期日期计算（纯函数）。
func TestRecurringNextRun(t *testing.T) {
	mk := func(s string) time.Time { return mustDate(t, s) }
	cases := []struct {
		name  string
		cur   string
		start string
		freq  model.RuleFreq
		want  string
	}{
		{"每日 +1 天", "2026-10-01", "2026-10-01", model.FreqDaily, "2026-10-02"},
		{"每日跨月", "2026-10-31", "2026-10-01", model.FreqDaily, "2026-11-01"},
		{"每周 +7 天", "2026-10-01", "2026-10-01", model.FreqWeekly, "2026-10-08"},
		{"每月 1 日", "2026-10-01", "2026-10-01", model.FreqMonthly, "2026-11-01"},
		{"每月 31 日：短月钳到月末", "2026-01-31", "2026-01-31", model.FreqMonthly, "2026-02-28"},
		{"每月 31 日：长月回到 31 不漂移", "2026-02-28", "2026-01-31", model.FreqMonthly, "2026-03-31"},
		{"每月 30 日：2 月钳 28 后回 30", "2026-02-28", "2026-01-30", model.FreqMonthly, "2026-03-30"},
		{"每年 +1 年", "2026-10-01", "2026-10-01", model.FreqYearly, "2027-10-01"},
		{"每年 2/29：平年钳到 28", "2028-02-29", "2028-02-29", model.FreqYearly, "2029-02-28"},
		{"每年 2/29：闰年自然回到 29", "2031-02-28", "2028-02-29", model.FreqYearly, "2032-02-29"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := NextRunAfter(mk(c.cur), mk(c.start), c.freq)
			if got.Format(dayFmt) != c.want {
				t.Fatalf("NextRunAfter(cur=%s, start=%s, %s) = %s, want %s",
					c.cur, c.start, c.freq, got.Format(dayFmt), c.want)
			}
		})
	}
}

// TestRecurringEnsureIdempotent 任务 9.2：连续/重复触发只生成一笔（唯一约束 + 存在性检查）。
func TestRecurringEnsureIdempotent(t *testing.T) {
	e := newRecurringEnv(t)
	rule := e.newRule("50", "2026-10-01")
	now := mustDate(t, "2026-10-15")

	// 首轮：到期生成
	n, err := e.rules.EnsureDue(now)
	mustOK(t, err)
	if n != 1 {
		t.Fatalf("首轮生成 = %d, want 1", n)
	}
	// 连续第二次：节奏已推进到 11-01（晚于今天），不再生成
	n, err = e.rules.EnsureDue(now)
	mustOK(t, err)
	if n != 0 {
		t.Fatalf("连续第二次生成 = %d, want 0", n)
	}
	// 模拟崩溃断点：把 next_run 拨回 10-01 再触发 → 存在性检查兜住，仍只有一笔
	mustOK(t, e.ruleRepo.Advance(rule.ID, mustDate(t, "2026-10-01")))
	n, err = e.rules.EnsureDue(now)
	mustOK(t, err)
	if n != 0 {
		t.Fatalf("断点回拨后再次触发生成 = %d, want 0", n)
	}

	ts := e.ruleTxns(rule.ID)
	if len(ts) != 1 {
		t.Fatalf("模板流水 = %d 笔, want 1", len(ts))
	}
	got := ts[0]
	if got.Source != model.SourceRecurring || got.PeriodKey != "2026-10" ||
		got.RuleID == nil || *got.RuleID != rule.ID {
		t.Fatalf("生成笔标记不对: source=%s period=%s rule_id=%v", got.Source, got.PeriodKey, got.RuleID)
	}
	if got.EntryDate.Format(dayFmt) != "2026-10-01" {
		t.Fatalf("归属日 = %s, want 2026-10-01", got.EntryDate.Format(dayFmt))
	}
	if !got.Amount.Equal(dec("50")) {
		t.Fatalf("金额 = %s, want 50", got.Amount)
	}
}

// TestRecurringPausedThenResume 暂停不生成；重新启用后补齐到期期次（不重不漏）。
func TestRecurringPausedThenResume(t *testing.T) {
	e := newRecurringEnv(t)
	rule := e.newRule("50", "2026-10-01")
	now := mustDate(t, "2026-10-15")

	mustOK(t, e.rules.SetStatus(adminUser(), rule.ID, model.RulePaused))
	n, err := e.rules.EnsureDue(now)
	mustOK(t, err)
	if n != 0 || len(e.ruleTxns(rule.ID)) != 0 {
		t.Fatalf("暂停期生成 = %d 笔, want 0", n)
	}
	r, err := e.ruleRepo.GetByID(rule.ID)
	mustOK(t, err)
	if r.NextRunDate.Format(dayFmt) != "2026-10-01" {
		t.Fatalf("暂停不应改节奏: next_run=%s", r.NextRunDate.Format(dayFmt))
	}

	mustOK(t, e.rules.SetStatus(adminUser(), rule.ID, model.RuleEnabled))
	n, err = e.rules.EnsureDue(now)
	mustOK(t, err)
	if n != 1 {
		t.Fatalf("重新启用补齐 = %d, want 1", n)
	}
}

// TestRecurringEditDeleteIndependent specs/recurring-entries：生成笔独立修改与删除。
func TestRecurringEditDeleteIndependent(t *testing.T) {
	e := newRecurringEnv(t)
	rule := e.newRule("100", "2026-10-01")

	n, err := e.rules.EnsureDue(mustDate(t, "2026-10-15"))
	mustOK(t, err)
	if n != 1 {
		t.Fatalf("首轮生成 = %d, want 1", n)
	}
	ts := e.ruleTxns(rule.ID)

	// 修正生成笔 100 → 120：仅该笔变化，模板金额仍是 100
	ts[0].Amount = dec("120")
	mustOK(t, e.txnRepo.Update(&ts[0]))
	r, err := e.ruleRepo.GetByID(rule.ID)
	mustOK(t, err)
	if !r.Amount.Equal(dec("100")) {
		t.Fatalf("模板金额被联动改动 = %s, want 100", r.Amount)
	}
	// 同一期重复触发不因生成笔被改过而再生成
	n, err = e.rules.EnsureDue(mustDate(t, "2026-10-15"))
	mustOK(t, err)
	if n != 0 {
		t.Fatalf("改动生成笔后重复触发 = %d, want 0", n)
	}

	// 删除生成笔：模板保持启用，后续周期照常按 100 生成
	mustOK(t, e.txnRepo.Delete(ts[0].ID))
	r, err = e.ruleRepo.GetByID(rule.ID)
	mustOK(t, err)
	if r.Status != model.RuleEnabled {
		t.Fatalf("删除生成笔后模板状态 = %s, want enabled", r.Status)
	}
	mustOK(t, e.ruleRepo.Advance(rule.ID, mustDate(t, "2026-11-01")))
	n, err = e.rules.EnsureDue(mustDate(t, "2026-12-15"))
	mustOK(t, err)
	if n != 2 {
		t.Fatalf("下期起生成 = %d, want 2（11 月、12 月）", n)
	}
	for _, g := range e.ruleTxns(rule.ID) {
		if !g.Amount.Equal(dec("100")) {
			t.Fatalf("后续期次金额 = %s, want 模板金额 100", g.Amount)
		}
	}
}

// TestRecurringClosedPeriodSkipped 封账硬锁优先：跳过已封账期次并推进，不卡死后续生成。
func TestRecurringClosedPeriodSkipped(t *testing.T) {
	e := newRecurringEnv(t)
	rule := e.newRule("50", "2026-10-01")
	mustOK(t, e.db.Create(&model.PeriodClosing{
		PeriodKey: "2026-10", Status: model.ClosingClosed, ClosedBy: 1, ClosedAt: time.Now(),
	}).Error)

	n, err := e.rules.EnsureDue(mustDate(t, "2026-10-15"))
	mustOK(t, err)
	if n != 0 {
		t.Fatalf("封账期生成 = %d, want 0", n)
	}
	r, err := e.ruleRepo.GetByID(rule.ID)
	mustOK(t, err)
	if r.NextRunDate.Format(dayFmt) != "2026-11-01" {
		t.Fatalf("封账期次应跳过并推进: next_run=%s, want 2026-11-01", r.NextRunDate.Format(dayFmt))
	}
	// 之后的开放期次照常补上（不含被封的 10-01）
	n, err = e.rules.EnsureDue(mustDate(t, "2026-12-15"))
	mustOK(t, err)
	if n != 2 {
		t.Fatalf("后续生成 = %d, want 2（11-01、12-01）", n)
	}
	for _, g := range e.ruleTxns(rule.ID) {
		if g.EntryDate.Format(dayFmt) == "2026-10-01" {
			t.Fatalf("已封账期次不应生成: %s", g.EntryDate.Format(dayFmt))
		}
	}
}

// TestRecurringValidation 任务 9.1：模板入参校验与权限。
func TestRecurringValidation(t *testing.T) {
	e := newRecurringEnv(t)
	acc, root, leaf := e.seed()

	mkIn := func() RuleInput {
		return RuleInput{
			Amount:     dec("50"),
			Freq:       model.FreqMonthly,
			AccountID:  acc.ID,
			CategoryID: leaf,
			StartDate:  mustDate(t, "2026-10-01"),
		}
	}
	cases := []struct {
		name string
		mut  func(in *RuleInput)
		want error
	}{
		{"非法频率", func(in *RuleInput) { in.Freq = "monthlyy" }, ErrRuleFreqInvalid},
		{"金额为 0", func(in *RuleInput) { in.Amount = dec("0") }, ErrRuleAmountInvalid},
		{"金额三位小数", func(in *RuleInput) { in.Amount = dec("0.005") }, ErrRuleAmountInvalid},
		{"首次生成日期越界", func(in *RuleInput) { in.StartDate = mustDate(t, "1999-01-01") }, ErrRuleDateInvalid},
		{"一级分类不可挂账", func(in *RuleInput) { in.CategoryID = root }, ErrRuleCategoryInvalid},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := mkIn()
			c.mut(&in)
			_, err := e.rules.Create(adminUser(), in)
			if err == nil || !errors.Is(err, c.want) {
				t.Fatalf("Create err = %v, want %v", err, c.want)
			}
		})
	}

	// 停用账户不可挂模板
	acc2 := model.Account{Name: "旧卡", Type: model.AccountAsset, Status: model.StatusDisabled}
	mustOK(t, e.accRepo.Create(&acc2))
	in := mkIn()
	in.AccountID = acc2.ID
	if _, err := e.rules.Create(adminUser(), in); !errors.Is(err, ErrRuleAccountInvalid) {
		t.Fatalf("停用账户 err = %v, want %v", err, ErrRuleAccountInvalid)
	}

	// 只读成员不可创建（service 层二次门禁；三档可建见 TestRecurringOwnership）
	in = mkIn()
	if _, err := e.rules.Create(user(9, model.RoleViewer), in); !errors.Is(err, ErrRuleForbidden) {
		t.Fatalf("只读成员创建 err = %v, want %v", err, ErrRuleForbidden)
	}

	// 非法状态
	r := e.newRule("50", "2026-10-01")
	if err := e.rules.SetStatus(adminUser(), r.ID, "archived"); !errors.Is(err, ErrRuleStatusInvalid) {
		t.Fatalf("非法状态 err = %v, want %v", err, ErrRuleStatusInvalid)
	}
}

// TestRecurringLazyPatchNotify 任务 10.1（specs/notifications「周期记账补齐提醒」）：
// cron 正常生成不打扰；懒补齐（打开应用）补记时落一条「已补记」提醒；重复懒补齐不重复提醒。
func TestRecurringLazyPatchNotify(t *testing.T) {
	e := newRecurringEnv(t)
	rule := e.newRule("50", "2026-10-01")
	now := mustDate(t, "2026-10-15")

	// cron 路径正常到期生成 → 不提醒
	n, err := e.rules.EnsureDue(now)
	mustOK(t, err)
	if n != 1 {
		t.Fatalf("cron 生成 = %d, want 1", n)
	}
	list, err := e.notifs.List("")
	mustOK(t, err)
	if len(list) != 0 {
		t.Fatalf("cron 正常生成不应提醒，实际 %d 条", len(list))
	}

	// 模拟「cron 未跑 + 流水被删」：拨回节奏后懒补齐 → 生成且落一条提醒
	mustOK(t, e.db.Where("rule_id = ?", rule.ID).Delete(&model.Transaction{}).Error)
	mustOK(t, e.ruleRepo.Advance(rule.ID, mustDate(t, "2026-10-01")))
	n, err = e.rules.EnsureDueLazy(now)
	mustOK(t, err)
	if n != 1 {
		t.Fatalf("懒补齐生成 = %d, want 1", n)
	}
	list, err = e.notifs.List(model.NotifRecurringPatched)
	mustOK(t, err)
	if len(list) != 1 {
		t.Fatalf("补齐提醒 = %d 条, want 1", len(list))
	}
	var p RecurringPatchPayload
	mustOK(t, json.Unmarshal([]byte(list[0].Payload), &p))
	if p.Generated != 1 {
		t.Errorf("payload.generated = %d, want 1", p.Generated)
	}

	// 无新生成的懒补齐（幂等重复触发）→ 不再新增提醒
	n, err = e.rules.EnsureDueLazy(now)
	mustOK(t, err)
	if n != 0 {
		t.Fatalf("重复懒补齐生成 = %d, want 0", n)
	}
	list, err = e.notifs.List("")
	mustOK(t, err)
	if len(list) != 1 {
		t.Errorf("重复懒补齐后提醒 = %d 条, want 1", len(list))
	}
}

// TestRecurringListScope specs/recurring-entries「模板按创建者归属隔离」的列表侧：
// 管理员见全部且带创建人姓名；普通成员仅见自己的；只读成员返回自己的空列表。
func TestRecurringListScope(t *testing.T) {
	e := newRecurringEnv(t)
	e.seed()

	users := []model.User{
		{ID: 1, Username: "admin", Role: model.RoleAdmin, Status: model.StatusActive},
		{ID: 2, Username: "member-a", Role: model.RoleMember, Status: model.StatusActive},
		{ID: 3, Username: "member-b", Role: model.RoleMember, Status: model.StatusActive},
		{ID: 4, Username: "viewer", Role: model.RoleViewer, Status: model.StatusActive},
	}
	for i := range users {
		mustOK(t, e.userRepo.Create(&users[i]))
	}

	// 夹具：管理员 1 条、成员甲 2 条（直接落库，创建授权由 TestRecurringOwnership 覆盖）
	mkRule := func(createdBy int64, note string) {
		mustOK(t, e.ruleRepo.Create(&model.RecurringRule{
			Amount:      dec("50"),
			Freq:        model.FreqMonthly,
			AccountID:   1,
			CategoryID:  1,
			Note:        note,
			StartDate:   mustDate(t, "2026-10-01"),
			NextRunDate: mustDate(t, "2026-10-01"),
			Status:      model.RuleEnabled,
			CreatedBy:   createdBy,
		}))
	}
	mkRule(1, "admin-rule")
	mkRule(2, "a-rule-1")
	mkRule(2, "a-rule-2")

	// 管理员见全部，且每条携带创建人姓名
	adminViews, err := e.rules.List(user(1, model.RoleAdmin))
	mustOK(t, err)
	if len(adminViews) != 3 {
		t.Fatalf("管理员列表 = %d 条, want 3", len(adminViews))
	}
	byNote := map[string]string{}
	for _, v := range adminViews {
		byNote[v.Note] = v.CreatedByName
	}
	if byNote["admin-rule"] != "admin" || byNote["a-rule-1"] != "member-a" {
		t.Fatalf("创建人姓名不对: %v", byNote)
	}

	// 普通成员仅见自己的
	memViews, err := e.rules.List(user(2, model.RoleMember))
	mustOK(t, err)
	if len(memViews) != 2 {
		t.Fatalf("成员列表 = %d 条, want 2", len(memViews))
	}
	for _, v := range memViews {
		if v.CreatedBy != 2 {
			t.Fatalf("列表混入他人模板: created_by=%d", v.CreatedBy)
		}
	}

	// 只读成员：查询成功且为自己的空列表
	viewViews, err := e.rules.List(user(4, model.RoleViewer))
	mustOK(t, err)
	if len(viewViews) != 0 {
		t.Fatalf("只读成员列表 = %d 条, want 0", len(viewViews))
	}
}

// TestRecurringOwnership specs/recurring-entries「周期记账模板」授权场景：
// 只读成员创建 403；三档可记账角色均可创建自己的模板且 CreatedBy 落创建者；
// 普通成员改他人模板 403 且模板不变；记账员删自己模板成功且已生成流水保留；
// 管理员可暂停他人模板。
func TestRecurringOwnership(t *testing.T) {
	e := newRecurringEnv(t)
	acc, _, leaf := e.seed()
	mkIn := func(amount string) RuleInput {
		return RuleInput{
			Amount:     dec(amount),
			Freq:       model.FreqMonthly,
			AccountID:  acc.ID,
			CategoryID: leaf,
			StartDate:  mustDate(t, "2026-10-01"),
		}
	}

	// 只读成员创建 → ErrRuleForbidden
	if _, err := e.rules.Create(user(4, model.RoleViewer), mkIn("50")); !errors.Is(err, ErrRuleForbidden) {
		t.Fatalf("只读成员创建 err = %v, want %v", err, ErrRuleForbidden)
	}

	// 三档角色均可创建自己的模板，CreatedBy 落创建者
	adminRule, err := e.rules.Create(user(1, model.RoleAdmin), mkIn("50"))
	mustOK(t, err)
	recRule, err := e.rules.Create(user(2, model.RoleRecorder), mkIn("50"))
	mustOK(t, err)
	memRule, err := e.rules.Create(user(3, model.RoleMember), mkIn("50"))
	mustOK(t, err)
	if adminRule.CreatedBy != 1 || recRule.CreatedBy != 2 || memRule.CreatedBy != 3 {
		t.Fatalf("创建人落库不对: admin=%d recorder=%d member=%d",
			adminRule.CreatedBy, recRule.CreatedBy, memRule.CreatedBy)
	}

	// 普通成员改他人（管理员）模板 → 403 且模板保持不变
	if _, err := e.rules.Update(user(3, model.RoleMember), adminRule.ID, mkIn("99")); !errors.Is(err, ErrRuleForbidden) {
		t.Fatalf("成员改他人模板 err = %v, want %v", err, ErrRuleForbidden)
	}
	after, err := e.ruleRepo.GetByID(adminRule.ID)
	mustOK(t, err)
	if !after.Amount.Equal(dec("50")) {
		t.Fatalf("越权修改后金额 = %s, want 50（模板保持不变）", after.Amount)
	}

	// 三条模板到期各生成一笔
	n, err := e.rules.EnsureDue(mustDate(t, "2026-10-15"))
	mustOK(t, err)
	if n != 3 {
		t.Fatalf("首轮生成 = %d, want 3", n)
	}

	// 记账员删自己的模板 → 成功，已生成流水保留
	mustOK(t, e.rules.Delete(user(2, model.RoleRecorder), recRule.ID))
	gone, err := e.ruleRepo.GetByID(recRule.ID)
	mustOK(t, err)
	if gone != nil {
		t.Fatal("记账员删除自己的模板后模板应不存在")
	}
	if kept := e.ruleTxns(recRule.ID); len(kept) != 1 {
		t.Fatalf("删除模板后已生成流水 = %d 笔, want 1（保留）", len(kept))
	}

	// 记账员删他人模板 → 403（归属边界）
	if err := e.rules.Delete(user(2, model.RoleRecorder), memRule.ID); !errors.Is(err, ErrRuleForbidden) {
		t.Fatalf("记账员删他人模板 err = %v, want %v", err, ErrRuleForbidden)
	}

	// 管理员暂停他人（普通成员）模板 → 成功
	mustOK(t, e.rules.SetStatus(user(1, model.RoleAdmin), memRule.ID, model.RulePaused))
	mr, err := e.ruleRepo.GetByID(memRule.ID)
	mustOK(t, err)
	if mr.Status != model.RulePaused {
		t.Fatalf("管理员暂停他人模板后状态 = %s, want paused", mr.Status)
	}
}

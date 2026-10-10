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

// budgetEnv 预算测试环境（sqlite 内存库跑真实仓储，流水写钩子联动超支评估）。
type budgetEnv struct {
	periods *PeriodService
	budget  *BudgetService
	txn     *TransactionService
	accRepo *repository.AccountRepo
	catRepo *repository.CategoryRepo
	notifs  *repository.NotificationRepo
}

func newBudgetEnv(t *testing.T) *budgetEnv {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.Setting{}, &model.Account{}, &model.Category{}, &model.Transaction{},
		&model.PeriodClosing{}, &model.Budget{}, &model.Notification{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	periods := NewPeriodService(&memSettings{m: map[string]string{"settlement_day": "1"}})
	accRepo := repository.NewAccountRepo(db)
	catRepo := repository.NewCategoryRepo(db)
	notifs := repository.NewNotificationRepo(db)
	bs := NewBudgetService(BudgetDeps{
		Budgets: repository.NewBudgetRepo(db),
		Stats:   repository.NewStatsRepo(db),
		Cats:    catRepo,
		Notifs:  notifs,
		Periods: periods,
	})
	txn := NewTransactionService(TxnDeps{
		Txns:     repository.NewTransactionRepo(db),
		Accounts: accRepo,
		Cats:     catRepo,
		Closings: repository.NewClosingRepo(db),
		Periods:  periods,
	})
	// 复刻 main.go 装配：流水写入后评估该账期超支
	txn.OnWritePeriod = func(keys ...string) {
		for _, k := range keys {
			mustOK(t, bs.Evaluate(k))
		}
	}
	return &budgetEnv{
		periods: periods,
		budget:  bs,
		txn:     txn,
		accRepo: accRepo,
		catRepo: catRepo,
		notifs:  notifs,
	}
}

// seedCats 支出：餐饮{午餐,晚餐}、交通{地铁}；收入：工资{月薪}。
func (e *budgetEnv) seedCats(t *testing.T) (mealRoot, lunch, dinner, transitRoot, metro, salary int64) {
	t.Helper()
	mk := func(kind model.CategoryKind, name string, parent *int64) int64 {
		c := model.Category{Kind: kind, Name: name, ParentID: parent}
		mustOK(t, e.catRepo.Create(&c))
		return c.ID
	}
	mealRoot = mk(model.KindExpense, "餐饮", nil)
	lunch = mk(model.KindExpense, "午餐", &mealRoot)
	dinner = mk(model.KindExpense, "晚餐", &mealRoot)
	transitRoot = mk(model.KindExpense, "交通", nil)
	metro = mk(model.KindExpense, "地铁", &transitRoot)
	incomeRoot := mk(model.KindIncome, "工资", nil)
	salary = mk(model.KindIncome, "月薪", &incomeRoot)
	return
}

func (e *budgetEnv) seedAccounts(t *testing.T) (model.Account, model.Account) {
	t.Helper()
	a1 := model.Account{Name: "现金", Type: model.AccountAsset, Status: model.StatusActive}
	a2 := model.Account{Name: "银行卡", Type: model.AccountAsset, Status: model.StatusActive}
	mustOK(t, e.accRepo.Create(&a1))
	mustOK(t, e.accRepo.Create(&a2))
	return a1, a2
}

// expense 支出入参模板。
func (e *budgetEnv) expense(t *testing.T, accID, catID int64, amount, date string) TxnInput {
	t.Helper()
	return TxnInput{
		Kind: model.TxnExpense, Amount: dec(amount), AccountID: accID,
		CategoryID: catID, EntryDate: mustDate(t, date),
	}
}

func adminUser() *model.User {
	return &model.User{ID: 1, Username: "admin", Role: model.RoleAdmin, Status: model.StatusActive}
}

// mustCreate 记一笔并断言成功（Create 返回二值）。
func mustCreate(t *testing.T, s *TransactionService, actor *model.User, in TxnInput) *model.Transaction {
	t.Helper()
	tx, err := s.Create(actor, in)
	mustOK(t, err)
	return tx
}

// TestBudgetConsumptionScope 覆盖 8.1 / specs「预算消耗计算」：
// 消耗只算支出——收入与转账不计；分类消耗并入全部子分类；
// 分类超支按「消耗-预算」给出超出金额（spec：餐饮 3200/3000 → 超支 200）。
func TestBudgetConsumptionScope(t *testing.T) {
	env := newBudgetEnv(t)
	meal, lunch, dinner, transit, metro, salary := env.seedCats(t)
	cash, bank := env.seedAccounts(t)
	admin := adminUser()

	// spec 配置场景：总预算 8000、餐饮 3000
	total, mealBudget := "8000", "3000"
	mustOK(t, env.budget.Set("2026-10", BudgetSetInput{
		Total: &total,
		Items: []BudgetSetItem{{CategoryID: meal, Amount: &mealBudget}},
	}))

	// 餐饮子分类支出 1000+2200=3200；交通 500；收入 5000；转账 1000
	mustCreate(t, env.txn, admin, env.expense(t, cash.ID, lunch, "1000", "2026-10-03"))
	mustCreate(t, env.txn, admin, env.expense(t, cash.ID, dinner, "2200", "2026-10-04"))
	mustCreate(t, env.txn, admin, env.expense(t, cash.ID, metro, "500", "2026-10-05"))
	mustCreate(t, env.txn, admin, TxnInput{
		Kind: model.TxnIncome, Amount: dec("5000"), AccountID: bank.ID,
		CategoryID: salary, EntryDate: mustDate(t, "2026-10-06"),
	})
	mustCreate(t, env.txn, admin, TxnInput{
		Kind: model.TxnTransfer, Amount: dec("1000"), AccountID: cash.ID,
		TransferAccountID: bank.ID, EntryDate: mustDate(t, "2026-10-07"),
	})

	view, err := env.budget.Get("2026-10")
	mustOK(t, err)

	// 总预算：消耗 3700（收入与转账不计），进度 46.25%，未超支
	if view.Total.Budget == nil || *view.Total.Budget != "8000.00" {
		t.Fatalf("总预算 = %v, want 8000.00", view.Total.Budget)
	}
	if view.Total.Consumed != "3700.00" {
		t.Errorf("总消耗 = %s, want 3700.00（收入/转账不计）", view.Total.Consumed)
	}
	if view.Total.Percent != 46.25 {
		t.Errorf("总进度 = %v, want 46.25", view.Total.Percent)
	}
	if view.Total.Over || view.Total.OverAmount != "0.00" {
		t.Errorf("总预算超支 = %v/%s, want false/0.00", view.Total.Over, view.Total.OverAmount)
	}

	// 只列一级支出分类（收入树不出现）
	if len(view.Categories) != 2 {
		t.Fatalf("一级支出行数 = %d, want 2（餐饮、交通）", len(view.Categories))
	}

	// 餐饮：3200/3000 → 超支 200、进度封顶 100（spec 场景）
	var mealRow, transitRow *BudgetCategoryRow
	for i := range view.Categories {
		switch view.Categories[i].CategoryID {
		case meal:
			mealRow = &view.Categories[i]
		case transit:
			transitRow = &view.Categories[i]
		}
	}
	if mealRow == nil || transitRow == nil {
		t.Fatal("缺少餐饮/交通预算行")
	}
	if mealRow.CategoryName != "餐饮" {
		t.Errorf("餐饮行名 = %s", mealRow.CategoryName)
	}
	if mealRow.Consumed != "3200.00" {
		t.Errorf("餐饮消耗 = %s, want 3200.00（子分类并入一级）", mealRow.Consumed)
	}
	if !mealRow.Over || mealRow.OverAmount != "200.00" {
		t.Errorf("餐饮超支 = %v/%s, want true/200.00", mealRow.Over, mealRow.OverAmount)
	}
	if mealRow.Percent != 100 {
		t.Errorf("餐饮进度 = %v, want 100（超支封顶）", mealRow.Percent)
	}

	// 交通：未设预算但消耗可见，进度条未设置态
	if transitRow.Budget != nil {
		t.Errorf("交通预算 = %v, want nil（未设置）", *transitRow.Budget)
	}
	if transitRow.Consumed != "500.00" || transitRow.Over {
		t.Errorf("交通 = %s/over=%v, want 500.00/false", transitRow.Consumed, transitRow.Over)
	}
}

// TestBudgetOverNotifyOnce 覆盖 8.2 / specs「超支标红与提醒」：
// 首次超支生成一条提醒（含分类名与超出金额）；同账期同预算重复记账、删了再记都不再提醒；
// 把总预算改到低于既有消耗时立即补一条总预算提醒。
func TestBudgetOverNotifyOnce(t *testing.T) {
	env := newBudgetEnv(t)
	meal, lunch, dinner, _, metro, _ := env.seedCats(t)
	cash, _ := env.seedAccounts(t)
	admin := adminUser()

	mealBudget := "3000"
	mustOK(t, env.budget.Set("2026-10", BudgetSetInput{
		Items: []BudgetSetItem{{CategoryID: meal, Amount: &mealBudget}},
	}))

	// 首次超支 3100 > 3000 → 恰好一条提醒，内容含分类名与超出金额
	tx, err := env.txn.Create(admin, env.expense(t, cash.ID, lunch, "3100", "2026-10-03"))
	mustOK(t, err)
	notifs, err := env.notifs.List(model.NotifBudgetOverdue)
	mustOK(t, err)
	if len(notifs) != 1 {
		t.Fatalf("首次超支提醒数 = %d, want 1", len(notifs))
	}
	var p BudgetNotifyPayload
	mustOK(t, json.Unmarshal([]byte(notifs[0].Payload), &p))
	if p.CategoryName != "餐饮" || p.OverAmount != "100.00" || p.PeriodKey != "2026-10" || p.Scope != "category" {
		t.Errorf("提醒内容 = %+v, want 餐饮/100.00/2026-10/category", p)
	}
	if p.DedupKey == "" {
		t.Error("提醒缺少 dedup_key")
	}

	// 继续记账（更超）→ 不新增
	mustCreate(t, env.txn, admin, env.expense(t, cash.ID, dinner, "50", "2026-10-04"))
	// 删除一笔（仍超支）→ 不撤回也不新增
	mustOK(t, env.txn.Delete(admin, tx.ID))
	// 删了再记 → 仍不新增（同账期同预算只提醒一次）
	mustCreate(t, env.txn, admin, env.expense(t, cash.ID, dinner, "80", "2026-10-05"))
	notifs, err = env.notifs.List(model.NotifBudgetOverdue)
	mustOK(t, err)
	if len(notifs) != 1 {
		t.Fatalf("重复记账后提醒数 = %d, want 1", len(notifs))
	}

	// 总预算 100 低于既有消耗 → 保存时立即补一条总预算提醒
	total := "100"
	mustOK(t, env.budget.Set("2026-10", BudgetSetInput{
		Total: &total,
		Items: []BudgetSetItem{{CategoryID: meal, Amount: &mealBudget}},
	}))
	notifs, err = env.notifs.List(model.NotifBudgetOverdue)
	mustOK(t, err)
	if len(notifs) != 2 {
		t.Fatalf("总预算超支后提醒数 = %d, want 2", len(notifs))
	}
	scopes := map[string]bool{}
	for _, n := range notifs {
		var np BudgetNotifyPayload
		mustOK(t, json.Unmarshal([]byte(n.Payload), &np))
		scopes[np.Scope] = true
		if np.Scope == "total" && np.CategoryName != "总预算" {
			t.Errorf("总预算提醒分类名 = %s, want 总预算", np.CategoryName)
		}
	}
	if !scopes["category"] || !scopes["total"] {
		t.Errorf("提醒范围 = %v, want 同时有 category 与 total", scopes)
	}

	// 再记账：两个预算都提醒过 → 不再新增
	mustCreate(t, env.txn, admin, env.expense(t, cash.ID, metro, "30", "2026-10-06"))
	notifs, err = env.notifs.List(model.NotifBudgetOverdue)
	mustOK(t, err)
	if len(notifs) != 2 {
		t.Fatalf("最终提醒数 = %d, want 2", len(notifs))
	}
}

// TestBudgetSetValidation 配置校验：非法账期、仅支出一级分类、金额格式、0=清除、重复项去重。
func TestBudgetSetValidation(t *testing.T) {
	env := newBudgetEnv(t)
	meal, lunch, _, transit, _, salary := env.seedCats(t)

	// 非法账期
	if err := env.budget.Set("2026-13", BudgetSetInput{}); !errors.Is(err, ErrBudgetInvalidPeriod) {
		t.Errorf("非法账期 = %v, want ErrBudgetInvalidPeriod", err)
	}

	// 二级分类不可配
	bad := "100"
	if err := env.budget.Set("2026-10", BudgetSetInput{
		Items: []BudgetSetItem{{CategoryID: lunch, Amount: &bad}},
	}); !errors.Is(err, ErrBudgetCategoryInvalid) {
		t.Errorf("二级分类 = %v, want ErrBudgetCategoryInvalid", err)
	}
	// 收入分类不可配（金额合法也不放行）
	if err := env.budget.Set("2026-10", BudgetSetInput{
		Items: []BudgetSetItem{{CategoryID: salary, Amount: &bad}},
	}); !errors.Is(err, ErrBudgetCategoryInvalid) {
		t.Errorf("收入分类 = %v, want ErrBudgetCategoryInvalid", err)
	}
	// 金额格式（负数、三位小数、超上限、非数字）
	for _, s := range []string{"abc", "-5", "0.005", "99999999999"} {
		v := s
		if err := env.budget.Set("2026-10", BudgetSetInput{Total: &v}); !errors.Is(err, ErrBudgetAmountInvalid) {
			t.Errorf("金额 %q = %v, want ErrBudgetAmountInvalid", s, err)
		}
	}

	// 合法保存；同分类重复项取首项
	total, mealAmt, transitAmt, dup := "8000", "3000", "200", "999"
	mustOK(t, env.budget.Set("2026-10", BudgetSetInput{
		Total: &total,
		Items: []BudgetSetItem{
			{CategoryID: meal, Amount: &mealAmt},
			{CategoryID: meal, Amount: &dup}, // 重复 → 取首项 3000
			{CategoryID: transit, Amount: &transitAmt},
		},
	}))
	view, err := env.budget.Get("2026-10")
	mustOK(t, err)
	if view.Total.Budget == nil || *view.Total.Budget != "8000.00" {
		t.Fatalf("总预算 = %v, want 8000.00", view.Total.Budget)
	}
	budgetOf := func(v *BudgetView, id int64) *string {
		for i := range v.Categories {
			if v.Categories[i].CategoryID == id {
				return v.Categories[i].Budget
			}
		}
		t.Fatalf("缺少分类行 %d", id)
		return nil
	}
	if b := budgetOf(view, meal); b == nil || *b != "3000.00" {
		t.Errorf("餐饮预算 = %v, want 3000.00", b)
	}
	if b := budgetOf(view, transit); b == nil || *b != "200.00" {
		t.Errorf("交通预算 = %v, want 200.00", b)
	}

	// 整单语义：未列出的分类被清除；amount=0 也是清除
	zero := "0"
	mustOK(t, env.budget.Set("2026-10", BudgetSetInput{
		Total: &total,
		Items: []BudgetSetItem{{CategoryID: transit, Amount: &zero}},
	}))
	view, err = env.budget.Get("2026-10")
	mustOK(t, err)
	if b := budgetOf(view, meal); b != nil {
		t.Errorf("未列出的餐饮预算 = %v, want nil（整单清除）", *b)
	}
	if b := budgetOf(view, transit); b != nil {
		t.Errorf("amount=0 的交通预算 = %v, want nil（0 = 清除）", *b)
	}

	// total 缺省 = 清除总预算
	mustOK(t, env.budget.Set("2026-10", BudgetSetInput{}))
	view, err = env.budget.Get("2026-10")
	mustOK(t, err)
	if view.Total.Budget != nil {
		t.Errorf("总预算 = %v, want nil（已清除）", *view.Total.Budget)
	}
	if len(view.Categories) != 2 {
		t.Errorf("一级支出行数 = %d, want 2（未配置也返回行供回填）", len(view.Categories))
	}
}

// TestBudgetRefundPayback 覆盖任务 3.4 / specs/budgets「预算消耗计算」「进度与超支展示」：
// 退款按到账月回吐分类与总消耗；净消耗为负不超支、不触发提醒；净消耗重新超预算才提醒。
func TestBudgetRefundPayback(t *testing.T) {
	env := newBudgetEnv(t)
	meal, lunch, _, _, _, _ := env.seedCats(t)
	cash, _ := env.seedAccounts(t)
	admin := adminUser()

	total, mealBudget := "8000", "3000"
	mustOK(t, env.budget.Set("2026-10", BudgetSetInput{
		Total: &total,
		Items: []BudgetSetItem{{CategoryID: meal, Amount: &mealBudget}},
	}))

	// 支出 1000 → 消耗 1000
	mustCreate(t, env.txn, admin, TxnInput{
		Kind: model.TxnExpense, Amount: dec("1000"), AccountID: cash.ID,
		CategoryID: lunch, EntryDate: mustDate(t, "2026-10-05"),
	})
	// 退款 200（到账月同月）→ 消耗回吐 800
	mustCreate(t, env.txn, admin, TxnInput{
		Kind: model.TxnRefund, Amount: dec("200"), AccountID: cash.ID,
		CategoryID: lunch, EntryDate: mustDate(t, "2026-10-06"),
	})

	rowOf := func(view *BudgetView, catID int64) *BudgetCategoryRow {
		for i := range view.Categories {
			if view.Categories[i].CategoryID == catID {
				return &view.Categories[i]
			}
		}
		return nil
	}

	view, err := env.budget.Get("2026-10")
	mustOK(t, err)
	if view.Total.Consumed != "800.00" {
		t.Errorf("总消耗 = %s, want 800.00（退款回吐）", view.Total.Consumed)
	}
	if r := rowOf(view, meal); r == nil || r.Consumed != "800.00" {
		t.Errorf("餐饮消耗 = %v, want 800.00", r)
	}

	// 净消耗为负：再退 1000 → 餐饮/总 -200 → 不超支、进度 0
	mustCreate(t, env.txn, admin, TxnInput{
		Kind: model.TxnRefund, Amount: dec("1000"), AccountID: cash.ID,
		CategoryID: lunch, EntryDate: mustDate(t, "2026-10-07"),
	})
	view, err = env.budget.Get("2026-10")
	mustOK(t, err)
	if view.Total.Consumed != "-200.00" {
		t.Errorf("总消耗 = %s, want -200.00", view.Total.Consumed)
	}
	if view.Total.Over || view.Total.Percent != 0 {
		t.Errorf("负消耗不应超支/应零进度: over=%v pct=%v", view.Total.Over, view.Total.Percent)
	}
	mRow := rowOf(view, meal)
	if mRow == nil || mRow.Consumed != "-200.00" || mRow.Over || mRow.Percent != 0 {
		t.Errorf("餐饮负消耗 = %+v, want -200.00/false/0", mRow)
	}

	// 负消耗不触发超支提醒
	notifs, err := env.notifs.List(model.NotifBudgetOverdue)
	mustOK(t, err)
	if len(notifs) != 0 {
		t.Errorf("负消耗不应提醒, got %d 条", len(notifs))
	}

	// 净消耗重新超过预算才提醒（餐饮 -200 + 4000 = 3800 > 3000）
	mustCreate(t, env.txn, admin, TxnInput{
		Kind: model.TxnExpense, Amount: dec("4000"), AccountID: cash.ID,
		CategoryID: lunch, EntryDate: mustDate(t, "2026-10-08"),
	})
	notifs, err = env.notifs.List(model.NotifBudgetOverdue)
	mustOK(t, err)
	if len(notifs) != 1 {
		t.Errorf("净消耗超预算应提醒 1 条, got %d", len(notifs))
	}
	view, err = env.budget.Get("2026-10")
	mustOK(t, err)
	if r := rowOf(view, meal); r == nil || r.Consumed != "3800.00" || !r.Over || r.OverAmount != "800.00" {
		t.Errorf("餐饮超支行 = %+v, want 3800.00/true/800.00", r)
	}
}

package repository_test

import (
	"testing"

	"jizhang/internal/model"
	"jizhang/internal/repository"
)

// seedTxn 直接落库一条流水（kind/日期/归期/分类自定）。
func seedTxn(t *testing.T, repo *repository.TransactionRepo, kind model.TxnKind,
	amount, date, periodKey string, catID *int64, accountID int64) model.Transaction {
	t.Helper()
	tx := model.Transaction{
		Kind: kind, Amount: dec(amount), AccountID: accountID, CategoryID: catID,
		EntryDate: mustDate(t, date), PeriodKey: periodKey,
		CreatedBy: 1, Source: model.SourceManual,
	}
	must(t, repo.Create(&tx))
	return tx
}

// TestStatsRefundNet 覆盖任务 3.2 / statistics「退款计入统计口径」：
// 支出为退款冲减后净额；退款不计收入；跨月退款按到账日/归期计入退款月，原月不变。
func TestStatsRefundNet(t *testing.T) {
	db := newTestDB(t)
	txnRepo := repository.NewTransactionRepo(db)
	stats := repository.NewStatsRepo(db)

	acc := model.Account{Name: "现金", Type: model.AccountAsset, Status: model.StatusActive}
	must(t, repository.NewAccountRepo(db).Create(&acc))

	catRepo := repository.NewCategoryRepo(db)
	root := model.Category{Kind: model.KindExpense, Name: "餐饮"}
	must(t, catRepo.Create(&root))
	leaf := model.Category{Kind: model.KindExpense, Name: "午餐", ParentID: &root.ID}
	must(t, catRepo.Create(&leaf))
	leafID := leaf.ID

	// 9 月支出 500；10 月 5 日退款 500；10 月 8 日收入 1000
	seedTxn(t, txnRepo, model.TxnExpense, "500", "2026-09-15", "2026-09", &leafID, acc.ID)
	seedTxn(t, txnRepo, model.TxnRefund, "500", "2026-10-05", "2026-10", &leafID, acc.ID)
	seedTxn(t, txnRepo, model.TxnIncome, "1000", "2026-10-08", "2026-10", nil, acc.ID)

	// 逐日趋势：9-15 支出 500；10-05 支出 -500；10-08 收入 1000、支出 0
	rows, err := stats.DailyTotals(mustDate(t, "2026-09-01"), mustDate(t, "2026-10-31"))
	must(t, err)
	byDate := map[string]repository.DailyRow{}
	var incomeSum = dec("0")
	for _, r := range rows {
		byDate[r.D.Format("2006-01-02")] = r
		incomeSum = incomeSum.Add(r.Income)
	}
	if got := byDate["2026-09-15"].Expense; !got.Equal(dec("500")) {
		t.Errorf("9-15 支出 = %s, want 500", got)
	}
	if got := byDate["2026-10-05"].Expense; !got.Equal(dec("-500")) {
		t.Errorf("10-05 支出 = %s, want -500（退款按到账日冲减）", got)
	}
	if got := byDate["2026-10-05"].Income; !got.IsZero() {
		t.Errorf("10-05 收入 = %s, want 0（退款不计收入）", got)
	}
	if !incomeSum.Equal(dec("1000")) {
		t.Errorf("区间收入合计 = %s, want 1000（退款不计收入）", incomeSum)
	}

	// 账期汇总：9 月支出 500 不变；10 月支出 -500
	periods, err := stats.PeriodTotals(2026)
	must(t, err)
	byPeriod := map[string]repository.PeriodRow{}
	for _, p := range periods {
		byPeriod[p.PeriodKey] = p
	}
	if got := byPeriod["2026-09"].Expense; !got.Equal(dec("500")) {
		t.Errorf("2026-09 支出 = %s, want 500（不回填原月）", got)
	}
	if got := byPeriod["2026-10"].Expense; !got.Equal(dec("-500")) {
		t.Errorf("2026-10 支出 = %s, want -500", got)
	}

	// SumByPeriod（封账预览/预算总消耗同一口径）
	sum9, err := stats.SumByPeriod("2026-09")
	must(t, err)
	if !sum9.Expense.Equal(dec("500")) {
		t.Errorf("SumByPeriod(2026-09).Expense = %s, want 500", sum9.Expense)
	}
	sum10, err := stats.SumByPeriod("2026-10")
	must(t, err)
	if !sum10.Expense.Equal(dec("-500")) {
		t.Errorf("SumByPeriod(2026-10).Expense = %s, want -500", sum10.Expense)
	}
}

// TestCategoryTotalsNegative 覆盖任务 3.3 / statistics「分类占比」：
// 分类净额并入 expense 口径且可为负，后端原样返回（不过滤），供图表层裁剪。
func TestCategoryTotalsNegative(t *testing.T) {
	db := newTestDB(t)
	txnRepo := repository.NewTransactionRepo(db)
	stats := repository.NewStatsRepo(db)

	acc := model.Account{Name: "现金", Type: model.AccountAsset, Status: model.StatusActive}
	must(t, repository.NewAccountRepo(db).Create(&acc))

	catRepo := repository.NewCategoryRepo(db)
	digi := model.Category{Kind: model.KindExpense, Name: "数码"}
	must(t, catRepo.Create(&digi))
	phone := model.Category{Kind: model.KindExpense, Name: "手机", ParentID: &digi.ID}
	must(t, catRepo.Create(&phone))
	meal := model.Category{Kind: model.KindExpense, Name: "餐饮"}
	must(t, catRepo.Create(&meal))
	lunch := model.Category{Kind: model.KindExpense, Name: "午餐", ParentID: &meal.ID}
	must(t, catRepo.Create(&lunch))
	phoneID, lunchID := phone.ID, lunch.ID

	// 数码：仅退款 200 → 净额 -200；餐饮：支出 300 - 退款 100 = 200
	seedTxn(t, txnRepo, model.TxnRefund, "200", "2026-10-04", "2026-10", &phoneID, acc.ID)
	seedTxn(t, txnRepo, model.TxnExpense, "300", "2026-10-03", "2026-10", &lunchID, acc.ID)
	seedTxn(t, txnRepo, model.TxnRefund, "100", "2026-10-06", "2026-10", &lunchID, acc.ID)

	rows, err := stats.CategoryTotals(mustDate(t, "2026-10-01"), mustDate(t, "2026-10-31"))
	must(t, err)

	byRoot := map[int64]repository.CategoryRow{}
	for _, r := range rows {
		byRoot[r.RootID] = r
	}
	digiRow, ok := byRoot[digi.ID]
	if !ok {
		t.Fatal("净额为负的分类 MUST 仍在返回中（不过滤）")
	}
	if digiRow.Kind != model.TxnExpense {
		t.Errorf("退款应并入 expense 口径, got kind=%s", digiRow.Kind)
	}
	if !digiRow.Total.Equal(dec("-200")) {
		t.Errorf("数码净额 = %s, want -200", digiRow.Total)
	}
	mealRow, ok := byRoot[meal.ID]
	if !ok {
		t.Fatal("餐饮分类缺失")
	}
	if !mealRow.Total.Equal(dec("200")) {
		t.Errorf("餐饮净额 = %s, want 200", mealRow.Total)
	}
}

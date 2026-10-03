package repository_test

import (
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"jizhang/internal/model"
	"jizhang/internal/repository"
	"jizhang/internal/service"
)

// newTestDB 纯 Go SQLite 内存库（无需外部 DB），建核心表跑仓储逻辑。
func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.Account{}, &model.Category{}, &model.Transaction{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func dec(s string) decimal.Decimal { return decimal.RequireFromString(s) }

func mustDate(t *testing.T, s string) time.Time {
	t.Helper()
	d, err := time.ParseInLocation("2006-01-02", s, time.Local)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// TestBalanceAggregate 覆盖 3.2：期初 + 支出/收入/转账（信用卡还款）聚合，
// 断言总资产 = Σ资产 + Σ负债（负值）。
func TestBalanceAggregate(t *testing.T) {
	db := newTestDB(t)
	accRepo := repository.NewAccountRepo(db)
	txnRepo := repository.NewTransactionRepo(db)

	bank := model.Account{Name: "银行卡", Type: model.AccountAsset,
		OpeningBalance: dec("1000"), Status: model.StatusActive}
	card := model.Account{Name: "信用卡", Type: model.AccountLiability,
		OpeningBalance: decimal.Zero, Status: model.StatusActive}
	must(t, accRepo.Create(&bank))
	must(t, accRepo.Create(&card))

	entry := mustDate(t, "2026-10-02")
	cardID := card.ID
	txns := []model.Transaction{
		{Kind: model.TxnExpense, Amount: dec("500"), AccountID: bank.ID,
			EntryDate: entry, PeriodKey: "2026-10", CreatedBy: 1, Source: model.SourceManual},
		{Kind: model.TxnExpense, Amount: dec("300"), AccountID: card.ID,
			EntryDate: entry, PeriodKey: "2026-10", CreatedBy: 1, Source: model.SourceManual},
		{Kind: model.TxnTransfer, Amount: dec("200"), AccountID: bank.ID,
			TransferAccountID: &cardID, EntryDate: entry, PeriodKey: "2026-10",
			CreatedBy: 1, Source: model.SourceManual},
		{Kind: model.TxnIncome, Amount: dec("1000"), AccountID: bank.ID,
			EntryDate: entry, PeriodKey: "2026-10", CreatedBy: 1, Source: model.SourceManual},
	}
	for i := range txns {
		must(t, txnRepo.Create(&txns[i]))
	}

	accounts, err := accRepo.List()
	must(t, err)
	balance := map[int64]decimal.Decimal{}
	total := decimal.Zero
	for _, a := range accounts {
		balance[a.ID] = a.Balance
		total = total.Add(a.Balance)
	}
	// 银行 1000-500-200+1000 = 1300；信用卡 0-300+200 = -100；总资产 1200
	if got := balance[bank.ID]; !got.Equal(dec("1300")) {
		t.Errorf("银行卡余额 = %s, want 1300", got)
	}
	if got := balance[card.ID]; !got.Equal(dec("-100")) {
		t.Errorf("信用卡余额 = %s, want -100（欠款）", got)
	}
	if !total.Equal(dec("1200")) {
		t.Errorf("总资产 = %s, want 1200（Σ资产+Σ负债负值）", total)
	}
}

// TestDeleteRollsBackBalance 覆盖 4.4：删除流水前后余额差 = 流水金额。
func TestDeleteRollsBackBalance(t *testing.T) {
	db := newTestDB(t)
	accRepo := repository.NewAccountRepo(db)
	txnRepo := repository.NewTransactionRepo(db)

	acc := model.Account{Name: "现金", Type: model.AccountAsset,
		OpeningBalance: dec("800"), Status: model.StatusActive}
	must(t, accRepo.Create(&acc))

	txn := model.Transaction{Kind: model.TxnExpense, Amount: dec("120.50"),
		AccountID: acc.ID, EntryDate: mustDate(t, "2026-10-02"),
		PeriodKey: "2026-10", CreatedBy: 1, Source: model.SourceManual}
	must(t, txnRepo.Create(&txn))

	balanceOf := func() decimal.Decimal {
		accounts, err := accRepo.List()
		must(t, err)
		for _, a := range accounts {
			if a.ID == acc.ID {
				return a.Balance
			}
		}
		t.Fatal("account missing")
		return decimal.Zero
	}

	before := balanceOf()
	if !before.Equal(dec("679.50")) {
		t.Fatalf("记账后余额 = %s, want 679.50", before)
	}
	must(t, txnRepo.Delete(txn.ID))
	after := balanceOf()
	if !after.Sub(before).Equal(txn.Amount) {
		t.Errorf("删除前后余额差 = %s, want %s", after.Sub(before), txn.Amount)
	}
	if !after.Equal(dec("800")) {
		t.Errorf("删除后余额 = %s, want 800", after)
	}
}

// TestCategoryDeleteConstraints 覆盖 3.3：有子分类/被流水引用时不可删。
func TestCategoryDeleteConstraints(t *testing.T) {
	db := newTestDB(t)
	catRepo := repository.NewCategoryRepo(db)
	txnRepo := repository.NewTransactionRepo(db)

	root := model.Category{Kind: model.KindExpense, Name: "餐饮"}
	must(t, catRepo.Create(&root))
	child := model.Category{Kind: model.KindExpense, Name: "午餐", ParentID: &root.ID}
	must(t, catRepo.Create(&child))

	acc := model.Account{Name: "现金", Type: model.AccountAsset, Status: model.StatusActive}
	must(t, repository.NewAccountRepo(db).Create(&acc))
	catID := child.ID
	txn := model.Transaction{Kind: model.TxnExpense, Amount: dec("20"),
		AccountID: acc.ID, CategoryID: &catID,
		EntryDate: mustDate(t, "2026-10-02"), PeriodKey: "2026-10",
		CreatedBy: 1, Source: model.SourceManual}
	must(t, txnRepo.Create(&txn))

	// service 层删除拦截
	svc := service.NewCategoryService(catRepo)
	if err := svc.Delete(root.ID); err == nil || err.Error() != "该分类还有子分类，不能删除" {
		t.Errorf("删餐饮 = %v, want 有子分类拒绝", err)
	}
	if err := svc.Delete(child.ID); err == nil || err.Error() != "该分类仍被流水引用，不能删除" {
		t.Errorf("删午餐 = %v, want 被引用拒绝", err)
	}
	// 无约束叶子 → 可删
	free := model.Category{Kind: model.KindExpense, Name: "临时"}
	must(t, catRepo.Create(&free))
	if err := svc.Delete(free.ID); err != nil {
		t.Errorf("删无约束分类 = %v, want nil", err)
	}
}

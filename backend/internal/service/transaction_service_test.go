package service

import (
	"errors"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"jizhang/internal/model"
	"jizhang/internal/repository"
)

// refundEnv 退款测试环境（sqlite 内存库跑真实仓储，含封账表）。
type refundEnv struct {
	periods  *PeriodService
	txn      *TransactionService
	closings *repository.ClosingRepo
	accRepo  *repository.AccountRepo
	catRepo  *repository.CategoryRepo
}

func newRefundEnv(t *testing.T) *refundEnv {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.Account{}, &model.Category{}, &model.Transaction{},
		&model.PeriodClosing{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	periods := NewPeriodService(&memSettings{m: map[string]string{"settlement_day": "1"}})
	closings := repository.NewClosingRepo(db)
	return &refundEnv{
		periods:  periods,
		closings: closings,
		accRepo:  repository.NewAccountRepo(db),
		catRepo:  repository.NewCategoryRepo(db),
		txn: NewTransactionService(TxnDeps{
			Txns:     repository.NewTransactionRepo(db),
			Accounts: repository.NewAccountRepo(db),
			Cats:     repository.NewCategoryRepo(db),
			Closings: closings,
			Periods:  periods,
		}),
	}
}

// seedAccount / seedCat 建一个活跃账户与一个末级分类。
func (e *refundEnv) seedAccount(t *testing.T, name string) int64 {
	t.Helper()
	a := model.Account{Name: name, Type: model.AccountAsset, Status: model.StatusActive}
	mustOK(t, e.accRepo.Create(&a))
	return a.ID
}

func (e *refundEnv) seedCat(t *testing.T, kind model.CategoryKind, name string) int64 {
	t.Helper()
	c := model.Category{Kind: kind, Name: name}
	mustOK(t, e.catRepo.Create(&c))
	return c.ID
}

// expenseIn 支出模板。
func expenseIn(t *testing.T, accID, catID int64, amount, date string) TxnInput {
	t.Helper()
	return TxnInput{
		Kind: model.TxnExpense, Amount: dec(amount), AccountID: accID,
		CategoryID: catID, EntryDate: mustDate(t, date),
	}
}

// TestRefundValidation 覆盖任务 2.1 / specs/refunds「退款流水类型」「关联原支出」：
// 金额 0/负被拒；关联收入被拒；分类留空的独立退款可创建成功。
func TestRefundValidation(t *testing.T) {
	env := newRefundEnv(t)
	cash := env.seedAccount(t, "现金")
	lunch := env.seedCat(t, model.KindExpense, "午餐")
	salary := env.seedCat(t, model.KindIncome, "月薪")
	admin := adminUser()

	// 金额为 0 / 负数被拒
	for _, bad := range []string{"0", "-5"} {
		_, err := env.txn.Create(admin, TxnInput{
			Kind: model.TxnRefund, Amount: dec(bad), AccountID: cash,
			EntryDate: mustDate(t, "2026-10-05"),
		})
		if !errors.Is(err, ErrTxnAmountInvalid) {
			t.Fatalf("金额 %s 退款应被拒，got %v", bad, err)
		}
	}

	// 分类留空的独立退款创建成功，分类为 NULL
	r := mustCreate(t, env.txn, admin, TxnInput{
		Kind: model.TxnRefund, Amount: dec("30"), AccountID: cash,
		EntryDate: mustDate(t, "2026-10-05"),
	})
	if r.Kind != model.TxnRefund {
		t.Fatalf("kind = %s, want refund", r.Kind)
	}
	if r.CategoryID != nil {
		t.Fatalf("无关联退款分类应为 NULL, got %v", *r.CategoryID)
	}

	// 关联目标为收入 → 拒绝
	income := mustCreate(t, env.txn, admin, TxnInput{
		Kind: model.TxnIncome, Amount: dec("1000"), AccountID: cash,
		CategoryID: salary, EntryDate: mustDate(t, "2026-10-05"),
	})
	_, err := env.txn.Create(admin, TxnInput{
		Kind: model.TxnRefund, Amount: dec("50"), RelatedTxnID: income.ID,
		EntryDate: mustDate(t, "2026-10-05"),
	})
	if !errors.Is(err, ErrTxnRelatedInvalid) {
		t.Fatalf("关联收入应被拒，got %v", err)
	}

	// 关联目标不存在 → 拒绝
	_, err = env.txn.Create(admin, TxnInput{
		Kind: model.TxnRefund, Amount: dec("50"), RelatedTxnID: 99999,
		EntryDate: mustDate(t, "2026-10-05"),
	})
	if !errors.Is(err, ErrTxnRelatedInvalid) {
		t.Fatalf("关联不存在应被拒，got %v", err)
	}

	// 关联支出可正常创建（金额规则与普通流水一致）
	exp := mustCreate(t, env.txn, admin, expenseIn(t, cash, lunch, "100", "2026-10-05"))
	linked := mustCreate(t, env.txn, admin, TxnInput{
		Kind: model.TxnRefund, Amount: dec("50"), RelatedTxnID: exp.ID,
		EntryDate: mustDate(t, "2026-10-05"),
	})
	if linked.RelatedTxnID == nil || *linked.RelatedTxnID != exp.ID {
		t.Fatalf("退款应关联原支出 %d, got %v", exp.ID, linked.RelatedTxnID)
	}
}

// TestRefundDeleteConstraint 覆盖任务 2.2 / specs/refunds「关联支出的删除约束」：
// 存在关联退款时支出不可删；逐笔删完退款后恢复可删。
func TestRefundDeleteConstraint(t *testing.T) {
	env := newRefundEnv(t)
	cash := env.seedAccount(t, "现金")
	lunch := env.seedCat(t, model.KindExpense, "午餐")
	admin := adminUser()
	d := "2026-10-05"

	exp := mustCreate(t, env.txn, admin, expenseIn(t, cash, lunch, "100", d))
	r1 := mustCreate(t, env.txn, admin, TxnInput{
		Kind: model.TxnRefund, Amount: dec("30"), RelatedTxnID: exp.ID, AccountID: cash,
		EntryDate: mustDate(t, d),
	})
	mustCreate(t, env.txn, admin, TxnInput{
		Kind: model.TxnRefund, Amount: dec("70"), RelatedTxnID: exp.ID, AccountID: cash,
		EntryDate: mustDate(t, d),
	})

	// 有两笔关联退款 → 不可删
	if err := env.txn.Delete(admin, exp.ID); !errors.Is(err, ErrTxnRefundsExist) {
		t.Fatalf("带退款的支出应不可删，got %v", err)
	}

	// 删掉一笔后仍不可删
	mustOK(t, env.txn.Delete(admin, r1.ID))
	if err := env.txn.Delete(admin, exp.ID); !errors.Is(err, ErrTxnRefundsExist) {
		t.Fatalf("仍有退款时应不可删，got %v", err)
	}

	// 退款全部删除后可删
	rest, _, err := env.txn.List(admin, repository.TxnFilter{Kind: model.TxnRefund, Page: 1, PageSize: 20})
	mustOK(t, err)
	for _, r := range rest {
		mustOK(t, env.txn.Delete(admin, r.ID))
	}
	mustOK(t, env.txn.Delete(admin, exp.ID))
}

// TestRefundInheritance 覆盖任务 2.3 / specs/refunds「分类与账户继承」：
// 未显式给出账户/分类时按原支出预填；显式覆盖生效。
func TestRefundInheritance(t *testing.T) {
	env := newRefundEnv(t)
	cash := env.seedAccount(t, "现金")
	bank := env.seedAccount(t, "银行卡")
	lunch := env.seedCat(t, model.KindExpense, "午餐")
	dinner := env.seedCat(t, model.KindExpense, "晚餐")
	admin := adminUser()
	d := "2026-10-05"

	exp := mustCreate(t, env.txn, admin, expenseIn(t, cash, lunch, "100", d))

	// 未给账户/分类 → 继承原支出
	inherited := mustCreate(t, env.txn, admin, TxnInput{
		Kind: model.TxnRefund, Amount: dec("50"), RelatedTxnID: exp.ID,
		EntryDate: mustDate(t, d),
	})
	if inherited.AccountID != cash {
		t.Fatalf("应继承原账户 %d, got %d", cash, inherited.AccountID)
	}
	if inherited.CategoryID == nil || *inherited.CategoryID != lunch {
		t.Fatalf("应继承原分类 %d, got %v", lunch, inherited.CategoryID)
	}

	// 显式覆盖 → 用给定值
	over := mustCreate(t, env.txn, admin, TxnInput{
		Kind: model.TxnRefund, Amount: dec("20"), RelatedTxnID: exp.ID,
		AccountID: bank, CategoryID: dinner, EntryDate: mustDate(t, d),
	})
	if over.AccountID != bank {
		t.Fatalf("显式账户应生效，got %d", over.AccountID)
	}
	if over.CategoryID == nil || *over.CategoryID != dinner {
		t.Fatalf("显式分类应生效，got %v", over.CategoryID)
	}
}

// TestRefundClosedPeriodAndPermission 覆盖任务 2.4 / specs/refunds「退款账期归属」「退款的封账与权限约束」：
// 原支出账期封账不阻止开放期新退款；修改已封账期退款被拒；只读成员创建被拒。
func TestRefundClosedPeriodAndPermission(t *testing.T) {
	env := newRefundEnv(t)
	cash := env.seedAccount(t, "现金")
	lunch := env.seedCat(t, model.KindExpense, "午餐")
	admin := adminUser()
	viewer := &model.User{ID: 9, Username: "viewer", Role: model.RoleViewer, Status: model.StatusActive}

	// 原支出在 8 月；先在 8 月记一笔待封账的退款
	expAug := mustCreate(t, env.txn, admin, expenseIn(t, cash, lunch, "100", "2026-08-10"))
	rAug := mustCreate(t, env.txn, admin, TxnInput{
		Kind: model.TxnRefund, Amount: dec("30"), RelatedTxnID: expAug.ID,
		EntryDate: mustDate(t, "2026-08-12"),
	})

	// 封账 8 月
	mustOK(t, env.closings.Close(&model.PeriodClosing{
		PeriodKey: "2026-08", Status: model.ClosingClosed, ClosedBy: 1, ClosedAt: time.Now(),
	}))

	// 修改已封账期的退款 → 拒绝
	_, err := env.txn.Update(admin, rAug.ID, TxnInput{
		Kind: model.TxnRefund, Amount: dec("40"), RelatedTxnID: expAug.ID,
		EntryDate: mustDate(t, "2026-08-12"),
	})
	if !errors.Is(err, ErrTxnClosedPeriod) {
		t.Fatalf("修改已封账期退款应被拒，got %v", err)
	}

	// 删除已封账期的退款 → 拒绝
	if err := env.txn.Delete(admin, rAug.ID); !errors.Is(err, ErrTxnClosedPeriod) {
		t.Fatalf("删除已封账期退款应被拒，got %v", err)
	}

	// 原支出在已封账 8 月，开放的 10 月创建新退款 → 成功（不回填、不改原流水）
	mustCreate(t, env.txn, admin, TxnInput{
		Kind: model.TxnRefund, Amount: dec("70"), RelatedTxnID: expAug.ID,
		EntryDate: mustDate(t, "2026-10-05"),
	})

	// 只读成员创建退款 → 拒绝
	_, err = env.txn.Create(viewer, TxnInput{
		Kind: model.TxnRefund, Amount: dec("10"), AccountID: cash,
		EntryDate: mustDate(t, "2026-10-05"),
	})
	if !errors.Is(err, ErrTxnForbidden) {
		t.Fatalf("只读成员创建退款应被拒，got %v", err)
	}
}

// TestRefundedAmountAggregation 覆盖任务 4.2 / specs/refunds「部分与多次退款」：
// 列表聚合原支出的累计已退金额（两笔部分退款累计 100）。
func TestRefundedAmountAggregation(t *testing.T) {
	env := newRefundEnv(t)
	cash := env.seedAccount(t, "现金")
	lunch := env.seedCat(t, model.KindExpense, "午餐")
	admin := adminUser()
	d := "2026-10-05"

	exp := mustCreate(t, env.txn, admin, expenseIn(t, cash, lunch, "100", d))
	mustCreate(t, env.txn, admin, TxnInput{
		Kind: model.TxnRefund, Amount: dec("30"), RelatedTxnID: exp.ID, AccountID: cash,
		EntryDate: mustDate(t, d),
	})
	mustCreate(t, env.txn, admin, TxnInput{
		Kind: model.TxnRefund, Amount: dec("70"), RelatedTxnID: exp.ID, AccountID: cash,
		EntryDate: mustDate(t, d),
	})

	list, _, err := env.txn.List(admin, repository.TxnFilter{Page: 1, PageSize: 20})
	mustOK(t, err)
	var expRow *model.Transaction
	for i := range list {
		if list[i].ID == exp.ID {
			expRow = &list[i]
		}
	}
	if expRow == nil {
		t.Fatal("原支出不在列表中")
	}
	if !expRow.RefundedAmount.Equal(dec("100")) {
		t.Errorf("已退累计 = %s, want 100（30+70）", expRow.RefundedAmount)
	}
}

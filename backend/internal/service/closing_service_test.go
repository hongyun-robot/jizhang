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

// closingEnv 封账/归期联动测试环境（sqlite 内存库跑真实仓储）。
type closingEnv struct {
	periods *PeriodService
	closing *ClosingService
	txn     *TransactionService
	accRepo *repository.AccountRepo
	catRepo *repository.CategoryRepo
}

func newClosingEnv(t *testing.T, settings map[string]string) *closingEnv {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.Setting{}, &model.Account{}, &model.Category{}, &model.Transaction{},
		&model.PeriodClosing{}, &model.AuditLog{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	periods := NewPeriodService(&memSettings{m: settings})
	accRepo := repository.NewAccountRepo(db)
	catRepo := repository.NewCategoryRepo(db)
	closings := repository.NewClosingRepo(db)
	return &closingEnv{
		periods: periods,
		accRepo: accRepo,
		catRepo: catRepo,
		closing: NewClosingService(ClosingDeps{
			Closings: closings,
			Audits:   repository.NewAuditRepo(db),
			Periods:  periods,
			Stats:    repository.NewStatsRepo(db),
		}),
		txn: NewTransactionService(TxnDeps{
			Txns:     repository.NewTransactionRepo(db),
			Accounts: accRepo,
			Cats:     catRepo,
			Closings: closings,
			Periods:  periods,
		}),
	}
}

func mustOK(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// mustClose 封账并断言成功（Close 返回二值）。
func mustClose(t *testing.T, s *ClosingService, u *model.User, key string) {
	t.Helper()
	if _, err := s.Close(u, key); err != nil {
		t.Fatal(err)
	}
}

// seedAB 一个资产账户 + 一个末级支出分类，返回入参模板。
func (e *closingEnv) seedAB(t *testing.T) (TxnInput, model.Account) {
	t.Helper()
	acc := model.Account{Name: "现金", Type: model.AccountAsset, Status: model.StatusActive}
	mustOK(t, e.accRepo.Create(&acc))
	cat := model.Category{Kind: model.KindExpense, Name: "餐饮"}
	mustOK(t, e.catRepo.Create(&cat))
	return TxnInput{
		Kind: model.TxnExpense, Amount: dec("100"), AccountID: acc.ID,
		CategoryID: cat.ID, EntryDate: mustDate(t, "2026-10-05"),
	}, acc
}

// TestClosedPeriodBlocksWrites 覆盖 7.2 / specs「封账硬锁」：
// 进行中期不可封；封账后任何角色增删改被拒；解封恢复可写。
func TestClosedPeriodBlocksWrites(t *testing.T) {
	env := newClosingEnv(t, map[string]string{})
	env.closing.now = func() time.Time { return mustDate(t, "2026-11-05") }
	in, _ := env.seedAB(t)

	admin := &model.User{ID: 1, Username: "admin", Role: model.RoleAdmin, Status: model.StatusActive}
	member := &model.User{ID: 3, Username: "dazhi", Role: model.RoleMember, Status: model.StatusActive}
	recorder := &model.User{ID: 4, Username: "xiaoli", Role: model.RoleRecorder, Status: model.StatusActive}

	// 进行中的 2026-11 不可封
	if _, err := env.closing.Close(admin, "2026-11"); !errors.Is(err, ErrClosingNotEnded) {
		t.Fatalf("封进行中期 = %v, want ErrClosingNotEnded", err)
	}

	// 开放期可记账，归期正确
	tx, err := env.txn.Create(admin, in)
	mustOK(t, err)
	if tx.PeriodKey != "2026-10" {
		t.Fatalf("period_key = %s, want 2026-10", tx.PeriodKey)
	}

	// 封账成功；重复封账被拒
	if _, err := env.closing.Close(admin, "2026-10"); err != nil {
		t.Fatalf("封账 = %v, want nil", err)
	}
	if _, err := env.closing.Close(admin, "2026-10"); !errors.Is(err, ErrClosingAlreadyClosed) {
		t.Errorf("重复封账 = %v, want ErrClosingAlreadyClosed", err)
	}

	// 封账期内增删改被拒（任何角色）
	if _, err := env.txn.Create(admin, in); !errors.Is(err, ErrTxnClosedPeriod) {
		t.Errorf("管理员封账期新建 = %v, want ErrTxnClosedPeriod", err)
	}
	if _, err := env.txn.Create(member, in); !errors.Is(err, ErrTxnClosedPeriod) {
		t.Errorf("普通成员封账期新建 = %v, want ErrTxnClosedPeriod", err)
	}
	amt := in
	amt.Amount = dec("120")
	if _, err := env.txn.Update(admin, tx.ID, amt); !errors.Is(err, ErrTxnClosedPeriod) {
		t.Errorf("封账期修改 = %v, want ErrTxnClosedPeriod", err)
	}
	if _, err := env.txn.Update(recorder, tx.ID, amt); !errors.Is(err, ErrTxnClosedPeriod) {
		t.Errorf("记账员封账期修改 = %v, want ErrTxnClosedPeriod", err)
	}
	if err := env.txn.Delete(admin, tx.ID); !errors.Is(err, ErrTxnClosedPeriod) {
		t.Errorf("封账期删除 = %v, want ErrTxnClosedPeriod", err)
	}

	// 解封 → 恢复可写；重复解封被拒
	mustOK(t, env.closing.Reopen(admin, "2026-10"))
	if _, err := env.txn.Update(admin, tx.ID, amt); err != nil {
		t.Errorf("解封后修改 = %v, want nil", err)
	}
	if err := env.closing.Reopen(admin, "2026-10"); !errors.Is(err, ErrClosingNotClosed) {
		t.Errorf("重复解封 = %v, want ErrClosingNotClosed", err)
	}
}

// TestClosingAuditTrail 覆盖 specs「解封、修改、重新封账」场景：
// 封账与解封两步均留痕（谁、何时、哪一期、动作）。
func TestClosingAuditTrail(t *testing.T) {
	env := newClosingEnv(t, map[string]string{})
	env.closing.now = func() time.Time { return mustDate(t, "2026-11-05") }
	in, _ := env.seedAB(t)

	admin := &model.User{ID: 1, Username: "admin", Role: model.RoleAdmin, Status: model.StatusActive}
	recorder := &model.User{ID: 4, Username: "xiaoli", Role: model.RoleRecorder, Status: model.StatusActive}

	tx, err := env.txn.Create(admin, in)
	mustOK(t, err)
	mustClose(t, env.closing, admin, "2026-10")     // 封账
	mustOK(t, env.closing.Reopen(admin, "2026-10")) // 解封
	amt := in
	amt.Amount = dec("88")
	if _, err := env.txn.Update(recorder, tx.ID, amt); err != nil { // 修改流水
		t.Fatalf("解封后修改 = %v", err)
	}
	mustClose(t, env.closing, admin, "2026-10") // 重新封账

	items, total, err := env.closing.AuditLogs(1, 20)
	mustOK(t, err)
	if total != 3 || len(items) != 3 {
		t.Fatalf("审计条数 = %d/%d, want 3/3", len(items), total)
	}
	wantActions := []string{AuditActionClose, AuditActionReopen, AuditActionClose} // 最新在前
	for i, want := range wantActions {
		it := items[i]
		if it.Action != want {
			t.Errorf("第%d条动作 = %s, want %s", i+1, it.Action, want)
		}
		if it.Username != "admin" || it.UserID != 1 {
			t.Errorf("第%d条操作人 = %s(%d), want admin(1)", i+1, it.Username, it.UserID)
		}
		if it.PeriodKey != "2026-10" {
			t.Errorf("第%d条账期 = %s, want 2026-10", i+1, it.PeriodKey)
		}
		if it.CreatedAt.IsZero() {
			t.Errorf("第%d条时间为空", i+1)
		}
	}
}

// TestSettlementDayChangeKeepsHistory 覆盖 7.3 / specs「修改结算日不改历史」：
// 改日前历史行 period_key 不回溯、封账状态不变，新写入按新结算日归期。
func TestSettlementDayChangeKeepsHistory(t *testing.T) {
	env := newClosingEnv(t, map[string]string{"settlement_day": "1"})
	env.closing.now = func() time.Time { return mustDate(t, "2026-10-05") }
	in, _ := env.seedAB(t)

	admin := &model.User{ID: 1, Username: "admin", Role: model.RoleAdmin, Status: model.StatusActive}

	// 自然月时代写入 9/15 → 归 2026-09
	in.EntryDate = mustDate(t, "2026-09-15")
	in.Amount = dec("300")
	tx, err := env.txn.Create(admin, in)
	mustOK(t, err)
	if tx.PeriodKey != "2026-09" {
		t.Fatalf("改日前 period_key = %s, want 2026-09", tx.PeriodKey)
	}
	// 归档该期（9 月已结束）并封账
	mustClose(t, env.closing, admin, "2026-09")

	// 中途修改结算日 1 → 26（仅影响此后写入）
	mustOK(t, env.periods.SetSettlementDay(26))

	// 1) 历史行不被追溯改写
	refetched, err := env.txn.GetByID(tx.ID)
	mustOK(t, err)
	if refetched.PeriodKey != "2026-09" {
		t.Errorf("改日后历史 period_key = %s, want 2026-09（不回溯）", refetched.PeriodKey)
	}
	if !refetched.EntryDate.Equal(mustDate(t, "2026-09-15")) {
		t.Errorf("改日后历史 entry_date = %v, want 2026-09-15", refetched.EntryDate)
	}

	// 2) 历史期封账状态不变：仍锁写
	if _, err := env.txn.Create(admin, in); !errors.Is(err, ErrTxnClosedPeriod) {
		t.Errorf("改日后历史期新建 = %v, want ErrTxnClosedPeriod", err)
	}
	closed, err := env.closing.List()
	mustOK(t, err)
	found := false
	for _, c := range closed {
		if c.PeriodKey == "2026-09" {
			found = true
		}
	}
	if !found {
		t.Error("改日后 2026-09 封账记录丢失")
	}

	// 3) 新写入按新结算日（10/05 在新规则下归 [9/26,10/26) 期 → 键 2026-10）
	nx := in
	nx.EntryDate = mustDate(t, "2026-10-05")
	nx.Amount = dec("50")
	got, err := env.txn.Create(admin, nx)
	mustOK(t, err)
	if got.PeriodKey != "2026-10" {
		t.Errorf("改日后新写 period_key = %s, want 2026-10（新规则、不撞历史期）", got.PeriodKey)
	}
}

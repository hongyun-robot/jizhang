package service

import (
	"database/sql"
	"errors"
	"fmt"
	driver "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"jizhang/internal/liability"
	"jizhang/internal/model"
	"jizhang/internal/repository"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"
)

func TestLiabilityMySQL(t *testing.T) {
	dsn := os.Getenv("LIABILITY_MYSQL_DSN")
	if dsn == "" {
		t.Skip("set LIABILITY_MYSQL_DSN for isolated migration and concurrency verification")
	}
	cfg, err := driver.ParseDSN(dsn)
	mustOK(t, err)
	cfg.DBName = ""
	cfg.MultiStatements = true
	cfg.ParseTime = true
	cfg.Loc = liability.Location
	root, err := sql.Open("mysql", cfg.FormatDSN())
	mustOK(t, err)
	t.Cleanup(func() { _ = root.Close() })
	mustOK(t, root.Ping())
	name := fmt.Sprintf("jizhang_liability_test_%d", time.Now().UnixNano())
	_, err = root.Exec("CREATE DATABASE `" + name + "` CHARACTER SET utf8mb4")
	mustOK(t, err)
	t.Cleanup(func() {
		_, e := root.Exec("DROP DATABASE `" + name + "`")
		if e != nil {
			t.Error(e)
		}
	})
	cfg.DBName = name
	db, err := gorm.Open(mysql.Open(cfg.FormatDSN()), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	mustOK(t, err)
	pool, err := db.DB()
	mustOK(t, err)
	t.Cleanup(func() { _ = pool.Close() })
	migration := func(file string) string {
		b, e := os.ReadFile(filepath.Join("..", "..", "migrations", file))
		mustOK(t, e)
		return string(b)
	}
	files, err := filepath.Glob(filepath.Join("..", "..", "migrations", "*.up.sql"))
	mustOK(t, err)
	for _, file := range files {
		b, e := os.ReadFile(file)
		mustOK(t, e)
		mustOK(t, db.Exec(string(b)).Error)
	}
	t.Run("default-accounts-and-platforms", func(t *testing.T) {
		var accounts []model.Account
		mustOK(t, db.Order("sort").Find(&accounts).Error)
		if len(accounts) != 9 {
			t.Fatal("fresh migration skipped original assets", len(accounts))
		}
		expected := map[string]string{"花呗": "huabei", "京东白条": "jd_baitiao", "抖音月付": "douyin_monthly", "美团月付": "meituan_monthly"}
		for _, a := range accounts {
			if kind, ok := expected[a.Name]; ok {
				if a.Type != model.AccountLiability || a.LiabilityType != kind || a.BillingEnabled || !a.OpeningBalance.IsZero() {
					t.Fatal(a)
				}
				delete(expected, a.Name)
			}
		}
		if len(expected) != 0 {
			t.Fatal(expected)
		}
		up := migration("000009_default_credit_accounts_and_platforms.up.sql")
		down := migration("000009_default_credit_accounts_and_platforms.down.sql")
		mustOK(t, db.Exec(up).Error)
		var count int64
		mustOK(t, db.Model(&model.Account{}).Count(&count).Error)
		if count != 9 {
			t.Fatal("repeated migration duplicated accounts", count)
		}
		mustOK(t, db.Model(&model.VoucherPlatform{}).Count(&count).Error)
		if count != 9 {
			t.Fatal("default catalog missing or duplicated", count)
		}
		rollback := errors.New("restore fresh fixture")
		err := db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Where("1 = 1").Delete(&model.Account{}).Error; err != nil {
				return err
			}
			if err := tx.Where("1 = 1").Delete(&model.VoucherPlatform{}).Error; err != nil {
				return err
			}
			custom := model.Account{Name: "自定义银行卡", Type: model.AccountAsset, Status: model.StatusDisabled, OpeningBalance: dec("123")}
			if err := tx.Create(&custom).Error; err != nil {
				return err
			}
			sameName := model.Account{Name: "花呗", Type: model.AccountAsset, Status: model.StatusDisabled, OpeningBalance: dec("42")}
			if err := tx.Create(&sameName).Error; err != nil {
				return err
			}
			platform := model.VoucherPlatform{Name: "美团", CreatedAt: vdate(t, "2026-01-01")}
			if err := tx.Create(&platform).Error; err != nil {
				return err
			}
			if err := tx.Exec(up).Error; err != nil {
				return err
			}
			if err := tx.Exec(up).Error; err != nil {
				return err
			}
			if err := tx.Exec(down).Error; err != nil {
				return err
			}
			if err := tx.First(&sameName, sameName.ID).Error; err != nil {
				return err
			}
			if sameName.Type != model.AccountAsset || sameName.Status != model.StatusDisabled || !sameName.OpeningBalance.Equal(dec("42")) {
				t.Fatal("migration overwrote user account", sameName)
			}
			if err := tx.First(&platform, platform.ID).Error; err != nil {
				return err
			}
			if platform.Name != "美团" || !platform.CreatedAt.Equal(vdate(t, "2026-01-01")) {
				t.Fatal("catalog did not preserve identity", platform)
			}
			if err := tx.Model(&model.Account{}).Count(&count).Error; err != nil {
				return err
			}
			if count != 5 {
				t.Fatal("existing book recreated base accounts or duplicated defaults", count)
			}
			if err := tx.Model(&model.VoucherPlatform{}).Count(&count).Error; err != nil {
				return err
			}
			if count != 9 {
				t.Fatal("catalog upgrade was not idempotent", count)
			}
			return rollback
		})
		if !errors.Is(err, rollback) {
			t.Fatal(err)
		}
	})
	t.Run("database-catalogs-and-categories", func(t *testing.T) {
		var types []model.LiabilityAccountType
		mustOK(t, db.Order("sort,code").Find(&types).Error)
		if len(types) != 6 || types[0].Code != "credit_card" {
			t.Fatal(types)
		}
		var categoryCount int64
		mustOK(t, db.Model(&model.Category{}).Count(&categoryCount).Error)
		if categoryCount != 36 {
			t.Fatal("empty database categories incomplete", categoryCount)
		}
		var parentCount int64
		mustOK(t, db.Model(&model.Category{}).Where("parent_id IS NULL").Count(&parentCount).Error)
		if parentCount != 11 {
			t.Fatal(parentCount)
		}
		var parent model.Category
		mustOK(t, db.Where("kind = ? AND name = ? AND parent_id IS NULL", model.KindExpense, "餐饮").First(&parent).Error)
		mustOK(t, db.Model(&parent).Update("name", "自定义餐饮").Error)
		mustOK(t, db.Model(&model.LiabilityAccountType{}).Where("code = ?", "huabei").Update("name", "自定义花呗类型").Error)
		custom := model.LiabilityAccountType{Code: "database_custom", Name: "数据库自定义月付", Sort: 10}
		mustOK(t, db.Create(&custom).Error)
		mustOK(t, db.Exec(migration("000010_database_catalogs.up.sql")).Error)
		mustOK(t, db.Model(&model.Category{}).Count(&categoryCount).Error)
		if categoryCount != 36 {
			t.Fatal("repeat migration changed category tree", categoryCount)
		}
		mustOK(t, db.First(&parent, parent.ID).Error)
		if parent.Name != "自定义餐饮" {
			t.Fatal("migration overwrote category", parent)
		}
		var customized model.LiabilityAccountType
		mustOK(t, db.First(&customized, "code = ?", "huabei").Error)
		if customized.Name != "自定义花呗类型" {
			t.Fatal(customized)
		}
		mustOK(t, liability.ValidateAccountType(db, custom.Code))
		mustOK(t, db.Exec(migration("000010_database_catalogs.down.sql")).Error)
		mustOK(t, liability.ValidateAccountType(db, custom.Code))
	})
	mustOK(t, db.Exec(migration("000008_liability_billing.down.sql")).Error)
	mustOK(t, db.Exec("INSERT INTO accounts(name,type,opening_balance,status) VALUES ('default-card','liability',0,'active')").Error)
	mustOK(t, db.Exec(migration("000008_liability_billing.up.sql")).Error)
	var defaultCard model.Account
	mustOK(t, db.Where("name = ?", "default-card").First(&defaultCard).Error)
	if defaultCard.BillingEnabled || !defaultCard.OpeningBalance.IsZero() {
		t.Fatal("default account changed")
	}
	mustOK(t, db.Exec("INSERT INTO users(id,username,password_hash,role,status) VALUES(1,'liability-admin','test','admin','active'),(3,'liability-member','test','member','active')").Error)
	base := seedVoucherEnv(t, db)
	// The earlier downgrade/re-upgrade exercise resets type columns on the
	// legacy defaults. Keep that legacy fixture with its administrator before
	// testing a new member's independent, one-time template initialization.
	mustOK(t, db.Model(&model.Account{}).Where("type = ? AND owner_id IS NULL", model.AccountLiability).Update("owner_id", 1).Error)
	t.Run("personal-defaults-concurrency", func(t *testing.T) {
		svc := NewAccountService(repository.NewAccountRepo(db))
		member := &model.User{ID: 3, Role: model.RoleMember}
		var wg sync.WaitGroup
		errs := make(chan error, 8)
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() { defer wg.Done(); _, err := svc.ListFor(member); errs <- err }()
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			mustOK(t, err)
		}
		var personal []model.Account
		mustOK(t, db.Where("type = ? AND owner_id = ?", model.AccountLiability, member.ID).Find(&personal).Error)
		if len(personal) != 5 {
			t.Fatal("concurrent initialization duplicated profiles", len(personal))
		}
		mustOK(t, svc.DeleteFor(member, personal[0].ID))
		_, err := svc.ListFor(member)
		mustOK(t, err)
		var count int64
		mustOK(t, db.Model(&model.Account{}).Where("type = ? AND owner_id = ?", model.AccountLiability, member.ID).Count(&count).Error)
		if count != 4 {
			t.Fatal("deleted default was recreated", count)
		}
		_, err = svc.UpdateFor(adminUser(), personal[1].ID, CreateAccountInput{Name: "foreign", Type: model.AccountLiability}, model.StatusActive)
		if !errors.Is(err, ErrTxnForbidden) {
			t.Fatal("administrator changed foreign profile", err)
		}
	})
	owner := int64(1)
	for _, scenario := range []struct {
		name string
		run  func(*testing.T, *liabilityEnv)
	}{
		{"repayment-day-update", checkRepaymentDayUpdatesUnissuedStatement},
		{"repayment-day-legacy-pending", checkRepaymentChangeSupersedesLegacyPendingRule},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			settingsEnv := &liabilityEnv{voucherEnv: base, now: vdate(t, "2026-10-15")}
			settingsEnv.svcL = NewLiabilityService(db, settingsEnv.txn)
			settingsEnv.svcL.Now = func() time.Time { return settingsEnv.now }
			scenario.run(t, settingsEnv)
		})
	}
	card := model.Account{Name: "mysql花呗", Type: model.AccountLiability, OwnerID: &owner, Status: model.StatusActive}
	mustOK(t, db.Create(&card).Error)
	e := &liabilityEnv{voucherEnv: base, card: card.ID, now: vdate(t, "2026-10-15")}
	e.svcL = NewLiabilityService(db, e.txn)
	e.svcL.Now = func() time.Time { return e.now }
	_, err = e.svcL.Settings(adminUser(), card.ID, LiabilitySettingsInput{LiabilityType: "huabei", StatementDay: 31, RepaymentDay: 10, RepaymentMonthOffset: 1})
	mustOK(t, err)
	e.spend(t, "500", "2026-10-15")
	e.now = vdate(t, "2026-11-01")
	_, err = e.svcL.EnsureDue(e.now)
	mustOK(t, err)
	req := LiabilityRepaymentInput{AccountID: card.ID, FromAccountID: e.account, Amount: dec("500"), EntryDate: "2026-11-01", RequestKey: uuid.NewString()}
	run := func(fn func() error) {
		t.Helper()
		var wg sync.WaitGroup
		start := make(chan struct{})
		failures := make(chan error, 8)
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				if e := fn(); e != nil {
					failures <- e
				}
			}()
		}
		close(start)
		wg.Wait()
		close(failures)
		for e := range failures {
			t.Error(e)
		}
	}
	run(func() error { _, err := e.svcL.Repay(adminUser(), req); return err })
	var count int64
	mustOK(t, db.Model(&model.Transaction{}).Where("kind = ? AND transfer_account_id = ?", model.TxnTransfer, card.ID).Count(&count).Error)
	if count != 1 {
		t.Fatal("duplicate transfer", count)
	}
	_, err = e.svcL.CreatePlan(adminUser(), InstallmentInput{AccountID: card.ID, Amount: dec("3000"), EntryDate: "2026-11-02", CategoryID: e.category, TermCount: 3, FeeMode: "per_term", FeeInput: dec("20"), FeeAccountID: card.ID, FeeCategoryID: e.category, RequestKey: uuid.NewString()})
	mustOK(t, err)
	e.now = vdate(t, "2026-12-01")
	run(func() error { _, err := e.svcL.EnsureDue(e.now); return err })
	d := e.state(t)
	if !d.Summary.TotalDebt.Equal(dec("3020")) {
		t.Fatal(d.Summary)
	}
	// Distinct money operations compete for the same remaining principal.
	plan := d.Plans[0]
	start := make(chan struct{})
	results := make(chan error, 3)
	go func() {
		<-start
		_, e := e.svcL.Repay(adminUser(), LiabilityRepaymentInput{AccountID: card.ID, FromAccountID: e.account, Amount: dec("1020"), EntryDate: "2026-12-01", RequestKey: uuid.NewString()})
		results <- e
	}()
	go func() {
		<-start
		_, err := e.txn.Create(adminUser(), TxnInput{Kind: model.TxnRefund, AccountID: card.ID, RelatedTxnID: plan.SourceTxnID, Amount: dec("500"), EntryDate: e.now})
		results <- err
	}()
	go func() {
		<-start
		_, err := e.svcL.Settle(adminUser(), plan.ID, SettlementInput{Version: plan.Version, Principal: dec("3000"), FeeAmount: dec("15"), FeeAccountID: e.account, FromAccountID: e.account, EntryDate: "2026-12-01", RequestKey: uuid.NewString()})
		results <- err
	}()
	close(start)
	for i := 0; i < 3; i++ {
		err := <-results
		if err != nil && !errors.Is(err, liability.ErrConflict) {
			t.Fatal(err)
		}
	}
	d = e.state(t)
	for _, x := range d.Items {
		if x.Remaining.IsNegative() {
			t.Fatal("concurrent operations overallocated principal", x)
		}
	}
	if d.Plans[0].Status == "settled" {
		if !d.Summary.Balance.Equal(dec("1520")) {
			t.Fatal(d.Summary)
		}
	} else if !d.Summary.TotalDebt.Equal(dec("1500")) {
		t.Fatal(d.Summary)
	}
	// Fee generation and a real payment share the closing lock, so a frozen
	// period either includes a committed posting or rejects it completely.
	card2 := model.Account{Name: "closing-race", Type: model.AccountLiability, OwnerID: &owner, Status: model.StatusActive}
	mustOK(t, db.Create(&card2).Error)
	_, err = e.svcL.Settings(adminUser(), card2.ID, LiabilitySettingsInput{LiabilityType: "credit_card", StatementDay: 31, RepaymentDay: 10, RepaymentMonthOffset: 1})
	mustOK(t, err)
	_, err = e.svcL.CreatePlan(adminUser(), InstallmentInput{AccountID: card2.ID, Amount: dec("100"), EntryDate: "2026-12-01", CategoryID: e.category, TermCount: 3, FeeMode: "per_term", FeeInput: dec("20"), FeeAccountID: card2.ID, FeeCategoryID: e.category, RequestKey: uuid.NewString()})
	mustOK(t, err)
	e.now = vdate(t, "2027-01-01")
	e.closing.now = func() time.Time { return e.now }
	start = make(chan struct{})
	results = make(chan error, 3)
	go func() { <-start; _, err := e.closing.Close(adminUser(), "2026-12"); results <- err }()
	go func() {
		<-start
		_, err := e.svcL.Repay(adminUser(), LiabilityRepaymentInput{AccountID: card2.ID, FromAccountID: e.account, Amount: dec("10"), EntryDate: "2026-12-20", RequestKey: uuid.NewString()})
		results <- err
	}()
	go func() { <-start; _, err := e.svcL.EnsureDue(e.now); results <- err }()
	close(start)
	for i := 0; i < 3; i++ {
		err := <-results
		if err != nil && !errors.Is(err, ErrTxnClosedPeriod) {
			t.Fatal(err)
		}
	}
	before, err := repository.NewStatsRepo(db).SumByPeriod("2026-12")
	mustOK(t, err)
	run(func() error { _, err := e.svcL.EnsureDue(e.now); return err })
	after, err := repository.NewStatsRepo(db).SumByPeriod("2026-12")
	mustOK(t, err)
	if !before.Expense.Equal(after.Expense) || !before.Income.Equal(after.Income) {
		t.Fatal("ensure rewrote frozen money", before, after)
	}
	state2, err := e.svcL.Detail(adminUser(), card2.ID)
	mustOK(t, err)
	mustOK(t, liability.CheckInvariant(db, state2.Account))
	_, err = e.svcL.Repay(adminUser(), LiabilityRepaymentInput{AccountID: card2.ID, FromAccountID: e.account, Amount: dec("10"), EntryDate: "2026-12-20", RequestKey: uuid.NewString()})
	if !errors.Is(err, ErrTxnClosedPeriod) {
		t.Fatal("payment bypassed closing", err)
	}
	if err := db.Exec(migration("000008_liability_billing.down.sql")).Error; err == nil {
		t.Fatal("down erased liability history")
	}
	if !db.Migrator().HasTable(&model.InstallmentPlan{}) {
		t.Fatal("guard failed to preserve schema")
	}
	t.Run("principal-remainder-preference-and-rollback", func(t *testing.T) {
		a, err := repository.NewAccountRepo(db).GetByID(card2.ID)
		mustOK(t, err)
		if a.PrincipalRemainder != "last" {
			t.Fatal("migration did not preserve the legacy default", a)
		}
		svc := NewAccountService(repository.NewAccountRepo(db))
		_, err = svc.UpdateFor(adminUser(), a.ID, CreateAccountInput{Name: a.Name, Type: a.Type, OpeningBalance: a.OpeningBalance, Sort: a.Sort, PrincipalRemainder: "first"}, a.Status)
		mustOK(t, err)
		a, err = repository.NewAccountRepo(db).GetByID(a.ID)
		mustOK(t, err)
		if a.PrincipalRemainder != "first" {
			t.Fatal("MySQL preference did not persist", a)
		}
		var beforeTerms, afterTerms []model.InstallmentTerm
		mustOK(t, db.Order("id").Find(&beforeTerms).Error)
		balance := a.Balance
		mustOK(t, db.Exec(migration("000013_installment_principal_remainder.down.sql")).Error)
		if db.Migrator().HasColumn(&model.Account{}, "principal_remainder") {
			t.Fatal("rollback retained the preference column")
		}
		mustOK(t, db.Order("id").Find(&afterTerms).Error)
		if !reflect.DeepEqual(beforeTerms, afterTerms) {
			t.Fatal("rollback changed stored installment amounts")
		}
		mustOK(t, db.Exec(migration("000013_installment_principal_remainder.up.sql")).Error)
		a, err = repository.NewAccountRepo(db).GetByID(a.ID)
		mustOK(t, err)
		if a.PrincipalRemainder != "last" || !a.Balance.Equal(balance) {
			t.Fatal("reapplying migration changed money or the legacy default", a)
		}
	})
}

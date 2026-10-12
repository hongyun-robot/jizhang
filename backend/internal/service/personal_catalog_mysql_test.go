package service

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	driver "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"jizhang/internal/model"
	"jizhang/internal/repository"
	"strings"
	"sync"
)

func catalogMySQL(t *testing.T) (*gorm.DB, func(string)) {
	t.Helper()
	dsn := os.Getenv("PERSONAL_CATALOG_MYSQL_DSN")
	if dsn == "" {
		dsn = os.Getenv("VOUCHER_MYSQL_DSN")
	}
	if dsn == "" {
		dsn = os.Getenv("LIABILITY_MYSQL_DSN")
	}
	if dsn == "" {
		t.Skip("set PERSONAL_CATALOG_MYSQL_DSN for isolated catalog migration tests")
	}
	cfg, err := driver.ParseDSN(dsn)
	mustOK(t, err)
	cfg.DBName, cfg.MultiStatements, cfg.ParseTime = "", true, true
	root, err := sql.Open("mysql", cfg.FormatDSN())
	mustOK(t, err)
	mustOK(t, root.Ping())
	name := fmt.Sprintf("jizhang_catalog_test_%d", time.Now().UnixNano())
	_, err = root.Exec("CREATE DATABASE `" + name + "` CHARACTER SET utf8mb4")
	mustOK(t, err)
	t.Cleanup(func() {
		_, e := root.Exec("DROP DATABASE `" + name + "`")
		if e != nil {
			t.Error(e)
		}
		_ = root.Close()
	})
	cfg.DBName = name
	db, err := gorm.Open(mysql.Open(cfg.FormatDSN()), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	mustOK(t, err)
	pool, err := db.DB()
	mustOK(t, err)
	t.Cleanup(func() { _ = pool.Close() })
	migrate := func(file string) {
		t.Helper()
		b, e := os.ReadFile(filepath.Join("..", "..", "migrations", file))
		mustOK(t, e)
		mustOK(t, db.Exec(string(b)).Error)
	}
	files, err := filepath.Glob(filepath.Join("..", "..", "migrations", "*.up.sql"))
	mustOK(t, err)
	for _, file := range files {
		if filepath.Base(file) >= "000012" {
			break
		}
		migrate(filepath.Base(file))
	}
	return db, migrate
}

func TestPersonalCatalogMySQLMigration(t *testing.T) {
	t.Run("empty-up-down", func(t *testing.T) {
		db, migrate := catalogMySQL(t)
		migrate("000012_personal_catalogs.up.sql")
		for _, table := range []string{"account_templates", "category_templates", "account_create_requests", "catalog_account_ownership"} {
			if !db.Migrator().HasTable(table) {
				t.Fatal(table)
			}
		}
		migrate("000012_personal_catalogs.down.sql")
		if db.Migrator().HasColumn("categories", "owner_id") {
			t.Fatal("down retained catalog ownership")
		}
	})
	t.Run("history-and-guard", func(t *testing.T) {
		db, migrate := catalogMySQL(t)
		mustOK(t, db.Exec("INSERT INTO users(id,username,password_hash,role,status) VALUES (1,'catalog-admin','hash','admin','active'),(2,'catalog-member','hash','member','active'),(3,'catalog-viewer','hash','viewer','active')").Error)
		mustOK(t, db.Exec("UPDATE accounts SET owner_id=1 WHERE type='liability'").Error)
		mustOK(t, db.Exec("UPDATE accounts SET owner_id=2 WHERE id=5; UPDATE accounts SET owner_id=3 WHERE id=6").Error)
		mustOK(t, db.Exec("INSERT INTO accounts(id,name,type,opening_balance,status) VALUES(10,'单人资产','asset',100,'active'),(11,'多人资产','asset',200,'active'),(12,'链路甲','asset',300,'active'),(13,'链路乙','asset',400,'active'),(14,'混合负债来源','asset',500,'active')").Error)
		mustOK(t, db.Exec("INSERT INTO transactions(kind,amount,account_id,transfer_account_id,category_id,entry_date,period_key,created_by) VALUES ('expense',7,10,NULL,1,'2026-10-01','2026-10',2),('expense',3,11,NULL,1,'2026-10-01','2026-10',1),('expense',4,11,NULL,1,'2026-10-01','2026-10',2),('transfer',10,12,13,NULL,'2026-10-01','2026-10',2),('transfer',5,14,5,NULL,'2026-10-01','2026-10',2),('transfer',5,14,6,NULL,'2026-10-01','2026-10',3)").Error)
		mustOK(t, db.Exec("INSERT INTO recurring_rules(amount,freq,account_id,category_id,start_date,next_run_date,status,created_by) VALUES(10,'monthly',11,1,'2026-10-01','2026-11-01','enabled',2)").Error)
		voucher := model.Voucher{Name: "旧券", Platform: "共享商家", PaidAmount: dec("10"), PurchaseDate: vdate(t, "2026-10-01"), AccountID: 10, CategoryID: 1, TotalUnits: 1, CreatedBy: 2, CreateRequestKey: uuid.NewString(), CreatePayloadHash: "legacy-hash", ScheduleVersion: 1, Revision: 1}
		mustOK(t, db.Create(&voucher).Error)
		mustOK(t, db.Exec("INSERT INTO transactions(id,kind,amount,account_id,category_id,entry_date,period_key,created_by) VALUES(7,'expense',10,5,1,'2026-10-01','2026-10',2)").Error)
		mustOK(t, db.Exec("INSERT INTO liability_bill_rules(id,account_id,version,statement_day,repayment_day,repayment_month_offset,effective_from) VALUES(1,5,1,31,10,1,'2026-10-01')").Error)
		mustOK(t, db.Exec("INSERT INTO liability_statements(id,account_id,kind,bill_key,rule_id,cycle_start,cycle_end,due_date,snapshot_amount) VALUES(1,5,'monthly','fixture',1,'2026-10-01','2026-10-31','2026-11-10',10)").Error)
		mustOK(t, db.Exec("INSERT INTO installment_plans(id,source_txn_id,account_id,principal,term_count,fee_mode,fee_input,fee_account_id,fee_category_id,created_by,version,status) VALUES(1,7,5,10,1,'per_term',1,14,1,2,1,'active')").Error)
		mustOK(t, db.Exec("INSERT INTO installment_terms(plan_id,number,statement_id,principal,fee_amount,fee_date,status,fee_status) VALUES(1,1,1,10,1,'2026-10-31','scheduled','scheduled')").Error)
		before, err := repository.NewAccountRepo(db).List()
		mustOK(t, err)
		migrate("000012_personal_catalogs.up.sql")
		after, err := repository.NewAccountRepo(db).List()
		mustOK(t, err)
		balances := map[int64]string{}
		for _, a := range before {
			balances[a.ID] = a.Balance.StringFixed(2)
		}
		for _, a := range after {
			if b, ok := balances[a.ID]; ok && b != a.Balance.StringFixed(2) {
				t.Fatalf("account %d: %s -> %s", a.ID, b, a.Balance)
			}
		}
		var report []struct {
			AccountID int64
			OwnerID   int64
			Reason    string
		}
		mustOK(t, db.Table("catalog_account_ownership").Order("account_id").Find(&report).Error)
		expected := map[int64]int64{10: 2, 11: 1, 12: 2, 13: 2, 14: 1, 5: 2, 6: 3}
		for _, r := range report {
			if owner, ok := expected[r.AccountID]; ok && owner != r.OwnerID {
				t.Fatalf("ownership: %+v expected %d", r, owner)
			}
		}
		var foreign int64
		mustOK(t, db.Raw("SELECT COUNT(*) FROM categories c JOIN categories p ON p.id=c.parent_id WHERE c.owner_id<>p.owner_id OR c.kind<>p.kind").Scan(&foreign).Error)
		if foreign != 0 {
			t.Fatal("invalid parent mapping", foreign)
		}
		mustOK(t, db.Raw("SELECT COUNT(*) FROM transactions t JOIN accounts a ON a.id=t.account_id JOIN categories c ON c.id=t.category_id WHERE c.owner_id<>a.owner_id").Scan(&foreign).Error)
		if foreign != 0 {
			t.Fatal("invalid posting classification", foreign)
		}
		var blocked string
		mustOK(t, db.Raw("SELECT blocked_reason FROM recurring_rules LIMIT 1").Scan(&blocked).Error)
		if blocked == "" {
			t.Fatal("foreign recurring rule was not paused")
		}
		mustOK(t, db.Raw("SELECT blocked_reason FROM installment_terms LIMIT 1").Scan(&blocked).Error)
		if blocked == "" {
			t.Fatal("foreign future fee source was not blocked")
		}
		var mismatch int64
		mustOK(t, db.Raw("SELECT COUNT(*) FROM vouchers v JOIN accounts a ON a.id=v.account_id JOIN categories c ON c.id=v.category_id WHERE a.owner_id<>c.owner_id").Scan(&mismatch).Error)
		if mismatch != 0 {
			t.Fatal("voucher category mismatch")
		}
		periods := NewPeriodService(repository.NewSettingRepo(db))
		txns := NewTransactionService(TxnDeps{Txns: repository.NewTransactionRepo(db), Accounts: repository.NewAccountRepo(db), Cats: repository.NewCategoryRepo(db), Closings: repository.NewClosingRepo(db), Periods: periods})
		member := &model.User{ID: 2, Role: model.RoleMember}
		legacy, _, e := txns.List(member, repository.TxnFilter{})
		mustOK(t, e)
		found := false
		for _, row := range legacy {
			if row.ID == 5 {
				found = true
				if !row.HistoricalReadonly || row.AccountNameOverride != "历史关联账户" {
					t.Fatal("legacy transfer exposed external account", row)
				}
			}
		}
		if !found {
			t.Fatal("own legacy repayment disappeared")
		}
		export, e := txns.ExportCSV(member, repository.TxnFilter{})
		mustOK(t, e)
		if strings.Contains(string(export), "混合负债来源") {
			t.Fatal("foreign account name in export")
		}
		if e := txns.Delete(member, 5); !errors.Is(e, ErrTxnForbidden) {
			t.Fatal("legacy cross-owner mutation", e)
		}
		migrate("000012_personal_catalogs.down.sql")
		restored, err := repository.NewAccountRepo(db).List()
		mustOK(t, err)
		if len(restored) != len(before) {
			t.Fatal("rollback did not restore account count")
		}
		migrate("000012_personal_catalogs.up.sql")
		mustOK(t, db.Exec("UPDATE categories SET name='个人修改' WHERE owner_id=2 LIMIT 1").Error)
		down, e := os.ReadFile(filepath.Join("..", "..", "migrations", "000012_personal_catalogs.down.sql"))
		mustOK(t, e)
		if err := db.Exec(string(down)).Error; err == nil {
			t.Fatal("rollback erased a personal edit")
		}
		if !db.Migrator().HasTable("category_templates") {
			t.Fatal("guard partially removed schema")
		}
	})
}

func TestPersonalCatalogMySQLConcurrentCreate(t *testing.T) {
	db, migrate := catalogMySQL(t)
	migrate("000012_personal_catalogs.up.sql")
	mustOK(t, db.Exec("INSERT INTO users(id,username,password_hash,role,status) VALUES(1,'create-member','hash','member','active')").Error)
	actor := &model.User{ID: 1, Role: model.RoleMember, Status: model.StatusActive}
	svc := NewAccountService(repository.NewAccountRepo(db))
	in := CreateAccountInput{Name: "重试资产", Type: model.AccountAsset, OpeningBalance: dec("20"), RequestKey: uuid.NewString()}
	var wg sync.WaitGroup
	ids := make(chan int64, 16)
	errs := make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			a, e := svc.CreateFor(actor, in)
			if e != nil {
				errs <- e
				return
			}
			ids <- a.ID
		}()
	}
	wg.Wait()
	close(ids)
	close(errs)
	for e := range errs {
		t.Fatal(e)
	}
	var first int64
	for id := range ids {
		if first == 0 {
			first = id
		} else if first != id {
			t.Fatal("duplicated account", first, id)
		}
	}
	mustOK(t, svc.DeleteFor(actor, first))
	if _, e := svc.CreateFor(actor, in); !errors.Is(e, ErrAccountCreateConflict) {
		t.Fatal("deleted key recreated account", e)
	}
}

func TestPersonalCatalogMySQLInitialization(t *testing.T) {
	db, migrate := catalogMySQL(t)
	migrate("000012_personal_catalogs.up.sql")
	mustOK(t, db.Exec("INSERT INTO users(id,username,password_hash,role,status) VALUES(1,'init-admin','hash','admin','active'),(2,'init-member','hash','member','active')").Error)
	actor := &model.User{ID: 1, Role: model.RoleAdmin, Status: model.StatusActive}
	accounts := NewAccountService(repository.NewAccountRepo(db))
	categories := NewCategoryService(repository.NewCategoryRepo(db))
	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := accounts.ListFor(actor)
			if e == nil {
				_, e = categories.TreeFor(actor)
			}
			if e != nil {
				errs <- e
			}
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Fatal(e)
	}
	var count int64
	mustOK(t, db.Table("accounts").Where("owner_id = ?", 1).Count(&count).Error)
	if count != 9 {
		t.Fatal("duplicate default accounts", count)
	}
	rows, e := accounts.ListFor(actor)
	mustOK(t, e)
	cash := rows[0]
	mustOK(t, accounts.DeleteFor(actor, cash.ID))
	again, e := accounts.ListFor(actor)
	mustOK(t, e)
	for _, a := range again {
		if a.ID == cash.ID || a.Name == cash.Name {
			t.Fatal("deleted asset reseeded")
		}
	}
	failure := errors.New("injected initialization failure")
	mustOK(t, db.Callback().Create().Before("gorm:create").Register("catalog_init_failure", func(tx *gorm.DB) {
		if tx.Statement.Table == "categories" {
			tx.AddError(failure)
		}
	}))
	member := &model.User{ID: 2, Role: model.RoleMember, Status: model.StatusActive}
	if _, e := categories.TreeFor(member); !errors.Is(e, failure) {
		t.Fatal("expected atomic initialization failure", e)
	}
	mustOK(t, db.Callback().Create().Remove("catalog_init_failure"))
	var user model.User
	mustOK(t, db.First(&user, 2).Error)
	if user.CategoriesInitialized {
		t.Fatal("failed initialization marked done")
	}
	mustOK(t, db.Table("categories").Where("owner_id = ?", 2).Count(&count).Error)
	if count != 0 {
		t.Fatal("partial tree committed")
	}
	tree, e := categories.TreeFor(member)
	mustOK(t, e)
	if len(tree[0].Children) == 0 {
		t.Fatal("retry did not initialize tree")
	}
}

package service

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	driver "github.com/go-sql-driver/mysql"
	"github.com/shopspring/decimal"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"jizhang/internal/model"
	"jizhang/internal/repository"
)

// 只操作本测试新建的随机数据库，不复用 DSN 中的库。
func TestVoucherMySQL(t *testing.T) {
	dsn := os.Getenv("VOUCHER_MYSQL_DSN")
	if dsn == "" {
		t.Skip("set VOUCHER_MYSQL_DSN for isolated MySQL integration")
	}
	cfg, err := driver.ParseDSN(dsn)
	mustOK(t, err)
	cfg.DBName = ""
	cfg.MultiStatements = true
	cfg.ParseTime = true
	cfg.Loc = voucherLocation
	root, err := sql.Open("mysql", cfg.FormatDSN())
	mustOK(t, err)
	t.Cleanup(func() { _ = root.Close() })
	mustOK(t, root.Ping())
	name := fmt.Sprintf("jizhang_voucher_test_%d", time.Now().UnixNano())
	_, err = root.Exec("CREATE DATABASE `" + name + "` CHARACTER SET utf8mb4")
	mustOK(t, err)
	t.Cleanup(func() {
		if _, e := root.Exec("DROP DATABASE `" + name + "`"); e != nil {
			t.Errorf("cleanup isolated database: %v", e)
		}
	})
	cfg.DBName = name
	db, err := gorm.Open(mysql.Open(cfg.FormatDSN()), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	mustOK(t, err)
	pool, err := db.DB()
	mustOK(t, err)
	defer pool.Close()
	readMigration := func(file string) string {
		t.Helper()
		b, e := os.ReadFile(filepath.Join("..", "..", "migrations", file))
		mustOK(t, e)
		return string(b)
	}
	for _, file := range []string{"000001_init.up.sql", "000002_settings.up.sql", "000003_recurring_idempotency.up.sql", "000004_refund.up.sql"} {
		mustOK(t, db.Exec(readMigration(file)).Error)
	}
	mustOK(t, db.Exec("INSERT INTO users (id, username, password_hash, role, status) VALUES (1,'voucher-test-admin','test-hash','admin','active')").Error)
	mustOK(t, db.Exec("INSERT INTO accounts (id, name, type, opening_balance, status) VALUES (1,'legacy','asset',1000,'active')").Error)
	mustOK(t, db.Exec("INSERT INTO categories (id, kind, name) VALUES (1,'expense','legacy')").Error)
	mustOK(t, db.Exec("INSERT INTO transactions (kind,amount,account_id,category_id,entry_date,period_key,created_by,source) VALUES ('expense',5,1,1,'2026-09-01','2026-09',1,'manual')").Error)
	up := readMigration("000005_vouchers.up.sql")
	down := readMigration("000005_vouchers.down.sql")
	mustOK(t, db.Exec(up).Error)
	var legacy model.Transaction
	mustOK(t, db.First(&legacy, 1).Error)
	if legacy.Purpose != model.PurposeRegular || legacy.PeriodKey != "2026-09" || !legacy.Amount.Equal(decimal.NewFromInt(5)) || legacy.VoucherEventID != nil {
		t.Fatal("legacy data changed", legacy)
	}
	mustOK(t, db.Exec(down).Error)
	mustOK(t, db.Exec(up).Error)
	mustOK(t, db.Exec(readMigration("000006_voucher_platforms.up.sql")).Error)
	mustOK(t, db.Exec(readMigration("000007_transaction_platform.up.sql")).Error)
	mustOK(t, db.Exec(readMigration("000008_liability_billing.up.sql")).Error)
	mustOK(t, db.Exec(readMigration("000013_installment_principal_remainder.up.sql")).Error)
	// This fixture intentionally replays older voucher migrations; align its
	// account row shape with the current service without seeding new catalogs.
	mustOK(t, db.Exec("ALTER TABLE accounts ADD COLUMN owner_id BIGINT NULL, ADD COLUMN asset_class VARCHAR(16) NULL").Error)
	mustOK(t, db.Exec("ALTER TABLE transactions ADD COLUMN fund_event_id BIGINT NULL").Error)

	t.Run("double-instance-auto-refund", func(t *testing.T) {
		mustOK(t, db.AutoMigrate(&model.Category{}))
		e := seedVoucherEnv(t, db)
		v := e.buy(t, "100", 5, "2026-10-31")
		other := NewVoucherService(db, e.txn)
		start := make(chan struct{})
		var wg sync.WaitGroup
		var results [2]VoucherEnsureResult
		var errs [2]error
		for i, svc := range []*VoucherService{e.svc, other} {
			wg.Add(1)
			go func(i int, svc *VoucherService) {
				defer wg.Done()
				<-start
				results[i], errs[i] = svc.EnsureDue(vdate(t, "2026-12-02"))
			}(i, svc)
		}
		close(start)
		wg.Wait()
		for _, e := range errs {
			mustOK(t, e)
		}
		if results[0].Generated+results[1].Generated != 1 {
			t.Fatal(results)
		}
		e.balance(t, "1000")
		var count int64
		mustOK(t, db.Model(&model.VoucherEvent{}).Where("voucher_id = ? AND action = 'refund' AND voided_at IS NULL", v.ID).Count(&count).Error)
		if count != 1 {
			t.Fatal(count)
		}
	})
	t.Run("redeem-refund-race", func(t *testing.T) {
		mustOK(t, db.AutoMigrate(&model.Category{}))
		e := seedVoucherEnv(t, db)
		v := e.buy(t, "100", 5, "2026-10-31")
		start := make(chan struct{})
		var wg sync.WaitGroup
		var redemptionErr, refundErr error
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			_, redemptionErr = e.svc.Operate(adminUser(), v.ID, model.VoucherRedeem, VoucherOperation{EntryDate: vdate(t, "2026-10-31"), Units: 2, RequestKey: "955148e1-adf2-4cb8-b627-b222c562c79a"})
		}()
		go func() {
			defer wg.Done()
			<-start
			_, refundErr = NewVoucherService(db, e.txn).EnsureDue(vdate(t, "2026-12-02"))
		}()
		close(start)
		wg.Wait()
		mustOK(t, refundErr)
		if redemptionErr != nil && redemptionErr != ErrVoucherSettled {
			t.Fatal(redemptionErr)
		}
		d, err := e.svc.Detail(adminUser(), v.ID)
		mustOK(t, err)
		if d.RemainingUnits != 0 || d.UsedUnits+d.RefundedUnits != 5 || !d.UsedAmount.Add(d.RefundedAmount).Equal(dec("100")) {
			t.Fatal(d)
		}
		if d.UsedUnits == 2 {
			e.balance(t, "960")
		} else {
			e.balance(t, "1000")
		}
	})
	t.Run("closing-write-race", func(t *testing.T) {
		mustOK(t, db.AutoMigrate(&model.Category{}))
		e := seedVoucherEnv(t, db)
		v := e.buy(t, "100", 5, "")
		start := make(chan struct{})
		var wg sync.WaitGroup
		var writeErr, closeErr error
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			_, writeErr = e.svc.Operate(adminUser(), v.ID, model.VoucherRedeem, VoucherOperation{EntryDate: vdate(t, "2026-11-01"), Units: 1, RequestKey: "b5f137a8-79cd-453d-89b5-6c540c92f976"})
		}()
		go func() { defer wg.Done(); <-start; _, closeErr = e.closing.Close(adminUser(), "2026-11") }()
		close(start)
		wg.Wait()
		mustOK(t, closeErr)
		if writeErr != nil && writeErr != ErrTxnClosedPeriod {
			t.Fatal(writeErr)
		}
		_, err := e.svc.Operate(adminUser(), v.ID, model.VoucherRedeem, VoucherOperation{EntryDate: vdate(t, "2026-11-02"), Units: 1, RequestKey: "be05875d-4b42-44e2-889e-f406d620a381"})
		if err != ErrTxnClosedPeriod {
			t.Fatal(err)
		}
		var count int64
		mustOK(t, db.Model(&model.VoucherEvent{}).Where("voucher_id = ? AND action='redeem'", v.ID).Count(&count).Error)
		if (writeErr == nil && count != 1) || (writeErr != nil && count != 0) {
			t.Fatal(writeErr, count)
		}
		mustOK(t, e.closing.Reopen(adminUser(), "2026-11"))
	})
	t.Run("populated-down-is-rejected", func(t *testing.T) {
		if err := db.Exec(down).Error; err == nil {
			t.Fatal("down silently discarded vouchers")
		}
		var count int64
		mustOK(t, db.Model(&model.Voucher{}).Count(&count).Error)
		if count == 0 {
			t.Fatal("voucher data disappeared")
		}
		accounts, err := repository.NewAccountRepo(db).List()
		mustOK(t, err)
		if len(accounts) == 0 {
			t.Fatal("accounts disappeared")
		}
	})
	t.Run("calendar-dates-on-utc-server", func(t *testing.T) {
		original := time.Local
		time.Local = time.UTC
		defer func() { time.Local = original }()
		utcCfg := *cfg
		utcCfg.Loc = time.UTC
		utcDB, err := gorm.Open(mysql.Open(utcCfg.FormatDSN()), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
		mustOK(t, err)
		utcPool, err := utcDB.DB()
		mustOK(t, err)
		defer utcPool.Close()
		e := seedVoucherEnv(t, utcDB)
		v := e.buy(t, "89", 1, "2026-10-31")
		var saved model.Voucher
		mustOK(t, utcDB.First(&saved, v.ID).Error)
		if voucherDay(saved.PurchaseDate) != "2026-10-10" || voucherDay(*saved.ExpiresOn) != "2026-10-31" {
			t.Fatal("calendar date shifted", saved)
		}
		result, err := e.svc.EnsureDue(vdate(t, "2026-12-02"))
		mustOK(t, err)
		if result.Generated < 1 {
			t.Fatal(result)
		}
		var refund model.Transaction
		mustOK(t, utcDB.Joins("JOIN voucher_events e ON e.id = transactions.voucher_event_id").Where("e.voucher_id = ? AND e.action = 'refund'", v.ID).First(&refund).Error)
		if voucherDay(refund.EntryDate) != "2026-11-01" {
			t.Fatal("refund date shifted", refund.EntryDate)
		}
	})
}

package service

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	driver "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"jizhang/internal/model"
	"jizhang/internal/repository"
)

func TestVoucherPlatformMySQL(t *testing.T) {
	dsn := os.Getenv("VOUCHER_MYSQL_DSN")
	if dsn == "" {
		t.Skip("set VOUCHER_MYSQL_DSN for isolated MySQL integration")
	}
	cfg, err := driver.ParseDSN(dsn)
	mustOK(t, err)
	cfg.DBName, cfg.MultiStatements, cfg.ParseTime, cfg.Loc = "", true, true, voucherLocation
	root, err := sql.Open("mysql", cfg.FormatDSN())
	mustOK(t, err)
	t.Cleanup(func() { _ = root.Close() })
	name := fmt.Sprintf("jizhang_platform_test_%d", time.Now().UnixNano())
	_, err = root.Exec("CREATE DATABASE `" + name + "` CHARACTER SET utf8mb4")
	mustOK(t, err)
	t.Cleanup(func() {
		if _, err := root.Exec("DROP DATABASE `" + name + "`"); err != nil {
			t.Error("cleanup isolated database", err)
		}
	})
	cfg.DBName = name
	db, err := gorm.Open(mysql.Open(cfg.FormatDSN()), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	mustOK(t, err)
	pool, err := db.DB()
	mustOK(t, err)
	t.Cleanup(func() { _ = pool.Close() })
	migration := func(file string) {
		t.Helper()
		b, err := os.ReadFile(filepath.Join("..", "..", "migrations", file))
		mustOK(t, err)
		mustOK(t, db.Exec(string(b)).Error)
	}
	for _, file := range []string{"000001_init.up.sql", "000002_settings.up.sql", "000003_recurring_idempotency.up.sql", "000004_refund.up.sql", "000005_vouchers.up.sql"} {
		migration(file)
	}
	migration("000008_liability_billing.up.sql")
	migration("000013_installment_principal_remainder.up.sql")
	// Keep this legacy migration fixture compatible with current cash models.
	mustOK(t, db.Exec("ALTER TABLE accounts ADD COLUMN owner_id BIGINT NULL, ADD COLUMN asset_class VARCHAR(16) NULL").Error)
	mustOK(t, db.Exec("ALTER TABLE transactions ADD COLUMN fund_event_id BIGINT NULL").Error)
	mustOK(t, db.Exec("INSERT INTO users (id,username,password_hash,role,status) VALUES (1,'platform-test','test','admin','active')").Error)
	mustOK(t, db.AutoMigrate(&model.Category{}))
	e := seedVoucherEnv(t, db)
	for _, platform := range []string{"美团", "美团", "JD", "jd", ""} {
		row := model.Voucher{Name: "历史券", Platform: platform, PaidAmount: dec("10"), PurchaseDate: vdate(t, "2026-10-01"), AccountID: e.account, CategoryID: e.category, TotalUnits: 1, ScheduleVersion: 1, Revision: 1, CreatedBy: 1, CreateRequestKey: uuid.NewString()}
		mustOK(t, db.Create(&row).Error)
	}
	deleted := model.Voucher{Name: "已删除券", Platform: "不能回填", PaidAmount: dec("10"), PurchaseDate: vdate(t, "2026-10-01"), AccountID: e.account, CategoryID: e.category, TotalUnits: 1, CreatedBy: 1, CreateRequestKey: uuid.NewString()}
	mustOK(t, db.Create(&deleted).Error)
	mustOK(t, db.Delete(&deleted).Error)
	snapshot := func() string {
		t.Helper()
		var rows []model.Voucher
		mustOK(t, db.Unscoped().Order("id").Find(&rows).Error)
		b, err := json.Marshal(rows)
		mustOK(t, err)
		return string(b)
	}
	before := snapshot()
	migration("000006_voucher_platforms.up.sql")
	rows, err := e.svc.ListPlatforms()
	mustOK(t, err)
	if len(rows) != 3 || rows[0].Name != "JD" || rows[1].Name != "jd" || rows[2].Name != "美团" {
		t.Fatal("wrong historical backfill", rows)
	}
	migration("000006_voucher_platforms.down.sql")
	if snapshot() != before || db.Migrator().HasTable(&model.VoucherPlatform{}) {
		t.Fatal("down changed vouchers or did not remove directory")
	}
	e.balance(t, "1000")
	migration("000006_voucher_platforms.up.sql")
	if snapshot() != before {
		t.Fatal("up changed historical vouchers")
	}
	migration("000007_transaction_platform.up.sql")

	t.Run("concurrent-create-or-get", func(t *testing.T) {
		const n = 12
		var wg sync.WaitGroup
		start := make(chan struct{})
		ids, errs := make([]int64, n), make([]error, n)
		for i := range n {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				row, err := e.svc.CreatePlatform("  并发创建  ")
				errs[i] = err
				if row != nil {
					ids[i] = row.ID
				}
			}()
		}
		close(start)
		wg.Wait()
		for i := range n {
			mustOK(t, errs[i])
			if ids[i] == 0 || ids[i] != ids[0] {
				t.Fatal("different identities", ids)
			}
		}
	})
	t.Run("existing-snapshot-sees-concurrent-name", func(t *testing.T) {
		mustOK(t, db.Transaction(func(tx *gorm.DB) error {
			var count int64
			if err := tx.Model(&model.VoucherPlatform{}).Count(&count).Error; err != nil {
				return err
			}
			created, err := e.svc.CreatePlatform("快照后新建")
			if err != nil {
				return err
			}
			row, err := repository.NewVoucherPlatformRepo(tx).Ensure("快照后新建")
			if err == nil && row.ID != created.ID {
				t.Fatal("snapshot returned wrong identity")
			}
			return err
		}))
	})
	t.Run("save-rolls-back-on-directory-failure", func(t *testing.T) {
		in := e.input(t, "20", 1, "")
		in.Platform = "存储失败"
		mustOK(t, db.Exec(`CREATE TRIGGER reject_platform BEFORE INSERT ON voucher_platforms FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'storage rejected'`).Error)
		_, err := e.svc.Create(adminUser(), in)
		if err == nil {
			t.Fatal("storage error swallowed")
		}
		mustOK(t, db.Exec("DROP TRIGGER reject_platform").Error)
		e.balance(t, "1000")
		if snapshot() != before {
			t.Fatal("failed save left a voucher")
		}
		for _, table := range []any{&model.VoucherEvent{}, &model.Transaction{}} {
			var count int64
			mustOK(t, db.Model(table).Count(&count).Error)
			if count != 0 {
				t.Fatal("failed save left financial data")
			}
		}
	})
}

package service

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"errors"
	"gorm.io/gorm"
	"jizhang/internal/model"
)

func platformMigrationDB(t *testing.T, withUsers bool) (*gorm.DB, func(string)) {
	t.Helper()
	db, migrate := catalogMySQL(t)
	if withUsers {
		mustOK(t, db.Exec("INSERT INTO users(id,username,password_hash,role,status) VALUES(1,'platform-admin','hash','admin','active'),(2,'platform-member','hash','member','active')").Error)
		mustOK(t, db.Exec("UPDATE accounts SET owner_id=1 WHERE type='liability'").Error)
	}
	for _, file := range []string{"000012_personal_catalogs.up.sql", "000013_installment_principal_remainder.up.sql", "000014_personal_fund_accounts.up.sql"} {
		migrate(file)
	}
	return db, migrate
}

func TestPersonalPlatformMySQLConcurrency(t *testing.T) {
	db, migrate := platformMigrationDB(t, false)
	migrate("000015_personal_voucher_platforms.up.sql")
	migrate("000016_catalog_ordering.up.sql")
	actors := []*model.User{
		{ID: 1, Username: "concurrent-platform-one", Role: model.RoleMember, Status: model.StatusActive},
		{ID: 2, Username: "concurrent-platform-two", Role: model.RoleViewer, Status: model.StatusActive},
	}
	for _, actor := range actors {
		mustOK(t, db.Create(actor).Error)
	}
	e := seedVoucherEnv(t, db)
	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for range 8 {
		for _, actor := range actors {
			wg.Go(func() { _, err := e.svc.ListPlatforms(actor); errs <- err })
		}
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		mustOK(t, err)
	}
	for _, actor := range actors {
		rows, err := e.svc.ListPlatforms(actor)
		mustOK(t, err)
		if len(rows) != 9 {
			t.Fatal("concurrent initialization duplicated defaults", rows)
		}
	}
	ids := make([][]int64, 2)
	for i, actor := range actors {
		ids[i] = make([]int64, 8)
		for n := range 8 {
			wg.Go(func() {
				row, err := e.svc.CreatePlatform(actor, "跨账号并发同名")
				if err != nil {
					t.Error(err)
					return
				}
				ids[i][n] = row.ID
			})
		}
	}
	wg.Wait()
	for i := range 2 {
		for _, id := range ids[i] {
			if id == 0 || id != ids[i][0] {
				t.Fatal("same-owner identities diverged", ids)
			}
		}
	}
	if ids[0][0] == ids[1][0] {
		t.Fatal("owners shared a same-name identity", ids)
	}
	for n := range 6 {
		row, err := e.svc.CreatePlatform(actors[0], "删除竞争"+strings.Repeat("甲", n))
		mustOK(t, err)
		input := expenseIn(t, e.account, e.category, "5", "2026-10-01")
		input.Platform = row.Name
		var deleteErr, writeErr error
		start := make(chan struct{})
		wg.Go(func() { <-start; deleteErr = e.svc.DeletePlatform(actors[0], row.ID) })
		wg.Go(func() { <-start; _, writeErr = e.txn.Create(actors[0], input) })
		close(start)
		wg.Wait()
		mustOK(t, writeErr)
		if deleteErr != nil && !errors.Is(deleteErr, ErrPlatformReferenced) {
			t.Fatal(deleteErr)
		}
		var current model.VoucherPlatform
		mustOK(t, db.Where("owner_id=1 AND name=?", row.Name).First(&current).Error)
		if err := e.svc.DeletePlatform(actors[0], current.ID); !errors.Is(err, ErrPlatformReferenced) {
			t.Fatal("racing consumption has no protected candidate", err)
		}
	}
	e.balance(t, "970")
	third := &model.User{ID: 3, Username: "initialization-retry", Role: model.RoleMember, Status: model.StatusActive}
	mustOK(t, db.Create(third).Error)
	mustOK(t, db.Exec(`CREATE TRIGGER reject_platform_initialization BEFORE INSERT ON voucher_platforms FOR EACH ROW BEGIN IF NEW.owner_id=3 AND NEW.name='微信' THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='initialization rejected'; END IF; END`).Error)
	if _, err := e.svc.ListPlatforms(third); err == nil {
		t.Fatal("initialization failure was ignored")
	}
	var count int64
	mustOK(t, db.Model(&model.VoucherPlatform{}).Where("owner_id=3").Count(&count).Error)
	var user model.User
	mustOK(t, db.First(&user, 3).Error)
	if count != 0 || user.VoucherPlatformsInitialized {
		t.Fatal("partial initialization committed")
	}
	mustOK(t, db.Exec("DROP TRIGGER reject_platform_initialization").Error)
	rows, err := e.svc.ListPlatforms(third)
	mustOK(t, err)
	if len(rows) != 9 {
		t.Fatal("initialization retry failed", rows)
	}
}

func platformFinancialSnapshot(t *testing.T, db *gorm.DB) string {
	t.Helper()
	var vouchers []model.Voucher
	var txns []model.Transaction
	mustOK(t, db.Unscoped().Order("id").Find(&vouchers).Error)
	mustOK(t, db.Unscoped().Order("id").Find(&txns).Error)
	b, err := json.Marshal([]any{vouchers, txns})
	mustOK(t, err)
	return string(b)
}

func TestPersonalPlatformMySQLMigration(t *testing.T) {
	t.Run("empty-up-down", func(t *testing.T) {
		db, migrate := platformMigrationDB(t, false)
		migrate("000015_personal_voucher_platforms.up.sql")
		var count int64
		mustOK(t, db.Model(&model.VoucherPlatform{}).Count(&count).Error)
		if count != 0 {
			t.Fatal("empty book exposed ownerless defaults", count)
		}
		migrate("000015_personal_voucher_platforms.down.sql")
		if db.Migrator().HasColumn("voucher_platforms", "owner_id") {
			t.Fatal("down retained ownership")
		}
		mustOK(t, db.Table("voucher_platforms").Count(&count).Error)
		if count != 9 {
			t.Fatal("old defaults were not restored", count)
		}
	})
	t.Run("history-independent-copies-and-guard", func(t *testing.T) {
		db, migrate := platformMigrationDB(t, true)
		mustOK(t, db.Exec("INSERT INTO voucher_platforms(name,created_at) VALUES('未用于券的自定义项','2026-01-01'),('JD','2026-01-02'),('jd','2026-01-03')").Error)
		mustOK(t, db.Exec("UPDATE accounts SET owner_id=2 WHERE id=1").Error)
		mustOK(t, db.Exec("INSERT INTO transactions(kind,amount,account_id,entry_date,period_key,created_by,platform) VALUES('expense',7,1,'2026-10-01','2026-10',1,'  本人历史商家  ')").Error)
		deletedVoucher := model.Voucher{Name: "已删除券", Platform: "已删除商家", PaidAmount: dec("3"), PurchaseDate: vdate(t, "2026-10-01"), AccountID: 1, CategoryID: 1, TotalUnits: 1, CreatedBy: 1, CreateRequestKey: "deleted-history", CreatePayloadHash: "legacy", ScheduleVersion: 1, Revision: 1}
		mustOK(t, db.Create(&deletedVoucher).Error)
		mustOK(t, db.Delete(&deletedVoucher).Error)
		before := platformFinancialSnapshot(t, db)
		migrate("000015_personal_voucher_platforms.up.sql")
		var copies []model.VoucherPlatform
		mustOK(t, db.Where("name = ?", "未用于券的自定义项").Order("owner_id").Find(&copies).Error)
		if len(copies) != 2 || copies[0].ID == copies[1].ID || copies[0].CreatedAt.Format("2006-01-02") != "2026-01-01" {
			t.Fatal("independent copies lost identity or creation time", copies)
		}
		var history []model.VoucherPlatform
		mustOK(t, db.Where("name IN ?", []string{"本人历史商家", "已删除商家"}).Find(&history).Error)
		if len(history) != 2 || history[0].OwnerID != 2 || history[1].OwnerID != 2 {
			t.Fatal("history used original creator instead of account owner", history)
		}
		for _, candidate := range history {
			if err := NewVoucherService(db, nil).DeletePlatform(&model.User{ID: 2, Role: model.RoleMember}, candidate.ID); !errors.Is(err, ErrPlatformReferenced) {
				t.Fatal("legacy whitespace or deleted-voucher reference was not protected", candidate, err)
			}
		}
		if platformFinancialSnapshot(t, db) != before {
			t.Fatal("migration changed financial history")
		}
		migrate("000015_personal_voucher_platforms.down.sql")
		if platformFinancialSnapshot(t, db) != before {
			t.Fatal("down changed financial history")
		}
		migrate("000015_personal_voucher_platforms.up.sql")
		mustOK(t, db.Model(&model.VoucherPlatform{}).Where("owner_id=2 AND name=?", "未用于券的自定义项").Update("name", "本人修改").Error)
		down, err := os.ReadFile(filepath.Join("..", "..", "migrations", "000015_personal_voucher_platforms.down.sql"))
		mustOK(t, err)
		if err = db.Exec(string(down)).Error; err == nil || !strings.Contains(err.Error(), "Personal platform catalog changed") {
			t.Fatal("down erased a private edit", err)
		}
		if !db.Migrator().HasColumn("voucher_platforms", "owner_id") || platformFinancialSnapshot(t, db) != before {
			t.Fatal("guard partially destroyed schema or finance")
		}
		mustOK(t, db.Model(&model.VoucherPlatform{}).Where("owner_id=2 AND name=?", "本人修改").Update("name", "未用于券的自定义项").Error)
		mustOK(t, db.Exec("UPDATE voucher_platform_templates SET name='默认修改' WHERE name='微信'").Error)
		if err = db.Exec(string(down)).Error; err == nil {
			t.Fatal("down ignored template edits")
		}
		mustOK(t, db.Exec("UPDATE voucher_platform_templates SET name='微信' WHERE name='默认修改'").Error)
		newUser := model.User{ID: 3, Username: "post-migration", Role: model.RoleViewer, Status: model.StatusActive}
		migrate("000016_catalog_ordering.up.sql")
		mustOK(t, db.Create(&newUser).Error)
		newRows, err := NewVoucherService(db, nil).ListPlatforms(&newUser)
		mustOK(t, err)
		if len(newRows) != 9 {
			t.Fatal("new user inherited custom copies", newRows)
		}
		if err = db.Exec(string(down)).Error; err == nil {
			t.Fatal("down erased a newly initialized user")
		}
		if platformFinancialSnapshot(t, db) != before {
			t.Fatal("guard altered financial records")
		}
	})
	for _, invalid := range []string{"missing-owner", "invalid-name"} {
		t.Run(invalid, func(t *testing.T) {
			db, _ := platformMigrationDB(t, true)
			if invalid == "missing-owner" {
				mustOK(t, db.Exec("UPDATE accounts SET owner_id=NULL WHERE id=1").Error)
				mustOK(t, db.Exec("INSERT INTO transactions(kind,amount,account_id,entry_date,period_key,created_by,platform) VALUES('expense',1,1,'2026-10-01','2026-10',1,'无法归属')").Error)
			} else {
				mustOK(t, db.Exec("INSERT INTO voucher_platforms(name) VALUES('   ')").Error)
			}
			up, err := os.ReadFile(filepath.Join("..", "..", "migrations", "000015_personal_voucher_platforms.up.sql"))
			mustOK(t, err)
			if err = db.Exec(string(up)).Error; err == nil {
				t.Fatal("preflight accepted invalid data")
			}
			if db.Migrator().HasColumn("voucher_platforms", "owner_id") || db.Migrator().HasTable("platform_catalog_before") {
				t.Fatal("preflight changed schema before rejecting data")
			}
		})
	}
}

package service

import (
	"errors"
	"strings"
	"testing"

	"gorm.io/gorm"
	"jizhang/internal/model"
)

func TestVoucherPlatformDirectory(t *testing.T) {
	e := newVoucherEnv(t)
	rows, err := e.svc.ListPlatforms(adminUser())
	mustOK(t, err)
	if rows == nil || len(rows) != 0 {
		t.Fatal("empty directory must be an empty slice", rows)
	}
	var first int64
	for _, name := range []string{"美团", "\t 美团 \n", "jd", "JD", strings.Repeat("商", 128)} {
		row, err := e.svc.CreatePlatform(adminUser(), name)
		mustOK(t, err)
		if strings.TrimSpace(name) == "美团" {
			if first != 0 && row.ID != first {
				t.Fatal("duplicate name did not reuse identity")
			}
			first = row.ID
		}
	}
	for _, name := range []string{"", " \n\t", strings.Repeat("商", 129)} {
		_, err := e.svc.CreatePlatform(adminUser(), name)
		if !errors.Is(err, ErrVoucherInvalid) {
			t.Fatalf("invalid name accepted: %q: %v", name, err)
		}
	}
	rows, err = e.svc.ListPlatforms(adminUser())
	mustOK(t, err)
	if len(rows) != 4 || rows[0].Name != "美团" || rows[1].Name != "jd" || rows[2].Name != "JD" {
		t.Fatal("wrong directory or sort", rows)
	}
	e.balance(t, "1000")
	for _, table := range []any{&model.Voucher{}, &model.VoucherEvent{}, &model.Transaction{}, &model.PeriodClosing{}} {
		var count int64
		mustOK(t, e.db.Model(table).Count(&count).Error)
		if count != 0 {
			t.Fatalf("directory created financial data: %T", table)
		}
	}
	// 非唯一冲突错误不能被当作重复创建而吞掉。
	mustOK(t, e.db.Exec(`CREATE TRIGGER reject_platform BEFORE INSERT ON voucher_platforms BEGIN SELECT RAISE(ABORT, 'storage rejected'); END`).Error)
	if _, err := e.svc.CreatePlatform(adminUser(), "美团"); err == nil {
		t.Fatal("non-unique error was swallowed")
	}
}

func TestVoucherPlatformSaveCompatibility(t *testing.T) {
	e := newVoucherEnv(t)
	in := e.input(t, "20", 1, "")
	in.Platform = "  直接输入  "
	v, err := e.svc.Create(adminUser(), in)
	mustOK(t, err)
	if v.Platform != "直接输入" {
		t.Fatal(v.Platform)
	}
	checkNames := func(want ...string) {
		t.Helper()
		rows, err := e.svc.ListPlatforms(adminUser())
		mustOK(t, err)
		if len(rows) != len(want) {
			t.Fatalf("directory=%v want=%v", rows, want)
		}
		for _, name := range want {
			found := false
			for _, row := range rows {
				found = found || row.Name == name
			}
			if !found {
				t.Fatal("missing name", name)
			}
		}
	}
	retry, err := e.svc.Create(adminUser(), in)
	mustOK(t, err)
	if retry.ID != v.ID {
		t.Fatal("retry created another voucher")
	}
	e.balance(t, "980")
	in.Platform = "冲突不能加入"
	_, err = e.svc.Create(adminUser(), in)
	if !errors.Is(err, ErrVoucherKeyConflict) {
		t.Fatal(err)
	}
	checkNames("直接输入")
	in.Platform = "修改新名称"
	_, err = e.svc.Update(adminUser(), v.ID, in)
	mustOK(t, err)
	checkNames("直接输入", "修改新名称")
	in.Platform = "   "
	v, err = e.svc.Update(adminUser(), v.ID, in)
	mustOK(t, err)
	if v.Platform != "" {
		t.Fatal("optional field cannot be cleared")
	}
	mustOK(t, e.svc.Delete(adminUser(), v.ID))
	checkNames("直接输入", "修改新名称")
	e.balance(t, "1000")
}

func TestVoucherPlatformTransactionRollback(t *testing.T) {
	e := newVoucherEnv(t)
	_, err := e.svc.CreatePlatform(adminUser(), "已明确创建")
	mustOK(t, err)
	in := e.input(t, "20", 1, "")
	in.Platform = "新名称"
	in.AccountID = -1
	if _, err := e.svc.Create(adminUser(), in); err == nil {
		t.Fatal("invalid account accepted")
	}
	// 故障发生在购买事件和资金投影写入之后，整个事务仍必须回滚。
	mustOK(t, e.db.Exec(`CREATE TRIGGER reject_new_platform BEFORE INSERT ON voucher_platforms WHEN NEW.name = '新名称' BEGIN SELECT RAISE(ABORT, 'storage rejected'); END`).Error)
	in.AccountID = e.account
	if _, err := e.svc.Create(adminUser(), in); err == nil {
		t.Fatal("directory storage error ignored")
	}
	e.balance(t, "1000")
	for _, table := range []any{&model.Voucher{}, &model.VoucherEvent{}, &model.Transaction{}} {
		var count int64
		mustOK(t, e.db.Model(table).Count(&count).Error)
		if count != 0 {
			t.Fatalf("rollback left financial data: %T", table)
		}
	}
	rows, err := e.svc.ListPlatforms(adminUser())
	mustOK(t, err)
	if len(rows) != 1 || rows[0].Name != "已明确创建" {
		t.Fatal("explicit creation was lost or failed save left an option", rows)
	}
	in.Platform = "已明确创建"
	v, err := e.svc.Create(adminUser(), in)
	mustOK(t, err)
	in.Platform = "新名称"
	if _, err := e.svc.Update(adminUser(), v.ID, in); err == nil {
		t.Fatal("update ignored directory failure")
	}
	var saved model.Voucher
	mustOK(t, e.db.First(&saved, v.ID).Error)
	if saved.Platform != "已明确创建" {
		t.Fatal("failed update changed original platform")
	}
	// 名称目录错误会传播，不会回退为“创建成功”。
	mustOK(t, e.db.Migrator().DropTable(&model.VoucherPlatform{}))
	if _, err := e.svc.ListPlatforms(adminUser()); err == nil || errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatal("directory read hid storage failure", err)
	}
}

package service

import (
	"errors"
	"strings"
	"testing"

	"jizhang/internal/model"
	"jizhang/internal/repository"
)

func TestTransactionPlatformSaveAndEdit(t *testing.T) {
	e := newVoucherEnv(t)
	in := expenseIn(t, e.account, e.category, "20", "2026-10-10")
	in.Platform, in.Note = "  共享商家  ", "独立备注"
	row, err := e.txn.Create(adminUser(), in)
	mustOK(t, err)
	if row.Platform != "共享商家" || row.Note != in.Note {
		t.Fatal("platform was not saved independently", row)
	}
	options, err := e.svc.ListPlatforms()
	mustOK(t, err)
	if len(options) != 1 || options[0].Name != row.Platform {
		t.Fatal("transaction did not populate shared directory", options)
	}
	list, total, err := e.txn.List(adminUser(), repository.TxnFilter{Keyword: "共享商家"})
	mustOK(t, err)
	if total != 1 || list[0].Platform != row.Platform {
		t.Fatal("platform search failed", list, total)
	}
	csv, err := e.txn.ExportCSV(adminUser(), repository.TxnFilter{Keyword: "共享商家"})
	mustOK(t, err)
	if !strings.Contains(string(csv), "平台 / 商家") || !strings.Contains(string(csv), ",共享商家\r\n") {
		t.Fatal("CSV lost platform", string(csv))
	}
	in.Platform = "=新的平台"
	updated, err := e.txn.Update(adminUser(), row.ID, in)
	mustOK(t, err)
	if updated.Platform != in.Platform {
		t.Fatal("edit lost platform")
	}
	csv, err = e.txn.ExportCSV(adminUser(), repository.TxnFilter{})
	mustOK(t, err)
	if !strings.Contains(string(csv), ",'=新的平台\r\n") {
		t.Fatal("platform text was not escaped in CSV")
	}
	in.Platform = " "
	updated, err = e.txn.Update(adminUser(), row.ID, in)
	mustOK(t, err)
	if updated.Platform != "" {
		t.Fatal("platform cannot be cleared")
	}
	options, err = e.svc.ListPlatforms()
	mustOK(t, err)
	if len(options) != 2 {
		t.Fatal("editing removed reusable options")
	}
	e.balance(t, "980")
	in.Platform = strings.Repeat("商", 129)
	_, err = e.txn.Update(adminUser(), row.ID, in)
	if !errors.Is(err, ErrTxnPlatformInvalid) {
		t.Fatal("platform length not validated", err)
	}
}

func TestTransactionPlatformFailureIsAtomic(t *testing.T) {
	e := newVoucherEnv(t)
	in := expenseIn(t, e.account, e.category, "20", "2026-10-10")
	in.Platform = "失败商家"
	mustOK(t, e.db.Exec(`CREATE TRIGGER reject_txn_platform BEFORE INSERT ON voucher_platforms BEGIN SELECT RAISE(ABORT, 'storage rejected'); END`).Error)
	_, err := e.txn.Create(adminUser(), in)
	if err == nil {
		t.Fatal("failed directory write was ignored")
	}
	var count int64
	mustOK(t, e.db.Model(&model.Transaction{}).Count(&count).Error)
	if count != 0 {
		t.Fatal("failed transaction left a financial record")
	}
	e.balance(t, "1000")
	_, err = e.txn.Create(&model.User{ID: 2, Role: model.RoleViewer, Status: model.StatusActive}, in)
	if !errors.Is(err, ErrTxnForbidden) {
		t.Fatal("platform granted transaction permission", err)
	}
}

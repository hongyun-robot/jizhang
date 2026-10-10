package service

import (
	"errors"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"jizhang/internal/model"
	"jizhang/internal/repository"
	"testing"
)

func TestVoucherFailureIsolationAndStaleFailure(t *testing.T) {
	e := newVoucherEnv(t)
	in := e.input(t, "89", 1, "2026-10-09")
	in.PurchaseDate = vdate(t, "2026-10-01")
	blocked, err := e.svc.Create(adminUser(), in)
	mustOK(t, err)
	e.buy(t, "10", 1, "2026-10-31")
	mustClose(t, e.closing, adminUser(), "2026-10")
	injected := errors.New("failure metadata unavailable")
	mustOK(t, e.db.Callback().Update().Before("gorm:update").Register("failure_metadata_error", func(db *gorm.DB) {
		if payload, ok := db.Statement.Dest.(map[string]any); ok && payload["last_error"] != nil {
			db.AddError(injected)
		}
	}))
	result, err := e.svc.EnsureDue(vdate(t, "2026-12-02"))
	mustOK(t, e.db.Callback().Update().Remove("failure_metadata_error"))
	if !errors.Is(err, injected) || result.Generated != 1 || result.Failed != 1 {
		t.Fatal(result, err)
	}
	e.balance(t, "911")
	in.Name = "修改后的券名"
	_, err = e.svc.Update(adminUser(), blocked.ID, in)
	mustOK(t, err)
	mustOK(t, repository.NewVoucherRepo(e.db).Failure(blocked.ID, blocked.Revision, vdate(t, "2026-12-02"), "outdated failure"))
	d, err := e.svc.Detail(adminUser(), blocked.ID)
	mustOK(t, err)
	if d.LastError != "" {
		t.Fatal("stale failure overwrote successful edit")
	}
}

func TestVoucherLiabilityAccount(t *testing.T) {
	e := newVoucherEnv(t)
	mustOK(t, e.db.Model(&model.Account{}).Where("id = ?", e.account).Updates(map[string]any{"type": model.AccountLiability, "opening_balance": 0}).Error)
	v := e.buy(t, "100", 5, "")
	e.balance(t, "-100")
	e.op(t, v.ID, model.VoucherRedeem, "2026-11-01", 2)
	e.balance(t, "-100")
	e.op(t, v.ID, model.VoucherRefund, "2026-11-02", 0)
	e.balance(t, "-40")
}

func TestVoucherInvalidUnitsAndAmount(t *testing.T) {
	e := newVoucherEnv(t)
	for _, amount := range []string{"0", "-1", "0.001"} {
		_, err := e.svc.Create(adminUser(), e.input(t, amount, 1, ""))
		if !errors.Is(err, ErrTxnAmountInvalid) {
			t.Fatal(err)
		}
	}
	input := e.input(t, "89", 0, "")
	if _, err := e.svc.Create(adminUser(), input); !errors.Is(err, ErrVoucherInvalid) {
		t.Fatal(err)
	}
	v := e.buy(t, "89", 2, "")
	for _, units := range []int64{0, -1, 3} {
		_, err := e.svc.Operate(adminUser(), v.ID, model.VoucherRedeem, VoucherOperation{EntryDate: vdate(t, "2026-11-01"), Units: units, RequestKey: uuid.NewString()})
		if !errors.Is(err, ErrVoucherSettled) {
			t.Fatal(err)
		}
	}
}

func TestVoucherLastSupportedExpiry(t *testing.T) {
	e := newVoucherEnv(t)
	in := e.input(t, "1", 1, "2100-12-31")
	v, err := e.svc.Create(adminUser(), in)
	mustOK(t, err)
	result, err := e.svc.EnsureDue(vdate(t, "2101-01-01"))
	mustOK(t, err)
	if result.Generated != 1 {
		t.Fatal(result)
	}
	d, err := e.svc.Detail(adminUser(), v.ID)
	mustOK(t, err)
	if voucherDay(d.Events[len(d.Events)-1].EntryDate) != "2101-01-01" {
		t.Fatal(d)
	}
}

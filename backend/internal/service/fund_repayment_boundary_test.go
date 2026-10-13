package service

import (
	"errors"
	"testing"

	"jizhang/internal/model"
)

func TestFundAccountCannotPayInstallmentOrEarlySettlement(t *testing.T) {
	e := newLiabilityEnv(t)
	id := repaymentPlan(t, e)
	owner := adminUser().ID
	fund := model.Account{Name: "基金市值", Type: model.AccountAsset, AssetClass: "fund", OwnerID: &owner, Status: model.StatusActive}
	mustOK(t, e.db.Create(&fund).Error)
	if _, err := e.svcL.List(adminUser()); err != nil {
		t.Fatal("fund account broke unrelated liability list", err)
	}
	if _, err := e.svcL.Detail(adminUser(), e.card); err != nil {
		t.Fatal("fund account broke unrelated liability details", err)
	}
	before := map[string]int64{}
	for _, table := range []string{"transactions", "liability_operations", "liability_allocations", "installment_terms"} {
		var n int64
		mustOK(t, e.db.Table(table).Count(&n).Error)
		before[table] = n
	}
	for _, all := range []bool{false, true} {
		count := 1
		if all {
			count = 0
		}
		in, _ := quotedRepayment(t, e, id, count)
		in.FromAccountID = fund.ID
		_, err := e.svcL.InstallmentRepaymentPreview(adminUser(), id, in)
		if !errors.Is(err, ErrTxnAccountInvalid) {
			t.Fatal("fund source preview accepted", err)
		}
		_, err = e.svcL.RepayInstallment(adminUser(), id, in)
		if !errors.Is(err, ErrTxnAccountInvalid) {
			t.Fatal("fund source payment accepted", err)
		}
	}
	settlement := settlementRequest(t, e, id)
	settlement.FromAccountID = fund.ID
	_, err := e.svcL.Settle(adminUser(), id, settlement)
	if !errors.Is(err, ErrTxnAccountInvalid) {
		t.Fatal("fund source settlement accepted", err)
	}
	for table, want := range before {
		var n int64
		mustOK(t, e.db.Table(table).Count(&n).Error)
		if n != want {
			t.Fatal("partial financial write", table, n, want)
		}
	}
}

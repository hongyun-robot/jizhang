package service

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"jizhang/internal/model"
)

func TestWealthCashBackfillDoesNotCountManuallyAdoptedIntervalTwice(t *testing.T) {
	e := newCashEnv(t)
	enableCash(t, e, "2026-10-12", "unit_nav")
	e.quote(t, "2026-10-12", "1")
	e.quote(t, "2026-10-15", "1.03")
	ctx := context.Background()
	mustOK(t, e.svc.ensureCashYields(ctx))
	view, err := e.svc.CashAccount(e.users[0], e.banks[0])
	mustOK(t, err)
	row := view.Yields[0]
	_, err = e.svc.EditCashYield(ctx, e.users[0], row.ID, WealthCashYieldInput{RequestKey: uuid.NewString(), ExpectedRevision: row.Revision, Amount: "151"})
	mustOK(t, err)
	e.quote(t, "2026-10-13", "1.01")
	mustOK(t, e.svc.ensureCashYields(ctx))
	view, err = e.svc.CashAccount(e.users[0], e.banks[0])
	mustOK(t, err)
	if e.bank(t, 0) != "5151.00" || len(view.Yields) != 2 || view.Yields[0].Status != "manual_interval" || !view.Yields[0].Amount.IsZero() || view.Yields[0].TxnID != nil {
		t.Fatal(view)
	}
	target := "5160.55"
	row = view.Yields[1]
	_, err = e.svc.EditCashYield(ctx, e.users[0], row.ID, WealthCashYieldInput{RequestKey: uuid.NewString(), ExpectedRevision: row.Revision, Balance: &target})
	mustOK(t, err)
	mustOK(t, e.svc.ensureCashYields(ctx))
	e.quote(t, "2026-10-13", "1.012")
	mustOK(t, e.svc.ensureCashYields(ctx))
	if e.bank(t, 0) != "5160.55" {
		t.Fatal("backfill overrode calibrated balance", e.bank(t, 0))
	}
}

func TestWealthCashTransferBetweenWalletsUsesOneCashFactAndSeparateConfirmation(t *testing.T) {
	e := newCashEnv(t)
	enableCash(t, e, "2026-10-12", "unit_nav")
	owner := e.users[0].ID
	other := model.Account{Name: "第二个理财零钱", Type: model.AccountAsset, AssetClass: "cash", OwnerID: &owner, Status: model.StatusActive}
	mustOK(t, e.db.Create(&other).Error)
	_, err := e.svc.ConfigureCash(context.Background(), e.users[0], other.ID, WealthCashInput{RequestKey: uuid.NewString(), Active: true, ProductID: 1, StartDate: "2026-10-12", Policy: "unit_nav"})
	mustOK(t, err)
	movement, err := e.txn.Create(e.users[0], TxnInput{Kind: model.TxnTransfer, AccountID: e.banks[0], TransferAccountID: other.ID, Amount: dec("1000"), EntryDate: fday("2026-10-13"), WealthCashTimings: []WealthCashFlowInput{{AccountID: e.banks[0], TransferMode: "quick", ApplicationTime: "10:00:00"}, {AccountID: other.ID, ApplicationTime: "10:00:00", ConfirmationDate: "2026-10-14"}}})
	mustOK(t, err)
	if len(movement.WealthCashFlows) != 2 {
		t.Fatal("transfer sides lost timing", movement)
	}
	for _, flow := range movement.WealthCashFlows {
		if flow.AccountID == other.ID && (flow.ConfirmationDate == nil || wealthDay(*flow.ConfirmationDate).Format("2006-01-02") != "2026-10-14") {
			t.Fatal("destination confirmation missing", flow)
		}
	}
	e.quote(t, "2026-10-12", "1")
	e.quote(t, "2026-10-13", "1.01")
	e.quote(t, "2026-10-14", "1.02")
	mustOK(t, e.svc.ensureCashYields(context.Background()))
	dest, err := e.svc.CashAccount(e.users[0], other.ID)
	mustOK(t, err)
	if e.bank(t, 0) != "4080.00" || dest.Account.Balance.String() != "1009.9" || dest.Yields[0].Amount.String() != "0" {
		t.Fatal("transfer double charged or pending transfer earned", dest, e.bank(t, 0))
	}
	mustOK(t, e.txn.Delete(e.users[0], movement.ID))
	mustOK(t, e.svc.ensureCashYields(context.Background()))
	dest, err = e.svc.CashAccount(e.users[0], other.ID)
	mustOK(t, err)
	if e.bank(t, 0) != "5100.00" || !dest.Account.Balance.IsZero() {
		t.Fatal("deleting transfer did not restore both sides", dest, e.bank(t, 0))
	}
}

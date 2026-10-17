package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/google/uuid"
	"jizhang/internal/model"
	"jizhang/internal/repository"
)

func cashMigration(t *testing.T, e *wealthEnv, file string) error {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "migrations", file))
	mustOK(t, err)
	return e.db.Exec(string(raw)).Error
}
func TestWealthCashMySQLMigrationAndConcurrentRetries(t *testing.T) {
	e := newWealthMySQLEnv(t)
	before := e.bank(t, 0)
	mustOK(t, cashMigration(t, e, "000020_cmbcwm_wealth_product.up.sql"))
	mustOK(t, cashMigration(t, e, "000021_wealth_cash_accounts.up.sql"))
	mustOK(t, cashMigration(t, e, "000021_wealth_cash_accounts.down.sql"))
	mustOK(t, cashMigration(t, e, "000021_wealth_cash_accounts.up.sql"))
	if e.bank(t, 0) != before {
		t.Fatal("migration changed old money")
	}
	ctx := context.Background()
	in := WealthCashInput{RequestKey: uuid.NewString(), Active: true, ProductID: 1, StartDate: "2026-10-12"}
	var wg sync.WaitGroup
	results := make(chan WealthCashReceipt, 8)
	failures := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := e.svc.ConfigureCash(ctx, e.users[0], e.banks[0], in)
			if err != nil {
				failures <- err
			} else {
				results <- r
			}
		}()
	}
	wg.Wait()
	close(results)
	close(failures)
	for err := range failures {
		t.Fatal(err)
	}
	var first WealthCashReceipt
	for row := range results {
		if first.LinkID == 0 {
			first = row
		} else if row != first {
			t.Fatal("different concurrent receipts", row, first)
		}
	}
	e.quote(t, "2026-10-12", "1")
	e.quote(t, "2026-10-13", "1.01")
	failures = make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := e.svc.ensureCashYields(ctx); err != nil {
				failures <- err
			}
		}()
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		t.Fatal(err)
	}
	v, err := e.svc.CashAccount(e.users[0], e.banks[0])
	mustOK(t, err)
	if len(v.Yields) != 1 || e.bank(t, 0) != "5050.00" {
		t.Fatal(v, e.bank(t, 0))
	}
	input := WealthCashYieldInput{RequestKey: uuid.NewString(), ExpectedRevision: v.Yields[0].Revision, Amount: "48", Note: "银行实际收益"}
	results = make(chan WealthCashReceipt, 8)
	failures = make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := e.svc.EditCashYield(ctx, e.users[0], v.Yields[0].ID, input)
			if err != nil {
				failures <- err
			} else {
				results <- r
			}
		}()
	}
	wg.Wait()
	close(results)
	close(failures)
	for err := range failures {
		t.Fatal(err)
	}
	first = WealthCashReceipt{}
	for row := range results {
		if first.YieldID == 0 {
			first = row
		} else if row != first {
			t.Fatal("different concurrent yield receipts", row, first)
		}
	}
	if e.bank(t, 0) != "5048.00" {
		t.Fatal(e.bank(t, 0))
	}
	// A stale editor cannot overwrite the user's adopted return.
	input.RequestKey = uuid.NewString()
	_, err = e.svc.EditCashYield(ctx, e.users[0], v.Yields[0].ID, input)
	if !errors.Is(err, ErrWealthConflict) {
		t.Fatal(err)
	}
	v, err = e.svc.CashAccount(e.users[0], e.banks[0])
	mustOK(t, err)
	// Editing through zero safely removes the circularly linked transaction,
	// while a later negative amount recreates exactly one managed ledger fact.
	input = WealthCashYieldInput{RequestKey: uuid.NewString(), ExpectedRevision: v.Yields[0].Revision, Amount: "0"}
	_, err = e.svc.EditCashYield(ctx, e.users[0], v.Yields[0].ID, input)
	mustOK(t, err)
	v, err = e.svc.CashAccount(e.users[0], e.banks[0])
	mustOK(t, err)
	if v.Yields[0].TxnID != nil || e.bank(t, 0) != "5000.00" {
		t.Fatal(v)
	}
	input = WealthCashYieldInput{RequestKey: uuid.NewString(), ExpectedRevision: v.Yields[0].Revision, Amount: "-2"}
	_, err = e.svc.EditCashYield(ctx, e.users[0], v.Yields[0].ID, input)
	mustOK(t, err)
	v, err = e.svc.CashAccount(e.users[0], e.banks[0])
	mustOK(t, err)
	target := "5010.55"
	_, err = e.svc.EditCashYield(ctx, e.users[0], v.Yields[0].ID, WealthCashYieldInput{RequestKey: uuid.NewString(), ExpectedRevision: v.Yields[0].Revision, Balance: &target, Note: "余额校准"})
	mustOK(t, err)
	if e.bank(t, 0) != "5010.55" {
		t.Fatal("calibration mismatch", e.bank(t, 0))
	}
	var txn model.Transaction
	mustOK(t, e.db.Where("wealth_cash_yield_id = ?", v.Yields[0].ID).First(&txn).Error)
	if txn.Purpose != model.PurposeWealthCashYield || txn.Kind != model.TxnIncome || txn.Amount.String() != "10.55" {
		t.Fatal(txn)
	}
	// Enforce account, owner, and yield consistency in SQL as well as services.
	if e.db.Table("transactions").Where("id = ?", txn.ID).Update("created_by", e.users[1].ID).Error == nil {
		t.Fatal("cross owner yield accepted")
	}
	if e.db.Model(&model.Account{}).Where("id = ?", e.banks[1]).Update("asset_class", "unknown").Error == nil {
		t.Fatal("unknown class accepted")
	}
	link := v.Link
	_, err = e.svc.ConfigureCash(ctx, e.users[0], e.banks[0], WealthCashInput{RequestKey: uuid.NewString(), ExpectedRevision: link.Revision, Active: false})
	mustOK(t, err)
	if cashMigration(t, e, "000021_wealth_cash_accounts.down.sql") == nil {
		t.Fatal("rollback erased retained financial history")
	}
	if !repository.NewWealthCashRepo(e.db).Available() || e.bank(t, 0) != "5010.55" {
		t.Fatal("guard partially destroyed wallet")
	}
}

func TestWealthCashMySQLFinancialRollbackAndClosing(t *testing.T) {
	e := newWealthMySQLEnv(t)
	mustOK(t, cashMigration(t, e, "000021_wealth_cash_accounts.up.sql"))
	enableCash(t, e, "2026-10-12", "unit_nav")
	category := model.Category{Name: "支出", OwnerID: &e.users[0].ID, Kind: model.KindExpense}
	mustOK(t, e.db.Create(&category).Error)
	input := TxnInput{Kind: model.TxnExpense, Amount: dec("50"), AccountID: e.banks[0], CategoryID: category.ID, EntryDate: fday("2026-10-13"), WealthCash: &WealthCashFlowInput{TransferMode: "ordinary"}}
	_, err := e.txn.Create(e.users[0], input)
	if !errors.Is(err, ErrWealthCashFlow) {
		t.Fatal(err)
	}
	if e.bank(t, 0) != "5000.00" {
		t.Fatal("invalid timing partially committed", e.bank(t, 0))
	}
	input.WealthCash = &WealthCashFlowInput{ApplicationTime: "10:00:00", TransferMode: "quick"}
	expense, err := e.txn.Create(e.users[0], input)
	mustOK(t, err)
	input.WealthCash.TransferMode = "ordinary"
	input.WealthCash.RightsEndDate = "2026-10-12"
	input.Amount = dec("100")
	_, err = e.txn.Update(e.users[0], expense.ID, input)
	mustOK(t, err)
	e.quote(t, "2026-10-12", "1")
	e.quote(t, "2026-10-13", "1.01")
	key := e.txn.deps.Periods.KeyForDate(fday("2026-10-13"))
	closing := model.PeriodClosing{PeriodKey: key, Status: model.ClosingClosed, ClosedBy: 1, ClosedAt: e.now}
	mustOK(t, e.db.Create(&closing).Error)
	mustOK(t, e.svc.ensureCashYields(context.Background()))
	v, err := e.svc.CashAccount(e.users[0], e.banks[0])
	mustOK(t, err)
	if v.Link.SyncStatus != "blocked_closed" || v.Yields[0].TxnID != nil || e.bank(t, 0) != "4900.00" {
		t.Fatal(v)
	}
	mustOK(t, e.db.Delete(&closing).Error)
	mustOK(t, e.svc.ensureCashYields(context.Background()))
	if e.bank(t, 0) != "4949.00" {
		t.Fatal(e.bank(t, 0))
	}
	mustOK(t, e.txn.Delete(e.users[0], expense.ID))
	mustOK(t, e.svc.ensureCashYields(context.Background()))
	if e.bank(t, 0) != "5050.00" {
		t.Fatal("delete did not rebuild yield", e.bank(t, 0))
	}
	var n int64
	mustOK(t, e.db.Model(&model.WealthCashFlow{}).Where("txn_id = ?", expense.ID).Count(&n).Error)
	if n != 0 {
		t.Fatal("flow metadata leaked", n)
	}
}

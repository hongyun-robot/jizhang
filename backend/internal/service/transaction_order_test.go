package service

import (
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"gorm.io/gorm"
	"jizhang/internal/model"
	"jizhang/internal/repository"
)

func transactionOrderEnv(t *testing.T) (*voucherEnv, []int64) {
	t.Helper()
	e := newVoucherEnv(t)
	mustOK(t, e.db.AutoMigrate(&model.TransactionDisplayOrder{}, &model.CatalogOrderRequest{}))
	var ids []int64
	for _, date := range []string{"2026-10-01", "2026-10-02", "2026-10-03", "2026-10-04"} {
		row := model.Transaction{Kind: model.TxnExpense, Amount: dec("1"), AccountID: e.account, CategoryID: &e.category, EntryDate: vdate(t, date), PeriodKey: "2026-10", CreatedBy: 1, Source: model.SourceManual}
		mustOK(t, e.db.Create(&row).Error)
		ids = append([]int64{row.ID}, ids...)
	}
	return e, ids
}

func transactionOrderIDs(t *testing.T, e *voucherEnv, owner int64) []int64 {
	t.Helper()
	ids, err := repository.NewTransactionOrder(e.db, owner).IDs(repository.TxnFilter{PersonalOwnerID: owner, CustomOrder: true})
	mustOK(t, err)
	return ids
}

func TestTransactionOrderPartialViewsAndRetry(t *testing.T) {
	e, base := transactionOrderEnv(t)
	var before []model.Transaction
	mustOK(t, e.db.Order("id").Find(&before).Error)
	snapshot, _ := json.Marshal(before)
	input := CatalogOrderInput{OrderedIDs: []int64{base[3], base[1]}, PreviousIDs: []int64{base[1], base[3]}}
	_, err := e.txn.Reorder(adminUser(), input, "first")
	mustOK(t, err)
	want := []int64{base[0], base[3], base[2], base[1]}
	if got := transactionOrderIDs(t, e, 1); !slices.Equal(got, want) {
		t.Fatalf("partial view changed other slots: %v", got)
	}
	_, err = e.txn.Reorder(adminUser(), input, "first")
	mustOK(t, err)
	for page := 1; page <= 2; page++ {
		rows, total, err := e.txn.List(adminUser(), repository.TxnFilter{CustomOrder: true, Page: page, PageSize: 2})
		mustOK(t, err)
		if total != 4 || len(rows) != 2 || rows[0].ID != want[(page-1)*2] || rows[1].ID != want[(page-1)*2+1] {
			t.Fatalf("wrong pagination: %v / %d", rows, total)
		}
	}
	rows, err := e.txn.ExportRows(adminUser(), repository.TxnFilter{CustomOrder: true})
	mustOK(t, err)
	for i, row := range rows {
		if row.ID != want[i] {
			t.Fatal("export differs from saved order")
		}
	}
	var after []model.Transaction
	mustOK(t, e.db.Order("id").Find(&after).Error)
	afterSnapshot, _ := json.Marshal(after)
	if string(snapshot) != string(afterSnapshot) {
		t.Fatal("sorting changed journal data")
	}
	_, err = e.txn.Reorder(adminUser(), CatalogOrderInput{OrderedIDs: input.PreviousIDs, PreviousIDs: input.OrderedIDs}, "second")
	mustOK(t, err)
	_, err = e.txn.Reorder(adminUser(), input, "first")
	if !errors.Is(err, ErrOrderConflict) {
		t.Fatalf("old retry overwrote newer order: %v", err)
	}
	newRow := model.Transaction{Kind: model.TxnIncome, Amount: dec("2"), AccountID: e.account, EntryDate: vdate(t, "2026-10-05"), CreatedBy: 1}
	mustOK(t, e.db.Create(&newRow).Error)
	if got := transactionOrderIDs(t, e, 1); !slices.Equal(got, append([]int64{newRow.ID}, base...)) {
		t.Fatalf("new row lost saved order: %v", got)
	}
}

func TestTransactionOrderPermissionsAndConflicts(t *testing.T) {
	e, base := transactionOrderEnv(t)
	foreignAccount := model.Account{OwnerID: testOwner(2), Name: "another owner", Type: model.AccountAsset, Status: model.StatusActive}
	mustOK(t, e.db.Create(&foreignAccount).Error)
	foreign := model.Transaction{AccountID: foreignAccount.ID, Kind: model.TxnIncome, Amount: dec("1"), EntryDate: vdate(t, "2026-10-05"), CreatedBy: 2}
	mustOK(t, e.db.Create(&foreign).Error)
	for _, check := range []struct {
		actor *model.User
		input CatalogOrderInput
		want  error
	}{
		{nil, CatalogOrderInput{[]int64{base[1], base[0]}, base[:2]}, ErrTxnForbidden},
		{adminUser(), CatalogOrderInput{[]int64{foreign.ID, base[0]}, []int64{base[0], foreign.ID}}, ErrTxnForbidden},
		{adminUser(), CatalogOrderInput{[]int64{999999, base[0]}, []int64{base[0], 999999}}, ErrOrderNotFound},
		{adminUser(), CatalogOrderInput{[]int64{base[0], base[0]}, base[:2]}, ErrOrderInvalid},
		{adminUser(), CatalogOrderInput{base[:2], []int64{base[1], base[0]}}, ErrOrderConflict},
	} {
		_, err := e.txn.Reorder(check.actor, check.input, "invalid")
		if !errors.Is(err, check.want) {
			t.Fatalf("got %v, want %v", err, check.want)
		}
	}
	if got := transactionOrderIDs(t, e, 1); !slices.Equal(got, base) {
		t.Fatal("rejected request changed order")
	}
	for _, role := range []model.Role{model.RoleAdmin, model.RoleViewer, model.RoleMember, model.RoleRecorder} {
		_, err := e.txn.Reorder(&model.User{ID: 1, Role: role, Status: model.StatusActive}, CatalogOrderInput{[]int64{base[1], base[0]}, base[:2]}, string(role))
		mustOK(t, err)
		_, err = e.txn.Reorder(adminUser(), CatalogOrderInput{base[:2], []int64{base[1], base[0]}}, "restore-"+string(role))
		mustOK(t, err)
	}
	if got := transactionOrderIDs(t, e, 2); !slices.Equal(got, []int64{foreign.ID}) {
		t.Fatal("another owner's view changed")
	}
}

func TestTransactionOrderSaveRollsBack(t *testing.T) {
	e, base := transactionOrderEnv(t)
	_, err := e.txn.Reorder(adminUser(), CatalogOrderInput{base, base}, "initial")
	mustOK(t, err)
	injected := errors.New("receipt storage unavailable")
	mustOK(t, e.db.Callback().Create().Before("gorm:create").Register("fail_order_receipt", func(tx *gorm.DB) {
		if tx.Statement.Table == "catalog_order_requests" {
			tx.AddError(injected)
		}
	}))
	t.Cleanup(func() { _ = e.db.Callback().Create().Remove("fail_order_receipt") })
	_, err = e.txn.Reorder(adminUser(), CatalogOrderInput{[]int64{base[1], base[0]}, base[:2]}, "failure")
	if !errors.Is(err, injected) {
		t.Fatalf("missing storage error: %v", err)
	}
	if got := transactionOrderIDs(t, e, 1); !slices.Equal(got, base) {
		t.Fatal("failed save retained partial order")
	}
}

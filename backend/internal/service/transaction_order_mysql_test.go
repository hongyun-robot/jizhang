package service

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	"jizhang/internal/model"
	"jizhang/internal/repository"
)

func TestTransactionOrderMySQLMigrationAndConcurrency(t *testing.T) {
	db, migrate := catalogMySQL(t)
	files, err := filepath.Glob(filepath.Join("..", "..", "migrations", "*.up.sql"))
	mustOK(t, err)
	for _, file := range files {
		name := filepath.Base(file)
		if name >= "000012" && name < "000023" {
			migrate(name)
		}
	}
	migrate("000023_transaction_display_order.up.sql")
	mustOK(t, db.Exec("INSERT INTO users(id,username,password_hash,role,status) VALUES(1,'order-admin','hash','admin','active')").Error)
	mustOK(t, db.Exec("INSERT INTO accounts(id,owner_id,name,type,status) VALUES(1001,1,'order-cash','asset','active')").Error)
	mustOK(t, db.Exec("INSERT INTO transactions(kind,amount,account_id,entry_date,period_key,created_by,source) VALUES('income',1,1001,'2026-10-01','2026-10',1,'manual'),('income',2,1001,'2026-10-02','2026-10',1,'manual'),('income',3,1001,'2026-10-03','2026-10',1,'manual')").Error)
	s := NewTransactionService(TxnDeps{Txns: repository.NewTransactionRepo(db), Accounts: repository.NewAccountRepo(db)})
	r := repository.NewTransactionOrder(db, 1)
	filter := repository.TxnFilter{PersonalOwnerID: 1, CustomOrder: true}
	base, err := r.IDs(filter)
	mustOK(t, err)
	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make([]error, 2)
	targets := [][]int64{{base[1], base[0], base[2]}, {base[2], base[1], base[0]}}
	for i := range targets {
		wg.Go(func() {
			<-start
			_, errs[i] = s.Reorder(adminUser(), CatalogOrderInput{targets[i], base}, []string{"device-a", "device-b"}[i])
		})
	}
	close(start)
	wg.Wait()
	successes, conflicts := 0, 0
	for _, err := range errs {
		if err == nil {
			successes++
		} else if errors.Is(err, ErrOrderConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("concurrent saves: %v", errs)
	}
	current, err := r.IDs(filter)
	mustOK(t, err)
	rows, total, err := repository.NewTransactionRepo(db).List(repository.TxnFilter{PersonalOwnerID: 1, CustomOrder: true, PageSize: 2})
	mustOK(t, err)
	if total != 3 || len(rows) != 2 || rows[0].ID != current[0] || rows[1].ID != current[1] {
		t.Fatal("MySQL list order differs", rows)
	}
	// The real MySQL foreign key discards only the deleted row's preference.
	mustOK(t, db.Delete(&model.Transaction{}, current[1]).Error)
	afterDelete, err := r.IDs(filter)
	mustOK(t, err)
	if !slices.Equal(afterDelete, []int64{current[0], current[2]}) {
		t.Fatal("delete changed remaining order")
	}
	var dangling int64
	mustOK(t, db.Model(&model.TransactionDisplayOrder{}).Where("transaction_id = ?", current[1]).Count(&dangling).Error)
	if dangling != 0 {
		t.Fatal("deleted transaction left a preference")
	}
	down, err := os.ReadFile(filepath.Join("..", "..", "migrations", "000023_transaction_display_order.down.sql"))
	mustOK(t, err)
	mustOK(t, db.Exec(string(down)).Error)
	var count int64
	mustOK(t, db.Model(&model.Transaction{}).Count(&count).Error)
	if count != 2 {
		t.Fatal("rollback removed financial history")
	}
	migrate("000023_transaction_display_order.up.sql")
}

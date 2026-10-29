package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"
	"gorm.io/gorm"
	"jizhang/internal/model"
	"jizhang/internal/repository"
)

func TestXLSXMySQLSnapshotAndLimit(t *testing.T) {
	db, migrate := catalogMySQL(t)
	files, err := filepath.Glob(filepath.Join("..", "..", "migrations", "*.up.sql"))
	mustOK(t, err)
	for _, file := range files {
		if filepath.Base(file) >= "000012" {
			migrate(filepath.Base(file))
		}
	}
	mustOK(t, db.Create(&model.User{ID: 1, Username: "export-test", Role: model.RoleAdmin, Status: model.StatusActive, PasswordHash: "fixture-only"}).Error)
	account := model.Account{OwnerID: testOwner(1), Name: "读取前", Type: model.AccountAsset, Status: model.StatusActive}
	mustOK(t, db.Create(&account).Error)
	txn := model.Transaction{Kind: model.TxnExpense, Amount: dec("1.23"), AccountID: account.ID, EntryDate: mustDate(t, "2026-10-01"), PeriodKey: "2026-10", CreatedBy: 1, Source: model.SourceManual, Purpose: model.PurposeRegular}
	mustOK(t, db.Create(&txn).Error)
	s := NewTransactionService(TxnDeps{Txns: repository.NewTransactionRepo(db), Accounts: repository.NewAccountRepo(db), Cats: repository.NewCategoryRepo(db)})
	var changed atomic.Bool
	mustOK(t, db.Callback().Query().After("gorm:query").Register("export-concurrent-change", func(tx *gorm.DB) {
		if tx.Statement.Table == "transactions" && changed.CompareAndSwap(false, true) {
			mustOK(t, db.Model(&model.Account{}).Where("id=?", account.ID).Update("name", "读取后").Error)
		}
	}))
	rows, err := s.XLSXRows(context.Background(), adminUser(), repository.TxnFilter{})
	mustOK(t, err)
	mustOK(t, db.Callback().Query().Remove("export-concurrent-change"))
	if len(rows) != 1 || rows[0].Cells[3] != "读取前" || !changed.Load() {
		t.Fatal("mixed snapshot", rows)
	}
	rows, err = s.XLSXRows(context.Background(), adminUser(), repository.TxnFilter{})
	mustOK(t, err)
	if rows[0].Cells[3] != "读取后" {
		t.Fatal("later read did not see committed change")
	}
	// Insert directly into this random isolated database; no real ledger is used.
	batch := make([]model.Transaction, 999)
	for i := range batch {
		batch[i] = txn
		batch[i].ID = 0
		batch[i].Note = "导出规模测试，中文与特殊字符"
	}
	mustOK(t, db.CreateInBatches(batch, 500).Error)
	batch = make([]model.Transaction, 1000)
	for i := range batch {
		batch[i] = txn
		batch[i].ID = 0
		batch[i].Note = "导出规模测试，中文与特殊字符"
	}
	for i := 0; i < 99; i++ {
		for j := range batch {
			batch[j].ID = 0
		}
		mustOK(t, db.CreateInBatches(batch, 500).Error)
	}
	var count int64
	mustOK(t, db.Model(&model.Transaction{}).Where("account_id=?", account.ID).Count(&count).Error)
	if count != 100000 {
		t.Fatal(count)
	}
	ctx, cancel := context.WithTimeout(context.Background(), XLSXTimeout)
	defer cancel()
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	var peak atomic.Uint64
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				var m runtime.MemStats
				runtime.ReadMemStats(&m)
				for old := peak.Load(); m.Alloc > old; old = peak.Load() {
					if peak.CompareAndSwap(old, m.Alloc) {
						break
					}
				}
			}
		}
	}()
	start := time.Now()
	path, err := s.ExportXLSX(ctx, adminUser(), repository.TxnFilter{})
	close(done)
	mustOK(t, err)
	defer os.Remove(path)
	elapsed := time.Since(start)
	runtime.ReadMemStats(&after)
	info, err := os.Stat(path)
	mustOK(t, err)
	book, err := excelize.OpenFile(path)
	mustOK(t, err)
	iter, err := book.Rows("流水明细")
	mustOK(t, err)
	lines := 0
	for iter.Next() {
		lines++
	}
	mustOK(t, iter.Error())
	mustOK(t, iter.Close())
	mustOK(t, book.Close())
	if lines != 100001 || elapsed >= XLSXTimeout {
		t.Fatal("incomplete or too slow", lines, elapsed)
	}
	t.Logf("100000 rows: elapsed=%s file_bytes=%d peak_heap_bytes=%d allocated_bytes=%d", elapsed, info.Size(), peak.Load(), after.TotalAlloc-before.TotalAlloc)
	last := txn
	last.ID = 0
	mustOK(t, db.Create(&last).Error)
	var hydration atomic.Bool
	mustOK(t, db.Callback().Query().Before("gorm:query").Register("export-limit-hydration", func(tx *gorm.DB) {
		if tx.Statement.Table == "accounts" || tx.Statement.Table == "categories" {
			hydration.Store(true)
		}
	}))
	if path, err = s.ExportXLSX(context.Background(), adminUser(), repository.TxnFilter{}); path != "" || !errors.Is(err, ErrExportTooLarge) {
		t.Fatal(path, err)
	}
	if hydration.Load() {
		t.Fatal("overflow hydrated metadata")
	}
	mustOK(t, db.Callback().Query().Remove("export-limit-hydration"))
	data, err := s.ExportCSV(adminUser(), repository.TxnFilter{})
	mustOK(t, err)
	if len(csvDataLines(t, data)) != 100001 {
		t.Fatal("XLSX cap leaked into CSV", strconv.Itoa(len(data)))
	}
}

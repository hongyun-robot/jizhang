package service

import (
	"encoding/json"
	"errors"
	"jizhang/internal/model"
	"jizhang/internal/repository"
	"slices"
	"sync"
	"testing"
)

func TestCatalogOrderingMySQLMigration(t *testing.T) {
	for _, populated := range []bool{false, true} {
		t.Run(map[bool]string{false: "empty", true: "personal-history"}[populated], func(t *testing.T) {
			db, migrate := platformMigrationDB(t, populated)
			migrate("000015_personal_voucher_platforms.up.sql")
			if populated {
				mustOK(t, db.Exec("INSERT INTO voucher_platforms(owner_id,name) VALUES(1,'JD'),(1,'jd'),(1,'😀商家'),(2,'A自定义'),(2,'中名称')").Error)
			}
			var before []struct {
				ID, OwnerID int64
				Name        string
				CreatedAt   string
			}
			mustOK(t, db.Raw("SELECT id,owner_id,name,CAST(created_at AS CHAR) AS created_at FROM voucher_platforms ORDER BY owner_id,BINARY name,id").Scan(&before).Error)
			finance := platformFinancialSnapshot(t, db)
			migrate("000016_catalog_ordering.up.sql")
			rows := []model.VoucherPlatform{}
			mustOK(t, db.Order("owner_id,sort,id").Find(&rows).Error)
			position := map[int64]int{}
			for i, row := range rows {
				if row.ID != before[i].ID || row.Name != before[i].Name || row.OwnerID != before[i].OwnerID || row.Sort != position[row.OwnerID] {
					t.Fatal("migration changed metadata/order", row, before[i])
				}
				position[row.OwnerID]++
			}
			if platformFinancialSnapshot(t, db) != finance {
				t.Fatal("migration changed financial history")
			}
			migrate("000016_catalog_ordering.down.sql")
			if db.Migrator().HasColumn("voucher_platforms", "sort") {
				t.Fatal("down retained sort")
			}
			var after []struct {
				ID, OwnerID int64
				Name        string
				CreatedAt   string
			}
			mustOK(t, db.Raw("SELECT id,owner_id,name,CAST(created_at AS CHAR) AS created_at FROM voucher_platforms ORDER BY owner_id,BINARY name,id").Scan(&after).Error)
			b, _ := json.Marshal(before)
			a, _ := json.Marshal(after)
			if string(b) != string(a) || platformFinancialSnapshot(t, db) != finance {
				t.Fatal("down changed identity or finance")
			}
		})
	}
}

func TestCatalogOrderingMySQLConcurrency(t *testing.T) {
	db, migrate := platformMigrationDB(t, true)
	migrate("000015_personal_voucher_platforms.up.sql")
	migrate("000016_catalog_ordering.up.sql")
	e := seedVoucherEnv(t, db)
	actor := adminUser()
	s := NewAccountService(repository.NewAccountRepo(db))
	_, err := s.ListFor(actor)
	mustOK(t, err)
	before, err := repository.AccountOrder(db, 1).IDs()
	mustOK(t, err)
	first := slices.Clone(before)
	slices.Reverse(first)
	second := append(slices.Clone(before[1:]), before[0])
	var wg sync.WaitGroup
	errs := make([]error, 2)
	start := make(chan struct{})
	for i, target := range [][]int64{first, second} {
		wg.Go(func() {
			<-start
			_, errs[i] = s.ReorderFor(actor, CatalogOrderInput{target, before}, []string{"first", "second"}[i])
		})
	}
	close(start)
	wg.Wait()
	success, conflict := 0, 0
	for _, err := range errs {
		if err == nil {
			success++
		} else if errors.Is(err, ErrOrderConflict) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatal("stale writers both committed", errs)
	}
	current, err := repository.AccountOrder(db, 1).IDs()
	mustOK(t, err)
	old := first
	oldKey := "first"
	if errs[0] != nil {
		old = second
		oldKey = "second"
	}
	_, err = s.ReorderFor(actor, CatalogOrderInput{old, before}, oldKey)
	mustOK(t, err)
	_, err = s.ReorderFor(actor, CatalogOrderInput{before, current})
	mustOK(t, err)
	_, err = s.ReorderFor(actor, CatalogOrderInput{old, before}, oldKey)
	if !errors.Is(err, ErrOrderConflict) {
		t.Fatal("replayed old request overwrote current", err)
	}
	// Make the first saved target the new baseline, then race against a new account.
	base, err := repository.AccountOrder(db, 1).IDs()
	mustOK(t, err)
	next := slices.Clone(base)
	slices.Reverse(next)
	var sortErr, createErr error
	var created *model.Account
	start = make(chan struct{})
	wg.Go(func() { <-start; _, sortErr = s.ReorderFor(actor, CatalogOrderInput{next, base}) })
	wg.Go(func() {
		<-start
		created, createErr = s.CreateFor(actor, CreateAccountInput{Name: "并发新增", Type: model.AccountAsset})
	})
	close(start)
	wg.Wait()
	mustOK(t, createErr)
	if sortErr != nil && !errors.Is(sortErr, ErrOrderConflict) {
		t.Fatal(sortErr)
	}
	ids, err := repository.AccountOrder(db, 1).IDs()
	mustOK(t, err)
	if ids[len(ids)-1] != created.ID {
		t.Fatal("concurrent new account was not appended", ids)
	}
	// A real MySQL trigger rejects the second position update; nothing may commit.
	base = ids
	next = slices.Clone(base)
	slices.Reverse(next)
	mustOK(t, db.Exec("CREATE TRIGGER catalog_order_reject BEFORE UPDATE ON accounts FOR EACH ROW BEGIN IF NEW.sort=1 THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='ordering storage rejected'; END IF; END").Error)
	_, err = s.ReorderFor(actor, CatalogOrderInput{next, base})
	if err == nil {
		t.Fatal("trigger failure ignored")
	}
	mustOK(t, db.Exec("DROP TRIGGER catalog_order_reject").Error)
	ids, err = repository.AccountOrder(db, 1).IDs()
	mustOK(t, err)
	if !slices.Equal(ids, base) {
		t.Fatal("partial transaction committed", ids, base)
	}
	// Deleting a no-history account cannot let an obsolete full-list request partially save.
	var deleteErr error
	start = make(chan struct{})
	wg.Go(func() { <-start; _, sortErr = s.ReorderFor(actor, CatalogOrderInput{next, base}) })
	wg.Go(func() { <-start; deleteErr = s.DeleteFor(actor, created.ID) })
	close(start)
	wg.Wait()
	mustOK(t, deleteErr)
	if sortErr != nil && !errors.Is(sortErr, ErrOrderNotFound) {
		t.Fatal(sortErr)
	}
	ids, err = repository.AccountOrder(db, 1).IDs()
	mustOK(t, err)
	if slices.Contains(ids, created.ID) {
		t.Fatal("deleted account retained")
	}
	e.balance(t, "1000")
}

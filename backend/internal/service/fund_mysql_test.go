package service

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"jizhang/internal/fuyao"
	"jizhang/internal/model"
	"jizhang/internal/repository"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func fundMySQLSchema(t *testing.T) (*repository.FundRepo, func(string)) {
	t.Helper()
	if dsn := os.Getenv("FUND_MYSQL_DSN"); dsn != "" {
		t.Setenv("PERSONAL_CATALOG_MYSQL_DSN", dsn)
	}
	db, migrate := catalogMySQL(t)
	mustOK(t, db.Exec("INSERT INTO users(id,username,password_hash,role,status) VALUES(1,'fund-admin','hash','admin','active'),(2,'fund-member','hash','member','active')").Error)
	migrate("000012_personal_catalogs.up.sql")
	migrate("000013_installment_principal_remainder.up.sql")
	return repository.NewFundRepo(db), migrate
}

func TestFundMySQLMigration(t *testing.T) {
	t.Run("empty-up-down-preserves-balances", func(t *testing.T) {
		r, migrate := fundMySQLSchema(t)
		db := r.DB()
		before, e := repository.NewAccountRepo(db).List()
		mustOK(t, e)
		migrate("000014_personal_fund_accounts.up.sql")
		after, e := repository.NewAccountRepo(db).List()
		mustOK(t, e)
		if len(before) != len(after) {
			t.Fatal("migration seeded accounts")
		}
		for i := range before {
			if before[i].ID != after[i].ID || !before[i].Balance.Equal(after[i].Balance) {
				t.Fatal("old balance changed")
			}
		}
		var n int64
		mustOK(t, db.Model(&model.FundPosition{}).Count(&n).Error)
		if n != 0 {
			t.Fatal("positions seeded")
		}
		migrate("000014_personal_fund_accounts.down.sql")
		if db.Migrator().HasTable(&model.FundPosition{}) || db.Migrator().HasColumn(&model.Account{}, "AssetClass") {
			t.Fatal("down retained fund schema")
		}
	})
	t.Run("ownership-precision-keys-and-down-guard", func(t *testing.T) {
		r, migrate := fundMySQLSchema(t)
		db := r.DB()
		migrate("000014_personal_fund_accounts.up.sql")
		accounts := []model.Account{}
		for _, id := range []int64{1, 2} {
			owner := id
			a := model.Account{Name: "基金", Type: model.AccountAsset, AssetClass: "fund", OwnerID: &owner, Status: model.StatusActive}
			mustOK(t, db.Create(&a).Error)
			accounts = append(accounts, a)
		}
		i := model.FundInstrument{THSCode: "023564.OF", Ticker: "023564", AssetType: "fund-otc"}
		mustOK(t, r.SaveInstrument(&i))
		for j, a := range accounts {
			p := model.FundPosition{OwnerID: int64(j + 1), AccountID: a.ID, InstrumentID: i.ID, ValuationBasis: "cny_otc_unit_nav", BasisConfirmedBy: int64(j + 1), BasisConfirmedAt: time.Now()}
			mustOK(t, db.Create(&p).Error)
		}
		otherInstrument := model.FundInstrument{THSCode: "023565.OF", Ticker: "023565", AssetType: "fund-otc"}
		mustOK(t, r.SaveInstrument(&otherInstrument))
		wrong := model.FundPosition{OwnerID: 2, AccountID: accounts[0].ID, InstrumentID: otherInstrument.ID, ValuationBasis: "cny_otc_unit_nav", BasisConfirmedBy: 2, BasisConfirmedAt: time.Now()}
		if db.Create(&wrong).Error == nil {
			t.Fatal("ownership foreign key missing")
		}
		for _, id := range []int64{1, 2} {
			mustOK(t, db.Create(&model.FundMutation{OwnerID: id, RequestKey: "same-key", PayloadHash: "hash", Action: "test"}).Error)
		}
		if db.Create(&model.FundMutation{OwnerID: 1, RequestKey: "same-key", PayloadHash: "hash", Action: "test"}).Error == nil {
			t.Fatal("duplicate request accepted")
		}
		date := time.Date(2026, 10, 12, 0, 0, 0, 0, time.Local)
		nav := model.FundNAV{InstrumentID: i.ID, NAVDate: date, UnitNAV: dec("1.19460001"), FetchedAt: time.Now()}
		_, e := r.SaveNAV(&nav, time.Now())
		mustOK(t, e)
		rows, e := r.NAVs(i.ID)
		mustOK(t, e)
		if len(rows) != 1 || rows[0].UnitNAV.String() != "1.19460001" {
			t.Fatal(rows)
		}
		body, e := os.ReadFile(filepath.Join("..", "..", "migrations", "000014_personal_fund_accounts.down.sql"))
		mustOK(t, e)
		if db.Exec(string(body)).Error == nil {
			t.Fatal("populated down accepted")
		}
		if !db.Migrator().HasTable(&model.FundPosition{}) {
			t.Fatal("down removed data before guard")
		}
	})
}

func TestFundMySQLConcurrentFinancialAndQuoteWrites(t *testing.T) {
	r, migrate := fundMySQLSchema(t)
	db := r.DB()
	migrate("000014_personal_fund_accounts.up.sql")
	owner := int64(1)
	actor := &model.User{ID: owner, Role: model.RoleAdmin, Status: model.StatusActive}
	bank := model.Account{Name: "并发现金", OwnerID: &owner, Type: model.AccountAsset, AssetClass: "cash", Status: model.StatusActive, OpeningBalance: dec("10000")}
	fund := model.Account{Name: "并发基金", OwnerID: &owner, Type: model.AccountAsset, AssetClass: "fund", Status: model.StatusActive}
	mustOK(t, db.Create(&bank).Error)
	mustOK(t, db.Create(&fund).Error)
	accounts := repository.NewAccountRepo(db)
	periods := NewPeriodService(repository.NewSettingRepo(db))
	txn := NewTransactionService(TxnDeps{Txns: repository.NewTransactionRepo(db), Accounts: accounts, Cats: repository.NewCategoryRepo(db), Closings: repository.NewClosingRepo(db), Periods: periods})
	market := &fundTestMarket{quotes: []fuyao.NAV{{Date: fday("2026-10-09"), Value: dec("1.2305")}, {Date: fday("2026-10-12"), Value: dec("1.1946")}}}
	svc := NewFundService(db, txn, market)
	now := fday("2026-11-02")
	svc.Now = func() time.Time { return now }
	svc.BindAccounts(accounts)
	item, e := svc.instrument(context.Background(), "023564")
	mustOK(t, e)
	mustOK(t, svc.syncInstrument(context.Background(), item.ID, true))
	input := FundInput{RequestKey: uuid.NewString(), Code: "023564", Shares: "100", Amount: "120", Fee: "0.54", TradeNAV: "1.1946", EffectiveDate: "2026-10-12", CashDate: "2026-10-12", CashAccountID: bank.ID, ValuationBasis: "cny_otc_unit_nav", BasisConfirmed: true}
	start := make(chan struct{})
	receipts := make(chan FundReceipt, 12)
	failures := make(chan error, 12)
	var wg sync.WaitGroup
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			result, err := svc.FirstBuy(context.Background(), actor, fund.ID, input)
			if err != nil {
				failures <- err
			} else {
				receipts <- result
			}
		}()
	}
	close(start)
	wg.Wait()
	close(receipts)
	close(failures)
	for err := range failures {
		t.Fatal(err)
	}
	var first FundReceipt
	for result := range receipts {
		if first.PositionID == 0 {
			first = result
		} else if first != result {
			t.Fatal("different retry receipts", first, result)
		}
	}
	for _, table := range []any{&model.FundEvent{}, &model.FundMutation{}} {
		var n int64
		mustOK(t, db.Model(table).Count(&n).Error)
		if n != 1 {
			t.Fatal("duplicate financial writes", n)
		}
	}
	rows, e := accounts.ForOwner(owner).List()
	mustOK(t, e)
	for _, a := range rows {
		if a.ID == bank.ID && a.Balance.String() != "9880" {
			t.Fatal("double bank debit", a.Balance)
		}
	}
	otherWorker := NewFundService(db, txn, market)
	otherWorker.Now = svc.Now
	mustOK(t, db.Model(&model.FundInstrument{}).Where("id = ?", item.ID).Updates(map[string]any{"next_attempt": nil, "lease_until": nil}).Error)
	market.quotes = append(market.quotes, fuyao.NAV{Date: fday("2026-10-13"), Value: dec("1.2")})
	wg.Add(3)
	var concurrentBuyError error
	go func() {
		defer wg.Done()
		_, concurrentBuyError = svc.Operate(context.Background(), actor, first.PositionID, FundInput{RequestKey: uuid.NewString(), Kind: "buy", Shares: "1", Amount: "1.2", TradeNAV: "1.2", EffectiveDate: "2026-10-13", CashDate: "2026-10-13", CashAccountID: bank.ID})
	}()
	for _, worker := range []*FundService{svc, otherWorker} {
		go func(s *FundService) { defer wg.Done(); _ = s.syncInstrument(context.Background(), item.ID, true) }(worker)
	}
	wg.Wait()
	mustOK(t, concurrentBuyError)
	snapshots, e := svc.Valuations(actor, first.PositionID, "", "")
	mustOK(t, e)
	var currentPosition model.FundPosition
	mustOK(t, db.First(&currentPosition, first.PositionID).Error)
	if len(snapshots) == 0 || snapshots[0].EventVersion != currentPosition.EventVersion || snapshots[0].Shares.String() != "101" || snapshots[0].MarketValue.String() != "121.2" {
		t.Fatal("quote/trade race published stale shares or version", snapshots, currentPosition)
	}
	var quotes int64
	mustOK(t, db.Model(&model.FundNAV{}).Count(&quotes).Error)
	if quotes != 3 {
		t.Fatal("duplicate quote", quotes)
	}
	closing := NewClosingService(ClosingDeps{Closings: repository.NewClosingRepo(db), Audits: repository.NewAuditRepo(db), Periods: periods, Stats: repository.NewStatsRepo(db)})
	closing.now = svc.Now
	ready := make(chan struct{})
	var closeError, sellError error
	wg.Add(2)
	go func() { defer wg.Done(); <-ready; _, closeError = closing.Close(actor, "2026-10") }()
	go func() {
		defer wg.Done()
		<-ready
		_, sellError = svc.Operate(context.Background(), actor, first.PositionID, FundInput{RequestKey: uuid.NewString(), Kind: "redeem", Shares: "50", Amount: "59.73", TradeNAV: "1.1946", EffectiveDate: "2026-10-12", CashDate: "2026-10-13", CashAccountID: bank.ID})
	}()
	close(ready)
	wg.Wait()
	mustOK(t, closeError)
	if sellError != nil && !errors.Is(sellError, ErrTxnClosedPeriod) {
		t.Fatal(sellError)
	}
	v, e := svc.Position(actor, first.PositionID)
	mustOK(t, e)
	if sellError == nil && v.Shares.String() != "51" {
		t.Fatal("sale partial state", v)
	}
	if sellError != nil && v.Shares.String() != "101" {
		t.Fatal("closed write changed holdings", v)
	}
}

func TestFundMySQLScheduledClaimsAcrossWorkersAndRestart(t *testing.T) {
	r, migrate := fundMySQLSchema(t)
	migrate("000014_personal_fund_accounts.up.sql")
	item := model.FundInstrument{THSCode: "023564.OF", Ticker: "023564", AssetType: "fund-otc"}
	mustOK(t, r.SaveInstrument(&item))
	other := model.FundInstrument{THSCode: "012345.OF", Ticker: "012345", AssetType: "fund-otc"}
	mustOK(t, r.SaveInstrument(&other))
	claim := func(now time.Time, morning bool) int {
		t.Helper()
		run, active := fundRunAt(now)
		if !active || run.Morning != morning {
			t.Fatal("invalid test clock", now)
		}
		var wg sync.WaitGroup
		results := make(chan bool, 12)
		failures := make(chan error, 12)
		for range 12 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				// Recreate repository handles to model separate workers.
				claimed, err := repository.NewFundRepo(r.DB()).ClaimScheduled(item.ID, now, run.TargetDate, run.SlotStart, run.Morning)
				results <- claimed
				failures <- err
			}()
		}
		wg.Wait()
		close(results)
		close(failures)
		for err := range failures {
			mustOK(t, err)
		}
		count := 0
		for result := range results {
			if result {
				count++
			}
		}
		return count
	}
	if n := claim(fundClock("2026-10-16 19:00:00"), false); n != 1 {
		t.Fatal("duplicate evening claims", n)
	}
	mustOK(t, r.DB().Model(&item).Update("lease_until", nil).Error)
	if n := claim(fundClock("2026-10-16 19:10:00"), false); n != 0 {
		t.Fatal("same slot resumed again", n)
	}
	if n := claim(fundClock("2026-10-16 19:30:00"), false); n != 1 {
		t.Fatal("next evening slot skipped", n)
	}
	mustOK(t, r.DB().Model(&item).Updates(map[string]any{"lease_until": nil, "evening_completed_for": fday("2026-10-16")}).Error)
	if n := claim(fundClock("2026-10-16 20:00:00"), false); n != 0 {
		t.Fatal("completed evening repeated", n)
	}
	// Morning is independent from evening completion and an earlier manual
	// refresh's backoff, but its attempted target is atomically claimed once.
	mustOK(t, r.DB().Model(&item).Update("next_attempt", fundClock("2026-10-17 05:00:00")).Error)
	if n := claim(fundClock("2026-10-17 04:00:00"), true); n != 1 {
		t.Fatal("missing/duplicate morning claims", n)
	}
	mustOK(t, r.DB().Model(&item).Update("lease_until", nil).Error)
	if n := claim(fundClock("2026-10-17 04:10:00"), true); n != 0 {
		t.Fatal("morning retried after restart", n)
	}
	mustOK(t, r.DB().First(&item, item.ID).Error)
	mustOK(t, r.DB().First(&other, other.ID).Error)
	if !fundSameDay(item.MorningAttemptedFor, fday("2026-10-16")) || other.MorningAttemptedFor != nil {
		t.Fatal("claim target or code scope incorrect", item, other)
	}
}

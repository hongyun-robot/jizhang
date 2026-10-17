package service

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"jizhang/internal/ccb"
	"jizhang/internal/model"
	"jizhang/internal/repository"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func goldMySQLSchema(t *testing.T, latest bool) (*goldEnv, func(string)) {
	t.Helper()
	dsn := os.Getenv("GOLD_MYSQL_DSN")
	if dsn == "" {
		t.Skip("set GOLD_MYSQL_DSN; only a random disposable database is used")
	}
	t.Setenv("PERSONAL_CATALOG_MYSQL_DSN", dsn)
	r, migrate := fundMySQLSchema(t)
	db := r.DB()
	for _, f := range []string{"000014_personal_fund_accounts.up.sql", "000015_personal_voucher_platforms.up.sql", "000016_catalog_ordering.up.sql", "000017_ccb_gold_accounts.up.sql"} {
		migrate(f)
	}
	if latest {
		migrate("000018_bank_wealth_accounts.up.sql")
	}
	accounts := repository.NewAccountRepo(db)
	periods := NewPeriodService(repository.NewSettingRepo(db))
	txn := NewTransactionService(TxnDeps{Txns: repository.NewTransactionRepo(db), Accounts: accounts, Cats: repository.NewCategoryRepo(db), Closings: repository.NewClosingRepo(db), Periods: periods})
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, ccb.Shanghai)
	market := &goldTestMarket{q: ccb.Quote{Buy: dec("899.04"), Sell: dec("893.04"), At: now}}
	s := NewGoldService(db, txn, market)
	f := &fundEnv{db: db, txn: txn, accounts: accounts, now: now}
	g := &goldEnv{fundEnv: f, gold: s, marketGold: market}
	s.Now = func() time.Time { return g.now }
	s.BindAccounts(accounts)
	for _, id := range []int64{1, 2} {
		var user model.User
		mustOK(t, db.First(&user, id).Error)
		g.users = append(g.users, &user)
		owner := id
		cash := model.Account{Name: "测试现金", Type: model.AccountAsset, AssetClass: "cash", OwnerID: &owner, OpeningBalance: dec("20000"), Status: model.StatusActive}
		gold := model.Account{Name: "测试黄金", Type: model.AccountAsset, AssetClass: "gold", OwnerID: &owner, Status: model.StatusActive}
		mustOK(t, db.Create(&cash).Error)
		mustOK(t, db.Create(&gold).Error)
		g.banks = append(g.banks, cash.ID)
		g.ids = append(g.ids, gold.ID)
	}
	return g, migrate
}
func TestGoldMySQLSharedWorkersAndClosingRace(t *testing.T) {
	g, _ := goldMySQLSchema(t, true)
	other := NewGoldService(g.db, g.txn, g.marketGold)
	other.Now = g.gold.Now
	var workers sync.WaitGroup
	workerErrors := make(chan error, 4)
	for i := 0; i < 4; i++ {
		workers.Add(1)
		go func(i int) {
			defer workers.Done()
			s := g.gold
			if i%2 == 1 {
				s = other
			}
			workerErrors <- s.Ensure(context.Background())
		}(i)
	}
	workers.Wait()
	close(workerErrors)
	for e := range workerErrors {
		mustOK(t, e)
	}
	if g.marketGold.calls.Load() != 1 {
		t.Fatal("workers duplicated public query", g.marketGold.calls.Load())
	}
	restarted := NewGoldService(g.db, g.txn, g.marketGold)
	restarted.Now = g.gold.Now
	mustOK(t, restarted.Ensure(context.Background()))
	if g.marketGold.calls.Load() != 1 {
		t.Fatal("restart repeated slot")
	}
	held, release := make(chan struct{}), make(chan struct{})
	closeResult := make(chan error, 1)
	go func() {
		closeResult <- repository.LedgerWrite(g.db, func(tx *gorm.DB) error {
			close(held)
			<-release
			return tx.Create(&model.PeriodClosing{PeriodKey: "2026-10", Status: model.ClosingClosed, ClosedBy: 1, ClosedAt: g.now}).Error
		})
	}()
	<-held
	writeResult := make(chan error, 1)
	go func() {
		_, e := g.gold.Write(g.users[0], g.ids[0], GoldInput{RequestKey: uuid.NewString(), Kind: "buy", Grams: "1", CashAmount: "800", CashAccountID: g.banks[0]}, false)
		writeResult <- e
	}()
	close(release)
	mustOK(t, <-closeResult)
	if e := <-writeResult; !errors.Is(e, ErrTxnClosedPeriod) {
		t.Fatal("closing did not serialize trade", e)
	}
	var count int64
	mustOK(t, g.db.Model(&model.GoldEvent{}).Count(&count).Error)
	if count != 0 {
		t.Fatal("closed trade partially persisted")
	}
}
func TestGoldMySQLConcurrencyPrecisionAndRollbackGuard(t *testing.T) {
	g, _ := goldMySQLSchema(t, true)
	in := GoldInput{RequestKey: uuid.NewString(), Kind: "buy", Grams: "10.12345678", CashAmount: "8010", CashAccountID: g.banks[0], TradeDate: "2026-10-15"}
	var wg sync.WaitGroup
	errs := make(chan error, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, e := g.gold.Write(g.users[0], g.ids[0], in, false); errs <- e }()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		mustOK(t, e)
	}
	v, e := g.gold.Account(g.users[0], g.ids[0])
	mustOK(t, e)
	if v.Grams.String() != "10.12345678" || g.bank(t, 0) != "11990.00" {
		t.Fatal(v, g.bank(t, 0))
	}
	_, e = g.gold.Refresh(context.Background(), g.users[0])
	mustOK(t, e)
	v, e = g.gold.Account(g.users[0], g.ids[0])
	mustOK(t, e)
	if v.MarketValue == nil {
		t.Fatal("valid quote missing")
	}
	var txns int64
	mustOK(t, g.db.Model(&model.Transaction{}).Where("gold_event_id IS NOT NULL").Count(&txns).Error)
	if txns != 1 {
		t.Fatal(txns)
	}
	wrong := model.GoldPosition{OwnerID: 2, AccountID: g.ids[0], ProductKey: ccb.ProductKey}
	if g.db.Create(&wrong).Error == nil {
		t.Fatal("foreign ownership accepted")
	}
	body, e := os.ReadFile(filepath.Join("..", "..", "migrations", "000017_ccb_gold_accounts.down.sql"))
	mustOK(t, e)
	if g.db.Exec(string(body)).Error == nil {
		t.Fatal("destructive down accepted")
	}
	if !g.db.Migrator().HasTable(&model.GoldEvent{}) {
		t.Fatal("guard ran too late")
	}
	r := g.write(t, GoldInput{Kind: "sell", Grams: "10.12345678", CashAmount: "9000", CashAccountID: g.banks[0]})
	v, e = g.gold.Account(g.users[0], g.ids[0])
	mustOK(t, e)
	if !v.Grams.IsZero() || !v.Cost.IsZero() || v.RealizedPnL.String() != "990" {
		t.Fatal(v)
	}
	_, e = g.gold.Undo(g.users[0], g.ids[0], r.EventID, uuid.NewString())
	mustOK(t, e)
	if g.db.Exec(string(body)).Error == nil {
		t.Fatal("retained history lost guard")
	}
}
func TestGoldMySQLEmptyMigrationDown(t *testing.T) {
	g, migrate := goldMySQLSchema(t, false)
	// Only these newly created, never-used test accounts may be removed.
	mustOK(t, g.db.Where("asset_class = ?", "gold").Delete(&model.Account{}).Error)
	_, e := g.gold.Refresh(context.Background(), g.users[0])
	mustOK(t, e)
	before, e := repository.NewAccountRepo(g.db).Metadata()
	mustOK(t, e)
	migrate("000017_ccb_gold_accounts.down.sql")
	if g.db.Migrator().HasTable(&model.GoldPosition{}) || g.db.Migrator().HasColumn(&model.Transaction{}, "GoldEventID") {
		t.Fatal("empty down incomplete")
	}
	after, e := repository.NewAccountRepo(g.db).Metadata()
	mustOK(t, e)
	if len(before) != len(after) {
		t.Fatal("legacy accounts changed")
	}
}
func TestGoldMySQLAccountLifecycleAndOrdering(t *testing.T) {
	g, _ := goldMySQLSchema(t, true)
	svc := NewAccountService(g.accounts)
	input := CreateAccountInput{Name: "追加黄金", Type: model.AccountAsset, AssetClass: "gold", ActorID: 1, RequestKey: uuid.NewString()}
	a, e := svc.Create(input)
	mustOK(t, e)
	if a.OpeningBalance.String() != "0" || !a.IsGold() {
		t.Fatal(a)
	}
	initialSort := a.Sort
	input.Name = "改名黄金"
	input.RequestKey = ""
	updated, e := svc.Update(a.ID, input, model.StatusDisabled)
	mustOK(t, e)
	if updated.Sort != initialSort {
		t.Fatal("rename reset order")
	}
	_, e = svc.Update(a.ID, input, model.StatusActive)
	mustOK(t, e)
	_, e = g.gold.Write(g.users[0], a.ID, GoldInput{RequestKey: uuid.NewString(), Kind: "opening", Grams: "1", CostMode: "total", Cost: "800"}, false)
	mustOK(t, e)
	input.AssetClass = "cash"
	input.AssetClassProvided = true
	if _, e = svc.Update(a.ID, input, model.StatusActive); !errors.Is(e, ErrAccountHasTransactions) {
		t.Fatal("reclassified financial history", e)
	}
	if e = svc.Delete(a.ID); !errors.Is(e, ErrAccountHasTransactions) {
		t.Fatal(e)
	}
	rows, e := g.accounts.ForOwner(1).List()
	mustOK(t, e)
	for _, row := range rows {
		if row.ID == a.ID && (row.Sort != initialSort || !row.HasFinancialHistory) {
			t.Fatal("order/history lost", row)
		}
	}
}

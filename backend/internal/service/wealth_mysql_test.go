package service

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"jizhang/internal/ccb"
	"jizhang/internal/suyin"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"
	"jizhang/internal/model"
	"jizhang/internal/repository"
)

func wealthMySQLSchema(t *testing.T) (*gorm.DB, func(string)) {
	t.Helper()
	if dsn := os.Getenv("WEALTH_MYSQL_DSN"); dsn != "" {
		t.Setenv("PERSONAL_CATALOG_MYSQL_DSN", dsn)
	}
	db, migrate := catalogMySQL(t)
	mustOK(t, db.Exec("INSERT INTO users(id,username,password_hash,role,status) VALUES(1,'wealth-admin','hash','admin','active'),(2,'wealth-member','hash','member','active'),(3,'wealth-viewer','hash','viewer','active')").Error)
	for _, file := range []string{"000012_personal_catalogs.up.sql", "000013_installment_principal_remainder.up.sql", "000014_personal_fund_accounts.up.sql", "000015_personal_voucher_platforms.up.sql", "000016_catalog_ordering.up.sql", "000017_ccb_gold_accounts.up.sql"} {
		migrate(file)
	}
	return db, migrate
}

func TestWealthMySQLMigration(t *testing.T) {
	t.Run("round-trip-preserves-existing-assets-and-seeds-only-pending-public-identity", func(t *testing.T) {
		db, migrate := wealthMySQLSchema(t)
		before, err := repository.NewAccountRepo(db).List()
		mustOK(t, err)
		migrate("000018_bank_wealth_accounts.up.sql")
		r := repository.NewWealthRepo(db)
		product, err := r.ProductForCode("J07241")
		mustOK(t, err)
		if product == nil || product.AdmissionStatus != "pending_validation" || product.EvidenceVersion != "" || product.RegistrationCode != "Z7003125000238" || product.SourceCode != "Y0408Ux" {
			t.Fatal(product)
		}
		catalog, err := r.Catalog()
		mustOK(t, err)
		if len(catalog) != 0 {
			t.Fatal("unqualified product offered", catalog)
		}
		for _, table := range []string{"wealth_positions", "wealth_events", "wealth_manual_navs", "wealth_navs"} {
			var n int64
			mustOK(t, db.Table(table).Count(&n).Error)
			if n != 0 {
				t.Fatal("personal data or default price seeded", table)
			}
		}
		after, err := repository.NewAccountRepo(db).List()
		mustOK(t, err)
		if len(before) != len(after) {
			t.Fatal("accounts seeded")
		}
		for i := range before {
			if before[i].ID != after[i].ID || !before[i].Balance.Equal(after[i].Balance) {
				t.Fatal("old assets changed")
			}
		}
		if db.Model(product).Updates(map[string]any{"admission_status": "enabled", "evidence_version": ""}).Error == nil {
			t.Fatal("enabled without evidence")
		}
		migrate("000018_bank_wealth_accounts.down.sql")
		if db.Migrator().HasTable("wealth_positions") || db.Migrator().HasColumn("transactions", "wealth_event_id") {
			t.Fatal("down retained schema")
		}
		migrate("000018_bank_wealth_accounts.up.sql")
		product, err = repository.NewWealthRepo(db).ProductForCode("J07241")
		mustOK(t, err)
		if product == nil || product.AdmissionStatus != "pending_validation" {
			t.Fatal(product)
		}
	})
	t.Run("owner-foreign-keys-precision-personal-overlay-scope-and-financial-rollback-guard", func(t *testing.T) {
		db, migrate := wealthMySQLSchema(t)
		migrate("000018_bank_wealth_accounts.up.sql")
		r := repository.NewWealthRepo(db)
		product, err := r.ProductForCode("J07241")
		mustOK(t, err)
		accounts := []model.Account{}
		for _, owner := range []int64{1, 1, 2} {
			ownerID := owner
			a := model.Account{Name: "同名理财", Type: model.AccountAsset, AssetClass: "wealth", OwnerID: &ownerID, Status: model.StatusActive}
			mustOK(t, db.Create(&a).Error)
			accounts = append(accounts, a)
			p := model.WealthPosition{OwnerID: owner, AccountID: a.ID, ProductID: product.ID, ValuationBasis: "cny_wealth_unit_nav", BasisConfirmedBy: owner, BasisConfirmedAt: time.Now()}
			mustOK(t, db.Create(&p).Error)
		}
		var p model.WealthPosition
		mustOK(t, db.Where("account_id = ?", accounts[0].ID).First(&p).Error)
		wrong := model.WealthPosition{OwnerID: 2, AccountID: accounts[1].ID, ProductID: product.ID, ValuationBasis: "cny_wealth_unit_nav", BasisConfirmedBy: 2, BasisConfirmedAt: time.Now()}
		if db.Create(&wrong).Error == nil {
			t.Fatal("cross-owner position")
		}
		if got, err := r.Position(2, p.ID, false); err != nil || got != nil {
			t.Fatal("position scope", got, err)
		}
		for _, owner := range []int64{1, 2} {
			mustOK(t, db.Create(&model.WealthManualNAV{OwnerID: owner, ProductID: product.ID, NAVDate: fday("2026-10-10"), UnitNAV: dec("1.12345678"), Revision: 1, SourceNote: "银行对账"}).Error)
		}
		data, err := r.LoadAccounts([]int64{accounts[0].ID, accounts[1].ID})
		mustOK(t, err)
		if len(data.Positions) != 2 || len(data.ManualNAVs) != 1 || data.ManualNAVs[0].OwnerID != 1 || data.ManualNAVs[0].UnitNAV.String() != "1.12345678" {
			t.Fatal("public identity broadened personal query", data)
		}
		event := model.WealthEvent{OwnerID: 2, AccountID: accounts[0].ID, PositionID: p.ID, Kind: "opening", Shares: dec("100"), Amount: dec("100"), EffectiveDate: fday("2026-10-10"), RightsPositionID: p.ID, CreatedBy: 2}
		if db.Create(&event).Error == nil {
			t.Fatal("cross-owner event")
		}
		for _, owner := range []int64{1, 2} {
			mustOK(t, db.Create(&model.WealthMutation{OwnerID: owner, RequestKey: "shared-key", PayloadHash: "hash", Action: "test"}).Error)
		}
		if db.Create(&model.WealthMutation{OwnerID: 1, RequestKey: "shared-key", PayloadHash: "hash", Action: "test"}).Error == nil {
			t.Fatal("duplicate mutation")
		}
		assertWealthDownRefused(t, db)
	})
	t.Run("manual-audit-alone-blocks-destructive-down", func(t *testing.T) {
		db, migrate := wealthMySQLSchema(t)
		migrate("000018_bank_wealth_accounts.up.sql")
		r := repository.NewWealthRepo(db)
		product, err := r.ProductForCode("J07241")
		mustOK(t, err)
		nav := model.WealthManualNAV{OwnerID: 1, ProductID: product.ID, NAVDate: fday("2026-10-10"), UnitNAV: dec("1.02"), Revision: 1, SourceNote: "银行对账", Withdrawn: true}
		mustOK(t, db.Create(&nav).Error)
		audit := model.WealthManualAudit{OwnerID: 1, ProductID: product.ID, ManualNAVID: nav.ID, ActorID: 1, Action: "withdraw", BeforeJSON: "{}", AfterJSON: "{}"}
		mustOK(t, db.Create(&audit).Error)
		wrong := audit
		wrong.ID = 0
		wrong.OwnerID = 2
		if db.Create(&wrong).Error == nil {
			t.Fatal("cross-owner manual audit")
		}
		assertWealthDownRefused(t, db)
	})
	t.Run("concurrent-account-product-unique-key", func(t *testing.T) {
		db, migrate := wealthMySQLSchema(t)
		migrate("000018_bank_wealth_accounts.up.sql")
		owner := int64(1)
		account := model.Account{Name: "理财", Type: model.AccountAsset, AssetClass: "wealth", OwnerID: &owner, Status: model.StatusActive}
		mustOK(t, db.Create(&account).Error)
		product, err := repository.NewWealthRepo(db).ProductForCode("J07241")
		mustOK(t, err)
		var wg sync.WaitGroup
		start := make(chan struct{})
		results := make(chan error, 8)
		for range 8 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				p := model.WealthPosition{OwnerID: owner, AccountID: account.ID, ProductID: product.ID, ValuationBasis: "cny_wealth_unit_nav", BasisConfirmedBy: owner, BasisConfirmedAt: time.Now()}
				results <- db.Create(&p).Error
			}()
		}
		close(start)
		wg.Wait()
		close(results)
		success := 0
		for err := range results {
			if err == nil {
				success++
			}
		}
		if success != 1 {
			t.Fatal("duplicate holdings", success)
		}
	})
}

func assertWealthDownRefused(t *testing.T, db *gorm.DB) {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "..", "migrations", "000018_bank_wealth_accounts.down.sql"))
	mustOK(t, err)
	if db.Exec(string(body)).Error == nil {
		t.Fatal("populated down accepted")
	}
	if !db.Migrator().HasTable("wealth_products") || !db.Migrator().HasTable("wealth_manual_audits") || !db.Migrator().HasColumn("transactions", "wealth_event_id") {
		t.Fatal("guard ran after destructive statements")
	}
}

func newWealthMySQLEnv(t *testing.T) *wealthEnv {
	t.Helper()
	db, migrate := wealthMySQLSchema(t)
	migrate("000018_bank_wealth_accounts.up.sql")
	migrate("000019_wealth_pending_orders.up.sql")
	mustOK(t, db.Model(&model.WealthProduct{}).Where("id = ?", 1).Updates(map[string]any{"admission_status": "enabled", "evidence_version": "isolated-integration-fixture"}).Error)
	accounts := repository.NewAccountRepo(db)
	periods := NewPeriodService(repository.NewSettingRepo(db))
	txns := NewTransactionService(TxnDeps{Txns: repository.NewTransactionRepo(db), Accounts: accounts, Cats: repository.NewCategoryRepo(db), Closings: repository.NewClosingRepo(db), Periods: periods})
	svc := NewWealthService(db, txns, nil)
	env := &wealthEnv{db: db, svc: svc, txn: txns, accounts: accounts, now: fday("2026-10-16")}
	svc.Now = func() time.Time { return env.now }
	svc.BindAccounts(accounts)
	for _, id := range []int64{1, 2, 3} {
		var user model.User
		mustOK(t, db.First(&user, id).Error)
		env.users = append(env.users, &user)
		owner := id
		cash := model.Account{Name: "理财测试现金", OwnerID: &owner, Type: model.AccountAsset, AssetClass: "cash", OpeningBalance: dec("5000"), Status: model.StatusActive}
		wealth := model.Account{Name: "理财测试", OwnerID: &owner, Type: model.AccountAsset, AssetClass: "wealth", Status: model.StatusActive}
		mustOK(t, db.Create(&cash).Error)
		mustOK(t, db.Create(&wealth).Error)
		env.banks = append(env.banks, cash.ID)
		env.wealths = append(env.wealths, wealth.ID)
	}
	return env
}

func TestWealthMySQLConcurrentFinancialRetriesAndQuoteOverlayCoordination(t *testing.T) {
	e := newWealthMySQLEnv(t)
	ctx := context.Background()
	input := WealthInput{RequestKey: uuid.NewString(), Code: "J07241", Shares: "990", Amount: "1000", Fee: "10", TradeNAV: "1", EffectiveDate: "2026-10-12", CashDate: "2026-10-10", CashAccountID: e.banks[0]}
	var wg sync.WaitGroup
	start := make(chan struct{})
	receipts := make(chan WealthReceipt, 12)
	failures := make(chan error, 12)
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			receipt, err := e.svc.FirstBuy(ctx, e.users[0], e.wealths[0], input)
			if err != nil {
				failures <- err
			} else {
				receipts <- receipt
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
	var first WealthReceipt
	for r := range receipts {
		if first.PositionID == 0 {
			first = r
		} else if r != first {
			t.Fatal("different retry receipts", first, r)
		}
	}
	for _, table := range []string{"wealth_positions", "wealth_events", "wealth_mutations", "transactions", "wealth_audits"} {
		var n int64
		mustOK(t, e.db.Table(table).Count(&n).Error)
		if n != 1 {
			t.Fatal("duplicate financial or audit write", table, n)
		}
	}
	if e.bank(t, 0) != "4000.00" {
		t.Fatal(e.bank(t, 0))
	}
	other := NewWealthService(e.db, e.txn, nil)
	other.Now = e.svc.Now
	ready := make(chan struct{})
	var manualErr, publicErr error
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-ready
		_, manualErr = e.svc.WriteManualNAV(e.users[0], 1, WealthManualInput{RequestKey: uuid.NewString(), NAVDate: "2026-10-12", UnitNAV: "1", SourceNote: "银行对账"})
	}()
	go func() {
		defer wg.Done()
		<-ready
		_, publicErr = other.SavePublicNAVs(1, []suyin.NAV{{Date: fday("2026-10-12"), Value: dec("1"), FetchedAt: e.now, SourceCode: "Y0408Ux"}})
	}()
	close(ready)
	wg.Wait()
	mustOK(t, publicErr)
	if manualErr != nil && !errors.Is(manualErr, ErrWealthAutomatic) {
		t.Fatal(manualErr)
	}
	v, err := e.svc.Position(e.users[0], first.PositionID)
	mustOK(t, err)
	if v.Quote == nil || v.Quote.Source != "automatic" || v.Quote.Revision != 1 || v.Shares.String() != "990" {
		t.Fatal(v)
	}
	snapshots, err := e.svc.Valuations(e.users[0], first.PositionID, "", "")
	mustOK(t, err)
	if len(snapshots) != 1 || snapshots[0].QuoteSource != "automatic" {
		t.Fatal(snapshots)
	}
	// Competing edits of one overlay version cannot silently overwrite.
	initial, err := e.svc.WriteManualNAV(e.users[0], 1, WealthManualInput{RequestKey: uuid.NewString(), NAVDate: "2026-10-13", UnitNAV: "1.01", SourceNote: "银行对账"})
	mustOK(t, err)
	edits := make(chan error, 2)
	wg.Add(2)
	for _, svc := range []*WealthService{e.svc, other} {
		go func(s *WealthService) {
			defer wg.Done()
			_, err := s.WriteManualNAV(e.users[0], 1, WealthManualInput{RequestKey: uuid.NewString(), NAVDate: "2026-10-13", UnitNAV: "1.02", ExpectedRevision: initial.Revision, SourceNote: "更正核对"})
			edits <- err
		}(svc)
	}
	wg.Wait()
	close(edits)
	success, conflict := 0, 0
	for err := range edits {
		if err == nil {
			success++
		} else if errors.Is(err, ErrWealthConflict) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatal(success, conflict)
	}
}

func TestWealthMySQLSealRaceAndAtomicPersistenceFailure(t *testing.T) {
	e := newWealthMySQLEnv(t)
	ctx := context.Background()
	r := e.opening(t, 0, "200", "1.15", "2026-10-09")
	injected := errors.New("injected wealth commit failure")
	mustOK(t, e.db.Callback().Create().Before("gorm:create").Register("wealth_mysql_failure", func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Table == "wealth_mutations" {
			tx.AddError(injected)
		}
	}))
	_, err := e.svc.Operate(ctx, e.users[0], r.PositionID, WealthInput{RequestKey: uuid.NewString(), Kind: "buy", Shares: "100", Amount: "110", TradeNAV: "1.1", CashAccountID: e.banks[0], CashDate: "2026-10-10", EffectiveDate: "2026-10-12"})
	if !errors.Is(err, injected) {
		t.Fatal(err)
	}
	mustOK(t, e.db.Callback().Create().Remove("wealth_mysql_failure"))
	if e.bank(t, 0) != "5000.00" {
		t.Fatal("partial cash", e.bank(t, 0))
	}
	v, err := e.svc.Position(e.users[0], r.PositionID)
	mustOK(t, err)
	if v.Shares.String() != "200" || v.Cost.String() != "230" {
		t.Fatal(v)
	}
	e.now = fday("2026-11-02")
	closing := NewClosingService(ClosingDeps{Closings: repository.NewClosingRepo(e.db), Audits: repository.NewAuditRepo(e.db), Periods: e.txn.deps.Periods, Stats: repository.NewStatsRepo(e.db)})
	closing.now = e.svc.Now
	ready := make(chan struct{})
	var closeErr, sellErr error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); <-ready; _, closeErr = closing.Close(e.users[0], "2026-10") }()
	go func() {
		defer wg.Done()
		<-ready
		_, sellErr = e.svc.Operate(ctx, e.users[0], r.PositionID, WealthInput{RequestKey: uuid.NewString(), Kind: "redeem", Shares: "100", Amount: "130", Fee: "2", EffectiveDate: "2026-10-13"})
	}()
	close(ready)
	wg.Wait()
	mustOK(t, closeErr)
	if sellErr != nil && !errors.Is(sellErr, ErrTxnClosedPeriod) {
		t.Fatal(sellErr)
	}
	v, err = e.svc.Position(e.users[0], r.PositionID)
	mustOK(t, err)
	if sellErr == nil && v.Shares.String() != "100" {
		t.Fatal(v)
	}
	if sellErr != nil && v.Shares.String() != "200" {
		t.Fatal(v)
	}
}

func TestWealthMySQLAccountLifecycleOrderingAndCashIsolation(t *testing.T) {
	e := newWealthMySQLEnv(t)
	svc := NewAccountService(e.accounts)
	actor := e.users[0]
	in := CreateAccountInput{Name: "本人理财", Type: model.AccountAsset, AssetClass: "wealth", ActorID: actor.ID, RequestKey: uuid.NewString()}
	a, err := svc.CreateFor(actor, in)
	mustOK(t, err)
	if !a.IsWealth() || !a.OpeningBalance.IsZero() {
		t.Fatal(a)
	}
	initialSort := a.Sort
	in.RequestKey = ""
	in.Name = "重命名理财"
	updated, err := svc.UpdateFor(actor, a.ID, in, model.StatusDisabled)
	mustOK(t, err)
	if updated.Sort != initialSort {
		t.Fatal("rename/disable reordered")
	}
	_, err = svc.UpdateFor(actor, a.ID, in, model.StatusActive)
	mustOK(t, err)
	opening, err := e.svc.CreateOpening(context.Background(), actor, a.ID, wealthOpening("100", "1", "2026-10-09"))
	mustOK(t, err)
	e.quote(t, "2026-10-10", "1.02")
	in.AssetClass = "cash"
	in.AssetClassProvided = true
	if _, err = svc.UpdateFor(actor, a.ID, in, model.StatusActive); !errors.Is(err, ErrAccountHasTransactions) {
		t.Fatal("reclassified history", err)
	}
	if err = svc.DeleteFor(actor, a.ID); !errors.Is(err, ErrAccountHasTransactions) {
		t.Fatal(err)
	}
	if err = svc.DeleteFor(e.users[1], a.ID); !errors.Is(err, ErrTxnForbidden) {
		t.Fatal(err)
	}
	in.AssetClass = "wealth"
	_, err = svc.UpdateFor(actor, a.ID, in, model.StatusDisabled)
	mustOK(t, err)
	value, err := e.svc.Account(actor, a.ID)
	mustOK(t, err)
	if value.TotalValue == nil || value.TotalValue.String() != "102" {
		t.Fatal("disabled holding vanished", value)
	}
	_, err = e.svc.Operate(context.Background(), actor, opening.PositionID, WealthInput{RequestKey: uuid.NewString(), Kind: "redeem", Shares: "50", Amount: "51", EffectiveDate: "2026-10-10"})
	mustOK(t, err)
	value, err = e.svc.Account(actor, a.ID)
	mustOK(t, err)
	if value.TotalValue == nil || value.TotalValue.String() != "102" {
		t.Fatal("disabled receivable vanished", value)
	}
	owned, err := svc.ListFor(actor)
	mustOK(t, err)
	ids := []int64{}
	for i := len(owned) - 1; i >= 0; i-- {
		ids = append(ids, owned[i].ID)
	}
	previousIDs := []int64{}
	for _, row := range owned {
		previousIDs = append(previousIDs, row.ID)
	}
	_, err = svc.ReorderFor(actor, CatalogOrderInput{OrderedIDs: ids, PreviousIDs: previousIDs}, uuid.NewString())
	mustOK(t, err)
	reordered, err := e.accounts.ForOwner(actor.ID).List()
	mustOK(t, err)
	for i, row := range reordered {
		if row.ID != ids[i] {
			t.Fatal("incomplete shared order", reordered)
		}
	}
	for _, input := range []TxnInput{{Kind: model.TxnTransfer, Amount: dec("1"), AccountID: e.banks[0], TransferAccountID: a.ID, EntryDate: fday("2026-10-10")}, {Kind: model.TxnTransfer, Amount: dec("1"), AccountID: a.ID, TransferAccountID: e.banks[0], EntryDate: fday("2026-10-10")}} {
		_, err = e.txn.Create(actor, input)
		if err == nil {
			t.Fatal("ordinary money used wealth account")
		}
	}
	in.OpeningBalance = dec("1")
	in.RequestKey = uuid.NewString()
	if _, err = svc.CreateFor(actor, in); !errors.Is(err, ErrInvalidAmount) {
		t.Fatal("cash opening allowed", err)
	}
}

func TestWealthMySQLScheduledLeaseClaimsAndRestart(t *testing.T) {
	e := newWealthMySQLEnv(t)
	e.opening(t, 0, "100", "1", "2026-10-09")
	now := e.now
	slots := wealthSlots(now, nil)
	var wg sync.WaitGroup
	start := make(chan struct{})
	results := make(chan bool, 8)
	failures := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			claimed, err := repository.NewWealthRepo(e.db).ClaimCollection(1, now, uuid.NewString(), slots, true)
			if err != nil {
				failures <- err
			}
			results <- claimed
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	close(failures)
	for err := range failures {
		t.Fatal(err)
	}
	count := 0
	for claimed := range results {
		if claimed {
			count++
		}
	}
	if count != 1 {
		t.Fatal("multiple effective product leases", count)
	}
	var n int64
	mustOK(t, e.db.Model(&model.WealthSyncSlot{}).Count(&n).Error)
	if n != 1 {
		t.Fatal("duplicate slot", n)
	}
	r := repository.NewWealthRepo(e.db)
	claimed, err := r.ClaimCollection(1, now.Add(91*time.Second), "restart-worker", nil, true)
	mustOK(t, err)
	if !claimed {
		t.Fatal("restart failed to recover unfinished slot")
	}
	mustOK(t, r.FinishCollection(1, "restart-worker", now.Add(92*time.Second), true, false, ""))
	claimed, err = r.ClaimCollection(1, now.Add(180*time.Second), "duplicate", slots, true)
	mustOK(t, err)
	if claimed {
		t.Fatal("completed slot replayed")
	}
}

func TestWealthMySQLMigrationPreservesExistingCashLiabilityFundAndGold(t *testing.T) {
	db, migrate := wealthMySQLSchema(t)
	owner := int64(1)
	actor := &model.User{ID: owner, Role: model.RoleAdmin, Status: model.StatusActive}
	r := repository.NewAccountRepo(db)
	periods := NewPeriodService(repository.NewSettingRepo(db))
	txns := NewTransactionService(TxnDeps{Txns: repository.NewTransactionRepo(db), Accounts: r, Cats: repository.NewCategoryRepo(db), Closings: repository.NewClosingRepo(db), Periods: periods})
	now := fday("2026-10-16")
	accounts := []model.Account{{Name: "旧现金", OwnerID: &owner, Type: model.AccountAsset, AssetClass: "cash", OpeningBalance: dec("5000"), Status: model.StatusActive}, {Name: "旧负债", OwnerID: &owner, Type: model.AccountLiability, OpeningBalance: dec("-1000"), Status: model.StatusActive}, {Name: "旧基金", OwnerID: &owner, Type: model.AccountAsset, AssetClass: "fund", Status: model.StatusActive}, {Name: "旧黄金", OwnerID: &owner, Type: model.AccountAsset, AssetClass: "gold", Status: model.StatusActive}}
	for i := range accounts {
		mustOK(t, db.Create(&accounts[i]).Error)
	}
	funds := NewFundService(db, txns, nil)
	funds.Now = func() time.Time { return now }
	funds.BindAccounts(r)
	instrument := model.FundInstrument{THSCode: "023564.OF", Ticker: "023564", AssetType: "fund-otc", Name: "旧基金"}
	mustOK(t, funds.repo.SaveInstrument(&instrument))
	_, err := funds.repo.SaveNAV(&model.FundNAV{InstrumentID: instrument.ID, NAVDate: fday("2026-10-15"), UnitNAV: dec("1.2"), FetchedAt: now}, now)
	mustOK(t, err)
	_, err = funds.CreateOpening(context.Background(), actor, accounts[2].ID, fundOpening("100", "1", "2026-10-09"))
	mustOK(t, err)
	gold := NewGoldService(db, txns, &goldTestMarket{q: ccb.Quote{Buy: dec("899.04"), Sell: dec("893.04"), At: now}})
	gold.Now = func() time.Time { return now }
	gold.BindAccounts(r)
	_, err = gold.Write(actor, accounts[3].ID, GoldInput{RequestKey: uuid.NewString(), Kind: "opening", Grams: "10", CostMode: "total", Cost: "8000", TradeDate: "2026-10-09"}, false)
	mustOK(t, err)
	mustOK(t, gold.Ensure(context.Background()))
	before, err := r.ForOwner(owner).List()
	mustOK(t, err)
	values := map[int64]string{}
	for _, a := range before {
		values[a.ID] = a.Balance.String()
	}
	for i, want := range []string{"5000", "-1000", "120", "8930.4"} {
		if values[accounts[i].ID] != want {
			t.Fatal("invalid old-asset fixture", values)
		}
	}
	for _, file := range []string{"000018_bank_wealth_accounts.up.sql", "000018_bank_wealth_accounts.down.sql", "000018_bank_wealth_accounts.up.sql"} {
		migrate(file)
		after, err := r.ForOwner(owner).List()
		mustOK(t, err)
		if len(after) != len(before) {
			t.Fatal("migration added personal accounts")
		}
		for _, a := range after {
			if values[a.ID] != a.Balance.String() {
				t.Fatal("existing asset changed", file, a.ID, values[a.ID], a.Balance)
			}
		}
	}
}

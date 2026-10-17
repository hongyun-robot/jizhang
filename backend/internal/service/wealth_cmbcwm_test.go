package service

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"jizhang/internal/cmbcwm"
	"jizhang/internal/model"
	"jizhang/internal/repository"
	"jizhang/internal/suyin"
	"jizhang/internal/wealthdata"
)

func addCmbcwmProduct(t *testing.T, env *wealthEnv) model.WealthProduct {
	t.Helper()
	identity, _ := wealthdata.Catalog("FBAE68020E")
	product := model.WealthProduct{Issuer: identity.Issuer, RegistrationCode: identity.RegistrationCode, Code: identity.Code, Name: identity.Name, Currency: identity.Currency, ValuationBasis: "cny_wealth_unit_nav", InceptionDate: fday(identity.InceptionDate), SourceAdapter: identity.Adapter, SourceCode: identity.SourceCode, AdmissionStatus: "enabled", EvidenceVersion: "isolated-cmbcwm-fixture"}
	mustOK(t, env.db.Create(&product).Error)
	return product
}

func cmbcwmOpening(t *testing.T, env *wealthEnv, owner int, shares string) WealthReceipt {
	t.Helper()
	input := wealthOpening(shares, "1", "2026-10-14")
	input.Code = "FBAE68020E"
	receipt, err := env.svc.CreateOpening(context.Background(), env.users[owner], env.wealths[owner], input)
	mustOK(t, err)
	return receipt
}

func TestWealthCmbcwmScheduledNAVsRevalueOwnersAndPreserveFinancialFacts(t *testing.T) {
	e := newWealthEnv(t)
	product := addCmbcwmProduct(t, e)
	e.now = time.Date(2026, 10, 16, 8, 0, 0, 0, wealthdata.Shanghai)
	e.opening(t, 0, "100", "1", "2026-10-14")
	e.quote(t, "2026-10-15", "1.02")
	first := cmbcwmOpening(t, e, 0, "10000")
	second := cmbcwmOpening(t, e, 1, "20000")
	market := &wealthTestMarket{quotes: []wealthdata.NAV{
		{Date: fday("2026-10-14"), Value: dec("1.068000"), SourceCode: product.Code, FetchedAt: e.now},
		{Date: fday("2026-10-15"), Value: dec("1.068033"), SourceCode: product.Code, FetchedAt: e.now},
	}}
	e.svc.market = wealthdata.NewRouter(map[string]wealthdata.Source{"suyin": suyin.New("", ""), "cmbcwm": market})
	invalidated := map[int64]int{}
	e.svc.OnWrite = func(owner int64) { invalidated[owner]++ }
	mustOK(t, e.svc.Ensure(context.Background()))
	for index, want := range []struct {
		receipt WealthReceipt
		value   string
		pnl     string
		daily   string
	}{{first, "10680.33", "680.33", "0.33"}, {second, "21360.66", "1360.66", "0.66"}} {
		position, err := e.svc.Position(e.users[index], want.receipt.PositionID)
		mustOK(t, err)
		if position.MarketValue == nil || position.MarketValue.String() != want.value || position.HoldingPnL.String() != want.pnl || position.DailyPnL.String() != want.daily || position.Shares.String() != []string{"10000", "20000"}[index] || position.Cost.String() != position.Shares.String() || position.NAVDate != "2026-10-15" || position.RebuildPending {
			t.Fatal(position)
		}
		if invalidated[e.users[index].ID] != 1 || e.bank(t, index) != "5000.00" {
			t.Fatal("owner cache or cash changed incorrectly", invalidated, e.bank(t, index))
		}
		history, err := e.svc.repo.Valuations(e.users[index].ID, want.receipt.PositionID, time.Time{}, time.Time{})
		mustOK(t, err)
		if len(history) != 2 {
			t.Fatal("historical valuation not rebuilt", history)
		}
	}
	suyinProduct, err := e.svc.repo.Product(1, false)
	mustOK(t, err)
	if suyinProduct.SyncStatus != "failed" || suyinProduct.LastError != wealthdata.ErrUnconfigured.Error() {
		t.Fatal("Minsheng configuration concealed missing Suyin collector", suyinProduct)
	}
	onlyCmbcwm, err := e.svc.Account(e.users[1], e.wealths[1])
	mustOK(t, err)
	if !onlyCmbcwm.Configured {
		t.Fatal("Minsheng required Suyin configuration")
	}
	var revisions, events, slots int64
	mustOK(t, e.db.Model(&model.WealthNAVRevision{}).Where("product_id = ?", product.ID).Count(&revisions).Error)
	mustOK(t, e.db.Model(&model.WealthEvent{}).Count(&events).Error)
	mustOK(t, e.db.Model(&model.WealthSyncSlot{}).Where("product_id = ?", product.ID).Count(&slots).Error)
	if revisions != 2 || events != 3 || slots != 1 {
		t.Fatal(revisions, events, slots)
	}
	// A restart before the next slot must not fetch or add derived gains again.
	mustOK(t, e.svc.Ensure(context.Background()))
	e.now = e.now.Add(4 * time.Hour)
	mustOK(t, e.svc.Ensure(context.Background()))
	state, err := e.svc.repo.Product(product.ID, false)
	mustOK(t, err)
	if state.SyncStatus != "no_new_nav" || invalidated[e.users[0].ID] != 1 || invalidated[e.users[1].ID] != 1 {
		t.Fatal("repeated quotes changed gains", state, invalidated)
	}
	// A legitimate revision revalues all holders without touching cost or cash.
	market.quotes[1].Value = dec("1.068043")
	e.now = e.now.Add(6 * time.Hour)
	mustOK(t, e.svc.Ensure(context.Background()))
	position, err := e.svc.Position(e.users[0], first.PositionID)
	mustOK(t, err)
	if position.MarketValue.String() != "10680.43" || position.DailyPnL.String() != "0.43" || position.Cost.String() != "10000" || invalidated[e.users[0].ID] != 2 || invalidated[e.users[1].ID] != 2 {
		t.Fatal("revision not applied exactly once", position, invalidated)
	}
	market.failure = wealthdata.ErrUnavailable
	e.now = e.now.Add(4 * time.Hour)
	mustOK(t, e.svc.Ensure(context.Background()))
	position, err = e.svc.Position(e.users[0], first.PositionID)
	mustOK(t, err)
	if position.MarketValue.String() != "10680.43" || position.SourceStatus != "failed" || e.bank(t, 0) != "5000.00" {
		t.Fatal("failure destroyed previous valid value", position)
	}
	for _, code := range []string{"FBAE68020A", "FBAE68020", "Z7007923000092"} {
		in := wealthOpening("10", "1", "2026-10-14")
		in.Code = code
		if _, err = e.svc.CreateOpening(context.Background(), e.users[0], e.wealths[0], in); !errors.Is(err, ErrWealthCode) {
			t.Fatal("unregistered share accepted", code, err)
		}
	}
}

func TestWealthCmbcwmGenericSubscriptionAndActualRedemptionWithoutWeeklyPolicy(t *testing.T) {
	e := newWealthEnv(t)
	product := addCmbcwmProduct(t, e)
	in := orderInput("subscribe", "2026-10-14")
	in.Code, in.Amount, in.TradeDate, in.ApplicationTime, in.CashAccountID = product.Code, "1068", "2026-10-14", "09:00:00", e.banks[0]
	receipt, err := e.svc.Subscribe(context.Background(), e.users[0], e.wealths[0], in)
	mustOK(t, err)
	_, err = e.svc.SavePublicNAVs(product.ID, []wealthdata.NAV{{Date: fday("2026-10-14"), Value: dec("1.068"), FetchedAt: e.now, SourceCode: product.Code}})
	mustOK(t, err)
	confirm := orderInput("confirm_buy", "2026-10-15")
	confirm.Code, confirm.SourceEventID, confirm.Shares = product.Code, receipt.EventID, "1000"
	_, err = e.svc.Operate(context.Background(), e.users[0], receipt.PositionID, confirm)
	mustOK(t, err)
	weekly := orderInput("request_redeem", "2026-10-16")
	weekly.Code, weekly.Amount, weekly.TradeDate, weekly.PreviousTradeDate, weekly.NAVDate = product.Code, "100", "2026-10-16", "2026-10-15", "2026-10-14"
	if _, err = e.svc.PreviewOrder(e.users[0], receipt.PositionID, weekly); !errors.Is(err, ErrWealthInvalid) {
		t.Fatal("Suyin allocation preview applied to Minsheng", err)
	}
	if _, err = e.svc.Operate(context.Background(), e.users[0], receipt.PositionID, weekly); !errors.Is(err, ErrWealthInvalid) {
		t.Fatal("Suyin allocation write applied to Minsheng", err)
	}
	actual := WealthInput{RequestKey: uuid.NewString(), Kind: "redeem", Code: product.Code, Shares: "100", Amount: "106.8", Fee: "0", EffectiveDate: "2026-10-16"}
	_, err = e.svc.Operate(context.Background(), e.users[0], receipt.PositionID, actual)
	mustOK(t, err)
	position, err := e.svc.Position(e.users[0], receipt.PositionID)
	mustOK(t, err)
	if position.Shares.String() != "900" || position.PendingFunds.String() != "106.8" || e.bank(t, 0) != "3932.00" {
		t.Fatal(position)
	}
}

func TestWealthMySQLCmbcwmCatalogMigrationAndRollbackGuard(t *testing.T) {
	for _, retainHistory := range []bool{false, true} {
		t.Run(map[bool]string{false: "public-only-up-down-up", true: "personal-reference-preserved"}[retainHistory], func(t *testing.T) {
			e := newWealthMySQLEnv(t)
			up, err := os.ReadFile(filepath.Join("..", "..", "migrations", "000020_cmbcwm_wealth_product.up.sql"))
			mustOK(t, err)
			down, err := os.ReadFile(filepath.Join("..", "..", "migrations", "000020_cmbcwm_wealth_product.down.sql"))
			mustOK(t, err)
			mustOK(t, e.db.Exec(string(up)).Error)
			product, err := repository.NewWealthRepo(e.db).ProductForCode("FBAE68020E")
			mustOK(t, err)
			if product == nil || product.AdmissionStatus != "enabled" || !strings.HasPrefix(product.EvidenceVersion, "user-approved-") {
				t.Fatal(product)
			}
			var count int64
			mustOK(t, e.db.Model(&model.WealthPosition{}).Count(&count).Error)
			if count != 0 {
				t.Fatal("migration seeded personal holdings")
			}
			if retainHistory {
				cmbcwmOpening(t, e, 0, "100")
			}
			_, err = e.svc.SavePublicNAVs(product.ID, []wealthdata.NAV{{Date: fday("2026-10-15"), Value: dec("1.068033"), FetchedAt: e.now, SourceCode: product.Code}})
			mustOK(t, err)
			_, err = e.svc.repo.ClaimCollection(product.ID, e.now, "fixture", wealthSlots(e.now, nil), true)
			mustOK(t, err)
			err = e.db.Exec(string(down)).Error
			if retainHistory {
				if err == nil {
					t.Fatal("down erased personal product history")
				}
				mustOK(t, e.db.Model(&model.WealthNAV{}).Where("product_id = ?", product.ID).Count(&count).Error)
				if count != 1 {
					t.Fatal("guard ran after destructive statements")
				}
				return
			}
			mustOK(t, err)
			removed, err := repository.NewWealthRepo(e.db).ProductForCode(product.Code)
			mustOK(t, err)
			if removed != nil {
				t.Fatal("down retained public product")
			}
			mustOK(t, e.db.Exec(string(up)).Error)
			suyinProduct, err := repository.NewWealthRepo(e.db).ProductForCode("J07241")
			mustOK(t, err)
			if suyinProduct == nil || suyinProduct.AdmissionStatus != "enabled" {
				t.Fatal("Minsheng migration changed Suyin")
			}
		})
	}
}

func TestWealthAdmissionEvidenceCannotCrossShareClasses(t *testing.T) {
	e := newWealthEnv(t)
	addCmbcwmProduct(t, e)
	evidence := qualificationFixture()
	raw, err := json.Marshal(evidence)
	mustOK(t, err)
	if err = e.svc.SetAdmission("FBAE68020E", "enabled", raw); !errors.Is(err, ErrWealthCode) {
		t.Fatal("Suyin evidence admitted Minsheng", err)
	}
	evidence["product"] = "FBAE68020E"
	raw, err = json.Marshal(evidence)
	mustOK(t, err)
	mustOK(t, e.svc.SetAdmission("FBAE68020E", "enabled", raw))
	if err = e.svc.SetAdmission("J07241", "enabled", raw); !errors.Is(err, ErrWealthCode) {
		t.Fatal("Minsheng evidence admitted Suyin", err)
	}
}

func TestWealthCmbcwmLiveHTTPValuation(t *testing.T) {
	if os.Getenv("CMBCWM_LIVE_TEST") != "1" {
		t.Skip("set CMBCWM_LIVE_TEST=1 for anonymous source-to-isolated-ledger verification")
	}
	e := newWealthEnv(t)
	product := addCmbcwmProduct(t, e)
	e.now = time.Now()
	receipt := cmbcwmOpening(t, e, 0, "10000")
	e.svc.market = wealthdata.NewRouter(map[string]wealthdata.Source{"cmbcwm": cmbcwm.New(), "suyin": suyin.New("", "")})
	ctx, cancel := context.WithTimeout(context.Background(), 70*time.Second)
	defer cancel()
	mustOK(t, e.svc.Ensure(ctx))
	position, err := e.svc.Position(e.users[0], receipt.PositionID)
	mustOK(t, err)
	navs, err := e.svc.repo.NAVs(product.ID)
	mustOK(t, err)
	if len(navs) == 0 || position.SourceStatus != "updated" || position.UnitNAV == nil || position.MarketValue == nil || position.MarketValue.String() != position.UnitNAV.Mul(dec("10000")).Round(2).String() || e.bank(t, 0) != "5000.00" {
		t.Fatal("real public source did not revalue isolated holdings", position)
	}
	t.Logf("anonymous official source -> isolated ledger: rows=%d code=%s date=%s NAV=%s value=%s holding_pnl=%s daily_pnl=%s", len(navs), position.Code, position.NAVDate, position.UnitNAV, position.MarketValue, position.HoldingPnL, position.DailyPnL)
}

package service

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"jizhang/internal/model"
	"jizhang/internal/repository"
	"jizhang/internal/suyin"
)

type wealthSelectiveMarket struct{ now time.Time }

func (m wealthSelectiveMarket) Configured() bool { return true }
func (m wealthSelectiveMarket) NAVs(ctx context.Context, code, window string) ([]suyin.NAV, error) {
	if code == "J07241" {
		return nil, suyin.ErrUnavailable
	}
	return []suyin.NAV{{Date: fday("2026-10-15"), Value: dec("1.1"), FetchedAt: m.now, SourceCode: "fixture-second"}}, nil
}
func TestWealthProductFailureDoesNotBlockOtherTrackedProducts(t *testing.T) {
	e := newWealthEnv(t)
	e.opening(t, 0, "100", "1", "2026-10-09")
	e.quote(t, "2026-10-10", "1.02")
	product := model.WealthProduct{Issuer: "fixture", RegistrationCode: "fixture-second", Code: "FIXTURE2", Name: "隔离测试产品", Currency: "CNY", ValuationBasis: "cny_wealth_unit_nav", InceptionDate: fday("2025-08-29"), SourceAdapter: "fixture", SourceCode: "fixture-second", AdmissionStatus: "enabled", EvidenceVersion: "isolated-test"}
	mustOK(t, e.db.Create(&product).Error)
	p := model.WealthPosition{OwnerID: e.users[0].ID, AccountID: e.wealths[0], ProductID: product.ID, ValuationBasis: "cny_wealth_unit_nav", BasisConfirmedBy: e.users[0].ID, BasisConfirmedAt: e.now}
	mustOK(t, e.db.Create(&p).Error)
	mustOK(t, e.db.Create(&model.WealthEvent{OwnerID: p.OwnerID, AccountID: p.AccountID, PositionID: p.ID, Kind: "opening", Shares: dec("10"), Amount: dec("10"), EffectiveDate: fday("2026-10-09"), RightsPositionID: p.ID, CreatedBy: p.OwnerID}).Error)
	e.svc.market = wealthSelectiveMarket{e.now}
	mustOK(t, e.svc.Ensure(context.Background()))
	first, err := e.svc.repo.Product(1, false)
	mustOK(t, err)
	second, err := e.svc.repo.Product(product.ID, false)
	mustOK(t, err)
	if first.SyncStatus != "failed" || second.SyncStatus != "updated" || second.LastSuccess == nil {
		t.Fatal(first, second)
	}
	account, err := e.svc.Account(e.users[0], e.wealths[0])
	mustOK(t, err)
	if account.TotalValue == nil || account.TotalValue.String() != "113" {
		t.Fatal("failure discarded old valid NAV or isolated product", account)
	}
}

func TestWealthShanghaiSlotsAndMergedStartup(t *testing.T) {
	for _, test := range []struct {
		now  string
		want string
	}{{"2026-10-16T23:00:00Z", "2026-10-16T14:00:00Z"}, {"2026-10-17T00:00:00Z", "2026-10-17T00:00:00Z"}, {"2026-10-17T15:59:00Z", "2026-10-17T14:00:00Z"}} {
		now, _ := time.Parse(time.RFC3339, test.now)
		rows := wealthSlots(now, nil)
		if len(rows) != 1 || rows[0].UTC().Format(time.RFC3339) != test.want {
			t.Fatal(rows)
		}
	}
	last, _ := time.Parse(time.RFC3339, "2026-10-16T14:00:00Z")
	now, _ := time.Parse(time.RFC3339, "2026-10-19T00:00:00Z")
	if rows := wealthSlots(now, &last); len(rows) != 9 {
		t.Fatal(rows)
	}
	e := newWealthEnv(t)
	e.opening(t, 0, "100", "1", "2026-10-09")
	e.now = time.Date(2026, 10, 16, 8, 0, 0, 0, suyin.Shanghai)
	e.market.quotes = []suyin.NAV{{Date: fday("2026-10-15"), Value: dec("1.02"), FetchedAt: e.now, SourceCode: "Y0408Ux"}}
	mustOK(t, e.svc.Ensure(context.Background()))
	var slots []model.WealthSyncSlot
	mustOK(t, e.db.Order("slot").Find(&slots).Error)
	if len(slots) != 1 || slots[0].Status != "updated" {
		t.Fatal(slots)
	}
	e.now = e.now.Add(14 * time.Hour)
	mustOK(t, e.svc.Ensure(context.Background()))
	mustOK(t, e.db.Order("slot").Find(&slots).Error)
	if len(slots) != 4 {
		t.Fatal(slots)
	}
	for i := 1; i < 4; i++ {
		if slots[i].Status != "no_new_nav" || (i < 3 && slots[i].MergedInto == nil) {
			t.Fatal(slots)
		}
	}
	restart := NewWealthService(e.db, e.txn, e.market)
	restart.Now = e.svc.Now
	mustOK(t, restart.Ensure(context.Background()))
	var n int64
	mustOK(t, e.db.Model(&model.WealthNAV{}).Count(&n).Error)
	if n != 1 {
		t.Fatal(n)
	}
}

type wealthBlockingMarket struct {
	started chan struct{}
	release chan struct{}
	rows    []suyin.NAV
	calls   atomic.Int64
}

func (m *wealthBlockingMarket) Configured() bool { return true }
func (m *wealthBlockingMarket) NAVs(ctx context.Context, code, window string) ([]suyin.NAV, error) {
	if m.calls.Add(1) == 1 {
		close(m.started)
		select {
		case <-m.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return m.rows, nil
}

func TestWealthRefreshCoalescesOwnersAndScheduledWorkerAndReadsStayLocal(t *testing.T) {
	e := newWealthEnv(t)
	e.opening(t, 0, "100", "1", "2026-10-09")
	e.opening(t, 1, "200", "1", "2026-10-09")
	market := &wealthBlockingMarket{started: make(chan struct{}), release: make(chan struct{}), rows: []suyin.NAV{{Date: fday("2026-10-15"), Value: dec("1.02"), FetchedAt: e.now, SourceCode: "Y0408Ux"}}}
	e.svc.market = market
	done := make(chan error, 1)
	go func() { _, err := e.svc.RefreshProduct(context.Background(), e.users[0], 1); done <- err }()
	<-market.started
	other := NewWealthService(e.db, e.txn, market)
	other.Now = e.svc.Now
	refreshing, err := other.RefreshProduct(context.Background(), e.users[1], 1)
	mustOK(t, err)
	if refreshing.SyncStatus != "refreshing" {
		t.Fatal(refreshing)
	}
	mustOK(t, other.Ensure(context.Background()))
	_, err = e.svc.Account(e.users[0], e.wealths[0])
	mustOK(t, err)
	_, err = e.svc.EffectiveNAVs(e.users[1], 1)
	mustOK(t, err)
	if market.calls.Load() != 1 {
		t.Fatal("shared reads or schedule called bank again", market.calls.Load())
	}
	close(market.release)
	mustOK(t, <-done)
	_, err = e.svc.RefreshProduct(context.Background(), e.users[0], 1)
	mustOK(t, err)
	if market.calls.Load() != 1 {
		t.Fatal("cooldown ignored")
	}
	e.now = e.now.Add(61 * time.Second)
	state, err := e.svc.RefreshProduct(context.Background(), e.users[1], 1)
	mustOK(t, err)
	if state.SyncStatus != "no_new_nav" || state.LastSuccess == nil || market.calls.Load() != 2 {
		t.Fatal(state, market.calls.Load())
	}
}

func TestWealthLeaseExpiryCooldownAndFailurePreservesNAV(t *testing.T) {
	e := newWealthEnv(t)
	r := e.opening(t, 0, "100", "1", "2026-10-09")
	e.quote(t, "2026-10-15", "1.02")
	now := e.now
	slots := wealthSlots(now, nil)
	claimed, err := e.svc.repo.ClaimCollection(1, now, "worker-one", slots, true)
	mustOK(t, err)
	if !claimed {
		t.Fatal("initial claim")
	}
	claimed, err = e.svc.repo.ClaimCollection(1, now.Add(30*time.Second), "worker-two", slots, true)
	mustOK(t, err)
	if claimed {
		t.Fatal("active lease or cooldown ignored")
	}
	e.now = now.Add(91 * time.Second)
	claimed, err = e.svc.repo.ClaimCollection(1, e.now, "worker-two", nil, true)
	mustOK(t, err)
	if !claimed {
		t.Fatal("expired lease unrecoverable")
	}
	_, err = e.svc.SavePublicNAVs(1, []suyin.NAV{{Date: fday("2026-10-16"), Value: dec("2"), FetchedAt: e.now, SourceCode: "Y0408Ux"}}, "worker-one")
	if !errors.Is(err, repository.ErrWealthLease) {
		t.Fatal("stale worker committed", err)
	}
	mustOK(t, e.svc.repo.FinishCollection(1, "worker-two", e.now, false, false, "受控故障"))
	v, err := e.svc.Position(e.users[0], r.PositionID)
	mustOK(t, err)
	if v.UnitNAV.String() != "1.02" || v.SourceStatus != "failed" {
		t.Fatal(v)
	}
	e.now = e.now.Add(61 * time.Second)
	e.market.failure = suyin.ErrUnavailable
	p, err := e.svc.RefreshProduct(context.Background(), e.users[0], 1)
	mustOK(t, err)
	if p.SyncStatus != "failed" || p.LastSuccess != nil {
		t.Fatal(p)
	}
}

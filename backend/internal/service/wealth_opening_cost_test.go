package service

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"jizhang/internal/model"
	"jizhang/internal/repository"
)

func openingTotal(shares, total string) WealthInput {
	return WealthInput{RequestKey: uuid.NewString(), Code: "J07241", Shares: shares, TotalCost: &total, EffectiveDate: "2026-10-16"}
}
func assertOpeningActualCost(t *testing.T, e *wealthEnv) {
	t.Helper()
	in := openingTotal("1.14", "1.16")
	receipt, err := e.svc.CreateOpening(context.Background(), e.users[0], e.wealths[0], in)
	mustOK(t, err)
	replayed, err := e.svc.CreateOpening(context.Background(), e.users[0], e.wealths[0], in)
	mustOK(t, err)
	if replayed.EventID != receipt.EventID {
		t.Fatal("opening replay duplicated")
	}
	var event model.WealthEvent
	mustOK(t, e.db.First(&event, receipt.EventID).Error)
	if !event.Amount.Equal(dec("1.16")) || event.TradeNAV.String() != "1.01754386" {
		t.Fatal(event.Amount, event.TradeNAV)
	}
	view, err := e.svc.Position(e.users[0], receipt.PositionID)
	mustOK(t, err)
	if view.Cost.String() != "1.16" || view.CostPrice.String() != "1.01754386" || e.bank(t, 0) != "5000.00" {
		t.Fatal(view, e.bank(t, 0))
	}
	zero := openingTotal("1.14", "0")
	_, err = e.svc.AmendOpening(context.Background(), e.users[0], receipt.PositionID, zero)
	mustOK(t, err)
	view, err = e.svc.Position(e.users[0], receipt.PositionID)
	mustOK(t, err)
	if !view.Cost.IsZero() || !view.CostPrice.IsZero() || e.bank(t, 0) != "5000.00" {
		t.Fatal(view)
	}
	var cashRows int64
	mustOK(t, e.db.Model(&model.Transaction{}).Where("wealth_event_id IS NOT NULL").Count(&cashRows).Error)
	if cashRows != 0 {
		t.Fatal("opening created cash transaction")
	}
}
func TestWealthOpeningActualTotalCostPreservedExactly(t *testing.T) {
	assertOpeningActualCost(t, newWealthEnv(t))
}
func TestWealthMySQLOpeningActualTotalCostPreservedExactly(t *testing.T) {
	assertOpeningActualCost(t, newWealthMySQLEnv(t))
}
func TestWealthOpeningTotalRejectsAmbiguousOrInvalidInputs(t *testing.T) {
	for _, total := range []string{"", "-1", "NaN", "1.160", "10000000000"} {
		e := newWealthEnv(t)
		_, err := e.svc.CreateOpening(context.Background(), e.users[0], e.wealths[0], openingTotal("1.14", total))
		if !errors.Is(err, ErrWealthInvalid) {
			t.Fatal(total, err)
		}
	}
	e := newWealthEnv(t)
	in := openingTotal("1.14", "1.16")
	in.CostPrice = "1.02142"
	if _, err := e.svc.CreateOpening(context.Background(), e.users[0], e.wealths[0], in); !errors.Is(err, ErrWealthInvalid) {
		t.Fatal(err)
	}
	in = openingTotal("0", "1.16")
	if _, err := e.svc.CreateOpening(context.Background(), e.users[0], e.wealths[0], in); !errors.Is(err, ErrWealthInvalid) {
		t.Fatal(err)
	}
	in = openingTotal("0.00000001", "9999999999.99")
	if _, err := e.svc.CreateOpening(context.Background(), e.users[0], e.wealths[0], in); !errors.Is(err, ErrWealthInvalid) {
		t.Fatal(err)
	}
}
func TestWealthOpeningTotalDoesNotUseReferenceNAVAndKeepsLegacyAPI(t *testing.T) {
	e := newWealthEnv(t)
	e.quote(t, "2026-10-16", "1.02142")
	r, err := e.svc.CreateOpening(context.Background(), e.users[0], e.wealths[0], openingTotal("1.14", "1.16"))
	mustOK(t, err)
	v, err := e.svc.Position(e.users[0], r.PositionID)
	mustOK(t, err)
	if v.UnitNAV == nil || v.UnitNAV.String() != "1.02142" || v.Cost.String() != "1.16" {
		t.Fatal(v)
	}
	legacy := e.opening(t, 1, "1.14", "1.02142", "2026-10-16")
	var row model.WealthEvent
	mustOK(t, e.db.First(&row, legacy.EventID).Error)
	if row.Amount.String() != "1.1644188" {
		t.Fatal("legacy opening rewritten", row.Amount)
	}
}

func TestWealthOpeningTotalOwnerReadonlyRoleAndClosedPeriod(t *testing.T) {
	e := newWealthEnv(t)
	ctx := context.Background()
	if _, err := e.svc.CreateOpening(ctx, e.users[0], e.wealths[1], openingTotal("1.14", "1.16")); !errors.Is(err, ErrTxnForbidden) {
		t.Fatal(err)
	}
	_, err := e.svc.CreateOpening(ctx, e.users[2], e.wealths[2], openingTotal("1.14", "1.16"))
	mustOK(t, err)
	initial, err := e.svc.CreateOpening(ctx, e.users[0], e.wealths[0], openingTotal("1.14", "1.16"))
	mustOK(t, err)
	e.quote(t, "2026-10-16", "1.02142")
	e.now = fday("2026-11-02")
	closing := NewClosingService(ClosingDeps{Closings: repository.NewClosingRepo(e.db), Audits: repository.NewAuditRepo(e.db), Periods: e.txn.deps.Periods, Stats: repository.NewStatsRepo(e.db)})
	closing.now = e.svc.Now
	_, err = closing.Close(e.users[0], "2026-10")
	mustOK(t, err)
	_, err = e.svc.AmendOpening(ctx, e.users[0], initial.PositionID, openingTotal("1.14", "2"))
	if !errors.Is(err, ErrTxnClosedPeriod) {
		t.Fatal(err)
	}
}

func TestWealthOpeningNAVSnapshotIsIndependentOfCostAndQuotes(t *testing.T) {
	e := newWealthEnv(t)
	e.quote(t, "2026-10-16", "1.02142")
	in := openingTotal("1.14", "1.16")
	nav := "1.021420"
	in.OpeningNAV = &nav
	r, err := e.svc.CreateOpening(context.Background(), e.users[0], e.wealths[0], in)
	mustOK(t, err)
	v, err := e.svc.Position(e.users[0], r.PositionID)
	mustOK(t, err)
	if v.OpeningNAV == nil || v.OpeningNAV.String() != "1.02142" || v.OpeningNAVDate != "2026-10-16" || v.Cost.String() != "1.16" {
		t.Fatal(v)
	}
	rows, err := e.svc.Events(e.users[0], r.PositionID)
	mustOK(t, err)
	if orderData(rows[0]).NAVSource != "automatic" || rows[0].TradeNAV.String() != "1.01754386" {
		t.Fatal(rows)
	}
	changed := openingTotal("1.14", "1.16")
	other := "1.03"
	changed.OpeningNAV = &other
	_, err = e.svc.AmendOpening(context.Background(), e.users[0], r.PositionID, changed)
	mustOK(t, err)
	v, err = e.svc.Position(e.users[0], r.PositionID)
	mustOK(t, err)
	if v.OpeningNAV.String() != "1.03" || v.UnitNAV.String() != "1.02142" || v.Cost.String() != "1.16" || e.bank(t, 0) != "5000.00" {
		t.Fatal(v)
	}
	for _, invalid := range []string{"0", "-1", "NaN", "1.123456789"} {
		bad := openingTotal("1.14", "1.16")
		bad.OpeningNAV = &invalid
		if _, err = e.svc.AmendOpening(context.Background(), e.users[0], r.PositionID, bad); !errors.Is(err, ErrWealthInvalid) {
			t.Fatal(invalid, err)
		}
	}
}

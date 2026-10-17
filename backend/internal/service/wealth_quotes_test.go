package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"jizhang/internal/model"
	"jizhang/internal/suyin"
)

func TestWealthPersonalNAVSharedAcrossOwnAccountsAndAutomaticTakeover(t *testing.T) {
	e := newWealthEnv(t)
	ctx := context.Background()
	a := e.opening(t, 0, "100", "1", "2026-10-09")
	b := e.opening(t, 1, "200", "1", "2026-10-09")
	owner := e.users[0].ID
	other := model.Account{Name: "第二理财", OwnerID: &owner, Type: model.AccountAsset, AssetClass: "wealth", Status: model.StatusActive}
	mustOK(t, e.db.Create(&other).Error)
	ownSecond, err := e.svc.CreateOpening(ctx, e.users[0], other.ID, wealthOpening("300", "1", "2026-10-09"))
	mustOK(t, err)
	changedOwners := []int64{}
	e.svc.OnWrite = func(owner int64) { changedOwners = append(changedOwners, owner) }
	in := WealthManualInput{RequestKey: uuid.NewString(), NAVDate: "2026-10-10", UnitNAV: "1.02", SourceNote: "银行产品持仓页"}
	receipt, err := e.svc.WriteManualNAV(e.users[0], 1, in)
	mustOK(t, err)
	again, err := e.svc.WriteManualNAV(e.users[0], 1, in)
	mustOK(t, err)
	if again != receipt {
		t.Fatal(receipt, again)
	}
	if len(changedOwners) != 1 || changedOwners[0] != e.users[0].ID {
		t.Fatal("personal cache scope", changedOwners)
	}
	for _, position := range []int64{a.PositionID, ownSecond.PositionID} {
		v, err := e.svc.Position(e.users[0], position)
		mustOK(t, err)
		if v.UnitNAV == nil || v.UnitNAV.String() != "1.02" || v.Quote.Source != "manual" {
			t.Fatal(v)
		}
	}
	v, err := e.svc.Position(e.users[1], b.PositionID)
	mustOK(t, err)
	if v.MarketValue != nil {
		t.Fatal("another owner overlay leaked", v)
	}
	before, err := e.svc.Valuations(e.users[0], a.PositionID, "", "")
	mustOK(t, err)
	if len(before) != 1 {
		t.Fatal(before)
	}
	// Same numeric value AND same local revision must still change identity.
	e.quote(t, "2026-10-10", "1.02")
	after, err := e.svc.Valuations(e.users[0], a.PositionID, "", "")
	mustOK(t, err)
	if len(after) != 1 || after[0].QuoteRevision != 1 || before[0].QuoteRevision != 1 || after[0].QuoteSource != "automatic" || after[0].SeriesVersion <= before[0].SeriesVersion {
		t.Fatal(before, after)
	}
	v, err = e.svc.Position(e.users[1], b.PositionID)
	mustOK(t, err)
	if v.MarketValue == nil || v.MarketValue.String() != "204" {
		t.Fatal(v)
	}
	in.RequestKey = uuid.NewString()
	in.ExpectedRevision = receipt.Revision
	in.UnitNAV = "1.03"
	if _, err = e.svc.WriteManualNAV(e.users[0], 1, in); !errors.Is(err, ErrWealthAutomatic) {
		t.Fatal(err)
	}
	in.Withdraw = true
	if _, err = e.svc.WriteManualNAV(e.users[0], 1, in); !errors.Is(err, ErrWealthAutomatic) {
		t.Fatal(err)
	}
	manuals, err := e.svc.ManualNAVs(e.users[0], 1)
	mustOK(t, err)
	if len(manuals) != 1 || !manuals[0].AutomaticReplaced || manuals[0].Effective {
		t.Fatal(manuals)
	}
	audits, err := e.svc.ManualAudits(e.users[0], 1)
	mustOK(t, err)
	if len(audits) != 2 || audits[0].Action != "automatic_takeover" {
		t.Fatal(audits)
	}
	_, err = e.svc.SavePublicNAVs(1, []suyin.NAV{})
	if err == nil {
		t.Fatal("empty accepted")
	}
	v, err = e.svc.Position(e.users[0], a.PositionID)
	mustOK(t, err)
	if v.Quote.Source != "automatic" {
		t.Fatal("failure revived manual", v)
	}
}

func TestWealthManualInsertWithdrawRebuildsSuccessorAndMissingPrices(t *testing.T) {
	e := newWealthEnv(t)
	r := e.opening(t, 0, "100", "1", "2026-10-09")
	e.quote(t, "2026-10-09", "1")
	e.quote(t, "2026-10-12", "1.2")
	in := WealthManualInput{RequestKey: uuid.NewString(), NAVDate: "2026-10-10", UnitNAV: "1.1", SourceNote: "银行历史净值"}
	receipt, err := e.svc.WriteManualNAV(e.users[0], 1, in)
	mustOK(t, err)
	rows, err := e.svc.Valuations(e.users[0], r.PositionID, "", "")
	mustOK(t, err)
	if len(rows) != 3 || rows[0].PreviousNAVDate == nil || rows[0].PreviousNAVDate.Format("2006-01-02") != "2026-10-10" || rows[0].DailyPnL == nil || rows[0].DailyPnL.String() != "10" {
		t.Fatal(rows)
	}
	in.RequestKey = uuid.NewString()
	in.ExpectedRevision = receipt.Revision
	in.Withdraw = true
	in.UnitNAV = ""
	in.SourceNote = "录入日期错误，撤回"
	_, err = e.svc.WriteManualNAV(e.users[0], 1, in)
	mustOK(t, err)
	rows, err = e.svc.Valuations(e.users[0], r.PositionID, "", "")
	mustOK(t, err)
	if len(rows) != 2 || rows[0].PreviousNAVDate.Format("2006-01-02") != "2026-10-09" || rows[0].DailyPnL.String() != "20" {
		t.Fatal("stale middle snapshot or interval", rows)
	}
	// Today's price is never used for earlier missing history.
	e2 := newWealthEnv(t)
	r2 := e2.opening(t, 0, "10", "1", "2026-10-09")
	today := WealthManualInput{RequestKey: uuid.NewString(), NAVDate: "2026-10-16", UnitNAV: "1.2", SourceNote: "银行当日披露"}
	rec, err := e2.svc.WriteManualNAV(e2.users[0], 1, today)
	mustOK(t, err)
	vals, err := e2.svc.ValuesForDates(e2.db, []int64{e2.wealths[0]}, []time.Time{fday("2026-10-15"), fday("2026-10-16")})
	mustOK(t, err)
	if vals["2026-10-15"][e2.wealths[0]].Complete || !vals["2026-10-16"][e2.wealths[0]].Complete {
		t.Fatal(vals)
	}
	today.RequestKey = uuid.NewString()
	today.ExpectedRevision = rec.Revision
	today.Withdraw = true
	today.UnitNAV = ""
	today.SourceNote = "撤回核对"
	_, err = e2.svc.WriteManualNAV(e2.users[0], 1, today)
	mustOK(t, err)
	v, err := e2.svc.Position(e2.users[0], r2.PositionID)
	mustOK(t, err)
	if v.MarketValue != nil {
		t.Fatal(v)
	}
	rows, err = e2.svc.Valuations(e2.users[0], r2.PositionID, "", "")
	mustOK(t, err)
	if len(rows) != 0 {
		t.Fatal(rows)
	}
}

func TestWealthManualAllRolesVersionAndValidation(t *testing.T) {
	e := newWealthEnv(t)
	if _, err := e.svc.WriteManualNAV(e.users[0], 1, WealthManualInput{RequestKey: uuid.NewString(), NAVDate: "2026-10-10", UnitNAV: "1", SourceNote: "未登记产品"}); !errors.Is(err, ErrWealthUnregistered) {
		t.Fatal(err)
	}
	for who, user := range e.users {
		e.opening(t, who, "100", "1", "2026-10-09")
		in := WealthManualInput{RequestKey: uuid.NewString(), NAVDate: "2026-10-10", UnitNAV: "1.02", SourceNote: "本人银行截图核对"}
		receipt, err := e.svc.WriteManualNAV(user, 1, in)
		mustOK(t, err)
		in.RequestKey = uuid.NewString()
		in.UnitNAV = "1.03"
		in.ExpectedRevision = receipt.Revision
		next, err := e.svc.WriteManualNAV(user, 1, in)
		mustOK(t, err)
		if next.Revision != 2 {
			t.Fatal(next)
		}
		in.RequestKey = uuid.NewString()
		if _, err = e.svc.WriteManualNAV(user, 1, in); !errors.Is(err, ErrWealthConflict) {
			t.Fatal(err)
		}
	}
	for _, date := range []string{"2026-10-17", "2026-02-30", "2025-08-28"} {
		if _, err := e.svc.WriteManualNAV(e.users[0], 1, WealthManualInput{RequestKey: uuid.NewString(), NAVDate: date, UnitNAV: "1", SourceNote: "核对"}); err == nil {
			t.Fatal(date)
		}
	}
	for _, price := range []string{"0", "-1", "NaN", "Infinity", "1.123456789", "99999999999"} {
		if _, err := e.svc.WriteManualNAV(e.users[0], 1, WealthManualInput{RequestKey: uuid.NewString(), NAVDate: "2026-10-11", UnitNAV: price, SourceNote: "核对"}); err == nil {
			t.Fatal(price)
		}
	}
}

func TestWealthPublicBatchRevisionsAndInvalidBatchPreservesHistory(t *testing.T) {
	e := newWealthEnv(t)
	e.opening(t, 0, "100", "1", "2026-10-09")
	e.quote(t, "2026-10-12", "1.2")
	e.quote(t, "2026-10-09", "1")
	e.quote(t, "2026-10-12", "1.2")
	e.quote(t, "2026-10-09", "1.01")
	rows, err := e.svc.repo.NAVs(1)
	mustOK(t, err)
	if len(rows) != 2 || rows[0].Revision != 2 || rows[1].Revision != 1 {
		t.Fatal(rows)
	}
	var revisions int64
	mustOK(t, e.db.Model(&model.WealthNAVRevision{}).Count(&revisions).Error)
	if revisions != 3 {
		t.Fatal(revisions)
	}
	input := []suyin.NAV{{Date: fday("2026-10-13"), Value: dec("1.3"), SourceCode: "Y0408Ux", FetchedAt: e.now}, {Date: fday("2026-10-14"), Value: dec("-1"), SourceCode: "Y0408Ux", FetchedAt: e.now}}
	if _, err = e.svc.SavePublicNAVs(1, input); err == nil {
		t.Fatal("partly invalid batch accepted")
	}
	rows, err = e.svc.repo.NAVs(1)
	mustOK(t, err)
	if len(rows) != 2 || rows[1].NAVDate.Format("2006-01-02") != "2026-10-12" {
		t.Fatal(rows)
	}
}

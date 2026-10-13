package service

import (
	"errors"
	"reflect"
	"testing"

	"github.com/google/uuid"
	"jizhang/internal/model"
	"jizhang/internal/repository"
)

func TestLiabilityPrincipalRemainderSetting(t *testing.T) {
	e := newLiabilityEnv(t)
	svc := NewAccountService(repository.NewAccountRepo(e.db))
	in := CreateAccountInput{Name: "花呗", Type: model.AccountLiability, PrincipalRemainder: "first"}
	_, err := svc.UpdateFor(adminUser(), e.card, in, model.StatusActive)
	mustOK(t, err)
	r, err := e.svcL.CreatePlan(adminUser(), InstallmentInput{AccountID: e.card, Amount: dec("998.33"), EntryDate: "2026-10-15", CategoryID: e.category, TermCount: 3, FeeInput: dec("10"), FeeMode: "total_spread", FeeAccountID: e.card, FeeCategoryID: e.category, RequestKey: uuid.NewString()})
	mustOK(t, err)
	p, err := e.svcL.Plan(adminUser(), r.PlanID)
	mustOK(t, err)
	for i, want := range []string{"332.79", "332.77", "332.77"} {
		if !p.Terms[i].Principal.Equal(dec(want)) {
			t.Fatalf("term %d: want principal %s, got %s", i+1, want, p.Terms[i].Principal)
		}
	}
	for i, want := range []string{"3.33", "3.33", "3.34"} {
		if !p.Terms[i].FeeAmount.Equal(dec(want)) {
			t.Fatalf("principal setting changed fee distribution: %v", p.Terms)
		}
	}
	// Metadata edits from old clients retain the preference. Changing the
	// preference after a plan exists must only affect subsequent plans.
	in.PrincipalRemainder = ""
	a, err := svc.UpdateFor(adminUser(), e.card, in, model.StatusActive)
	mustOK(t, err)
	if a.PrincipalRemainder != "first" {
		t.Fatal("omitted setting reset the preference", a.PrincipalRemainder)
	}
	before := e.state(t)
	in.PrincipalRemainder = "last"
	_, err = svc.UpdateFor(adminUser(), e.card, in, model.StatusActive)
	mustOK(t, err)
	after := e.state(t)
	if !reflect.DeepEqual(before.Plans, after.Plans) || !before.Summary.TotalDebt.Equal(after.Summary.TotalDebt) {
		t.Fatal("setting rewrote existing installments or debt")
	}
	in.PrincipalRemainder = "middle"
	_, err = svc.UpdateFor(adminUser(), e.card, in, model.StatusActive)
	if !errors.Is(err, ErrPrincipalRemainderInvalid) || e.state(t).Account.PrincipalRemainder != "last" {
		t.Fatal("invalid setting was not rejected atomically", err)
	}
	source := e.spend(t, "998.33", "2026-10-15")
	r, err = e.svcL.CreatePlan(adminUser(), InstallmentInput{AccountID: e.card, SourceTxnID: source.ID, TermCount: 3, FeeInput: dec("0"), RequestKey: uuid.NewString()})
	mustOK(t, err)
	p, err = e.svcL.Plan(adminUser(), r.PlanID)
	mustOK(t, err)
	for i, want := range []string{"332.77", "332.77", "332.79"} {
		if !p.Terms[i].Principal.Equal(dec(want)) {
			t.Fatalf("converted expense term %d: want %s, got %s", i+1, want, p.Terms[i].Principal)
		}
	}
}

func TestAccountPrincipalRemainderCreation(t *testing.T) {
	e := newLiabilityEnv(t)
	svc := NewAccountService(repository.NewAccountRepo(e.db))
	for _, placement := range []string{"", "first", "last"} {
		a, err := svc.CreateFor(adminUser(), CreateAccountInput{Name: "独立负债", Type: model.AccountLiability, PrincipalRemainder: placement})
		mustOK(t, err)
		want := placement
		if want == "" {
			want = "last"
		}
		stored, err := repository.NewAccountRepo(e.db).GetByID(a.ID)
		mustOK(t, err)
		if stored.PrincipalRemainder != want {
			t.Fatal("preference was not persisted", stored)
		}
	}
	for _, in := range []CreateAccountInput{
		{Name: "无效负债", Type: model.AccountLiability, PrincipalRemainder: "middle"},
		{Name: "无效资产", Type: model.AccountAsset, PrincipalRemainder: "first"},
	} {
		if _, err := svc.CreateFor(adminUser(), in); !errors.Is(err, ErrPrincipalRemainderInvalid) {
			t.Fatal("invalid preference accepted", err)
		}
	}
}

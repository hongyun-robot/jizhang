package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"jizhang/internal/model"
	"jizhang/internal/wealthdata"
)

func cashAccountInput(product int64) CreateAccountInput {
	return CreateAccountInput{Name: "直接新建余利宝", Type: model.AccountAsset, AssetClass: "wealth_cash", AssetClassProvided: true, OpeningBalance: dec("10000"), RequestKey: uuid.NewString(), WealthCash: &WealthCashSetupInput{ProductID: product, StartDate: "2026-10-14"}}
}
func cashAccountService(t *testing.T, e *wealthEnv) *AccountService {
	t.Helper()
	mustOK(t, e.db.AutoMigrate(&model.AccountCreateRequest{}))
	s := NewAccountService(e.accounts)
	s.Now = func() time.Time { return e.now }
	return s
}
func TestWealthCashNewAccountAtomicCreationReplayAndYield(t *testing.T) {
	e := newCashEnv(t)
	product := addCmbcwmProduct(t, e)
	s := cashAccountService(t, e)
	in := cashAccountInput(product.ID)
	in.ActorID = e.users[0].ID
	account, err := s.CreateFor(e.users[1], in)
	mustOK(t, err)
	if !account.IsWealthCash() || account.OwnerID == nil || *account.OwnerID != e.users[1].ID || account.OpeningBalance.String() != "10000" || account.Balance.String() != "10000" || !account.HasFinancialHistory {
		t.Fatal(account)
	}
	view, err := e.svc.CashAccount(e.users[1], account.ID)
	mustOK(t, err)
	if view.Link == nil || !view.Link.Active || view.Link.Policy != "yulibao_20250715" || len(view.Audits) != 1 || view.Audits[0].Action != "creation" {
		t.Fatal(view)
	}
	_, err = e.svc.SavePublicNAVs(product.ID, []wealthdata.NAV{{Date: fday("2026-10-14"), Value: dec("1.068"), SourceCode: product.Code, FetchedAt: e.now}, {Date: fday("2026-10-15"), Value: dec("1.068033"), SourceCode: product.Code, FetchedAt: e.now}})
	mustOK(t, err)
	mustOK(t, e.svc.ensureCashYields(context.Background()))
	replay, err := s.CreateFor(e.users[1], in)
	mustOK(t, err)
	if replay.ID != account.ID || replay.Balance.String() != "10000.31" || !replay.HasFinancialHistory {
		t.Fatal("replay duplicated or lost balance", replay)
	}
	view, err = e.svc.CashAccount(e.users[1], account.ID)
	mustOK(t, err)
	if len(view.Yields) != 1 || view.Yields[0].Amount.String() != "0.31" || view.Yields[0].TxnID == nil {
		t.Fatal(view)
	}
	for _, changed := range []CreateAccountInput{
		func() CreateAccountInput { x := in; x.OpeningBalance = dec("10001"); return x }(),
		func() CreateAccountInput {
			x := in
			x.WealthCash = &WealthCashSetupInput{ProductID: 1, StartDate: "2026-10-14"}
			return x
		}(),
		func() CreateAccountInput {
			x := in
			x.WealthCash = &WealthCashSetupInput{ProductID: product.ID, StartDate: "2026-10-13"}
			return x
		}(),
	} {
		if _, err = s.CreateFor(e.users[1], changed); !errors.Is(err, ErrAccountCreateConflict) {
			t.Fatal("changed payload reused creation key", err)
		}
	}
	var count int64
	mustOK(t, e.db.Model(&model.WealthCashLink{}).Where("account_id = ?", account.ID).Count(&count).Error)
	if count != 1 {
		t.Fatal(count)
	}
	// A request key already used for an ordinary account cannot convert it.
	plain := CreateAccountInput{Name: "旧现金", Type: model.AccountAsset, AssetClass: "cash", RequestKey: uuid.NewString()}
	old, err := s.CreateFor(e.users[1], plain)
	mustOK(t, err)
	collision := cashAccountInput(product.ID)
	collision.RequestKey = plain.RequestKey
	if _, err = s.CreateFor(e.users[1], collision); !errors.Is(err, ErrAccountCreateConflict) {
		t.Fatal(err)
	}
	stored, err := e.accounts.GetByID(old.ID)
	mustOK(t, err)
	if stored.IsWealthCash() {
		t.Fatal("ordinary request collision converted old account")
	}
}

func TestWealthCashNewAccountValidationAndPermissions(t *testing.T) {
	for _, test := range []struct {
		name   string
		modify func(*CreateAccountInput)
		want   error
	}{
		{"missing configuration", func(in *CreateAccountInput) { in.WealthCash = nil }, ErrWealthCashSetup},
		{"missing request key", func(in *CreateAccountInput) { in.RequestKey = "" }, ErrAccountCreateConflict},
		{"unknown product", func(in *CreateAccountInput) { in.WealthCash.ProductID = 999 }, ErrWealthCode},
		{"future baseline", func(in *CreateAccountInput) { in.WealthCash.StartDate = "2026-10-17" }, ErrWealthInvalid},
		{"before inception", func(in *CreateAccountInput) { in.WealthCash.StartDate = "2020-01-01" }, ErrWealthInvalid},
		{"negative balance", func(in *CreateAccountInput) { in.OpeningBalance = dec("-1") }, ErrInvalidAmount},
		{"invalid precision", func(in *CreateAccountInput) { in.OpeningBalance = dec("1.001") }, ErrInvalidAmount},
		{"policy", func(in *CreateAccountInput) { in.WealthCash.Policy = "unknown" }, ErrWealthInvalid},
		{"configuration on cash", func(in *CreateAccountInput) { in.AssetClass = "cash" }, ErrWealthInvalid},
		{"liability", func(in *CreateAccountInput) { in.Type = model.AccountLiability }, ErrInvalidAccountType},
	} {
		t.Run(test.name, func(t *testing.T) {
			e := newCashEnv(t)
			s := cashAccountService(t, e)
			in := cashAccountInput(1)
			test.modify(&in)
			if _, err := s.CreateFor(e.users[0], in); !errors.Is(err, test.want) {
				t.Fatal(err)
			}
			var count int64
			mustOK(t, e.db.Model(&model.Account{}).Count(&count).Error)
			if count != 8 {
				t.Fatal("failed create left an account", count)
			}
		})
	}
	e := newCashEnv(t)
	s := cashAccountService(t, e)
	if _, err := s.CreateFor(e.users[2], cashAccountInput(1)); !errors.Is(err, ErrTxnForbidden) {
		t.Fatal("viewer created income-bearing account", err)
	}
	mustOK(t, e.db.Model(&model.WealthProduct{}).Where("id = ?", 1).Update("admission_status", "retired").Error)
	if _, err := s.CreateFor(e.users[0], cashAccountInput(1)); !errors.Is(err, ErrWealthCode) {
		t.Fatal("retired product accepted", err)
	}
}

func TestWealthCashNewAccountRollsBackConfigurationAndReceiptFailures(t *testing.T) {
	for _, table := range []string{"wealth_cash_audits", "account_create_requests"} {
		t.Run(table, func(t *testing.T) {
			e := newCashEnv(t)
			s := cashAccountService(t, e)
			in := cashAccountInput(1)
			failure := errors.New("isolated create persistence failure")
			mustOK(t, e.db.Callback().Create().Before("gorm:create").Register("fail_new_cash", func(db *gorm.DB) {
				if db.Statement.Table == table {
					db.AddError(failure)
				}
			}))
			if _, err := s.CreateFor(e.users[0], in); !errors.Is(err, failure) {
				t.Fatal(err)
			}
			var count int64
			mustOK(t, e.db.Model(&model.Account{}).Count(&count).Error)
			if count != 8 {
				t.Fatal("partial account", count)
			}
			for _, model := range []any{&model.WealthCashLink{}, &model.WealthCashAudit{}, &model.AccountCreateRequest{}} {
				mustOK(t, e.db.Model(model).Count(&count).Error)
				if count != 0 {
					t.Fatal("partial configuration or receipt", count)
				}
			}
			mustOK(t, e.db.Callback().Create().Remove("fail_new_cash"))
			account, err := s.CreateFor(e.users[0], in)
			mustOK(t, err)
			if !account.IsWealthCash() {
				t.Fatal(account)
			}
		})
	}
}

func TestWealthCashNewAccountKeepsOldOrdinaryRequestDigests(t *testing.T) {
	e := newCashEnv(t)
	s := cashAccountService(t, e)
	owner := e.users[0].ID
	old := model.Account{Name: "legacy cash", Type: model.AccountAsset, AssetClass: "cash", OwnerID: &owner, OpeningBalance: dec("100"), Status: model.StatusActive}
	mustOK(t, e.db.Create(&old).Error)
	key := uuid.NewString()
	// Frozen pre-wealth_cash v2 digest: old callers omitted the new optional
	// configuration. Adding a field must not invalidate their retry receipts.
	mustOK(t, e.db.Create(&model.AccountCreateRequest{OwnerID: owner, RequestKey: key, Digest: "b9b7b0af8feca8e206e0c52f19dde8e30240ab549db436417ebee1a2a25f26d6", AccountID: &old.ID}).Error)
	row, err := s.CreateFor(e.users[0], CreateAccountInput{Name: old.Name, Type: model.AccountAsset, AssetClass: "cash", AssetClassProvided: true, OpeningBalance: dec("100"), RequestKey: key})
	mustOK(t, err)
	if row.ID != old.ID {
		t.Fatal("old request created another account", row.ID, old.ID)
	}
}

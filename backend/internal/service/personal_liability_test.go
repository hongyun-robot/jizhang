package service

import (
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"jizhang/internal/model"
	"jizhang/internal/repository"
	"testing"
)

func TestPersonalLiabilityAccountsAndIndependentRules(t *testing.T) {
	e := newLiabilityEnv(t)
	svc := NewAccountService(repository.NewAccountRepo(e.db))
	admin := adminUser()
	member := &model.User{ID: 3, Role: model.RoleMember}
	viewer := &model.User{ID: 2, Role: model.RoleViewer}
	day, due, offset := 20, 25, 0
	first, err := svc.CreateFor(member, CreateAccountInput{Name: "我的花呗账号一", Type: model.AccountLiability, LiabilityType: "huabei", StatementDay: &day, RepaymentDay: &due, RepaymentMonthOffset: &offset})
	mustOK(t, err)
	day, due, offset = 31, 10, 1
	second, err := svc.CreateFor(member, CreateAccountInput{Name: "我的花呗账号二", Type: model.AccountLiability, LiabilityType: "huabei", StatementDay: &day, RepaymentDay: &due, RepaymentMonthOffset: &offset})
	mustOK(t, err)
	if first.ID == second.ID || *first.OwnerID != member.ID || *second.OwnerID != member.ID {
		t.Fatal(first, second)
	}
	d1, err := e.svcL.Detail(member, first.ID)
	mustOK(t, err)
	d2, err := e.svcL.Detail(member, second.ID)
	mustOK(t, err)
	if d1.Rules[0].StatementDay != 20 || d1.Rules[0].RepaymentDay != 25 || d2.Rules[0].StatementDay != 31 || d2.Rules[0].RepaymentDay != 10 {
		t.Fatal("account dates not independent", d1.Rules, d2.Rules)
	}
	for _, other := range []*model.User{admin, viewer} {
		_, err = e.svcL.Detail(other, first.ID)
		if !errors.Is(err, ErrTxnForbidden) {
			t.Fatal("foreign account read", err)
		}
		_, err = e.svcL.Settings(other, first.ID, LiabilitySettingsInput{LiabilityType: "huabei", StatementDay: 15, RepaymentDay: 20, RepaymentMonthOffset: 0})
		if !errors.Is(err, ErrTxnForbidden) {
			t.Fatal("foreign settings changed", err)
		}
	}
	_, err = e.svcL.Settings(member, first.ID, LiabilitySettingsInput{LiabilityType: "huabei", StatementDay: 15, RepaymentDay: 20, RepaymentMonthOffset: 0})
	mustOK(t, err)
	rows, err := svc.ListFor(admin)
	mustOK(t, err)
	for _, a := range rows {
		if a.ID == first.ID || a.ID == second.ID {
			t.Fatal("foreign account in list")
		}
	}
	// Metadata profiles remain self-service for all logins; financial operations
	// still respect the existing read-only role.
	ownViewer, err := svc.CreateFor(viewer, CreateAccountInput{Name: "我的只读信用账户", Type: model.AccountLiability, LiabilityType: "huabei", StatementDay: &day, RepaymentDay: &due, RepaymentMonthOffset: &offset})
	mustOK(t, err)
	_, err = e.svcL.Repay(viewer, LiabilityRepaymentInput{AccountID: ownViewer.ID, FromAccountID: e.account, Amount: dec("10"), EntryDate: "2026-10-15", RequestKey: uuid.NewString()})
	if !errors.Is(err, ErrTxnForbidden) {
		t.Fatal(err)
	}
	asset, assetErr := svc.CreateFor(member, CreateAccountInput{Name: "我的资产", Type: model.AccountAsset})
	if assetErr != nil || asset.OwnerID == nil || *asset.OwnerID != member.ID {
		t.Fatal(err)
	}
}

func TestPrivateLiabilityCannotBeAccessedThroughMoneyOrNotifications(t *testing.T) {
	e := newLiabilityEnv(t)
	member := &model.User{ID: 3, Role: model.RoleMember}
	svc := NewAccountService(repository.NewAccountRepo(e.db))
	day, due, offset := 31, 10, 1
	own, err := svc.CreateFor(member, CreateAccountInput{Name: "私人负债", Type: model.AccountLiability, LiabilityType: "huabei", StatementDay: &day, RepaymentDay: &due, RepaymentMonthOffset: &offset})
	mustOK(t, err)
	category := model.Category{Name: "我的支出", Kind: model.KindExpense, OwnerID: &member.ID}
	mustOK(t, e.db.Create(&category).Error)
	e.category = category.ID
	source, err := e.txn.Create(member, TxnInput{Kind: model.TxnExpense, Amount: dec("500"), AccountID: own.ID, CategoryID: e.category, EntryDate: e.now})
	mustOK(t, err)
	_, err = e.txn.Create(adminUser(), TxnInput{Kind: model.TxnTransfer, Amount: dec("100"), AccountID: e.account, TransferAccountID: own.ID, EntryDate: e.now})
	if !errors.Is(err, ErrTxnForbidden) {
		t.Fatal("foreign repayment", err)
	}
	err = e.txn.Delete(adminUser(), source.ID)
	if !errors.Is(err, ErrTxnForbidden) {
		t.Fatal("admin modified foreign debt", err)
	}
	rows, total, err := e.txn.List(adminUser(), repository.TxnFilter{})
	mustOK(t, err)
	if total != 0 || len(rows) != 0 {
		t.Fatal("foreign debt leaked to records", rows)
	}
	csv, err := e.txn.ExportRows(adminUser(), repository.TxnFilter{})
	mustOK(t, err)
	if len(csv) != 0 {
		t.Fatal("foreign debt leaked to export")
	}
	e.now = vdate(t, "2026-11-07")
	_, err = e.svcL.EnsureDue(e.now)
	mustOK(t, err)
	notifications := NewNotificationService(NotifDeps{Notifs: repository.NewNotificationRepo(e.db), Accounts: repository.NewAccountRepo(e.db)})
	hidden, n, err := notifications.ForUser(adminUser()).List()
	mustOK(t, err)
	if len(hidden) != 0 || n != 0 {
		t.Fatal("private reminder leaked", hidden, n)
	}
	visible, n, err := notifications.ForUser(member).List()
	mustOK(t, err)
	if len(visible) != 1 || n != 1 {
		t.Fatal(visible, n)
	}
	_, err = notifications.ForUser(adminUser()).MarkRead(visible[0].ID)
	if !errors.Is(err, ErrNotifNotFound) {
		t.Fatal("foreign reminder marked read", err)
	}
	stats := NewStatsService(repository.NewStatsRepo(e.db), repository.NewAccountRepo(e.db), repository.NewCategoryRepo(e.db), e.txn.deps.Periods, nil)
	raw, _, err := stats.ForUser(adminUser()).Assets("2026-10-01", "2026-10-31")
	mustOK(t, err)
	var report struct {
		Points []struct {
			Accounts []struct {
				ID int64 `json:"account_id"`
			}
		}
	}
	mustOK(t, json.Unmarshal(raw, &report))
	for _, p := range report.Points {
		for _, a := range p.Accounts {
			if a.ID == own.ID {
				t.Fatal("foreign debt leaked to assets")
			}
		}
	}
}

package service

import (
	"errors"
	"github.com/google/uuid"
	"jizhang/internal/liability"
	"jizhang/internal/model"
	"jizhang/internal/repository"
	"testing"
)

func TestLiabilityTypesComeFromDatabase(t *testing.T) {
	e := newLiabilityEnv(t)
	catalog := model.LiabilityAccountType{Code: "database_pay_later", Name: "数据库添加的月付", Sort: -1}
	mustOK(t, e.db.Create(&catalog).Error)
	svc := NewAccountService(repository.NewAccountRepo(e.db))
	rows, err := svc.LiabilityTypes()
	mustOK(t, err)
	if rows[0].Code != catalog.Code || rows[0].Name != catalog.Name {
		t.Fatal("catalog did not come from database", rows)
	}
	day, due, offset := 31, 10, 1
	a, err := svc.Create(CreateAccountInput{Name: "可扩展消费信用", Type: model.AccountLiability, ActorID: 1, StatementDay: &day, RepaymentDay: &due, RepaymentMonthOffset: &offset})
	mustOK(t, err)
	if a.LiabilityType != catalog.Code {
		t.Fatal("default type hardcoded", a)
	}
	_, err = e.svcL.Settings(adminUser(), a.ID, LiabilitySettingsInput{LiabilityType: catalog.Code, StatementDay: 31, RepaymentDay: 10, RepaymentMonthOffset: 1})
	mustOK(t, err)
	_, err = e.svcL.Settings(adminUser(), a.ID, LiabilitySettingsInput{LiabilityType: "not_in_catalog", StatementDay: 31, RepaymentDay: 10, RepaymentMonthOffset: 1})
	if !errors.Is(err, liability.ErrInvalid) {
		t.Fatal("unknown type accepted", err)
	}
	mustOK(t, e.db.Model(&catalog).Update("name", "已更新的数据库名称").Error)
	rows, err = svc.LiabilityTypes()
	mustOK(t, err)
	if rows[0].Name != "已更新的数据库名称" {
		t.Fatal("stale code labels", rows)
	}
	_, err = e.txn.Create(adminUser(), TxnInput{Kind: model.TxnExpense, AccountID: a.ID, CategoryID: e.category, Amount: dec("10"), EntryDate: e.now})
	mustOK(t, err)
	e.now = vdate(t, "2026-11-01")
	_, err = e.svcL.EnsureDue(e.now)
	mustOK(t, err)
	_, err = e.svcL.Repay(adminUser(), LiabilityRepaymentInput{AccountID: a.ID, FromAccountID: e.account, Amount: dec("10"), EntryDate: "2026-11-01", RequestKey: uuid.NewString()})
	mustOK(t, err)
}

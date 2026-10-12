package service

import (
	"errors"
	"jizhang/internal/model"
	"testing"
)

func TestPersonalRecurringRepair(t *testing.T) {
	e := newRecurringEnv(t)
	old, _, _ := e.seed()
	member := user(2, model.RoleMember)
	member.Username = "repair-member"
	mustOK(t, e.userRepo.Create(member))
	account := model.Account{OwnerID: &member.ID, Name: "本人资金", Type: model.AccountAsset, Status: model.StatusActive}
	mustOK(t, e.db.Create(&account).Error)
	category := model.Category{OwnerID: &member.ID, Name: "本人分类", Kind: model.KindExpense}
	mustOK(t, e.db.Create(&category).Error)
	date := mustDate(t, "2026-10-01")
	rule := model.RecurringRule{CreatedBy: 2, AccountID: old.ID, CategoryID: category.ID, Amount: dec("50"), Freq: model.FreqMonthly, StartDate: date, NextRunDate: date, Status: model.RulePaused, BlockedReason: "原共享账户已归其他账号"}
	mustOK(t, e.ruleRepo.Create(&rule))
	rows, err := e.rules.List(member)
	mustOK(t, err)
	if len(rows) != 1 || rows[0].AccountID != 0 || rows[0].BlockedReason == "" {
		t.Fatal("missing repair view", rows)
	}
	if err = e.rules.SetStatus(user(1, model.RoleAdmin), rule.ID, model.RulePaused); !errors.Is(err, ErrRuleForbidden) {
		t.Fatal("admin bypassed private category", err)
	}
	_, err = e.rules.Update(member, rule.ID, RuleInput{Amount: dec("50"), Freq: model.FreqMonthly, AccountID: account.ID, CategoryID: category.ID, StartDate: date})
	mustOK(t, err)
	mustOK(t, e.rules.SetStatus(member, rule.ID, model.RuleEnabled))
	n, err := e.rules.EnsureDueLazy(mustDate(t, "2026-10-15"))
	mustOK(t, err)
	if n != 1 {
		t.Fatal("repair did not run", n)
	}
	notices, err := e.notifs.ForOwner(2).List("")
	mustOK(t, err)
	if len(notices) != 1 || notices[0].OwnerID == nil || *notices[0].OwnerID != 2 {
		t.Fatal("patch reminder scope", notices)
	}
}

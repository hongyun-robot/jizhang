package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"jizhang/internal/model"
	"jizhang/internal/repository"
)

func cashYieldTransaction(t *testing.T, e *wealthEnv, account int64) model.Transaction {
	t.Helper()
	var row model.Transaction
	mustOK(t, e.db.Where("account_id = ? AND purpose = ?", account, model.PurposeWealthCashYield).Order("entry_date, id").Last(&row).Error)
	return row
}

func cashYieldCategoryPath(t *testing.T, e *wealthEnv, row model.Transaction, kind model.CategoryKind) (model.Category, model.Category) {
	t.Helper()
	if row.CategoryID == nil {
		t.Fatal("automatic yield has no category", row.ID)
	}
	var leaf, root model.Category
	mustOK(t, e.db.First(&leaf, *row.CategoryID).Error)
	if leaf.ParentID == nil {
		t.Fatal("yield category is not a second-level category", leaf)
	}
	mustOK(t, e.db.First(&root, *leaf.ParentID).Error)
	if leaf.OwnerID == nil || root.OwnerID == nil || *leaf.OwnerID != row.CreatedBy || *root.OwnerID != row.CreatedBy || leaf.Kind != kind || root.Kind != kind {
		t.Fatal("yield category crossed owner or transaction kind", leaf, root)
	}
	return root, leaf
}

func TestWealthCashYieldCategoriesAndRenames(t *testing.T) {
	e := newCashEnv(t)
	mustOK(t, e.db.Model(&model.Account{}).Where("id = ?", e.banks[0]).Update("name", "余利宝").Error)
	enableCash(t, e, "2026-10-12", "unit_nav")
	e.quote(t, "2026-10-12", "1")
	e.quote(t, "2026-10-13", "1.01")
	mustOK(t, e.svc.ensureCashYields(context.Background()))
	row := cashYieldTransaction(t, e, e.banks[0])
	root, leaf := cashYieldCategoryPath(t, e, row, model.KindIncome)
	if root.Name != "理财" || leaf.Name != "收益-余利宝" || row.Amount.StringFixed(2) != "50.00" || e.bank(t, 0) != "5050.00" {
		t.Fatal(root, leaf, row, e.bank(t, 0))
	}
	// Existing personal taxonomy edits remain authoritative for future yields.
	mustOK(t, e.db.Model(&leaf).Update("name", "我改名后的收益").Error)
	mustOK(t, e.db.Model(&root).Update("name", "我的投资").Error)
	mustOK(t, e.db.Model(&model.Account{}).Where("id = ?", e.banks[0]).Update("name", "账户新名称").Error)
	e.quote(t, "2026-10-14", "1.02")
	mustOK(t, e.svc.ensureCashYields(context.Background()))
	next := cashYieldTransaction(t, e, e.banks[0])
	if next.CategoryID == nil || *next.CategoryID != leaf.ID {
		t.Fatal("renamed taxonomy was replaced", next)
	}
	var count int64
	mustOK(t, e.db.Model(&model.Category{}).Where("owner_id = ?", e.users[0].ID).Count(&count).Error)
	if count != 2 {
		t.Fatal("unchanged or renamed category duplicated", count)
	}
}

func TestWealthCashYieldCategoriesSeparateOwnersAndSigns(t *testing.T) {
	e := newCashEnv(t)
	ctx := context.Background()
	for _, who := range []int{0, 1} {
		mustOK(t, e.db.Model(&model.Account{}).Where("id = ?", e.banks[who]).Update("name", "余利宝").Error)
		_, err := e.svc.ConfigureCash(ctx, e.users[who], e.banks[who], WealthCashInput{RequestKey: uuid.NewString(), Active: true, ProductID: 1, StartDate: "2026-10-12", Policy: "unit_nav"})
		mustOK(t, err)
	}
	e.quote(t, "2026-10-12", "1")
	e.quote(t, "2026-10-13", "1.01")
	mustOK(t, e.svc.ensureCashYields(ctx))
	a := cashYieldTransaction(t, e, e.banks[0])
	b := cashYieldTransaction(t, e, e.banks[1])
	_, ac := cashYieldCategoryPath(t, e, a, model.KindIncome)
	_, bc := cashYieldCategoryPath(t, e, b, model.KindIncome)
	if ac.ID == bc.ID {
		t.Fatal("two owners share a personal category")
	}
	view, err := e.svc.CashAccount(e.users[0], e.banks[0])
	mustOK(t, err)
	_, err = e.svc.EditCashYield(ctx, e.users[0], view.Yields[0].ID, WealthCashYieldInput{RequestKey: uuid.NewString(), ExpectedRevision: view.Yields[0].Revision, Amount: "-10"})
	mustOK(t, err)
	negative := cashYieldTransaction(t, e, e.banks[0])
	nr, nc := cashYieldCategoryPath(t, e, negative, model.KindExpense)
	if negative.Kind != model.TxnExpense || nc.ID == ac.ID || nr.Name != "理财" || nc.Name != "收益-余利宝" {
		t.Fatal("signed correction kept the wrong category kind", negative, nc)
	}
	view, err = e.svc.CashAccount(e.users[0], e.banks[0])
	mustOK(t, err)
	_, err = e.svc.EditCashYield(ctx, e.users[0], view.Yields[0].ID, WealthCashYieldInput{RequestKey: uuid.NewString(), ExpectedRevision: view.Yields[0].Revision, Amount: "0"})
	mustOK(t, err)
	var count int64
	mustOK(t, e.db.Model(&model.Transaction{}).Where("account_id = ? AND purpose = ?", e.banks[0], model.PurposeWealthCashYield).Count(&count).Error)
	if count != 0 || e.bank(t, 0) != "5000.00" {
		t.Fatal("zero yield left a cash fact", count, e.bank(t, 0))
	}
}

func TestWealthCashYieldCategoryBackfillPreservesClosedStoppedFacts(t *testing.T) {
	e := newCashEnv(t)
	ctx := context.Background()
	mustOK(t, e.db.Model(&model.Account{}).Where("id = ?", e.banks[0]).Update("name", "余利宝").Error)
	enableCash(t, e, "2026-10-12", "unit_nav")
	e.quote(t, "2026-10-12", "1")
	e.quote(t, "2026-10-13", "1.01")
	mustOK(t, e.svc.ensureCashYields(ctx))
	row := cashYieldTransaction(t, e, e.banks[0])
	mustOK(t, e.db.Model(&row).UpdateColumn("category_id", nil).Error)
	mustOK(t, e.db.Model(&model.WealthCashLink{}).Where("account_id = ?", row.AccountID).Update("active", false).Error)
	mustOK(t, e.db.Create(&model.PeriodClosing{PeriodKey: row.PeriodKey, Status: model.ClosingClosed, ClosedBy: e.users[0].ID, ClosedAt: e.now}).Error)
	var before model.Transaction
	mustOK(t, e.db.First(&before, row.ID).Error)
	beforeJSON, err := json.Marshal(before)
	mustOK(t, err)
	var yieldsBefore []model.WealthCashYield
	mustOK(t, e.db.Find(&yieldsBefore).Error)
	yieldJSON, err := json.Marshal(yieldsBefore)
	mustOK(t, err)
	invalidated := 0
	e.svc.OnWrite = func(owner int64) {
		if owner == row.CreatedBy {
			invalidated++
		}
	}
	mustOK(t, e.svc.backfillCashYieldCategories(ctx))
	after := cashYieldTransaction(t, e, e.banks[0])
	root, leaf := cashYieldCategoryPath(t, e, after, model.KindIncome)
	if root.Name != "理财" || leaf.Name != "收益-余利宝" || invalidated != 1 {
		t.Fatal(root, leaf, invalidated)
	}
	after.CategoryID = nil
	afterJSON, err := json.Marshal(after)
	mustOK(t, err)
	if string(beforeJSON) != string(afterJSON) {
		t.Fatal("classification rewrote a closed financial fact")
	}
	var yieldsAfter []model.WealthCashYield
	mustOK(t, e.db.Find(&yieldsAfter).Error)
	afterYieldJSON, err := json.Marshal(yieldsAfter)
	mustOK(t, err)
	if string(yieldJSON) != string(afterYieldJSON) || e.bank(t, 0) != "5050.00" {
		t.Fatal("backfill changed yield revisions or money")
	}
	mustOK(t, e.svc.backfillCashYieldCategories(ctx))
	var count int64
	mustOK(t, e.db.Model(&model.WealthCashAudit{}).Where("action = ?", "category_assignment").Count(&count).Error)
	if count != 1 || invalidated != 1 {
		t.Fatal("backfill was not idempotent", count, invalidated)
	}
}

func TestWealthCashYieldCategoryBackfillRollsBackWithAudit(t *testing.T) {
	e := newCashEnv(t)
	enableCash(t, e, "2026-10-12", "unit_nav")
	e.quote(t, "2026-10-12", "1")
	e.quote(t, "2026-10-13", "1.01")
	mustOK(t, e.svc.ensureCashYields(context.Background()))
	row := cashYieldTransaction(t, e, e.banks[0])
	mustOK(t, e.db.Model(&row).UpdateColumn("category_id", nil).Error)
	mustOK(t, e.db.Where("owner_id = ?", row.CreatedBy).Delete(&model.Category{}).Error)
	failure := errors.New("classification audit unavailable")
	mustOK(t, e.db.Callback().Create().Before("gorm:create").Register("fail_category_audit", func(db *gorm.DB) {
		if db.Statement.Table == "wealth_cash_audits" {
			db.AddError(failure)
		}
	}))
	if err := e.svc.backfillCashYieldCategories(context.Background()); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	var fact model.Transaction
	mustOK(t, e.db.First(&fact, row.ID).Error)
	var count int64
	mustOK(t, e.db.Model(&model.Category{}).Where("owner_id = ?", row.CreatedBy).Count(&count).Error)
	if fact.CategoryID != nil || count != 0 || e.bank(t, 0) != "5050.00" {
		t.Fatal("partial metadata backfill", fact, count)
	}
	mustOK(t, e.db.Callback().Create().Remove("fail_category_audit"))
	mustOK(t, e.svc.backfillCashYieldCategories(context.Background()))
	cashYieldCategoryPath(t, e, cashYieldTransaction(t, e, e.banks[0]), model.KindIncome)
}

func TestWealthCashYieldCategoryLongNames(t *testing.T) {
	a := model.Account{ID: 123, Name: strings.Repeat("长", 64)}
	b := a
	b.ID = 124
	if utf8.RuneCountInString(cashYieldCategoryName(a)) > 64 || cashYieldCategoryName(a) == cashYieldCategoryName(b) {
		t.Fatal("invalid or colliding long category names")
	}
}

func TestWealthCashYieldCategoriesSeparateWallets(t *testing.T) {
	e := newCashEnv(t)
	ctx := context.Background()
	mustOK(t, e.db.Model(&model.Account{}).Where("id = ?", e.banks[0]).Update("name", "余利宝").Error)
	enableCash(t, e, "2026-10-12", "unit_nav")
	owner := e.users[0].ID
	other := model.Account{OwnerID: &owner, Name: "零钱通", Type: model.AccountAsset, AssetClass: "cash", Status: model.StatusActive, OpeningBalance: dec("1000")}
	mustOK(t, e.db.Create(&other).Error)
	_, err := e.svc.ConfigureCash(ctx, e.users[0], other.ID, WealthCashInput{RequestKey: uuid.NewString(), Active: true, ProductID: 1, StartDate: "2026-10-12", Policy: "unit_nav"})
	mustOK(t, err)
	e.quote(t, "2026-10-12", "1")
	e.quote(t, "2026-10-13", "1.01")
	mustOK(t, e.svc.ensureCashYields(ctx))
	a, ac := cashYieldCategoryPath(t, e, cashYieldTransaction(t, e, e.banks[0]), model.KindIncome)
	b, bc := cashYieldCategoryPath(t, e, cashYieldTransaction(t, e, other.ID), model.KindIncome)
	if a.ID != b.ID || ac.ID == bc.ID || ac.Name != "收益-余利宝" || bc.Name != "收益-零钱通" {
		t.Fatal("wallet categories mixed", ac, bc)
	}
	manual := model.Transaction{Kind: model.TxnIncome, Amount: dec("1"), AccountID: other.ID, EntryDate: fday("2026-10-13"), PeriodKey: "2026-10", Note: "余利宝收益", Source: model.SourceManual, Purpose: model.PurposeRegular, CreatedBy: owner}
	mustOK(t, e.db.Create(&manual).Error)
	mustOK(t, e.svc.backfillCashYieldCategories(ctx))
	mustOK(t, e.db.First(&manual, manual.ID).Error)
	if manual.CategoryID != nil {
		t.Fatal("classified ordinary income by its note")
	}
}

func TestWealthCashYieldCategoriesAppendToPersonalOrder(t *testing.T) {
	e := newCashEnv(t)
	owner := e.users[0].ID
	prior := model.Category{OwnerID: &owner, Kind: model.KindIncome, Name: "已有收入", Sort: 7}
	mustOK(t, e.db.Create(&prior).Error)
	enableCash(t, e, "2026-10-12", "unit_nav")
	e.quote(t, "2026-10-12", "1")
	e.quote(t, "2026-10-13", "1.01")
	mustOK(t, e.svc.ensureCashYields(context.Background()))
	root, _ := cashYieldCategoryPath(t, e, cashYieldTransaction(t, e, e.banks[0]), model.KindIncome)
	if root.Sort != 8 {
		t.Fatal("automatic category reset existing order", root.Sort)
	}
	stats, err := repository.NewStatsRepo(e.db).ForOwner(owner).SumByPeriod("2026-10")
	mustOK(t, err)
	if !stats.Income.IsZero() || !stats.Expense.IsZero() {
		t.Fatal("classification changed living statistics", stats)
	}
}

func TestWealthCashYieldCategoriesPreserveDefaultInitialization(t *testing.T) {
	e := newCashEnv(t)
	mustOK(t, e.db.AutoMigrate(&model.CategoryTemplate{}))
	mustOK(t, e.db.Create(&model.CategoryTemplate{Kind: model.KindExpense, Name: "默认餐饮"}).Error)
	mustOK(t, e.db.Create(&model.CategoryTemplate{Kind: model.KindIncome, Name: "默认工资"}).Error)
	enableCash(t, e, "2026-10-12", "unit_nav")
	e.quote(t, "2026-10-12", "1")
	e.quote(t, "2026-10-13", "1.01")
	mustOK(t, e.svc.ensureCashYields(context.Background()))
	var defaults int64
	mustOK(t, e.db.Model(&model.Category{}).Where("owner_id = ? AND name IN ?", e.users[0].ID, []string{"默认餐饮", "默认工资"}).Count(&defaults).Error)
	if defaults != 2 {
		t.Fatal("automatic income suppressed default personal categories", defaults)
	}
}

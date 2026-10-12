package service

import (
	"errors"
	"github.com/google/uuid"
	"jizhang/internal/model"
	"jizhang/internal/repository"
	"strings"
	"testing"
)

func TestPersonalFinancialScope(t *testing.T) {
	e := newVoucherEnv(t)
	mustOK(t, e.db.AutoMigrate(&model.Budget{}, &model.Notification{}))
	other := &model.User{ID: 2, Role: model.RoleMember}
	account := model.Account{OwnerID: &other.ID, Name: "另一人的私有资产", Type: model.AccountAsset, Status: model.StatusActive, OpeningBalance: dec("500")}
	mustOK(t, e.db.Create(&account).Error)
	category := model.Category{OwnerID: &other.ID, Name: "另一人的私有分类", Kind: model.KindExpense}
	mustOK(t, e.db.Create(&category).Error)
	own, err := e.txn.Create(adminUser(), expenseIn(t, e.account, e.category, "10", "2026-10-01"))
	mustOK(t, err)
	_, err = e.txn.Create(other, expenseIn(t, account.ID, category.ID, "20", "2026-10-01"))
	mustOK(t, err)
	foreign := expenseIn(t, account.ID, category.ID, "1", "2026-10-01")
	if _, err = e.txn.Create(adminUser(), foreign); !errors.Is(err, ErrTxnForbidden) {
		t.Fatal("admin created foreign posting", err)
	}
	foreign.AccountID = e.account
	if _, err = e.txn.Create(adminUser(), foreign); !errors.Is(err, ErrTxnForbidden) {
		t.Fatal("foreign classification accepted", err)
	}
	if _, err = e.txn.Update(other, own.ID, expenseIn(t, account.ID, category.ID, "1", "2026-10-01")); !errors.Is(err, ErrTxnForbidden) {
		t.Fatal("foreign original mutated", err)
	}
	voucher, err := e.svc.Create(other, VoucherInput{Name: "私券", Platform: "共享商家", PaidAmount: dec("11"), PurchaseDate: vdate(t, "2026-10-10"), AccountID: account.ID, CategoryID: category.ID, TotalUnits: 1, RequestKey: uuid.NewString()})
	mustOK(t, err)
	_, err = e.svc.Operate(other, voucher.ID, model.VoucherRedeem, VoucherOperation{EntryDate: vdate(t, "2026-10-11"), Units: 1, RequestKey: uuid.NewString()})
	mustOK(t, err)
	if _, err = e.svc.Detail(adminUser(), voucher.ID); !errors.Is(err, ErrTxnForbidden) {
		t.Fatal("foreign voucher exposed", err)
	}
	visible, err := e.svc.List(adminUser(), "", "", 1, 20)
	mustOK(t, err)
	if visible.Total != 0 || !visible.UnusedAmount.IsZero() {
		t.Fatal("foreign voucher summary", visible)
	}
	rows, total, err := e.txn.List(adminUser(), repository.TxnFilter{})
	mustOK(t, err)
	if total != 1 || len(rows) != 1 || rows[0].ID != own.ID {
		t.Fatal("personal transaction scope", rows, total)
	}
	csv, err := e.txn.ExportCSV(adminUser(), repository.TxnFilter{})
	mustOK(t, err)
	if strings.Contains(string(csv), "另一人的") {
		t.Fatal("foreign data in CSV")
	}
	stats := repository.NewStatsRepo(e.db)
	for _, tc := range []struct {
		owner   int64
		expense string
	}{{1, "10"}, {2, "31"}} {
		sum, e := stats.ForOwner(tc.owner).SumByPeriod("2026-10")
		mustOK(t, e)
		if !sum.Expense.Equal(dec(tc.expense)) {
			t.Fatal("personal statistics", tc, sum)
		}
	}
	budget := NewBudgetService(BudgetDeps{Budgets: repository.NewBudgetRepo(e.db), Stats: stats, Cats: repository.NewCategoryRepo(e.db), Notifs: repository.NewNotificationRepo(e.db), Periods: e.txn.deps.Periods})
	limit := "5"
	mustOK(t, budget.Set("2026-10", BudgetSetInput{Total: &limit}))
	view, err := budget.ForUser(other).Get("2026-10")
	mustOK(t, err)
	if view.Total.Consumed != "31.00" || view.Total.Budget == nil || *view.Total.Budget != "5.00" {
		t.Fatal("budget scope", view)
	}
	for _, row := range view.Categories {
		if row.CategoryID != category.ID {
			t.Fatal("foreign budget category", row)
		}
	}
	for _, owner := range []int64{1, 2} {
		repo := repository.NewNotificationRepo(e.db).ForOwner(owner)
		notifications, err := repo.List("")
		mustOK(t, err)
		if len(notifications) != 1 || notifications[0].OwnerID == nil || *notifications[0].OwnerID != owner {
			t.Fatal("notification scope", owner, notifications)
		}
		mustOK(t, budget.Evaluate("2026-10"))
		n, err := repo.UnreadCount()
		mustOK(t, err)
		if n != 1 {
			t.Fatal("duplicate notification", n)
		}
	}
	_, err = repository.NewNotificationRepo(e.db).ForOwner(1).MarkAllRead()
	mustOK(t, err)
	unread, err := repository.NewNotificationRepo(e.db).ForOwner(2).UnreadCount()
	mustOK(t, err)
	if unread != 1 {
		t.Fatal("other unread state changed")
	}
}

func TestPersonalFeeSourceRepair(t *testing.T) {
	e := newLiabilityEnv(t)
	foreign := model.Account{OwnerID: testOwner(2), Name: "别人的旧支付账户", Type: model.AccountAsset, Status: model.StatusActive, OpeningBalance: dec("444")}
	mustOK(t, e.db.Create(&foreign).Error)
	input := InstallmentInput{AccountID: e.card, Amount: dec("30"), EntryDate: "2026-10-15", CategoryID: e.category, TermCount: 1, FeeMode: "per_term", FeeInput: dec("1"), FeeAccountID: foreign.ID, FeeCategoryID: e.category, RequestKey: uuid.NewString()}
	if _, err := e.svcL.CreatePlan(adminUser(), input); !errors.Is(err, ErrTxnForbidden) {
		t.Fatal("foreign fee source accepted", err)
	}
	input.FeeAccountID = e.account
	input.RequestKey = uuid.NewString()
	created, err := e.svcL.CreatePlan(adminUser(), input)
	mustOK(t, err)
	mustOK(t, e.db.Model(&model.InstallmentPlan{}).Where("id = ?", created.PlanID).Update("fee_account_id", foreign.ID).Error)
	e.now = vdate(t, "2026-11-01")
	_, err = e.svcL.EnsureDue(e.now)
	mustOK(t, err)
	plan, err := e.svcL.Plan(adminUser(), created.PlanID)
	mustOK(t, err)
	if plan.Terms[0].FeeTxnID != nil || plan.Terms[0].FeeStatus != "blocked" {
		t.Fatal("foreign automatic fee was posted", plan)
	}
	_, err = e.svcL.UpdatePlan(adminUser(), plan.ID, InstallmentUpdate{RequestKey: uuid.NewString(), Reason: "修复费用来源", Version: plan.Version, FeeAccountID: &e.account, FeeCategoryID: &e.category}, false)
	mustOK(t, err)
	_, err = e.svcL.EnsureDue(e.now)
	mustOK(t, err)
	plan, err = e.svcL.Plan(adminUser(), created.PlanID)
	mustOK(t, err)
	if plan.Terms[0].FeeTxnID == nil {
		t.Fatal("repaired fee did not post")
	}
	accounts, err := repository.NewAccountRepo(e.db).List()
	mustOK(t, err)
	for _, a := range accounts {
		if a.ID == foreign.ID && !a.Balance.Equal(dec("444")) {
			t.Fatal("foreign money changed", a)
		}
	}
}

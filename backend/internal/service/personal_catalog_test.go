package service

import (
	"errors"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"jizhang/internal/model"
	"jizhang/internal/repository"
	"testing"
)

func testOwner(id int64) *int64 { return &id }

func TestPersonalCatalogPermissionsAndCreation(t *testing.T) {
	e := newVoucherEnv(t)
	mustOK(t, e.db.AutoMigrate(&model.User{}, &model.AccountTemplate{}, &model.CategoryTemplate{}, &model.AccountCreateRequest{}))
	mustOK(t, e.db.Create(&model.AccountTemplate{Code: "cash", Name: "现金", Sort: 0}).Error)
	mustOK(t, e.db.Create(&[]model.CategoryTemplate{{ID: 1, Kind: model.KindExpense, Name: "餐饮"}, {ID: 2, ParentID: testOwner(1), Kind: model.KindExpense, Name: "早餐"}}).Error)
	accounts := NewAccountService(repository.NewAccountRepo(e.db))
	categories := NewCategoryService(repository.NewCategoryRepo(e.db))
	roles := []model.Role{model.RoleAdmin, model.RoleRecorder, model.RoleMember, model.RoleViewer}
	var first *model.Account
	var firstCategory *model.Category
	sharedKey := uuid.NewString()
	for i, role := range roles {
		actor := &model.User{ID: int64(i + 1), Username: string(role), Role: role, Status: model.StatusActive, LiabilityAccountsInitialized: true}
		mustOK(t, e.db.Create(actor).Error)
		rows, err := accounts.ListFor(actor)
		mustOK(t, err)
		if len(rows) == 0 {
			t.Fatal("missing default assets")
		}
		tree, err := categories.TreeFor(actor)
		mustOK(t, err)
		if len(tree[0].Children) == 0 {
			t.Fatal("missing default categories")
		}
		in := CreateAccountInput{Name: " 同名资产 ", Type: model.AccountAsset, OpeningBalance: dec("12"), RequestKey: sharedKey}
		account, err := accounts.CreateFor(actor, in)
		mustOK(t, err)
		replay, err := accounts.CreateFor(actor, in)
		mustOK(t, err)
		if replay.ID != account.ID || account.Name != "同名资产" {
			t.Fatal("creation retry", account, replay)
		}
		changed := in
		changed.Name = "其他账户"
		_, err = accounts.CreateFor(actor, changed)
		if !errors.Is(err, ErrAccountCreateConflict) {
			t.Fatal("changed request accepted", err)
		}
		parent, err := categories.CreateFor(actor, CreateCategoryInput{Name: "私人分类", Kind: model.KindExpense})
		mustOK(t, err)
		_, err = categories.CreateFor(actor, CreateCategoryInput{Name: "子分类", Kind: model.KindExpense, ParentID: &parent.ID})
		mustOK(t, err)
		if first != nil {
			_, err = accounts.UpdateFor(actor, first.ID, in, model.StatusActive)
			if !errors.Is(err, ErrTxnForbidden) {
				t.Fatal("foreign update", err)
			}
			err = accounts.DeleteFor(actor, first.ID)
			if !errors.Is(err, ErrTxnForbidden) {
				t.Fatal("foreign delete", err)
			}
			_, err = categories.UpdateFor(actor, firstCategory.ID, "越权", 0)
			if !errors.Is(err, ErrTxnForbidden) {
				t.Fatal("foreign category", err)
			}
			_, err = categories.CreateFor(actor, CreateCategoryInput{Name: "越权子分类", Kind: model.KindExpense, ParentID: &firstCategory.ID})
			if !errors.Is(err, ErrTxnForbidden) {
				t.Fatal("foreign parent", err)
			}
		} else {
			first = account
			firstCategory = parent
		}
		if role == model.RoleViewer {
			_, err = e.txn.Create(actor, expenseIn(t, account.ID, parent.ID, "1", "2026-10-01"))
			if !errors.Is(err, ErrTxnForbidden) {
				t.Fatal("viewer obtained money write", err)
			}
		}
		disposable, err := accounts.CreateFor(actor, CreateAccountInput{Name: "临时", Type: model.AccountAsset, RequestKey: in.RequestKey + "delete"})
		mustOK(t, err)
		mustOK(t, accounts.DeleteFor(actor, disposable.ID))
		_, err = accounts.CreateFor(actor, CreateAccountInput{Name: "临时", Type: model.AccountAsset, RequestKey: in.RequestKey + "delete"})
		if !errors.Is(err, ErrAccountCreateConflict) {
			t.Fatal("deleted request recreated account", err)
		}
		leaf := tree[0].Children[0]
		if len(leaf.Children) > 0 {
			leaf = leaf.Children[0]
		}
		mustOK(t, categories.DeleteFor(actor, leaf.ID))
		again, err := categories.TreeFor(actor)
		mustOK(t, err)
		for _, root := range again[0].Children {
			for _, c := range root.Children {
				if c.ID == leaf.ID {
					t.Fatal("deleted category reseeded")
				}
			}
		}
	}
	if _, err := accounts.CreateFor(nil, CreateAccountInput{Name: "nil", Type: model.AccountAsset}); !errors.Is(err, ErrTxnForbidden) {
		t.Fatal(err)
	}
	actor := &model.User{ID: 1, Role: model.RoleAdmin}
	category, err := categories.CreateFor(actor, CreateCategoryInput{Name: "资金分类", Kind: model.KindExpense})
	mustOK(t, err)
	_, err = e.txn.Create(actor, expenseIn(t, first.ID, category.ID, "1", "2026-10-01"))
	mustOK(t, err)
	if err = accounts.DeleteFor(actor, first.ID); !errors.Is(err, ErrAccountHasTransactions) {
		t.Fatal("deleted referenced funds", err)
	}
	if err = categories.DeleteFor(actor, category.ID); !errors.Is(err, ErrCategoryHasTransactions) {
		t.Fatal("deleted referenced classification", err)
	}
	_, err = accounts.UpdateFor(actor, first.ID, CreateAccountInput{Name: first.Name, Type: model.AccountLiability}, model.StatusActive)
	if !errors.Is(err, ErrAccountHasTransactions) {
		t.Fatal("changed historical account type", err)
	}
	mustOK(t, e.db.AutoMigrate(&model.RecurringRule{}))
	unposted, err := categories.CreateFor(actor, CreateCategoryInput{Name: "未来分类", Kind: model.KindExpense})
	mustOK(t, err)
	mustOK(t, e.db.Create(&model.RecurringRule{AccountID: first.ID, CategoryID: unposted.ID, CreatedBy: 1, Amount: dec("1"), Freq: model.FreqMonthly, StartDate: vdate(t, "2026-10-01"), NextRunDate: vdate(t, "2026-11-01"), Status: model.RulePaused}).Error)
	if err = categories.DeleteFor(actor, unposted.ID); !errors.Is(err, ErrCategoryHasReferences) {
		t.Fatal("deleted live template classification", err)
	}
	mustOK(t, e.db.Callback().Create().Before("gorm:create").Register("catalog_request_failure", func(tx *gorm.DB) {
		if tx.Statement.Table == "account_create_requests" {
			tx.AddError(errors.New("request storage failure"))
		}
	}))
	_, err = accounts.CreateFor(actor, CreateAccountInput{Name: "不能部分保存", Type: model.AccountAsset, RequestKey: uuid.NewString()})
	if err == nil {
		t.Fatal("expected request failure")
	}
	mustOK(t, e.db.Callback().Create().Remove("catalog_request_failure"))
	var n int64
	mustOK(t, e.db.Model(&model.Account{}).Where("name = ?", "不能部分保存").Count(&n).Error)
	if n != 0 {
		t.Fatal("partial account committed")
	}
}

package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"gorm.io/gorm"
	"jizhang/internal/model"
	"jizhang/internal/repository"
	"slices"
	"testing"
)

func TestCatalogOrdering(t *testing.T) {
	e := newVoucherEnv(t)
	mustOK(t, e.db.AutoMigrate(&model.AccountCreateRequest{}, &model.CatalogOrderRequest{}))
	mustOK(t, e.db.Model(&model.User{}).Where("id > 0").Updates(map[string]any{"categories_initialized": true, "asset_accounts_initialized": true, "liability_accounts_initialized": true, "voucher_platforms_initialized": true}).Error)
	accounts := NewAccountService(repository.NewAccountRepo(e.db))
	categories := NewCategoryService(repository.NewCategoryRepo(e.db))
	roles := []model.Role{model.RoleAdmin, model.RoleViewer, model.RoleMember, model.RoleRecorder}
	for i, role := range roles {
		actor := &model.User{ID: int64(i + 1), Role: role, Status: model.StatusActive}
		a, err := accounts.CreateFor(actor, CreateAccountInput{Name: "现金", Type: model.AccountAsset})
		mustOK(t, err)
		b, err := accounts.CreateFor(actor, CreateAccountInput{Name: "基金", Type: model.AccountAsset, AssetClass: "fund"})
		mustOK(t, err)
		credit := model.Account{OwnerID: &actor.ID, Type: model.AccountLiability, Name: "停用信用", Status: model.StatusDisabled, Sort: b.Sort + 1}
		mustOK(t, e.db.Create(&credit).Error)
		ids, err := repository.AccountOrder(e.db, actor.ID).IDs()
		mustOK(t, err)
		before := slices.Clone(ids)
		slices.Reverse(ids)
		out, err := accounts.ReorderFor(actor, CatalogOrderInput{ids, before})
		mustOK(t, err)
		if !slices.Equal(out.OrderedIDs, ids) {
			t.Fatal(out)
		}
		_, err = accounts.ReorderFor(actor, CatalogOrderInput{ids, before})
		mustOK(t, err)
		_, err = accounts.ReorderFor(actor, CatalogOrderInput{before, before})
		if !errors.Is(err, ErrOrderConflict) {
			t.Fatal("old basis accepted", err)
		}
		_, err = accounts.ReorderFor(actor, CatalogOrderInput{[]int64{a.ID, a.ID}, []int64{a.ID, b.ID}})
		if !errors.Is(err, ErrOrderInvalid) {
			t.Fatal(err)
		}
		_, err = accounts.ReorderFor(actor, CatalogOrderInput{ids[:len(ids)-1], ids[:len(ids)-1]})
		if !errors.Is(err, ErrOrderConflict) {
			t.Fatal("partial list accepted", err)
		}
		_, err = accounts.ReorderFor(actor, CatalogOrderInput{[]int64{999999}, []int64{999999}})
		if !errors.Is(err, ErrOrderNotFound) {
			t.Fatal(err)
		}
		if actor.ID != 1 {
			_, err = accounts.ReorderFor(actor, CatalogOrderInput{[]int64{e.account}, []int64{e.account}})
			if !errors.Is(err, ErrTxnForbidden) {
				t.Fatal("foreign account accepted", err)
			}
		}
		parent, err := categories.CreateFor(actor, CreateCategoryInput{Kind: model.KindExpense, Name: "餐饮"})
		mustOK(t, err)
		other, err := categories.CreateFor(actor, CreateCategoryInput{Kind: model.KindExpense, Name: "交通"})
		mustOK(t, err)
		leaf, err := categories.CreateFor(actor, CreateCategoryInput{Kind: model.KindExpense, Name: "早餐", ParentID: &parent.ID})
		mustOK(t, err)
		leaf2, err := categories.CreateFor(actor, CreateCategoryInput{Kind: model.KindExpense, Name: "晚餐", ParentID: &parent.ID})
		mustOK(t, err)
		rootIDs, err := repository.CategoryOrder(e.db, actor.ID, model.KindExpense, nil).IDs()
		mustOK(t, err)
		newRoots := slices.Clone(rootIDs)
		slices.Reverse(newRoots)
		_, err = categories.ReorderFor(actor, CategoryOrderInput{CatalogOrderInput: CatalogOrderInput{newRoots, rootIDs}, Kind: model.KindExpense})
		mustOK(t, err)
		_, err = categories.ReorderFor(actor, CategoryOrderInput{CatalogOrderInput: CatalogOrderInput{[]int64{leaf2.ID, leaf.ID}, []int64{leaf.ID, leaf2.ID}}, Kind: model.KindExpense, ParentID: &parent.ID})
		mustOK(t, err)
		_, err = categories.UpdateFor(actor, leaf.ID, "早餐改名")
		mustOK(t, err)
		var got model.Category
		mustOK(t, e.db.First(&got, leaf.ID).Error)
		if got.Sort != 1 || got.ParentID == nil || *got.ParentID != parent.ID {
			t.Fatal("rename lost position/parent", got)
		}
		_, err = categories.ReorderFor(actor, CategoryOrderInput{CatalogOrderInput: CatalogOrderInput{[]int64{leaf.ID, other.ID}, []int64{other.ID, leaf.ID}}, Kind: model.KindExpense, ParentID: &parent.ID})
		if !errors.Is(err, ErrOrderInvalid) {
			t.Fatal(err)
		}
		p, err := e.svc.CreatePlatform(actor, "Z常用")
		mustOK(t, err)
		p2, err := e.svc.CreatePlatform(actor, "A后来")
		mustOK(t, err)
		_, err = e.svc.ReorderPlatforms(actor, CatalogOrderInput{[]int64{p2.ID, p.ID}, []int64{p.ID, p2.ID}})
		mustOK(t, err)
		_, err = e.svc.RenamePlatform(actor, p2.ID, "新名称")
		mustOK(t, err)
		replay, err := e.svc.CreatePlatform(actor, "新名称")
		mustOK(t, err)
		if replay.ID != p2.ID || replay.Sort != 0 {
			t.Fatal("same name moved", replay)
		}
		rows, err := e.svc.ListPlatforms(actor)
		mustOK(t, err)
		if rows[0].ID != p2.ID {
			t.Fatal(rows)
		}
	}
	var original model.Account
	mustOK(t, e.db.First(&original, e.account).Error)
	if !original.OpeningBalance.Equal(dec("1000")) {
		t.Fatal("sorting changed opening balance")
	}
	var txnCount int64
	mustOK(t, e.db.Model(&model.Transaction{}).Count(&txnCount).Error)
	if txnCount != 0 {
		t.Fatal("sorting created financial rows")
	}
	_, err := accounts.ReorderFor(nil, CatalogOrderInput{[]int64{}, []int64{}})
	if !errors.Is(err, ErrTxnForbidden) {
		t.Fatal(err)
	}
	foreign, err := repository.AccountOrder(e.db, 2).IDs()
	mustOK(t, err)
	_, err = accounts.ReorderFor(adminUser(), CatalogOrderInput{foreign, foreign})
	if !errors.Is(err, ErrTxnForbidden) {
		t.Fatal("administrator bypassed ownership", err)
	}
	foreignParents, err := repository.CategoryOrder(e.db, 2, model.KindExpense, nil).IDs()
	mustOK(t, err)
	_, err = categories.ReorderFor(adminUser(), CategoryOrderInput{CatalogOrderInput: CatalogOrderInput{[]int64{}, []int64{}}, Kind: model.KindExpense, ParentID: &foreignParents[0]})
	if !errors.Is(err, ErrTxnForbidden) {
		t.Fatal("foreign parent accepted", err)
	}
	for _, input := range []CatalogOrderInput{{[]int64{-1}, []int64{-1}}, {[]int64{}, nil}} {
		_, err = accounts.ReorderFor(adminUser(), input)
		if !errors.Is(err, ErrOrderInvalid) {
			t.Fatal("invalid identifiers accepted", err)
		}
	}
	mustOK(t, e.db.Create(&model.PeriodClosing{PeriodKey: "2026-10", Status: model.ClosingClosed, ClosedBy: 1, ClosedAt: vdate(t, "2026-11-01")}).Error)
	closedIDs, err := repository.AccountOrder(e.db, 1).IDs()
	mustOK(t, err)
	closedTarget := slices.Clone(closedIDs)
	slices.Reverse(closedTarget)
	_, err = accounts.ReorderFor(adminUser(), CatalogOrderInput{closedTarget, closedIDs})
	mustOK(t, err)
	var closingCount int64
	mustOK(t, e.db.Model(&model.PeriodClosing{}).Where("status = ?", model.ClosingClosed).Count(&closingCount).Error)
	if closingCount != 1 {
		t.Fatal("sorting changed closed period")
	}
	before, err := repository.AccountOrder(e.db, 1).IDs()
	mustOK(t, err)
	next := slices.Clone(before)
	slices.Reverse(next)
	writes := 0
	mustOK(t, e.db.Callback().Update().Before("gorm:update").Register("ordering_fail", func(tx *gorm.DB) {
		if tx.Statement.Table == "accounts" {
			writes++
			if writes == 2 {
				tx.AddError(errors.New("storage rejected"))
			}
		}
	}))
	_, err = accounts.ReorderFor(adminUser(), CatalogOrderInput{next, before})
	if err == nil {
		t.Fatal("write failure ignored")
	}
	mustOK(t, e.db.Callback().Update().Remove("ordering_fail"))
	after, err := repository.AccountOrder(e.db, 1).IDs()
	mustOK(t, err)
	if !slices.Equal(before, after) {
		t.Fatal("partial save", before, after)
	}
	mustOK(t, e.db.Model(&model.User{}).Where("id=1").Update("voucher_platforms_initialized", false).Error)
	_, err = e.svc.ReorderPlatforms(adminUser(), CatalogOrderInput{[]int64{}, []int64{}})
	if !errors.Is(err, ErrOrderConflict) {
		t.Fatal("uninitialized accepted", err)
	}
}

func TestAccountOrderingCreateReplay(t *testing.T) {
	e := newVoucherEnv(t)
	mustOK(t, e.db.AutoMigrate(&model.AccountCreateRequest{}))
	s := NewAccountService(repository.NewAccountRepo(e.db))
	in := CreateAccountInput{Name: "新账户", Type: model.AccountAsset, ActorID: 1, RequestKey: "order-create"}
	a, err := s.Create(in)
	mustOK(t, err)
	_, err = s.Create(CreateAccountInput{Name: "后创建", Type: model.AccountAsset, ActorID: 1})
	mustOK(t, err)
	replay, err := s.Create(in)
	mustOK(t, err)
	if replay.ID != a.ID || replay.Sort != a.Sort {
		t.Fatal("replay appended again")
	}
	changed := in
	changed.SortProvided = true
	_, err = s.Create(changed)
	if !errors.Is(err, ErrAccountCreateConflict) {
		t.Fatal("changed sort presence accepted", err)
	}
	legacy := in
	legacy.RequestKey = ""
	raw, err := json.Marshal(legacy)
	mustOK(t, err)
	hash := sha256.Sum256(raw)
	mustOK(t, e.db.Model(&model.AccountCreateRequest{}).Where("request_key=?", in.RequestKey).Update("digest", hex.EncodeToString(hash[:])).Error)
	replay, err = s.Create(in)
	mustOK(t, err)
	if replay.ID != a.ID {
		t.Fatal("legacy digest replay failed")
	}
	_, err = s.Update(a.ID, CreateAccountInput{Name: "改名", Type: model.AccountAsset, OpeningBalance: a.OpeningBalance, ActorID: 1}, model.StatusDisabled)
	mustOK(t, err)
	var edited model.Account
	mustOK(t, e.db.First(&edited, a.ID).Error)
	if edited.Sort != a.Sort {
		t.Fatal("edit reset position")
	}
	_, err = s.Update(a.ID, CreateAccountInput{Name: "改名", Type: model.AccountAsset, OpeningBalance: a.OpeningBalance, ActorID: 1, SortProvided: true, Sort: 0}, model.StatusActive)
	mustOK(t, err)
	mustOK(t, e.db.First(&edited, a.ID).Error)
	if edited.Sort != 0 {
		t.Fatal("explicit zero ignored")
	}
}

func TestCatalogOrderingPreservesFundHistory(t *testing.T) {
	e := newFundEnv(t)
	mustOK(t, e.db.AutoMigrate(&model.CatalogOrderRequest{}))
	mustOK(t, e.db.Model(&model.User{}).Where("id=1").Updates(map[string]any{"asset_accounts_initialized": true, "liability_accounts_initialized": true}).Error)
	opening := e.opening(t, 0, "5028.62", "1.4119", "2026-10-09")
	before, err := e.svc.Position(e.users[0], opening.PositionID)
	mustOK(t, err)
	rows, err := e.accounts.ForOwner(1).List()
	mustOK(t, err)
	s := NewAccountService(e.accounts)
	_, err = s.ReorderFor(e.users[0], CatalogOrderInput{[]int64{e.funds[0], e.banks[0]}, []int64{e.banks[0], e.funds[0]}}, "fund-sort")
	mustOK(t, err)
	after, err := e.svc.Position(e.users[0], opening.PositionID)
	mustOK(t, err)
	b, _ := json.Marshal(before)
	a, _ := json.Marshal(after)
	if string(a) != string(b) || e.bank(t, 0) != "5000.00" {
		t.Fatal("sorting changed holdings or cash")
	}
	ordered, err := e.accounts.ForOwner(1).List()
	mustOK(t, err)
	balances := map[int64]string{}
	for _, row := range rows {
		balances[row.ID] = row.Balance.StringFixed(2)
	}
	for _, row := range ordered {
		if balances[row.ID] != row.Balance.StringFixed(2) {
			t.Fatal("sorting changed valuation", row)
		}
	}
	if ordered[0].ID != e.funds[0] {
		t.Fatal("fund account did not move")
	}
}

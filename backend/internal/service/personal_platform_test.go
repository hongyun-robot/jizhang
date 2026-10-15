package service

import (
	"errors"
	"strings"
	"sync"
	"testing"

	"jizhang/internal/model"
	"jizhang/internal/repository"
)

func TestVoucherPlatformPersonalModel(t *testing.T) {
	e := newVoucherEnv(t)
	first := model.VoucherPlatform{OwnerID: 1, Name: "同名商家"}
	second := model.VoucherPlatform{OwnerID: 2, Name: first.Name}
	mustOK(t, e.db.Create(&first).Error)
	mustOK(t, e.db.Create(&second).Error)
	if first.ID == second.ID {
		t.Fatal("owners reused a platform identity")
	}
	if err := e.db.Create(&model.VoucherPlatform{OwnerID: 1, Name: first.Name}).Error; err == nil {
		t.Fatal("duplicate owner/name was accepted")
	}
}

func TestPersonalPlatformAutomaticRefundKeepsOwnerAndHistory(t *testing.T) {
	e := newVoucherEnv(t)
	other := &model.User{ID: 3, Role: model.RoleMember, Status: model.StatusActive}
	account := model.Account{OwnerID: &other.ID, Name: "他人私有现金", Type: model.AccountAsset, Status: model.StatusActive, OpeningBalance: dec("100")}
	category := model.Category{OwnerID: &other.ID, Name: "他人私有分类", Kind: model.KindExpense}
	mustOK(t, e.db.Create(&account).Error)
	mustOK(t, e.db.Create(&category).Error)
	input := e.input(t, "10", 1, "2026-10-31")
	input.AccountID, input.CategoryID, input.Platform = account.ID, category.ID, "他人的历史商家"
	voucher, err := e.svc.Create(other, input)
	mustOK(t, err)
	rows, err := e.svc.ListPlatforms(other)
	mustOK(t, err)
	_, err = e.svc.RenamePlatform(other, rows[0].ID, "他人的新候选名称")
	mustOK(t, err)
	result, err := e.svc.EnsureDue(vdate(t, "2026-12-02"))
	mustOK(t, err)
	if result.Generated != 1 {
		t.Fatal(result)
	}
	own, err := e.svc.ListPlatforms(adminUser())
	mustOK(t, err)
	if len(own) != 0 {
		t.Fatal("background refund leaked a private merchant", own)
	}
	rows, err = e.svc.ListPlatforms(other)
	mustOK(t, err)
	if len(rows) != 1 || rows[0].Name != "他人的新候选名称" {
		t.Fatal("automatic refund rebuilt the old candidate", rows)
	}
	detail, err := e.svc.Detail(other, voucher.ID)
	mustOK(t, err)
	if detail.Platform != input.Platform || detail.Events[len(detail.Events)-1].CreatedBy != other.ID {
		t.Fatal("background operation changed history or creator", detail)
	}
	e.balance(t, "1000")
	accounts, err := repository.NewAccountRepo(e.db).List()
	mustOK(t, err)
	for _, row := range accounts {
		if row.ID == account.ID && !row.Balance.Equal(dec("100")) {
			t.Fatal("refund changed the wrong account", row)
		}
	}
}

func TestPersonalPlatformEditingLegacyCreatorUsesCurrentOwner(t *testing.T) {
	e := newVoucherEnv(t)
	input := expenseIn(t, e.account, e.category, "10", "2026-10-01")
	input.Platform = "原候选"
	row, err := e.txn.Create(adminUser(), input)
	mustOK(t, err)
	// The payment resource now belongs to admin, while the legacy creator does not.
	mustOK(t, e.db.Model(&model.Transaction{}).Where("id=?", row.ID).Update("created_by", 2).Error)
	input.Platform = "修改后的本人商家"
	updated, err := e.txn.Update(adminUser(), row.ID, input)
	mustOK(t, err)
	if updated.CreatedBy != 2 {
		t.Fatal("update rewrote the original creator")
	}
	rows, err := e.svc.ListPlatforms(adminUser())
	mustOK(t, err)
	found := false
	for _, candidate := range rows {
		if candidate.Name == input.Platform {
			found = true
		}
	}
	if !found {
		t.Fatal("updated name missing from resource owner", rows)
	}
	other, err := e.svc.ListPlatforms(&model.User{ID: 2, Role: model.RoleViewer})
	mustOK(t, err)
	if len(other) != 0 {
		t.Fatal("update wrote the old creator's directory", other)
	}
	e.balance(t, "990")
}

func TestPersonalPlatformManagement(t *testing.T) {
	e := newVoucherEnv(t)
	roles := []model.Role{model.RoleAdmin, model.RoleViewer, model.RoleMember, model.RoleRecorder}
	rows := make([]*model.VoucherPlatform, len(roles))
	for i, role := range roles {
		actor := &model.User{ID: int64(i + 1), Role: role, Status: model.StatusActive}
		row, err := e.svc.CreatePlatform(actor, "  同名商家  ")
		mustOK(t, err)
		rows[i] = row
		own, err := e.svc.ListPlatforms(actor)
		mustOK(t, err)
		if len(own) != 1 || own[0].OwnerID != actor.ID || own[0].Name != "同名商家" {
			t.Fatal("list leaked another owner", own)
		}
		for j := 0; j < i; j++ {
			if rows[j].ID == row.ID {
				t.Fatal("same-name identities shared across owners")
			}
		}
	}
	admin := adminUser()
	for _, foreign := range rows[1:] {
		if _, err := e.svc.RenamePlatform(admin, foreign.ID, "越权"); !errors.Is(err, ErrTxnForbidden) {
			t.Fatal("admin renamed another user's entry", err)
		}
		if err := e.svc.DeletePlatform(admin, foreign.ID); !errors.Is(err, ErrTxnForbidden) {
			t.Fatal("admin deleted another user's entry", err)
		}
	}
	in := e.input(t, "10", 1, "")
	in.Platform = rows[0].Name
	voucher, err := e.svc.Create(admin, in)
	mustOK(t, err)
	if err = e.svc.DeletePlatform(admin, rows[0].ID); !errors.Is(err, ErrPlatformReferenced) {
		t.Fatal("deleted a referenced name", err)
	}
	mustOK(t, e.svc.Delete(admin, voucher.ID))
	if err = e.svc.DeletePlatform(admin, rows[0].ID); !errors.Is(err, ErrPlatformReferenced) {
		t.Fatal("ignored retained deleted voucher", err)
	}
	renamed, err := e.svc.RenamePlatform(admin, rows[0].ID, "  新名称  ")
	mustOK(t, err)
	if renamed.ID != rows[0].ID || renamed.Name != "新名称" {
		t.Fatal("rename changed identity or failed to trim", renamed)
	}
	var historical model.Voucher
	mustOK(t, e.db.Unscoped().First(&historical, voucher.ID).Error)
	if historical.Platform != "同名商家" {
		t.Fatal("rename rewrote financial text", historical.Platform)
	}
	_, err = e.svc.CreatePlatform(admin, "冲突名称")
	mustOK(t, err)
	if _, err = e.svc.RenamePlatform(admin, renamed.ID, "冲突名称"); !errors.Is(err, ErrPlatformDuplicate) {
		t.Fatal("rename merged duplicate entries", err)
	}
	_, err = e.svc.RenamePlatform(admin, renamed.ID, "新名称")
	mustOK(t, err)
	mustOK(t, e.db.Exec(`CREATE TRIGGER reject_platform_rename BEFORE UPDATE OF name ON voucher_platforms WHEN NEW.name='存储失败' BEGIN SELECT RAISE(ABORT,'storage failed'); END`).Error)
	if _, err = e.svc.RenamePlatform(admin, renamed.ID, "存储失败"); err == nil || errors.Is(err, ErrPlatformDuplicate) {
		t.Fatal("rename masked a storage error", err)
	}
	mustOK(t, e.db.Exec("DROP TRIGGER reject_platform_rename").Error)
	mustOK(t, e.db.Exec(`CREATE TRIGGER reject_platform_delete BEFORE DELETE ON voucher_platforms BEGIN SELECT RAISE(ABORT,'storage failed'); END`).Error)
	if err = e.svc.DeletePlatform(admin, renamed.ID); err == nil {
		t.Fatal("delete masked a storage error")
	}
	mustOK(t, e.db.Exec("DROP TRIGGER reject_platform_delete").Error)
	var retained model.VoucherPlatform
	mustOK(t, e.db.First(&retained, renamed.ID).Error)
	if retained.Name != "新名称" {
		t.Fatal("failed write changed the directory", retained)
	}
	legacy := model.Voucher{Name: "历史空白名称", Platform: "\t  历史商家\u00a0", PaidAmount: dec("1"), AccountID: e.account,
		CategoryID: e.category, TotalUnits: 1, PurchaseDate: vdate(t, "2026-10-01"), CreatedBy: 1, CreateRequestKey: "legacy-platform-spaces"}
	mustOK(t, e.db.Create(&legacy).Error)
	legacyName, err := e.svc.CreatePlatform(admin, "历史商家")
	mustOK(t, err)
	if err = e.svc.DeletePlatform(admin, legacyName.ID); !errors.Is(err, ErrPlatformReferenced) {
		t.Fatal("normalized legacy text did not protect its candidate", err)
	}
	for _, name := range []string{" ", strings.Repeat("商", 129)} {
		if _, err = e.svc.RenamePlatform(admin, renamed.ID, name); !errors.Is(err, ErrVoucherInvalid) {
			t.Fatal("rename accepted invalid name", err)
		}
	}
	viewer := &model.User{ID: 2, Role: model.RoleViewer, Status: model.StatusActive}
	// The administrator's retained same-name history cannot block the viewer.
	mustOK(t, e.svc.DeletePlatform(viewer, rows[1].ID))
	if err = e.svc.DeletePlatform(viewer, rows[1].ID); !errors.Is(err, ErrPlatformNotFound) {
		t.Fatal("deleted entry was not reported missing", err)
	}
	if _, err = e.svc.Create(viewer, in); !errors.Is(err, ErrTxnForbidden) {
		t.Fatal("directory access granted a financial write", err)
	}
	if _, err = e.svc.ListPlatforms(nil); !errors.Is(err, ErrTxnForbidden) {
		t.Fatal("anonymous directory read", err)
	}
	e.balance(t, "1000")
}

func TestPersonalPlatformInstallmentRollback(t *testing.T) {
	e := newLiabilityEnv(t)
	input := InstallmentInput{AccountID: e.card, Amount: dec("100"), EntryDate: "2026-10-15", CategoryID: e.category,
		Platform: "分期本人商家", TermCount: 0, FeeMode: "per_term", FeeInput: dec("0"), FeeAccountID: e.card,
		FeeCategoryID: e.category, RequestKey: "platform-plan-failure"}
	if _, err := e.svcL.CreatePlan(adminUser(), input); err == nil {
		t.Fatal("invalid plan succeeded")
	}
	var count int64
	mustOK(t, e.db.Model(&model.Transaction{}).Count(&count).Error)
	if count != 0 {
		t.Fatal("failed plan left its consumption", count)
	}
	mustOK(t, e.db.Model(&model.VoucherPlatform{}).Count(&count).Error)
	if count != 0 {
		t.Fatal("failed plan committed a name", count)
	}
	input.TermCount = 2
	input.RequestKey = "platform-plan-success"
	_, err := e.svcL.CreatePlan(adminUser(), input)
	mustOK(t, err)
	rows, err := e.svc.ListPlatforms(adminUser())
	mustOK(t, err)
	if len(rows) != 1 || rows[0].Name != input.Platform {
		t.Fatal(rows)
	}
	other, err := e.svc.ListPlatforms(&model.User{ID: 2, Role: model.RoleViewer})
	mustOK(t, err)
	if len(other) != 0 {
		t.Fatal("plan name leaked to another account", other)
	}
}

func TestPersonalPlatformInitialization(t *testing.T) {
	e := newVoucherEnv(t)
	for _, name := range []string{"支付宝", "微信", "淘宝", "天猫", "京东", "拼多多", "抖音", "美团", "饿了么"} {
		mustOK(t, e.db.Create(&model.VoucherPlatformTemplate{Name: name}).Error)
	}
	mustOK(t, e.db.Exec(`CREATE TRIGGER reject_platform_initialization BEFORE INSERT ON voucher_platforms WHEN NEW.name='微信' BEGIN SELECT RAISE(ABORT,'initialization failed'); END`).Error)
	if _, err := e.svc.ListPlatforms(adminUser()); err == nil {
		t.Fatal("initialization failure was swallowed")
	}
	var user model.User
	mustOK(t, e.db.First(&user, 1).Error)
	var count int64
	mustOK(t, e.db.Model(&model.VoucherPlatform{}).Count(&count).Error)
	if count != 0 || user.VoucherPlatformsInitialized {
		t.Fatal("failed initialization committed partial state", count, user)
	}
	mustOK(t, e.db.Exec("DROP TRIGGER reject_platform_initialization").Error)
	var wg sync.WaitGroup
	errs := make(chan error, 6)
	for range 6 {
		wg.Go(func() {
			_, err := e.svc.ListPlatforms(adminUser())
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		mustOK(t, err)
	}
	rows, err := e.svc.ListPlatforms(adminUser())
	mustOK(t, err)
	if len(rows) != 9 {
		t.Fatal("defaults duplicated", rows)
	}
	for _, row := range rows {
		if row.Name == "淘宝" {
			mustOK(t, e.svc.DeletePlatform(adminUser(), row.ID))
		}
	}
	_, err = e.svc.CreatePlatform(adminUser(), "仅本人自定义")
	mustOK(t, err)
	for range 3 {
		rows, err = NewVoucherService(e.db, e.txn).ListPlatforms(adminUser())
		mustOK(t, err)
		for _, row := range rows {
			if row.Name == "淘宝" {
				t.Fatal("deleted default restored")
			}
		}
	}
	rows, err = e.svc.ListPlatforms(&model.User{ID: 2, Role: model.RoleViewer})
	mustOK(t, err)
	if len(rows) != 9 {
		t.Fatal("new owner inherited a private catalog", rows)
	}
	for _, row := range rows {
		if row.Name == "仅本人自定义" {
			t.Fatal("private name seeded to new owner")
		}
	}
}

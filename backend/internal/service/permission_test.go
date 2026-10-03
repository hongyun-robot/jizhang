package service

import (
	"testing"

	"jizhang/internal/model"
)

func user(id int64, role model.Role) *model.User {
	return &model.User{ID: id, Role: role, Status: model.StatusActive}
}

// TestPermissionMatrix 覆盖 specs/identity/access-control 全部场景：
// 四档角色 × 建/改/删（自己与他人流水）× 设置类操作。
func TestPermissionMatrix(t *testing.T) {
	const (
		selfID     = int64(10)
		otherID    = int64(20)
		selfOwned  = int64(10)
		otherOwned = int64(20)
	)

	admin := user(selfID, model.RoleAdmin)
	recorder := user(selfID, model.RoleRecorder)
	member := user(selfID, model.RoleMember)
	viewer := user(selfID, model.RoleViewer)

	cases := []struct {
		name      string
		u         *model.User
		createdBy int64
		create    bool
		modify    bool
		settings  bool
	}{
		{"管理员改他人流水", admin, otherOwned, true, true, true},
		{"记账员改他人流水", recorder, otherOwned, true, true, false},
		{"记账员改自己流水", recorder, selfOwned, true, true, false},
		{"普通成员改自己流水", member, selfOwned, true, true, false},
		{"普通成员改他人流水", member, otherOwned, true, false, false},
		{"只读成员记账被拒", viewer, selfOwned, false, false, false},
		{"只读成员改他人流水被拒", viewer, otherOwned, false, false, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CanCreateTransaction(tc.u); got != tc.create {
				t.Errorf("CanCreateTransaction = %v, want %v", got, tc.create)
			}
			if got := CanModifyTransaction(tc.u, tc.createdBy); got != tc.modify {
				t.Errorf("CanModifyTransaction(createdBy=%d) = %v, want %v", tc.createdBy, got, tc.modify)
			}
			if got := CanManageSettings(tc.u); got != tc.settings {
				t.Errorf("CanManageSettings = %v, want %v", got, tc.settings)
			}
			if !CanView(tc.u) {
				t.Errorf("CanView = false, want true（四档均可查看）")
			}
		})
	}

	// 边界：nil 用户一律拒绝
	if CanCreateTransaction(nil) || CanModifyTransaction(nil, selfOwned) || CanManageSettings(nil) || CanView(nil) {
		t.Error("nil 用户必须全部被拒")
	}
	_ = otherID
}

// TestCanManageRule 覆盖 specs/recurring-entries「模板按创建者归属隔离」：
// 只读/nil 拒绝、管理员管全部、记账员与普通成员仅管自己的。
func TestCanManageRule(t *testing.T) {
	const (
		selfID  = int64(10)
		otherID = int64(20)
	)
	ownRule := &model.RecurringRule{ID: 1, CreatedBy: selfID}
	otherRule := &model.RecurringRule{ID: 2, CreatedBy: otherID}

	cases := []struct {
		name string
		u    *model.User
		rule *model.RecurringRule
		want bool
	}{
		{"管理员管他人模板", user(selfID, model.RoleAdmin), otherRule, true},
		{"管理员管自己模板", user(selfID, model.RoleAdmin), ownRule, true},
		{"记账员管自己模板", user(selfID, model.RoleRecorder), ownRule, true},
		{"记账员管他人模板被拒", user(selfID, model.RoleRecorder), otherRule, false},
		{"普通成员管自己模板", user(selfID, model.RoleMember), ownRule, true},
		{"普通成员管他人模板被拒", user(selfID, model.RoleMember), otherRule, false},
		{"只读成员管自己模板被拒", user(selfID, model.RoleViewer), ownRule, false},
		{"只读成员管他人模板被拒", user(selfID, model.RoleViewer), otherRule, false},
		{"nil 用户被拒", nil, ownRule, false},
		{"nil 模板被拒", user(selfID, model.RoleMember), nil, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CanManageRule(tc.u, tc.rule); got != tc.want {
				t.Errorf("CanManageRule = %v, want %v", got, tc.want)
			}
		})
	}

	// 创建资格：三档可建，只读与 nil 不可
	for _, tc := range []struct {
		name string
		u    *model.User
		want bool
	}{
		{"管理员可创建模板", user(selfID, model.RoleAdmin), true},
		{"记账员可创建模板", user(selfID, model.RoleRecorder), true},
		{"普通成员可创建模板", user(selfID, model.RoleMember), true},
		{"只读成员不可创建模板", user(selfID, model.RoleViewer), false},
		{"nil 不可创建模板", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := CanManageRuleCreate(tc.u); got != tc.want {
				t.Errorf("CanManageRuleCreate = %v, want %v", got, tc.want)
			}
		})
	}
}

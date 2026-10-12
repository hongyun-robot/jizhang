package service

import "jizhang/internal/model"

// 授权矩阵（specs/identity/access-control 的可执行版本）。
// 路由中间件做粗粒度门禁，写操作前必须调用这里的细粒度判断。

// CanManageSettings 账期、封账、预算、用户管理仅管理员；个人目录按归属授权。
// 周期模板管理不在此列，见 CanManageRule（specs/recurring-entries 按创建者归属隔离）。
func CanManageSettings(u *model.User) bool {
	return u != nil && u.Role == model.RoleAdmin
}

// CanManageRuleCreate 创建周期模板的资格：与记账权限同级（管理员/记账员/普通成员），只读不可。
func CanManageRuleCreate(u *model.User) bool {
	return CanCreateTransaction(u)
}

// CanManageRule 周期模板的修改/启停/删除（specs/recurring-entries「按创建者归属隔离」）：
// nil 与只读成员一律拒绝；管理员可管理任意模板；记账员/普通成员仅管理自己创建的。
func CanManageRule(u *model.User, rule *model.RecurringRule) bool {
	if u == nil || rule == nil {
		return false
	}
	if u.Role == model.RoleAdmin {
		return true
	}
	if !CanManageRuleCreate(u) {
		return false
	}
	return u.ID == rule.CreatedBy
}

// CanView 四档角色均可查看全部数据。
func CanView(u *model.User) bool {
	return u != nil && u.ID > 0
}

// CanCreateTransaction 记账权限：管理员/记账员/普通成员可记，只读不可。
func CanCreateTransaction(u *model.User) bool {
	if u == nil {
		return false
	}
	return u.Role == model.RoleAdmin || u.Role == model.RoleRecorder || u.Role == model.RoleMember
}

// CanModifyTransaction 流水改删：管理员与记账员动全部；普通成员仅动自己创建的；只读不可。
func CanModifyTransaction(u *model.User, createdByID int64) bool {
	if u == nil {
		return false
	}
	switch u.Role {
	case model.RoleAdmin, model.RoleRecorder:
		return true
	case model.RoleMember:
		return u.ID == createdByID
	default:
		return false
	}
}

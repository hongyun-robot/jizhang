# Proposal

## Why

周期记账模板目前仅管理员可创建与管理（路由、服务、界面三重门禁），但记账员与普通成员同样有房租、订阅、水电等每月固定扣款要记，只能找管理员代建模板，绕且与「普通成员可记账」的授权矩阵不符。目标：每个能记账的人自助设置自己的每月定期扣款模板，各管各的。

## What Changes

- 放开周期模板管理权限：**记账员、普通成员**可创建、修改、启用/暂停、删除**自己创建**的模板；管理员可查看并管理**全部**模板；只读成员不可写（非授权请求仍返回 403）。**BREAKING**：`GET/POST/PUT/PATCH/DELETE /api/v1/recurring` 的授权语义从「仅管理员」变为「按角色 + 创建者归属」，只读成员对写接口仍 403，但 `GET /recurring` 对其开放（只返回自己名下模板）。
- 列表按属主隔离：非管理员仅看到自己创建的模板；管理员看到全部，并能识别每条模板的创建人。
- 生成逻辑不变：到期流水仍由服务端 cron + 打开应用懒补齐幂等生成，`created_by` 取模板创建者（现状已如此），生成流水的改删权限沿用流水写权限矩阵，无需改动。
- 前端开放入口：`/recurring` 路由与「我的」页入口对记账员、普通成员可见；模板列表对管理员展示创建人。

## Capabilities

### New Capabilities

（无）

### Modified Capabilities

- `recurring-entries`: 「周期记账模板」要求中「模板管理仅管理员可操作」改为按角色与创建者归属授权（记账员/普通成员管自己的，管理员管全部），列表按属主隔离，越权操作 403。
- `identity/access-control`: 「设置类操作仅限管理员」的适用范围移除「周期记账模板管理」（模板管理改由 `recurring-entries` 的归属规则约束），其余设置类操作维持仅管理员。

## Impact

- 后端：`internal/router/router.go`（`/recurring` 门禁调整）、`internal/service/permission.go`（新增模板授权判断）、`internal/service/recurring_service.go`（List/Create/Update/SetStatus/Delete 按 actor + 归属）、`internal/handler/recurring_handler.go`（透传 actor）、`internal/repository/recurring_repo.go`（按创建者过滤的列表查询）；对应单测 `recurring_service_test.go`、`permission_test.go`。
- 前端：`App.tsx` 路由、`Mine.tsx` 入口显隐、`components/RequireRole.tsx`（支持多角色）、`pages/Recurring.tsx`（创建人展示）、`api/hooks.ts`。
- 无需数据库迁移：`recurring_rules.created_by` 字段与既有数据已具备属主信息。
- 测试：`frontend/e2e/recurring.mjs` 扩展成员自助建模板与越权 403 场景。
- 规格：`recurring-entries`、`identity/access-control` 两处 delta（与 `add-ledger-system` 变更的同名能力 delta 兼容，需注意归档顺序）。

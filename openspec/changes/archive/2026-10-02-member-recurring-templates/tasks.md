# Tasks

## 1. 后端：授权矩阵与服务层

- [x] 1.1 在 `internal/service/permission.go` 新增 `CanManageRule(actor, rule)` 归属授权（viewer/nil → false、管理员 → true、其他角色 → `actor.ID == rule.CreatedBy`），并在 `permission_test.go` 补表驱动用例覆盖各分支，验证 `go test ./internal/service -run TestCanManageRule` 全绿
- [x] 1.2 仓储与列表作用域：`recurring_repo.go` 新增按创建者查询；`RecurringService` 依赖注入 `UserRepo`（`RuleDeps` 与 `main.go` 接线）；`List(actor)` 改为管理员全量、其他角色按 `created_by` 过滤，并填充 `created_by_name`，验证新增测试「普通成员仅见自己模板、管理员见全部且带创建人姓名、只读成员返回自己的空列表」通过（`go test ./internal/service -run TestRecurringListScope`）
- [x] 1.3 写操作归属校验：`Create` 改用创建资格判断（admin/recorder/member，替换 `CanManageSettings`），`Update`/`SetStatus`/`Delete` 增加 actor 参数并对非己模板返回 `ErrRuleForbidden`，验证新增测试覆盖「只读成员创建 → ErrRuleForbidden」「普通成员改他人模板 → ErrRuleForbidden 且模板不变」「记账员删自己模板 → 成功且流水保留」「管理员暂停他人模板 → 成功」，且既有 `TestRecurring*` 用例全部通过（`go test ./internal/service`）
- [x] 1.4 `recurring_handler.go` 的 List/Update/SetStatus/Delete 透传 `middleware.CurrentUser(c)`，列表响应在规则字段上附 `created_by_name`，验证 `go build ./... && go vet ./...` 通过

## 2. 后端：路由门禁

- [x] 2.1 `internal/router/router.go` 调整 `/recurring` 分组：`GET` 移除 `RequireRoles(model.RoleAdmin)`（仅保留组级 Auth），`POST`/`PUT`/`PATCH`/`DELETE` 改为 `RequireRoles(model.RoleAdmin, model.RoleRecorder, model.RoleMember)`，`POST /recurring/ensure` 维持任意登录角色，验证 `go build ./...` 通过；接口级门禁行为（viewer 写 403、member 读 200）由任务 4.1 的 e2e 断言确认，中间件语义已由既有 `middleware/auth_test.go` 覆盖

## 3. 前端：入口、路由与页面

- [x] 3.1 `components/RequireRole.tsx` 增加可选 `roles?: Role[]`（保留原 `role` prop 以兼容其他页面），验证 `pnpm build`（含 `tsc --noEmit`）通过且现有单角色路由行为不变
- [x] 3.2 提取「可记账角色」共享常量并用于 `App.tsx` 的 `/recurring` 路由与 `Mine.tsx` 的「周期记账」入口显隐（admin/recorder/member 可见，viewer 不见且直连被重定向回首页），验证 `pnpm build` 通过，入口显隐由任务 4.2 e2e 断言确认
- [x] 3.3 `Recurring.tsx` 管理员视角列表显示每条模板的创建人（`created_by_name`），页面说明文案改为「仅显示你自己创建的模板；到期流水由服务端生成」；`api/types.ts` 的 `RecurringRule` 增加 `created_by_name` 字段，验证 `pnpm build` 通过，创建人可见性由任务 4.2 e2e 断言确认

## 4. 端到端与回归验证

- [x] 4.1 重写 `frontend/e2e/recurring.mjs` 权限断言：删除旧「模板管理仅管理员（member 直达被拦、member/viewer 读模板 403）」步骤，新增「普通成员自助创建月度扣款模板并生成带『自动』标记的流水」「普通成员改他人模板 403」「只读成员写接口 403、GET 列表 200 且为空」「管理员看到全部模板并显示创建人」断言与截图，验证启动 dev server + 后端后 `node e2e/recurring.mjs` 全部 PASS（含既有生成/幂等/封账/暂停场景回归）
- [x] 4.2 全量回归：`go test ./...`、`pnpm build`、既有 e2e `users.mjs` 与 `walkthrough.mjs`，验证各命令退出码为 0
- [x] 4.3 归档顺序确认：运行 `openspec validate "member-recurring-templates"`，确认两条 INFO（MODIFIED 需先有主规格）已按 design D7 处理——`add-ledger-system` 先归档生成 `recurring-entries` 与 `identity/access-control` 主规格，再归档本变更，验证 validate 输出 `valid: true` 且归档顺序已记录在案

# Design

## Context

- 授权现状：`internal/service/permission.go` 是可执行的授权矩阵（四档角色 admin/recorder/member/viewer）；周期模板管理在三处被锁为仅管理员——`router.go` 的 `RequireRoles(model.RoleAdmin)`、`RecurringService.Create` 的 `CanManageSettings` 检查、前端 `App.tsx` 路由与 `Mine.tsx` 入口显隐。
- 数据现状：`recurring_rules.created_by` 自始存在，历史模板的创建者都是管理员；`TransactionService.CreateFromRule` 已按 `rule.CreatedBy` 落流水创建人，生成流水的改删走既有 `CanModifyTransaction`（普通成员仅动自己的），因此生成侧与流水侧无需改动。
- 生成链路与属主无关：cron `EnsureDue` 与懒补齐 `EnsureDueLazy` 经 `ListDueBefore` 拉取**全部**到期模板逐条生成，按 `(rule_id, entry_date)` 唯一约束幂等；授权只影响模板管理面，不影响生成面。
- 前端现状：`RequireRole` 只支持单角色；`Recurring.tsx` 列表已渲染 `created_by` 所在的完整模型，但不展示创建人。
- 规格现状：主规格 `openspec/specs/` 尚未同步（`add-ledger-system` 变更未归档），`recurring-entries` 与 `identity/access-control` 的需求文本以该变更的 delta 为基线。动机见 proposal.md - Why。

## Goals / Non-Goals

**Goals:**

- 记账员、普通成员自助创建与管理**自己**的周期扣款模板；管理员看全部、管全部。
- 服务端按「角色 + 创建者归属」双层授权（粗粒度路由、细粒度服务层），非授权 403；前端入口按同一矩阵显隐。
- 保持生成语义完全不变：到期生成、幂等、封账跳过、暂停补齐、生成流水独立改删。

**Non-Goals:**

- 不做模板共享/协作编辑（除管理员代管任何人的模板外）。
- 不改流水写权限矩阵与统计、提醒等周边能力。
- 不为只读成员开放任何模板写操作。
- 不引入数据库迁移或模板属主转移（换人）功能。

## Decisions

### D1 授权判定集中在 permission.go

新增 `CanManageRuleCreate(u *model.User) bool`（= 既有的 `CanCreateTransaction`：admin/recorder/member）与 `CanManageRule(u *model.User, rule *model.RecurringRule) bool`（创建者为否 → false；admin → true；否则 `u.ID == rule.CreatedBy`）。服务层所有写方法在执行前调用，拒绝返回既有 `ErrRuleForbidden`（handler 映射 403）。

- 备选：在 service 内联角色判断——会被多处复制且偏离「矩阵只写一处」的项目约定，否。
- 备选：全部交给 `RequireRoles` 中间件——无法表达「按归属」的细粒度，否。

### D2 路由门禁：读放开、写粗拦

`/recurring` 组已挂 `middleware.Auth`：

- `GET /recurring`：移除 `RequireRoles(admin)`，仅要求登录（四档角色可读，符合「只读角色的区别仅在写操作上」）。
- `POST/PUT/PATCH/DELETE`：改为 `RequireRoles(admin, recorder, member)`，viewer 在中间件层直接 403；归属校验在服务层（D1）。`POST /recurring/ensure` 维持任意登录角色不变。
- 与 `permission.go` 头注释「路由中间件做粗粒度门禁，写操作前必须调用细粒度判断」一致，满足「API 与界面双重执行」。

### D3 列表作用域：管理员全量，其余按创建者过滤

`RecurringService.List(actor)`：admin → 现有全量查询；其他角色 → `created_by = actor.ID`（viewer 结果天然为空，无需特判）。生成链路的 `ListDueBefore` **不**按属主过滤——生成必须处理所有人的模板。

- 备选：非管理员可见全部模板但只读——与用户决策「每人管理自己的」相悖，且诱导误操作他人模板，否。
- 只读成员列表返回自己的（空）而非 403：避免把「查看」变成写式门禁，与访问控制规格的查看条款一致。

### D4 创建人标识随列表返回

`GET /recurring` 响应在规则字段上附加 `created_by_name`（admin 视角用于区分谁建的）。实现：列表返回后经 `UserRepo.List()` 一次建 ID→用户名映射填充，避免 N+1。

- 备选：前端调用户管理 API 自行拼接——该 API 仅管理员可用且前端 join 脆弱，否。
- 备选：模型加 gorm 关联（eager load）——改动面大于一次内存映射，否。

### D5 前端门禁向后兼容地扩到多角色

`RequireRole` 增加可选 `roles?: Role[]`（保留原 `role` prop，其他页面零改动）；`/recurring` 路由与 `Mine.tsx` 入口改用 `['admin', 'recorder', 'member']`。判定与后端 `CanCreateTransaction` 一一对应，入口常量提取复用，避免两处手写列表漂移。

### D6 生成与流水行为不动

`CreateFromRule`、幂等约束、封账跳过、`EnsureDueLazy` 提醒等全部保持现状；生成流水 `created_by` = 模板创建者，普通成员因此能改删**自己**模板生成的流水，管理员/记账员可改全部——现有流水写矩阵已覆盖，无规格与代码改动。

### D7 规格兼容与归档顺序

本变更的两份 delta 以 `add-ledger-system` 的同名需求为基线做 MODIFIED。该变更先归档/同步后，本变更的 MODIFIED 才有主规格可套用；若本变更先归档，需在归档时人工确认基线顺序。文本上两份 delta 已互斥衔接（access-control 移除「周期记账模板管理」，recurring-entries 接管其规则），不会产生重复要求。

## Risks / Trade-offs

- [成员看不到管理员代建的全局模板，流水页却出现「自动」流水，可能困惑] → 周期记账页加文案说明「仅显示你创建的模板；他人代建的模板由管理员维护」；接受该取舍（换取清晰的属主边界）。
- [`add-ledger-system` 未归档导致 MODIFIED 暂无主规格基线，`openspec validate`/归档顺序敏感] → 实施与归档时先同步 `add-ledger-system`；本 delta 已逐字沿用其需求标题与原文，套用时按 D7 衔接。
- [写权限从管理员扩到三档角色，共享账本可能被周期性写入] → 与手动记账完全同级的 `CanCreateTransaction` 门禁；封账 `guardOpen`、金额/账户/分类校验照旧；越权与 viewer 403 均有单测覆盖。
- [前端入口显隐与后端矩阵漂移] → 常量集中在一处；即使漏配，服务端 403 仍是最终防线（spec「API 与界面双重执行」）。
- [删除他人模板的破坏半径（生成流水保留但后续停更）] → 归属校验 403 在删除前执行；删除确认弹窗保留现有文案。

## Migration Plan

1. 无数据库迁移，`created_by` 数据已就绪；旧模板创建者=管理员，权限放开后管理员仍全见全管，行为向后兼容。
2. 部署顺序：先后端（新语义对管理员调用完全兼容）再前端；期间旧前端 + 新后端、新前端 + 旧后端均可正常工作（旧前端入口仍 admin-only，旧后端仍是 admin-only，新前端对非 admin 的写请求在旧后端会 403，符合预期失败）。
3. 回滚：直接回滚代码即可，无数据清理；期间非管理员创建的模板在回滚后由管理员继续可见可管（创建者字段仍在）。

## Open Questions

（无——角色范围与管理员视角已在提案阶段确认：记账员+普通成员可自建、管理员看全部管全部。）

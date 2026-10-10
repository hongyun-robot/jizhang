# Design

## Context

动机见 proposal.md。与本设计相关的现状：

- `transactions.kind` 为 `ENUM('expense','income','transfer')`，金额恒正，方向由 kind 表达（`backend/migrations/000001_init.up.sql`、`backend/internal/model/transaction.go`）。
- 余额由流水聚合推导（`repository/account_repo.go` 的 CASE SQL），统计与预算同样按 kind 分 CASE（`stats_repo.go`、`budget_repo.go`）——没有冗余余额字段需要同步。
- 无任何关联流水字段（无 `related_txn_id`）、无券/预付实体、无导入管线。
- 写操作统一经过 `transaction_service`：权限矩阵（`permission.go`）、封账锁 `guardOpen`、叶子分类校验、写钩子（统计缓存失效 + 预算重估）。
- 分类占比为 ECharts 饼图，无法绘制负值。

## Goals / Non-Goals

**Goals:**

- 退款作为一等流水类型贯通余额、统计、预算、导出、UI 的既有链路，改动模式与现有三态同构。
- 关联、部分退款、删除约束在服务端强制执行，不依赖前端。
- 迁移无破坏性：存量流水行为完全不变。

**Non-Goals:**

- 券/预付卡实体（有效期、核销）——明确排除，见 proposal。
- 导入与平台自动识别退款。
- 权责发生制（按核销/使用期记账）。

## Decisions

### D1: 第 4 个 kind 而非 income+关联标记（已由 grilling Q2 定案）

`kind='refund'`，金额恒正，余额公式 `+amount`。

- **备选 B（income + `related_txn_id`）**：改动小，但收入虚增、支出与预算不回吐——被否，报表会说谎。
- **备选 C（改原支出）**：破坏审计、撞封账锁——被否。
- 落点：DB ENUM 迁移 + `model.TxnKind` 常量 + `transaction_service` 校验（kind 分支）+ `account_repo`/`stats_repo`/`budget_repo` 各 CASE 增加 `WHEN 'refund'` 分支（余额 `+amount`；统计/预算 `−amount`）。所有 CASE 都是同构扩展。

### D2: `related_txn_id` 可空外键，完整性在服务层强制

新增可空列 `related_txn_id BIGINT NULL` + 索引，指向 `transactions(id)`，**不加数据库 FK 级联**（本仓 migrations 未用 FK 级联，且删除约束本就要走服务层）。

服务层规则（`transaction_service`）：

- 仅 `kind='expense'` 可作为关联目标；退款创建/修改时校验目标存在且为支出。
- 删除支出前检查 `EXISTS(related_txn_id = 该笔)`，存在则拒绝（grilling Q9=A）。
- 删除退款正常走既有删除流程，删除后关联自然解除。
- 金额超原支出**只在前端**黄条提示（Q6=A），服务端不校验累计——避免并发下为对账逻辑引入锁。

### D3: 继承预填仅是创建时的默认值，不派生存储

关联创建时后端按 `related_txn_id` 预填分类与账户，用户可改（Q7/Q10）。退款一旦落库，其 `category_id`/`account_id` 是独立字段，与原支出**无持续派生关系**：

- 统计/预算只读退款自身的分类——避免查询时 join 原支出。
- 原支出的"已退 ¥x"展示由列表查询按 `SUM(amount) GROUP BY related_txn_id` 聚合得出，不落冗余列。
- 无关联退款 `category_id` 可空（复用转账可空的既有形态），但 `account_id` 必填。

### D4: 账期归属只认 `entry_date`，无跨期特殊逻辑

退款复用既有 `entry_date → period_key` 推导（Q3）。原支出所在账期已封账不阻止新退款：封账锁 `guardOpen` **只按退款自身 entry_date 判断**，退款不修改原流水，因此无需对原流水做任何写操作。跨月错位（原月支出偏高、退款月出现负支出）是接受的固有代价，已在 specs 显式声明。

### D5: 统计/预算负值的展示分层处理

- **饼图**（Q11=A）：前端过滤 `net <= 0` 的分类不渲染，图例/排行列表如实显示负值；后端聚合接口返回全部净额（含负值），**口径过滤放在前端图表层**，导出与列表不丢数据。
- **趋势/环比/月度汇总**：支出直接用 `SUM(expense) − SUM(refund)`，天然可为负，折线图支持负值。
- **预算**（Q12=C）：后端返回净消耗（可为负），前端检测 `consumption < 0` 时渲染"本月已退回 ¥x"文案替代进度条（进度条不渲染负宽度）。超支提醒仅在净消耗 > 预算时触发——负消耗不会误触发。
- 首页结余复用统计口径，不新增逻辑。

### D6: 两个 UI 入口共享同一表单与提交路径（Q8=C）

- `TxnFormModal` 的 Segmented 增加"退款"项（仅有流水写权限的角色可见），新增 `related_txn_id` 可选字段。
- `Records.tsx` 支出行的"退款"操作 → 打开同一 Modal 并预填 `kind=refund`、原分类/账户/关联 id、日期默认今天、金额空（用户填）。
- 提交复用 `POST /transactions`，`txnReq` 增加可选 `related_txn_id`；无新端点，权限/封账/缓存钩子零新增。

### D7: 统计缓存与导出

- 写钩子（`OnWrite`/`OnWritePeriod`）已按流水写操作触发缓存失效与预算重估，退款走同一链路，无新失效点。
- CSV 导出：类型列输出"退款"标记，金额恒正（与列表一致），沿用既有注入转义。

## Risks / Trade-offs

- [ENUM 迁移中 MySQL 对 ENUM 加值需 online DDL] → `ALTER TABLE ... MODIFY kind ENUM(...,'refund')` 仅追加值，Instant/INPLACE 可行；迁移前备份，回滚只需还原 ENUM 定义（新数据需先清理）。
- [跨月退款导致原月统计偏高、退款月出现负支出，用户可能误解] → specs 与 UI 明确"退款按到账日"，趋势负值如实展示；这是现金流口径的固有代价。
- [超额退款仅前端提示，API 直调可绕过产生"累计已退 > 原支出"] → 已定案允许（Q6=A），数据语义仍自洽（钱确实多退了），仅在原支出"已退"标记上如实显示超额。
- [D5 口径过滤在前端，导出与图表口径需保持一致] → 后端始终返回全量净额，各端各自选择子集；specs 以"图不画负、列表如实"为准。
- [退款分类留空导致分类占比出现"未分类"口径] → 排行列表可显示为"（未分类）"，与转账无分类的处理一致。

## Migration Plan

1. 后端迁移：追加 ENUM 值 + `related_txn_id` 列与索引（向前兼容，可回滚）。
2. 后端代码：模型/校验/各 CASE/DTO 与导出（与迁移同版本发布）。
3. 前端：类型、表单、行内入口、统计与预算展示。
4. 无数据订正：存量流水不含 refund/关联，行为不变。

## Open Questions

- 退款行在流水列表的视觉样式（符号、标签颜色）为纯展示细节，实现时按现有 kind 标签体系顺手决定，不影响 specs 与任务拆分。

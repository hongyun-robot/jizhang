# add-ccb-gold-accounts 验证记录

日期：2026-10-15，北京时间。对应 spec-driven 提案、7 份 delta 规格及实施清单。

## 汇总

| 维度 | 结果 |
|---|---|
| 完整性 | 38/38 实施任务；7 个能力、26 条 ADDED/MODIFIED requirement 均有实现依据 |
| 正确性 | 核对 delta 的 88 个场景（包含保留的既有场景）；财务、权限、封账、报价、缺价及交互有单元/原生 MySQL/浏览器证据 |
| 一致性 | 独立黄金子类和事件、真实现金转账、匿名双边报价、小时共享缓存、不可变报价观察、空值估值及登录 scope 与设计一致 |

本次无 REMOVED/RENAMED requirement。没有修改现金/基金投资的既有口径，也未将黄金伪装为场外基金。

## 实际执行

| 检查 | 结果与范围 |
|---|---|
| `backend: go test ./...` | 通过；保留仓库既有按 DSN 显式启用原生集成的机制 |
| `backend: go vet ./...` | 通过 |
| `backend: go build ./cmd/server` | 通过 |
| `frontend: npm run typecheck` | 通过，包含测试 TypeScript 配置 |
| `frontend: npm run test:unit` | 26 文件、82 测试通过；其中黄金精度/换算/普通支付隔离 3 项 |
| `frontend: npm run build` | 通过；现有 session/stats 资源仍触发 500 kB 分包提示，不阻断构建 |
| 黄金 SQLite/纯计算与 CCB 离线测试 | 通过，包含默认日期跨日重试、严格精度、费用、清仓、无报价、版本修订、两小时告警、过期租约、后继/封账与投影恢复 |
| `GOLD_MYSQL_DSN` 原生黄金集成 | 显式运行 4 项且均通过，随机独立数据库测试后清理；未跳过 |
| `node e2e/gold.mjs` | 52 项断言通过，桌面与 360px；仅 `gold-browser` 临时账本和假行情 |
| `node e2e/funds.mjs` | 80 项断言通过，验证既有基金、现金费用、待结算、分红、独立后台更新及恢复路径 |
| `CCB_GOLD_LIVE_SMOKE=1` | 真实建行匿名公开报价只读通过；不创建持仓或执行银行交易 |
| `openspec validate add-ccb-gold-accounts --strict` | 通过 |
| `git diff --check` | 通过；Windows CRLF 提示不是内容错误 |

浏览器最终验证包含：无有效价录入期初且首页总资产标明不完整、按赎回价估值、历史缺价不回填、记账弹窗直接买入、响应丢失后相同键重试、费用不重复扣减、卖出/撤销/CSV、普通入口保护、越权拒绝、手动刷新保留草稿、报价失败留旧价、手机实际买入，以及成功回执延迟到另一个账号登录后不污染其持仓。

原生 MySQL 四项为：`TestGoldMySQLSharedWorkersAndClosingRace`、`TestGoldMySQLConcurrencyPrecisionAndRollbackGuard`、`TestGoldMySQLEmptyMigrationDown`、`TestGoldMySQLAccountLifecycleAndOrdering`。覆盖 worker 共用请求与重启、封账串行、并发幂等、八位克数、所有者外键、清仓、回滚拒绝/允许条件、账户生命周期及排序保留。

真实 smoke 于 2026-10-15 20:28 运行，返回客户买入 897.47、赎回 891.47 元/克，银行源时间 `2026-10-15T20:28:54.701+08:00`，仅为该次样本。发现官网对默认 Go UA 返回空 HTTP 200，客户端改用明确的公开查询标识后成功，并由离线请求测试校验该标识。日志只输出公开报价，不输出 Cookie 或银行个人资料。

## Requirement 到实现与验证的映射

代码位置以仓库根目录为起点；行号对应本次交付版本。

| 能力 / Requirement | 实现依据 | 验证依据 |
|---|---|---|
| gold-accounts / 本人建行积存金账户与产品身份 | `gold_service.go:38,488`、`ccb/client.go:103` | 多账号环境、MySQL 生命周期、客户端错产品/币种、浏览器越权 |
| gold-accounts / 期初克数和双方式成本录入 | `gold_calculation.go:56`、`gold_service.go:128`、`GoldForm.tsx:23` | 双模式换算、无报价保存、期初更正依赖、精度及溢出测试 |
| gold-accounts / 真实已完成买入及同日现金联动 | `gold_service.go:128`、`gold_calculation.go:117` | 首买/追加、一次扣款、跨日拒绝、浏览器弹窗买入 |
| gold-accounts / 真实卖出和比例成本结转 | `gold_calculation.go:117`、`gold_service.go:128` | 比例成本、净到账与费用、超卖、MySQL 清仓尾差 |
| gold-accounts / 赎回价估值与独立投资盈亏 | `gold_service.go:406,432` | 893.04 赎回价、成本/持仓/已实现计算、清仓零值和费用解释 |
| gold-accounts / 建行双边报价身份与时间校验 | `ccb/client.go:76,103`、`gold_sync.go:77` | 匿名会话恢复、业务/币种/产品/时间/正价、超时/大小/重定向、旧价/修订、真实 smoke |
| gold-accounts / 每小时自动查询和共享缓存 | `gold_sync.go:77,191`、`cmd/server/main.go` | 同小时不重复、MySQL 多 worker 和重启、GET 不查银行 |
| gold-accounts / 买卖页面手动刷新与草稿保护 | `gold_sync.go:71`、`GoldQuotePanel.tsx` | 30 秒冷却、小时内刷新、并发租约、浏览器保持 4500 元真实净到账 |
| gold-accounts / 报价故障保留与完整性展示 | `gold_sync.go:27`、`GoldQuotePanel.tsx`、`Dashboard.tsx:186` | 两小时边界、首次缺价、worker 未运行、旧价保留、成功旧源价状态 |
| gold-accounts / 财务事件幂等和末笔撤销 | `gold_service.go:101,128,298,580` | MySQL 并发、回执丢失、早期撤销拒绝、现金归属、历史补录、投影失败恢复 |
| gold-accounts / 黄金管理和统一买卖表单 | `pages/Gold.tsx`、`TxnFormModal.tsx:366`、`GoldForm.tsx` | 桌面/手机完整流程、8 位输入、中文日期、失败草稿及换号迟到回执 |
| accounts / 流水关联账户 | `gold_service.go:128`、`account_repo.go:159` | 一笔黄金受管理现金转账，估值覆盖本金净额 |
| accounts / 余额与总资产计算 | `model/account.go`、`account_repo.go:159`、`stats_service.go:546` | JSON 空余额、首页不完整、当前曲线与持仓、基金原回归 |
| accounts / 账户删除约束 | `account_repo.go:205`、`account_service.go` | 原生 MySQL 及 SQLite 清仓/撤销仍有历史，停用可卖出但禁止新买入 |
| accounts / 现金与基金资产子类（扩展黄金） | `model/account.go`、`account_service.go`、迁移 000017 | 零现金期初、主动建户、多账户及约束验证 |
| accounts / 本人账户目录统一拖动排序 | 既有目录排序结合 `Accounts.tsx` 黄金账户；账户写入保留 sort | 既有排序单元回归及 MySQL 新黄金追加、改名启停保留排序 |
| accounts / 黄金账户与普通支付隔离 | `transaction_service.go:146,167`、`recurring_service.go`、`domain/accounts.ts`、现金选择器 | 黄金普通转账拒绝、支付选项单元测试、原基金浏览器回归及 IsCash 边界检查 |
| transactions / 黄金事件管理的资金转账 | `gold_service.go:128,298`、`transaction_service.go:376,458`、`txn_handler.go:34` | 普通删除拒绝、专用撤销恢复、关联字段禁止伪造、费用一次计算 |
| transactions / 黄金资金记录展示导航与导出 | `txn_export.go:63`、`labels.ts`、`RecordActions.tsx` | 本人流水、消费影响零及 CSV 黄金用途断言 |
| statistics / 资产走势 | `gold_service.go:514,580,630`、`stats_service.go:546` | 历史克数、当日观察、缺价空点、不可倒算、日终引用源时间 |
| statistics / 统计查询缓存 | `GoldService.OnWrite`、`StatsService.InvalidateAll`、`api/cache.ts` | 浏览器刷新后首页/账户同步、缓存读取不请求上游、个人 scope |
| statistics / 投资口径不进入日常收支 | 黄金均为 transfer、`Transaction.Effects` | CSV expense_effect=0、费用核算、现有统计/基金回归 |
| home-dashboard / 快捷记账入口 | `TxnFormModal.tsx:366`、`GoldForm.tsx` | 首页记一笔直接买入并保存、失败与重试、卖出共用表单 |
| home-dashboard / 首页黄金资产与估值完整性 | `Dashboard.tsx:186`、账户空余额 | 初始缺价首页“报价不完整”、有效报价后估值 |
| identity/access-control / 个人黄金配置与资金权限分离 | `gold_service.go:38,128,298`、认证路由 | viewer 合法期初、资金 403、管理员/成员跨号 403、迟到响应换号隔离 |
| billing-periods / 黄金财务事件与报价重估的封账边界 | `gold_service.go:57,63,128,298`、`LedgerWrite` | 直接封账、闭锁后继、无交易的已封账结转状态、MySQL 真实锁竞争；报价独立成功 |

黄金 service 路径为 `backend/internal/service/`，账户仓储为 `backend/internal/repository/`，前端组件分别在 `frontend/src/pages/`、`components/`、`features/gold/`、`api/`。完整场景以七份 delta 为准，表格不替代原规格。

## 设计与交付边界

- 独立 `asset_class=gold`、固定建行产品键；未复用基金的 `.OF` 日净值约束，未重构既有投资引擎。
- 财务事件在统一 `LedgerWrite` 内提交并与封账串行，报价网络请求在事务之外；失败的派生历史以持久化标记恢复，不推翻财务回执。
- 报价观察保留源时间和获取时间；成功重复、同时间修订和源时间倒退分别处理，历史估值引用观察并可展示银行时间。
- 未添加用户银行登录、真实交易下单、其他银行、跨日结算或历史价格猜测；这些属于提案的非目标。
- 新迁移仅用于随机隔离库验证；真实开发和生产财务库没有执行迁移，未部署应用。使用新版本前需按 `docs/ccb-gold-migration.md` 备份并执行迁移。
- 工作区原有负债改动保留；仅在共享路由、README 和负债现金选择器作必要的黄金接入增补，没有替换其未提交实现。

## 结论与限制

要求的实施及验证没有未解决的阻断项。可进入独立的归档或部署步骤，本轮未执行归档、提交或发布。

官网内部接口长期可用性无法由一次 smoke 保证，这是设计已记录的外部依赖；现有缓存、小时查询、手动刷新和故障状态负责处理失败。浏览器手机验收为 360px 模拟，不等同 iOS 实机测试；本变更未要求真实银行交易或实机操作。其他原生集成保留既有按各自 DSN 运行/跳过机制，本次显式黄金 MySQL 四项已实际执行。

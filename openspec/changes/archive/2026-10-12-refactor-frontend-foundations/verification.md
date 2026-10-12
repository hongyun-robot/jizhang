# Verification Report: refactor-frontend-foundations

复验日期：2026-10-12；schema：spec-driven。本轮在首次发现的三个问题上完成修复，并重新核查实现、测试及对应规划。

## Summary scorecard

| Dimension | Status |
| --- | --- |
| Completeness | 原 47 项与追加的 3 项修复全部完成；五份 delta 的 14 条要求、55 个场景均有实现及证据映射 |
| Correctness | 已执行检查中无未修复的 CRITICAL/WARNING；旧 SQL/开发账本端到端仍 Not verified（历史测试环境未提供），不将其计为通过 |
| Coherence | 分域 API、会话、权限、领域模块、页面拆分、后台任务、懒加载及主题符合设计；草稿、重试及侧栏状态的时序缺口已补齐 |

报告是验证证据，apply 的 all_done 状态不代替行为检查。旧环境未验证是报告范围限制，不另行创造归档前提；归档遵循用户此前“没问题就归档”的授权。

## 首次发现的问题及修复

| 问题 | 原复现 | 修复与复验 |
| --- | --- | --- |
| C1 / P1：旧提交关闭新草稿 | 记账/周期提交等待响应时取消并新建，旧成功关闭新弹窗 | createEditorSession + useEditorSession 按每次打开、对象、会话和卸载隔离反馈；旧请求仍更新账务缓存。两种表单各覆盖旧成功/旧失败，新草稿保持，正常新提交仍关闭 |
| W1 / P1：分期重试载荷漂移 | 费用 0 改 1 已提交但响应丢失，后台重取后未改值重试删掉 fee_amount，返回 409 | LiabilityDialogs 以 requestKey 为实例边界保留编辑基线；计划 diff、操作模式和已注册字段稳定。调整与退款核对重放的端点、完整 payload、key/version 均相同，返回原 operation_id，不增加流水/操作/欠款；改值仍 409，恢复原值可重放 |
| W2 / P2：侧栏错误变持续加载 | 桌面流水页侧栏 stats/compare 返回 500 后仍显示读取中 | 收支与预算分别呈现真实错误/重试；已有数据后台失败标明更新失败并保留旧值。两类查询的首次错误、恢复、后台失败与恢复均通过 |

当前已知问题计数：0 CRITICAL、0 WARNING、0 SUGGESTION。三个问题均是原承诺的验收遗漏，未误称为已经证明由页面拆分新引入的回归。

主要实现：frontend/src/lib/editorSession.ts、hooks/useEditorSession.ts、components/TxnFormModal.tsx、features/recurring/RecurringFormModal.tsx、components/LiabilityDialogs.tsx、features/liabilities/LiabilityDetail.tsx、components/AppShell.tsx。计划编辑基线与当前服务器缓存各司其职，不将客户端重试当作新资金操作。

## 本轮实际执行

- npm run typecheck、npm run build：通过。
- Vitest：20 文件、61 用例通过，新增编辑会话生命周期测试。
- refactor_repairs.mjs：记账/周期各两种迟到结果及正常保存；侧栏收支/预算首次错误、重试、后台失败保留与恢复，全部通过。
- refactor_plan_retry.mjs：调整、退款核对、改值冲突三类完整请求重试通过，实际操作和流水没有重复。
- transaction_platforms、vouchers、voucher_retry、refactor_interactions、refactor_errors：通过；逐页错误覆盖 13 个目标。
- liabilities.mjs：完整 53 项通过，覆盖金额、还款、退款、核对、分期、结清/撤销、个人归属、提醒和窄屏，无运行错误。
- 根路径基础回归、预算/个人草稿、键盘和加载检查结果补充在 acceptance.md。之前的全阶段 Go/MySQL、根/子路径和图像证据仍保留，没有把未复跑的检查改写为新跑通过。
- 新回归使用独立内存账本及 miniredis；普通流水/模板/预算在 finally 清理，不能经业务接口删除的管理流水/计划随 fixture 关闭销毁。真实数据库没有写入。

## 要求与场景证据

| 能力（要求/场景） | 实现与证据 |
| --- | --- |
| budgets（1/5） | Budgets 与 draft 分离；单元、foundations/drafts：后台保留、服务器回填、确认/取消切换，迟到 key/revision 保护 |
| identity/user-auth（2/11） | coordinator/client/session：单飞、迟到 401、一次重试、载荷/key、403、退出/换号；单元及 foundations/navigation/background |
| identity/access-control（3/10） | permissions、路由和行操作；四角色/本人/他人/受管理记录，viewer 入口/API 403，目录例外及 liabilities 个人归属 |
| transactions（3/13） | filters、PeriodPicker、导出与导航：日期、URL、POP/REPLACE、防抖、关联退款 CSV/BOM、定位、分页及清理 |
| ui-shell（5/16） | RequireAuth/returnTarget、QueryState、cache、草稿、RouteContent：登录/完整地址、各页及侧栏错误、资金更新、异步草稿、根/子路径资源失败恢复；新增两个修复脚本补时序证据 |

55 是规划场景数量，并非 55 个新浏览器用例。原有五份 delta 没有 REMOVED/RENAMED，也没有声明跳过的 artifact；本轮保留规范范围。

九项设计决策与代码模式复核：模块职责、传输与查询键、会话代次、权限导航、整数分/分类/日期、流程拆分、后台/共享视口、加载与主题、测试隔离均符合设计。没有把服务端分配、权限或幂等裁决搬到前端；严格类型及仓库命名/缩进约定通过。

## 运行限制与最终评估

已执行检查未发现 critical/warning/suggestion。旧 refunds/budgets/recurring/notifications/users 及 interaction_* SQL/开发脚本仍未验证，依赖历史账号或硬编码 MySQL 客户端/库；不宣称通过。真实手机键盘未执行，自动证据为 visualViewport 模拟；生产部署不在此任务范围。

修复与复验已完成。五份主规范已同步，变更已归档，结果记录在 acceptance.md；不通过换 key、删除真实资金数据或跳过已知失败来获得通过。

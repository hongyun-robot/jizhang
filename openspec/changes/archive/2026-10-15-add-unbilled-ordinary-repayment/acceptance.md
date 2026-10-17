# 未出账普通消费提前还款验收

验收日期：2026-10-15。变更：`add-unbilled-ordinary-repayment`。

## 实现结果

现有「记录还款」增加「还已出账／提前还未出账」范围选择，默认仍为已出账。普通提前还款支持已发生消费的部分或全额偿付，不限制自然月份；可按到期日自动分配或手动指定多份未出账账单。服务端提供只读预览并在统一资金事务内重新核验，前端修改字段后立即使旧预览失效；过时响应不能重新启用保存。

本金只生成一笔资产到负债的转账，固定关联本次真实消费。正常出账、补记消费、改约定还款日和合法移账不挪用原资金；撤销按原权限及还款自身封账规则恢复对应资金和欠款。超额、无目标资金、分期混入、非法日期、跨账号、失效预览和破坏固定目标的修改整体拒绝。

初始可还提示不依赖负债详情中可能为默认零值的 `balance_effect`，而按消费用途与本金债项计算，服务端始终独立重算；新增前端测试覆盖该返回形态。分期未来本金和费用继续走原分期入口。旧请求省略范围、旧请求摘要和同请求重试保持兼容。

## 验证结果

| 检查 | 结果 |
| --- | --- |
| `backend`: `go test ./... -timeout 90s` | 通过，含新增普通提前还款服务测试 |
| `backend`: `go vet ./...` | 通过 |
| `backend`: `go build ./cmd/server` | 通过 |
| 设置本机 `LIABILITY_MYSQL_DSN`，运行 `go test ./internal/service -run '^TestLiabilityMySQL$' -count=1 -v -timeout 120s` | 通过；使用随机独立数据库并自动清理，新增 `ordinary-advance-concurrency` 验证真实 SQL 持久化、同请求并发重试、不同请求竞争、出账竞争、重载及注入失败回滚 |
| `frontend`: `npm run test:unit` | 25 个测试文件、79 项测试通过 |
| `frontend`: `npm run typecheck` | 业务与测试源码类型检查通过 |
| `frontend`: `npm run build` | 通过；存在既有的大分包提示 |
| `node e2e/unbilled_ordinary_repayment.mjs` | 47 项检查通过：1440×1000、474×884、390×844；含改值及迟到预览失效、HTTP 非法输入、响应丢失后原请求重试、出账、撤销、多账单、混合分期及四档角色 |
| `node e2e/installment_early_repayment.mjs` | 32 项既有分期回归通过 |
| `node e2e/liabilities.mjs` | 54 项既有负债回归通过 |
| `openspec validate add-unbilled-ordinary-repayment --strict` | 通过 |
| `git diff --check` | 通过 |

浏览器最终验证使用固定的生产构建与分别重新启动的本机临时负债 fixture（`127.0.0.1:18185`，预览 `127.0.0.1:5612`）。脚本先验证测试服务身份，未对生产账本写入。响应丢失测试等待预期网络错误显示后再重试，确认同一笔转账只写入一次。

## 规范与测试对应

| 规范关注点 | 对应证据 |
| --- | --- |
| 约定到期日与实际提前支付、默认已出账范围 | `TestOrdinaryAdvanceLifecycle`、`TestOrdinaryAdvanceLegacyDigestAndScope`，浏览器范围与旧请求检查 |
| 普通可还本金、日期排序、混合分期、购券本金、跨月份及手动多账单 | `TestOrdinaryAdvanceTargetsAndDates`、`TestOrdinaryAdvanceVoucherAndTransferCandidates`、`TestOrdinaryAdvanceManualBillsAndRules`，新增浏览器脚本末段多账单检查 |
| 日期、角色、本人账户及封账 | `TestOrdinaryAdvanceAuthorizationAndClosedPeriods`、前端容量单测、浏览器非法日期及 member／recorder／viewer 检查 |
| 只读预览、金额变化、并发、失败原子性与请求重试 | `TestOrdinaryAdvanceStaleAndAtomic`、MySQL `ordinary-advance-concurrency`、浏览器迟到响应与故意丢失响应检查 |
| 固定消费目标、正常出账、补记消费、退款、改删、转分期与撤销 | `TestOrdinaryAdvanceLifecycle`、`TestOrdinaryAdvanceMutationAndRefundProtection`、`TestOrdinaryAdvanceManualBillsAndRules`，浏览器完整生命周期及既有负债／分期回归 |

## 截图

截图已保存在本变更目录，分别记录范围／可还金额及确认后的分配预览。手机视口验证了内容滚动、日期和选择面板、底部保存按钮可达及页面无横向溢出。

- 桌面：[范围与可还金额](screenshots/unbilled-ordinary-repayment-scope-1440.png)、[核对后的分配](screenshots/unbilled-ordinary-repayment-1440.png)。
- 474px 手机：[范围与可还金额](screenshots/unbilled-ordinary-repayment-scope-474.png)、[核对后的分配](screenshots/unbilled-ordinary-repayment-474.png)。
- 390px 手机：[范围与可还金额](screenshots/unbilled-ordinary-repayment-scope-390.png)、[核对后的分配](screenshots/unbilled-ordinary-repayment-390.png)。

## 发布与回滚

实施验收完成时尚未部署或归档；后续归档及生产发布结果见 [发布记录](deployment.md)。未新增数据库表或列，也未改写历史资金。新提前还款使用现有 credit 的 `ordinary_advance` 模式、targets JSON 和操作结果 JSON。

发布须备份并配套更新前后端，避免旧后端忽略新范围。产生新模式记录后，不可直接回退到不认识该模式的旧后端；应保留兼容后端修复。不能把模式改成 auto、清空目标或自动撤销真实还款作为回滚。详细策略见 `design.md` 和 README 的「普通消费提前还款」。

手机验收为浏览器视口模拟，未进行实体手机系统键盘验收。

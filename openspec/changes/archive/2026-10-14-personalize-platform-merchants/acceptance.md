# 实施与验收记录

日期：2026-10-14。变更：`personalize-platform-merchants`。

“我的 → 我的记账选项”已增加“平台 / 商家管理”。全部有效登录角色（含只读成员）只能维护自己的名称，支持搜索、新增、重命名及确认删除。管理员不能操作他人条目；不同账号同名具有独立 ID。管理页、购券、手工流水及分期表单使用同一份本人目录。

目录重命名保留历史券与流水文字；本人保留记录的引用阻止删除，软删除券及历史首尾 Unicode 空白也纳入检查。目录维护不改变资金写权限或账务；事务内名称补入与业务一起成功或回滚。保存期间取消、继续编辑、换对象和换账号后，旧响应不会关闭新草稿或污染新会话。

## 数据库

新增成对迁移 `000015_personal_voucher_platforms`，包括 owner 外键、账号内名称唯一索引、默认模板、一次初始化标记、旧目录快照与来源映射。已有账号得到旧共享目录的独立副本，后续新账号仅初始化九个稳定默认名称。迁移前后财务身份、文字与金额保持；私人修改、模板变化或新账号初始化后拒绝自动 down。

验证使用本机 MySQL / Redis 及随机独立 MySQL 数据库；浏览器只使用本机内存账本和临时 Redis。未对真实账本执行迁移或写入验收数据。

## 检查结果

| 检查 | 结果 |
| --- | --- |
| `backend: go test ./...` | 通过，已设置券、负债、个人目录及基金 MySQL DSN，包含原生迁移与资金行为回归 |
| `backend: go vet ./...` | 通过 |
| `backend: go build ./cmd/server` | 通过 |
| 个人平台 MySQL 迁移、并发及回滚测试 | 通过：空库、旧名称副本、原创建人不同、软删除与空白文字引用、并发初始化 / 同名创建、删除与记账竞争、故障回滚、受保护 down |
| `frontend: npm run test:unit` | 23 个文件、73 个测试通过 |
| `frontend: npm run typecheck` | 通过，业务与测试源码均检查 |
| `frontend: npm run build` | 通过，新增管理页独立懒加载；现有 session / Stats 包仍有超过 500 kB 的构建提示 |
| `platform_management.mjs` | 通过：四角色、同名隔离、API / 界面越权拒绝、引用保护、五种宽度、明暗主题、长名称、键盘、错误态、草稿、取消和换号迟到响应 |
| `voucher_platforms.mjs` | 通过：本人目录、选择 / 创建、取消持久化、直接保存、编辑 / 清空、响应丢失重试与迟到响应 |
| `transaction_platforms.mjs` | 通过：本人商家选择、新增、保存、编辑、搜索、CSV 和手机清空，金额影响保持 |
| `personal_catalogs.mjs` | 通过：四角色个人 CRUD、跨账号拒绝、账户创建幂等、只读资金权限及私人商家隔离 |
| `mobile_selection_lifecycle.mjs` | 通过：读取失败重试、断点切换、父层关闭、草稿 / 焦点与键盘监听清理 |
| `liability_platform_totals.mjs` | 通过：1440px / 532px / 390px 本人选择器、创建期间阻止父保存、迟到响应、分期精确金额与费用归属 |
| OpenSpec strict 校验及 `git diff --check` | 通过 |

## 截图

- [桌面 1280px](../../../../frontend/e2e/shots/refactor-frontend/platform-management-1280.png)
- [手机 360px](../../../../frontend/e2e/shots/refactor-frontend/platform-management-360.png)
- [手机 767px 暗色](../../../../frontend/e2e/shots/refactor-frontend/platform-management-767.png)
- [平板 768px](../../../../frontend/e2e/shots/refactor-frontend/platform-management-768.png)
- [手机 390px 暗色](../../../../frontend/e2e/shots/refactor-frontend/platform-management-390.png)

截图位于已忽略的验收产物目录。部署说明见 `docs/personal-platform-migration.md`；需求 delta 已同步至四份主规范，变更于 2026-10-14 完成归档，19/19 项任务全部完成。

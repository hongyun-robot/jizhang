# XLSX 流水导出验收

日期：2026-10-29。本机 Windows、Go 1.26.2、Node 24.15.0、MySQL97 与 redis6380。

## 已实现

- 流水页默认导出 Excel，格式菜单保留 CSV；桌面、390px 手机、键盘操作可达，选择格式后菜单关闭，加载期间两种操作共用防重。
- XLSX 为单张「流水明细」表，原 13 列加 7 个追溯列；日期/金额为原生可计算类型，ID/产品代码为文字，冻结首行并筛选全部范围；空表保留 20 列表头。
- 与列表共享全部生效筛选和本人排序，移除分页；本人范围、历史关联遮蔽、必要关联失败、退款/券/投资及理财零钱口径均有验证。
- XLSX 最多 100,000 条，100,001 条返回 413；不截断、不自动切换格式。原 CSV 13 列、BOM、默认接口与无新增行数上限保持兼容。
- 同一只读数据库视图读取明细及关联；完成文件后才发送，错误返回 JSON，临时资源按成功/失败/取消清理。下载错误在认证处理前解码，换号迟到响应不下载旧会话文件。
- 无数据库迁移、无账务写入、无行情采集、无后台导出任务。此记录保存实施阶段结果，归档和发布在后续操作中执行。

## 通过的检查

| 检查 | 结果 |
| --- | --- |
| Go CSV/XLSX 服务和 HTTP 测试 | 12 项通过；独立覆盖类型、原文、金额、空表、筛选、顺序、券、投资、权限、取消、存储失败、格式及错误响应 |
| `TestXLSXMySQLSnapshotAndLimit`，设置真实 MySQL DSN | 通过，使用随机独立数据库并清理；并发修改期间账户名称保持快照一致，后续查询可见新值 |
| `go vet ./...` | 通过 |
| `go build ./cmd/server` | 通过 |
| `npm run typecheck` | 通过 |
| `npm run test:unit` | 31 个文件、111 项通过，含 Blob 错误、认证重试/终止、换号及下载客户端测试 |
| `npm run build` | 通过，存在既有大分包提示 |
| `node e2e/export.mjs` | 通过，使用本机独立券 fixture 和生产构建预览 |
| `openspec validate add-transaction-xlsx-export --strict` | 通过 |

浏览器覆盖 45 条测试流水、跨页/手机未全加载、两种格式、组合筛选、含 CRLF/逗号/引号的 CSV、空结果、只读成员、未登录拒绝、模拟 413、失败重试和连续触发防重；导出前后余额、流水数量不变，结束清理本次创建的记录并核对余额恢复。10 万条上限真实判断由 Go/MySQL 测试覆盖，浏览器的 413 是响应模拟。浏览器使用触屏尺寸模拟，没有代替实体 iOS/Android 验收。

截图：

- [桌面](../../../../frontend/e2e/shots/refactor-frontend/export-desktop.png)
- [390px 手机](../../../../frontend/e2e/shots/refactor-frontend/export-mobile.png)

截图和测试下载位于仓库已忽略的 E2E 产物目录，不纳入源文件。它们是临时测试账本数据。

## 十万条规模结果

单独 MySQL 验证的一次成功记录：

```text
100000 rows: elapsed=2.8700498s file_bytes=6643692
peak_heap_bytes=220299472 allocated_bytes=967178112
--- PASS: TestXLSXMySQLSnapshotAndLimit (22.45s)
```

`elapsed` 只统计服务查询、投影和工作簿生成，约 2.87 秒；测试总时长还包含迁移、造数、工作簿回读和 CSV 兼容检查。峰值堆约 210 MiB，为测试进程采样值，不能当作任意并发或生产机器的内存保证。成功工作簿有 100,000 条数据及 1 行表头；新增第 100,001 条后，在元数据补全和编码前返回超限错误，CSV 仍含全部 100,001 条。

## 全量 Go 测试的既有失败

已设置个人目录、券、负债、基金、黄金和理财 MySQL DSN，执行 `go test ./... -count=1 -timeout 10m`。新 XLSX/MySQL 测试实际执行；其余包通过，`internal/service` 因以下三个既有顶层测试失败，全量命令退出码为 1：

| 测试 | 原因 |
| --- | --- |
| `TestPersonalLiabilityAccountsAndIndependentRules` | `personal_liability_test.go:45`：账单规则尚未配置，现有服务拒绝启用非零余额账单管理 |
| `TestPersonalPlatformMySQLConcurrency` | 旧 fixture 的 transactions 表缺少 `gold_event_id` |
| `TestVoucherMySQL` | 旧 fixture 缺少 `gold_event_id`，并有既有 down 保护与后续 vouchers 表缺失失败 |

使用 Go `-overlay` 将本次改动的已有 Go 文件替换为 HEAD `fa833d9` 的源码，同时通过构建约束排除本次新增 Go 文件，在相同 MySQL 环境重跑上述三个测试；全部复现相同失败。基线复跑未修改工作区文件，也未触及真实账本。由此确认这些失败并非本次导出实现引入；本变更不将全量 Go 测试标记为通过。

重现入口：

```powershell
# 已在当前进程私有配置好测试 DSN；不要将密码写进命令记录或文档。
go test ./internal/service -run '^TestPersonalLiabilityAccountsAndIndependentRules$|^TestPersonalPlatformMySQLConcurrency$|^TestVoucherMySQL$' -count=1 -v
```

## 使用与发布

使用及复测命令见仓库 README 的「流水导出」。API 不指定格式仍是 CSV，前端显式请求 `format=xlsx`；先发布后端，再发布前端。无需财务数据迁移；回滚先恢复前端 CSV 入口，再回滚后端。

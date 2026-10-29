## Why

流水页目前只能下载 CSV，缺少原生 Excel 的日期、数值类型和固定的对账追溯字段。用户需要按当前筛选下载可直接计算、筛选和追溯来源的流水明细，同时继续兼容现有 CSV 使用方式。

## What Changes

- 在流水页现有导出入口增加默认 XLSX 下载和显式 CSV 选择，桌面、移动端均可用。
- XLSX 使用一张「流水明细」工作表，保留现有 13 列并追加流水 ID、关联原流水 ID、所属账期、投资类型、产品代码、关联记录类型和关联记录 ID；日期为日期单元格，金额为可计算数值，冻结表头并开启筛选。
- XLSX 使用与列表相同的全部生效筛选和本人展示顺序，忽略分页；最多导出 100,000 条数据，超限明确提示缩小范围或选择 CSV，不截断结果。
- 保留退款、券及投资资金流水现有金额和消费影响口径；按本人权限验证追加关联信息，历史跨账号关联继续遮蔽。无记录时生成带表头的空工作表，导出不修改账务或触发行情同步。
- CSV 保持现有 13 列、列顺序、UTF-8 BOM、金额语义及不指定格式时的 API 默认行为，不追加追溯列、不施加新的 XLSX 行数上限。
- 本次不包含汇总工作表、持仓或其他模块导出、PDF、导入恢复、列配置、后台任务或导出历史。

## Capabilities

### New Capabilities

无。

### Modified Capabilities

- `transactions`：新增 XLSX 流水明细、追溯字段、格式选择、10 万条边界和失败反馈要求；明确原 CSV 接口与列结构兼容约束。

## Impact

- 后端：`backend/internal/handler/txn_handler.go` 的既有导出接口增加显式格式参数；`service/txn_export.go`、流水服务及仓储增加受限读取、本人范围追溯投影和 XLSX 生成；保留 CSV 路径。
- 前端：`frontend/src/pages/Records.tsx` 和 `frontend/src/api/transactions.ts` 增加格式选择、正确 MIME/文件名与下载状态；复用 `useRecordsState` 的导出筛选参数。共享 `frontend/src/api/client.ts` 在错误规范化前解码 Blob 中的 JSON，保留认证刷新和终止会话行为。
- 依赖：拟新增后端 `github.com/xuri/excelize/v2`，用于带类型和格式的 XLSX 输出；不新增前端工作簿生成依赖。
- 验证：扩展 Go 导出/权限/投资关联测试及 `frontend/e2e/export.mjs`，覆盖 CSV 回归、XLSX 内容、10 万条边界和移动端下载。
- 数据与部署：不新增表、迁移、存储服务或角色权限；实现后需要按普通后端依赖及前端资源变更构建发布。本轮仅生成规划文档。

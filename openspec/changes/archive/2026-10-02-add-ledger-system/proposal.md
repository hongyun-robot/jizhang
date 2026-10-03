# Proposal

## Why

自用家庭记账没有合手的工具：市面产品要么面向单人、要么塞满广告和理财推销，且无法满足"多人共享一本账 + 自定义账期 + 预算/统计"的核心诉求。本变更从零构建一个自用记账站点（Web + 移动端 H5 同一代码库），由 Go 后端 + MySQL/Redis 支撑，部署在自有云服务器上，通过 `https://zhanghongyun.cn` 对家人开放。

## What Changes

- 新建 monorepo 双端项目：`frontend/`（React 19 + Vite + TS + Ant Design 6 + ECharts，一套响应式代码，移动优先）与 `backend/`（Go + Gin + GORM + golang-migrate + zap + robfig/cron），前后端分开打包部署。
- 建立单账本共享数据模型：**资金账户**（资产/负债双类型，含信用卡负余额与转账）、**两级自定义分类**（收支各一套）、**流水**（支出/收入/转账三态，CNY `DECIMAL(12,2)`）。
- 建立身份与权限体系：关闭注册、管理员手动建号；**四档角色**（管理员 / 记账员 / 普通成员 / 只读）及其资源操作矩阵；JWT 双 token 认证（refresh 落 Redis，登录失败限流）。
- 引入**自定义账期**：账本级结算日（如每月 26 日），本期按钮、预算、月度汇总、封账均按账期计算；封账硬锁该期流水，管理员可解封并留审计日志。
- 新增功能页：首页概览、流水列表（自由日期区间 + 组合搜索 + 分页 + CSV 导出）、统计页（分类占比/收支趋势/环比/月度汇总/资产走势五图）、预算（总 + 分类）、周期记账（服务端 cron 生成 + 打开页面懒补齐）、站内提醒（超支/临近还款日/周期账单）。
- 新增 `ui-shell`：移动底部 Tab / 桌面侧栏导航与暗色模式。
- 配套运维：宿主 nginx 路径分流（`/` 出前端 dist、`/api/v1` 反代 Go）、Docker Compose（api + mysql + redis）、每日 mysqldump 本机 30 天 + rclone 加密推 OSS 备份。
- 分三期交付：一期记账主流程，二期账期/预算/周期/封账/提醒，三期暗色模式等锦上添花项。

## Capabilities

### New Capabilities

- `identity/user-auth`: 登录认证 — JWT 双 token、refresh token 存 Redis、登录失败限流、停用账号拒绝登录。
- `identity/user-management`: 用户管理 — 管理员手动建号、四档角色分配、软停用（历史流水保留）、不提供物理删除。
- `identity/access-control`: 授权矩阵 — 四档角色对各资源的操作规则（记账员改删全部流水、普通成员仅改删自己创建的、只读零写权限、设置类仅管理员）。
- `accounts`: 资金账户 — 资产/负债双类型、期初余额、账户间转账（不计收支）、账户余额与总资产计算。
- `categories`: 分类体系 — 账本级自定义两级分类、收支各一套、新账本种子默认集、删除约束。
- `transactions`: 记账与流水 — 支出/收入/转账三态 CRUD、流水列表（自由日期区间、备注关键字/金额区间/分类/账户组合搜索、offset 分页）、当前筛选结果导出 CSV（UTF-8 BOM）。
- `billing-periods`: 账期与封账 — 结算日账期定义、流水归期规则、改结算日不改历史归档、封账硬锁、管理员解封 + 操作日志。
- `budgets`: 预算 — 账期维度月总预算 + 分类预算、进度展示、超支标红与超支事件。
- `recurring-entries`: 周期记账 — 固定金额/频率模板、服务端 cron 按期生成、打开页面懒补齐、生成笔可单独修改不影响模板。
- `statistics`: 统计分析 — 自定义区间下分类占比、按天/周/月收支趋势、本期环比、月度（账期）汇总、资产余额走势，查询结果 Redis 短缓存。
- `home-dashboard`: 首页概览 — 本期收支结余、预算进度、最近流水、快捷记账入口。
- `notifications`: 站内提醒 — 预算超支、临近还款/账单日、周期记账待处理的铃铛红点与列表。
- `ui-shell`: 界面骨架 — 一套响应式布局（移动底部 Tab / 桌面侧栏）、暗色模式（跟随系统 + 手动切换）。

### Modified Capabilities

（无 — 本项目无既有 spec，全部为新能力。）

## Impact

- **代码**：全新 `frontend/`、`backend/` 目录，无既有代码受影响。
- **API**：新建 REST `/api/v1/*`，统一 `{code, message, data}` 响应包装。
- **依赖**：前端 React/Vite/AntD/ECharts；后端 Gin/GORM/golang-migrate/validator/zap/robfig/cron；运行时 MySQL 8 + Redis 7。
- **系统**：宿主 nginx 新增 location 配置；新增 Docker Compose 栈；新增每日备份 cron + rclone；管理后台复用同一前端（路由守卫按角色渲染）。

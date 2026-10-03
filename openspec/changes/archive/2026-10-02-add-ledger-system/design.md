# Design

## Context

全新项目（无既有代码），动机与范围见 `proposal.md`。约束来自已达成共识的设计树：

- 单域名部署：宿主 nginx（已有 HTTPS 证书）分流 —— **站点走独立子域名 `jizhang.zhanghongyun.cn`**（复用通配证书，宿主 nginx 终止 TLS 后反代本机容器端口），`/` 出前端 dist、`/api/v1` 反代 Go 服务；前后端分开打包。（曾评估在主域名下挂 `/jizhang` 子路径，已改为子域名，见 D10；子路径变体的配置清单保留在 `docker/nginx.conf` 注释里。）
- 单账本全家共享数据模型，四档角色（见 `specs/identity/access-control`）。
- 自定义账期（结算日）是预算、汇总、封账的统一时间口径（见 `specs/billing-periods`）。
- 自用规模：用户数 < 20，流水量 < 数万条/年，无高并发诉求，但**账务数字必须算得准**。
- 运维人力 = 1（用户本人），方案必须一次配好后长期免维护。

## Goals / Non-Goals

**Goals:**

- 前后端分离的清晰分层，账务核心逻辑（余额、归期、预算消耗）可单测。
- 数据一致性优先于性能：所有余额/统计由流水单一事实来源推导。
- 部署与备份自动化，日常操作只有 `docker compose up -d` 和上传 dist。

**Non-Goals:**

- 不做高可用、分库分表、读写分离（数据量远达不到）。
- 不做多租户/多账本（明确砍掉，见 proposal）。
- 不做移动端原生 App / 小程序 / PWA。
- 不做开放注册、OAuth、短信验证码。

## Decisions

### D1. 后端分层与目录

`backend/` 按 `router → middleware → handler → service → repository → model` 四层分层：

- **handler**：参数绑定 + validator 校验 + 组装响应，不含业务逻辑。
- **service**：全部业务规则（权限判断、账务计算、归期），是单测的主战场。
- **repository**：GORM 数据访问，接口定义在 service 层，便于测试时 mock。
- 中间件：`auth`（JWT 校验 + 加载用户）、`role`（角色门禁）、`rate-limit`（登录限流）。

**替代方案**：按 feature 垂直分包（`modules/transaction/...`）。放弃：自用规模下垂直分包收益小，水平分层更符合多数人阅读直觉。

### D2. 认证：双 token，refresh 落 HttpOnly Cookie + Redis

- access token：JWT，30 分钟，**存前端内存**（刷新页面时用 refresh 换新）。
- refresh token：随机不透明串，30 天，`Set-Cookie`（HttpOnly、SameSite=Strict、Path=可配置的 `COOKIE_PATH`，默认 `/api/v1/auth`），服务端存 Redis（`refresh:{token} → userID`，TTL 即有效期）。
- 刷新即轮换：新 refresh 签发后旧值从 Redis 删除，检测到已删除则视为盗用，**吊销该用户全部会话**。
- 停用用户：token 校验中间件每次查用户状态（users 表主键查询，数据量下无成本），停用即 401。
- 登录限流：Redis INCR 计数 `login_fail:{username}` / `login_fail:{ip}`，5 次封 10 分钟。

**为什么 Cookie 而非 localStorage**：同域部署使 HttpOnly Cookie 可用，refresh token 不可被 JS 读取；SameSite=Strict 同时挡 CSRF（写接口均为 JSON POST/PUT/DELETE，无简单表单）。access token 放内存则避免持久化泄露面。**替代方案**（双 token 都放 localStorage）更简单但 XSS 即失守，此处安全收益明显大于成本。

### D3. 金额与精度

- DB：`DECIMAL(12,2)`，货币仅 CNY。
- Go：`shopspring/decimal` 贯穿 service 层，禁止 `float64` 参与账务计算。
- API：金额序列化为**字符串**（`"25.50"`），前端用字符串展示与校验，杜绝 JS 浮点误差。

### D4. 数据模型核心（9 张表）

```
users            id, username(uniq), password_hash(bcrypt), role(enum), status(active/disabled), created_at
accounts         id, name, type(asset/liability), opening_balance DECIMAL(12,2), repayment_day(可空,信用卡还款日1-28), status(active/disabled), sort
categories       id, parent_id(可空,二级), kind(income/expense), name, sort
transactions     id, kind(expense/income/transfer), amount DECIMAL(12,2),
                 account_id, transfer_account_id(转账对方), category_id, entry_date DATE,
                 period_key CHAR(7)  -- 归期物化，见 D5
                 note, created_by, source(recurring标记), created_at, updated_at
budgets          id, period_key, category_id(NULL=总预算), amount
recurring_rules  id, amount, freq(daily/weekly/monthly/yearly), account_id, category_id,
                 note, start_date, next_run_date, status(enabled/paused), created_by
period_closings  period_key(uniq), status(closed), closed_by, closed_at   -- 无记录=未封
notifications    id, kind(budget_overdue/repayment_due/recurring_patched), payload JSON,
                 read_at, created_at
audit_logs       id, user_id, action(close_period/open_period/...), detail JSON, created_at
```

索引重点：`transactions(entry_date, period_key)`、`transactions(account_id)`、`transactions(category_id)`。

**余额不冗余**：账户余额 = 期初余额 + `SUM(流水净额)` 实时聚合（自用量级毫秒级），杜绝"余额字段与流水不一致"这类最伤信任的 bug。

### D5. 账期：归期物化为 `period_key`，封账独立表

- 每笔流水**写入时**按当时结算日计算 `period_key`（格式 `YYYY-MM`，指账期归属键，如结算日 26 日时 2026-09-26～10-25 记为 `2026-10`）并固化在行上。
- 这天然实现"改结算日不改历史"（spec 硬要求）：历史行的 period_key 不回溯改写，只有新写入的流水用新规则。
- 预算消耗、月度汇总、封账校验全部按 `period_key` 聚合，SQL 简单且走索引。
- 封账不锁全表，只在写接口校验 `period_closings` 是否存在该 period_key（主键查询）。
- 未配置结算日 = 结算日 1 日（自然月），归期公式一致。

**替代方案**（查询时实时推算归期）：SQL 复杂且改结算日会改写历史口径，放弃。

### D6. 权限执行：中间件粗粒度 + service 细粒度

- `role` 中间件挡路由级权限（设置类接口 `admin only`）。
- 流水增删改在 service 内做**属主判断**：普通成员仅 `created_by == userID` 可改删；记账员/管理员放行。
- 前端路由守卫 + 菜单显隐仅为体验层，与后端校验独立实现（spec 要求双重执行）。

### D7. 定时任务：进程内 cron + 懒补齐双保险

- `robfig/cron` 随 Go 进程启动：每 5 分钟扫描 `recurring_rules.next_run_date` 生成流水（事务内幂等：以 `(rule_id, entry_date)` 唯一约束防重——按**归属日**每期一笔，若按 `period_key` 唯一则每日/每周频率同月只能生成一笔）；每日 08:00 评估还款日与预算生成提醒。
- 前端应用启动时调 `POST /api/v1/recurring/ensure`，服务端补跑同一幂等生成逻辑，兜底 cron 失效。
- 生成的流水 `source=recurring` 单笔可改删，不回写模板。

### D8. 统计与缓存

- 全部统计为聚合 SQL（无预计算表），结果缓存 Redis：key = 路由 + 参数哈希，TTL 30 秒；任何写操作主动 `DEL` 当前用户相关统计 key（自用下前缀统一易清除）。
- 五图对应五个端点：`/stats/category`、`/stats/trend`、`/stats/compare`、`/stats/summary`、`/stats/assets`，均接受 `start/end` 自由区间参数（与流水列表共用同一 query 参数语义）。

### D9. 前端架构

- React 19 + Vite + TS，**React Router v7**（路由守卫按 role），**TanStack Query** 管服务端状态（统计缓存、列表），**Zustand** 管 auth/主题/全局 UI（轻量）。
- axios 拦截器：401 → 自动 `/auth/refresh`（带 Cookie）→ 重放原请求，仅失败一次则跳登录。
- AntD 6 `ConfigProvider` 主题：`theme.darkAlgorithm` 切换，`localStorage` 持久化 + `prefers-color-scheme` 跟随。
- 响应式：CSS 断点 + AntD Grid；<768px 渲染底部 Tab（记一笔/流水/统计/我的），≥768px 渲染侧栏，同一路由树两套壳组件。
- 区间选择器做成共享组件（流水页、统计页、对比共用），状态放 URL query，刷新/分享不丢。
- 开发期 Vite dev server proxy `/api` → `localhost:8080`，后端 CORS 白名单仅 `http://localhost:5600`（本机 5173 被 Windows 端口保留区间 5141-5240 占用，开发端口定为 5600）。

### D10. 部署与运维

- **访问地址：独立子域名 `jizhang.zhanghongyun.cn`（根路径）**，复用服务器已有通配证书，与主站 `zhanghongyun.cn` 及其他子域互不冲突。宿主 nginx 终止 TLS 后反代本机 `127.0.0.1:8084`（容器只监听回环，不直接暴露公网）。
- **前缀能力保留为可配置**：`frontend/Dockerfile` 的 `ARG VITE_BASE`（默认 `/`）→ Vite `base` → 静态资源前缀 / react-router `basename` / `API_BASE`（`frontend/src/base.ts` 派生），后端 `COOKIE_PATH`（默认 `/api/v1/auth`）。**若改挂子路径，这两处 + 两份 nginx 必须同步改**，否则 refresh cookie 的 Path 与浏览器实际路径不匹配 → 刷新必挂。子路径变体的完整配置清单见 `docker/nginx.conf` 末尾注释。
- `docker-compose.yml`：`api`（多阶段构建，编译期 `go:embed` 不用，前端由 nginx 出）、`mysql:8`（挂 volume + 初始化 SQL）、`redis:7`（`--appendonly yes`）。迁移由容器启动时执行 `golang-migrate up`。
- 容器内 nginx（`frontend/nginx.conf`）出 dist SPA 并反代 `/api/v1/` 到同网络 `api:8080`；Go 侧路由始终是 `/api/v1`，不感知部署形态。挂子路径时用带 URI 的 `proxy_pass http://api:8080/api/` 剥前缀，后端同样零改动。
- 宿主 nginx：子域名 server 块（80 → 301 https，443 反代回环端口），样例见 `docker/nginx.conf`。
- 初始管理员：首次启动读 env `ADMIN_USERNAME/ADMIN_PASSWORD` 幂等创建。
- 配置全走 env（`.env` 不进 git），OSS 凭据走 rclone 配置文件（`~/.config/rclone/rclone.conf`，不进 git）。
- 备份：每日 03:00 cron → `mysqldump | gzip` 本机留 30 天 → `rclone copy --crypt` 推 OSS。
- 日志：zap JSON 到 stdout，Docker 日志驱动限制 50m×3。

### D11. 测试策略

- **service 层单测**（Go，标准 `testing`）：归期计算、余额聚合、预算消耗、环比区间、周期幂等——D3/D5 的纯逻辑抽成可注入依赖的纯函数。
- repository 用接口 mock，不引 testcontainers（自用项目成本不值）。
- 前端不做单测，人工按 spec scenario 走查；E2E 不做。

## Risks / Trade-offs

- [**归期物化后无法"重新归档"**] 改结算日不动历史是 spec 要求，但若用户误改结算日，已写入的当期流水 period_key 不会跟着变 → 提供改结算日时的明确 UI 提示（"仅影响今日之后的流水"）；如需修复，管理员可批量重算工具（二期不做，先手工 SQL）。
- [**cron 单点**] Go 进程重启期间错过生成 → D7 懒补齐幂等兜底，唯一约束防重。
- [**余额实时聚合在数据量暴增后变慢**] 自用量级（<10 万行）SUM 走索引无感；真到那天再加余额快照表，接口语义不变。
- [**SameSite=Strict Cookie 在某些内网/嵌入场景异常**] 单域名正常访问无此问题；若出现，回退方案是双 token 进 localStorage（已记录在案）。
- [**备份失败无人知晓**] dump 脚本写退出码与日志，失败时在下次打开应用时由 `/api/v1/health` 暴露最近备份时间，首页角标提示（最小可行方案，不做邮件）。
- [**用户输入的备注/XSS**] React 默认转义 + AntD 输出，禁止 `dangerouslySetInnerHTML`；CSV 注入（`=cmd`）导出时对公式前缀单元格加 `'` 转义。

## Migration Plan

1. 本地开发环境：compose 起 mysql/redis → `migrate up` → 后端 seed（管理员 + 默认账户/分类）。
2. 服务器：上传 compose + `.env` → `docker compose up -d` → 配 nginx location → 上传 dist → 验证登录与记账主流程。
3. 配置备份 cron + rclone → 手动跑一次验证 OSS 有产出。
4. 回滚策略：镜像按 git tag 构建，回滚 = 换 tag 重新 up；DB 迁移带 `down` 脚本，但**上线前先备份**。

## Open Questions

- OSS 的 bucket/凭据（不阻塞任何设计，实现备份任务时填入 rclone 配置即可）。
- 还款日提醒的默认提前天数（spec 定 3 天），用户用起来若不合适，做成账户级可配置项即可，不影响架构。

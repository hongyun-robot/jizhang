# 记账（jizhang）

自用家庭记账站点：Web + 移动 H5 一套代码，Go + MySQL + Redis 后端，React + Ant Design 前端。

## 券

“我的 → 我的券”管理单次券和多次券；首页/流水页的“记一笔”也能登记购券。

“平台 / 商家”支持下拉选择和输入搜索。没有所需名称时，可在原输入框中确认“创建「名称」”，或直接输入并随券保存；新选项在整个账本内共享。点击创建后即使取消购券也会保留选项，仅输入或失焦不会创建。该字段可清空，名称去除首尾空白后最多 128 字，同名复用。

普通“记一笔”和“编辑流水”也提供可选的平台 / 商家字段，与购券共用选项目录。平台 / 商家独立保存，在流水列表和 CSV 中展示，也可作为搜索关键词；新增数据库迁移为 `000007_transaction_platform`。

平台 / 商家选项的读取和创建只要求登录，四个角色均可使用，包括只读成员。创建选项不产生券或账务记录；购券、编辑券及流水操作仍按原权限执行。

| 操作 | 账户余额 | 消费与预算 |
|---|---|---|
| 购券 | 按实付扣款 | 不计入 |
| 核销 | 不重复扣款 | 按核销日与实付分摊成本计入 |
| 退未用部分 | 回到原支付账户 | 不冲减已核销消费 |
| 作废 | 不重复扣款 | 剩余成本计入作废日 |

- 填写有效期时默认开启到期自动退款，也可关闭。有效期当天可用，按北京时间次日零点后五分钟内自动记录剩余退款；停机恢复或打开应用会补处理，仍按原计划日期记账。
- 自动退款按填写的配置记账，不连接平台执行退款或核实到账。平台实际没退时可撤销自动退款，系统同时暂停该券自动退款；确认后可手动退款或明确重新启用。
- 关闭自动退款的券过期后保留在待处理，用户选择退款、延长有效期或作废。未用金额按剩余实付成本单列，不并入账户总资产。
- 多次券前面各份按分向下取整，最后一份补齐差额。第一版退款只退全部未用份额；已消费份额退款、手续费和储值卡任意金额扣减不在券流程内。
- 撤销按操作倒序进行，保留撤销记录；已封账期间的事件需先解封。购买期封账不阻止开放新期核销/退款。券生成流水不能在普通流水页单独修改或删除。
- 所有成员可查看共享账本的券，只读成员不可操作；普通成员仅可编辑自己的购买信息、撤销自己的操作，管理员与记账员可管理全部。

发布券功能需备份并升级 `000005_vouchers`，先发布支持用途口径的后端，再配套前端。存在券数据时数据库 down 会拒绝执行，也不可仅回退到不识别券用途的旧应用；应保留当前 schema 修复，或恢复发布前备份及其匹配应用版本。

平台 / 商家下拉功能在其后升级 `000006_voucher_platforms`，自动从未删除券回填已有非空名称，先升级数据库和后端，再发布前端；原字符串接口继续可用。应用回退到仍支持券的上一版本时可保留新增目录表。数据库 down 仅删除目录表，执行前需导出目录，保留那些已创建但尚未用于券的选项。

## 券的集成验证

后端常规验证：在 `backend` 运行 `go test ./...` 与 `go vet ./...`。MySQL 验证设置 `VOUCHER_MYSQL_DSN` 后运行同一测试命令，账号需具备建库/删库权限；测试创建并清理随机命名的独立数据库，不复用 DSN 内的账本库。

浏览器验收使用独立的内存账本和临时 Redis，按三个 PowerShell 终端启动：

```powershell
# 终端一，在 backend
$env:VOUCHER_BROWSER_SERVER='1'
go test ./internal/router -run TestVoucherBrowserServer -v -timeout 25m

# 终端二，在 frontend
$env:VITE_API_PROXY='http://127.0.0.1:18084'
npm run dev -- --port 5601 --host 127.0.0.1

# 终端三，在 frontend
node e2e/vouchers.mjs
node e2e/voucher_retry.mjs
node e2e/voucher_platforms.mjs
node e2e/transaction_platforms.mjs
```

脚本默认使用本机 Playwright 的 Firefox，可用 `FIREFOX_EXE` 指定已安装的浏览器文件。它先校验测试服务标识，只在本地测试账本里写入，覆盖桌面/手机购券、核销、退款、作废、撤销、自动退款暂停、CSV 和只读权限。重试脚本还模拟“已保存但响应超时”，验证修改金额后重试不会重复扣款；同一次表单提交沿用原请求标识，需刷新核对后才重新发起操作。截图与下载产物放在已忽略的 `frontend/e2e/shots`、`frontend/e2e/downloads`。

平台 / 商家脚本通过普通成员表单验证选择、键盘创建、取消保留、直接保存、编辑、清空及手机弹层；同时验证四个角色目录接口、未登录拒绝、读取失败后提交、创建响应丢失后重试和迟到响应保护。MySQL 集成测试覆盖 000006 历史回填、精确名称去重、并发创建与财务事务回滚。

验收结束后调用 `Invoke-RestMethod -Method Post -Uri 'http://127.0.0.1:18084/__test__/stop'` 关闭测试后端，并停止测试 Vite 进程。

## 目录结构

```
backend/    Go 后端（Gin + GORM + golang-migrate），含 Dockerfile 与容器入口（自动迁移）
frontend/   前端（React 19 + Vite + TypeScript + Ant Design 6 + ECharts），含 Dockerfile 与容器内 nginx 配置
docker/     部署与备份脚本（宿主 nginx 样例、备份脚本）
openspec/   变更与规格文档
```

## 本地启动步骤

### 0. 前置

- Go >= 1.26、Node >= 20、Docker（含 compose 插件）

### 1. 启动依赖（MySQL 8 + Redis 7）

```bash
docker compose up -d
```

Docker 依赖的本机端口映射为 3307/6380，配置见根目录 `.env`（复制自 `.env.example`）。

使用 Windows 本机 Redis 时，在仓库根目录运行以下 PowerShell 脚本，并将 `.env` 的 `REDIS_ADDR` 设为 `127.0.0.1:6380`。脚本默认使用 `C:\Program Files\Redis\redis-server.exe`，可通过 `-RedisExecutable` 指定其他安装路径；数据库仍使用当前配置的本机 MySQL。

```powershell
.\scripts\start-local-redis.ps1
```

脚本会在后台启动 Windows Redis，并检查连接、会话写入及磁盘快照。AOF、快照和日志保存到已忽略的 `bin/redis-native`；再次执行会复用已运行的本机实例。更新 `.env` 后需重新启动本地后端。

### 2. 执行数据库迁移

```bash
cd backend
migrate -path migrations -database "mysql://root:devpass@tcp(127.0.0.1:3307)/jizhang" up
```

### 3. 启动后端（:8080）

```bash
cd backend
go run ./cmd/server
```

首次启动会幂等创建管理员与默认账户/分类种子（账号密码来自 `.env` 的 `ADMIN_USERNAME`/`ADMIN_PASSWORD`）。

### 4. 启动前端（:5600）

```bash
cd frontend
npm install
npm run dev
```

访问 `http://localhost:5600`（本机 5173 属 Windows 保留端口区间），`/api` 请求由 Vite 代理到 `:8080`。

### 5. 运行后端单测

```bash
cd backend
go test ./...
```

## 服务器从零部署（Docker 全栈）

前置：服务器已安装 Docker 与 compose 插件。站点挂在**独立子域名**（本项目用 `jizhang.zhanghongyun.cn`，复用已有通配证书）。

```bash
# 1. 配置环境（改掉 JWT_SECRET 与管理员口令）
cp .env.example .env
vi .env
#    服务器上还要设：HTTP_BIND=127.0.0.1（只监听回环，由宿主 nginx 反代）、
#    HTTP_PORT=8084、COOKIE_SECURE=true（HTTPS 下 refresh cookie 才会被浏览器接受）

# 2. 构建并拉起全栈（api 容器启动时自动执行 migrate，幂等）
docker compose --profile app up -d --build

# 3. 健康检查
curl http://127.0.0.1:8084/api/v1/health
```

浏览器打开 `https://jizhang.zhanghongyun.cn/`，用 `.env` 中的管理员账号登录并记一笔，即完成真实登录记账验证。

- `api`：Go 服务 + 启动时自动迁移（`backend/entrypoint.sh`），不映射宿主端口
- `web`：nginx 托管前端 dist 并反代 `/api/v1/`（`frontend/nginx.conf`），对外 `${HTTP_BIND}:${HTTP_PORT}`
- **宿主 nginx 必配**：子域名 server 块终止 TLS 后反代到 `127.0.0.1:8084`，样例见 `docker/nginx.conf`
- **监听面**：mysql/redis/web 默认绑 `127.0.0.1`（`DB_BIND`/`REDIS_BIND`/`HTTP_BIND`）。靠云安全组挡住是运气不是设计，纵深防御要在进程层就不听公网。
- **日志**：四个容器统一 `json-file` 50m×3，避免 zap 打到 stdout 后无限增长吃满磁盘（40G 盘必须限制）
- 换成挂子路径（如 `/jizhang/`）而非子域名：要同时改三处 —— `frontend/Dockerfile` 的 `ARG VITE_BASE`（派生静态资源前缀、router basename、`API_BASE`）、compose 的 `COOKIE_PATH`、以及两份 nginx 配置，详见 `docker/nginx.conf` 末尾变体注释

## 备份与恢复

```bash
# 手动/定时备份：产出 backups/jizhang-<时间戳>.sql.gz，保留 KEEP_DAYS 天（默认 30）
# 只本机不推送：  sh docker/backup.sh
# 本机 + 推 OSS： RCLONE_REMOTE="jizhang-oss-crypt:" sh docker/backup.sh
# 服务器 cron（已装）：0 3 * * * cd /srv/jizhang && RCLONE_REMOTE="jizhang-oss-crypt:" sh docker/backup.sh >> backups/backup.log 2>&1
# 推送状态由 /api/v1/health 暴露（push=ok|failed|skipped），失败下次打开首页可见

# 恢复演练：恢复进空库并核对行数
docker exec jizhang-mysql sh -c 'mysql -uroot -p"$MYSQL_ROOT_PASSWORD" -e "CREATE DATABASE IF NOT EXISTS jizhang_restore"'
gunzip -c backups/jizhang-<时间戳>.sql.gz | docker exec -i jizhang-mysql sh -c 'mysql -uroot -p"$MYSQL_ROOT_PASSWORD" jizhang_restore'
```

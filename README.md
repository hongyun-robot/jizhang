# 记账（jizhang）

自用家庭记账站点：Web + 移动 H5 一套代码，Go + MySQL + Redis 后端，React + Ant Design 前端。

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

本机端口映射 3307/6380（避开 WSL 原生 3306/6379），配置见根目录 `.env`（复制自 `.env.example`）。

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

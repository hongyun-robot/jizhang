---
name: start-dev
description: 启动本仓库的本地开发环境（Windows 本机 MySQL/Redis 服务、数据库迁移、Go 后端 :8080、Vite 前端 :5600）。当用户说"启动项目"、"启动开发环境"、"跑起来"、"start the dev stack" 时使用。
---

# 启动本地开发环境

本机实际运行方式是 Windows 原生服务 + 直接进程，README 的 docker compose 路径不适用于本机。`.env` 已配置好（DB `127.0.0.1:3306` root，Redis `127.0.0.1:6380`），只需按下面步骤拉起。

## 1. 启动依赖服务

`MySQL84` 和 `redis6380` 是 Manual 启动类型（用户要求保持手动，开机不自动加载）；已在运行时跳过本步。

1. 用 `Get-Service MySQL84, redis6380` 检查状态，两个都是 Running 则跳到第 2 步。
2. 启动服务需要管理员权限，普通 shell 会报 `cannot open service`。弹 UAC 提权启动，并提醒用户在屏幕上点"是"：

   ```powershell
   Start-Process powershell -Verb RunAs -ArgumentList '-NoProfile','-Command','Start-Service MySQL84, redis6380' -Wait
   ```

3. 完成标准：`Test-NetConnection 127.0.0.1 -Port 3306` 与 `-Port 6380` 均为 True。

坑（不要绕开服务另起进程）：Data 目录的 binlog 等文件是 SYSTEM 身份的服务创建的，ACL 只允许 SYSTEM/Administrators 写入，普通进程直接跑 `mysqld.exe` 必然报 `binlog.index ... Permission denied`。也不要改用 WSL 里的 MySQL——本机约定使用 Windows 服务。

## 2. 检查数据库迁移

`backend/migrations` 当前有 21 个迁移。对比版本后只在落后时升级：

```powershell
# 读密码（.env 是 GBK 编码，中文注释乱码不影响值）
$dbPass = ((Get-Content .env) -match '^DB_PASSWORD=') -replace '^DB_PASSWORD=',''

# 当前版本
& 'C:\Program Files\MySQL\MySQL Server 8.4\bin\mysql.exe' -h 127.0.0.1 -P 3306 -uroot "-p$dbPass" jizhang -e 'SELECT * FROM schema_migrations'

# 落后时升级（在 backend/ 执行）
migrate -path migrations -database "mysql://root:$dbPass@tcp(127.0.0.1:3306)/jizhang" up
```

完成标准：`schema_migrations` 的 version 等于迁移文件数且 dirty=0。

## 3. 启动后端与前端

端口被占用说明已在运行，直接复用，不重复启动：

- 后端：在 `backend/` 后台运行 `go run ./cmd/server`，监听 8080。
- 前端：在 `frontend/` 后台运行 `npm run dev`，监听 5600（Windows 保留 5173），`/api` 代理到 8080。

`FUYAO_API_KEY` 未配置不阻止启动，只是基金行情不可用。

## 4. 验收

1. `GET http://127.0.0.1:8080/api/v1/health` 返回 JSON 且 `data.status` 为 `ok`。
2. `GET http://127.0.0.1:5600` 返回 200。
3. 向用户报告访问地址 `http://localhost:5600`（登录用 `.env` 的 ADMIN_USERNAME/ADMIN_PASSWORD），并说明两个后台进程的 shell ID，便于后续停止。

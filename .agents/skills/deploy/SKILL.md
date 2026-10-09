---
name: deploy
description: "Deploy local changes to the production jizhang server: hash-diff local vs remote, sync only what changed, rebuild affected containers, verify. Use when the user says 部署 / 上线 / 发布 / deploy."
---

# Deploy

把当前工作树发布到 `https://jizhang.zhanghongyun.cn/`。整个流程由一个编排器驱动，因此**重复执行始终安全**：哈希比对让"树没变"的那次成为 **no-op**，`up -d --build` 本身幂等，`ssh` 全程只读写 `/srv/jizhang` 自己的四个容器。构建失败时**旧容器原样继续服务**——部署不会因为失败而让站点下线。

## Steps

1. 在仓库根执行：

   ```powershell
   pwsh -ExecutionPolicy Bypass -File .agents\skills\deploy\scripts\deploy.ps1 -Smoke
   ```

   **完成判据**：输出 `DEPLOY: ALL_GREEN`。

   若输出 `无差异，无需同步`，这是一次**完成的空跑**——内容本就已在线上。如实报告后停止；`-Force` 用来处理"没改动但想重建"，不用来把空跑变成成功。

2. 若第 1 步打印出 `D 路径` 行，那是**孤儿**：服务器上有、本地已删或已移出作用域的文件。与用户确认后加 `-Purge` 重跑一次即删除（默认只报警不删）。

3. 若输出 `DEPLOY: FAILED`，按阶段头定位：

   | 阶段 | 含义 | 处置 |
   |---|---|---|
   | `[1/4]` | SSH 连不上 / 私钥无效 / 本地清单为空 | 确认 `-Key` 指向的私钥存在且可读 |
   | `[2/4]` | 某文件标为待同步但本地不存在 | 清单与磁盘不同步，**先原样重跑一次**；仍失败再查 |
   | `[3/4]` | 构建失败，或 `--wait` 等不到 healthy | 末 8 行带编译/打包错误；超时则 `docker logs --tail 50 jizhang-web` |
   | `[4/4]` | 已部署但验证不过 | 看具体哪条 `FAIL` 及其 `实际`/`期望` |
   | `[5/5]` | 冒烟没过（仅 `-Smoke`） | 失败时输出**全量**打印，据此定位是哪一项真的坏了 |

   修完回到第 1 步重跑——diff 驱动，重跑只补未完成的部分。

4. 报告：同步了哪些文件、重建了哪些容器、`verify.sh` 四项结果、冒烟是否通过。

## Reference

### 编排器做什么

`deploy.ps1` 依次跑四个阶段，每个阶段在输出里带编号：

1. 生成本地哈希清单并上传。`targets` 覆盖 `frontend/src|public|index.html|package.json|package-lock.json|tsconfig.json|vite.config.ts|Dockerfile|nginx.conf|.dockerignore|e2e`、`backend`、`docker`、`openspec`、`README.md` 等 —— **`package-lock.json` 与 `nginx.conf` 少一个都不行**：前者漏了，服务器就是"新 package.json + 旧 lock"，`npm ci` 直接报 out of sync；后者是 Dockerfile 里 `COPY nginx.conf` 的源。同时上传 `roots` + `exclude`，让远端用**同一份**作用域规则找孤儿。
2. 远端 `diff.sh` 比对 → 产出 `M`（内容不同）/`A`（缺失）清单，逐个 `scp` 补齐；另产出 `D`（孤儿）清单，只报警不删除。
3. `docker compose --profile app up -d --build --wait --wait-timeout 180`，服务集按改动范围推导。`--wait` 阻塞到 healthcheck 通过——不等的话紧接着的验证会看到 `running/starting` 而误判 FAIL。
4. 远端 `verify.sh`：源码复比对必须 `0 不同 / 0 缺失`、四容器 `running/healthy`、`/` 与 `/api/v1/health` 返回 200、线上 `index.html` 引用的每个 favicon 都取得到 → `VERIFY: ALL_GREEN`。验证不过是**结果**不是异常，逐行打印出来。

`-Smoke` 再追加一步：读 `/srv/jizhang/.env` 的 `ADMIN_PASSWORD`，跑 `frontend/e2e/prod_smoke.mjs`（17 项浏览器冒烟）。

### 开关

| 开关 | 作用 |
|---|---|
| `-Smoke` | 验证通过后跑 17 项生产冒烟 |
| `-Force` | 无视差异，强制重建 `web` + `api` |
| `-Purge` | 删除孤儿文件（默认只报警）；带 `..` 或绝对路径的条目会被拒删 |
| `-Key` / `-Server` / `-Remote` / `-Site` | 覆盖默认连接参数 |

### 构建发生在服务器上

`compose` 的 `build: ./frontend` 意味着 **npm 构建在远端**，本地 `frontend/dist` 是陈旧产物，不进清单。`frontend/.dockerignore` 是 `node_modules / dist / e2e`——**`frontend/public/` 不在排除列表**，由 Vite 拷进 `dist`，所以新增静态资源只要 `scp` 过去、重建 `web` 即可；`e2e` 整个排除是因为 `tsconfig.json` 只 `"include": ["src"]`、`vite.config.ts` 也不引用它，留着只是把 84MB 截图塞进构建上下文。`.env` 永不入清单：服务器有自己的一份（mode 600）。

不是 git 仓库——**源码同步就是部署**，没有 commit / tag / branch 步骤。

### 重建范围

| 改动 | 重建 |
|---|---|
| `frontend/**`、`docker/**`、`docker-compose.yml` | `web` |
| `backend/**`、`docker/**`、`docker-compose.yml` | `api` |
| `openspec/**`、`README.md`、`.env.example` 等纯文档 | 只同步，跳过重建 |

### 远端命令纪律（踩过的坑）

- 远端命令一律**写成脚本文件、scp 过去执行**。内联进 PowerShell 的 `$(...)` 会被本机提前展开，`\"` 穿过 ssh 参数也会破形。
- 传给远端的文本必须是 **LF**。CRLF 会让 bash `read` 把 `\r` 留在路径末尾，症状是"全部文件服务器缺失"。
- SSH 一律 `-o BatchMode=yes -o ConnectTimeout=15 -i <key>`；私钥 `C:\Users\hongy\AppData\Local\Temp\opencode\id_jizhang`。原键 `C:\Users\hongy\Documents\1` 权限过宽，ssh 会拒用。
- PowerShell 的 `2>&1` 会把远端 stderr 变成 ErrorRecord，在 `$ErrorActionPreference='Stop'` 下放大成终止错误——脚本里已把 stderr 落到临时文件。
- 非 root 下 `/var/lib/docker/**` 的通配符不展开，需要 `sudo bash -c '...'`；`docker ps --format '{{.ImageID}}'` 字段不存在，改用 `docker inspect -f '{{.Image}}'`。

### 同机的其他项目

`weibo_watch*`、`westar_*`、`redis`、`weibo_watch_mysql` 与本项目共用这台主机。`docker` 命令一律带 `cd /srv/jizhang` 并只点名 jizhang 的服务（`web` / `api` / `mysql` / `redis`）；`prune`、`clean`、删卷这类操作按项目归属确认后再执行。

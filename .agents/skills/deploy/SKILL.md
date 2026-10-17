---
name: deploy
description: "Deploy local changes to the production jizhang server: hash-diff local vs remote, sync only what changed, rebuild affected containers, verify. Use when the user says 部署 / 上线 / 发布 / deploy."
---

# Deploy

把当前工作树发布到 `https://jizhang.zhanghongyun.cn/`。编排器按哈希差异同步并重建受影响的服务；构建期间旧容器继续服务。有待执行的数据库迁移时，构建完成后暂停 API 写入和后台任务，备份并在独立恢复库验证升级、回滚和再次升级，再切换服务。

## Steps

1. 在仓库根执行：

   ```powershell
   pwsh -ExecutionPolicy Bypass -File .agents\skills\deploy\scripts\deploy.ps1 -Smoke
   ```

   **完成判据**：输出 `DEPLOY: ALL_GREEN`。

   若无差异且验证成功，这是一次完成的空跑。若上次已同步但构建或切换失败，文件一致不能证明新镜像已运行，修复原因后用 `-Force` 完成重建。

2. 若第 1 步打印出 `D 路径` 行，那是**孤儿**：服务器上有、本地已删或已移出作用域的文件。与用户确认后加 `-Purge` 重跑一次即删除（默认只报警不删）。

3. 若输出 `DEPLOY: FAILED`，按阶段头定位：

   | 阶段 | 含义 | 处置 |
   |---|---|---|
   | `[1/4]` | SSH 连不上 / 私钥无效 / 本地清单为空 | 确认 `-Key` 指向的私钥存在且可读 |
   | `[2/4]` | 某文件标为待同步但本地不存在 | 清单与磁盘不同步，**先原样重跑一次**；仍失败再查 |
   | `[3/4]` | 构建失败，或 `--wait` 等不到 healthy | 末 8 行带编译/打包错误；超时则 `docker logs --tail 50 jizhang-web` |
   | `[4/4]` | 已部署但验证不过 | 看具体哪条 `FAIL` 及其 `实际`/`期望` |
   | `[5/5]` | 冒烟没过（仅 `-Smoke`） | 失败时输出**全量**打印，据此定位是哪一项真的坏了 |

   修复后重跑。迁移验证失败时，`release.sh` 仅在生产库仍为原版本且 clean 时重启旧 API；若生产库已变更或 dirty，保留备份，先查明迁移状态并修复，再恢复服务。数据库迁移的业务回滚限制见 `docs/personal-platform-migration.md`。

4. 报告：同步了哪些文件、重建了哪些容器、`verify.sh` 四项结果、冒烟是否通过。

## Reference

### 银行理财生产采集配置

生产私有 `.env` 已选择 `COMPOSE_FILE=docker-compose.yml:docker-compose.wealth.yml` 并保存共享采集密钥。后续部署保留这一选择；`.env` 仍只留在服务器。编排器已同步 `collector/` 与 overlay，采集运行时代码变化会构建 `wealth-collector`，overlay 变化也会重建 API；先等采集器 healthy，再切换 API。验证会增加采集器健康与 API 内部连通性检查。采集器源码无差异但上次构建失败时，需明确补跑其构建及健康切换，单独 `-Force` 只保证 web/api 重建。

生产采集服务名为 `jizhang-wealth-collector`。`jizhang-wealth-qualification-<run-id>` 是独立真实时间验收，发布不重启、不替换、不清理它；持续采样记录见 `openspec/changes/add-bank-wealth-accounts/qualification.md`。采集器进程健康和一次取数成功各自记录，均不替代三天正式验收。

### 编排器做什么

`deploy.ps1` 依次跑四个阶段，每个阶段在输出里带编号：

1. 生成本地哈希清单并上传。`targets` 覆盖 `frontend/src|public|index.html|package.json|package-lock.json|tsconfig.json|vite.config.ts|Dockerfile|nginx.conf|.dockerignore|e2e`、`backend`、`docker`、`openspec`、`README.md` 等 —— **`package-lock.json` 与 `nginx.conf` 少一个都不行**：前者漏了，服务器就是"新 package.json + 旧 lock"，`npm ci` 直接报 out of sync；后者是 Dockerfile 里 `COPY nginx.conf` 的源。同时上传 `roots` + `exclude`，让远端用**同一份**作用域规则找孤儿。
2. 远端 `diff.sh` 比对 → 产出 `M`（内容不同）/`A`（缺失）清单，逐个 `scp` 补齐；另产出 `D`（孤儿）清单，只报警不删除。
3. 上传并执行 `scripts/release.sh`：先构建受影响服务；有待执行迁移时暂停 API、创建压缩备份、恢复到随机命名的独立库，验证 up/down/up 和原财务表数据哈希一致，再清理恢复库。生产迁移也在 API 暂停期间执行并比较原财务表哈希，验证后切换服务。备份保留在 `/srv/jizhang/backups/jizhang-pre-release-*.sql.gz`。切换使用 `up -d --no-build --wait --wait-timeout 180`，等健康检查通过。
4. 远端 `verify.sh`：源码复比对必须 `0 不同 / 0 缺失`、四容器 `running/healthy`、`/` 与 `/api/v1/health` 返回 200、线上 `index.html` 引用的每个 favicon 都取得到 → `VERIFY: ALL_GREEN`。验证不过是**结果**不是异常，逐行打印出来。

`-Smoke` 再追加一步：读 `/srv/jizhang/.env` 的 `ADMIN_PASSWORD`，创建唯一命名的临时账本账号，跑 `frontend/e2e/prod_smoke.mjs`。包括管理页 CRUD、跨账号目录隔离、记账与会话验证。仅清理本次测试流水及商家，最后停用临时账号并撤销会话；系统不提供用户物理删除接口，因此保留停用的测试账号。清理失败也判冒烟失败。

### 开关

| 开关 | 作用 |
|---|---|
| `-Smoke` | 验证通过后用临时账本跑生产冒烟 |
| `-Force` | 无视差异，强制重建 `web` + `api` |
| `-Purge` | 删除孤儿文件（默认只报警）；带 `..` 或绝对路径的条目会被拒删 |
| `-Key` | 指定 SSH 私钥；省略时自动选择候选路径中第一个存在的文件 |
| `-Server` / `-Remote` / `-Site` | 覆盖默认连接参数 |

### 构建发生在服务器上

`compose` 的 `build: ./frontend` 意味着 **npm 构建在远端**，本地 `frontend/dist` 是陈旧产物，不进清单。`frontend/.dockerignore` 是 `node_modules / dist / e2e`——**`frontend/public/` 不在排除列表**，由 Vite 拷进 `dist`，所以新增静态资源只要 `scp` 过去、重建 `web` 即可；`e2e` 整个排除是因为 `tsconfig.json` 只 `"include": ["src"]`、`vite.config.ts` 也不引用它，留着只是把 84MB 截图塞进构建上下文。`.env` 永不入清单：服务器有自己的一份（mode 600）。

不是 git 仓库——**源码同步就是部署**，没有 commit / tag / branch 步骤。

### 重建范围

| 改动 | 重建 |
|---|---|
| `frontend/**`、`docker/**`、`docker-compose.yml` | `web` |
| `backend/**`、`docker/**`、`docker-compose.yml` | `api` |
| `collector` 运行时代码、`docker-compose.wealth.yml`、`docker/playwright-seccomp.json` | `wealth-collector`；overlay 变化同时重建 `api` |
| `openspec/**`、`README.md`、`.env.example` 等纯文档 | 只同步，跳过重建 |

### 远端命令纪律（踩过的坑）

- 远端命令一律**写成脚本文件、scp 过去执行**。内联进 PowerShell 的 `$(...)` 会被本机提前展开，`\"` 穿过 ssh 参数也会破形。
- 传给远端的文本必须是 **LF**。CRLF 会让 bash `read` 把 `\r` 留在路径末尾，症状是"全部文件服务器缺失"。
- SSH 一律 `-o BatchMode=yes -o ConnectTimeout=15 -i <key>`。未传 `-Key` 时，编排器依次检查 `C:\Users\hongy\AppData\Local\Temp\opencode\id_jizhang`、`C:\Users\Administrator\Documents\1`，使用第一个存在的文件；全部缺失时提示用 `-Key` 指定。显式传入 `-Key` 优先，指定文件缺失时直接报错。候选列表由 `scripts/deploy.ps1` 的 `$KeyCandidates` 维护。旧路径 `C:\Users\hongy\Documents\1` 曾因权限过宽被 ssh 拒用，不在自动候选中。
- PowerShell 的 `2>&1` 会把远端 stderr 变成 ErrorRecord，在 `$ErrorActionPreference='Stop'` 下放大成终止错误——脚本里已把 stderr 落到临时文件。
- 非 root 下 `/var/lib/docker/**` 的通配符不展开，需要 `sudo bash -c '...'`；`docker ps --format '{{.ImageID}}'` 字段不存在，改用 `docker inspect -f '{{.Image}}'`。

### 同机的其他项目

`weibo_watch*`、`westar_*`、`redis`、`weibo_watch_mysql` 与本项目共用这台主机。`docker` 命令一律带 `cd /srv/jizhang` 并只点名 jizhang 的服务（`web` / `api` / `mysql` / `redis`）；`prune`、`clean`、删卷这类操作按项目归属确认后再执行。

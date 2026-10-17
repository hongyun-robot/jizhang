# 银行理财公开净值采集器

当前只允许苏银理财恒源日申周赎1号A（J07241）。它使用新的匿名浏览器上下文访问固定官方页面，取得该份额“成立以来”的 JSON，返回产品身份、来源、获取时间和原始净值行，供 Go 适配器校验。它不访问数据库、不接收个人份额/成本/银行凭据、不接受任意网址或脚本。

**短期匿名采集已打通，多日准入尚未完成。** 2026-10-16 核实 Cookie、User-Agent 和自动化标志后，本机与服务器隔离 Linux 容器均取得 265 条净值，服务器采集服务返回 200，Go 适配器解析全部记录通过。另已验证：完整匿名会话下不附动态签名的 HTTP 表单查询可成功。见 [动态校验分析](../docs/research/suyin-dynamic-challenge.md)。连续三天准入尚未完成，产品不因单次取数成功自动进入可新增目录。

## 本机测试

```powershell
cd collector
npm ci --ignore-scripts
npm test
# 仅查询公开产品，不写任何个人账本；需要安装相应浏览器
$env:WEALTH_BROWSER_EXE = 'C:/Program Files/Google/Chrome/Application/chrome.exe'
$env:WEALTH_LIVE_SMOKE = '1'
npm run smoke
# 可选：只打印参数和 Cookie 元数据的对照，不输出令牌原值
node challenge-diagnostic.mjs normal-ua --http-replay
```

服务运行前，将至少 24 字符的随机 `WEALTH_COLLECTOR_KEY` 放入进程环境或已忽略的 `.env`，不写入命令、文档或日志。`npm start` 默认仅监听 `127.0.0.1:8091`；内部调用为带 Bearer 密钥的 `POST /v1/products/J07241/nav`，不接收请求正文。`GET /health` 只显示进程存活，不表示上游取数成功。相同产品在途请求合并、尝试间隔至少 60 秒，错误不返回原始浏览器会话信息。

## 隔离容器

先在根目录已忽略 `.env` 配置同一私有密钥，再执行：

```powershell
docker compose -f docker-compose.yml -f docker-compose.wealth.yml --profile wealth build wealth-collector
docker compose -f docker-compose.yml -f docker-compose.wealth.yml --profile wealth run --rm -e WEALTH_LIVE_SMOKE=1 wealth-collector node smoke.mjs
```

容器不映射宿主端口，以非 root 用户运行 Chromium 沙箱，限制内存/进程和共享内存。Playwright 依赖与官方镜像均固定为 1.63.0；此镜像的 pwuser 为 UID/GID 1001，临时 home 挂载保持同一归属。`docker/playwright-seccomp.json` 基于 Microsoft Playwright 的同版本源码，保留拒绝式默认规则并允许浏览器用户命名空间。因本配置移除了所有宿主 capabilities，额外允许 Chromium 沙箱使用 chroot 系统调用；内核仍校验当前命名空间权限，未增加宿主 SYS_CHROOT/SYS_ADMIN capability。参照[官方 Docker 指引](https://playwright.dev/docs/docker)与[配置源码](https://github.com/microsoft/playwright/blob/v1.63.0/utils/docker/seccomp_profile.json)。不通过关闭浏览器沙箱或复用个人会话使验收变绿。

多日验收必须在目标部署等价环境按真实时间完成；一次 smoke、单元测试或手工补值都不能替代。2026-10-16 生产已启用独立采集服务并接入账户 API 和四槽调度，真实取得并入库 265 条，详见 [生产同步记录](../openspec/changes/add-bank-wealth-accounts/production-sync-2026-10-16.md)。生产私有 `.env` 持久选择 wealth overlay，其采集器支持 app/wealth profile；默认本地开发仍只用主 Compose 文件。独立固定版本三天采样继续运行，生产同步结果不替代正式采样槽。

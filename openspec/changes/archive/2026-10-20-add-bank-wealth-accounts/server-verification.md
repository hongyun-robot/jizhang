# 隔离服务器采集验证

> 本页保留修复前的失败诊断。后续已于 2026-10-16 修复运行环境及页面等待问题，本机、服务器浏览器和完整匿名会话下的 HTTP 查询均已成功；最新结论见 [动态校验分析](../../../../docs/research/suyin-dynamic-challenge.md) 及 `evidence/2026-10-16-challenge/`。多日准入尚未完成。

验证日期：2026-10-16（北京时间）。用户已明确允许在现有服务器执行隔离、只读采集测试；本次没有发布应用、执行数据库迁移或写入真实个人账本。

## 结果

**部署运行边界验证通过，J07241 匿名实际取数未通过。** 服务器 Linux Chromium 的主页面导航仍被官网返回 HTTP 400，没有取得净值 JSON。应用服务的健康检查成功不能视为数据源接入成功；产品继续不具备开放新增的准入证据，连续三天验收也未完成。

最终公开查询结果见 [smoke.json](evidence/2026-10-16-server/smoke.json)：

```json
{
  "code": "J07241",
  "fetched_at": "2026-10-15T16:20:43.656Z",
  "status": "failed",
  "error": "source_navigation_rejected",
  "http_status": 400,
  "browser_version": "153.0.8010.12",
  "platform": "linux"
}
```

## 已验证的隔离边界

- Docker 29.6.1，官方 Playwright 1.63.0 Noble 镜像，linux/amd64。本机用官方 Go 模块 `github.com/google/go-containerregistry/cmd/crane@v0.22.1` 下载相同摘要镜像，流式转入服务器，避免远端额外保存约 911 MB tar。
- 官方平台 manifest 为 `sha256:bc6ab0d6d44ff4826e4cb8c1e6d801e185bfc42bb0753f8e2a30efc70db054c7`，加载后核对 config 为 `sha256:2c1f4e0fd6450f43ddb46d60c2a6df30855a8588e165b1f2559fb0eda8d7ff35`。
- 非 root pwuser（UID/GID 1001）、只读根文件系统、768 MiB 内存、0.75 CPU、256 进程上限、256 MiB 共享内存、移除全部宿主 capabilities，Chromium sandbox 开启，没有增加 SYS_ADMIN 或使用 `--no-sandbox`。
- 临时专用 Docker 网络实际连通采集服务，服务没有宿主端口映射、没有数据库或生产目录挂载；Compose overlay 配置通过 `config --quiet`，没有启动生产 Compose 服务。
- `GET /health` 为 200，未认证为 401，未知产品为 400，拒绝请求正文为 400；实际取数为 502 且没有数据，立即重复请求为冷却 429。
- 服务容器重启后健康恢复 200；独立无网络容器内强制 1ms 采集超时，验证浏览器回收、连接断开及有界退出。
- 检查记录见 [service-check.txt](evidence/2026-10-16-server/service-check.txt) 和 [service-result.json](evidence/2026-10-16-server/service-result.json)。临时密钥仅存在权限受限文件及测试进程环境，证据中不包含密钥或浏览器会话信息。

## 运行配置修正

1. 从 Windows 打包的源文件可能是 root 只读的 0600，Dockerfile 改用 `COPY --chown=pwuser:pwuser`，确保非 root 可读。
2. 官方镜像 pwuser 实测为 1001，临时 home 的 UID/GID 从原先假设的 1000 修正为 1001，解决不可写 home 导致的 crashpad 启动失败。
3. 在移除全部宿主 capabilities 后，官方 seccomp 的条件 chroot 规则不能满足 Chromium 的嵌套用户命名空间沙箱。配置显式放行 chroot 系统调用，内核仍执行当前命名空间能力检查；保持默认拒绝、无宿主 capabilities、非 root 及浏览器 sandbox。
4. 取数错误仅将主框架导航的拒绝视作整次失败，避免子框架错误误判；脱敏 smoke 增加具体 HTTP 状态，不输出动态参数或 Cookie。

## 生产服务及清理

四个既有记账容器保持 healthy，RestartCount 均为 0；web/api 启动时间仍为 2026-10-15 13:32/13:31 UTC，早于本次测试，未重启或替换。最终校验见 [final-check.txt](evidence/2026-10-16-server/final-check.txt)。

所有本次临时容器、专用网络和临时密钥已移除。保留官方镜像缓存、以 `jizhang-wealth-probe` 命名的测试镜像及权限受限的源码/日志目录供复现，没有后台采集任务继续运行；未执行 prune、删除其他项目资源或更改生产配置。

## 同期完成的独立界面工作

本地“记一笔”已移除黄金买卖模式，黄金详情保留买卖及报价刷新；没有生产发布。类型检查、生产构建及 `node e2e/gold.mjs` 的 66 项断言通过，原响应丢失重试、草稿、资金只扣一次、手机、卖出和换号检查保留，同时新增首页/流水页无投资模式检查。

截图：[普通记账桌面](screenshots/ordinary-record-desktop.png)、[独立黄金买入手机](screenshots/gold-independent-buy-mobile.png)。

## 后续边界

本轮完成任务 1.2、7.6、7.7，连同此前 1.3，共 4/38。1.1 的成功实际采集和 1.4 的多日准入未通过；账户模型、财务事件、本人补值及理财页面等其余任务仍未完成。继续前需解决官方匿名访问通道，或明确调整数据源方案；不能把人工补录或一次模拟结果视作自动接入通过。

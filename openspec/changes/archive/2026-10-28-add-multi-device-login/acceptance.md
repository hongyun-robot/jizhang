# 多设备登录实施验收

日期：2026-10-28。环境：Windows、Go 1.26.2、Node 24.15.0；本机 MySQL97 与 Redis6380 已运行。实现使用现有 MySQL 用户表及新版 Redis 会话 Hash，没有财务数据库迁移。

## 已实现行为

- 手机、平板、电脑每类一个有效会话，三类并存；同类新登录成功后替换旧会话，失败不替换。
- 新版 JWT/refresh 绑定 sid、设备类型及账号代次；所有受保护请求校验服务端会话。登录、刷新和撤销按原子提交顺序执行。
- 主动“退出所有设备”撤销账号全部会话；失效凭据、被替换和自然到期不触发账号级撤销。管理员角色、状态、密码操作继续撤销全部会话，撤销失败不静默成功。
- Web Locks 串行完整认证响应，SharedWorker 提供回退、队列和内存结果共享；缺少广播时通过非秘密 storage 生命周期标记配合代理恢复。无法可靠协调时暂停认证写入。
- 退出失败的待确认状态跨页面重载保留；后台回前台重新验证，认证服务故障时阻止资金请求。缓存使用真实的 access 到期时间，迟到广播不延长令牌有效期。
- 登录页提示同类替换原因，保留站内回跳；退出/换号清理旧缓存、导航记忆及草稿，主题设置沿用原行为。
- 旧协议和缺少 sid 的旧令牌拒绝使用。升级须同步发布前后端并统一重新登录，操作及安全回滚说明已写入 README。

## 自动检查

| 检查 | 结果 |
| --- | --- |
| 认证服务、handler、中间件专项 Go 测试 | 通过，覆盖分类、失败限流、签发失败、三类并存、同类并发、旧 token、轮换/撤销交错及 HTTP Cookie 行为 |
| `AUTH_REDIS_ADDR=127.0.0.1:6380` 下的 `TestNativeRedisSessions` | 通过；独立随机测试键空间，覆盖并发、滑动过期、旧 access/refresh、撤销和读取故障后的恢复；测试键已清理 |
| `go vet ./...` | 通过 |
| `go test ./...` | 448 个通过结果，58 个条件测试跳过，1 项既有负债测试失败，详见下文 |
| `npm run test:unit` | 30 个文件、101 项测试通过 |
| `npm run typecheck` | 业务及测试 TypeScript 均通过 |
| `npm run build` | 通过，包含 SharedWorker 产物；仍有 session/Stats 分包超过 500 kB 的构建提示 |
| `node --test e2e/support/fixture.test.mjs` | 3 项通过：本地 fixture 保护、UI 当前凭据复用、延后 cleanup 不使用被替换 token |
| 全部 63 个浏览器 `.mjs` 脚本语法检查 | 通过 |
| `git diff --check` | 通过 |
| `openspec validate add-multi-device-login --strict` | 通过 |

Go 专项使用：

```powershell
go test ./internal/service ./internal/handler ./internal/middleware -run 'Test(AuthHTTP|Login|Device|Token|Devices|Refresh|SessionExpiry|Logout|AccessExpiry|DisabledAccount|ConcurrentSession|UserSecurity|AdminOperations)' -count=1
```

## 隔离浏览器验收

券 fixture 使用 18084/5601，负债 fixture 使用 18085/5602；均为临时 SQLite/miniredis 账本，写入前验证 fixture 身份，未向真实账本写入。资金和退出用例在同一账本内顺序执行。

| 脚本 | 已验证结果 |
| --- | --- |
| `multi_device_login.mjs` | 三类共存、同类替换、失败登录与旧 logout 隔离、其他账号不受影响、双标签页共享与合并刷新、503 阻止资金提交、全设备退出、前台恢复、旧成功响应不能覆盖新 Cookie、退出失败重载及重试、SharedWorker/无广播/能力缺失路径；无浏览器运行时错误 |
| `users.mjs` | 随机临时用户、重复用户名、最后管理员门禁、角色/密码/状态操作撤销旧会话、恢复后必须重登、移动端布局；不再删除开发数据库用户 |
| `voucher_retry.mjs` | 已提交响应超时后改金额重试仍使用相同操作键，返回 409，只有一笔购券，余额清理恢复 |
| `refactor_cache.mjs` | 购券后刷新当前流水列表，准备/断言/cleanup 复用正确认证 |
| `refactor_navigation.mjs` | 标签页恢复、重复导航、全设备退出及账号切换 |
| `refactor_drafts.mjs` | 预算取消/确认切换/保存、负债账户草稿不被后台读取覆盖、换编辑对象与迟到目录隔离 |

开发热更新可为源码模块附加时间戳，直接重新 import 不带时间戳的 store 会创建第二份测试实例。多设备脚本已按实际加载的资源 URL 获取模块，并使用干净测试 Vite 完成最终回归。截图等待入场动画结束后捕获并已目视检查。

## 既有失败及验证边界

全量 Go 测试唯一失败为 `TestPersonalLiabilityAccountsAndIndependentRules`，位置 `backend/internal/service/personal_liability_test.go:45`，错误为“请先配置负债账户的账单规则；账单管理仅支持零余额启用”。在实施前基线 `69de3b3` 的临时副本中单独执行同名测试，得到相同失败；当时通过 `git archive HEAD backend` 提取该副本，随后已清理。本次没有修改该负债实现或测试，不能将全量 Go 结果报告为通过。

未配置 DSN 的 MySQL 集成测试及显式环境开关控制的用例保持条件跳过；本次使用本机真实 Redis 和隔离浏览器补充认证验证。未运行生产冒烟、生产部署或全部旧金融浏览器套件。设备验收为浏览器模拟，未进行 iPhone/iPad 实机验证。

以上为实施阶段记录；后续生产发布及线上 22/22 通用冒烟、24/24 多设备专项结果见[部署记录](deployment.md)。

## 截图

- [桌面退出所有设备](../../../../frontend/e2e/shots/multi-device/desktop-logout.png)
- [手机同类替换提示](../../../../frontend/e2e/shots/multi-device/phone-replaced.png)
- [桌面用户管理](../../../../frontend/e2e/shots/refactor-frontend/users-multi-device-desktop.png)
- [手机用户管理](../../../../frontend/e2e/shots/refactor-frontend/users-multi-device-mobile.png)

截图位于 Git 忽略的测试产物目录；生产配置和签名密钥没有在本轮修改。

## 归档核对

2026-10-28 已将 `identity/user-auth` 增量同步至主规范：新增 3 条、修改 3 条要求，保留原 Purpose、登录失败限流及停用账号要求；主规范共 8 条要求、39 个场景。`openspec validate --specs` 验证 22 项通过、0 项失败，26 项实施任务全部完成，变更归档至 `openspec/changes/archive/2026-10-28-add-multi-device-login/`。

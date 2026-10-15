# 建行积存金接入与迁移

## 数据和账务

迁移为 `000017_ccb_gold_accounts.up.sql` / `.down.sql`。新增黄金持仓、真实事件、请求幂等、审计、双边报价观察、产品同步租约和每日估值表；资金流水增加唯一的 `gold_event_id`，黄金与基金/券/负债计划关联互斥。原账户 ID、期初、余额及金融历史保持，不自动创建个人黄金账户。

黄金账户使用 `type=asset, asset_class=gold`、零现金期初。一位用户可建立多个账户，克数/成本独立，公共报价共享。期初填写总成本或每克成本，不补扣现金。买入金额为含手续费的实际总扣款，卖出金额为实际净到账；可空费用只用于解释，不能重复扣减。首版只记录同日生效的已完成交易，不向银行下单，也不处理跨日待结算或实物提取。

成本采用加权平均。示例：10 克成本 8000 元，再买 10 克实际扣款 9000 元，则平均成本为 850 元/克。卖出 5 克净到账 4500 元，结转成本 4250 元、已实现收益 250 元，剩余 15 克成本 12750 元。市值采用建行客户赎回价；费用、买卖与损益不进入生活收支和预算。

只读成员可配置本人账户、录入合法期初和刷新报价，不能买卖或撤销资金。所有个人资源按本人隔离。只能撤销最后有效事件，原事件与审计保留；普通流水改删/退款不能分开处理黄金现金。补录须重放后继，任何超卖、财务影响涉及封账或部分写入失败都整体拒绝。

## 报价与缓存

服务端按固定建行产品使用匿名 Cookie jar 访问 `100119` 后请求 `NGJS01`，无需用户银行凭据或 API Key。公开网页端点、实测字段和已知限制见 [调研](research/ccb-gold-accumulation-api.md)。Go 客户端显式设置 `Mozilla/5.0 (compatible; JizhangGold/1.0)` 客户端标识；2026-10-15 实测默认 Go UA 得到空 HTTP 200，使用该标识后成功。请求严格限制主机、超时、响应大小，并校验业务成功、产品 33、币种 156、正价和源时间；会话提示最多恢复一次，Cookie 不输出或落盘。

后台北京时间每小时整点查询，首个黄金账户建立后安排一次合并冷查询，启动恢复当前未尝试小时槽；无用户在线也执行。MySQL 保存报价、小时槽、租约令牌和冷却，进程重启不丢缓存，多 worker 不重复请求同槽。手动刷新可越过小时缓存，全产品30秒尝试冷却并合并在途请求。普通 GET 和页面一分钟更新仅读取本站缓存，不直接访问银行。

分别保留银行源时间 `quote_at`、成功获取时间 `fetched_at/last_success_at` 和最近错误。同源时间重复成功只增加观察，不生成行情收益或个人交易；源时间倒退拒绝覆盖，同时间价格修订追加保留原观察。错误保留最近有效价，连续至少两小时未成功提示更新异常；银行休市源时间不动不等于请求失败。参考价不覆盖用户填写的实际成交金额。

从未有价也可保存期初和真实交易。非零黄金未知市值返回 `balance/market_value=null`，首页显示估值不完整；清仓为真实零。前端须与后端同时升级以支持此扩展。当前市值可用带时间的最近缓存；已结束日期的历史曲线使用当日已保存的最后有效观察，没有观察则保留缺口，不用今天价格倒算。成功查到银行旧源价仍保留其真实源时间。

## 应用 API

路径前缀 `/api/v1/gold`，全部要求登录；个人资源同时校验账户归属。金额和克数使用十进制字符串。

| 方法与路径 | 用途 |
|---|---|
| `GET /quote` | 缓存双边价、源/获取时间、错误和刷新状态 |
| `POST /quote/refresh` | 合并手动查询，返回错误/冷却/进行中状态，不创建财务记录 |
| `GET /accounts`、`GET /accounts/:id` | 本人账户、克数、成本和盈亏 |
| `POST /accounts/:id/opening`、`PUT /accounts/:id/opening` | 创建或合法更正期初 |
| `POST /accounts/:id/events` | 真实 `buy` / `sell` |
| `GET /accounts/:id/events`、`/valuations`、`/audits` | 事件、估值版本和审计 |
| `POST /accounts/:id/events/:eventId/undo` | 账号幂等的末笔撤销 |

买卖输入包括 `request_key, kind, grams, cash_amount, trade_date, cash_account_id`，可选 `fee,note`。期初为 `request_key, grams, trade_date, cost_mode=total|unit,cost`。未来日期、非法精度、范围越界、超卖、非本人现金或跨日资金拒绝；同账号同键同内容返回原回执，不同内容冲突。资金已提交而派生历史失败保留原成功回执和重建标记，读取/后台可恢复。

## 升级及回滚

1. 升级前按仓库规则备份财务库，核实迁移编号与现有版本，暂停旧版本写入后按现有迁移流程应用 up；本变更验收仅迁移随机测试库，不修改真实开发或生产财务库。
2. 同批发布前后端，再启动 API 和黄金小时任务。已有现金、基金、券及负债数据不需改写，用户主动建立自己的黄金账户。
3. down 在存在任何个人黄金账户、持仓、事件、审计、幂等或黄金资金引用时明确拒绝，即使已清仓或事件已撤销也不可删除财务历史。只有无个人资源且仅有公共报价时可回滚，先停止 worker。
4. 已有黄金财务资源时采用兼容版本前滚修复或完整验证过的备份恢复，不直接启动只识别 cash/fund 的旧应用；旧版可能将黄金本金或空余额误算为资产。

## 隔离验收

本地 MySQL 与 Redis 按仓库要求启动。`GOLD_MYSQL_DSN` 需要创建/删除临时数据库权限，测试忽略 DSN 数据库名、随机建库并清理：

```powershell
# backend；在当前终端安全配置 GOLD_MYSQL_DSN，勿把凭据写入仓库
go test ./internal/service -run '^TestGoldMySQL' -count=1 -v
go test ./internal/ccb ./internal/service -run 'TestGold|TestAnonymous|TestBadQuotes|TestResponse|TestSession|TestHTTP' -count=1
```

浏览器只允许本机 `gold-browser` 标识的临时账本，使用内存数据库、临时 Redis 和假行情，不能指向真实账本：

```powershell
# backend 终端
$env:GOLD_BROWSER_SERVER='1'
$env:BROWSER_FIXTURE_TTL='1h'
go test ./internal/router -run '^TestGoldBrowserServer$' -count=1 -v -timeout=1h
# frontend 终端
npm run build
node e2e/support/preview.mjs --dist=dist --port=5604 --api=http://127.0.0.1:18087
# frontend 另一终端，每轮重启 backend fixture 恢复初始无报价状态
node e2e/gold.mjs
```

显式只读 live smoke 默认不运行，开启后只请求公开报价，不录入持仓：

```powershell
# backend
$env:CCB_GOLD_LIVE_SMOKE='1'
go test ./internal/ccb -run '^TestGoldLiveSmoke$' -count=1 -v
Remove-Item Env:CCB_GOLD_LIVE_SMOKE
```

界面截图保存于 `frontend/e2e/shots/refactor-frontend/gold-*.png`。完整记录见 `openspec/changes/archive/2026-10-15-add-ccb-gold-accounts/verification.md`。

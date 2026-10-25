# 民生理财天天盈 9 号：HTTP 净值接口验证

核对日期：2026-10-16（北京时间）。本次只验证匿名 HTTP 查询和项目接入可行性，未实施业务集成，未验证生产服务器访问或跨日稳定性。

后续实施（2026-10-16）：已按用户要求增加 `backend/internal/cmbcwm` 原生 HTTP 适配器、共用 `wealthdata` 行情分派及 `000020` E 份额公共目录迁移，复用现有四槽/租约/持仓重估。Go 匿名完整历史验证取得 754 条，起始 2023-08-30、最新 2026-10-15 净值 1.068033；隔离测试覆盖修订、失败保留、不同来源配置和 MySQL 迁移回滚。本节下文保留原调查快照与当时接入建议，最新实施验收见 [实施记录](../../openspec/changes/archive/2026-10-20-add-bank-wealth-accounts/cmbcwm-implementation.md)。尚未生产部署或完成民生多日采样。

结论：**可以通过官网的 HTTP POST 接口获取该产品 E 份额的最新净值和分页历史，无需登录、Cookie、API Key 或浏览器执行 JavaScript。** 这是本机本次实测结果；接口由官网页面内部使用，本次没有取得面向第三方的正式 API 契约、配额、发布时间或 SLA，不能将一次成功视为长期可用承诺。[官方产品页](https://www.cmbcwm.com.cn/cmbcwm/grlc/cpxq/index.html?code=FBAE68020E)、[官方请求封装脚本](https://www.cmbcwm.com.cn/uiFramework/commonResource/zip/cmbc_wm/skin/js/main.js)

## 产品及份额

匿名调用 `POST /gw/po_web/BTAProductQry`，表单 `prd_type=0&code_or_name=FBAE68020&pageNo=1&pageSize=10`，返回以下两条记录；两者的 `TBPRDEXTEND_DEBT_REGIST_CODE` 都为 `Z7007923000092`。登记编码不能单独区分份额。用户提供的 2026-10-15 净值 `1.068033` 与 E 份额匹配。[官方产品查询端点](https://www.cmbcwm.com.cn/gw/po_web/BTAProductQry)、[官方 E 份额详情](https://www.cmbcwm.com.cn/cmbcwm/grlc/cpxq/index.html?code=FBAE68020E)

| 产品代码 `REAL_PRD_CODE` | 返回产品名称 | 净值日期 `NAV_DATE` | 单位净值 `NAV` |
|---|---|---|---:|
| `FBAE68020A` | 民生理财贵竹固收增利天天盈9号理财产品A | 20261015 | 1.072331 |
| `FBAE68020E` | 民生理财贵竹固收增利天天盈9号理财产品E | 20261015 | 1.068033 |

## 净值 HTTP 请求

官方产品页的 `queryJzssTable()` 使用 `BTADailyQry`；公共脚本的 `queryData()` 将端点拼为 `/gw/po_web/`，通过 `$.post` 发送表单。页面另以 `chart_type=0` 查询图表，本次分页验证使用表格模式 `chart_type=1`。[官方产品页源码](https://www.cmbcwm.com.cn/cmbcwm/grlc/cpxq/index.html?code=FBAE68020E)、[官方请求封装脚本](https://www.cmbcwm.com.cn/uiFramework/commonResource/zip/cmbc_wm/skin/js/main.js)

```http
POST https://www.cmbcwm.com.cn/gw/po_web/BTADailyQry
Content-Type: application/x-www-form-urlencoded

chart_type=1&real_prd_code=FBAE68020E&pageNo=1&pageSize=10
```

| 参数 | 本次验证的用法 |
|---|---|
| `chart_type` | `1`：表格分页模式 |
| `real_prd_code` | `FBAE68020E`：按具体份额查询 |
| `begin_date`、`end_date` | 可选，格式 `YYYYMMDD`；本次测试 `20261008` 至 `20261015` |
| `pageNo`、`pageSize` | 页码从 `1` 开始；本次测试每页 `2`、`3`、`10` 条，未探测最大页容量 |

本次完全省略两个日期参数，接口仍成功返回最新记录，`totalSize=754`；`pageSize=3` 返回下面三条，`pageSize=10` 返回十条。指定 `20261008` 至 `20261015`、`pageSize=2` 时，`totalSize=6`：第 1 页为 10 月 15 日、14 日，第 2 页为 13 日、12 日，观察到按净值日期倒序分页。这些是一次实测的记录数和排序，不是历史完整性或未来排序保证。[官方净值端点](https://www.cmbcwm.com.cn/gw/po_web/BTADailyQry)

下面摘录省略日期、`pageNo=1&pageSize=3` 的响应关键字段；`list` 为这次请求返回的完整三条记录，省略与之重复的 `btaDailyAddFieldList`：

```json
{
  "returnCode": { "code": "AAAAAAA", "type": "S" },
  "totalSize": 754,
  "isJXGL": "0",
  "list": [
    { "NAV": 1.068033, "ISS_DATE": 20261015, "INCOME": 0.0, "WEEK_CLIENTRATE": 0.0, "TOT_NAV": 1.068033 },
    { "NAV": 1.068, "ISS_DATE": 20261014, "INCOME": 0.0, "WEEK_CLIENTRATE": 0.0, "TOT_NAV": 1.068 },
    { "NAV": 1.067969, "ISS_DATE": 20261013, "INCOME": 0.0, "WEEK_CLIENTRATE": 0.0, "TOT_NAV": 1.067969 }
  ]
}
```

`ISS_DATE` 是净值所属日期，`NAV` 是单位净值，`TOT_NAV` 是累计净值。官方页面仅在 `isJXGL === "1"` 时展示“万份收益 / 七日年化”；本产品返回 `"0"`，页面展示“单位净值 / 累计净值”。因此这里的 `INCOME=0`、`WEEK_CLIENTRATE=0` 不能解释为“当日收益率为零”。[官方表格渲染逻辑 `reloadTableJzzs()`](https://www.cmbcwm.com.cn/cmbcwm/grlc/cpxq/index.html?code=FBAE68020E)

### 可运行样例

Windows PowerShell 中可直接运行以下 `curl.exe` 命令。本次已验证同样的请求不附带登录信息或 Cookie 即可返回 JSON：

```powershell
curl.exe --fail-with-body --silent --show-error --connect-timeout 5 --max-time 15 --request POST --header 'Content-Type: application/x-www-form-urlencoded' --data 'chart_type=1&real_prd_code=FBAE68020E&pageNo=1&pageSize=10' 'https://www.cmbcwm.com.cn/gw/po_web/BTADailyQry'
```

原生 PowerShell 样例：

```powershell
$navParams = @{
  chart_type = '1'
  real_prd_code = 'FBAE68020E'
  pageNo = '1'
  pageSize = '10'
}
$navResult = Invoke-RestMethod `
  -Uri 'https://www.cmbcwm.com.cn/gw/po_web/BTADailyQry' `
  -Method Post `
  -ContentType 'application/x-www-form-urlencoded' `
  -Body $navParams `
  -TimeoutSec 15
if ($navResult.returnCode.type -ne 'S' -or $navResult.returnCode.code -ne 'AAAAAAA') {
  throw '民生理财净值查询失败'
}
$navResult.list | Select-Object ISS_DATE, NAV, TOT_NAV
```

需要时间范围时，在表单加入 `begin_date='20261008'`、`end_date='20261015'`；需要继续分页时递增 `pageNo`，结合 `totalSize` 与实际记录数判断结束，不能只取默认第一页作为全部历史。[官方产品页表格请求参数](https://www.cmbcwm.com.cn/cmbcwm/grlc/cpxq/index.html?code=FBAE68020E)

## 收益率计算边界

对于期间没有分红、拆分等净值调整的相邻披露日，可计算 `净值涨幅 = 当次单位净值 / 上次单位净值 - 1`。例如 `1.068033 / 1.068000 - 1 ≈ 0.003090%`，这是 2026-10-15 相对 10 月 14 日的计算值，未年化。接口原始值见上面的[官方净值端点实测](https://www.cmbcwm.com.cn/gw/po_web/BTADailyQry)。

实现时保留发行方净值原值，使用十进制数计算派生收益率；若两个净值日期不连续，只能称为该区间涨幅，不能拆成未披露自然日的实际收益率。发生分红或拆分时需另外处理，不能直接把单位净值跳变当作收益。个人实际收益还取决于份额、申赎生效日等，本接口不提供个人持仓。以上是计算和接入建议，不是官网额外披露的收益率字段。

## 当前项目接入建议

以下为本地代码核对后的建议，尚未实施：

- 增加 `cmbcwm` HTTP 适配器，返回净值日期、单位净值、累计净值、份额代码和采集时间；在后端采集，共享持久化结果供前端读取。
- 将 [WealthService](../../backend/internal/service/wealth_service.go) 当前的 `suyin.MarketData` 注入改为按发行方 / 产品分派，或通过统一分派器实现现有接口；[同步服务](../../backend/internal/service/wealth_sync.go) 的全局 `Configured()` 判定也需支持多来源，避免民生查询受苏银采集器配置影响。
- 扩展 `wealthCanonicalCode()` 目前仅允许 `J07241` 的校验，并调整 [ProductForCode](../../backend/internal/repository/wealth_repo.go) 固定为苏银及其登记编码的查询。新增受控产品目录项 `cmbcwm / Z7007923000092 / FBAE68020E`，使用新的迁移，不修改已发布的 `000018` 迁移。
- 复用 [wealthSlots](../../backend/internal/service/wealth_schedule.go) 现有北京时间 08:00、12:00、18:00、22:00 的到期槽和 [Ensure 调度入口](../../backend/cmd/server/main.go)。当前入口每 30 秒检查到期任务，不等于每 30 秒访问银行；没有必要另加高频采集任务。官网固定发布时间未验证。
- 初始化按分页补历史；后续查询最近成功日期前后的一段重叠窗口，兼顾延迟披露和修订。复用 [SavePublicNAVs](../../backend/internal/service/wealth_quotes.go) 及现有持久化逻辑，按“产品份额 + 净值日期”幂等写入；相同日期出现修订时保留版本依据。
- 设置 HTTP 超时和有限重试，校验 HTTP 状态、业务返回码、有效日期和正数净值；超时、错误或空结果时保留旧值并标明数据截至日期。无新披露不能伪造今日净值；从生产环境验证连通性及跨日更新后，再确认上线可用性。

验收应覆盖匿名请求解析、分页合并、重复写入、上游失败保留旧值，以及该产品与苏银产品同时存在时的来源分派；资金估值与收益计算沿用项目已有规则。上述均为后续实施范围。

# 基金净值接口与收益计算

核对日期：2026-10-13（北京时间）。仅新增研究笔记，未改业务代码或数据库。

## 接口选择

| 方案 | 主要数据 | 本次验证 |
|---|---|---|
| 东方财富 / 天天基金网页端点 | 日期、单位净值、累计净值、日增长率 | 已完成一次请求实测 |
| Tushare Pro `fund_nav` | `nav_date`、`ann_date`、`unit_nav`、`accum_nav`、`adj_nav` 等 | 仅核对官方文档 |
| AKShare `fund_open_fund_info_em` | 净值走势、日增长率、分红及拆分详情 | 仅核对官方文档 |

Tushare 需要 token 和至少 2000 积分，支持基金代码或日期、起止日期查询。净值日期与公告日期分别返回，官方只写每日定期更新，没有固定小时承诺。它提供 POST JSON HTTP API，Go 可直接接入。[净值文档](https://tushare.pro/document/2?doc_id=119)、[权限说明](https://tushare.pro/document/1?doc_id=108)、[HTTP 文档](https://tushare.pro/document/1?doc_id=130)

AKShare 是 Python 库，可调用 `ak.fund_open_fund_info_em(symbol="161725", indicator="单位净值走势")`。返回日期、单位净值和百分数日增长率；`0.53` 表示 `0.53%`。文档的 16:00–23:00 更新窗口针对每日汇总接口，不能作为每只基金的保证。官方说明数据用于学术研究，目标网页变化会使接口异常。[基金文档](https://github.com/akfamily/akshare/blob/main/docs/data/fund/fund_public.md)、[项目概览](https://github.com/akfamily/akshare/blob/main/docs/introduction.md)

## 东方财富实测

主代理于上述日期请求：

```text
GET https://api.fund.eastmoney.com/f10/lsjz?fundCode=161725&pageIndex=1&pageSize=3
Referer: https://fundf10.eastmoney.com/
User-Agent: Mozilla/5.0
```

返回 HTTP 200、`ErrCode=0`、`TotalCount=2767`，`Data.LSJZList` 最近三条：

| 日期 | 单位净值 DWJZ | 累计净值 LJJZ | 日增长率 JZZZL |
|---|---:|---:|---:|
| 2026-10-12 | 0.5338 | 2.2499 | 0.53 |
| 2026-10-09 | 0.5310 | 2.2471 | 0.23 |
| 2026-10-08 | 0.5298 | 2.2459 | -0.30 |

[基金历史净值页面](https://fundf10.eastmoney.com/jjjz_161725.html)与[数据文件](https://fund.eastmoney.com/pingzhongdata/161725.js)交叉核对：数据文件也返回 HTTP 200，基金名为招商中证白酒指数（LOF）A，2767 条单位净值末尾三条一致。未执行远端 JavaScript，仅提取 JSON 数据，时间戳转北京时间。

这是网页内部端点，一次成功不保证长期稳定；本次未核实公共 API 契约、长期限频或 SLA。Go 项目可优先研究直接请求 JSON，这是接入判断，尚未验证生产运行。

## 计算及所需数据

单位净值适合计算当前市值，累计净值包含历史分红，不能直接乘当前份额当作可赎回金额；复权净值用于含红利再投资的历史表现。个人收益仍需实际交易及分红记录。[华夏基金指标说明](https://edu.chinaamc.com/c/2021-12-23/511783.shtml)

份额固定，期间无交易、分红、拆分或额外费用时：

```text
当前市值 = 已确认份额 × 最新单位净值
期间盈亏 = 已确认份额 × (最新净值 − 上一期净值)
期间收益率 = (最新净值 / 上一期净值 − 1) × 100%
```

例如场外持有 10000 份，上述净值从 0.5310 到 0.5338，市值从 5310 元到 5338 元，赚 28 元。10000 份不等于投入 10000 元；净值差比已舍入的增长率更适合计算金额。该例为 LOF 的场外口径，场内持仓应使用市场价格。

货币基金使用 `fund_money_fund_info_em` 的每万份收益：当日收益 = 参与收益分配的份额 / 10000 × 当日每万份收益。七日年化不能除以 365 当作实际日收益，结转规则按具体基金处理。[AKShare 货基字段](https://akshare.akfamily.xyz/data/fund/fund_public.html)、[华夏基金万份收益说明](https://www.chinaamc.com/portal/cn/hxxc/2012/201201xjgj/index.html)

用户至少需提供基金代码及份额类别、已确认份额、建账基准日期；累计盈亏还需成本、申购赎回、费用与分红记录。接口不会自动提供支付宝或银行里的个人持仓。建议保存净值日期、按持仓与净值区间防重复入账，无新净值时显示截至日期，申购赎回记转账，涨跌单独记投资盈亏。此部分是实现建议，尚未开发。


## 同花顺扶摇站点的主体身份核对（2026-10-13）

结论：有较强证据支持 `aicubes.cn` 属于同花顺体系；`fuyao.aicubes.cn` 当前产品身份可作有保留的同花顺关联判断，不能只凭该站或 GitHub 自称“官方”作严格证明。

- 同花顺 2019 年年度报告的域名资产表将 `aicubes.cn` 注册所有人列为“智富软件公司”，注册日为 2018-09-07、当时登记的到期日为 2022-09-07。这是历史归属证据，不能当作当前域名登记结果。[同花顺官网托管的 2019 年年报](https://notice.10jqka.com.cn/api/pdf/424af9ae8a6012ce.pdf)
- 深交所托管的同花顺 2025 年年度报告（2026 年 3 月披露），第 5 页定义“智富软件公司”为“浙江同花顺智富软件有限公司”，第 125 页子公司表列出该公司持股比例为 100.00%。这证明子公司关系的现行披露依据，但该年报全文未找到 `aicubes`。[深交所 2025 年年报](https://disc.static.szse.cn/disc/disk03/finalpage/2026-03-10/be3ef956-362a-4b45-ad74-d931cdae96c1.PDF)
- 主代理读取扶摇站点前端源码，发现认证代码使用 `upass.aicubes.cn` 或 `upass.10jqka.com.cn`，按环境选择。这是技术关联的辅助证据，来源仍由该站提供，不能独立证明其身份。[站点当前前端脚本](https://fuyao.aicubes.cn/assets/js/main.7f88f5fe.js)

本次没有完成工信部备案主体的实时核验，也没有找到同花顺主站对 `fuyao.aicubes.cn` 的现行独立回链。因此适合表述为“证据支持同花顺体系关联”，不适合表述为“当前官方身份已经完全独立核验”。未安装任何 CLI / skill，未登录、申请或创建 API Key。

## 扶摇基金接口的接入适用性（2026-10-13）

文档提供 `GET /api/fund/performance/nav`（净值）与 `GET /api/fund/performance/returns`（区间收益）。净值请求要求完整 `thscode`，保留市场后缀；`nav_type=unit` 获取计算持仓市值所需的单位净值。`range=week` 等返回区间序列，不传时最多最新一个净值日期。返回 `nav_date` 为毫秒 Unix 时间戳，日期按上海时区处理；`adj_nav` 是复权净值，不等同于累计净值。[基金业绩文档](https://fuyao.aicubes.cn/docs/api-reference/fund-performance/)

`GET /api/fund/corporate-actions/dividends` 提供分红记录，`per_ten_cash_after_tax` 是每 10 份税后现金分红，需要换算到具体持有份额；仍须结合个人权益登记、现金分红或再投资选择。[分红文档](https://fuyao.aicubes.cn/docs/api-reference/fund-corporate-actions/)

接入要求使用同花顺账号登录、在 API Key 管理页创建 Key，并在请求头传 `X-api-key`。主代理未登录、未签发 Key，直接请求文档的 `510300.SH` 净值示例，返回 HTTP 200、业务 `code=2003`、`message="Missing X-api-key"`、`data=null`。这仅验证该路由存在并要求鉴权，尚未验证授权后实际取数、具体场外基金覆盖或更新时间。[接入说明](https://fuyao.aicubes.cn/docs/introduction/)、[Key 管理](https://fuyao.aicubes.cn/admin/)

`llms.txt` 当前说明不限制累计调用次数，但存在动态限流，HTTP 429 或业务 `code=4001` 需降低并发并延后重试。这不能推论为永久免费或没有请求频率限制。项目现有 Go 后端可以直接使用 REST HTTP，不必安装 MCP、CLI 或 Python SDK；这是技术接入判断，尚未开发。[文档索引与限流说明](https://fuyao.aicubes.cn/llms.txt)

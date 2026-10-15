# 建设银行个人黄金积存双边报价接口

核对日期：2026-10-15（北京时间）。本次仅研究与只读请求，未接入业务代码、未登录个人银行账户、未进行交易。

## 结论

建设银行官网确实有用于显示个人黄金积存**主动积存价（客户买入）与赎回价（客户卖出）**的网页内部 JSON 端点。主代理已用临时匿名会话请求成功，返回报价时间及双边价格；不需要 API Key 或个人账户登录。它与面向合作伙伴、具有正式接入流程的“开放平台 API”是两类东西。本次没有找到该黄金积存报价端点的正式开放 API 文档、外部调用授权、限频承诺或 SLA。[官网报价页面](https://www2.ccb.com/chn/home/gold_new/gjs/index.shtml)、[官网页面脚本](https://www2.ccb.com/uiFramework/commonResource/zip/cn/cn/gold_new/js/operate.js)、[建设银行开放平台](https://open.ccb.cn/)

## 网页端点与实测

官网 `operate.js` 中，`initiativePriceCondition.port` 指向以下报价端点；页面约每 6000 ms 调用一次。这个间隔只是网页当前实现，不能推导为银行承诺的行情延迟或允许外部程序采用的限频。[官网页面脚本](https://www2.ccb.com/uiFramework/commonResource/zip/cn/cn/gold_new/js/operate.js)

```text
GET https://www2.ccb.com/tran/WCCMainPlatV5?CCB_IBSVersion=V5&SERVLET_NAME=WCCMainPlatV5&TXCODE=NGJS01
```

主代理本机于本日进行只读实测：直接裸请求 `NGJS01` 时，得到提示重新登录的错误 `0060Z1108006`；先以临时匿名 Cookie 会话访问以下初始化地址，再在同一会话请求 `NGJS01`，即可成功。测试没有个人账户登录，也没有 API Key；Cookie 值没有记录到本笔记。

```text
GET https://www2.ccb.com/tran/WCCMainPlatV5?CCB_IBSVersion=V5&SERVLET_NAME=WCCMainPlatV5&TXCODE=100119
GET https://www2.ccb.com/tran/WCCMainPlatV5?CCB_IBSVersion=V5&SERVLET_NAME=WCCMainPlatV5&TXCODE=NGJS01
```

以下为新匿名会话复查成功响应中与报价有关的字段节选，报价时间为 **2026-10-15 19:30:57.911（北京时间）**，不是接口长期固定返回值：

```json
{
  "SUCCESS": "true",
  "retCode": "000000",
  "PM_Txn_Vrty_Cd": "33",
  "CcyCd": "156",
  "Cst_Buy_Prc": "899.04",
  "MdlRate": "896.04",
  "Cst_Sell_Prc": "893.04",
  "Tms": "2026-10-15 19:30:57.911"
}
```

| 字段 | 官网显示及用途 | 本次实测值 |
|---|---|---:|
| `Cst_Buy_Prc` | 脚本写入 `initiativeBuyPrice`，页面标注为主动积存价，即客户买入价，元/克 | 899.04 |
| `Cst_Sell_Prc` | 脚本写入 `initiativeSellPrice`，页面标注为赎回价，即客户卖出价，元/克 | 893.04 |
| `MdlRate` | 返回的中间值；不能代替双边交易报价 | 896.04 |
| `Tms` | 报价时间，应与采集时间分别保存 | 2026-10-15 19:30:57.911 |

字段与页面含义的映射来自官网脚本及页面标签，不按常见“银行买入/银行卖出”的命名习惯猜测。实测值和匿名会话行为来自主代理本机只读请求；笔记作者没有将一次成功扩大为连续运行可靠性的证明。[官网脚本](https://www2.ccb.com/uiFramework/commonResource/zip/cn/cn/gold_new/js/operate.js)、[官网报价页面](https://www2.ccb.com/chn/home/gold_new/gjs/index.shtml)

初次成功报价时间为 19:30:31.906，复查时推进至 19:30:57.911；后者再隔 7 秒查询时仍返回同一 `Tms`。这说明数据会推进，但不能保证每次轮询都有新源报价。PowerShell 复现可先用 `Invoke-WebRequest -SessionVariable ccbQuoteSession` 请求初始化地址，再用 `Invoke-RestMethod -WebSession $ccbQuoteSession` 请求报价地址；响应可能标记为 `text/html` 并被返回为字符串，此时需 `ConvertFrom-Json`。

同会话还可访问 `TXCODE=NGJS02&PM_Txn_Vrty_Cd=23`，实测返回定期买入价 `Cst_Buy_Prc=895.57`、`Tms=10:30:00`，同时带有实时赎回价 `Cst_Sell_Prc=893.04` 及其 `Mkt_Ofr_Tm=19:30:57.911`。这里两个时间字段语义不同，故当前双边实时查询主路径建议仅使用 `NGJS01`，避免把定期价的 10:30 时间当作实时双边报价时间。

另一个可直接读取的官方 XML 是 `https://www2.ccb.com/cn/home/news/gjshjjc01.xml`。本次样本报价时间为 10:30，与定期价一致；仅凭此样本不能证明该文件永远保持固定口径，也不能将其当作当前实时双边价的替代数据源。[官方 XML](https://www2.ccb.com/cn/home/news/gjshjjc01.xml)

## 产品口径

建行对个人黄金积存明确区分主动积存价、定期积存价与赎回价：主动积存价对应客户主动买入；赎回价对应客户卖出；定期积存价按规则取当日上午 10:30 的主动积存价。报价单位为人民币元/克。手机银行、网上银行等信息渠道的展示价也属于参考价格，实际成交以业务系统最终成交记录为准。[建行产品介绍及交易规则](https://www.ccb.com/chn/home/gold_new/cpjs/p_rule.shtml)、[建行 2025 年 11 月规则 PDF](https://www.ccb.com/chn/attachDir/2025/11/2025111117101889791.pdf)

因此，本项目若要估算持有黄金积存可赎回金额，应使用**克数 × 赎回价**作为参考估值，并显示报价时间；沪金期货、上金所 Au99.99、伦敦金、银行账户黄金及积存金中间价均不能自动当作这笔建设银行个人黄金积存的赎回价。此计算为基于报价口径的实现建议，尚未实现。[建行产品规则](https://www.ccb.com/chn/home/gold_new/cpjs/p_rule.shtml)

不同官方页面及不同年份 PDF 的交易时段存在版本差异，本次未据此给出固定交易日历。接入时应检查实际返回时间和状态；不要仅凭 HTTP 200 或请求时刻判断“实时”。

## 正式开放平台与第三方接口

建设银行开放平台的官方首页列明合作伙伴接入流程为应用申请、产品申请、证书申请、开发测试、上线申请。建行云的官方介绍也说明，开放银行为第三方合作伙伴提供注册、认证、申请和使用等管理服务。本次公开检索没有找到可供个人开发者直接领取 Key 调用的“个人黄金积存主动积存价/赎回价” API 文档；这只能表述为**未找到公开文档**，不能断言建行对所有合作机构都没有此能力。[建设银行开放平台](https://open.ccb.cn/)、[开放银行管理平台官方介绍](https://yun.ccb.com/universal/openbank)

第三方极速数据公开提供 `GET https://api.jisuapi.com/gold/bank?appkey=...`，文档类别是“银行账户黄金价格”，没有明确承诺覆盖建设银行个人黄金积存的主动积存与赎回双边报价；示例还出现伦敦金和期货数据，因此不能据此认定满足本需求。其官方旧示例代码有 `buyprice` / `sellprice` 字段，但同样不能证明是目标积存产品。本次未注册第三方账户、未用付费 Key 实测。[极速数据黄金 API 文档](https://www.jisuapi.com/api/gold/)、[极速数据官方银行账户黄金示例](https://www.jisuapi.com/code/741)

目前最直接、且已证实能返回目标产品双边报价的来源是上述**建设银行官网内部端点**。若用于项目，建议封装独立数据源、限制请求频率、处理匿名会话失效，并保存来源、报价时间、采集时间及错误状态。接口稳定性与允许的使用范围仍需以建行正式说明为准。

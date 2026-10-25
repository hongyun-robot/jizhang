# 苏银 J07241 动态校验诊断与复现

核对日期：2026-10-16（北京时间）。本次只查询官方公开产品，不登录个人银行、不使用或复制桌面已有 Cookie。此前的独立采集 400 已找到可复现的原因组合；短期取数已成功，多日准入仍未完成。

## 已验证的请求链路

1. 新匿名上下文访问 `https://www.suyinwealth.com/lccs/cpxq/J07241`，首次可返回 412，响应下发 `FSSBBIl1UgzbN7NO`（HttpOnly）。页面含 `$_ts.nsd`、`$_ts.cd` 并加载挑战脚本。
2. 官网脚本运行后生成 `FSSBBIl1UgzbN7NP`；Cookie 正常发送、未被浏览器阻止，并不保证环境校验通过。失败与成功样本均观察到长度为 235 的 NP，不能以长度判成功。
3. 页面进入 200 后建立应用 `JSESSIONID`，继续加载页面脚本。正常页面通过脚本发起净值请求，URL 可附加 `XhC8Wb`。
4. 成功的图表“成立以来”请求为 `POST /web/fundNav/getNav`，正文保持以下表单：

```text
fundCode=J07241&dateType=all&rows=9999&page=1
```

`$_ts.nsd` 和 `$_ts.cd` 是本次返回页面中的脚本输入，不是需要自行添加到净值 POST 的业务字段。其内部算法与全部输入绑定关系本次未完全逆向；实现让原始官网脚本在新匿名环境中生成校验状态，不写死这些值。

## 参数与状态分类

| 项目 | 实测来源与用途 | 当前实现 |
|---|---|---|
| fundCode/dateType/rows/page | 净值接口业务表单，目标为 A 份额 J07241 | 固定受控产品及历史范围，不猜份额 |
| FSSBBIl1UgzbN7NO | 初始响应服务端 Set-Cookie，HttpOnly，样本长度 87 | 由本次匿名上下文接收 |
| FSSBBIl1UgzbN7NP | 官网脚本执行后产生的 Cookie | 自动执行脚本生成，不复制历史值 |
| JSESSIONID | 通过页面校验后的应用会话 Cookie，样本长度 32 | 跟随本次匿名会话；本次移除它会使 HTTP 查询失败 |
| XhC8Wb | 页面脚本改写请求时附加的动态 URL 参数，净值 POST 样本长度 96 | 原生浏览器请求使用脚本生成值；不人工拼接或写死 |
| User-Agent 与自动化标志 | 影响匿名挑战执行结果的环境因素 | 使用实际浏览器的普通 Chrome UA，并配置对应运行标志 |
| Content-Type/Origin/Referer/X-Requested-With 等 | 本次成功 HTTP 查询的请求头上下文 | 表单编码、同源引用，UA 与生成会话时保持一致 |

所有持久化诊断只包含 Cookie 名称、长度、属性和参数名，不保存 Cookie 原值、签名值或个人会话。

## 环境对照

在同版本本机 Chrome 155、相同目标/匿名会话策略下，每次创建新上下文：

| 组合 | 结果 |
|---|---|
| 默认 HeadlessChrome UA、默认 webdriver | 412 → 400 |
| 仅调整自动化标志，UA 保持 HeadlessChrome | 412 → 400 |
| 仅普通 Chrome UA，webdriver 保持 true | 412 → 400 |
| 两项同时调整 | 412 → 200，净值接口 200、业务码 0000、265 条 |

实现使用 `--disable-blink-features=AutomationControlled`，并通过浏览器自身 `Browser.getVersion` 获取实际 UA，只将 `HeadlessChrome/` 替换为 `Chrome/`，保留真实平台和版本。不伪造一个固定 Windows UA 给 Linux；保持 headless、非 root、Chromium sandbox、超时及匿名上下文隔离。

上述对照证明当前修复组合有效，不能推断已经列全网站所有环境检查项，也不能当作未来稳定性保证。证据位于 `openspec/changes/archive/2026-10-20-add-bank-wealth-accounts/evidence/2026-10-16-challenge/`。

## 直接 HTTP 对照结果

在本机与隔离 Linux 容器中均完成了同类验证。Cookie 由本次独立匿名浏览器生成，HTTP 查询使用 Node fetch，不使用浏览器的网络请求通道：

| 请求 | 结果 |
|---|---|
| 正常浏览器及新生成的签名请求 | 200 / 0000 / 265 条 |
| 将同一签名 URL 复制给 HTTP 客户端重放 | 400 |
| 不附 XhC8Wb，保留完整匿名 Cookie 及请求头上下文 | 200 / 0000 / 265 条 |
| 只带 NO/NP，移除 JSESSIONID | 412 |
| 不带 Cookie | 412 |

因此，本次验证的 HTTP 路线不要求自行计算 `XhC8Wb`：先自动建立有效匿名会话，再用相同上下文发送无签名表单即可。签名重放失败的具体绑定机制尚未证明，不能宣称它一定是“一次性”或仅绑定时间。此结果是当前接口的实验观察，不是官方开放 API 契约。

生产采集器目前仍沿用浏览器原生数据请求，HTTP 路线保留在诊断工具中，避免未经多日验证就替换正式数据通道。两者都不把匿名 Cookie 发给记账前端，也不依赖用户个人银行身份。

## 同时修正的采集器问题

- “成立以来”在图表和过往业绩区域有重复文字，原来的文字选择器会歧义失败。现在定位图表控件 `#allbtn`；真实浏览器的重复文字回归用例先失败后通过。
- 产品名称可能先于登记编码异步出现。现在分别等待名称和登记编码可见，再核对完整身份，避免把未加载完成误判为产品不符；80ms 延迟登记编码的回归用例先失败后通过。
- 运行日志只记录枚举错误代码及 HTTP 状态，原始会话、Cookie、动态签名和未脱敏浏览器异常不对外输出。

## 复现

在 `collector/` 中安装依赖并指定已安装的 Chrome，显式设置 `WEALTH_LIVE_SMOKE=1`：

```powershell
npm test
npm run smoke
node challenge-diagnostic.mjs baseline
node challenge-diagnostic.mjs automation-flag
node challenge-diagnostic.mjs ua-only
node challenge-diagnostic.mjs normal-ua --http-replay
```

浏览器执行文件配置仍为 `WEALTH_BROWSER_EXE`；Linux 镜像内使用固定版本官方 Chromium。每次运行自动建立新匿名上下文并回收，命令不接受 Cookie 原值或任意 URL。

本机及服务器成功样本均为 265 条，最新净值日期 2026-10-15、单位净值/累计净值 1.02142；这只是该次抓取快照，不进入任何个人资产种子。多日准入、理财账户及财务功能仍按原 OpenSpec 任务逐项实施，不能将本轮诊断当作全部功能完成。

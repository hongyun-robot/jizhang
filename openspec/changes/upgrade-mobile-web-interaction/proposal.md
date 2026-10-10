# Proposal

## Why

本变更是交互修复路线的**阶段 3(体验升级)**,与 `improve-mobile-web-interaction`(阶段 1+2)配套但独立验收。阶段 1+2 解决"坏掉的"(断点、热区、反馈),阶段 3 解决"用着别扭的":流水筛选一切 Tab 就丢、长列表只有小分页没有移动端惯用的滑动加载、iOS 软键盘会盖住记账弹窗的提交按钮、筛选栏在手机上摊成五六行把列表挤出首屏、固定噪点层与毛玻璃在低端机上滚动掉帧、统计图表对读屏完全不可见。

**前置依赖**:`improve-mobile-web-interaction` 必须先实施完成(错误态组件 `QueryState`、移动端热区、断点统一是本变更的接入点)。两个变更可跨会话分批推进,本变更建议在前者归档后再开工。

**范围较大,按会话拆分**:tasks 按 6 个独立任务组编排,每组自带验证、可独立提交,适合多次会话续做。

## What Changes

**流水状态持久化与列表体验**
- 流水页筛选条件(range/kind/account/category/keyword/金额区间)与页码序列化到 URL searchParams,底部 Tab 切换、浏览器前进后退、外部分享链接均可恢复;既有 `locateId` 跳转定位逻辑保持不回归。
- 移动端列表:翻页后回到列表顶部;上拉加载下一页(保留分页控件作兜底);下拉刷新当前区间。
- 筛选栏移动端收纳:折叠为「筛选 + 条件摘要」入口,展开为抽屉/面板,已选条件常驻可见,首屏优先展示列表。

**键盘与弹窗**
- 监听 `visualViewport`,移动端软键盘弹出时记账/分类/账户/周期记账等弹窗的内容与底部操作栏保持可视,提交按钮不被键盘遮挡;组件卸载时清理监听。

**性能与可访问性**
- 移动端降级固定噪点层与底部 Tab 的 `backdrop-filter` 强度,消除与噪点层 z-index 叠加的滚动开销;噪点层 z-index 不再盖过 Tab 栏。
- 统计图表提供文本替代(图表容器语义标注 + 可展开的同数据表格),读屏与不熟悉手势的用户可获取数据。
- 窄屏登录首屏直接可见登录表单(装饰样张压缩/隐藏),不再把表单推到折叠线以下。
- 断点常量收敛:JS 断点(`useBreakpoint`)与 CSS 媒体查询统一到同一套值;移动端底部留白规则合并为单一来源。

**逐页窄屏挤压修复(不新增 spec,由既有 `ui-shell`「手机访问 → 页面单列布局」要求覆盖)**
- 预算行(名称+输入框+进度条)、账期摘要四列、券表单两列、统计/首页双列栅格在 ≤600px 的换行与单列化。

## Capabilities

### New Capabilities

(无)

### Modified Capabilities

- `transactions`: 增补筛选状态跨路由保留(URL 持久化)、移动端列表加载方式(上拉加载/翻页回顶/下拉刷新)、筛选栏移动端收纳。
- `ui-shell`: 增补移动端软键盘不遮挡弹窗主操作、移动端滚动性能约束(噪点层与毛玻璃)、窄屏登录首屏表单直接可见。
- `statistics`: 增补统计图表的文本替代(可及性)。

> 统计页的**错误态**仍沿用阶段 1+2 在 `ui-shell` 中确立的跨页面错误态要求,本变更不重复添加;预算/账期/券的窄屏挤压由 `ui-shell` 既有"手机访问 → 单列布局"要求覆盖,只修实现不加 delta。

## Impact

**受影响代码(全部在 `frontend/src`)**
- `pages/Records.tsx` — searchParams 序列化、上拉加载、下拉刷新、筛选收纳接入
- `components/PeriodPicker.tsx`、`styles.css` 筛选栏相关(795-805) — 移动端收纳面板
- `components/TxnFormModal.tsx`、`pages/Categories.tsx`、`pages/Recurring.tsx`、`pages/Accounts.tsx` — 键盘避让 hook 复用
- 新增 `hooks/useViewportKeyboard.ts`(或等价实现)— `visualViewport` 监听与清理
- `styles.css`、`liability.css` — 噪点层/毛玻璃降级、断点常量收敛、底部留白合并、逐页 ≤600px 挤压修复
- `pages/Login.tsx` + `styles.css` 登录媒体查询 — 窄屏首屏
- `components/EChart.tsx`、`pages/Stats.tsx` — 图表文本替代
- `frontend/e2e/` — 新增 `interaction_upgrade.mjs`

**依赖 / 基础设施**:无新增依赖(上拉加载/下拉刷新优先用原生 `touch`/`IntersectionObserver` + 既有 React Query `fetchNextPage` 能力评估;若确需第三方库,须在 design 阶段单独论证并征得用户同意)。

**规范约束(不得回归)**
- `specs/transactions`:offset 分页默认 20 条、区间筛选与统计页共用选择器语义 → URL 持久化 MUST NOT 改变服务端查询参数含义;分页控件在上拉加载下仍可用(兜底)
- `specs/ui-shell`:四 Tab 枚举不变;`AppShell.tsx` 的 `locateId` 让行逻辑与 `Records.tsx:137-149` 定位流程不回归
- `specs/recurring-entries`:"打开页面懒补齐"定时器不动
- 阶段 1+2 的成果(热区、错误态、reduced-motion)不得回归

**风险**
- URL 持久化与 `locateId`(router state)两套机制并存 → 设计阶段明确分工:URL 存筛选、state 存一次性定位
- 上拉加载与浏览器原生滚动/分页控件冲突 → 保留分页兜底,可开关
- `visualViewport` 在 iOS/Android 行为差异 → 需真机或 Playwright 设备描述符验收

## 会话拆分建议

| 会话 | 任务组 | 产出 |
|---|---|---|
| 1 | 组 1(URL 持久化) | 筛选不丢,可分享链接 |
| 2 | 组 2(列表加载) | 上拉加载 + 翻页回顶 + 下拉刷新 |
| 3 | 组 3(筛选收纳) | 移动端首屏回归列表 |
| 4 | 组 4(键盘避让) | 提交按钮永不被键盘遮挡 |
| 5 | 组 5(性能 + 可及性) | 滚动流畅、图表可读、登录首屏 |
| 6 | 组 6(收敛 + 集成验收) | 断点/留白收敛、全量回归 |

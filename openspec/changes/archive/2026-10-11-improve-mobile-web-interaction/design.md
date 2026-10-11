# Design

## Context

动机与范围见 `proposal.md`。此处只记录影响技术方案的既有事实（2026-10-11 审计时点，行号以当时文件为准）：

- **双形态导航**：`components/AppShell.tsx` 用 `Grid.useBreakpoint()` 的 `!screens.md`（768px）选形态（`AppShell.tsx:171-201`），而 `<Sider breakpoint="lg" collapsedWidth={0}>`（`AppShell.tsx:105`）在 <992px 就把侧栏收成 0 宽 → 768–991px 只剩 antd 自动生成的无标签触发器。
- **固定轨道裁切**：`styles.css:961` `.cat-section { repeat(auto-fit, minmax(480px,1fr)) }`、`styles.css:1349` `.rule-list { minmax(470px,1fr) }` 与 `styles.css:508` `.ant-card.panel { overflow:hidden }` 叠加，窄屏右半区被裁。
- **控件高度**：`components/ThemeProvider.tsx:48` `controlHeight: 34`（antd 派生 small=25.5、large=42.5）；分类磁贴 `styles.css:1101-1111`（padding 3px 12px，约 28px 高）；行内操作为 `type="link" size="small"`（`Records.tsx:226-243` 等）。
- **hover 不对称**：`.row-actions/.txn-actions { opacity:.6 }`（`styles.css:893-901`），提亮包在 `@media (hover:hover) and (pointer:fine)`（`styles.css:902-907`）里，压暗没有对应保护 → 触屏恒为 0.6。
- **reduced-motion 遗漏**：关停清单在 `styles.css:2416-2454`，未覆盖登录页入场（`styles.css:2006/2014/2020/2054`）与 `tr.flash > td`（`styles.css:1931-1934`）。
- **错误态**：`Records.tsx:405,447-451`、`Dashboard.tsx:171,207-214`、`Stats.tsx:223-234` 均以 `!data && !isError` 判骨架、其后一律落 `Empty`；`api/queryClient.ts:2` 仅 `retry:1`。
- **弹层挂载**：全库无 `getPopupContainer`，Modal（`TxnFormModal.tsx` / `Accounts.tsx` / `VoucherFormModal.tsx` / `Recurring.tsx`）内下拉默认挂 `document.body`。
- **既有让行逻辑**：`AppShell.tsx:195-200` 路由复位跳过 `state.locateId`，`Records.tsx:137-149` 负责 `scrollIntoView` + 闪烁 + 清 state，**必须保留**。
- 约束：无新依赖；桌面视觉零变化；四 Tab 枚举与铃铛工具位不变。

## Goals / Non-Goals

**Goals**
- 断点、热区、hover 对称、reduced-motion、对比度、错误态、防重、内联校验、弹层挂载、语义化、滚动复位，共 15 项修复（见 tasks.md 阶段 1/2）
- 每项都可在 ≤767px 与 ≥992px 两个视口独立验收
- 改动集中在 `styles.css` + 少量组件，便于跨会话分批提交

**Non-Goals**
- 不做阶段 3 项（URL 筛选持久化、上拉加载、iOS visualViewport 键盘避让、筛选栏收纳、断点常量收敛、图表 aria）→ 另一变更 `upgrade-mobile-web-interaction`
- 不改桌面（≥968px 以上既有观感）任何尺寸与间距
- 不动 `controlHeight: 34`、不引入动画库、不改后端与 API

## Decisions

### D1 断点统一：shell 形态切换改用 `lg`（992px）
- **选择**：`AppShell` 用 `!screens.lg` 切形态，`Sider breakpoint="lg"` 保持不变 → 两者天然一致；768–991px 走底部 Tab 形态。
- **理由**：底部 Tab 在平板竖屏同样自然（拇指可达、与手机一致），改动只有 1 行判断；audit 中该区间现状是"几乎无导航"，任何方案都比现状好。
- **备选（否决）**：`collapsedWidth={64}` 保留图标侧栏 → 需要自绘触发器、图标无标签、与"双形态"spec 的简单心智冲突。
- **注意**：`Grid.useBreakpoint()` 首帧可能 `undefined`（SSR/初始渲染），需按"桌面优先"兜底避免闪烁：`const isMobile = screens.lg === false`（仅明确为 false 才切移动形态，`undefined` 视为桌面）。审计确认当前代码用 `!screens.md` 存在同样首帧问题，顺手修正。

### D2 触控热区：只写在 `≤767px` 媒体查询，不动主题 token
- **选择**：新增一段 `@media (max-width: 767px)` 规则，为 `.ant-btn`、`.ant-select`、`.ant-picker`、`.ant-input-number`、`.ant-segmented`、`.ant-pagination` 设 `min-height: 40px`（`.ant-btn-primary` 与底部 Tab 保持 ≥44px 语义由现有 58px Tab 满足）；分类磁贴 `.ant-tag-checkable` 移动端 `min-height:40px; padding:8px 14px`；行内 `size="small"` link 按钮移动端用 `min-height:40px; padding-inline:8px` 或 `::after` 扩展命中区。
- **理由**：改主题 `controlHeight` 会同时改变桌面密度（spec 明确桌面不变）；纯 CSS 媒体查询可独立提交、独立回滚。
- **风险对冲**：antd v6 的按钮高度由 `line-height + padding` 推导，`min-height` 需同时压 `line-height`（`line-height: 38px` 视口内）以避免内容被居中挤压；实现时以 DevTools 实测为准，验收标准是**命中区**而非计算样式。

### D3 错误态：新建共享组件 `components/QueryState.tsx`
- **选择**：一个组件承接三态（`loading` → 既有 Skeleton / `error` → Alert + 重试按钮 / `empty` → 既有 Empty），props：`loading`、`error`、`isEmpty`、`onRetry`、`emptyDescription`、`skeleton`。接入 `Records`、`Dashboard`、`Stats`。
- **理由**：三页当前各写一份 `!data && !isError` 判断，重复且已出错（错误落空态）；集中一处后续阶段 3 复用（上拉加载仍复用其 error 分支）。
- **备选（否决）**：每页各写 → 三份逻辑再次漂移；用 antd `Result` → 视觉重量过大、与纸面账簿风格冲突，用 `Alert` 换装更贴合既有 `.ant-alert-*` 主题规则（`styles.css:2267-2285`）。

### D4 内联校验：表单顶部固定 `Alert` + 字段级 `rules` 二选一的折中
- **选择**：`TxnFormModal` 在 body 顶部渲染一个受控 `Alert`（错误信息来自 `validate()` 返回值，保持既有校验函数单一来源），同时对账户/分类两个必填 Select 加 antd `status="error"` 描边；Toast 降为"提交成功"与"二次确认超额"提示。
- **理由**：`validate()` 是既有纯函数，返回中文文案，改造成本最小；整表重构成 antd `Form` + `rules` 会牵动 `TxnRefill` 预填、退款超额二次确认等 4 条既有逻辑，风险不成比例。
- **备选（否决）**：完整迁移到 antd `Form` → 任务量翻倍且阶段 3 才需要的表单重构。

### D5 语义化：渐进补齐，不做全站重写
- **选择**：只改审计点名的三处：账户行 `div`（`Accounts.tsx:213-222`）→ 保留行内按钮并把整行点击改为 `<button>` 包裹或 `role="button" tabIndex onKeyDown`；分类 `Tag`（`Categories.tsx:110-118`）与"已退"标签（`Records.tsx:264-273,421-429`）→ 补 `role="button" tabIndex={0}` + Enter/Space 触发（`CategoryPicker.tsx:26-31` 已有同类实现可照抄）。焦点环统一为一条 `:focus-visible` 通配兜底（`styles.css:1942-1948` 扩展）。
- **理由**：`CheckableTag` 自带 role/键盘是既有先例，保持一致即可；整行改 button 会牵动表格语义，收益低于风险。

### D6 滚动复位：显式 `behavior:'instant'`
- **选择**：`AppShell.tsx:198` 改为 `window.scrollTo({ top: 0, behavior: 'instant' })`；`html { scroll-behavior: smooth }` 保留（锚点/`scrollIntoView` 依赖）。
- **备选（否决）**：移除全局 smooth → 会破坏 `Records.tsx:141` 定位跳转的平滑体验。
- **兼容**：`behavior:'instant'` 非旧版规范值，需 `as ScrollBehavior` 类型断言或走 `document.documentElement.scrollTop = 0` 兜底（TS 版本已含 `instant`，以 `tsc --noEmit` 通过为准）。

## Risks / Trade-offs

- [**平板布局回归**：768–991 从侧栏变底部 Tab，侧栏独有的"本期速览"在该区间不再展示] → 底部 Tab 形态本身不含速览（手机端既有如此），属形态一致性的必然结果；spec 未承诺平板展示速览。e2e 补一条 900px 视口导航可达断言。
- [**移动端 min-height 破坏 antd 内部对齐**（按钮图标偏移、Select 文字不居中）] → 逐控件验收；优先 `min-height` + `line-height` 配对，失败则回退为"只给明确可点元素（行内按钮、磁贴、Tab）达标，容器类控件 ≥36px"并在 tasks 中标注降级。
- [**热区变大导致移动端表单变长**（记一笔表单需要滚动）] → 表单 body 已有 `maxHeight + overflowY:auto`（`TxnFormModal.tsx:262`），可容纳；验收时确认提交按钮在滚动可达范围内。
- [**错误态组件接入改动三页渲染分支，可能误伤 loading 骨架与空态样式] → 组件内部完整保留原 Skeleton/Empty 节点，仅新增 error 分支；先接 `Stats`（最简单）再接 `Dashboard`、`Records`。
- [**reduced-motion 是全局关停清单，新增选择器容易漏] → 用 `prefers-reduced-motion` 块统一管理，验收时用 DevTools 的 "Emulate prefers-reduced-motion" 全页扫一遍（登录页、流水定位闪烁、Tab 动画）。
- [**跨会话中断导致半成品]** → tasks 按"可独立提交"粒度拆分，每个任务自带验收标准；阶段 1 完成即可单独提交，不依赖阶段 2。

## Migration Plan

无数据/部署迁移。纯前端改动，分两批发布：

1. **阶段 1 提交**（断点 + 裁切 + 热区 + hover + reduced-motion + Tab 可达性）→ 构建 `pnpm build`（`tsc --noEmit && vite build`）+ e2e 冒烟 → 可独立上线。
2. **阶段 2 提交**（错误态 + 防重 + 内联校验 + 弹层 + 语义化 + 滚动复位 + hero + Login message）→ 同上。

**回滚**：两个提交各自独立 revert 即可；D2 的媒体查询块与 D3 的组件接入无相互依赖。

## Open Questions

- 行内 link 按钮在移动端采用 `min-height` 撑高还是 `::after` 扩展命中区（视觉 vs 热区解耦）—— 实现时按最小视觉扰动原则现场决定，两者都满足 spec 的"命中区 ≥40px"，不影响任务拆分。
- 阶段 3 中"上拉加载"是否最终采用（可能改回增强版分页）—— 该不确定性已在 `upgrade-mobile-web-interaction` 变更中单独处理，不阻塞本变更。

# Design

## Context

动机见 `proposal.md`。影响方案的既有事实（2026-10-11 时点）：

- **前置**：本变更假定 `improve-mobile-web-interaction`（阶段 1+2）已完成 —— 断点统一为 992px、`QueryState` 组件存在、移动端热区达标、错误态模式确立。所有接入点以此为基线。
- **流水筛选**：`pages/Records.tsx:94-130` 用 `useState<FilterState>` + `page` state，`params` 由 `useMemo` 派生后同时喂给 `useTxnList` 与 `exportTxns`；`patch()` 统一改条件并 `setPage(1)`。
- **定位跳转**：`AppShell.tsx:195-200` 路由复位跳过 `location.state.locateId`，`Records.tsx:137-149` 用 `scrollIntoView` + 闪烁 + `navigate replace` 清 state —— **一次性跳转语义，与 URL 持久化是两套机制，必须共存**。
- **分页**：`useTxnList(params)` 是 react-query 的 `useQuery`（`api/hooks.ts`），单页返回 `items + total`；`PAGE_SIZE = 20`。
- **弹窗**：antd `Modal`（portal 到 body），`TxnFormModal.tsx:262` body `maxHeight: calc(100dvh - 160px)` —— iOS 键盘不改变 `dvh`，故底部 footer 被盖住。
- **噪点/毛玻璃**：`styles.css:135-143` 固定噪点层 `z-index: var(--z-grain)=60`，底部 Tab `z-index:20` + `backdrop-filter: saturate(1.4) blur(14px)`（`styles.css:1651-1666`）。
- **断点**：JS 用 `Grid.useBreakpoint()`（768/992），CSS 现存 560/767/860/900/1023 五档（`styles.css:2318,2327,2334,2362,2369`），`.two` 类由 JS 加、被 860px CSS 撤销。
- **图表**：`components/EChart.tsx` 封装 `echarts-for-react`，`Stats.tsx` 三处使用；窄屏饼图已隐藏标签走图例（`Stats.tsx:99-100`）。

## Goals / Non-Goals

**Goals**
- 流水视图状态可分享、可恢复；移动端列表加载符合平台习惯；筛选不抢首屏；键盘不遮挡提交；移动端滚动更流畅；图表数据可被读屏获取
- 无新增运行时依赖（除非 design 明确论证并征得同意）
- 每个任务组独立提交、独立验收，支持跨会话续做

**Non-Goals**
- 不改服务端 API 与查询语义（URL 参数仅是既有 `params` 的序列化）
- 不改桌面端筛选栏布局与分页控件形态
- 不做离线缓存 / PWA
- 不处理阶段 1+2 已修项（防止回归即可）

## Decisions

### D1 URL 持久化：`useSearchParams` 序列化，URL 存筛选、router state 存定位
- **选择**：`Records.tsx` 以 `useSearchParams` 为筛选与页码的单一事实来源；`FilterState` 由 searchParams 派生（`useMemo`），`patch()` 改为 `setSearchParams(..., { replace: false })`（产生可后退的历史条目）；定位跳转仍走 `location.state.locateId`，互不写入。
- **编码**：`start/end`（区间）用 `from`/`to`，其余与既有 `params` 键同名（`kind/account/category/keyword/min/max/page`）；**默认值不写入 URL**（如 `page=1`、默认本月区间省略），保持 URL 短且可读。
- **理由**：URL 是浏览器原生状态容器，零依赖、自动支持前进后退与分享；`replace: false` 让"改筛选 → 后退"符合直觉。
- **备选（否决）**：zustand + sessionStorage → 不可分享、跨标签页行为不一致；只存 sessionStorage → 后退/分享不恢复，未满足 spec。
- **兼容**：`locateId` 清 state 的 `navigate('/records', { replace: true, state: null })` 必须改为**保留当前 searchParams**（`navigate({ pathname: '/records', search, state: null, replace: true })`），否则会把筛选一并清掉 —— 这是本变更最高风险的一行，e2e 必须覆盖。

### D2 列表加载：迁移到 `useInfiniteQuery` + 原生 `IntersectionObserver` 上拉加载
- **选择**：`useTxnList` 为流水页新增无限变体（保留原 `useTxnList` 给统计/导出复用）；`fetchNextPage` 由列表底部的 sentinel `IntersectionObserver` 触发，`isFetchingNextPage` 显示进行中指示并防重复触发；分页控件继续存在，点击时定位到对应 `pages[i]`（缓存命中直接取，未命中 `fetchPage` 回填）并回顶（spec 要求）；下拉刷新自写轻量实现（列表 `scrollTop === 0` 时 `touchstart/touchmove` 下拉，超过阈值松手触发 `refetch`），指示器只动 `transform`。
- **理由**：`useInfiniteQuery` 是 react-query 既有能力，无新依赖；原生 `IntersectionObserver` 比第三方观察器库更省；导出继续用扁平 `params`（取当前全部筛选、不带 page），避免改变 CSV 语义。
- **备选（否决）**：`react-intersection-observer`、`react-virtuoso`（长列表虚拟化）→ 20 条/页 + 追加的规模不需要虚拟化；纯 `setPage` 累加 state → 手写去重与 total 同步，重复造轮子。
- **降级路径**：若上拉加载与分页控件的组合在实现中出现难以调和的冲突（如总页数跳转），保留分页 + 翻页回顶，上拉加载降级为"加载更多"按钮 —— 两条路径都满足 spec，届时在任务备注中记录取舍。

### D3 筛选收纳：移动端 `Drawer` + 单行摘要，桌面 `filter-bar` 不动
- **选择**：≤767px 时 `.filter-bar` 渲染为一行「筛选按钮（带生效条件数徽标）+ 条件摘要（区间 · 类型 · 账户数）」，点开用 antd `Drawer`（`placement="bottom"`，含全部既有控件与"清除"）；≥768px 渲染既有 `filter-bar`，同一份控件组件复用，只是容器不同。
- **理由**：antd `Drawer` 已在依赖内；底部抽屉符合移动端心智；控件复用保证两端行为一致。
- **备选（否决）**：`Collapse` 就地展开 → 仍占首屏纵向空间；`Popover` → 手机上容纳 7 个控件体验差。

### D4 键盘避让：`interactive-widget` viewport 声明 + `visualViewport` hook 双保险
- **选择**：`index.html` viewport 加 `interactive-widget=resizes-content`（Android Chrome 生效）；新增 `hooks/useVisualViewportKeyboard.ts` 监听 `visualViewport.resize/scroll`，计算键盘遮挡高度并以 CSS 变量（`--kb-inset`）写到弹窗容器，Modal body/footer 据此加 `padding-bottom`；组件卸载/`open=false` 时移除监听并复位变量。
- **理由**：iOS Safari 忽略 `interactive-widget`，必须走 `visualViewport`；两者互补，hook 可复用于 4 个弹窗。
- **备选（否决）**：只改 `maxHeight` 计算 → `dvh` 在 iOS 键盘下不变，算不出来；仅延后 `autoFocus` → 缓解但不解决"滚动到底提交仍被盖"。
- **注意**：antd Modal 的 DOM 在 portal 中，hook 需挂到 Modal 内部容器节点而非 body；监听回调用 `requestAnimationFrame` 合帧，避免滚动期间高频写样式。

### D5 移动端性能：噪点降级 + Tab 毛玻璃减负，桌面零变化
- **选择**：`--z-grain` 与 Tab 层级关系修正（噪点层降到 Tab 之下，或移动端噪点 `opacity` 降至 0.015 甚至 `display:none`，取实测帧率决定）；≤767px 下 Tab `backdrop-filter` 从 `saturate(1.4) blur(14px)` 降到 `blur(8px)` 或改纯色 + 半透明背景。
- **理由**：噪点 + blur 双滤镜叠加是移动端滚动合成开销的主要来源；两者都是纯 CSS 开关，可独立回滚。
- **验收口径**：Playwright 截图 + 手动在真机/ DevTools Performance 采样"滚动 3 秒长列表"的掉帧情况，写入任务完成备注（不设硬性 FPS 门槛，以"无明显掉帧"为准）。

### D6 断点收敛与留白合并：`560/767/860/900/1023` → `600/768/992`
- **选择**：CSS 断点收敛为三档（≤600 窄屏单列化、≤767 移动形态细节、<992 以下形态由 JS 主导）；删除与 JS 打架的 860 档（`.two` 栅格列数交给 CSS 单一来源）；移动端底部留白（`styles.css:437-440` 与 `2369-2372`）合并为 `.mobile-shell` 单一来源。
- **理由**：两套断点并存是 768–860 区间布局抖动的根因；收敛后 JS 只管形态、CSS 只管排版。
- **风险对冲**：逐条规则迁移并对照三档视口截图（375/900/1440），任何视觉 diff 都需人工确认属"修复"而非"意外"。

### D7 图表文本替代：`role="img"` + `aria-label` 摘要 + `<details>` 数据表格
- **选择**：`EChart.tsx` 容器加 `role="img"` 与由 option 数据生成的 `aria-label` 摘要；`Stats.tsx` 每个图表下挂 `<details><summary>查看数据表格</summary><table>…</table></details>`，表格由该图的同一份聚合数据渲染（纯 HTML，受 `prefers-reduced-motion`/主题变量约束，样式内联到 `styles.css` 现有表格规则）。
- **理由**：渐进增强、不改视觉；`<details>` 原生键盘可用，无需状态管理；同一数据源保证表格与图一致。
- **备选（否决）**：ECharts 官方无障碍扩展 → 体积与定制成本高；仅 `aria-label` → 读屏只能拿摘要、拿不到逐项数值，不满足 spec 场景 2。

## Risks / Trade-offs

- [**URL 持久化清掉 locateId 或反之**] → D1 明确两机制分工；e2e 必测"首页点流水定位 → 筛选保留 → 定位高亮仍生效"与"改筛选后后退恢复"。
- [**`useInfiniteQuery` 迁移影响导出/统计共用的 params**] → 只为流水列表新增无限变体，原 hook 不动；导出继续传扁平筛选（不含 page）。
- [**上拉加载与下拉刷新手势抢占原生滚动**（列表顶部下拉 vs 继续上滑）] → 只在 `scrollTop === 0` 且位移向下时接管，阈值 + `touch-action: pan-y` 约束；实现中若与浏览器返回手势冲突，按 D2 降级路径回退。
- [**分页控件跳页与无限缓存不一致**（第 5 页从未加载）] → 缓存命中直接取、未命中回填该页；跳页后 sentinel 重新武装，避免立即又加载相邻页造成跳页感。
- [**`visualViewport` 在 iOS 上 scroll/resize 触发时机与 Modal 动画打架**] → 只在弹窗 `open` 期间监听，`rAF` 合帧，键盘动画期用 CSS transition 跟随而非硬跳。
- [**断点收敛引起既有布局漂移**] → D6 的三档截图对照验收；发现非预期 diff 先回滚该条再单独评估。
- [**跨会话中断**] → 任务组即提交单元（proposal 的会话拆分表），每组结束跑 `pnpm build` + 相关 e2e 后提交；`.openspec.yaml` 与 tasks 勾选状态是续做断点。
- [**阶段 1+2 尚未实施就开工本变更**] → 组 0（前置检查）显式验证：断点、`QueryState`、热区已落地，否则先完成前者。

## Migration Plan

1. **前置检查**（组 0）：确认 `improve-mobile-web-interaction` 已实施（`QueryState` 存在、shell 断点为 lg）。
2. 按组 1→6 顺序实施，每组：实现 → 本组 e2e 断言 → `pnpm build` → 独立 commit。
3. e2e 统一收敛到 `frontend/e2e/interaction_upgrade.mjs`（随组追加断言），最终与阶段 1+2 的两个脚本、既有 `walkthrough/darkmode` 一起跑全量。

**回滚**：组级 revert 即可；D1（URL 持久化）与 D2（无限加载）是最大两块，各自独立、互不依赖，可单独撤回而不影响其余组。

## Open Questions

- 下拉刷新的最终去留：实现后按真机手感评估，若与列表滚动冲突明显则保留"顶部刷新按钮 + 下拉触发 refetch"的轻量形态。**不改变 spec**（spec 只要求"可刷新"，两条路径都满足），不影响任务拆分。
- 上拉加载与分页控件长期并存的必要性：上线后观察使用数据，可在未来变更中精简为单一路径；本变更按 spec 保留兜底。

# Tasks

> 验收环境:`cd frontend && pnpm dev`（:5600）；e2e 沿用 `walkthrough.mjs` 的写法（`node e2e/<脚本>.mjs`，PASS/FAIL 打印 + `shots/` 截图）。构建：`cd frontend && pnpm build`。
> **前置**：`improve-mobile-web-interaction`（阶段 1+2）必须已实施。每组独立提交，可跨会话续做（组号即续做断点）。

## 0. 前置检查

- [x] 0.1 确认阶段 1+2 已落地：`components/QueryState.tsx` 存在、`AppShell.tsx` 形态切换基于 `lg` 断点、`styles.css` 存在 ≤767px 热区块；验证：三项逐一打开文件确认，任一缺失则先回去完成前一变更，不开工本变更

## 1. 流水筛选 URL 持久化

- [x] 1.1 改造 `Records.tsx`：`useSearchParams` 成为 `FilterState` 与 `page` 的单一事实来源，`patch()` 改 `setSearchParams`（push 历史），默认值（本月区间、`page=1`、空条件）不写入 URL；验证：改筛选后地址栏出现 `?kind=expense&...`，刷新页面条件保持
- [x] 1.2 保住 `locateId` 定位链路：`Records.tsx:144/148` 的 `navigate('/records', { replace: true, state: null })` 改为携带当前 `search`；验证：e2e 断言"首页点最近流水 → 定位高亮生效 → URL 筛选条件仍在 → 定位后二次导航不重触发"
- [x] 1.3 前进/后退与分享恢复：验证：e2e 断言"设筛选 A → 改为 B → 后退恢复 A 且列表刷新"、"复制 URL 到新页面打开恢复 B"
- [x] 1.4 新建 `frontend/e2e/interaction_upgrade.mjs`（沿用 `walkthrough.mjs` 的 `ok/login/shot` 结构），落地 1.1–1.3 断言；验证：`node e2e/interaction_upgrade.mjs` 全部 PASS

## 2. 移动端列表加载

- [x] 2.1 新增流水列表的无限查询变体（`useInfiniteQuery`），保留原 `useTxnList` 给统计/导出复用；导出继续传扁平筛选（不含 page）；验证：桌面分页与导出行为与改动前一致（`node e2e/export.mjs` PASS）
- [x] 2.2 上拉加载：列表底部 sentinel + 原生 `IntersectionObserver` 触发 `fetchNextPage`，`isFetchingNextPage` 期间不重复触发并展示进行中指示；验证：e2e 在 375px 视口滚到底 → 断言新增行出现、请求数 +1，快速连滚 → 仍只 +1
- [x] 2.3 分页控件兼容 + 翻页回顶：点击分页切页时缓存命中直取、未命中回填，加载后列表回到顶部；验证：e2e 点第 3 页 → 断言 `scrollY` 归 0 且首条为第 41 条
- [x] 2.4 下拉刷新：列表 `scrollTop === 0` 下拉超阈值松手触发 `refetch`（指示器只动 `transform`，`prefers-reduced-motion` 下无过渡）；验证：e2e touch 下拉 → 断言触发一次刷新请求并展示最新数据；刷新失败 → 走 `QueryState` 错误态 + 重试
- [x] 2.5 本组 e2e 断言追加进 `interaction_upgrade.mjs`；验证：脚本全部 PASS，桌面 1440px 分页控件仍正常

## 3. 筛选栏移动端收纳

- [ ] 3.1 拆分筛选控件为可复用子组件（区间/类型/账户/分类/关键字/金额/清除），≤767px 渲染单行「筛选入口 + 生效条件摘要」，≥768px 渲染既有 `.filter-bar`；验证：两端控件来自同一组件（代码确认 + 双视口截图）
- [ ] 3.2 移动端用 antd `Drawer`（`placement="bottom"`）承载筛选编辑与"清除"，收起后摘要与列表同步；验证：e2e 375px 断言首屏可见列表首行（筛选栏不再多行堆叠）、开抽屉改条件 → 摘要与列表刷新、点清除 → 条件重置
- [ ] 3.3 本组 e2e 断言追加进 `interaction_upgrade.mjs`；验证：脚本全部 PASS，桌面筛选行为无回归

## 4. 软键盘避让

- [ ] 4.1 `index.html` viewport 增加 `interactive-widget=resizes-content`；验证：Android Chrome（或 DevTools 设备模拟）聚焦输入框时布局随键盘收缩
- [ ] 4.2 新增 `hooks/useVisualViewportKeyboard.ts`：监听 `visualViewport.resize/scroll`，`rAF` 合帧写 `--kb-inset`，卸载/弹窗关闭时清理复位；验证：单测或 Storybook 无（项目无单测）→ 以 e2e 移动视口 + 键盘模拟断言变量写入与清理
- [ ] 4.3 接入 4 个弹窗（`TxnFormModal` / `Categories` / `Recurring` / `Accounts`）：body/footer 按 `--kb-inset` 加避让，提交按钮始终可视；验证：e2e 375px 视口打开记一笔 → 聚焦金额 → 断言"记账"按钮 `boundingBox` 位于视口内且可点击；关闭弹窗后变量复位
- [ ] 4.4 本组 e2e 断言追加进 `interaction_upgrade.mjs`；验证：脚本全部 PASS，桌面弹窗无偏移（截图对比）

## 5. 移动端性能、可及性与登录首屏

- [ ] 5.1 噪点层修正：z-index 不再高于底部 Tab，≤767px 下噪点弱化/关闭；验证：375px 滚动长列表 DevTools Performance 采样无明显掉帧（结论写入任务备注），截图确认桌面噪点质感不变
- [ ] 5.2 底部 Tab 毛玻璃移动端降级（`blur(14px)` → `blur(8px)` 或纯色半透明）；验证：375px 截图 Tab 仍半透明可读、滚动无卡顿，1440px 桌面无变化
- [ ] 5.3 图表文本替代：`EChart.tsx` 容器加 `role="img"` + 数据生成的 `aria-label`；`Stats.tsx` 三图挂 `<details>` 数据表格（同一份聚合数据渲染）；验证：e2e 断言表格展开后行数与图表数据一致，`pnpm build` 通过
- [ ] 5.4 窄屏登录首屏：≤600px 隐藏/压缩 `.login-art` 样张，登录表单与按钮首屏可见；验证：360px 截图断言登录按钮 `boundingBox.y + height < viewportHeight`，≥992px 双栏不变
- [ ] 5.5 本组 e2e 断言追加进 `interaction_upgrade.mjs`；验证：脚本全部 PASS

## 6. 收敛与集成验收

- [ ] 6.1 断点收敛：CSS 断点 `560/767/860/900/1023` → `600/768/992`，删除与 JS 打架的 860 档（`.two` 栅格交 CSS 单一来源）；验证：375/768/900/1440 四档截图逐页对照，diff 均为预期修复并记录在提交说明
- [ ] 6.2 底部留白合并：`styles.css:437-440` 与 `2369-2372` 合并为 `.mobile-shell` 单一来源；验证：375px 各页底部不被 Tab 遮挡、无多余空白（截图）
- [ ] 6.3 逐页 ≤600px 挤压修复：预算行换行、账期摘要降列、券表单单列、统计/首页双列转单列（由既有 ui-shell「手机访问 → 单列布局」覆盖）；验证：360px 各页截图无溢出与压扁
- [ ] 6.4 全量回归：`pnpm build` 通过；`node e2e/interaction_upgrade.mjs`、`interaction_shell.mjs`、`interaction_feedback.mjs`、`walkthrough.mjs`、`darkmode.mjs`、`export.mjs`、`recurring.mjs` 全部 PASS
- [ ] 6.5 `openspec validate upgrade-mobile-web-interaction` 通过；`openspec status --change upgrade-mobile-web-interaction` 四个 artifact 全部 done；两个变更的实施状态在各自 `tasks.md` 中如实勾选

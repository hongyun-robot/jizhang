# Tasks

> 验收环境:`cd frontend && pnpm dev`（:5600）已启动；e2e 用 `node e2e/<脚本>.mjs`（playwright-core，BASE 默认 http://localhost:5600）。构建验收：`cd frontend && pnpm build`（= `tsc --noEmit && vite build`）。
> 阶段 1、阶段 2 各自独立可提交，互不阻塞；每个任务的验证写在其描述内。

## 1. 阶段 1 · 断裂级修复

- [x] 1.1 统一 shell 断点：`AppShell.tsx` 形态切换改用 `!screens.lg === false`（仅明确为 false 时切移动形态，`undefined` 首帧视为桌面），`Sider breakpoint="lg"` 保持不变；验证：900px 视口出现底部 Tab 四项导航、1000px 视口出现侧栏，DevTools 在 991/992px 间切换无"无导航"中间态
- [x] 1.2 新建 `frontend/e2e/interaction_shell.mjs`，覆盖 1.1 与 1.3–1.7 的断言（导航可达、热区测量、reduced-motion、Tab 语义），沿用 `walkthrough.mjs` 的 `ok()/login()/shot()` 写法与结果打印；验证：`node e2e/interaction_shell.mjs` 输出全部 PASS 并生成 `shots/` 截图
- [x] 1.3 修窄屏裁切：`styles.css` 的 `.cat-section`（961 行）与 `.rule-list`（1349 行）改 `repeat(auto-fit, minmax(min(480px, 100%), 1fr))`，必要时 ≤600px 强制单列；验证：360px 视口打开分类管理与周期记账，每条记录的重命名/删除/状态按钮 `boundingBox()` 完全位于视口宽度内（e2e 断言），截图无横向溢出
- [x] 1.4 移动端全局控件热区：`styles.css` 新增 `@media (max-width: 767px)` 块，为 `.ant-btn / .ant-select / .ant-picker / .ant-input-number / .ant-segmented / .ant-pagination` 设 `min-height: 40px`（按钮同步压 `line-height` 保证内容居中），主 CTA（`.ant-btn-primary`）≥44px；验证：e2e 在 375px 视口对记账页按钮/下拉逐个 `boundingBox().height >= 40`，且 ≥768px 视口下高度与改动前一致（截图对比桌面尺寸不变）
- [x] 1.5 记账分类磁贴热区：`styles.css` `.ant-tag.ant-tag-checkable`（1101 行）在 ≤767px 设 `min-height: 40px; padding: 8px 14px`（`cat-leaves` 内同适用）；验证：记一笔表单内磁贴 `boundingBox().height >= 40`，相邻磁贴间距 ≥6px 不误触（e2e 断言 + 截图）
- [x] 1.6 行内操作热区与 hover 对称：行内 `size="small"` link 按钮（`Records.tsx` / `Categories.tsx` / `Accounts.tsx`）在 ≤767px 命中区 ≥40px（`min-height` 或 `::after` 扩展）；`styles.css:893-901` 的 `opacity:.6` 压暗包进 `@media (hover:hover) and (pointer:fine)`，`(hover:none)` 下恢复 `opacity:1`；验证：e2e 用 `hasTouch` 上下文打开流水页，断言操作按钮不透明度为 1 且高度 ≥40，桌面 hover 提亮仍生效
- [x] 1.7 `prefers-reduced-motion` 补齐：把登录页入场（`styles.css:2006/2014/2020/2054` 的 `.art-title/.art-sub/.art-ledger/.login-card`）与 `tr.flash > td`（1931 行）加入 2416 行的关停块；验证：e2e `page.emulateMedia({ reducedMotion: 'reduce' })` 打开登录页与流水定位流程，断言 `getComputedStyle` 的 `animation-name` 为 `none`，既有关停项无回归
- [x] 1.8 底部 Tab 可达性：Tab 文字字号提到 11–12px、`--tab-fg` 不透明度提升至 ≥0.7（`styles.css:65,1678-1692`），选中 Tab 加 `aria-current="page"`（`AppShell.tsx:151-165`）；验证：对比度按 WCAG 公式手算 ≥4.5:1 并把计算写进任务完成备注，e2e 断言选中项带 `aria-current="page"`
- [x] 1.9 阶段 1 自检：`pnpm build` 通过；`node e2e/interaction_shell.mjs` 与既有 `node e2e/walkthrough.mjs` 全部 PASS；900px 视口截图与既有 `shots/` 对比确认除断点修复外无意外视觉变化 → 满足后提交为独立 commit

## 2. 阶段 2 · 反馈与状态

- [x] 2.1 新建 `components/QueryState.tsx`：三态组件（loading → 传入的 Skeleton；error → `Alert` + 重试按钮；empty → 既有 `Empty`），props 为 `loading / error / isEmpty / onRetry / emptyDescription / skeleton`，样式复用 `.ant-alert-*` 主题规则；验证：组件独立渲染三分支的断言写入 e2e（错误分支在窗口 `setOffline` 或路由拦截失败时出现）
- [x] 2.2 统计页接入 `QueryState`：改 `Stats.tsx:223-234` 的分支，`isError` 走错误态 + `refetch`；验证：e2e 拦截统计接口返回 500 → 断言出现错误提示与"重试"按钮且**不出现**"暂无数据"，恢复接口后点重试 → 断言图表数据渲染
- [x] 2.3 首页接入 `QueryState`：改 `Dashboard.tsx:171,207-214`；验证同 2.2（拦截首页统计/流水接口）
- [x] 2.4 流水页接入 `QueryState`：改 `Records.tsx:405,447-451`，保留既有 Skeleton 与 Empty 节点仅新增 error 分支；验证：断网打开流水页出现错误态，恢复后重试加载列表，区间内真实空数据显示"暂无数据"而非错误
- [x] 2.5 删除确认防重：`Records.tsx` `doDelete`、`Categories.tsx`、`Accounts.tsx` 的 Popconfirm 加 `okButtonProps={{ loading: mutation.isPending, disabled: mutation.isPending }}`，失败时保留列表并 `message.error`；验证：e2e 拦截删除接口为 pending 状态时连点确认 → 断言仅发出 1 个 DELETE 请求（请求计数）
- [x] 2.6 记账表单内联校验：`TxnFormModal.tsx` 在 body 顶部渲染受控 `Alert`（内容来自既有 `validate()` 返回值），账户/分类 Select 失焦校验失败时加 `status="error"`；`message.warning` 仅保留"超额二次确认"提示；验证：不选账户点"记账" → 断言表单内出现错误 Alert、Modal 未关闭、金额为 0 时金额字段有内联提示（e2e）
- [x] 2.7 弹层挂载统一：`TxnFormModal.tsx` / `Accounts.tsx` / `VoucherFormModal.tsx` / `Recurring.tsx` 内的 `Select` / `DatePicker` / `Cascader` / `AutoComplete` 加 `getPopupContainer`（`lib/popup.ts` 的 `popupInParent`：挂到 `node.parentElement`，容器补 `position: relative`）；验证：在记一笔弹窗内滚动到中部打开账户下拉与日期面板，面板贴合触发器且在可视区内（e2e 断言 `boundingBox` 相邻，接受空间不足时的上下翻转）
- [x] 2.8 滚动复位瞬时：`AppShell.tsx:198` 改 `window.scrollTo({ top: 0, behavior: 'instant' })`（类型不通过则用 `document.documentElement.scrollTop = 0` 兜底）；验证：e2e 滚到流水页底部 → 切统计页 → 断言 `window.scrollY === 0` 立即成立（无平滑过渡中间态），首页点流水定位仍平滑居中并闪烁
- [x] 2.9 可点元素语义化与焦点环：`Accounts.tsx:213-222` 行级点击补 `role="button" tabIndex={0}` + Enter/Space（或改为 button 包裹），`Categories.tsx:110-118` 与 `Records.tsx:264-273,421-429` 的可点 Tag 照 `CategoryPicker.tsx:26-31` 先例补 role/键盘；`styles.css` 焦点环规则（1942-1948）扩展为通配 `:focus-visible` 兜底；验证：e2e 用键盘 Tab 到账户行/已退标签并按 Enter 触发，断言触发对应行为且 `outline` 可见
- [x] 2.10 首页 hero：金额窄屏不裁切（`styles.css` `.hero-value` 窄屏 clamp + `heroFontSize` 按位数缩放、`title` 兜底），`Dashboard.tsx` `onPointerMove` 加 `pointerType !== 'mouse'` 早退；验证：360px 视口渲染 1,234,567.89 的结余 → 截图断言无截断，触摸滑过 hero 时断言 CSS 变量 `--mx` 不更新
- [x] 2.11 登录页 `Login.tsx` 静态 `message` 改 `App.useApp()`（与全站一致）；验证：登录失败时无静态 message 主题警告（antd v6 holder 为 body 挂载，断言改查「无 Static function 警告 + 暗色底样式生效」）、暗色主题下样式正确（e2e + 截图）
- [x] 2.12 新建 `frontend/e2e/interaction_feedback.mjs`，覆盖 2.1–2.11 的断言（错误态/重试、防重计数、内联校验、弹层定位、滚动复位、键盘可达）；验证：`node e2e/interaction_feedback.mjs` 全部 PASS（44/44）

## 3. 集成验收（仅整体检查，不新增实现）

- [x] 3.1 `cd frontend && pnpm build` 通过（`tsc --noEmit` 零错误）
- [x] 3.2 dev server 下跑全量 e2e：`interaction_shell.mjs`（34/34）、`interaction_feedback.mjs`（44/44）、`walkthrough.mjs`（52/52）、`darkmode.mjs`（17/17）、`recurring.mjs`（56/56）全部 PASS；`export.mjs` 有 3 处**既有失败**（CSV 表头「平台 / 商家」空格、类型中文标签、`备注关键字` placeholder 与 UI 文案不同步），已用 `git stash` 在阶段 1 代码上复跑确认与本次改动无关。顺带修复的环境/脚本漂移：recurring/refunds/budgets/notifications 的 `sql()` 增加本机 MySQL 8.4（3307）回退（WSL 停用）；refunds/periods 弹窗选择器兼容 VoucherFormModal forceRender 常驻容器；notifications 还款提醒种入补 `owner_id`（个人负债 owner 隔离）、周期模板改用末级分类；periods 造数改末级分类
- [x] 3.3 双主题 + 双视口回归：亮/暗 × 375/900/1440 三档截图（首页、流水、统计、分类、周期、记一笔弹窗），与改动前 `shots/` 对比，确认除既定修复外无视觉回归，且 ≤767px 热区放大未影响 ≥768px 桌面密度（interaction_shell 1.4 桌面断言 + walkthrough 全绿佐证）
- [x] 3.4 `openspec validate --change improve-mobile-web-interaction` 通过；`openspec status --change improve-mobile-web-interaction` 四个 artifact 全部 done

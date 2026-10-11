# Proposal

## Why

2026-10 交互审计(移动端 + Web 端)发现两类硬伤:其一,**断点断裂**导致 768–991px 视口(iPad 竖屏)几乎无导航、<480px 视口下分类管理与周期记账的右侧操作被 `overflow:hidden` 裁掉,属于"功能不可达";其二,**触控与反馈缺陷**使移动端高频操作热区普遍只有 25–34px(标准 ≥40px)、行内操作靠 hover 才提亮(触屏永远等不到)、请求失败被渲染成"暂无数据"(用户无法分辨断网与空数据,也无重试入口)。

前端的设计系统本身质量不错(hover 有触屏保护、动画只动 transform/opacity、已有 `prefers-reduced-motion` 覆盖),本变更是**定向修复**,不是 redesign:只修断裂、热区、反馈、可达性四类问题,不引入动画库、不改信息架构、不改品牌视觉。

本变更覆盖修复路线的**阶段 1(断裂级)与阶段 2(反馈与状态)**;阶段 3(体验升级:URL 筛选持久化、上拉加载、iOS 键盘避让、筛选收纳、性能与打磨)另开变更 `upgrade-mobile-web-interaction`,以便分开验收、跨会话续做。

## What Changes

**阶段 1 · 断裂级修复**
- 统一 shell 断点:移动/桌面形态切换与侧栏收起使用同一断点(992px),消除 768–991px 的"无导航带"。**BREAKING(布局)**:平板竖屏(<992px)从侧栏形态改为底部 Tab 形态。
- 修复 `minmax(480px/470px)` 固定轨道在窄屏溢出被裁切的问题(分类管理、周期记账),窄屏下全部操作可见可点。
- 移动端触控热区达标:全局控件(按钮/Select/DatePicker/Segmented/分页)在 ≤767px 下 min-height ≥40px,主操作 ≥44px;记一笔分类磁贴 ≥40px;行内"编辑/退款/删除"等 link 按钮命中区扩展。
- 触屏操作可见性解耦 hover:操作的压暗态(`opacity:.6`)在无 hover 设备上解除;hover 才出现的视觉线索补 `:active` / `:focus-visible` 等价反馈。
- `prefers-reduced-motion` 补齐遗漏(登录页入场动画、表格行闪烁),并保证既有覆盖不回归。
- 底部 Tab 可达性:文字对比度达 WCAG AA(≥4.5:1),选中项带 `aria-current="page"`。

**阶段 2 · 反馈与状态**
- 新增统一的数据态组件(loading / empty / error + 重试),接入首页、流水页、统计页;请求失败与"暂无数据"可区分,并提供重试入口。
- 删除与二次确认类操作加 loading/禁用,杜绝连点重复提交(流水、分类、账户)。
- 记账表单校验从"仅全局 Toast"改为字段级/内联错误提示,Toast 降为补充。
- 路由切换的滚动复位改为瞬时滚动(`behavior:'instant'`),消除长列表"慢滚回顶"。
- Modal 内的 Select / DatePicker / Cascader / AutoComplete 下拉统一挂载到触发器父级,修复滚动与键盘场景下的脱位。
- 可点元素语义化:账户行(div)、分类标签(span)、已退标签(span)改为 button 或补齐 role + tabIndex + 键盘处理;焦点环通配补齐。
- 首页 hero 金额在窄屏不再静默裁切;hero 指针柔光仅对鼠标指针生效(触摸移动不触发)。
- 登录页从 antd 静态 `message` 改为 `App.useApp()`,与全站一致。

## Capabilities

### New Capabilities

(无)

### Modified Capabilities

- `ui-shell`: 增补双形态导航的断点一致性与"无导航带"禁令、触控热区下限、触屏操作可见性、减弱动效完整性、底部 Tab 可达性、弹层挂载约束、路由滚动复位行为、数据加载错误态(跨首页/流水/统计的统一错误态模式)、记账表单内联校验。
- `transactions`: 增补流水页错误态与重试、删除确认防重、移动端行内操作可达性;筛选状态跨路由保留、上拉加载等由阶段 3 变更处理。
- `home-dashboard`: 增补首屏结余金额在任意视口必须完整可读(不得静默裁切),指针跟随柔光仅对鼠标指针生效。
- `categories`: 增补分类管理页在窄视口下全部操作可见可点。
- `recurring-entries`: 增补周期记账列表在窄视口下全部操作可见可点。

> statistics 的错误态**不单开 delta**:由 `ui-shell` 的跨页面错误态要求覆盖,并与首页共用同一实现(用户决策 2026-10-11)。

## Impact

**受影响代码(全部在 `frontend/src`)**
- `components/AppShell.tsx` — 断点切换逻辑(形态选择、Sider breakpoint、导航可达)
- `components/TxnFormModal.tsx` — 分类磁贴热区、校验内联化、弹层挂载
- `components/CategoryPicker.tsx`、`components/PeriodPicker.tsx` — 热区与弹层
- `components/ThemeProvider.tsx` — 移动端控件高度(仅移动端 media query,不动桌面 `controlHeight: 34`)
- `pages/Records.tsx`、`pages/Dashboard.tsx`、`pages/Stats.tsx` — 错误态接入、删除防重、金额展示
- `pages/Categories.tsx`、`pages/Recurring.tsx`、`pages/Accounts.tsx`、`pages/Login.tsx` — 窄屏裁切、语义化、message 统一
- `styles.css` — 热区、hover 对称性、reduced-motion、对比度、焦点环、断点收敛;`liability.css` 焦点环同步

**依赖 / 基础设施**:无新增依赖(antd 6 + React 19 既有能力)。

**规范约束(不得回归)**
- `specs/ui-shell`:四 Tab 枚举不变(记一笔/流水/统计/我的),铃铛为 Tab 栏内工具位非 Tab(specs/notifications)
- `specs/transactions`:区间筛选与统计页共用同一选择器语义;offset 分页默认 20 条
- `specs/recurring-entries`:"打开页面懒补齐"幂等逻辑与 `AppShell` 中的定时器不动
- `AppShell.tsx:195-200` 的 `locateId` 定位让行逻辑必须保留

**风险**
- 断点改动影响平板布局 → 用 `frontend/e2e/`(playwright-core)补导航可达断言
- 热区放大影响桌面密度 → 热区规则只写在 `≤767px` 媒体查询内,桌面视觉零变化

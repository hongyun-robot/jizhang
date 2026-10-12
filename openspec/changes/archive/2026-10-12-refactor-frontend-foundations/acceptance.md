# 前端基础重构验收记录

## 环境与基线（2026-10-12）

- Windows PowerShell；Node 24.15.0、Vitest 4.1.11、TypeScript 7.0.2、Vite 8.3.2；本机 Firefox/Chrome。
- 本机 MySQL97 和 Redis 6380 运行。浏览器资金验收使用独立 SQLite 内存账本及 miniredis，未写真实账本。MySQL 集成测试使用随机数据库，测试通过且清理成功。
- 原根路径主 JS 2666.49 kB、gzip 859.74 kB，CSS 49.77 kB；原子路径 gzip 859.92 kB。冻结基线在已忽略的 bin/frontend-baseline。
- 已复现基线缺陷：登录回跳丢失查询串/锚点、viewer 新建入口、提醒错误变空态、关联退款 CSV 多导出、401 未刷新、预算 dirty 草稿被刷新覆盖、自定义日期回显为空、购券后追加流水缓存未更新。

## 四阶段交付

| 阶段 | 完成实现 | 验证 |
| --- | --- | --- |
| 基础层 | 分域 API、请求/错误契约、会话代次/刷新协调、查询键/缓存、权限导航、错误反馈与草稿 | 单元测试及 foundations/navigation/errors/drafts/cache |
| 领域规则 | 整数分金额、分类索引、账户选项、日期/账单、筛选/导出、负债命令与载荷守卫 | 边界/尾差/请求契约/编译期用例，filters/CSV/四角色权限 |
| 页面模块 | Records 状态/手势/定位/视图，周期表单，券列表/详情/操作，负债汇总/详情/分模式表单，图表/数据表，后台任务与键盘观察器 | interactions，四套券/平台旧脚本，liabilities 53 项，settlement/background/keyboard |
| 加载样式 | 路由/用户管理懒加载，ECharts core，删除过渡 API/未用依赖，样式拆分/媒体规则合并/覆盖收窄，共同主题源 | 根/子路径 loading，六档宽度/主题/截图，生产体积与构建 |

## 检查结果

| 检查 | 实际结果 |
| --- | --- |
| npm run test:unit | 修复复验为 20 文件、61 用例通过（初次实现为 19 文件、59 用例） |
| npm run typecheck / npm run build | 通过，包含测试源码类型检查 |
| go test ./... / go vet ./... | 通过 |
| TestLiabilityMySQL | 默认账户/目录、数据库分类、个人默认账户并发通过，随机库清理成功 |
| TestVoucherMySQL | 双实例自动退款、核销/退款竞争、封账竞争、有数据 down 拒绝、UTC 服务器日期通过，随机库清理成功 |
| vouchers.mjs | 桌面/移动购买、核销、退款、作废、撤销、自动暂停、CSV、只读权限通过，无运行错误 |
| voucher_retry.mjs | 已提交超时后改值重试同 key 返回 409，只购买一次、余额恢复通过 |
| voucher_platforms.mjs | 四角色目录，桌面键盘与手机浮层，选择/清空/编辑，失败及迟到保护通过 |
| transaction_platforms.mjs | 四模式、共享目录创建、保存/编辑/清空、搜索和资金影响通过 |
| liabilities.mjs | 完整 53 项通过：资金、归属、分期、结清/撤销、来源、提醒、目录与窄屏 |
| 新增隔离脚本 | foundations 8 项、navigation、errors 13 目标、drafts、filters、cache、interactions、loading、background、keyboard、settlement 全部通过 |
| 后台任务 | 生产预览与开发 Vite 均通过计数检查；单元覆盖 StrictMode 快速重挂载、在途防重、频率、退出前排队与清理 |
| git diff --check | 通过 |

浏览器临时流水、券和配置通过 finally 清理。原始验收遗留的受管理流水及不能经业务 API 删除的负债计划，随 fixture 关闭销毁整个内存账本，不绕过资金规则删记录。fixture 仅在测试文件增加 TTL 配置：默认 20 分钟不变，长套件使用 3 小时与 -count=1。

**未执行旧 SQL/开发账本脚本**：refunds、budgets、recurring、notifications、users 和 interaction_*。它们依赖历史种子账号或硬编码的 MySQL 8.4/3307 客户端与 jizhang 库，部分直接批量删除数据；本次没有把真实开发库改造成该验收环境。已执行隔离回归覆盖关联退款 CSV、预算草稿、周期 CRUD/归属、权限、手势、弹层、键盘与缓存，但不宣称旧脚本通过。

构建仍提示共享 AntD/统计 chunk 超过 500 kB；以下体积是实际测量，没有通过隐藏提示替代验证。

## 五份 delta 场景证据

| 能力 | 证据 |
| --- | --- |
| identity/user-auth | client/coordinator 单元：拒绝 token 强制刷新、单飞、迟到旧 401、一次重试、请求内容/key、403、退出/换号迟到；foundations 实际 SPA 401；navigation 退出换号 |
| identity/access-control | permissions 四角色/本人/他人/受管理记录表驱动；viewer 入口与 API 403；目录四角色例外；liabilities 个人资料例外与管理员不可查看他人负债 |
| transactions | filters 单元与 URL/日期/防抖/POP 浏览器；跨月关联退款 CSV/BOM/资金列；interactions 追加 REPLACE/分页回顶/无重复/touchcancel/一次性定位及清理；navigation Tab/换号 |
| budgets | 浏览器覆盖后台刷新保留 9000 草稿、保存服务器回填、取消切换、确认切换；draft 单元及 revision/editorKey 检查覆盖迟到保存不能写入其他账期 |
| ui-shell | errors 逐页错误/重试；QueryState 空态与后台失败单元；drafts 迟到目录/详情；cache 当前追加列表购券；background 无变化/失败/退出；loading 资源失败恢复、URL、根/子路径刷新；keyboard 与共享观察器 |

结清专项另验证：预览后修改手续费必须重新核对，失败保留草稿，连点只提交一次，重试保留 request_key/version。预览期间改字段会使迟到预览被代次检查丢弃。

## 加载体积

浏览器记录登录至首页全部脚本请求，包含 entry、modulepreload 及首次首页依赖，去重文件后分别计算 gzip，没有只比较一个 entry chunk。

| 产物 | 基线 | 重构后 | 比例 |
| --- | --- | --- | --- |
| 根路径首次 JS/modulepreload gzip | 859.74 kB | 修复后 418.93 kB | 48.7% |
| 子路径首次 JS/modulepreload gzip | 859.92 kB | 约 418.62 kB | 48.7% |
| 根路径全部 JS（含懒加载） | 2666.49 kB | 修复后 2156.90 kB | 80.9% |
| 根路径全部 JS gzip | 859.74 kB | 修复后 739.92 kB | 86.1% |

首次加载减少约 51.3%，低于基线 70% 的目标；登录/首页没有 Stats/ECharts 和 UserManager chunk。进入统计才加载图表，展开用户管理才加载管理模块。最终格式化构建按相同逻辑 chunk 重算，原网络清单同时保留。准确清单与计量 JSON 在 frontend/e2e/shots/refactor-frontend/loading*.json；该目录由 Git 忽略。

## 视觉与交互

- loading 在 360、767、768、991、992、1280px 检查首页/流水/统计/负债、滚动、横向溢出和导航触控热区。<992 底部导航，>=992 侧栏；768 内容断点保持。
- 浅色、暗色、系统跟随、刷新持久化、减弱动效通过。主题 JSON 同时驱动 CSS 变量、AntD 和图表，保留 HTML 首屏预置。
- 人工复核桌面/手机基线与最终截图的结构、密度、颜色、导航及行操作。截图数据来自不同临时测试阶段，金额/条数差异不作为像素布局差异。
- 图像在有限动画结束后保存：frontend/e2e/shots/refactor-frontend/final-1280-{home,records,stats,liabilities}.png、final-360-{home,records,stats,liabilities}.png、final-dark-stats.png；负债详情图在 frontend/e2e/shots/liabilities-*.png。
- 键盘验证模拟 visualViewport 收缩：购券/流水/个人负债保存按钮在可视区，取消清除 inset/标记；多弹层引用计数由单元覆盖。未声称实体手机键盘验收。

## 交付与回滚

初次实现保留活动变更供独立验证；用户随后授权修复并在复验通过后归档。本轮不部署、不提交或推送；主规范同步和归档结果另行记录。

生产 API 与数据库结构未改，后端仅修改测试 fixture TTL。按本变更的补丁/提交恢复前端源码与锁文件并重建，或恢复上一整套已验证静态产物；整套替换 chunk，不能仅覆盖 index 留下缺失依赖。无需数据库 down。更新后重新加载标签页取得一致资源与会话；不要使用会删除其他未提交工作的大范围 Git 重置。

## 独立验证后的修复复验

2026-10-12，追加 14.1–14.3 三项修复，原 47 项与追加项合计 50 项。当前验证结论见 verification.md，首次的 C1/W1/W2 均已修复，已执行检查没有剩余 critical/warning/suggestion。

- 编辑会话代次覆盖关闭、重开、目标/登录会话变化和卸载。迟到提交仍刷新资金缓存，只对原编辑会话执行反馈、关闭和导航。
- 分期编辑按 requestKey 保留原对照快照和操作模式，后台详情更新不能改变本次重试载荷与字段注册。相同请求重放取回已完成操作，改值后仍保留 409，未通过更换 key 绕过保护。
- 桌面侧栏收支和预算的首次错误/后台错误各自可重试，已有数据保持可读。
- refactor_repairs 六组回归通过：记账/周期各旧成功与旧失败，以及两项侧栏的首次/后台错误和恢复；同会话正常提交关闭不回归。
- refactor_plan_retry 三类回归通过：调整、退款核对、改值冲突。重放的完整 payload、端点、版本/key 和 operation_id 一致，流水、操作数量与欠款/余额不重复。
- 修复后重新运行 transaction_platforms、vouchers、voucher_retry、interactions、errors 13 目标和完整 liabilities 53 项，全部通过。foundations 8 项、drafts、keyboard、loading 的六档宽度/主题/资源错误恢复复跑通过。
- 草稿脚本曾因两次 document navigation 打断 refresh 的 Set-Cookie 响应，触发后端旧 cookie 重放吊销而失败，随后因清理失败留下预算值，重跑也受污染；已增加页面就绪等待，销毁旧 fixture 后在新账本通过。加载检查增加可清理的 0.01 元测试支出，确保新账本也能验证全部图表注册。这些失败没有计为通过。
- npm typecheck/build、20 文件/61 单元用例、OpenSpec strict 与 git diff --check 均通过。旧 SQL 脚本与实体手机等原有执行范围限制保持公开，未改写为通过。

## 归档结果

2026-10-12 按用户授权完成归档，路径为 openspec/changes/archive/2026-10-12-refactor-frontend-foundations。50/50 任务完成；五份主规范已同步，新增 4 条要求、修改 10 条要求，标题、Purpose 及未涉及的条目/场景保留。主规范校验 18 passed/0 failed；仓库既有 Purpose 少于 50 字的提示保留原文，不为消除警告改写既有 Purpose。已执行复验没有未修复的实现问题，旧 SQL 与实体设备的范围限制仍如上记录。未部署、未提交或推送。

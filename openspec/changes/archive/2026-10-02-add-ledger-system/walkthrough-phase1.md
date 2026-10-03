# 一期验收走查清单（tasks 6.1）

- 日期：2026-10-02
- 环境：本地 dev（后端 :8080 `go run`、前端 :5600 Vite）+ 全栈 Docker（nginx :80 → api 容器）
- 回归基线：`go vet ./...` + `go test ./... -count=1` 全绿；`node e2e/walkthrough.mjs` **52/52 PASS**
- 范围：`specs/identity/user-auth`、`identity/user-management`、`identity/access-control`、`accounts`、`categories`、`transactions`、`statistics`、`home-dashboard`、`ui-shell`（导航与登录部分）

## 不符项与修复（本轮走查发现）

1. **home-dashboard：最近流水"点击定位"未实现**
   - 修复：`Dashboard` 最近流水行可点击 → `navigate('/records', { state: { locateId } })`；`Records` 挂载后 `scrollIntoView` + `.flash` 高亮 1.8s 并清状态，行带 `data-txn-id` 锚点（桌面 `onRow` / 移动卡片同锚点）。
   - 验证：e2e 46–48（clickable → 跳转 → flash 命中 → 锚点存在）。
2. **user-management：重置密码后旧 access token 仍有效（最长 30 分钟窗口）**
   - 首版修复（吊销截止时间）因 JWT `iat` 秒级粒度在同秒边界漏拒，弃用。
   - 终版修复：`AuthClaims.ep` 会话代次 —— `RevokeAll`（重置密码/停用/角色调整/refresh 盗用）对 `session_epoch:{uid}` 执行 `INCR`（键无 TTL，防过期重建复活），`issueAccess` 携带当前代次，`Authenticate` 代次不匹配即 401（Redis 异常放行，读路径可用性优先）。旧代次 token 与时间无关，精确失效。
   - 验证：E2（重置后旧 token 401、新登录 200）+ ROLE（提权后旧 token 401、重登按新角色 200、降权后 403）。

## 场景逐条核对

### identity/user-auth

| 场景 | 验证方式 | 结果 |
| --- | --- | --- |
| 登录成功（access 短期 + refresh 长期） | e2e 登录；Set-Cookie `jz_rt; Max-Age=2592000; HttpOnly; SameSite=Strict`（复核时发现缺 SameSite → 已修 `auth_handler.go` 三处 `SetSameSite(http.SameSiteStrictMode)` 并重测 login/refresh/logout 三个响应头均为 Strict，登出后重放 401；dev 与 Docker 生产栈均复测通过） | ✅ |
| 凭据错误统一提示 | curl 错密码 → `code:10002 用户名或密码错误`（不泄露账号存在） | ✅ |
| 过期 access → 401 → 自动刷新重试 | 2.2 负 TTL 验证 + e2e 刷新后会话恢复（cookie 恢复→放行） | ✅ |
| refresh 失效 → 跳登录页 | REG：登出后 refresh 401；e2e 未登录重定向 | ✅ |
| 登录失败限流（5 次/10 分钟） | 2.3 第 6 次 429 | ✅ |
| 停用账号登录失败 / 在线会话 401 | 2.4 | ✅ |
| 登出后 refresh 立即失效 | REG：logout 200 → refresh 401；旧 refresh 轮换重放 401 | ✅ |

### identity/user-management

| 场景 | 验证方式 | 结果 |
| --- | --- | --- |
| 关闭自助注册 | `POST /auth/register` → 404；界面无注册入口 | ✅ |
| 管理员创建用户即刻可用 | 2.5 创建 xiaoli 并登录（e2e 亦以四角色登录） | ✅ |
| 用户名重复 | curl 重复 xiaoli → `409 用户名已占用` | ✅ |
| 四档角色分配与调整 | ROLE：member 改他人 403 → 提 recorder（旧 token 401、重登 200 可改）→ 降回 member 403 | ✅ |
| 停用（无法登录+会话失效+历史保留）/ 恢复 | 2.4 / 2.5 | ✅ |
| 重置密码使已签发 token 全部失效 | E2（修复后）：重置前 200 → 重置后旧 token **401** → 新登录 200 | ✅ |
| 拿掉最后一个管理员 | 2.5 → 409 | ✅ |

### identity/access-control

| 场景 | 验证方式 | 结果 |
| --- | --- | --- |
| 分类/账户/用户管理仅管理员（其他 403） | permission_test；e2e member/viewer/recorder 无入口 + `/accounts` 拦截 | ✅ |
| 流水写矩阵（admin/recorder 全量、member 仅己出、viewer 只读） | 4.1 单测 + ROLE/D 组 API + e2e viewer 行按钮 0 个 | ✅ |
| 查看权限对全员开放 | curl viewer 统计三端点 200 + e2e 51/52 viewer 统计页完整渲染 | ✅ |
| API 与界面双重执行 | 直调 API 403（非仅前端隐藏）+ UI 隐藏 e2e | ✅ |
| 记账员修改账期设置 → 403 | 7.1 落地后冒烟：recorder `PUT /settings` → 403 | ✅ |

### accounts

| 场景 | 验证方式 | 结果 |
| --- | --- | --- |
| 负债账户负余额按欠款展示 | 3.x 信用卡 −100 → UI"欠 ¥100.00"红字 | ✅ |
| 流水关联账户、余额随流水变动 | e2e 记一笔 12.34 扣减；6.2 nginx 链路 23.45 扣减→删除回滚 | ✅ |
| 转账不计收支/预算、仅动两账户 | G：转账前后支出 922.66 不变、总资产 1077.34 守恒（写后缓存失效亦一致） | ✅ |
| 余额与总资产公式 | 5.2 ending_total=1077.34=Σ各账户 | ✅ |
| 有流水账户不可删（仅可停用） | H：`DELETE /accounts/1` → 409，账户仍在 | ✅ |

### categories

| 场景 | 验证方式 | 结果 |
| --- | --- | --- |
| 两级结构、末级记账、一级聚合含子级 | J：`category_id=1`（餐饮）→ 5 笔全为子分类；`category_id=3` → 2 笔 | ✅ |
| 默认种子 + 新增分类 | 36 粒种子；e2e 新建/删除一级；I 新建"走查宠物→走查粮食"父子 | ✅ |
| 删除约束（被引用 / 有子分类先处理子级） | I：被引用 409、有子 409；解除引用/子级后 200 | ✅ |

### transactions

| 场景 | 验证方式 | 结果 |
| --- | --- | --- |
| 记录支出 / 收入 | e2e 记一笔 12.34 列表首位；工资 1000 入统计 | ✅ |
| 删除回滚余额/统计 | 4.4 单测（差=金额）+ e2e 删除行消失 | ✅ |
| 自由区间 ↔ 统计页同区间语义 | 4.3 + e2e 共享区间选择器切"近7天" | ✅ |
| 组合搜索（备注+金额+分类+账户+日期） | 4.3 curl 组合（金额 200-600→3 等）+ J 分类组合 | ✅ |
| offset 分页（20/页、总数） | 4.3 `page_size=2` 返回 2+总数；e2e 分页控件 | ✅ |
| CSV 导出（BOM、防注入） | **延后** 11.1 | ⏭ |
| 封账拒绝增删改 | 7.2 落地后单测 `TestClosedPeriodBlocksWrites` + 冒烟 409 | ✅ |

### statistics

| 场景 | 验证方式 | 结果 |
| --- | --- | --- |
| 分类占比合计=区间支出、一级聚合 | 5.1 跨端点一致（delta=66.66 对账） | ✅ |
| 天/周/月三粒度趋势 | e2e 按周切换 | ✅ |
| 本期环比（上一等长区间） | 5.1 PrevMonthWindow 单测 + 界面对比 | ✅ |
| 账期汇总跨年翻阅 | K：`year=2025` → 12 行全零、`year=2026` → 10 行 | ✅ |
| 资产走势（负债负贡献） | 5.2 | ✅ |
| 30s 缓存 + 写后失效 | 5.1 miss→hit→写后 miss | ✅ |

### home-dashboard

| 场景 | 验证方式 | 结果 |
| --- | --- | --- |
| 本期收支结余与统计页同口径 | 5.4 数值逐一对账（1000/922.66/77.34） | ✅ |
| 最近 10 笔 + 点击跳转定位 | 不符项 #1 修复 → e2e 46–48 | ✅ |
| 预算进度与超支状态 | 占位展示（实装与超支标红 **延后** 8.3） | ⏭ |
| 快捷记账默认今天 | e2e 5.4 日期=今天 | ✅ |

### ui-shell

| 场景 | 验证方式 | 结果 |
| --- | --- | --- |
| 移动底部 Tab / 桌面侧栏双形态 | e2e 桌面 + 390×844 双形态截图 | ✅ |
| 手机数字键盘 + 常用金额快捷键 | e2e `inputMode=decimal`、+50 写入 | ✅ |
| 未登录重定向 → 登录后返回原路径 | e2e 49（/stats→/login）/ 50（登录后回 /stats） | ✅ |
| 单域名同一产物 | 6.2：nginx 同源出 SPA + `/api/v1/` 反代 | ✅ |
| 暗色模式（手动/跟随系统持久化） | **延后** 12.1 | ⏭ |

## 延后清单（随对应任务验证，非一期缺口）

- ~~access-control「记账员修改账期设置 403」、transactions「封账拒绝增删改」~~ → 已在 7.1/7.2 落地验证 ✅
- home-dashboard「预算进度条与超支状态」 → **8.3**
- transactions「CSV 导出」 → **11.1**
- ui-shell「暗色模式」 → **12.1**

## 结论

一期范围内可验证场景 **全部通过**；发现 2 处不符项已修复并回归（e2e 52/52、`go test` 全绿）。4 处场景按分期计划延后至对应任务。

---

## 组 7 账期与封账走查（tasks 7.1–7.4，对照 specs/billing-periods）

浏览器走查脚本 `frontend/e2e/periods.mjs` → **29/29 PASS**（截图 `frontend/e2e/shots/periods-01~07`）；主走查回归 **52/52 PASS**；后端 `TestSpecScenario26CurrentPeriod` / `TestClosedPeriodBlocksWrites` / `TestClosingAuditTrail` / `TestSettlementDayChangeKeepsHistory` 全绿。

| spec 场景 | 验证方式 | 结果 |
| --- | --- | --- |
| 结算日为 26 日：本期 2026-09-26~2026-10-25、上期 2026-08-26~2026-09-25 | 设置页改 26 后预览逐字命中（走查 05/06）+ 单测 `TestSpecScenario26CurrentPeriod` + API preview 区间一致 | ✅ |
| 未配置 = 自然月 | 设置页默认结算日=1，账期 2026-09 = 09-01~09-30（走查 01/03） | ✅ |
| 跨期日期归期（9/25 归 8/26-9/25 期，非 9/26 新期） | 单测 `TestSettlementDay26Boundary` / `TestSpecScenario26CurrentPeriod`（period_key 写入时物化） | ✅ |
| 修改结算日不改历史 | 提示「仅影响今日之后的流水」可见（走查 02/04）+ 单测断言历史行 period_key/entry_date/封账状态不变（7.3） | ✅ |
| 封账确认（展示收入/支出/结余） | 弹窗展示 ¥0.00/¥88.00/-¥88.00 后确认 → 状态「已封账」（走查 10–13） | ✅ |
| 封账期内修改流水被拒（任何角色） | UI 删除弹错「该账期已封账」且行保留（走查 15/16）+ API 409 + 单测 admin/member/recorder 全拒 | ✅ |
| 解封、修改、重新封账 | UI 解封 → 改 88→99 成功 → 重新封账（走查 17–22） | ✅ |
| 审计留痕（谁、何时、哪一期、动作） | 审计页最新三条=封账/解封/封账，2026-09/admin/含时间（走查 23–25）+ 单测 `TestClosingAuditTrail` | ✅ |
| 非管理员设置/审计 | recorder `PUT /settings` 403；member 无入口 + `/settings` 拦截、viewer `/audit` 拦截（走查 26–28）+ audit GET 403 | ✅ |

实现期间修正一处口径不符：`PeriodService.KeyForDate/Range` 原按起始月键实现，与设计 D7 示例（`2026-09-26～10-25 记为 2026-10`）不一致，且 S=1→26 中途变更时当期键会倒退回已封历史期；已改为**结束月键**（S=1 行为与既往完全一致，存量 `period_key` 无影响），相关单测同步更新。


## 组 8 预算走查（tasks 8.1–8.3，对照 specs/budgets）

浏览器走查脚本 `frontend/e2e/budgets.mjs` → **29/29 PASS**（截图 `frontend/e2e/shots/budgets-01~07`）；API 冒烟 19/19；主走查回归 **52/52 PASS**、账期走查 **29/29 PASS**；单测 `TestBudgetConsumptionScope` / `TestBudgetOverNotifyOnce` / `TestBudgetSetValidation` 全绿（`go vet` + `go test ./...`）。

| spec 场景 | 验证方式 | 结果 |
| --- | --- | --- |
| 配置本期预算（总 8000 / 餐饮 3000）→ 首页与统计页显示进度 | 预算页保存后首页卡（账期 2026-10/总预算/餐饮）与统计卡展示 922.66/8000.00、922.66/3000.00（走查 06–13） | ✅ |
| 消耗只算支出、分类含子分类（spec 例 3200/3000 → 超支 200） | 单测精确断言（收入 5000/转账 1000 不计、子分类并入一级、超支 200 进度封顶 100）；走查按 dev 实测 922.66 口径（走查 07/08） | ✅ |
| 超支标红并显示超出金额 | 餐饮预算 3000→500 → 进度条 exception 标红 + 「已超支 ¥422.66」，总预算不标红（走查 14–17） | ✅ |
| 首次超支生成一次站内提醒（分类名+超出金额），同账期同预算不重复 | SQL 断言首超 1 条 payload 含 `餐饮`/`422.66`/`budget_overdue:2026-10:cat:1`，重复保存仍 1（走查 18–21）；API 冒烟同证（重复保存/记账/删流水均不重复）；单测覆盖删了再记、总预算改低即时提醒 | ✅ |
| 预算配置仅管理员、查看对四档开放 | viewer 页面「仅查看」无输入框/无保存按钮，仍可见进度与超支（走查 22–25）；recorder `PUT /budgets` 403；非法配置单测全拒 | ✅ |

基线恢复：预算清空（首页回「预算尚未配置 · 去配置」）、提醒 0 条（走查 26–29）。提醒读取接口要到任务 10.2（铃铛）提供，本轮存在性经 dev SQL 断言。


## 组 9 周期记账走查（tasks 9.1–9.4，对照 specs/recurring-entries）

浏览器走查脚本 `frontend/e2e/recurring.mjs` → **40/40 PASS**（截图 `frontend/e2e/shots/recurring-01~07`）；API 冒烟 `smoke-recurring.ps1` **17/17**；主走查回归 **52/52 PASS**、账期 **29/29 PASS**、预算 **29/29 PASS**；单测 `TestRecurringNextRun`（日/周/月/年 + day31 不漂移 + Feb29 夹取共 10 例）/ `TestRecurringEnsureIdempotent` / `TestRecurringPausedThenResume` / `TestRecurringClosedPeriodSkipped` / `TestRecurringEditDeleteIndependent` / `TestRecurringValidation` 全绿（`go vet` + `go test ./... -count=1`）；`npx tsc -b --force` 0。

| spec 场景 | 验证方式 | 结果 |
| --- | --- | --- |
| 创建房租模板（每月 1 日 2500 银行卡 → 居家/房租），自下一周期起生成 | 走查 08–11：创建成功、行显 2500/每月/启用/居家·房租/银行卡、下次生成=起算日 2026-10-01 | ✅ |
| 到期自动生成（服务端定时任务） | 进程 cron `@every 5m`：模板 04:51:32 创建后零 ensure 调用，04:53:34 tick 日志 `{"msg":"recurring generated","count":1}` 且 next_run 推进到 10-03 | ✅ |
| 生成笔按归属日期归期 + 源标记 | 走查 12–17：懒补齐生成 1 笔（entry_date=2026-10-01、period_key=2026-10、source=recurring、2500.00），流水页行带「自动」标记（桌面类型列 + 移动卡片），生成后节奏推进到 2026-11-01（走查 18） | ✅ |
| 打开页面懒补齐（停掉 cron 也能补） | 重启进程验证：到期模板（start 2026-09-01 每月 7 元）就位后杀进程重启，服务 05:21:04 启动（首 tick +5min），应用 05:21:27/31 两次打开触发 ensure → 生成 09-01/10-01 两笔，日志无任何 cron 行 | ✅ |
| 停机跨期补齐 + 重复触发幂等 | 走查 19–22：start=2026-08-01 每月模板一次补齐 08/09/10 三期（月锚不漂移），再次打开应用仍 3 笔；单测连续触发 3 次仅 1 笔 | ✅ |
| 修正生成金额（100→120，模板仍 100，下期仍按 100） | 走查 23–25：改笔保存成功 → 120×1 + 100×2，`recurring_rules.amount` 仍 100.00 | ✅ |
| 删除生成笔（模板保活，之后按模板金额重新生成） | 走查 26–29：删笔剩 2、模板状态仍启用；重写 next_run_date 后再开应用按 100 重新生成 3 笔 | ✅ |
| 模板编辑不影响已生成流水 | 走查 30–32：模板 2500→2600 保存，已生成的房租流水仍 2500.00 | ✅ |
| 模板管理仅管理员（member/viewer 403、直达拦截），ensure 对任意登录角色开放 | 走查 04–07：member 直达 `/recurring` 拦回首页、member/viewer `GET /recurring` 403、member `POST /recurring/ensure` 200（generated=0） | ✅ |
| 启用/暂停状态切换 | 走查 33–36：暂停→状态标签「暂停」→启用→「启用」，消息提示正确；单测暂停保留 next_run、恢复后补齐不重不漏 | ✅ |

实现期间修正一处设计口径：幂等唯一键由初稿 `(rule_id, period_key)` 改为 **`(rule_id, entry_date)`**（migration `000003_recurring_idempotency` 已应用，`schema_migrations version=3, dirty=0`）——若按账期键去重，每日/每周频率整月只能生成一笔；spec「同一模板同一期只生成一笔」按每次 occurrence 的归属日成立，对 cron 与 ensure 双触发防重同样成立。design.md D7 已同步修正。生成笔 `created_by = 模板创建者`，改删与手记流水同规则（属主与管理员）。

基线恢复：模板表与生成笔清空（走查 39/40）、支出一级分类 8 个（走查 37）、2026-10 支出 922.66（走查 38）。走查分类**全程复用基线种子**（居家/房租、居家/水电燃气）不增不删。

## 组 10 站内提醒（tasks 10.1—10.3，对照 specs/notifications）

提醒中心走查脚本 `frontend/e2e/notifications.mjs` → **47/47 PASS**，截图 `frontend/e2e/shots/notif-01~11`；API 冒烟 `smoke-notifications.ps1` **13/13 PASS exit=0**；回归 walkthrough **52/52 PASS**、budgets **29/29 PASS**、recurring **40/40 PASS**、periods **29/29 PASS**；单测 `TestRepaymentNoDebtNoReminder`（无欠款不提醒）/`TestRepaymentWithinLeadDays`/`TestRepaymentOutsideWindowNoReminder`/`TestRepaymentOverdueReminder`/`TestRepaymentSkipsDisabledAndAsset`/`TestNotificationReadFlow`/`TestRecurringLazyPatchNotify` 全绿（`gofmt` + `go vet ./...` + `go test ./... -count=1`）；`npx tsc -b --force` 0。

| spec 场景 | 验证方式 | 结果 |
| --- | --- | --- |
| 未读红点与数字：2 条未读 → 铃铛显示数字 2 | e2e 01—02：基线铃铛纯图标无红点 → 真实保存预算（总 1 / 餐饮 1）触发 Evaluate 生成 2 条 `budget_overdue` → `.brand .notif-bell` 红点数字 2，DB count=2 | ✅ |
| 点击铃铛打开提醒列表（类型标签/分类名/超出金额/账期可读） | e2e 02：点击铃铛跳 `/notifications`，2 行未读态，正文含「预算超支 ×2 / 总预算 / 餐饮 / 921.66 / 2026-10」 | ✅ |
| 单条已读：未读 2→1、行保留 | e2e 03：点行内「标为已读」→ 红点 1，`.notif-row` 仍 2 条、未读 1 条 | ✅ |
| 全部已读：未读归零红点消失、历史仍可见 | e2e 04：点「全部已读」→ 无红点、2 条历史在列且带「已读」标签、按钮转禁用 | ✅ |
| 还款提醒正文：账户名 + 欠款 + 还款日 | e2e 05：SQL 种入真实 payload（账号 5「信用卡」/100.00/2026-10-10/距 8 天）→ 列表渲染「还款提醒 信用卡欠款 ¥100.00，距还款日 8 天（2026-10-10）」，红点 1；生成逻辑（提前 3 天窗口/无欠款不提醒/已停用与资产跳过）由 10.1 单测 + 13 项 API 冒烟覆盖，不等 08:00 cron | ✅ |
| 周期补齐提醒真实链路 | e2e 06：建模板（月/88.88/午餐/2026-10-01）→ 刷新页面 AppShell 懒补齐 `EnsureDueLazy` → `recurring_patched` 生成 1 条 + 补记 1 笔流水，正文「已补记 1 笔周期流水」，列表累计 4 条 | ✅ |
| 移动端铃铛形态与跳转 | e2e 07：390×844 底部 Tab 第 5 格 `.bottom-tabs .notif-bell` 红点 2 → 点击进列表 4 条 | ✅ |
| 四档开放（viewer 可读可标已读） | e2e 08：viewer 直达 `/notifications` 读 4 条、看到 2 条未读、铃铛红点 2，标已读后未读 1（路由不挂 RequireRoles，接口冒烟同证 viewer read-all 200） | ✅ |
| 未读数归零后红点消失且列表仍可见历史 | e2e 04 + 09：全部已读后空态前历史 4 条在列；清空基线后空态「暂无提醒」+ 红点消失 | ✅ |

实现要点：通知为**全局行**（共享单账本，不按用户分），`GET /notifications` 按 id 倒序返回 `{list, unread}`；预算超支复用 8.2 的 `Evaluate`（保存后立即评估 + 每日 08:00 cron 补跑），还款评估挂同一每日 cron（`"0 8 * * *"`），周期补齐只在懒补齐路径产生（cron 静默，幂等去重键保证不重复）；失效时机=流水写入 / 预算保存 / 补齐 `generated>0` / 标已读，另 60s 轮询兜底 cron 追加；按钮文案用 4 字「标为已读」避免 antd 两字中文自动加空格与「已读」状态标签撞车。

e2e 收尾基线恢复：提醒 0 条、预算表空、周期模板与生成笔清空、2026-10 支出回到 922.66（e2e 09 全过）。

## 组 11 导出与二期收尾（tasks 11.1—11.4，对照 specs/transactions 导出部分 + 二期四 spec 全场景）

### 11.1 CSV 导出端点

`GET /api/v1/transactions/export`（`middleware.Auth`，四档可导）：共享 `applyFilter` + `TxnStore.ListAll` 保证与列表同筛选口径，`buildFilter` 与 List 同参；`ExportCSV`（`txn_export.go`）产出 BOM `\xEF\xBB\xBF` + CRLF，表头 `日期,类型,分类,账户,转入账户,金额,备注,来源`，带符号金额（收入 +、支出/转账 −），`csvCell` 对 `=`/`+`/`-`/`@` 前缀加 `'` 转义（纯数字如 `-100.00` 豁免）+ RFC4180 引号转义；附件头 `transactions-YYYY-MM-DD.csv`、`text/csv; charset=utf-8`。

- 单测 `txn_export_test.go` 4 个（BOM/表头/行数=List total、公式四前缀+金额豁免、RFC4180 逗号引号、关键词与区间筛选行数=命中数）全绿
- API 冒烟 `smoke-export.ps1` **17/17 PASS**：BOM 三字节 `239 187 191`、全量 7=7、kind=expense 5=5、四种公式前缀各转义 1 行、删除后基线恢复 922.66、匿名 401、viewer 可导出含 BOM
- `gofmt` / `go vet ./...` / `go test ./... -count=1` 全绿

### 11.2 前端导出按钮

`Records.tsx` `page-head` 新增 `.head-actions`（导出 + 记一笔），四档均可见；`hooks.ts` `exportTxns` 与列表同参但不分页、`responseType: blob` 30s、从 Content-Disposition 取文件名触发下载。**走查 `frontend/e2e/export.mjs` 20/20 PASS**：默认区间导出（BOM 三字节、表头与列表列一致、行数=「共 7 笔」、UTF-8 解码无 U+FFFD、分类全路径中文、支出负号）→ kind=支出 5=5 → 叠加关键字「午饭」1=1 → 与后端 API 同参 total 一致 → viewer 含 BOM → 基线 922.66 未动。Excel 中文不乱码由 UTF-8 BOM 保证（Excel 依 BOM 判码）。顺带修复 walkthrough.mjs 两处登录后未等 react-query 返回即 count 的时序竞态，回归 **52/52 连跑 2 次全过**。

### 11.3 自动化备份（对照 design D10）

`docker/backup.sh` 重写：`docker exec mysqldump --single-transaction --routines --triggers` → `gzip -f`（同秒撞名不假失败）→ `find -mtime +30` 清理 + 残留 `.sql` 兜底 → 状态 JSON `backups/last-backup.json` 成功/失败都写（失败保留 `last_success_at`）→ `RCLONE_REMOTE` 已设才推送，否则 `push=skipped`；推失败整体失败 exit 1。

- 状态读取 `backend/internal/backup`：`Read(envPath)` 候选 env `BACKUP_STATUS_FILE` → 仓库根 → backend 目录，缺失/损坏返回**可见异常态**而非静默 ok；挂进 `/api/v1/health` 响应 `data.backup`；compose api 挂 `./backups:/backups:ro` + `BACKUP_STATUS_FILE=/backups/last-backup.json`
- 单测 `status_test.go` 4 例全绿（缺失=异常、显式路径不回退、损坏=异常、失败态保留上次成功时间）
- rclone 部署（WSL，v1.75.1）：该构建无独立 oss backend → `type=s3 provider=Alibaba endpoint=oss-cn-beijing.aliyuncs.com region=cn-beijing` 走 OSS S3 兼容 API；AK/SK 与 crypt 密码（本地随机生成、`rclone obscure`）只进 `~/.config/rclone/rclone.conf`（600，不进 git）；crypt 远端 `jizhang-oss:westar-dev/jizhang-backup`
- **关键坑**：OSS HeadBucket 行为让 rclone 误判 bucket 不存在走 CreateBucket（409 BucketAlreadyExists）→ 脚本加 `--s3-no-check-bucket` 修复
- 三路退出码：坏远端 fail=1 / 未配置 nopush=0 / 正常 oss=0；最终状态 `ok:true push:ok`
- **OSS 侧产出验证**：`rclone lsl jizhang-oss-crypt:` → 解密名 `jizhang-20261002-093402.sql.gz` 3852B；底层 `jizhang-oss:westar-dev/jizhang-backup/` → 加密名 `iocat4ujir…150` 3900B（含加密开销），文件名加密生效
- **人为失败可见异常**：坏远端名 → 状态 `ok:false push:failed` + `/api/v1/health` 返回完整错误（`last_success_at` 保留上次成功 09:34:02）→ 恢复成功
- **cron**：WSL crontab `0 3 * * * cd … && RCLONE_REMOTE="jizhang-oss-crypt:" sh docker/backup.sh >> backups/backup.log 2>&1`（systemd cron active+enabled）；用 2 分钟后临时探针实测 cron **真实触发写入** 后移除（教训：crontab 字段序=分 时，反了会 bad hour；时/分前导零 09 被当非法八进制）
- **6.3 备份演练**：dump 4.0K → 解压 24K 导入空库 `jizhang_restore_drill` → 11 张表行数逐一比对全 OK（users 4 / accounts 5 / categories 36 / transactions 7 / settings 1 / audit_logs 22 / 其余 0）→ 恢复库 2026-10 支出 **922.66** 与原库一致 → CJK 前 5 分类「餐饮/早餐/午餐/晚餐/外卖」完好 → 临时库 DROP

### 11.4 二期 spec 逐条走查（22 场景）

本轮全量回归：**periods 29/29、budgets 29/29、recurring 40/40、notifications 47/47、export 20/20、walkthrough 52/52** 六套全绿；`gofmt`/`go vet`/`go test ./... -count=1` 全绿；`openspec validate add-ledger-system --strict` 通过。**未发现新不符项**（组 7–10 发现的不符项已在各组修复，见上文「不符项与修复」节）。

| spec | 场景 | 证据 | 结果 |
| --- | --- | --- | --- |
| billing-periods | 结算日为 26 日 | e2e periods 02–03：本期 `2026-09-26 ~ 2026-10-25`、上期 `2026-08-26 ~ 2026-09-25` 逐字命中；单测 `TestSpecScenario26CurrentPeriod` | ✅ |
| billing-periods | 未配置结算日 | e2e periods 01：自然月 `2026-09-01 ~ 2026-09-30`；单测 `TestKeyForDateNaturalMonth` | ✅ |
| billing-periods | 跨期日期归期 | 单测 `TestSettlementDay26Boundary`：09-25 与 09-26 落不同账期键（9 月 vs 10 月），9/25 归 8/26–9/25 期 | ✅ |
| billing-periods | 中途修改结算日 | 单测 `TestSettlementDayChangeKeepsHistory`：1→26 后历史 period_key/封账状态不变、新写入按新规则归期；冒烟 stats 922.66/1000 无扰动 | ✅ |
| billing-periods | 封账确认 | e2e periods 04–07：封账弹窗展示该期收支结余，确认后状态「已封账」 | ✅ |
| billing-periods | 封账期内修改流水 | 单测 `TestClosedPeriodBlocksWrites`（三角色增改删全拒）+ e2e：封账期 UI 写操作被拒提示 | ✅ |
| billing-periods | 解封、修改、重新封账 | e2e periods：解封→改→再封全程，审计表 4 条留痕（谁/何时/哪一期/动作，含 `2026-09`+`admin`）；单测 `TestClosingAuditTrail` | ✅ |
| budgets | 配置本期预算 | e2e budgets 01–03：总 8000/餐饮 3000 → 首页进度卡 + 统计页进度卡同口径 | ✅ |
| budgets | 计算分类消耗 | 单测 `TestBudgetConsumptionScope`（收入 5000/转账 1000 不计、子分类上卷）+ e2e 消耗 922.66 口径一致 | ✅ |
| budgets | 超支标红与提醒 | e2e budgets：餐饮 3000→超支 422.66 进度条标红 + SQL 断言首超 1 条提醒（分类名/超出金额/去重键）、重复保存不重复 | ✅ |
| recurring-entries | 创建房租模板 | e2e recurring：建「每月 1 日 2500 银行卡→居家/房租」模板，四频率 Segmented、仅管理员 | ✅ |
| recurring-entries | 到期自动生成 | e2e：打开应用生成带「自动」标记流水（`source=recurring`）；进程日志 cron `recurring generated count=1`（9.2 证据） | ✅ |
| recurring-entries | 停机跨期补齐 | e2e recurring：start=08-01 跨期一次补齐 08/09/10 三期；9.3 停 cron 实测打开应用补齐 | ✅ |
| recurring-entries | 重复触发幂等 | 单测 `TestRecurringEnsureIdempotent` 连触 3 次仅 1 笔（唯一键 `(rule_id, entry_date)`）+ e2e 再开页面不重复 | ✅ |
| recurring-entries | 修正生成金额 | e2e：生成笔 100→120，模板金额仍 100、下期按 100 重新生成 | ✅ |
| recurring-entries | 删除生成笔 | e2e：删笔模板保启用、next_run 重写后照常生成；单测 `TestRecurringEditDeleteIndependent` | ✅ |
| notifications | 预算超支提醒 | e2e notifications 01–02：真实超支生成提醒含分类名与超出金额 | ✅ |
| notifications | 临近还款日提醒 | 单测 `TestRepaymentWithinLeadDays`（账户名 招行信用卡/欠款 500.00/还款日 2026-10-10/Days=2）+ e2e 05 渲染「信用卡欠款 ¥… 距还款日 N 天」；无欠款不提醒 `TestRepaymentNoDebtNoReminder` | ✅ |
| notifications | 周期记账补齐提醒 | e2e notifications 06：懒补齐真实链路生成 `recurring_patched` + 正文「已补记 1 笔」 | ✅ |
| notifications | 未读红点 | e2e 02：2 条未读 → 铃铛红点数字 2（>99 显 99+） | ✅ |
| notifications | 全部已读 | e2e 04：未读归零红点消失、历史 4 条仍可看、按钮转禁用 | ✅ |
| transactions | 导出筛选结果 | e2e export 20/20：BOM `EF BB BF`、内容与列表一致（行数=「共 N 笔」、表头=列名、全路径中文）、UTF-8 无替换符（Excel 不乱码前提）、筛选叠加口径与 API total 一致 | ✅ |

## 组 12 三期：暗色模式（task 12.1，对照 specs/ui-shell「暗色模式」）

走查脚本 `frontend/e2e/darkmode.mjs` → **17/17 PASS**，截图 `frontend/e2e/shots/dark-01~07`；回归 walkthrough **52/52**、periods **29/29**、budgets **29/29**、recurring **40/40**、notifications **47/47**、export **20/20**；`npx tsc -b --force` 0、`npm run build` 成功（tsc --noEmit && vite build）。

| spec 场景 | 验证方式 | 结果 |
| --- | --- | --- |
| 手动切换并保持：手动切暗后刷新仍为暗色 | e2e 04–08：「我的」页外观切「暗色」→ `data-theme=dark`、body `rgb(9,9,11)`、面板 `rgb(24,24,27)`、`localStorage['jizhang-theme']='dark'`、`style.colorScheme='dark'` → **reload 后仍为暗色**（`dark rgb(9,9,11)`） | ✅ |
| 跟随系统：设为跟随且系统切深色 → 自动切暗 | e2e 11–13：切「跟随系统」（值 `system`，当时系统浅色→浅）→ `emulateMedia({colorScheme:'dark'})` → **自动变暗**（`dark rgb(9,9,11)`）→ 切回浅色 → **自动回浅**（`light rgb(244,244,245)`） | ✅ |
| 可选择跟随系统或手动切换，选择持久化本地 | e2e 02/05/11：默认「跟随系统」；手动「暗色」写入 `localStorage`；切回「跟随系统」后值为 `system`（持久化三种取值） | ✅ |
| 手动选择优先于系统 | e2e 09：手动暗色 + 系统浅色 → 仍为暗（`dark`），避免「跟随系统」态下被系统反复覆盖 | ✅ |
| 暗色同时作用于自绘 CSS 与 AntD | e2e 06/07/14：Segmented 底 `rgb(245,245,245)→rgb(9,9,11)`（`darkAlgorithm`）、侧栏选中项文字 `rgb(45,212,191)`、主按钮底 `rgb(15,118,110)→rgb(16,103,97)` 两套 token | ✅ |
| 同一代码库响应式：移动端同样生效 | e2e 15–17：390×844 视口 + 系统深色 → 页面暗色，底部 Tab 栏面板底 `rgb(24,24,27)`、非选中 Tab 文字 `rgba(244,244,245,0.6)`；系统切浅色自动回浅 | ✅ |
| 图表在暗色下可读 | e2e 10 + `shots/dark-03`：统计页 4 个画布照常渲染，轴标签 `#a1a1aa`、网格 `rgba(244,244,245,0.08)`、图例/浮层文字浅色、饼分隔边改面板色，无白底白字 | ✅ |

实现要点：`stores/theme.ts` 存 `mode`（`light|dark|system`，默认 `system`）与 `sys`（系统当前配色），`resolve()` 算生效主题、`applyTheme()` 写 `<html data-theme>` + `color-scheme`；`components/ThemeProvider.tsx` 挂 `ConfigProvider`（`dark ? theme.darkAlgorithm : theme.defaultAlgorithm`）并监听 `matchMedia('(prefers-color-scheme: dark)')` 的 `change`（仅在 `system` 下影响结果）；`index.html` 内联脚本在 CSS/JS 之前预置 `data-theme`，消除刷新亮暗闪动。暗色设计取值：页面 `#09090b`、面板/侧栏/Tab `#18181b`、描边 `rgba(244,244,245,0.1)`、正文 `#f4f4f5`、次级 `#a1a1aa`、收支语义提亮 `#4ade80/#f87171`；**主色仍单一青绿**——填充类保持 `#0f766e`（白字对比度），文字/图标级提亮为同色相 `#2dd4bf`（否则暗底上 3.3:1 不达 4.5:1）。CSS 侧把 9 处硬编码收敛为主题变量（`--tab-fg/--sel-bg/--hover-soft/--hover-tint/--flash` 等），`@keyframes row-flash` 直接引用变量即可双主题复用；ECharts 画布不读 CSS 变量，故 `EChart.tsx` 内置 `applyDark()` 只补「颜色」不动业务配置（调用方显式颜色优先），`Stats.tsx` 按主题取 TEAL/GREEN/RED/PALETTE/饼边/上期柱。

本轮**未发现 spec 不符项**；过程中修正的是走查脚本自身的 3 处取样错误（登录页读了 body 而非容器、菜单/Tab 取到选中项而非目标项）与 1 处截图口径（移动端布局视口 414×896 与 `fullPage:false` 截取 390×844 不一致 → 对齐仓库既有约定 `fullPage:true`，底部 Tab 栏才入画）。主题偏好只写 localStorage，全程不触碰业务数据，结束态为默认「跟随系统 + 系统浅色」，基线未变。

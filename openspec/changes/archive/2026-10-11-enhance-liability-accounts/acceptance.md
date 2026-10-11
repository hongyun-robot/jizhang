# 负债账户验收记录

验收日期：2026-10-10。实现范围为消费信用账户、月度账单、转账还款、固定分期、手填手续费、退款核对、提前结清和站内提醒。金额使用后端 decimal 与前端整数分；账本收支归期与负债账单周期分开。

## 七份 delta 的逐项核对

| Delta / Requirement | 实现与证据 | 结果 |
| --- | --- | --- |
| accounts：账户类型与默认种子 | 信用卡、花呗、抖音月付、白条和其他消费信用；默认负债待配置、零余额启用；AccountSetupAndHistoryGuards、E2E 管理员创建 | 通过 |
| accounts：流水关联账户 | 事务绑定的 RecordChange 读取 Effects 与转账双侧；OrdinaryMutationAndAllFundDirections、VoucherAndRecurringShareProjection | 通过 |
| accounts：余额与总资产 | 原资金聚合保持不变，未来费用仅预估；CrossCycleAcceptance 独立逐笔汇总余额、统计和预算 | 通过 |
| accounts：删除约束 | 有流水、账单、计划或财务操作历史时保护类型、期初与删除；停用不阻止既有还款和费用 | 通过 |
| accounts：查看与管理入口 | 全员 /liabilities；账户设置保持管理员门禁；RoleBounds、E2E 四角色及移动端 | 通过 |
| accounts：消费信用类型目录来源 | 类型、名称、排序读取数据库；移除 Go 启动默认数据与前端清单；数据库新增类型后配置、缺省类型与标签直接生效 | 通过 |
| transactions：编辑与删除 | 普通修改和删除在同一财务事务内撤销旧投影；已出账快照保留；受管理来源普通入口拒绝 | 通过 |
| transactions：本金与费用表达 | 消费本金一次支出、还款一笔转账、费用 purpose=regular；来源、账单、计划和结清可追溯 | 通过 |
| transactions：各资金入口一致 | 手工、周期、券与负债操作共用协调入口；券核销和作废无资金影响；VoucherAndRecurringShareProjection 与原券回归 | 通过 |
| refunds：资金与账单抵扣 | 原消费未还部分优先，已还及超额退款成为待抵扣；其他到账账户不替原负债还款 | 通过 |
| refunds：本金与费用分别核对 | 实际退款入账后计划 needs_reconcile，未来期次合计不可抹除本金；费用不自动退款；E2E 与 CrossCycleAcceptance | 通过 |
| refunds：跨期与历史保护 | 按退款自身日期和封账权限校验；保留原消费与出账快照；修改、删除退款更新抵扣及计划版本 | 通过 |
| notifications：提醒类型 | 使用实际账单到期日及未还额，提前三天、到期当天和逾期次日；旧总欠款用例已改成账单模型 | 通过 |
| notifications：更新与解除 | 单账单去重；初始与当前金额分开；还清 resolved_at、保留 read_at；撤销只激活一次；ReminderResolveAndReactivate | 通过 |
| liability-billing：独立周期与日期 | 1–31、短月、闰年、账单日含当天、次日归下期、跨年和无效同月组合；CalendarAndFees | 通过 |
| liability-billing：自动出账与追溯 | 启动、五分钟任务、每日提醒前和懒补齐；周期/券先补齐，手续费先于出账；MySQL 并发只形成一个账单 | 通过 |
| liability-billing：总欠款分解 | B=C-I-U；后续本金是未出账子项；预估费用不计余额；新消费不让已还旧账逾期 | 通过 |
| liability-billing：还款与分配 | 最早到期优先、手工多账单、权限、无效目标整笔回滚；相同请求键原结果；ManualAllocationAndReallocation、MySQL 和 E2E | 通过 |
| liability-billing：待抵扣与部分还款 | 500 欠款还 600 后消费 80，余额正 20；1000 已出账、2000 后续本金收到 1200 后留 200 待抵扣；PrepaymentAndRetry、E2E | 通过 |
| liability-billing：核对与归属 | 平台参考不覆盖余额；480/500 通过补记20解决；移账保留原因、前后金额和出账快照，日期/period_key/预算不变 | 通过 |
| liability-billing：设置与历史 | 当前及已出账规则冻结，下一周期生效；未来归属预览与前后快照；RuleChangesFreezeAndRemainContinuous | 通过 |
| liability-billing：从零启用 | 不重建存量负债，不清零；检测旧流水或非零期初则拒绝启用 | 通过 |
| liability-billing：角色权限 | 全员完整读取，管理员设置，admin/recorder 管理全部，member 修改自己的来源，viewer 手工写入403；复合来源逐项校验 | 通过 |
| liability-installments：固定分期 | 新消费一次入账、已有消费仅重排未还本金；整数分尾差；已还/已退部分有核对依据 | 通过 |
| liability-installments：手填手续费 | 每期固定、总额分摊、一次收取；选择资产或原负债、叶子分类和实际日期；20/10/60/零费 E2E | 通过 |
| liability-installments：费用补齐 | 期次唯一关联；封账、账户/分类失效或待核对时阻塞，修复后一次补齐；FeeBlockingRecoveryAndManualLink、MySQL | 通过 |
| liability-installments：调整与保护 | 未来本金总额守恒、费用和归属可调并留前后快照；已发生费用通过真实退款处理，已有手工费用可关联 | 通过 |
| liability-installments：退款核对 | 已还1000、退款500后未还1500；普通退款入口导航到计划，无需重复记退款；CrossCycleAcceptance、E2E | 通过 |
| liability-installments：提前结清 | 展示已还、未还、已发生费用、手填费、待抵扣与拟取消费用；实际本金和版本校验；结清未来本金转入独立结清单避免重复出账 | 通过 |
| liability-installments：原子性与幂等 | 费用写入故障完全回滚；相同操作重复返回原结果；专用结清撤销逐笔权限和封账校验，恢复真实计划 | 通过 |
| voucher-platform-options：默认共享目录 | 000009 和启动种子预置常用九项；同名身份和创建时间保留，自定义名称保留；原生 MySQL 空库、升级和重复执行验证 | 通过 |

## 默认账户与目录补充验收

用户追加要求后，新增 `000009_default_credit_accounts_and_platforms`。空库得到原五账户及花呗、京东白条、抖音月付、美团月付；既有账本只补齐四项中的缺失名称，同名账户不改类型、期初、状态及账单规则。新增 `meituan_monthly` 的服务端校验与前端名称。平台 / 商家预置支付宝、微信、淘宝、天猫、京东、拼多多、抖音、美团、饿了么，继续允许自定义名称。

- 默认账户和平台已全部由迁移维护，Go 启动种子测试随重复种子删除；新账本、重复执行与定制数据保护由原生 MySQL 迁移测试覆盖。
- `TestLiabilityMySQL/default-accounts-and-platforms`：空库迁移保留原五账户、重复迁移、同名资产账户保护、既有目录身份/时间、数据 down 保留，通过。
- 后端 vet 和完整测试、前端 build、严格规格校验通过。
- 本地迁移9，dirty=0；读取接口确认四个新账户及九项默认平台，原流水仍为5条。

## 数据库目录来源补充验收

按用户纠正，删除 `seed/defaults.go`，移除服务启动的默认数据写入。新增迁移10，默认分类只在空分类树中初始化；类型目录统一作为服务端校验和前端选项/名称来源。

- `TestLiabilityTypesComeFromDatabase` 验证数据库自定义类型可配置、缺省类型来自数据库排序、未知类型拒绝及改名后的读取一致。
- `TestLiabilityMySQL/database-catalogs-and-categories` 验证空库36个分类、11个一级分类、六种初始类型，以及重跑保留定制类型名称、定制分类和新增类型，down 保留数据。
- 页面 E2E 增加两项：选择只有测试数据库定义的新类型并保存、详情展示数据库名称；完整37项通过。
- Go完整测试/vet、原生 MySQL 负债和券回归、前端构建通过。本地迁移10，dirty=0；类型读取接口返回六项，账户9个、分类36个、流水5条。

## 核心完整用例

`TestLiabilityCrossCycleAcceptance` 在零余额账户中记录10月500元普通消费及3000元三期消费、实际20元费用；11月1720元还款留下200元待抵扣；本金退款500元后核对1500元未还本金；结清使用200元抵扣与1300元转账，另从资产支付15元费用、取消未来40元。每一步独立核对流水余额、B=C-I-U、账单、收支和预算。封账后撤销拒绝且无部分结果。

## 实际执行的验证

- `go vet ./...`、`go test ./... -timeout 90s`：通过，既有账户、退款、券、周期、预算与封账回归通过。
- 原生本机 MySQL 随机独立测试库：空库迁移、默认账户升级、非破坏性 down、相同请求并发还款、费用与出账补齐、还款/退款/结清竞争及封账竞争通过。既有 VoucherMySQL 与 VoucherPlatformMySQL 通过。
- `npm run build`：通过。构建仍提示主包超过500KB，此项不影响功能验收。
- 负债浏览器 E2E：35项交互检查通过；包含四角色、桌面/移动端、消费/费用/还款/退款/核对/结清/撤销、480/500差异、待抵扣、新记账转分期、三种费用模式、手动分配、资产费用关联跳转和提醒解除。
- 原券浏览器 E2E：桌面/移动端购券、核销、退款、作废、撤销、自动暂停、CSV与只读回归通过，无页面异常。
- 本地账本已升到 migration 8，dirty=0；前后均为5条流水；负债读取接口返回成功。没有向真实账本添加测试流水。

## 个人负债归属补充验收

用户确认“仅本人可查看和管理”。迁移11增加归属和个人模板初始化标记；每个登录账号可自助建立多份同平台账户，账户日期独立。资料创建/设置对全部登录账号开放，手工资金操作继续拒绝只读角色。共享资产保留原权限。

- `TestPersonalLiabilityAccountsAndIndependentRules`：成员建立两份花呗账户、不同日期；管理员和其他账号直接读取/设置拒绝；自己的资料可管理，通过。
- `TestPrivateLiabilityCannotBeAccessedThroughMoneyOrNotifications`：外部还款、改删、流水和CSV、提醒读写、资产明细隔离，通过。
- 原生 MySQL `personal-defaults-concurrency`：八次并发访问只建立五份个人默认账户，删除后再次访问不重建，管理员不能修改成员账户，通过。
- 负债页面53项检查：四角色的个人账号、同平台多账户、独立日期、跨账号403，以及原分期/手续费/退款/结清/提醒和移动端回归，通过。
- 本地迁移11，dirty=0；管理员读取接口只返回所属负债账户，原流水仍为5条，分类36个。
- 新增权限要求覆盖本文件较早阶段的“四角色读取同一负债/管理员统一设置”结论；后续归档以个人归属规格为准。

## 与现有 change 的规格合并要求

`accounts` 的三个重叠需求同时受 `add-vouchers` / `add-voucher-platform-options` 影响。将本 change 同步或归档时，必须保留券购入/退款影响资金、核销/作废不影响资金、未用券单列、平台商家快照与目录等已实现行为，再合并消费信用账单补充。不要用旧版“所有支出都扣余额”覆盖 Effects 口径。本次未归档或替换其他 change。

新增共享目录 delta 按用户追加要求覆盖旧版“目录不预置名称”的约束。当前主规格尚无 `voucher-platform-options`，同步/归档时应先合并 `add-voucher-platform-options` 的目录基础规格，再应用本变更的 MODIFIED 默认目录要求；严格校验给出此依赖的信息提示，实施不受影响。

本 change 的实现及验收完成后，可由用户选择进入归档流程。

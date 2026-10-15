# 平台 / 商家个人目录迁移

迁移为 `000015_personal_voucher_platforms`，在现有账户与分类个人归属迁移之后执行。平台 / 商家目录改为每个账号独立，全部有效登录角色（含只读成员）都能维护本人条目，管理员不能越过归属；券、记账、分期等资金写权限保持原规则。

## 切换与保留数据

1. 暂停业务写入及后台任务，备份数据库，在独立恢复库验证 up / down。迁移预检拒绝无有效主要资金账户归属的非空历史名称，以及不满足名称长度规则的数据；预检失败时不继续改变目录结构。
2. 保存旧目录快照。旧表没有创建者，所有升级时已有账号各获得旧共享目录的独立副本，保留未用于券或流水的自定义名称。初始管理员保留旧名称标识，其他账号获得新标识；同一规范化名称若有多个旧条目，保留最小旧 ID 对应的创建时间，映射表保留全部来源。
3. 补齐稳定默认名称和本人保留券 / 流水中的缺少名称，按主要资金账户或券支付账户的 owner 归属，而不是历史创建人或转账另一端。名称按同一 Unicode 首尾空白规则规范化，不改写原财务文字；保留的软删除券也纳入引用约束。
4. `voucher_platforms` 增加非空 owner 及用户外键，唯一约束改为 `(owner_id, name)`。为已有账号设置完成标记；新账号首次使用时从数据库 `voucher_platform_templates` 初始化九个默认名称，初始化事务失败可重试，完成后不恢复已删除或重命名项。
5. 协调切换前后端并刷新旧页面会话。验证四角色本人 CRUD、跨账号拒绝、字符串表单兼容及后台处理归属，再恢复写入。

迁移不会修改券、流水的身份、金额、日期、创建人、平台文字或账户余额。管理页重命名只改变本人候选项；删除会拒绝仍被本人保留券 / 流水使用的名称，其他账号的同名记录不影响本人删除。

## 回滚限制

迁移保存 `platform_catalog_before`、`platform_catalog_mapping` 与迁移后目录、模板、用户初始化状态基线。down 先比较当前状态，全部一致才恢复原共享目录；个人目录维护、模板变化、新账号或初始化变化都会使自动回滚被拒绝，拒绝时不删除现有个人数据或财务记录。已经继续使用的个人目录不能直接合并为共享目录，应保留当前结构并通过经过验证的备份或专门数据迁移处理。

## 独立验证

在本机启动 MySQL 与 Redis。设置 `VOUCHER_MYSQL_DSN`（或 `PERSONAL_CATALOG_MYSQL_DSN`）后，在 backend 运行：

```powershell
go test ./internal/service -run '^TestPersonalPlatformMySQL|^TestVoucherPlatformMySQL' -count=1 -v
```

测试只创建并清理随机命名的独立数据库，不使用 DSN 中指定的账本库。覆盖空库、历史副本、原创建人不同、软删除历史、初始化与同名并发、删除 / 记账竞争、故障回滚和拒绝破坏性 down。

浏览器使用 README 中的本机隔离 fixture，在 frontend 运行：

```powershell
node e2e/platform_management.mjs
node e2e/voucher_platforms.mjs
node e2e/transaction_platforms.mjs
node e2e/personal_catalogs.mjs
node e2e/mobile_selection_lifecycle.mjs
```

管理页脚本产出 1280px、360px、767px、768px 和 390px 截图，验证明暗主题、长名称、键盘操作、错误态、草稿保留与退出 / 换号后的迟到响应隔离。分期选择器使用独立负债 fixture 运行 `node e2e/liability_platform_totals.mjs`。

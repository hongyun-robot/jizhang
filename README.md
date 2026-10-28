# 记账（jizhang）

自用家庭记账站点：Web + 移动 H5 一套代码，Go + MySQL + Redis 后端，React + Ant Design 前端。

## 多设备登录

同一个账号可在手机、平板、电脑上同时登录，每类最多保留一个有效会话。同类新登录成功后立即替换旧会话，例如在另一台电脑或换浏览器登录，会让原电脑浏览器失效；错误密码和登录提交前的服务故障不替换原会话。一个浏览器 Cookie 存储空间内的标签页共享登录；不同浏览器配置和无痕窗口按独立登录处理。

“我的 → 退出所有设备”会立即撤销该账号全部设备的 access/refresh 凭据。同浏览器标签页同步退出，其他设备在下一次请求或回到前台校验时回到登录页；暂不做跨设备实时推送。自然到期、刷新失败或被同类替换只结束对应会话，不触发全设备退出。管理员重置密码、停用或调整角色继续撤销全部设备。

默认 access 有效期 30 分钟，refresh 每次成功续期后再有效 30 天，沿用现有有效期配置。access 仅保存在内存，refresh 保存在 HttpOnly、SameSite=Strict Cookie。浏览器持久存储仅保存非秘密的会话标识、生命周期代次和退出待确认标记。设备类型按浏览器信息自动判断，兼容常见 iPadOS 桌面 UA，未知情况归电脑；修改标识或桌面模式可能误判，设备类型不作为硬件身份或权限凭据。

认证写请求使用 Web Locks 串行执行；不具备该能力时使用同源 SharedWorker 代理。两者均不可用时提示更换受支持浏览器或安全地址，不并行发送可能覆盖新 Cookie 的请求。Redis 或会话校验暂不可用返回可重试错误，受保护操作不执行。退出请求未得到服务端成功确认时，当前浏览器仍清理业务状态并阻止自动恢复，登录页显示“尚未确认其他设备退出”及重试入口。

升级此认证机制不修改 MySQL 财务结构。发布时须停止旧后端实例、同步切换前后端并轮换私有 `JWT_SECRET`，所有旧登录统一重新登录一次；旧页面提交登录会提示刷新。新版拒绝缺少机制版本或 sid 的旧令牌，旧刷新请求不能撤销新会话。旧 Redis 键 `refresh:*`、`refresh_user:*`、`session_epoch:*` 使用通用名称：只有确认实例或逻辑库为本应用专用后才可清理前两类旧登录数据，并保留旧 epoch；无法证明归属时保留其自然到期，不能清空整个 Redis。回滚须成对回退前后端、再次轮换密钥，并在已确认归属的范围内清理旧 refresh，防止旧服务重新接受随机旧凭据；不能安全清理时采用向前修复。无需回滚财务数据。

验证使用下文的独立券 fixture 和测试 Vite，在 `frontend` 顺序运行 `node e2e/multi_device_login.mjs`、`node e2e/users.mjs`、`node e2e/voucher_retry.mjs`。其中多设备脚本直接验证前端会话协调模块，需要测试 Vite；用户管理脚本已改为临时账本，结束时停用随机测试账号，不删除真实用户。截图位于 `frontend/e2e/shots/multi-device` 和 `frontend/e2e/shots/refactor-frontend`。

`e2e/support/fixture.mjs` 的 `login` 会记录当前页面的认证响应；`fixtureApi` 在 UI 登录后复用已捕获的当前凭据，准备和延后 cleanup 也不复用被替换的旧 token。自定义登录流程须先调用 `registerFixturePage`；权限断言可用 `fixturePageRequest`，不要额外调用登录接口。辅助函数在 Vite 和生产预览均可使用，始终保留本地 fixture 身份检查。测试客户端的直接登录请求须携带 `protocol_version: 2`。

本机真实 Redis 脚本验证在 `backend` 设置 `AUTH_REDIS_ADDR`（例如 `127.0.0.1:6380`，如需要另设私有 `AUTH_REDIS_PASSWORD`），执行 `go test ./internal/service -run '^TestNativeRedisSessions$' -count=1 -v`。测试使用独立随机键空间，验证并发、轮换、到期、撤销与读取故障，只清理本次测试键，不清理本机账本会话。

## 个人分类与资金账户

全部有效登录角色（含只读成员）都可在“我的 → 分类管理 / 账户管理”维护自己的选项。分类、资产和负债按登录账号独立，同名也具有不同标识；管理员同样不能管理他人的个人目录。只读成员的目录管理能力不授予记账、购券、还款或分期权限，账期和用户管理仍限管理员，预算按本人归属配置。

记账、转账、购券、周期模板及负债操作中的可编辑账户字段提供“新建账户”：在选项内填写账户资料，成功后选中真实账户 ID，保留父表单草稿。取消或搜索不创建；创建成功后放弃父表单会保留账户。手机打开先显示选项，主动输入才唤起键盘；失败可用同一请求标识重试。名称最多 64 个 Unicode 字符，账户不会按同名合并资金。分类仍在管理页维护，筛选器与锁定字段仅选择已有资源。平台 / 商家同样按账号独立，可从“我的 → 平台 / 商家管理”维护本人名称。

迁移 `000012_personal_catalogs` 保留原账户和资金，将旧共享资产按资金链整体归属，为各账号建立独立分类与零余额默认资产。默认模板在数据库中维护，个人初始化完成后不会因重启、重新登录或刷新恢复已删除记录。历史跨账号资金事件保留本人影响并以只读说明展示；失配周期模板和后续费用来源需要本人修复。统计、券和提醒按个人范围读取；预算在 `000022_personal_budgets` 后同样按账号独立配置，分类预算不按同名跨账号复用。

升级须先备份并暂停写入。归属、对账 SQL 与有后续写入时拒绝自动 down 的说明见 [个人目录迁移说明](docs/personal-catalog-migration.md)。验证结果与截图见 [本次验收记录](openspec/changes/archive/2026-10-13-personalize-categories-and-accounts/acceptance.md)。独立 MySQL 测试可设置 `PERSONAL_CATALOG_MYSQL_DSN`（也支持既有两种 DSN）运行 `go test ./internal/service -run '^TestPersonalCatalogMySQL' -count=1 -v`；浏览器 fixture 启动后运行 `node e2e/personal_catalogs.mjs`、`node e2e/mobile_selections.mjs` 和 `node e2e/mobile_selection_lifecycle.mjs`。

## 个人预算

每个登录账号可从“我的 → 我的记账选项 → 预算管理”独立设置各账期的总预算和分类预算，四档角色均可配置本人预算。首页、统计页和超支提醒只使用本人的配置与消费；清空预算不影响其他账号或其他账期，只读角色仍不能记账。

迁移 `000022_personal_budgets` 为各现有账号复制旧共享总预算的独立初始值，分类预算归属原分类所有者。之后新建账号默认未设置预算。升级前须备份并暂停写入及后台任务；迁移不改变流水或余额。个人预算或账号集合已有变化时，down 会拒绝自动合并，须保留个人配置并使用核验过的备份处理回退。迁移验证使用随机独立库：设置 `PERSONAL_CATALOG_MYSQL_DSN` 后运行 `go test ./internal/service -run '^TestPersonalBudgetMySQLMigration$' -count=1 -v`。浏览器 fixture 启动后运行 `node e2e/budgets.mjs`，可用 `BUDGET_E2E_BASE` 指定本机测试地址。

## 个人平台 / 商家目录

“我的 → 我的记账选项 → 平台 / 商家管理”提供本人列表、搜索、新增、重命名和确认删除。名称去除首尾空白后为 1 至 128 个 Unicode 字符；同账号精确同名复用，不同账号同名具有独立标识，英文大小写作为不同名称保留。

迁移 `000015_personal_voucher_platforms` 为已有账号复制旧共享目录的独立副本，保留尚未用于业务的自定义项，再补齐本人资金账户范围内的券和流水名称。新账号仅从数据库默认模板初始化九个常用名称，初始化只完成一次，删除或重命名不会因刷新、重登或重启恢复。券与流水继续使用可选字符串字段，目录维护不会批量改写历史文字或改变资金。

切换须暂停写入及后台任务并先备份；迁移记录旧目录、副本映射和迁移后基线。down 仅在个人目录、默认模板、初始化状态和账号集合均未改变时允许恢复；存在个人修改或新账号时拒绝自动合并。详见 [平台 / 商家迁移说明](docs/personal-platform-migration.md)。

## 个人目录拖动排序

分类、账户和平台 / 商家管理共用拖动排序：鼠标或手机按住左侧手柄移动，松开后自动保存；键盘聚焦手柄后用空格 / 回车拾起、方向键移动，再按空格 / 回车保存，Escape 取消。分类收支大类分别排序，点击大类进入子分类弹窗后可调整同父小项。账户包含基金、负债和停用项，共用一个顺序；商家搜索时须先清空搜索才能排序。新条目追加到末尾，改名和普通编辑保留位置；记账选项使用保存顺序，资金与历史文字不变化。

失败时恢复已确认顺序，可重试保存或刷新列表。重试复用原请求标识；另一设备已更改列表时提示冲突，刷新后再调整。全部登录角色均只能排序本人目录。

新增迁移 `000016_catalog_ordering`，保留既有商家显示顺序并增加个人排序请求结果记录，详见 [目录排序迁移说明](docs/catalog-ordering-migration.md)。验收在临时本地 fixture 中运行，不能指向生产账本：启动支持基金的浏览器 fixture（`FUND_BROWSER_SERVER=1`、`VOUCHER_BROWSER_SERVER=1`、`BROWSER_FIXTURE_ADDR=127.0.0.1:18186`），构建前端后运行 `node e2e/support/preview.mjs --dist=dist --port=5616 --api=http://127.0.0.1:18186`，再依次运行 `node e2e/catalog_ordering.mjs` 和 `node e2e/catalog_ordering_lifecycle.mjs`；可用 `CATALOG_E2E_BASE` 指定同类本机测试地址。MySQL 测试使用既有 DSN 和独立随机数据库，命令为 `go test ./internal/service -run '^TestCatalogOrdering|^TestAccountOrdering' -count=1`。

## 消费信用账户

“我的 → 负债账户”管理登录账号自己的信用卡、花呗、月付等账户。每人可创建多个同类型账户，在各账户详情设置独立账单日、还款日及当月/次月规则；仅本人可查看和管理，管理员也不能跨账号操作。日期支持1–31，短月份取月底。新负债从零余额启用，配置完成后才能记消费。

`000009_default_credit_accounts_and_platforms` 自动补齐花呗、京东白条、抖音月付、美团月付，并预置支付宝、微信、淘宝、天猫、京东、拼多多、抖音、美团、饿了么的平台 / 商家选项。同名已有账户与选项保持原身份和设置；新负债默认零余额、待配置账单日期。此数据迁移的 down 保留账户和选项，避免删除已使用或修改的记录。

默认账户、分类与平台 / 商家模板由数据库迁移初始化，个人实例只初始化一次，服务启动不会补建已删除的个人记录。`000010_database_catalogs` 增加 `liability_account_types` 类型目录并初始化空库分类；后端校验与前端类型名称、选项统一读取该表。新增消费信用类型或修改显示名称时更新数据库即可，应用中不再维护相同的名称清单。

`000011_personal_liability_accounts` 增加个人归属。现有负债保留原账户和流水，有单一历史记账人的账户归该人，其余原共享账户归初始管理员。每个账号首次访问时按数据库的默认类型模板创建个人账户并保存初始化标记；后续可另建同平台账户，删除的默认账户不会在重启时重建。各登录账号可管理自己的零余额账户资料，只读角色继续禁止还款等手工资金操作。资产账户的共享与管理员管理权限已由 000012 迁移替换，平台目录的共享机制由 000015 迁移替换为个人目录。

消费本金按实际消费日期记一次支出；还款从本人可支付资产账户转入负债账户，不重复计算支出。详情分开展示最近应还、逾期、未出账及后续分期本金。负债模块不再提供待抵扣、暂不分配或溢缴功能；还款、普通转入、收入和退款必须当次完整分配到合法实际欠款，超额请求整体拒绝。普通非分期欠款仍可部分还款；涉及分期时必须从最早未还期开始连续整期偿清本金及全部未付手续费。平台参考金额只用于核对，通过补记真实流水或调整归属解决差异。

普通“记一笔”选负债账户后可带着草稿进入固定分期，也可将已有消费转分期。手续费在创建或合法调整计划时手动填写，支持每期固定、总额分摊和一次收取，可由原负债或资产支付。未来费用只作预估，到所填实际日期或提前整期偿付时才计支出；封账或配置失效时显示待处理原因，条件修复后补齐。本金退款按实际到账记录，再核对剩余期次；不能完全冲减欠款的退款不能记入该负债账户，只有实际到账资产时才能改用该资产账户，不自动拆分或虚构返还。

分期计划的“按期提前还款”支持从最早未还清期选择连续一期、多期，也可一次还清全部剩余期数；金额锁定为真实剩余本金加对应全部未付手续费，包括未出账和未入账费用。已经支付、实际退款冲减和已取消的费用不会重复收取。所选期次本金、费用统一从本次还款账户支付，未入账费用按实际还款日逐期记录支出并绑定来源，原计划日期保留，后续不会重复收费。禁止跳期、一期内部分还款、单独还手续费和手填额外结清费。日期默认今天，可补记未封账且不早于原消费的过去日期，不能填未来日期。

每次提交先核对报价；期次、账户或日期改变后必须重新核对，后台金额变化会拒绝旧预览。同一次提交响应丢失时保留原报价和请求标识重试，不能复制成功记录再还一次。专门撤销按依赖从后往前恢复期次、原费用计划、资金分配和账单归属；相关账期封账或存在后继引用时拒绝。普通流水入口不能拆开删除组合中的转账或费用。

### 移除待抵扣功能的切换核验

本变更复用现有表和 JSON 操作记录，不需要新增或删除数据库结构。先用目标账本的服务端环境配置，在 `backend` 执行只读核验：

```powershell
go run ./cmd/liability-audit
```

输出 `unallocated_funds`（账户、原资金来源、真实未分配金额）及 `term_problems`（仅剩手续费、费用阻塞、退款待核对等）。没有未分配余额时退出码为 0，有余额时为 2，读取失败为 1；命令不改流水、不重分配、不写设置。新 API 启动也执行同一资金切换检查，有旧未分配余额时停止启动并保留全部原记录。账户负余额不代表没有未分配资金。

发现历史余额时，保留旧版本和核验报告，先按平台及银行真实记录处理：确实偿付已有欠款时记录完整对应分配；确实已返还资产时记录实际转出，并在同一财务事务中把原资金的未分配部分固定到该返还流水的债务明细，保留原已分配目标。`hold` 或固定目标资金不会仅因新建普通转出自动归零，不能让返还误抵分期本金。所有受影响来源须满足资源归属、权限和开放账期；无法满足时先核对，不能改创建人、伪造个人账户或绕过封账。技术处理的隔离数据库样例见 `TestLiabilityHistoricalUnallocatedRealReturn`，只读及切换保护见 `TestLiabilityAuditReadOnlyAndCutover`。

没有实际返还就不能为了升级生成返还，也不能删除旧还款、清零资金金额、改原流水或把余款改名隐藏。重新核验归零后再发布前后端同一版本；发布前备份财务数据。旧已结清、已取消费用及原请求结果继续可追溯，新资金一律执行新规则。只有手续费没有本金的异常期次需先核对，本次不开放该类独立还款例外。

新按期操作采用逐期费用、固定偿付目标及操作依赖，已写入后不能直接降级到只支持旧整笔结清的程序。需要回退时保留当前数据和兼容保护，或先按权限、封账及依赖合法逆序撤销相关新操作，核对费用绑定和资金不变量后再回退；不能用删真实流水作为回滚方式。

### 普通消费提前还款

普通未出账消费也可提前还款：在负债详情点「还款」，将「还款范围」从默认的「还已出账」切换到「提前还未出账」，选择本人扣款资产账户、实际还款日期和金额，点击「核对还款金额」后保存。可以部分或全部偿还已发生的普通消费，不限制下个月；可自动按到期日分配，也可手动指定多份未出账账单。比如 1000 元普通消费提前还 300 元，未还剩余 700 元，正常出账时只计剩余应还。此操作只登记实际已完成的还款，不连接银行或平台扣款。

未出账普通可还金额排除分期本金、分期手续费和还款日之后的消费，包含负债购券的真实本金；分期继续从「分期计划」操作。还款不能超出所选范围，不能预存抵扣未来消费；实际日期不得为未来日，还款自身收支账期必须开放。本金还款只记转账，不重复记消费或预算。普通提前还款固定关联本次消费，后续出账、补记消费和改还款日不挪用原资金；纠错从「还款与抵扣」或「操作记录」使用「撤销并恢复」，再按实际情况重新登记。

普通提前还款未增加数据库列，使用现有资金记录的 `ordinary_advance` 模式及 JSON 目标。发布须备份并配套更新前后端；产生该模式记录后，不能直接退回不识别固定目标的旧后端，否则会错误重算历史偿付。应保留兼容后端修复，不能把模式改为 auto 或清空目标作为回滚。

普通提前还款验收使用下述临时负债服务，执行 `node e2e/unbilled_ordinary_repayment.mjs`。该脚本验证桌面和 474px／390px 手机、预览失效、部分还款、响应丢失重试、正常出账、撤销、多账单及角色归属；截图位于 `frontend/e2e/shots/refactor-frontend/unbilled-ordinary-repayment-*.png`。每次脚本重新运行、或切换到既有分期验收前，重新启动临时后端以清空账本与时钟。MySQL 验证仍为 `LIABILITY_MYSQL_DSN` 加 `go test ./internal/service -run '^TestLiabilityMySQL$' -count=1 -v`，其中 `ordinary-advance-concurrency` 使用独立随机库覆盖并发及回滚。

### 按期还款的隔离验收

其他 worktree 正在开发时可使用独立测试端口。以下服务使用内存账本和临时 Redis，不使用目标 MySQL 账本：

```powershell
# backend
$env:LIABILITY_BROWSER_SERVER='1'
$env:BROWSER_FIXTURE_ADDR='127.0.0.1:18185'
$env:BROWSER_FIXTURE_TTL='60m'
go test ./internal/router -run '^TestLiabilityBrowserServer$' -count=1 -v -timeout 75m

# frontend，先 npm run build
node e2e/support/preview.mjs --dist=dist --port=5612 --api=http://127.0.0.1:18185

# frontend，另一个终端顺序运行
$env:LIABILITY_E2E_BASE='http://127.0.0.1:5612'
node e2e/installment_early_repayment.mjs
node e2e/liability_history_amounts.mjs
node e2e/liabilities.mjs
```

脚本先验证本机测试服务身份，再记录临时账本数据；桌面与手机截图保存在 `frontend/e2e/shots/refactor-frontend`。会改变时钟和销毁账本的脚本不并行运行，按需重新启动 fixture；结束后仅调用该测试地址的 `/__test__/stop`，停止本次预览服务。

分类选择统一使用记账表单的内嵌一级 / 子分类按钮，包含流水筛选和负债各类费用表单。负债账户可配置“分期本金尾差”放首期或最后一期，默认最后一期；设置仅影响新建分期，已有分期及手续费分摊保持原值。迁移 `000013_installment_principal_remainder` 增加账户偏好列，既有账户默认最后一期，不改财务金额；回滚只删除偏好，已保存的期次保留，新建分期恢复最后一期补差。

分期新消费的平台 / 商家复用记账的本人目录选择器。期次预览以一列显示“本金 + 手续费”合计，下方小字显示手续费；金额按元录入，默认在实际收取日计入当前负债账单，随本金一起还款。选择资产直接支付时，该费用不会加入负债应还金额。

历史详情、账单、还款记录、调整 / 结清预览及操作历史采用相同双行金额展示。历史“计划合计”保留预估与取消记录，真实“当前未还”仅含本金和已入负债的费用；账单明细按期合并本金 / 已发生费用，保留来源链接和真实退款抵扣。手续费在收取日记一次支出、还款记转账，首页和流水保留费用标识；界面调整不重算、不补生成历史金额。

新增迁移为 `000008_liability_billing`。不重建存量负债、不清零旧流水；有既有流水或非零期初的未配置负债会拒绝直接启用。有负债财务历史时，数据库 down 会拒绝删除该模型。

负债 MySQL 验证设置 `LIABILITY_MYSQL_DSN` 后执行 `go test ./internal/service -run TestLiabilityMySQL -v`，测试只使用随机独立数据库。浏览器验收在三个 PowerShell 终端运行：

```powershell
# 在 backend
$env:LIABILITY_BROWSER_SERVER='1'
go test ./internal/router -run '^TestLiabilityBrowserServer$' -count=1 -v -timeout 25m

# 在 frontend
$env:VITE_API_PROXY='http://127.0.0.1:18085'
npm run dev -- --port 5602 --host 127.0.0.1

# 在 frontend；默认使用已安装的 Chrome，可用 CHROME_EXE 指定
node e2e/liabilities.mjs
node e2e/liability_categories_remainder.mjs
node e2e/liability_platform_totals.mjs
node e2e/liability_history_amounts.mjs
```

脚本校验独立测试服务标识，覆盖消费、费用、出账、还款、核对、退款、结清、撤销、四档角色、提醒和移动端。完整规格验收见 `openspec/changes/enhance-liability-accounts/acceptance.md`。

## 券

“我的 → 我的券”管理单次券和多次券；首页/流水页的“记一笔”也能登记购券。

“平台 / 商家”支持下拉选择和输入搜索。没有所需名称时，可在原输入框中确认“创建「名称」”，或直接输入并随券保存；新选项仅供本人后续表单使用，其他账号可独立创建同名名称。点击创建后即使取消购券也会保留选项，仅输入或失焦不会创建。该字段可清空，名称去除首尾空白后最多 128 字，同名复用。

普通“记一笔”和“编辑流水”也提供可选的平台 / 商家字段，与购券共用选项目录。平台 / 商家独立保存，在流水列表和 CSV 中展示，也可作为搜索关键词；新增数据库迁移为 `000007_transaction_platform`。

平台 / 商家选项的读取、新增、重命名和删除按本人归属开放，四个登录角色均可使用，包括只读成员；管理员不能管理他人目录。目录操作不产生券或账务记录，购券、编辑券及流水操作仍按原权限执行。重命名仅改变候选名称，历史券和流水保留原文字；仍被本人保留记录使用的名称不能删除，软删除券也会保留引用。

小于 768px 的手机视口中，选项下拉统一使用底部选择面板：打开先展示已有选项，点击面板内搜索框才进入文字输入。账户、分类、账单和关联流水可搜索；角色、状态、月份等短枚举直接点选。平台 / 商家可创建本人名称，业务账户可就地创建个人账户；筛选器、分类和固定枚举只选择已有项。平台目录读取或独立创建失败时，可明确选择“使用此名称”回填草稿，再随父表单保存；搜索和取消不会改变原值。桌面下拉与日期选择器沿用原交互。

| 操作 | 账户余额 | 消费与预算 |
|---|---|---|
| 购券 | 按实付扣款 | 不计入 |
| 核销 | 不重复扣款 | 按核销日与实付分摊成本计入 |
| 退未用部分 | 回到原支付账户 | 不冲减已核销消费 |
| 作废 | 不重复扣款 | 剩余成本计入作废日 |

- 填写有效期时默认开启到期自动退款，也可关闭。有效期当天可用，按北京时间次日零点后五分钟内自动记录剩余退款；停机恢复或打开应用会补处理，仍按原计划日期记账。
- 自动退款按填写的配置记账，不连接平台执行退款或核实到账。平台实际没退时可撤销自动退款，系统同时暂停该券自动退款；确认后可手动退款或明确重新启用。
- 关闭自动退款的券过期后保留在待处理，用户选择退款、延长有效期或作废。未用金额按剩余实付成本单列，不并入账户总资产。
- 多次券前面各份按分向下取整，最后一份补齐差额。第一版退款只退全部未用份额；已消费份额退款、手续费和储值卡任意金额扣减不在券流程内。
- 撤销按操作倒序进行，保留撤销记录；已封账期间的事件需先解封。购买期封账不阻止开放新期核销/退款。券生成流水不能在普通流水页单独修改或删除。
- 所有成员可查看本人支付账户范围内的券，只读成员不可操作；普通成员仅可编辑自己的购买信息、撤销自己的操作，管理员与记账员也须满足个人资源归属。

发布券功能需备份并升级 `000005_vouchers`，先发布支持用途口径的后端，再配套前端。存在券数据时数据库 down 会拒绝执行，也不可仅回退到不识别券用途的旧应用；应保留当前 schema 修复，或恢复发布前备份及其匹配应用版本。

平台 / 商家下拉功能在其后升级 `000006_voucher_platforms`，自动从未删除券回填已有非空名称，先升级数据库和后端，再发布前端；原字符串接口继续可用。应用回退到仍支持券的上一版本时可保留新增目录表。数据库 down 仅删除目录表，执行前需导出目录，保留那些已创建但尚未用于券的选项。

## 券的集成验证

后端常规验证：在 `backend` 运行 `go test ./...` 与 `go vet ./...`。MySQL 验证设置 `VOUCHER_MYSQL_DSN` 后运行同一测试命令，账号需具备建库/删库权限；测试创建并清理随机命名的独立数据库，不复用 DSN 内的账本库。

浏览器验收使用独立的内存账本和临时 Redis，按三个 PowerShell 终端启动：

```powershell
# 终端一，在 backend
$env:VOUCHER_BROWSER_SERVER='1'
go test ./internal/router -run TestVoucherBrowserServer -count=1 -v -timeout 25m

# 终端二，在 frontend
$env:VITE_API_PROXY='http://127.0.0.1:18084'
npm run dev -- --port 5601 --host 127.0.0.1

# 终端三，在 frontend
node e2e/vouchers.mjs
node e2e/voucher_retry.mjs
node e2e/platform_management.mjs
node e2e/voucher_platforms.mjs
node e2e/transaction_platforms.mjs
```

脚本默认使用本机 Playwright 的 Firefox，可用 `FIREFOX_EXE` 指定已安装的浏览器文件。它先校验测试服务标识，只在本地测试账本里写入，覆盖桌面/手机购券、核销、退款、作废、撤销、自动退款暂停、CSV 和只读权限。重试脚本还模拟“已保存但响应超时”，验证修改金额后重试不会重复扣款；同一次表单提交沿用原请求标识，需刷新核对后才重新发起操作。截图与下载产物放在已忽略的 `frontend/e2e/shots`、`frontend/e2e/downloads`。

平台 / 商家脚本通过普通成员表单验证选择、键盘创建、取消保留、直接保存、编辑、清空及手机弹层；同时验证四个角色的个人目录接口、同名独立、越权与未登录拒绝、读取失败后提交、创建响应丢失后重试和迟到响应保护。MySQL 集成测试覆盖 000006 历史回填、账号内精确名称去重、并发初始化 / 创建、删除与记账竞争及财务事务回滚。

验收结束后调用 `Invoke-RestMethod -Method Post -Uri 'http://127.0.0.1:18084/__test__/stop'` 关闭测试后端，并停止测试 Vite 进程。

## 前端基础重构验证

在 `frontend` 运行 `npm run test:unit`、`npm run typecheck` 和 `npm run build`。`typecheck` 同时检查业务与测试源码。

完整浏览器套件较长时，启动 fixture 前设置 `$env:BROWSER_FIXTURE_TTL='3h'`，Go 的 `-timeout` 设为 `4h`；未设置时仍为 20 分钟，允许范围为 1 分钟至 4 小时。`-count=1` 防止 Go 返回已完成的测试缓存而没有启动服务器。

生产分包验收可在 `frontend` 用以下预览代替测试 Vite。预览只代理本机 fixture，测试辅助再次校验独立账本标识。

```powershell
node e2e/support/preview.mjs --dist=dist --port=5601 --api=http://127.0.0.1:18084
```

在另一个 `frontend` 终端顺序运行；资金、退出或换号测试共用账本，不并行执行。

```powershell
$scripts = @(
  'refactor_foundations', 'refactor_navigation', 'refactor_errors',
  'refactor_drafts', 'refactor_filters', 'refactor_cache',
  'vouchers', 'voucher_retry', 'voucher_platforms', 'transaction_platforms',
  'refactor_interactions', 'refactor_loading', 'refactor_background', 'refactor_keyboard',
  'refactor_repairs'
)
foreach ($script in $scripts) {
  node "e2e/$script.mjs"
  if ($LASTEXITCODE -ne 0) { throw "浏览器验收失败：$script" }
}
```

`refactor_loading` 需要生产构建，检查实际 JS/modulepreload 合计 gzip、资源失败恢复、主题和六档宽度。对子路径使用 `npx vite build --base=/jizhang/ --outDir=dist-subpath`，预览改为 `--dist=dist-subpath --port=5604 --base=/jizhang`，测试终端设置 `REFACTOR_E2E_BASE=http://127.0.0.1:5604/jizhang` 和 `REFACTOR_DIST=dist-subpath`。重建独立输出目录前应清理其旧构建产物，避免将过期文件算入总产物。

手机选择面板在同一独立券 fixture 的 5601 预览上顺序运行 `node e2e/mobile_selections.mjs` 和 `node e2e/mobile_selection_lifecycle.mjs`。前者覆盖搜索 / 选择 / 清空 / 创建限制、模拟键盘、分类路径、账户与角色保存和六档断点；后者覆盖读取失败重试、跨断点关闭、父弹窗关闭清理及真实明暗主题切换。它们仅写入已校验身份的临时测试账本，截图保存在 `frontend/e2e/shots/refactor-frontend`；iOS / Android 实体键盘仍需单独复核。

弹层滚动回归在同一 fixture 上运行 `node e2e/overlay_scroll.mjs`，默认使用 Chrome；设置 `$env:SCROLL_BROWSER='firefox'` 可用 Firefox 复核。覆盖桌面和手机尺寸下的快捷记账、备注框、账户下拉与选择浮层、购券、账户、负债、周期模板及筛选抽屉，验证滚动边界不带动父层、模拟键盘下按钮可达，以及关闭后页面恢复滚动。长账户目录仅在浏览器响应中模拟，不提交表单；截图保存在同一截图目录。该脚本为 Firefox 单独启用 `test.events.async.enabled`，让自动滚轮使用可遵循 `overscroll-behavior` 的原生事件路径；背景可参考 [Mozilla 的自动化事件问题](https://bugzilla.mozilla.org/show_bug.cgi?id=1848957)。

日期与输入框回归运行 `node e2e/mobile_dates.mjs`，覆盖中文月份 / 星期、手机单日 / 月份 / 范围选择、日期只读且可点选、390px 手机及 820px 触屏平板的输入字号，以及桌面日期键入。它检查登录、记账、筛选、购券、账户、负债、周期模板、分类、用户密码、设置、预算和统计页面，不提交业务表单；手机日历截图保存在同一截图目录。基金表单改用同一日期组件，并由 `node e2e/funds.mjs` 验证 YYYY-MM-DD 日期值及保存行为。自动验收使用浏览器触屏模拟，iOS 实机的系统键盘和聚焦缩放仍需实机复核。

负债 fixture 使用 18085/5602，在 `liabilities.mjs` 后运行 `refactor_settlement.mjs`，验证改字段后的预览失效、失败草稿、防重和重试标识。后者在 finally 关闭整个 18085 临时账本，不与其他负债脚本并行；重新运行会改变时钟的负债原始验收时应重新启动 fixture。`refactor_plan_retry.mjs` 使用新启动的负债 fixture，覆盖调整与退款核对的已提交响应丢失、后台更新后的完整请求重放，以及改值 409；也会在 finally 销毁账本，可用 `LIABILITY_FIXTURE_API` 指定对应本机 fixture 地址。`refactor_repairs.mjs` 验证记账/周期新草稿不被旧成功或旧失败关闭，以及侧栏首次/后台错误与重试。`refactor_keyboard` 模拟 visualViewport 收缩，实体手机键盘可另作人工复核。

还款日期回归使用新启动的负债 fixture，执行 `node e2e/liability_repayment_dates.mjs`，覆盖桌面修改还款日以及手机重新保存旧版本的待生效设置：已有未出账消费从次月 1 日同步到 8 日，账单标识、流水和金额不变。脚本使用当前日期并在结束时销毁临时账本，不与其他负债脚本并行。

旧 SQL/开发脚本依赖历史种子账号，部分硬编码客户端路径和 `jizhang` 开发库并直接删除数据。应先按其文档建立临时 MySQL 账本，不能仅修改 BASE 就对真实库运行。本次结果、明确跳过的旧检查、截图和回滚说明见 `openspec/changes/archive/2026-10-12-refactor-frontend-foundations/acceptance.md`。

## 目录结构

```
backend/    Go 后端（Gin + GORM + golang-migrate），含 Dockerfile 与容器入口（自动迁移）
frontend/   前端（React 19 + Vite + TypeScript + Ant Design 6 + ECharts），含 Dockerfile 与容器内 nginx 配置
docker/     部署与备份脚本（宿主 nginx 样例、备份脚本）
openspec/   变更与规格文档
```

## 本地启动步骤

### 0. 前置

- Go >= 1.26、Node >= 20、Docker（含 compose 插件）

### 1. 启动依赖（MySQL 8 + Redis 7）

```bash
docker compose up -d
```

Docker 依赖的本机端口映射为 3307/6380，配置见根目录 `.env`（复制自 `.env.example`）。

使用 Windows 本机 Redis 时，在仓库根目录运行以下 PowerShell 脚本，并将 `.env` 的 `REDIS_ADDR` 设为 `127.0.0.1:6380`。脚本默认使用 `C:\Program Files\Redis\redis-server.exe`，可通过 `-RedisExecutable` 指定其他安装路径；数据库仍使用当前配置的本机 MySQL。

```powershell
.\scripts\start-local-redis.ps1
```

脚本会在后台启动 Windows Redis，并检查连接、会话写入及磁盘快照。AOF、快照和日志保存到已忽略的 `bin/redis-native`；再次执行会复用已运行的本机实例。更新 `.env` 后需重新启动本地后端。

### 2. 执行数据库迁移

```bash
cd backend
migrate -path migrations -database "mysql://root:devpass@tcp(127.0.0.1:3307)/jizhang" up
```

### 3. 启动后端（:8080）

```bash
cd backend
go run ./cmd/server
```

迁移负责初始化账户、分类、平台和负债类型；服务首次启动仅幂等创建管理员（账号密码来自 `.env` 的 `ADMIN_USERNAME`/`ADMIN_PASSWORD`）。

### 4. 启动前端（:5600）

```bash
cd frontend
npm install
npm run dev
```

访问 `http://localhost:5600`（本机 5173 属 Windows 保留端口区间），`/api` 请求由 Vite 代理到 `:8080`。

### 5. 运行后端单测

```bash
cd backend
go test ./...
```

## 服务器从零部署（Docker 全栈）

前置：服务器已安装 Docker 与 compose 插件。站点挂在**独立子域名**（本项目用 `jizhang.zhanghongyun.cn`，复用已有通配证书）。

```bash
# 1. 配置环境（改掉 JWT_SECRET 与管理员口令）
cp .env.example .env
vi .env
#    服务器上还要设：HTTP_BIND=127.0.0.1（只监听回环，由宿主 nginx 反代）、
#    HTTP_PORT=8084、COOKIE_SECURE=true（HTTPS 下 refresh cookie 才会被浏览器接受）

# 2. 构建并拉起全栈（api 容器启动时自动执行 migrate，幂等）
docker compose --profile app up -d --build

# 3. 健康检查
curl http://127.0.0.1:8084/api/v1/health
```

浏览器打开 `https://jizhang.zhanghongyun.cn/`，用 `.env` 中的管理员账号登录并记一笔，即完成真实登录记账验证。

- `api`：Go 服务 + 启动时自动迁移（`backend/entrypoint.sh`），不映射宿主端口
- `web`：nginx 托管前端 dist 并反代 `/api/v1/`（`frontend/nginx.conf`），对外 `${HTTP_BIND}:${HTTP_PORT}`
- **宿主 nginx 必配**：子域名 server 块终止 TLS 后反代到 `127.0.0.1:8084`，样例见 `docker/nginx.conf`
- **监听面**：mysql/redis/web 默认绑 `127.0.0.1`（`DB_BIND`/`REDIS_BIND`/`HTTP_BIND`）。靠云安全组挡住是运气不是设计，纵深防御要在进程层就不听公网。
- **日志**：四个容器统一 `json-file` 50m×3，避免 zap 打到 stdout 后无限增长吃满磁盘（40G 盘必须限制）
- 换成挂子路径（如 `/jizhang/`）而非子域名：要同时改三处 —— `frontend/Dockerfile` 的 `ARG VITE_BASE`（派生静态资源前缀、router basename、`API_BASE`）、compose 的 `COOKIE_PATH`、以及两份 nginx 配置，详见 `docker/nginx.conf` 末尾变体注释

## 备份与恢复

```bash
# 手动/定时备份：产出 backups/jizhang-<时间戳>.sql.gz，保留 KEEP_DAYS 天（默认 30）
# 只本机不推送：  sh docker/backup.sh
# 本机 + 推 OSS： RCLONE_REMOTE="jizhang-oss-crypt:" sh docker/backup.sh
# 服务器 cron（已装）：0 3 * * * cd /srv/jizhang && RCLONE_REMOTE="jizhang-oss-crypt:" sh docker/backup.sh >> backups/backup.log 2>&1
# 推送状态由 /api/v1/health 暴露（push=ok|failed|skipped），失败下次打开首页可见

# 恢复演练：恢复进空库并核对行数
docker exec jizhang-mysql sh -c 'mysql -uroot -p"$MYSQL_ROOT_PASSWORD" -e "CREATE DATABASE IF NOT EXISTS jizhang_restore"'
gunzip -c backups/jizhang-<时间戳>.sql.gz | docker exec -i jizhang-mysql sh -c 'mysql -uroot -p"$MYSQL_ROOT_PASSWORD" jizhang_restore'
```

## 个人基金账户

从“我的 → 基金账户”创建账户，同一账户可添加多只基金。也可在账户管理选择基金资产并进入详情。每个登录账号管理自己的持仓；管理员不能跨账号操作，只读成员可以创建账户、录入或合法更正期初，买入、赎回、分红和撤销沿用既有记账权限。

首次录入已有持仓时填写基金代码、确认份额、平均成本价和生效日期，确认“人民币场外净值型基金”。支持股票、混合、债券、ETF 联接等按人民币单位净值计算的场外持仓；货币基金、场内市场价格及外币份额另需估值方式。上游资料不能可靠判定细分类别和真实币种，须由录入者核对适用口径。源简称与完整基金代码均保留，可填写个人显示名称。

期初不会再次扣银行卡。历史从生效日期开始，没有对应历史份额时显示“暂无可核对的日盈亏”；不会把今天的份额套用到过去。要补录期初之前的交易，先核对基准并合法更正期初，有后续交易时需先处理后续记录。023564、5028.62 份和 1.4119 成本价仅是测试向量，迁移和启动均不创建实际持仓。

买入记录实际总投入、费用、确认份额、成交净值、生效日期和实际付款日期。总投入含费用并计入加权成本；部分赎回按比例结转成本，全部赎回清零尾差。现金分红不降低剩余成本；再投资增加实际份额和成本，没有额外银行卡扣款。费用已在成本或赎回净额中体现，页面费用是说明项，不再次扣除。收益、分红和费用都单独统计，不进入生活收支、结余或预算。

赎回或现金分红未到账时关闭“资金已到账”，应收仍计入基金资产；实际到账后在事件记录中“登记到账”，生成一次受基金管理的转账。买入付款和份额生效跨日时也保留对应待结算资金。账户总资产等于各持仓市值与待结算资金合计，首页和资产曲线采用相同口径。关联流水可查看和导出，到基金详情撤销末笔有效事件，不能在普通流水直接改删。

净值按各基金实际公布日期展示。最近区间盈亏会列出两端净值日期；周末、延迟披露或接口故障时保留最近有效报价。未核对的公开分红只提示核对，相关日盈亏保持待核对。后台按代码共享同步，不需要打开页面；基金、账户、首页及资产曲线在页面可见时约一分钟检查一次，聚焦窗口也会刷新。

### 行情配置与迁移

基金行情由服务端使用 `FUYAO_API_KEY` 获取。自动检查采用北京时间：周一至周五 19:00 起每半小时检查，23:00 最后一次；每只基金获得更新的最新报价并完成估值后，当晚停止该代码的检查，其他基金继续等待。没有更新也在 23:00 结束。次日 04:00 独立补查前一工作日一次，周五对应周六凌晨；无新数据或失败也不追加凌晨重试。完成/尝试状态持久化，多 worker 和重启不重复调用，04:00–04:29 重启可恢复尚未尝试的凌晨补查。

工作日按周一至周五处理；节假日没有新净值时按上述截止时间结束。启动在对应时段恢复检查，白天或周末夜间不自动拉取；首次持仓缺缓存时的冷取价和用户手动刷新仍保留原限频规则。旧 `FUND_SYNC_MINUTES` 配置已移除，不再影响调度。Key 缺失不会阻止普通记账启动，已有有效净值仍可展示。容器通过 Compose 的 API environment 显式读取配置。

Windows 用户环境变量在配置前已启动的终端或应用进程中可能不可见。在启动 API 或 Docker Compose 的 PowerShell 终端读取已配置的用户变量，然后启动服务；此命令不输出密钥：

```powershell
$env:FUYAO_API_KEY = [Environment]::GetEnvironmentVariable('FUYAO_API_KEY', 'User')
```

也可将 Key 保存在已忽略的 `.env` 中。不要将密钥写进源码、浏览器环境变量、日志或测试产物。

迁移为 `000014_personal_fund_accounts`，旧资产回填为现金子类，原余额与负债保留。生产升级前备份财务数据并暂停写入。down 在存在任何个人基金账户、持仓、事件或关联资金时明确拒绝，不自动删除财务数据；仅公共报价缓存且没有个人基金资源时可安全回滚。本变更的开发测试不迁移真实开发库或生产库。

### 隔离基金验收

在 backend 设置 `FUND_MYSQL_DSN` 后运行 `go test ./internal/service -run '^TestFundMySQL' -count=1 -v`。DSN 须有创建和删除临时库权限，测试忽略指定库名，使用随机数据库并在结束后清理。基金、券、负债和个人目录的原生集成分别支持 `FUND_MYSQL_DSN`、`VOUCHER_MYSQL_DSN`、`LIABILITY_MYSQL_DSN`、`PERSONAL_CATALOG_MYSQL_DSN`。

浏览器验收使用独立内存账本、假行情和 18086/5603 端口，避免干扰既有 fixture：

```powershell
# 终端一：backend
$env:FUND_BROWSER_SERVER='1'
$env:BROWSER_FIXTURE_TTL='2h'
go test ./internal/router -run '^TestFundBrowserServer$' -count=1 -v -timeout=2h

# 终端二：frontend
npm run build
node e2e/support/preview.mjs --dist=dist --port=5603 --api=http://127.0.0.1:18086

# 终端三：frontend；每轮重新启动终端一以恢复初始行情
node e2e/funds.mjs
```

脚本校验 localhost 和 `fund-browser` 标识后才写数据；桌面和 360px 截图保存在 `frontend/e2e/shots/refactor-frontend/funds-*.png`。真实接口 smoke 只读取公开资料，不建持仓：配置运行进程 Key 与 `FUYAO_LIVE_SMOKE=1` 后执行 `go test ./internal/fuyao -run '^TestFundLiveSmoke$' -count=1 -v`。


## 建行个人黄金积存

从“我的 → 建行积存金”或账户管理建立本人的黄金账户，可创建多个账户。已有持仓录入克数和总成本/平均每克成本，不补扣现金；买入和卖出均从独立黄金账户详情操作，“记一笔”弹窗仅保留普通记账及购券。填写银行实际成交克数、含费用总扣款或实际净到账，选择本人现金资产账户，同日更新持仓和资金；首版不处理跨日待结算。

市值按建行客户赎回价计算，持仓、已实现与累计盈亏分别展示，买卖和费用不进入生活收支与预算。后台每小时共享查询并持久化缓存，买卖表单可手动刷新，30秒内重复请求合并或冷却；显示银行报价时间与成功查询时间。失败保留旧价，连续两小时未成功提示异常；无有效报价也可保存真实记录，市值待报价、总资产提示不完整。历史缺报价观察不使用今天价格倒算。

迁移为 `000017_ccb_gold_accounts`。前后端须同步支持黄金空余额，存在个人黄金财务历史时 down 明确拒绝；升级前备份财务库。详见 [建行积存金接入与迁移](docs/ccb-gold-migration.md)。本变更只在随机 MySQL 数据库和内存浏览器账本验收，不迁移真实开发或生产财务库。

## 银行理财账户

已接入民生理财贵竹固收增利天天盈9号 **E 份额**（`FBAE68020E`，登记编码 `Z7007923000092`）。新增迁移为 `000020_cmbcwm_wealth_product`，按本次明确新增授权开放公共目录，仅增加产品元数据，不创建个人资产或默认净值。升级迁移后可在银行理财账户的“录入已有持仓”中选择 E 份额，并填写实际总成本、确认份额和持仓生效日。

民生由 Go 后端匿名 HTTP 直接读取官网，不需要银行登录、API Key、浏览器或 `WEALTH_COLLECTOR_*` 配置；苏银仍使用原采集器。两来源按具体份额分派、独立检查配置。沿用北京时间每天 **08:00、12:00、18:00、22:00** 的同步任务、启动恢复与手动刷新，完整分页读取历史，按实际净值日期幂等入库并对历史修订留痕，重算相关持仓收益及账户/首页/曲线缓存。已有持仓即可无人在线同步；没有新净值不伪造当日收益，失败保留最近有效值。

民生支持真实期初、已确认申赎及通用待确认申购；周利宝甲乙丙转出分配仅用于 `J07241`，不套用到民生，实际赎回仍从“赎回”记录。详情与接口样例见 [民生净值接口调查](docs/research/cmbcwm-wealth-api.md)。`000020` down 在 E 份额存在个人持仓或补值引用时拒绝，保留历史并使用前向修复；仅公共行情缓存时可回滚。官网页面接口没有长期可用性承诺，本次代码接入和隔离验收不等同于生产发布或多日采样通过。

“录入已有持仓”和“更正期初”填写 **实际总成本＋确认份额**，单位成本自动显示八位小数，实际总成本直接保存、不用舍入后的单位成本乘回计算。例如 1.16 元 / 1.14 份，单位成本显示 1.01754386，总成本精确保留 1.16 元。单位净值另有可填写字段，默认带入所选持仓生效日的同日有效值，缺价留空；手工修改后刷新不覆盖，切换日期会重新带入。填写的净值保存为建账参考记录，不改变成本或行情序列；实际估值仍遵循自动同日优先、本人缺价补录规则。已有数据不自动重写，旧单位成本 API 继续兼容，无数据库结构迁移。

2026-10-16 生产 J07241 已按用户明确要求提前开放选择，见 [授权及执行记录](openspec/changes/archive/2026-10-20-add-bank-wealth-accounts/admission-2026-10-16.md)。生产自动净值同步随后已开启，真实取数入库 265 条，详见 [启用及验证记录](openspec/changes/archive/2026-10-20-add-bank-wealth-accounts/production-sync-2026-10-16.md)。三天真实采样已于 2026-10-20 复核通过（12/12 正式槽、14/14 公开响应回放），见 [最终验收记录](openspec/changes/archive/2026-10-20-add-bank-wealth-accounts/qualification-final-2026-10-20.md)；验收观察已暂停，生产自动净值同步继续运行，缺失日期仍可按权限补录。

从“我的 → 银行理财”或账户管理创建本人账户，一个账户可管理多款产品。只允许下拉选择已验证或有明确产品级授权记录的已接入产品：苏银理财恒源日申周赎1号A（J07241，登记编码 Z7003125000238）及民生天天盈9号E（FBAE68020E，登记编码 Z7007923000092），均为人民币单位净值。不接受任意代码、其他份额或货币型万份收益产品。原 `000018` 只种待验证苏银元数据，`000020` 按本次授权增加民生 E 份额；均不创建个人账户、份额或默认价格。

期初登记实际确认份额、单位成本和生效日期，不补扣现金，零成本须明确填写 0。已接入产品缺净值时仍可保存真实持仓和交易。申购必须已有银行确认份额，填写含费用总扣款、实际成交净值、实际付款日和份额确认日，付款日不得晚于确认日，均不能未来；付款至确认之间保留已投入本金为待结算资产，确认后按含费投入计算加权成本。

赎回按实际毛额减费用形成净应收，部分赎回按份额比例结转成本，清仓清除成本尾差。未知到账日留空，实际收到后通过原事件“登记到账”，只转入本人现金账户一次。现金分红形成收益和应收，不冲减持仓成本；红利再投资按实际金额、确认份额增加成本及份额，不扣银行卡。不根据公共净值自动生成个人分红。

自动净值按产品共享，人工净值按“本人＋产品＋净值日期”保存，本人多个账户共用，其他账号不受影响。补值必须提供真实日期、正数单位净值和来源；允许带版本更正或撤回，仅补缺失日期，同日自动有效值存在后禁止覆盖，原补录保留只读审计。自动历史修订、人工补值和来源切换可重估已封账市场价值，不能修改资金、份额、成本、已实现交易收益或封账收支。所有角色可维护本人账户、合法期初和补值；申赎、分红、到账、撤销执行既有资金权限，管理员也不能越权。

市值使用截至日最近有效单位净值，累计净值仅作参考。账户资产为持仓市值加申购待结算和赎回/分红应收，不再叠加成本或转账本金。任一非零持仓缺价时完整余额为 `null`、总资产显示不完整，另列已知小计；清仓但有应收时应收仍计资产。投资本金、费用、分红及收益均不进入生活收支和预算。理财操作只在独立页面进行，“记一笔”不含理财或黄金买卖。

### 已付款待确认与周利宝转出

已付款、还没有确认份额时，选择“登记待确认申购”，填写实付金额、本人付款账户/日期、银行申请时间和申请所属交易日；预计确认日可空或填未来，仅作提醒。现金只扣一次，等额在途资金继续计入资产。银行确认后在持仓详情选择“确认申购份额”，自动带入**申请交易日**的精确同日净值；缺少该日值时留空，可刷新历史或按银行确认值填写。建议份额按金额/净值四舍五入两位，实际值可修改并留说明，不改写公共净值，确认不再扣款。

取消或银行确认失败后，未退金额继续为退款应收；每次实际到账再登记退款，支持分次，拒绝重复或超额退款。未确认本金不计算净值收益。“撤销末笔”用于纠正记账，不代表银行实际退款。

周利宝 20250402 规则及已确认口径见 [订单补充](openspec/changes/archive/2026-10-20-add-bank-wealth-accounts/pending-orders.md)。转出表单展示丙（当日待确认，仅可撤销）、乙（上一交易日未确认）和甲（已确认可用份额），先乙后甲，乙按银行申请时间后进先出。申请金额不含丙；乙确认后按每笔分配比例计算份额，失败时抵扣部分作为本金转出、余款可退。余额不足可选取消或按全部可转出余额登记。申请不是到账，待银行确认真实份额和毛额后再登记到账，实际净额可以不同于申请金额。

交易日、上一交易日及转出开放日由本人按银行记录选择，不按自然日推断截止时刻/节假日。最终以银行确认值为准，系统不向银行下单或自动确认个人交易。订单元数据迁移为 `000019_wealth_pending_orders`，有订单历史时 down 拒绝删除，采用保留历史的前向修复。新订单浏览器验收为 `node e2e/wealth-orders.mjs`，支持 `WEALTH_E2E_MOBILE=1`，每轮使用全新隔离 fixture。

### 采集运行与准入

苏银采集器为独立 Node 22+/Playwright Chromium 服务，固定苏银产品页和净值请求白名单，使用全新匿名上下文运行官网脚本；不需要银行个人登录、不复制桌面 Cookie。Go API 不内嵌浏览器。配置见 `.env.example` 的 `WEALTH_COLLECTOR_URL`、`WEALTH_COLLECTOR_KEY`，内部密钥至少 24 字符，不能放入前端或日志。苏银无采集配置时保留已存有效数据，民生独立 HTTP 源仍可运行；不能把人工补值当作产品自动准入证明。

Linux 容器配置为 `docker-compose.wealth.yml`，采用非 root、Chromium 沙箱、只读根目录、临时目录和资源限制，没有宿主端口。必须同时给 API 传入同一内部密钥，并让 API 和采集器在同一 Compose 网络。发布时先在私有环境设置密钥，再按既有部署流程合并该 overlay；只启动采集器的命令如下（不启动或更新应用）：

```bash
docker compose -f docker-compose.yml -f docker-compose.wealth.yml --profile wealth config --quiet
docker compose -f docker-compose.yml -f docker-compose.wealth.yml --profile wealth up -d --build wealth-collector
```

生产已在服务器私有 `.env` 设置 `COMPOSE_FILE=docker-compose.yml:docker-compose.wealth.yml`，后续默认 deploy 持续加载 overlay。仅默认本地开发栈不会启动采集器。overlay 中采集器支持 app/wealth profile，API 等待采集器健康。

进程健康检查仅代表采集服务可响应，不能代替上游取数验收。默认首次准入需在部署等价环境连续三个自然日的 12 个北京时间 08/12/18/22 正常检查槽全部成功，覆盖工作日、冷启动、重启、新匿名会话、历史及新披露/修订。已完成的真实采样及保留失败记录见 [J07241 准入验收](openspec/changes/archive/2026-10-20-add-bank-wealth-accounts/qualification.md)；J07241 已有单独留痕的用户提前开放授权，不能据此推断采样通过。应用发布与产品准入分开管理，部署状态见 [实施与验收记录](openspec/changes/archive/2026-10-20-add-bank-wealth-accounts/implementation-status.md)。

正式后台仅检查需估值产品，按上述四槽运行，启动时合并漏跑槽，持久化产品租约及结果。手动刷新与在途查询合并，单产品尝试至少间隔 60 秒；失败等下一正常槽或冷却后的明确手动操作，采集器最多一次匿名重建。普通页面只读缓存，每分钟或聚焦刷新；显示真实净值日期、来源、查询及重建状态，没有新披露也是成功检查。

准入更新仅有运维命令，不提供用户写目录的 API。全部采样和功能验收复核后，下载当前固定版本的 `state.json`，在已配置目标数据库的 backend 环境执行；命令校验 12 槽、时间、预检和披露证据并保存文件摘要，不自动发布前端或执行迁移：

```bash
go run ./cmd/wealth-admit -code J07241 -status enabled -evidence /private/qualification/state.json
# 停止新建持仓/申购，保留既有持仓、赎回、分红、到账及补值
go run ./cmd/wealth-admit -code J07241 -status retired
```

### 理财迁移与隔离验收

迁移为 `000018_bank_wealth_accounts`。升级前备份财务库并按既有迁移流程执行；前后端须同步识别 `wealth` 与空余额。存在任何个人理财账户、持仓、事件、mutation、人工补值或审计时 down 在删除前明确拒绝。回退优先关闭新增准入、保留新结构和只读历史；已有非零或未知理财资产时不能直接换回不识别 wealth 的旧客户端。无个人理财引用的独立空库可验证 down/up；不能以删除财务历史换取回滚。

本地先启动本机 MySQL、Redis。私有环境设置 `WEALTH_MYSQL_DSN`（具备创建/删除临时库权限）后，在 backend 执行：

```powershell
go test ./internal/service -run '^TestWealthMySQL' -count=1 -v
go test ./...
go vet ./...
go build ./cmd/server
```

MySQL 测试忽略 DSN 中的库名，创建并清理随机独立数据库；浏览器测试使用内存账本、临时 Redis 和模拟行情，不写真实资产，也不能算三天真实采样证据：

```powershell
# 终端一：backend
$env:WEALTH_BROWSER_SERVER='1'
$env:BROWSER_FIXTURE_TTL='2h'
go test ./internal/router -run '^TestWealthBrowserServer$' -count=1 -v -timeout=2h

# 终端二：frontend
npm run build
node e2e/support/preview.mjs --dist=dist --port=5605 --api=http://127.0.0.1:18088

# 终端三：frontend，每轮都重启终端一以恢复初始行情
node e2e/wealth.mjs
# 再重启终端一后，执行完整 360px 手机流程
$env:WEALTH_E2E_MOBILE='1'
node e2e/wealth.mjs
Remove-Item Env:WEALTH_E2E_MOBILE
```

脚本先校验 localhost 与 `wealth-browser` 身份再写临时数据，覆盖准入、期初、跨日申赎/到账、分红/再投、补值、自动替代、未知余额、撤销、权限及换号。截图位于 `frontend/e2e/shots/refactor-frontend/wealth-*.png`。采集器的独立回归在 `collector` 运行 `npm test`；真实浏览器路由测试需设置 `WEALTH_BROWSER_EXE` 为本机 Chrome 路径。

### 配置化产品和理财零钱

产品身份统一维护在 `backend/internal/wealthdata/products.json`，通过 `go generate ./internal/wealthdata` 同步给采集器。同银行新增份额复用适配器和定时同步；公共目录迁移可用 `go run ./cmd/wealth-catalog -code 份额代码 -sql` 生成待审核的 INSERT。新银行仅需增加来源适配器和一次路由注册，流程见 [产品接入说明](docs/wealth-products.md)。

迁移 `000021_wealth_cash_accounts` 增加可支付的“理财零钱”类型。新建账户时可直接选理财零钱，输入起始余额、产品（如天天盈9号 E）和基准日，一次保存账户及配置；记账窗口内也可直接创建，无需再新增迁移。现有余利宝仍可通过关联入口原地启用，保留原账户、期初及流水。普通收支实时改变同一余额；已披露净值区间收益由现有任务自动入流水，支持手动修改和余额校准，不重复计算总资产，也不混入生活收支和预算。支出默认快转，当天停止对应收益；转入确认后才估算收益，缺少确认日期时的 T+1 仅为可修改估算。

封账期不自动改资金流水，分红异常显示待核对；无新披露不伪造自然日收益。停用联动保留收益和审计。000021 有个人历史时拒绝破坏性 down；详细规则、估算边界、迁移和隔离验收见 [理财零钱说明](docs/wealth-products.md)。系统不会按“余利宝”名称自动修改真实账户。

# 记账（jizhang）

自用家庭记账站点：Web + 移动 H5 一套代码，Go + MySQL + Redis 后端，React + Ant Design 前端。

## 个人分类与资金账户

全部有效登录角色（含只读成员）都可在“我的 → 分类管理 / 账户管理”维护自己的选项。分类、资产和负债按登录账号独立，同名也具有不同标识；管理员同样不能管理他人的个人目录。只读成员的目录管理能力不授予记账、购券、还款或分期权限，账期、预算配置和用户管理仍限管理员。

记账、转账、购券、周期模板及负债操作中的可编辑账户字段提供“新建账户”：在选项内填写账户资料，成功后选中真实账户 ID，保留父表单草稿。取消或搜索不创建；创建成功后放弃父表单会保留账户。手机打开先显示选项，主动输入才唤起键盘；失败可用同一请求标识重试。名称最多 64 个 Unicode 字符，账户不会按同名合并资金。分类仍在管理页维护，筛选器与锁定字段仅选择已有资源。平台 / 商家继续全员共享。

迁移 `000012_personal_catalogs` 保留原账户和资金，将旧共享资产按资金链整体归属，为各账号建立独立分类与零余额默认资产。默认模板在数据库中维护，个人初始化完成后不会因重启、重新登录或刷新恢复已删除记录。历史跨账号资金事件保留本人影响并以只读说明展示；失配周期模板和后续费用来源需要本人修复。统计、券和提醒按个人范围读取，预算继续使用账本总预算上限并展示“本人进度”，分类预算不按同名跨账号复用。

升级须先备份并暂停写入。归属、对账 SQL 与有后续写入时拒绝自动 down 的说明见 [个人目录迁移说明](docs/personal-catalog-migration.md)。验证结果与截图见 [本次验收记录](openspec/changes/archive/2026-10-13-personalize-categories-and-accounts/acceptance.md)。独立 MySQL 测试可设置 `PERSONAL_CATALOG_MYSQL_DSN`（也支持既有两种 DSN）运行 `go test ./internal/service -run '^TestPersonalCatalogMySQL' -count=1 -v`；浏览器 fixture 启动后运行 `node e2e/personal_catalogs.mjs`、`node e2e/mobile_selections.mjs` 和 `node e2e/mobile_selection_lifecycle.mjs`。

## 消费信用账户

“我的 → 负债账户”管理登录账号自己的信用卡、花呗、月付等账户。每人可创建多个同类型账户，在各账户详情设置独立账单日、还款日及当月/次月规则；仅本人可查看和管理，管理员也不能跨账号操作。日期支持1–31，短月份取月底。新负债从零余额启用，配置完成后才能记消费。

`000009_default_credit_accounts_and_platforms` 自动补齐花呗、京东白条、抖音月付、美团月付，并预置支付宝、微信、淘宝、天猫、京东、拼多多、抖音、美团、饿了么的平台 / 商家选项。同名已有账户与选项保持原身份和设置；新负债默认零余额、待配置账单日期。此数据迁移的 down 保留账户和选项，避免删除已使用或修改的记录。

默认账户、分类模板及共享平台目录由数据库迁移初始化，个人实例只初始化一次，服务启动不会补建已删除的个人记录。`000010_database_catalogs` 增加 `liability_account_types` 类型目录并初始化空库分类；后端校验与前端类型名称、选项统一读取该表。新增消费信用类型或修改显示名称时更新数据库即可，应用中不再维护相同的名称清单。

`000011_personal_liability_accounts` 增加个人归属。现有负债保留原账户和流水，有单一历史记账人的账户归该人，其余原共享账户归初始管理员。每个账号首次访问时按数据库的默认类型模板创建个人账户并保存初始化标记；后续可另建同平台账户，删除的默认账户不会在重启时重建。各登录账号可管理自己的零余额账户资料，只读角色继续禁止还款等手工资金操作。资产账户的共享与管理员管理权限已由 000012 迁移替换，平台目录仍沿用共享机制。

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

分期新消费的平台 / 商家复用记账的共享选择器。期次预览以一列显示“本金 + 手续费”合计，下方小字显示手续费；金额按元录入，默认在实际收取日计入当前负债账单，随本金一起还款。选择资产直接支付时，该费用不会加入负债应还金额。

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

“平台 / 商家”支持下拉选择和输入搜索。没有所需名称时，可在原输入框中确认“创建「名称」”，或直接输入并随券保存；新选项在整个账本内共享。点击创建后即使取消购券也会保留选项，仅输入或失焦不会创建。该字段可清空，名称去除首尾空白后最多 128 字，同名复用。

普通“记一笔”和“编辑流水”也提供可选的平台 / 商家字段，与购券共用选项目录。平台 / 商家独立保存，在流水列表和 CSV 中展示，也可作为搜索关键词；新增数据库迁移为 `000007_transaction_platform`。

平台 / 商家选项的读取和创建只要求登录，四个角色均可使用，包括只读成员。创建选项不产生券或账务记录；购券、编辑券及流水操作仍按原权限执行。

小于 768px 的手机视口中，选项下拉统一使用底部选择面板：打开先展示已有选项，点击面板内搜索框才进入文字输入。账户、分类、账单和关联流水可搜索；角色、状态、月份等短枚举直接点选。平台 / 商家可创建共享名称，业务账户可就地创建个人账户；筛选器、分类和固定枚举只选择已有项。平台目录读取或独立创建失败时，可明确选择“使用此名称”回填草稿，再随父表单保存；搜索和取消不会改变原值。桌面下拉与日期选择器沿用原交互。

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
node e2e/voucher_platforms.mjs
node e2e/transaction_platforms.mjs
```

脚本默认使用本机 Playwright 的 Firefox，可用 `FIREFOX_EXE` 指定已安装的浏览器文件。它先校验测试服务标识，只在本地测试账本里写入，覆盖桌面/手机购券、核销、退款、作废、撤销、自动退款暂停、CSV 和只读权限。重试脚本还模拟“已保存但响应超时”，验证修改金额后重试不会重复扣款；同一次表单提交沿用原请求标识，需刷新核对后才重新发起操作。截图与下载产物放在已忽略的 `frontend/e2e/shots`、`frontend/e2e/downloads`。

平台 / 商家脚本通过普通成员表单验证选择、键盘创建、取消保留、直接保存、编辑、清空及手机弹层；同时验证四个角色目录接口、未登录拒绝、读取失败后提交、创建响应丢失后重试和迟到响应保护。MySQL 集成测试覆盖 000006 历史回填、精确名称去重、并发创建与财务事务回滚。

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

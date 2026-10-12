# 实施验收记录

## 已实现行为

- 全部有效登录角色管理本人分类、资产与负债；管理员不跨账号读取或管理这些资源，平台 / 商家保持共享。
- “我的”提供分类和账户管理；记账、转账两端、购券、周期模板、还款、分期 / 结清费用字段支持明确新建账户，筛选、分类和锁定字段只选已有项。
- 创建使用真实数值 ID 和账号内请求幂等，失败保留草稿，已保存但响应丢失的重试返回原账户；创建后放弃父表单保留账户。
- 私人目录、流水 / CSV、券、统计、预算本人进度及全部类型提醒执行同一归属范围。旧跨账号资金链保留本人影响并以只读历史说明呈现，不扩大写权限。
- `000012_personal_catalogs` 保留原资金和既有负债归属，复制独立分类、创建零余额默认资产；一次初始化不恢复已删除实例。旧失配模板暂停、费用来源阻塞后可本人修复，严格 down 拒绝删除后续业务修改。

## 自动检查

| 检查 | 结果与范围 |
|---|---|
| `go test ./... -count=1`（设置全部三种 MySQL DSN） | 通过；使用随机独立数据库，含旧券 / 负债集成测试、新迁移、资金守恒、跨账号拒绝、分类映射、幂等、初始化和回滚保护 |
| `go vet ./...`、`go build ./cmd/server` | 通过 |
| `npm run test:unit` | 20 个测试文件、61 个断言通过 |
| `npm run typecheck`、`npm run build` | 通过；构建仍提示既有大于 500 kB 分包 |
| `openspec validate personalize-categories-and-accounts --strict` | 通过 |
| `node e2e/personal_catalogs.mjs` | 通过：四档角色 CRUD / 越权 / 未登录 / 共享商家，只读成员管理页面；1280 / 360 / 390 / 767 / 768px 的创建、取消、数值 ID、父草稿、输入焦点和断点；模拟软键盘、失败 / 响应丢失重试、提交期间取消及迟到响应 |
| `node e2e/mobile_selections.mjs` | 通过：账户 / 分类选择、转账排除、平台创建、嵌套券表单、枚举零值、周期记账、角色更新、六档断点及明暗截图 |
| `node e2e/mobile_selection_lifecycle.mjs` | 通过：读取失败重试、跨断点关闭、父层结束清理、真实明暗主题状态和桌面 / 手机截图 |
| `node e2e/liabilities.mjs`（新启动的独立负债 fixture） | 54 项检查通过：账户内联创建的资产来源限制、数据库类型目录、四档角色、分期 / 手续费、还款、退款核对、结清 / 撤销、提醒与手机界面，未出现浏览器运行错误 |

MySQL 的 16 个同账号同标识并发建户请求只生成一份账户；同标识不同内容返回冲突，删除后旧标识不能重新建户。并发初始化、注入分类初始化失败后重试及删除后再次读取均通过。`TestPersonalFinancialScope`、`TestPersonalFeeSourceRepair` 和 `TestPersonalRecurringRepair` 校验查询隔离、提醒已读 / 去重、旧费用来源修复、暂停模板的本人修复和后台生成。

归属与余额样例见 [迁移说明](../../../../docs/personal-catalog-migration.md)，只读对账脚本为 `scripts/catalog-migration-report.sql`。全部验收仅写入已确认身份的临时浏览器账本及随机 MySQL 测试库。

## 截图

- [桌面分类管理](../../../../frontend/e2e/shots/refactor-frontend/personal-categories-1280.png)
- [手机分类管理](../../../../frontend/e2e/shots/refactor-frontend/personal-categories-360.png)
- [桌面账户内联创建](../../../../frontend/e2e/shots/refactor-frontend/personal-account-create-1280.png)
- [手机账户内联创建](../../../../frontend/e2e/shots/refactor-frontend/personal-account-create-390.png)
- [模拟键盘下保存可达](../../../../frontend/e2e/shots/refactor-frontend/personal-account-keyboard-390.png)

## 验证限制

键盘验收使用受控 `visualViewport` 收缩验证可见范围、滚动和按钮位置，未连接实体 iOS / Android 设备。实体设备输入法与真实软键盘仍需实机复核。截图位于仓库忽略的浏览器产物目录。

## 归档复核（2026-10-13）

- `openspec list --json` 确认全部 28 项任务完成；proposal、design、specs、tasks 四项规划产物均为 done。
- 已同步 accounts、budgets、categories、identity/access-control、notifications、recurring-entries、statistics、transactions、ui-shell、vouchers 十项主规范，共新增 13 条要求、修改 16 条要求；逐项比较确认增量全部落入主规范，原有 Purpose 和未涉及的要求、场景保留。
- `openspec validate personalize-categories-and-accounts --strict --no-interactive` 通过；`openspec validate --specs --no-interactive` 为 18/18 通过；`git diff --check` 通过。
- 额外运行的 `openspec validate --specs --strict --no-interactive` 因 12 条原有 Purpose 少于 50 字符的警告未通过。已与 HEAD 对比，所有 18 项主规范的 Purpose 均未修改，无新增规范结构或要求错误。
- 本次归档只同步规范和整理文档，沿用上列已完成的实现及资金、权限、MySQL、浏览器验收结果。
- 归档后 `openspec validate --archived --no-interactive` 为 11/11 通过，当前活跃变更列表为空；README 归档入口及本记录的六个迁移 / 截图链接均有效，`.openspec.yaml` 随目录完整保留。

## 生产部署（2026-10-13）

- 发布站点：`https://jizhang.zhanghongyun.cn/`。生产原为 schema 11，三个登录账号、16 个账户、44 个分类和四条流水；四个项目容器健康。
- 完整生产备份先在独立恢复库验证 `000012` up、18 项资金 / 归属检查及 down，全部通过；恢复库已删除，备份保留。
- 正式切换时暂停 API 和后台任务，备份文件为 `/srv/jizhang/backups/catalog-deploy-20261013-000012/pre-migration-20261013-041416.sql.gz`，校验 gzip 完整性及 SHA-256 通过。备份目录权限为 700，数据文件通过 umask 077 创建。回滚限制仍按迁移说明执行，发生业务写入后不得直接 down。
- 正式迁移到 schema 12 且 dirty=0；迁移完成时及生产冒烟后，两次 18 项检查均为零差异，原账户逐户余额、期初余额、原负债归属、流水金额 / 日期 / 账期及账单 / 抵扣记录保持。旧 `stats:*` 缓存已清理。
- 部署器同步 150 个差异文件，涉及后端服务、前端表单与导航、迁移、验收脚本和 OpenSpec 规范 / 归档；另同步迁移说明和只读对账脚本。重建 api、web，MySQL 和 Redis 保持运行。
- `verify.sh` 通过：清单内 526 个文件一致、0 不同、0 缺失；四个容器 running/healthy；首页、健康接口及 SVG / ICO 图标均为 200。部署器输出 `DEPLOY: ALL_GREEN`。
- 生产浏览器冒烟 17/17 通过，清理本次唯一标记的一条测试流水；补充生产只读 API 检查 12/12 通过，账户 / 分类目录与本人数据库资源精确匹配，管理员读取他人负债 / 账单为 403，匿名目录请求为 401，共享平台 / 商家和消费信用类型仍可读取。

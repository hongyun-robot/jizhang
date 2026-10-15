# 通用目录排序验收

日期：2026-10-14。实施基于当前工作区的个人分类、账户与商家目录，保留原有未提交改动。本轮完成本地实施与验证，没有部署或迁移真实开发 / 生产财务库。

## 实现

- 分类大类按收支类型独立排序，点击大类在弹窗管理和排序同父小项，保留卡片预览及新增、重命名、删除。
- 账户手机列表和桌面表格共用完整本人目录顺序，现金、基金、负债和停用项共同排序；账户编辑和负债账户设置不再提交旧位置。
- 商家支持持久化排序，搜索时禁排并明确提示清空；新建追加，改名和同名复用保序，业务候选不再按名称重排。
- 三处复用拖动容器、手柄、条目绑定、保存控制与请求协议。鼠标、触控、键盘均支持；有位置变化才自动保存，失败恢复，目录维护期间防止并发提交。
- API 一次事务校验完整基准、归属和分类范围，批量更新位置；成功请求结果与位置一起保存，响应丢失后可按原 key 重试，后续不同顺序使旧 key 返回冲突。
- 分类和账户省略 sort 时保序 / 默认追加，显式 0 的旧接口仍合法；账户新建摘要兼容既有重试记录。

## 验证结果

| 检查 | 结果 |
| --- | --- |
| `go test ./... -count=1` | 通过，启用本机 MySQL 的券、负债、基金、个人目录 DSN；各测试使用随机隔离库 |
| `go test ./internal/service -run '^TestCatalogOrdering$\|^TestCatalogOrderingPreservesFundHistory$\|^TestAccountOrderingCreateReplay$' -count=1` | 通过，额外验证管理员越权、他人父级、无效标识、封账保留及基金持仓 / 估值不变 |
| `go vet ./...` / `go build ./cmd/server` | 通过 |
| `npm run typecheck` / `npm run build` | 通过；构建仍有已有大块资源提示 |
| `npm run test:unit` | 24 个文件、77 个测试通过 |
| `node e2e/catalog_ordering.mjs` | 通过，1280 / 390 / 360 / 767px，鼠标 / 真正触控事件 / 键盘，16 张页面与弹窗截图 |
| `node e2e/catalog_ordering_lifecycle.mjs` | 通过，账户及负债设置保序、空大类新增、切换父级后的迟到响应、pending 期间后台读取与失败回滚 |
| `node e2e/personal_catalogs.mjs` | 通过，四角色 CRUD、归属、创建重试、资金门禁、内联创建与草稿 |
| `node e2e/platform_management.mjs` | 通过，五档宽度、明暗主题、长名称、失败恢复、引用删除限制与换号迟到响应 |
| `openspec validate add-shared-drag-sorting --strict --no-interactive` | 通过 |
| 相关 diff 空白检查 | 通过 |

浏览器使用支持基金的内存账本 fixture `127.0.0.1:18186`、临时 Redis 和本次构建预览 `127.0.0.1:5616`。本机 MySQL / Redis 在开发时保持运行。后台读取测试只在浏览器拦截只读响应修改展示余额，不向账本写入虚构金额；排序失败后仍保留新读取对象，刷新后恢复服务端真实数值。

MySQL 验证覆盖空库和已有个人商家目录 up / down、Unicode / 大小写名称初次顺序、两个设备同时排序、创建 / 删除竞争、已提交响应重试、顺序改回基准后的旧请求重放，以及触发器导致中途写失败的全部回滚。真实基金持仓验收确认银行现金、份额、市值和盈亏不因排序改变。

## 截图

产物目录：`frontend/e2e/shots/refactor-frontend/`（已忽略的本地验收产物）。

- 桌面账户：[catalog-order-accounts-1280.png](../../../../frontend/e2e/shots/refactor-frontend/catalog-order-accounts-1280.png)
- 手机分类弹窗：[catalog-order-children-390.png](../../../../frontend/e2e/shots/refactor-frontend/catalog-order-children-390.png)
- 手机分类页：[catalog-order-categories-360.png](../../../../frontend/e2e/shots/refactor-frontend/catalog-order-categories-360.png)
- 手机商家：[catalog-order-voucher-platforms-767.png](../../../../frontend/e2e/shots/refactor-frontend/catalog-order-voucher-platforms-767.png)

## 实施调整与迁移

原设计仅比较 previous_ids，MySQL 并发测试发现无法识别“改回原基准后再重放旧请求”。已同步规划，新增保存请求标识与 `catalog_order_requests` 成功摘要，保留既定冲突和重试语义。

采用 dnd-kit 当前 React / DOM / collision 0.5.0 并固定 lockfile。不同高度的分类卡片排除自身碰撞；子分类列表将自动滚动限定在列表内，拖动时禁止 Escape 关闭所属弹窗，取消后正常清理传感器。子分类管理复用页面内同一列表组件，未另造重复维护逻辑。

`000016_catalog_ordering` 需在 `000015` 之后执行；上线前备份并暂停写入 / worker，数据库和新前后端配套发布。down 保留归属、名称、ID 和财务数据，但会丢失商家自定义顺序及排序重试记录；分类 / 账户原 sort 保留。详细步骤见 [目录排序迁移说明](../../../../docs/catalog-ordering-migration.md)。

未连接实体手机；手机覆盖通过 Chromium 原生触控事件与移动视口完成，既有个人目录回归还覆盖模拟软键盘布局。实体软键盘体验可在上线前另外复核。

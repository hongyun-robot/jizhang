# 流水展示排序

首页为流水增加完整归属日期（YYYY-MM-DD）。首页前 10 笔、流水页手机列表及桌面表格共用个人展示顺序，拖动左侧手柄后自动保存；也支持空格拾起、方向键移动、空格放下和 Escape 取消。金额、日期、资金和账期不参与修改，所有有效角色均可调整本人可见流水。

列表和 CSV 使用 `order=custom`。没有保存顺序时保持日期及 ID 倒序。分页或筛选后排序，只在完整个人序列中替换当前所提交条目的位置；隐藏条目保留位置。新流水位于已保存序列之前，删除流水不会打乱剩余条目。首页可能因此显示较早的手动置顶记录。

`PUT /api/v1/transactions/reorder` 接收 `previous_ids`、`ordered_ids` 和 `request_key`。服务端在同一事务中检查本人可见范围、基准相对顺序与重试标识，原子保存个人位置。失败会恢复确认顺序，并提供重试保存、刷新列表；重试复用原标识。

迁移 `000023_transaction_display_order` 仅新增个人展示位置表，外键随账号或流水删除清理对应位置。升级前停止后端和后台任务、备份本地财务库，再执行迁移并重启服务。回滚仅删除展示位置和该作用域的排序请求记录，恢复默认日期顺序，不删除或修改财务历史；须配套回退应用，避免新应用继续读取已删除的位置表。

验证命令：

```powershell
cd backend
go test ./internal/service -run '^TestTransactionOrder' -count=1
```

MySQL 测试需设置 `VOUCHER_MYSQL_DSN`，自动创建并删除随机独立数据库，覆盖迁移 up/down、并发冲突、分页和删除级联。浏览器验收使用临时内存账本，不写真实财务库：

```powershell
# 后端测试终端
$env:VOUCHER_BROWSER_SERVER = '1'
$env:BROWSER_FIXTURE_ADDR = '127.0.0.1:18205'
go test ./internal/router -run '^TestVoucherBrowserServer$' -count=1 -timeout 20m

# frontend/ 的另一终端
npm run build
node e2e/support/preview.mjs --dist=dist --port=5625 --api=http://127.0.0.1:18205

# frontend/ 的验收终端，每次使用新启动的临时账本
node e2e/transaction_ordering.mjs
```

脚本覆盖日期、触控、鼠标、键盘、取消、失败恢复、请求重试、刷新和两处同步，并在 `frontend/e2e/shots/refactor-frontend/transaction-order-*.png` 保存桌面与移动端截图。

拖动结束时，拖动反馈层把流水行放回列表，浏览器会重放 `rise-in` 入场动画，导致透明度从 0 开始并向下偏移 12px。可排序的流水行不再使用这段入场动画，保留拖动过渡和首页跳转定位的 `row-flash` 高亮。排序保存引发的后台重取也不再显示下拉刷新条，真实下拉刷新仍显示进度。

`node e2e/transaction_ordering_stability.mjs` 对首页、手机流水和桌面流水逐帧检查放下至保存完成的透明度、列表位置及刷新条，并核对定位高亮和真实下拉刷新。测试会延迟后台列表响应，避免快速本机请求掩盖刷新提示问题；逐帧报告写入已忽略的 `bin/transaction-order-stability.json`。

2026-10-25 验证结果：前端构建、类型检查、90 项单元测试通过；新增 SQLite / MySQL 测试、浏览器验收及既有目录排序生命周期验收通过；仓储、处理器、路由测试和 `go vet ./...` 通过。服务层完整测试中的既有 `TestPersonalLiabilityAccountsAndIndependentRules` 在第 45 行失败，在未修改的 HEAD 导出代码中亦可复现，本次未修改该负债设置行为。

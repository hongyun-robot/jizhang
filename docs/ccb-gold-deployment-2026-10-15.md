# 建行积存金归档与生产发布记录

日期：2026-10-15，北京时间。站点：[记账](https://jizhang.zhanghongyun.cn/)。

## 归档

- 变更 `add-ccb-gold-accounts` 已完成 38/38 任务。
- 同步七份 delta 到主规范：新增 `gold-accounts`，更新 accounts、billing-periods、home-dashboard、identity/access-control、statistics、transactions；新增17条、修改9条 requirement。
- 归档目录：`openspec/changes/archive/2026-10-15-add-ccb-gold-accounts/`。
- 逐一核对同步内容，保留既有 Purpose 和未涉及场景；全部21份主规范验证通过，已有简短 Purpose 等提示保留，未改写无关规范。

## 发布结果

- 首次哈希比对706个文件，72个需同步，孤儿0；实际只同步本次黄金及归档相关差异。
- 重建并切换 `web`、`api`；项目 MySQL/Redis 未重建或清空，其他项目未操作。
- 生产数据库由16升级至 **17:0**，迁移干净。
- 已验证备份：`/srv/jizhang/backups/jizhang-pre-release-20261015-213048-4100681.sql.gz`，压缩文件45709字节；备份保留在生产服务器。
- 独立恢复库 up/down/up 通过，生产迁移前后原财务表的原有字段及记录校验一致。
- 最终命令：`pwsh -ExecutionPolicy Bypass -File .agents/skills/deploy/scripts/deploy.ps1 -Force -Smoke`。
- 最终输出：**`DEPLOY: ALL_GREEN`**。

## 部署校验

| 检查 | 结果 |
|---|---|
| 源码一致性 | 706一致、0不同、0缺失 |
| 容器 | jizhang-web/api/mysql/redis 全部 running/healthy |
| HTTP | `/`、`/api/v1/health` 均200 |
| 图标 | favicon.svg、favicon.ico 的状态和类型均正确 |
| 生产浏览器冒烟 | **22/22 PASS**；临时账本账号已停用、会话已撤销 |
| 黄金生产接口 | 登录后 GET缓存及POST手动刷新均成功，真实建行双边价有效；本次只更新公共报价，无个人持仓/资金记录 |

黄金真实报价样本为银行源时间 `2026-10-15T21:33:55.438+08:00`，客户买入897.40、赎回891.40元/克，系统获取时间 `21:33:55.874+08:00`、状态ok。此为验证时点样本，不代表之后价格。用于验证的额外登录会话已退出撤销。

## 迁移保护脚本修复

首轮构建成功，但原恢复校验使用 `mysqldump` 的完整 INSERT 文本作为数据指纹，增加可空 `transactions.gold_event_id` 后，即使原数据不变，INSERT 的列数也改变，因而阻止切换。该轮生产库未升级，脚本恢复旧API；随后确认四容器健康、版本16:0。

已修复 `.agents/skills/deploy/scripts/release.sh`：先冻结原财务表的原有列及主键顺序，再对原有每个字段的JSON行表示计算SHA-256。仍检查原行数和原字段值，新增中性关联列不再造成文本形状误报；SQL读取失败直接终止，未删除或放宽原表范围。

在第一轮备份的独立恢复库中，验证新增探针行和修改探针字段都会改变指纹，删除探针恢复原指纹；随后完整 up → goto16 → up 均保持原财务数据一致。恢复库按受控随机名称清理，生产数据未用于探针写入。

完成此验证后使用 `-Force` 重跑，创建新备份并再次在私有恢复库验证，再进行生产迁移和健康切换。业务数据回滚限制仍遵守 [黄金迁移说明](ccb-gold-migration.md)：存在个人黄金财务历史时不能删除历史后降级。

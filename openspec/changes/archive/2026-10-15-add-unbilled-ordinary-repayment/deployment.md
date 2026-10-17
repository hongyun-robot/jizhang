# 提前还款功能生产发布记录

发布时间：2026-10-15，Asia/Shanghai。站点：<https://jizhang.zhanghongyun.cn/>。

## 归档

`add-unbilled-ordinary-repayment` 的 18/18 项任务完成后，先将 `liability-billing` 的两条修改要求与四条新增要求同步至主规范，保持原 Purpose 和其余要求不变。20 份主规范校验通过，变更归档至本目录，活动变更清单为空。

## 发布前备份

备份在源码同步及容器切换前完成，服务器时间为 19:10:17。数据库采用事务一致性导出；备份已解压恢复到随机命名的独立验证库，验证表和迁移状态后删除验证库，没有覆盖生产库。

- 数据库：`/srv/jizhang/backups/jizhang-pre-ordinary-advance-20261015-191017-3839149.sql.gz`，37,280 字节。
- 数据库 SHA-256：`97ce27934df51de606761c1088a777fdb39b704352faeb43602f041861369935`。
- 旧源码：`/srv/jizhang/backups/jizhang-pre-ordinary-advance-20261015-191017-3839149.source.tar.gz`，1,094,275 字节；不包含 `.env`。
- 旧源码 SHA-256：`59c3f0ec403f1170132d0a2b73f6533a8b9610f495b7afbc6f30a5668916fd2a`。
- 同名前缀的 `.schema.txt`、`.images.txt`、`.restore.txt`、`.sha256` 保存迁移状态、原 API／Web 镜像标识、恢复核验及校验和。

数据库与源码压缩文件完整性检查通过，独立恢复结果为 `BACKUP: ALL_GREEN`。迁移状态为 `16:0`；本功能没有新增数据库迁移。

## 部署与常规验证

执行仓库部署技能的标准命令：

```powershell
pwsh -ExecutionPolicy Bypass -File .agents\skills\deploy\scripts\deploy.ps1 -Smoke
```

首次发布同步 38 个差异文件（19 个内容变化、19 个新增），没有孤儿文件。重建并切换 `web`、`api`，MySQL／Redis 保持原实例运行；迁移状态仍为 `16:0`。

| 验证 | 结果 |
| --- | --- |
| 源码哈希复核 | 首次发布 666 个文件一致，0 内容不同、0 缺失 |
| `jizhang-web`／`jizhang-api`／`jizhang-mysql`／`jizhang-redis` | 全部 `running/healthy` |
| 首页与 `/api/v1/health` | 均返回 HTTP 200 |
| `/favicon.svg`、`/favicon.ico` | HTTP 200，内容类型正确 |
| `verify.sh` | `VERIFY: ALL_GREEN` |
| 生产 `prod_smoke.mjs` | 22/22 PASS；临时账本账号已停用、会话已撤销 |
| 部署最终判据 | `DEPLOY: ALL_GREEN` |

发布证据补充后再执行一次标准部署命令（不带 `-Smoke`），仅同步归档中的文档、验证脚本和截图，跳过应用重建，并复核源码、容器、HTTP 和图标。

## 提前还款线上专项

15 项检查全部通过。验证脚本保存在 [production-check.mjs](production-check.mjs)，从仓库根运行，生产凭据只通过 `JZ_PASS` 环境变量提供，不写入文件。

1. 使用唯一命名的临时账号及本人零余额账户创建未出账普通消费，未修改其他账号资金。
2. 超额、未来日期及默认已出账范围提交均被原子拒绝；预览可还金额为 1000 元，预览本身不生成流水。
3. 线上桌面入口和服务端核对正常；独立手机会话可选「提前还未出账」，无页面横向溢出。
4. 手机保存 300 元提前还款后，普通未出账欠款剩 700 元，资金账户只减少 300 元，全部账户净资产合计不变。
5. 原请求重试返回同一还款操作，只存在一笔有效还款；浏览器无运行时异常。
6. 撤销后恢复原 1000 元欠款及对应资产，再删除本次测试消费，确认本次临时账号无剩余流水、测试负债归零。
7. 临时账号已停用并撤销会话。专项账号 10 及预检账号 9 均已完成流水清理与停用；系统保留停用账号和操作追溯记录，不物理删除用户。

生产检查结果：`PROD_ORDINARY_ADVANCE: ALL_GREEN checks=15`。

截图：[桌面入口](screenshots/prod-ordinary-advance-desktop.png)、[474px 手机入口](screenshots/prod-ordinary-advance-mobile.png)。

## 回滚限制

本次没有 DDL 升级。普通提前还款使用 `ordinary_advance` 固定消费目标，产生此类记录后，不能直接切回不识别该模式的旧后端，也不能清空目标或改为 auto。需要回退时保留兼容后端修复，并使用发布前数据库／源码备份及镜像标识评估恢复方案；恢复数据库前须核对备份之后的真实财务写入。

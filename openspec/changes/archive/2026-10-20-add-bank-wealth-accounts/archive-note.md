# 银行理财账户变更归档

归档日期：2026-10-20（北京时间）。变更 `add-bank-wealth-accounts`，schema 为 `spec-driven`，60/60 项任务完成；用户明确选择同步主规范后归档。

- 已新增 `wealth-accounts` 主规范，并同步 accounts、billing-periods、gold-accounts、home-dashboard、identity/access-control、statistics、transactions，共新增 26 项、修改 11 项要求。既有主规范标题、Purpose 及本变更未涉及的要求保留。
- 全部 8 份增量规范与对应主规范逐要求比对一致，主规范无增量操作标题；`openspec validate --specs` 为 22 passed / 0 failed，归档前变更严格校验通过。既有 Purpose 简短提示保留。
- 原变更 136 个文件整体移动，移动后逐文件 SHA-256 全部一致，`.openspec.yaml` 随目录保留。随后仅修正 5 份 Markdown 中因目录深度变化而失效的外部文档链接。
- 原 80 个证据文件全部逐字节保留，包括两次预检、12 个正式槽、14 份公开响应、完整 state.json、Go 回放日志、截图和此前失败记录。验收结论为 12/12 正式槽成功、无失败/漏槽/中断，14/14 公开响应 Go 回放通过。
- README、采集器说明、研究记录、部署技能及已暂停自动化中的引用已更新到归档路径。
- 归档前后只读检查生产 `jizhang-wealth-collector` 和 API，均 running/healthy，启动时间、重启计数和镜像完全一致。生产自动净值同步继续独立运行；三天验收自动化继续 PAUSED。

完整验收见 [最终验收记录](qualification-final-2026-10-20.md)，原始公开证据见 [证据目录](evidence/qualification-20261016t014727z/final-20261020/)。归档完整性摘要与逐文件哈希见 [保留清单](archive-preservation.json)，生产只读核对见 [运行状态](archive-production-check.json)。

本次仅同步和归档本地规范与文档，没有部署、生产数据库写入、容器重启、产品准入调整或删除验收证据。

-- 退款流水（任务 1.1）：kind 追加第 4 个枚举值 refund（仅追加，不改动存量数据）
ALTER TABLE transactions
    MODIFY kind ENUM('expense','income','transfer','refund') NOT NULL;

-- 退款可选关联原支出：可空列 + 索引（完整性由服务层强制，不加 FK 级联，见 design D2）
ALTER TABLE transactions
    ADD COLUMN related_txn_id BIGINT NULL COMMENT '关联原支出流水（退款用）' AFTER rule_id,
    ADD KEY idx_txn_related (related_txn_id);

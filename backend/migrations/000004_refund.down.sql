-- 回滚：先删列（同时清除新枚举值的引用），再还原 ENUM 定义
-- 注意：若表中已存在 kind='refund' 的流水，还原 ENUM 会失败，需先清理退款数据
ALTER TABLE transactions
    DROP KEY idx_txn_related,
    DROP COLUMN related_txn_id;

ALTER TABLE transactions
    MODIFY kind ENUM('expense','income','transfer') NOT NULL;

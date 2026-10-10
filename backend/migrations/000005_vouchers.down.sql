-- CHECK 在有任何券数据时终止回滚，且失败只留下连接级临时表。
CREATE TEMPORARY TABLE voucher_rollback_guard (
    data_count BIGINT NOT NULL,
    CONSTRAINT cannot_rollback_vouchers_with_data CHECK (data_count = 0)
);
INSERT INTO voucher_rollback_guard (data_count)
SELECT (SELECT COUNT(*) FROM vouchers)
     + (SELECT COUNT(*) FROM voucher_events)
     + (SELECT COUNT(*) FROM transactions WHERE purpose <> 'regular' OR voucher_event_id IS NOT NULL);
DROP TEMPORARY TABLE voucher_rollback_guard;

ALTER TABLE transactions
    DROP FOREIGN KEY fk_txn_voucher_event,
    DROP INDEX uk_txn_voucher_event,
    DROP COLUMN voucher_event_id,
    DROP COLUMN purpose,
    MODIFY source ENUM('manual','recurring') NOT NULL DEFAULT 'manual';
DROP TABLE voucher_events;
DROP TABLE vouchers;
DELETE FROM settings WHERE `key` = 'ledger_write_guard';

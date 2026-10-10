-- Refuse to erase any billing history. Run with the migration multi-statement driver.
DROP PROCEDURE IF EXISTS guard_liability_down;
CREATE PROCEDURE guard_liability_down()
BEGIN
 IF EXISTS(SELECT 1 FROM liability_statements LIMIT 1) OR EXISTS(SELECT 1 FROM installment_plans LIMIT 1) OR EXISTS(SELECT 1 FROM liability_operations LIMIT 1) OR EXISTS(SELECT 1 FROM transactions WHERE source='liability_auto' LIMIT 1) THEN
  SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='Liability history exists; retain schema and use a compatible service version';
 END IF;
END;
CALL guard_liability_down();
DROP PROCEDURE guard_liability_down;
ALTER TABLE notifications DROP FOREIGN KEY fk_notification_statement, DROP INDEX uk_notification_dedup, DROP COLUMN statement_id, DROP COLUMN dedup_key, DROP COLUMN resolved_at;
ALTER TABLE transactions DROP FOREIGN KEY fk_txn_installment_plan, DROP FOREIGN KEY fk_txn_liability_term, DROP FOREIGN KEY fk_txn_liability_operation, DROP INDEX idx_txn_plan, DROP COLUMN installment_plan_id, DROP COLUMN liability_term_id, DROP COLUMN liability_operation_id, MODIFY source ENUM('manual','recurring','voucher_auto') NOT NULL DEFAULT 'manual';
DROP TABLE liability_allocations;
DROP TABLE liability_credit_events;
DROP TABLE liability_debt_items;
DROP TABLE liability_operations;
DROP TABLE installment_terms;
DROP TABLE installment_plans;
DROP TABLE liability_statements;
DROP TABLE liability_bill_rules;
ALTER TABLE accounts DROP COLUMN liability_type, DROP COLUMN billing_enabled, MODIFY repayment_day TINYINT NULL COMMENT '信用卡还款日 1-28';

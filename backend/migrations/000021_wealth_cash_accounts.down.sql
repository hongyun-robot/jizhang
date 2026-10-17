DROP PROCEDURE IF EXISTS wealth_cash_rollback_guard;
CREATE PROCEDURE wealth_cash_rollback_guard()
BEGIN
 IF EXISTS(SELECT 1 FROM wealth_cash_links) OR EXISTS(SELECT 1 FROM wealth_cash_yields) OR EXISTS(SELECT 1 FROM wealth_cash_flows)
 OR EXISTS(SELECT 1 FROM wealth_cash_audits) OR EXISTS(SELECT 1 FROM accounts WHERE asset_class='wealth_cash')
 OR EXISTS(SELECT 1 FROM transactions WHERE wealth_cash_yield_id IS NOT NULL) THEN
  SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='Wealth cash configuration or yield history exists; retain records and use forward repair';
 END IF;
END;
CALL wealth_cash_rollback_guard();
DROP PROCEDURE wealth_cash_rollback_guard;
ALTER TABLE transactions DROP CHECK ck_txn_cash_yield, DROP FOREIGN KEY fk_txn_cash_yield,
 DROP INDEX idx_txn_cash_yield_owner,
 DROP INDEX uk_txn_cash_yield, DROP COLUMN wealth_cash_yield_id,
 MODIFY source ENUM('manual','recurring','voucher_auto','liability_auto') NOT NULL DEFAULT 'manual';
DROP TABLE wealth_cash_audits;
DROP TABLE wealth_cash_flows;
DROP TABLE wealth_cash_yields;
DROP TABLE wealth_cash_links;
ALTER TABLE accounts DROP CHECK ck_account_asset_class;
ALTER TABLE accounts ADD CONSTRAINT ck_account_asset_class CHECK
 ((type='asset' AND (asset_class IS NULL OR asset_class IN ('','cash','fund','gold','wealth')))
 OR (type='liability' AND (asset_class IS NULL OR asset_class='')));

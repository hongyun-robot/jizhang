-- This guard executes before the first destructive statement.
DROP PROCEDURE IF EXISTS wealth_rollback_guard;
CREATE PROCEDURE wealth_rollback_guard()
BEGIN
 IF EXISTS(SELECT 1 FROM accounts WHERE asset_class='wealth')
 OR EXISTS(SELECT 1 FROM wealth_positions) OR EXISTS(SELECT 1 FROM wealth_events)
 OR EXISTS(SELECT 1 FROM wealth_mutations) OR EXISTS(SELECT 1 FROM wealth_audits)
 OR EXISTS(SELECT 1 FROM wealth_manual_navs) OR EXISTS(SELECT 1 FROM wealth_manual_audits)
 OR EXISTS(SELECT 1 FROM transactions WHERE wealth_event_id IS NOT NULL) THEN
  SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='Personal wealth data or audit exists; retain schema and use a forward repair';
 END IF;
END;
CALL wealth_rollback_guard();
DROP PROCEDURE wealth_rollback_guard;
ALTER TABLE transactions DROP CHECK ck_txn_wealth_exclusive, DROP FOREIGN KEY fk_txn_wealth_event, DROP INDEX uk_txn_wealth_event, DROP COLUMN wealth_event_id;
DROP TABLE wealth_sync_slots,wealth_series,wealth_manual_audits,wealth_manual_navs,wealth_valuations,wealth_audits,wealth_mutations,wealth_events,wealth_positions,wealth_nav_revisions,wealth_navs,wealth_products;
ALTER TABLE accounts DROP CHECK ck_account_asset_class;
ALTER TABLE accounts ADD CONSTRAINT ck_account_asset_class CHECK
 ((type='asset' AND (asset_class IS NULL OR asset_class IN ('','cash','fund','gold')))
 OR (type='liability' AND (asset_class IS NULL OR asset_class='')));

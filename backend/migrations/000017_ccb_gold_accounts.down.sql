DROP PROCEDURE IF EXISTS gold_rollback_guard;
CREATE PROCEDURE gold_rollback_guard()
BEGIN
 IF EXISTS(SELECT 1 FROM accounts WHERE asset_class='gold')
 OR EXISTS(SELECT 1 FROM gold_positions) OR EXISTS(SELECT 1 FROM gold_events)
 OR EXISTS(SELECT 1 FROM gold_mutations) OR EXISTS(SELECT 1 FROM gold_audits)
 OR EXISTS(SELECT 1 FROM transactions WHERE gold_event_id IS NOT NULL) THEN
  SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='Personal gold data exists; retain schema and restore a verified backup';
 END IF;
END;
CALL gold_rollback_guard();
DROP PROCEDURE gold_rollback_guard;
ALTER TABLE transactions DROP CHECK ck_txn_gold_exclusive, DROP FOREIGN KEY fk_txn_gold_event, DROP INDEX uk_txn_gold_event, DROP COLUMN gold_event_id;
DROP TABLE gold_valuations,gold_audits,gold_mutations,gold_events,gold_positions,gold_quote_observations,gold_quote_syncs;
ALTER TABLE accounts DROP CHECK ck_account_asset_class;
ALTER TABLE accounts ADD CONSTRAINT ck_account_asset_class CHECK
 ((type='asset' AND (asset_class IS NULL OR asset_class IN ('','cash','fund')))
 OR (type='liability' AND (asset_class IS NULL OR asset_class='')));

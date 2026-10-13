DROP PROCEDURE IF EXISTS fund_rollback_guard;
CREATE PROCEDURE fund_rollback_guard()
BEGIN
 IF EXISTS(SELECT 1 FROM accounts WHERE asset_class='fund')
 OR EXISTS(SELECT 1 FROM fund_positions) OR EXISTS(SELECT 1 FROM fund_events)
 OR EXISTS(SELECT 1 FROM fund_mutations) OR EXISTS(SELECT 1 FROM transactions WHERE fund_event_id IS NOT NULL) THEN
  SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='Personal fund data exists; retain schema and restore a verified backup';
 END IF;
END;
CALL fund_rollback_guard();
DROP PROCEDURE fund_rollback_guard;
ALTER TABLE transactions DROP FOREIGN KEY fk_txn_fund_event, DROP INDEX uk_txn_fund_event, DROP COLUMN fund_event_id;
DROP TABLE fund_audits,fund_valuations,fund_dividends,fund_mutations,fund_events,fund_positions,fund_nav_revisions,fund_navs,fund_instruments;
ALTER TABLE accounts DROP CHECK ck_account_asset_class, DROP INDEX uk_account_id_owner, DROP COLUMN asset_class;

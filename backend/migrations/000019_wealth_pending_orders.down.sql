DROP PROCEDURE IF EXISTS wealth_order_rollback_guard;
CREATE PROCEDURE wealth_order_rollback_guard()
BEGIN
 IF EXISTS(SELECT 1 FROM wealth_events WHERE order_json<>'') THEN
  SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='Wealth order history exists; retain metadata and use a forward repair';
 END IF;
END;
CALL wealth_order_rollback_guard();
DROP PROCEDURE wealth_order_rollback_guard;
ALTER TABLE wealth_events DROP COLUMN order_json;

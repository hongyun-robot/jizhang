DROP PROCEDURE IF EXISTS cmbcwm_wealth_rollback_guard;
CREATE PROCEDURE cmbcwm_wealth_rollback_guard()
BEGIN
 IF EXISTS(SELECT 1 FROM wealth_positions p JOIN wealth_products w ON w.id=p.product_id
           WHERE w.issuer='cmbcwm' AND w.registration_code='Z7007923000092' AND w.code='FBAE68020E')
 OR EXISTS(SELECT 1 FROM wealth_manual_navs n JOIN wealth_products w ON w.id=n.product_id
           WHERE w.issuer='cmbcwm' AND w.registration_code='Z7007923000092' AND w.code='FBAE68020E') THEN
  SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='Minsheng wealth history exists; retain product and use a forward repair';
 END IF;
END;
CALL cmbcwm_wealth_rollback_guard();
DROP PROCEDURE cmbcwm_wealth_rollback_guard;
DELETE n FROM wealth_sync_slots n JOIN wealth_products w ON w.id=n.product_id
 WHERE w.issuer='cmbcwm' AND w.registration_code='Z7007923000092' AND w.code='FBAE68020E';
DELETE n FROM wealth_nav_revisions n JOIN wealth_products w ON w.id=n.product_id
 WHERE w.issuer='cmbcwm' AND w.registration_code='Z7007923000092' AND w.code='FBAE68020E';
DELETE n FROM wealth_navs n JOIN wealth_products w ON w.id=n.product_id
 WHERE w.issuer='cmbcwm' AND w.registration_code='Z7007923000092' AND w.code='FBAE68020E';
DELETE FROM wealth_products WHERE issuer='cmbcwm' AND registration_code='Z7007923000092' AND code='FBAE68020E';

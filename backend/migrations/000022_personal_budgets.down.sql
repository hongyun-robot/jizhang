-- Refuse to collapse independently edited budgets back into a shared total.
DROP PROCEDURE IF EXISTS personal_budget_rollback_guard;
CREATE PROCEDURE personal_budget_rollback_guard()
BEGIN
 IF (SELECT COUNT(*) FROM budgets)<>(SELECT COUNT(*) FROM personal_budget_after)
 OR EXISTS(SELECT 1 FROM budgets a LEFT JOIN personal_budget_after b ON a.id=b.id
  WHERE b.id IS NULL OR a.owner_id<>b.owner_id OR a.period_key<>b.period_key
   OR NOT(a.category_id<=>b.category_id) OR a.amount<>b.amount
   OR NOT(a.created_at<=>b.created_at) OR NOT(a.updated_at<=>b.updated_at))
 OR (SELECT COUNT(*) FROM users)<>(SELECT COUNT(*) FROM personal_budget_after_users)
 OR EXISTS(SELECT 1 FROM users a LEFT JOIN personal_budget_after_users b ON a.id=b.id WHERE b.id IS NULL) THEN
  SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='Personal budgets changed; retain private data and restore a verified backup';
 END IF;
END;
CALL personal_budget_rollback_guard();
DROP PROCEDURE personal_budget_rollback_guard;
DELETE FROM budgets;
ALTER TABLE budgets DROP FOREIGN KEY fk_budget_owner,
 DROP INDEX uk_budget_owner_period_scope,DROP COLUMN scope_id,DROP COLUMN owner_id,
 ADD UNIQUE KEY uk_budget_period_category(period_key,category_id);
INSERT INTO budgets(id,period_key,category_id,amount,created_at,updated_at)
 SELECT id,period_key,category_id,amount,created_at,updated_at FROM personal_budget_before;
DROP TABLE personal_budget_before,personal_budget_after,personal_budget_after_users;

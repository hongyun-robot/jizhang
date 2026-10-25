-- Pause application writes and background jobs; back up the ledger before upgrading.
-- Shared totals become independent initial values for every existing login account.
DROP PROCEDURE IF EXISTS personal_budget_preflight;
CREATE PROCEDURE personal_budget_preflight()
BEGIN
 IF EXISTS(SELECT 1 FROM budgets b LEFT JOIN categories c ON c.id=b.category_id
   LEFT JOIN users u ON u.id=c.owner_id
   WHERE b.category_id IS NOT NULL AND u.id IS NULL)
 OR (EXISTS(SELECT 1 FROM budgets) AND NOT EXISTS(SELECT 1 FROM users)) THEN
  SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='Budget has no valid owner; review ownership before migration';
 END IF;
END;
CALL personal_budget_preflight();
DROP PROCEDURE personal_budget_preflight;

CREATE TABLE personal_budget_before LIKE budgets;
INSERT INTO personal_budget_before SELECT * FROM budgets;
ALTER TABLE budgets ADD COLUMN owner_id BIGINT NULL;
UPDATE budgets b JOIN categories c ON c.id=b.category_id
 SET b.owner_id=c.owner_id,b.updated_at=b.updated_at;
-- The previous reader used the last total row by ID if legacy duplicates existed.
INSERT INTO budgets(owner_id,period_key,category_id,amount,created_at,updated_at)
SELECT u.id,b.period_key,NULL,b.amount,b.created_at,b.updated_at
FROM personal_budget_before b CROSS JOIN users u
WHERE b.category_id IS NULL AND NOT EXISTS(
 SELECT 1 FROM personal_budget_before newer
 WHERE newer.period_key=b.period_key AND newer.category_id IS NULL AND newer.id>b.id);
DELETE FROM budgets WHERE owner_id IS NULL;
ALTER TABLE budgets
 MODIFY owner_id BIGINT NOT NULL,
 DROP INDEX uk_budget_period_category,
 ADD COLUMN scope_id BIGINT GENERATED ALWAYS AS (IFNULL(category_id,0)) STORED,
 ADD UNIQUE KEY uk_budget_owner_period_scope(owner_id,period_key,scope_id),
 ADD CONSTRAINT fk_budget_owner FOREIGN KEY(owner_id) REFERENCES users(id) ON DELETE RESTRICT;
CREATE TABLE personal_budget_after LIKE budgets;
INSERT INTO personal_budget_after(id,owner_id,period_key,category_id,amount,created_at,updated_at)
 SELECT id,owner_id,period_key,category_id,amount,created_at,updated_at FROM budgets;
CREATE TABLE personal_budget_after_users AS SELECT id FROM users;

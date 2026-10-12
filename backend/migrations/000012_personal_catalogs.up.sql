-- Keep exact before/after images for a guarded, lossless rollback and audit.
CREATE TABLE catalog_migration_tables (name VARCHAR(64) PRIMARY KEY);
INSERT INTO catalog_migration_tables VALUES ('users'),('accounts'),('categories'),('transactions'),('vouchers'),('voucher_events'),('recurring_rules'),('installment_plans'),('installment_terms'),('liability_operations'),('liability_bill_rules'),('liability_statements'),('liability_debt_items'),('liability_credit_events'),('liability_allocations'),('notifications'),('budgets'),('period_closings'),('audit_logs');
CREATE PROCEDURE catalog_snapshot(IN prefix_name VARCHAR(32))
BEGIN
 DECLARE finished BOOLEAN DEFAULT FALSE;
 DECLARE table_name VARCHAR(64);
 DECLARE tables_cursor CURSOR FOR SELECT name FROM catalog_migration_tables ORDER BY name;
 DECLARE CONTINUE HANDLER FOR NOT FOUND SET finished=TRUE;
 OPEN tables_cursor;
 snapshot_loop: LOOP
  FETCH tables_cursor INTO table_name;
  IF finished THEN LEAVE snapshot_loop; END IF;
  SET @catalog_sql=CONCAT('CREATE TABLE `',prefix_name,table_name,'` LIKE `',table_name,'`');
  PREPARE catalog_statement FROM @catalog_sql; EXECUTE catalog_statement; DEALLOCATE PREPARE catalog_statement;
  SET @catalog_sql=CONCAT('INSERT INTO `',prefix_name,table_name,'` SELECT * FROM `',table_name,'`');
  PREPARE catalog_statement FROM @catalog_sql; EXECUTE catalog_statement; DEALLOCATE PREPARE catalog_statement;
 END LOOP;
 CLOSE tables_cursor;
END;
CALL catalog_snapshot('catalog_before_');

ALTER TABLE categories ADD COLUMN owner_id BIGINT NULL,
 ADD COLUMN legacy_source_id BIGINT NULL,
 ADD KEY idx_category_owner (owner_id,kind,parent_id,sort),
 ADD UNIQUE KEY uk_category_source (owner_id,legacy_source_id),
 ADD CONSTRAINT fk_category_owner FOREIGN KEY(owner_id) REFERENCES users(id) ON DELETE RESTRICT;
ALTER TABLE users ADD COLUMN asset_accounts_initialized BOOLEAN NOT NULL DEFAULT FALSE,
 ADD COLUMN categories_initialized BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE recurring_rules ADD COLUMN blocked_reason VARCHAR(255) NOT NULL DEFAULT '';
CREATE TABLE account_templates (
 code VARCHAR(32) PRIMARY KEY, name VARCHAR(64) NOT NULL, sort INT NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
INSERT INTO account_templates VALUES ('cash','现金',0),('wechat','微信',1),('alipay','支付宝',2),('bank','银行卡',3);
CREATE TABLE category_templates (
 id BIGINT PRIMARY KEY, parent_id BIGINT NULL, kind VARCHAR(16) NOT NULL,
 name VARCHAR(64) NOT NULL, sort INT NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
INSERT INTO category_templates SELECT id,parent_id,kind,name,sort FROM catalog_before_categories;
CREATE TABLE account_create_requests (
 owner_id BIGINT NOT NULL, request_key VARCHAR(128) COLLATE utf8mb4_bin NOT NULL,
 digest CHAR(64) NOT NULL, account_id BIGINT NULL, created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
 PRIMARY KEY(owner_id,request_key), FOREIGN KEY(owner_id) REFERENCES users(id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE catalog_account_ownership (
 account_id BIGINT PRIMARY KEY, previous_owner_id BIGINT NULL, owner_id BIGINT NULL,
 component_id BIGINT NOT NULL, reason VARCHAR(64) NOT NULL
) ENGINE=InnoDB;

-- Propagate connected components without a recursion-depth bound.
CREATE TEMPORARY TABLE catalog_edges (a BIGINT NOT NULL,b BIGINT NOT NULL,PRIMARY KEY(a,b));
INSERT IGNORE INTO catalog_edges SELECT account_id,transfer_account_id FROM transactions WHERE transfer_account_id IS NOT NULL;
INSERT IGNORE INTO catalog_edges SELECT t.account_id,o.account_id FROM transactions t JOIN transactions o ON o.id=t.related_txn_id;
INSERT IGNORE INTO catalog_edges SELECT account_id,fee_account_id FROM installment_plans;
INSERT IGNORE INTO catalog_edges SELECT t.account_id,o.account_id FROM transactions t JOIN liability_operations o ON o.id=t.liability_operation_id;
INSERT IGNORE INTO catalog_edges SELECT t.account_id,p.account_id FROM transactions t JOIN installment_terms term ON term.id=t.liability_term_id JOIN installment_plans p ON p.id=term.plan_id;
CREATE TEMPORARY TABLE catalog_reverse_edges LIKE catalog_edges;
INSERT INTO catalog_reverse_edges SELECT b,a FROM catalog_edges;
INSERT IGNORE INTO catalog_edges SELECT a,b FROM catalog_reverse_edges;
CREATE TEMPORARY TABLE catalog_components (id BIGINT PRIMARY KEY,component BIGINT NOT NULL);
INSERT INTO catalog_components SELECT id,id FROM accounts;
CREATE TEMPORARY TABLE catalog_component_copy LIKE catalog_components;
CREATE PROCEDURE catalog_connect()
BEGIN
 DECLARE changes_count BIGINT DEFAULT 1;
 WHILE changes_count>0 DO
  DELETE FROM catalog_component_copy;
  INSERT INTO catalog_component_copy SELECT * FROM catalog_components;
  UPDATE catalog_components c JOIN (SELECT e.a,MIN(n.component) AS component FROM catalog_edges e JOIN catalog_component_copy n ON n.id=e.b GROUP BY e.a) next_component ON next_component.a=c.id
  SET c.component=next_component.component WHERE next_component.component<c.component;
  SET changes_count=ROW_COUNT();
 END WHILE;
END;
CALL catalog_connect();
DROP PROCEDURE catalog_connect;
CREATE TEMPORARY TABLE catalog_users (account_id BIGINT NOT NULL,user_id BIGINT NOT NULL,PRIMARY KEY(account_id,user_id));
INSERT IGNORE INTO catalog_users SELECT account_id,created_by FROM transactions;
INSERT IGNORE INTO catalog_users SELECT transfer_account_id,created_by FROM transactions WHERE transfer_account_id IS NOT NULL;
INSERT IGNORE INTO catalog_users SELECT account_id,created_by FROM vouchers;
INSERT IGNORE INTO catalog_users SELECT account_id,created_by FROM recurring_rules;
INSERT IGNORE INTO catalog_users SELECT account_id,created_by FROM installment_plans;
INSERT IGNORE INTO catalog_users SELECT fee_account_id,created_by FROM installment_plans;
SET @catalog_initial_owner=COALESCE((SELECT MIN(id) FROM users WHERE role='admin'),(SELECT MIN(id) FROM users));
CREATE TEMPORARY TABLE catalog_anchors AS SELECT c.component,COUNT(DISTINCT a.owner_id) AS owners,MIN(a.owner_id) AS owner_id FROM catalog_components c JOIN accounts a ON a.id=c.id AND a.type='liability' GROUP BY c.component;
CREATE TEMPORARY TABLE catalog_creators AS SELECT c.component,COUNT(DISTINCT u.user_id) AS creators,MIN(u.user_id) AS creator FROM catalog_components c JOIN catalog_users u ON u.account_id=c.id GROUP BY c.component;
INSERT INTO catalog_account_ownership
SELECT a.id,a.owner_id,
 CASE WHEN a.type='liability' THEN a.owner_id
 WHEN COALESCE(anchor.owners,0)=1 AND (COALESCE(history.creators,0)=0 OR (history.creators=1 AND history.creator=anchor.owner_id)) THEN anchor.owner_id
 WHEN COALESCE(anchor.owners,0)=0 AND history.creators=1 THEN history.creator ELSE @catalog_initial_owner END,
 c.component,
 CASE WHEN a.type='liability' THEN 'existing-personal-liability'
 WHEN COALESCE(anchor.owners,0)=1 AND (COALESCE(history.creators,0)=0 OR (history.creators=1 AND history.creator=anchor.owner_id)) THEN 'single-owner-funding-chain'
 WHEN COALESCE(anchor.owners,0)=0 AND history.creators=1 THEN 'single-historical-user' ELSE 'ambiguous-or-empty-initial-admin' END
FROM accounts a JOIN catalog_components c ON c.id=a.id LEFT JOIN catalog_anchors anchor ON anchor.component=c.component LEFT JOIN catalog_creators history ON history.component=c.component;
UPDATE accounts a JOIN catalog_account_ownership o ON o.account_id=a.id SET a.owner_id=o.owner_id,a.updated_at=a.updated_at WHERE a.type='asset';

-- Clone by source identity, not by name, preserving custom two-level trees.
UPDATE categories SET owner_id=@catalog_initial_owner,legacy_source_id=id,updated_at=updated_at;
INSERT INTO categories(owner_id,legacy_source_id,parent_id,kind,name,sort,created_at,updated_at)
SELECT u.id,c.id,NULL,c.kind,c.name,c.sort,c.created_at,c.updated_at FROM users u CROSS JOIN catalog_before_categories c WHERE u.id<>@catalog_initial_owner AND c.parent_id IS NULL;
INSERT INTO categories(owner_id,legacy_source_id,parent_id,kind,name,sort,created_at,updated_at)
SELECT u.id,c.id,p.id,c.kind,c.name,c.sort,c.created_at,c.updated_at FROM users u CROSS JOIN catalog_before_categories c JOIN categories p ON p.owner_id=u.id AND p.legacy_source_id=c.parent_id WHERE u.id<>@catalog_initial_owner AND c.parent_id IS NOT NULL;
UPDATE transactions t JOIN accounts a ON a.id=t.account_id JOIN categories c ON c.owner_id=a.owner_id AND c.legacy_source_id=t.category_id SET t.category_id=c.id,t.updated_at=t.updated_at;
UPDATE vouchers v JOIN accounts a ON a.id=v.account_id JOIN categories c ON c.owner_id=a.owner_id AND c.legacy_source_id=v.category_id SET v.category_id=c.id,v.updated_at=v.updated_at;
UPDATE voucher_events e JOIN accounts a ON a.id=e.account_id JOIN categories c ON c.owner_id=a.owner_id AND c.legacy_source_id=e.category_id SET e.category_id=c.id;
UPDATE recurring_rules r JOIN categories c ON c.owner_id=r.created_by AND c.legacy_source_id=r.category_id JOIN accounts a ON a.id=r.account_id SET r.category_id=c.id,r.status=IF(a.owner_id=r.created_by,r.status,'paused'),r.blocked_reason=IF(a.owner_id=r.created_by,'','原共享账户已归其他账号，请选择自己的账户'),r.updated_at=r.updated_at;
UPDATE installment_plans p JOIN accounts a ON a.id=p.account_id JOIN categories c ON c.owner_id=a.owner_id AND c.legacy_source_id=p.fee_category_id SET p.fee_category_id=c.id,p.updated_at=p.updated_at;
UPDATE installment_terms t JOIN installment_plans p ON p.id=t.plan_id JOIN accounts a ON a.id=p.account_id JOIN accounts f ON f.id=p.fee_account_id SET t.fee_status='blocked',t.blocked_reason='费用支付账户已归其他账号，请修改费用来源' WHERE NOT(a.owner_id<=>f.owner_id) AND t.fee_txn_id IS NULL AND t.status='scheduled';
UPDATE notifications n SET n.owner_id=CASE WHEN n.kind='recurring_patched' THEN COALESCE((SELECT r.created_by FROM recurring_rules r WHERE r.id=CAST(JSON_UNQUOTE(JSON_EXTRACT(n.payload,'$.rule_id')) AS UNSIGNED)),@catalog_initial_owner) ELSE @catalog_initial_owner END WHERE n.owner_id IS NULL;
INSERT INTO accounts(name,type,owner_id,opening_balance,status,sort)
SELECT t.name,'asset',u.id,0,'active',t.sort FROM users u CROSS JOIN account_templates t WHERE NOT EXISTS(SELECT 1 FROM accounts a WHERE a.owner_id=u.id AND a.type='asset' AND BINARY a.name=BINARY t.name);
UPDATE users SET asset_accounts_initialized=TRUE,categories_initialized=TRUE,updated_at=updated_at;

CALL catalog_snapshot('catalog_after_');
DROP PROCEDURE catalog_snapshot;
DROP TEMPORARY TABLE catalog_edges,catalog_reverse_edges,catalog_components,catalog_component_copy,catalog_users,catalog_anchors,catalog_creators;

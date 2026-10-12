-- Exact comparison prevents rollback from deleting or merging post-upgrade data.
DROP PROCEDURE IF EXISTS catalog_rollback;
CREATE PROCEDURE catalog_rollback()
BEGIN
 DECLARE finished BOOLEAN DEFAULT FALSE;
 DECLARE table_name VARCHAR(64);
 DECLARE comparisons LONGTEXT;
 DECLARE assignments LONGTEXT;
 DECLARE tables_cursor CURSOR FOR SELECT name FROM catalog_migration_tables ORDER BY name;
 DECLARE CONTINUE HANDLER FOR NOT FOUND SET finished=TRUE;
 SET SESSION group_concat_max_len=65536;
 IF EXISTS(SELECT 1 FROM account_create_requests) THEN
  SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='Personal catalog writes exist; retain schema and restore a verified backup';
 END IF;
 OPEN tables_cursor;
 guard_loop: LOOP
  FETCH tables_cursor INTO table_name;
  IF finished THEN LEAVE guard_loop; END IF;
  SELECT GROUP_CONCAT(CONCAT('(BINARY a.`',column_name,'` <=> BINARY b.`',column_name,'`)') SEPARATOR ' AND ') INTO comparisons FROM information_schema.columns WHERE table_schema=DATABASE() AND information_schema.columns.table_name=table_name;
  SET @catalog_sql=CONCAT('SELECT (SELECT COUNT(*) FROM `',table_name,'`)<>(SELECT COUNT(*) FROM `catalog_after_',table_name,'`) OR EXISTS(SELECT 1 FROM `',table_name,'` a LEFT JOIN `catalog_after_',table_name,'` b ON a.id=b.id WHERE b.id IS NULL OR NOT(',comparisons,')) INTO @catalog_changed');
  PREPARE catalog_statement FROM @catalog_sql; EXECUTE catalog_statement; DEALLOCATE PREPARE catalog_statement;
  IF @catalog_changed THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='Personal catalog data changed; automatic rollback would lose data'; END IF;
 END LOOP;
 CLOSE tables_cursor;
 SET finished=FALSE;
 OPEN tables_cursor;
 restore_loop: LOOP
  FETCH tables_cursor INTO table_name;
  IF finished THEN LEAVE restore_loop; END IF;
  SELECT GROUP_CONCAT(CONCAT('a.`',column_name,'`=b.`',column_name,'`') SEPARATOR ',') INTO assignments FROM information_schema.columns WHERE table_schema=DATABASE() AND information_schema.columns.table_name=CONCAT('catalog_before_',table_name) AND column_name<>'id';
  SET @catalog_sql=CONCAT('UPDATE `',table_name,'` a JOIN `catalog_before_',table_name,'` b ON a.id=b.id SET ',assignments);
  PREPARE catalog_statement FROM @catalog_sql; EXECUTE catalog_statement; DEALLOCATE PREPARE catalog_statement;
 END LOOP;
 CLOSE tables_cursor;
 DELETE c FROM categories c LEFT JOIN catalog_before_categories b ON b.id=c.id WHERE b.id IS NULL AND c.parent_id IS NOT NULL;
 DELETE c FROM categories c LEFT JOIN catalog_before_categories b ON b.id=c.id WHERE b.id IS NULL;
 DELETE a FROM accounts a LEFT JOIN catalog_before_accounts b ON b.id=a.id WHERE b.id IS NULL;
 SET finished=FALSE;
 OPEN tables_cursor;
 cleanup_loop: LOOP
  FETCH tables_cursor INTO table_name;
  IF finished THEN LEAVE cleanup_loop; END IF;
  SET @catalog_sql=CONCAT('DROP TABLE `catalog_before_',table_name,'`,`catalog_after_',table_name,'`');
  PREPARE catalog_statement FROM @catalog_sql; EXECUTE catalog_statement; DEALLOCATE PREPARE catalog_statement;
 END LOOP;
 CLOSE tables_cursor;
END;
CALL catalog_rollback();
DROP PROCEDURE catalog_rollback;
DROP TABLE account_create_requests,category_templates,account_templates,catalog_account_ownership,catalog_migration_tables;
ALTER TABLE categories DROP FOREIGN KEY fk_category_owner,DROP INDEX idx_category_owner,DROP INDEX uk_category_source,DROP COLUMN owner_id,DROP COLUMN legacy_source_id;
ALTER TABLE users DROP COLUMN asset_accounts_initialized,DROP COLUMN categories_initialized;
ALTER TABLE recurring_rules DROP COLUMN blocked_reason;

-- Guard all changes before destructive DDL. Never merge private edits into a shared catalog.
DROP PROCEDURE IF EXISTS platform_catalog_rollback_guard;
CREATE PROCEDURE platform_catalog_rollback_guard()
BEGIN
 IF (SELECT COUNT(*) FROM voucher_platforms)<>(SELECT COUNT(*) FROM platform_catalog_after)
 OR EXISTS(SELECT 1 FROM voucher_platforms a LEFT JOIN platform_catalog_after b ON a.id=b.id
  WHERE b.id IS NULL OR a.owner_id<>b.owner_id OR NOT(BINARY a.name<=>BINARY b.name)
  OR NOT(a.created_at<=>b.created_at))
 OR (SELECT COUNT(*) FROM users)<>(SELECT COUNT(*) FROM platform_catalog_after_users)
 OR EXISTS(SELECT 1 FROM users a LEFT JOIN platform_catalog_after_users b ON a.id=b.id
  WHERE b.id IS NULL OR a.voucher_platforms_initialized<>b.voucher_platforms_initialized)
 OR (SELECT COUNT(*) FROM voucher_platform_templates)<>(SELECT COUNT(*) FROM platform_catalog_after_templates)
 OR EXISTS(SELECT 1 FROM voucher_platform_templates a LEFT JOIN platform_catalog_after_templates b
  ON BINARY a.name=BINARY b.name WHERE b.name IS NULL) THEN
  SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='Personal platform catalog changed; retain private data and restore a verified backup';
 END IF;
END;
CALL platform_catalog_rollback_guard();
DROP PROCEDURE platform_catalog_rollback_guard;
ALTER TABLE voucher_platforms DROP FOREIGN KEY fk_voucher_platform_owner,
 DROP INDEX uk_voucher_platform_owner_name,DROP COLUMN owner_id;
DELETE FROM voucher_platforms;
INSERT INTO voucher_platforms(id,name,created_at) SELECT id,name,created_at FROM platform_catalog_before;
ALTER TABLE voucher_platforms ADD UNIQUE KEY uk_voucher_platform_name(name);
ALTER TABLE users DROP COLUMN voucher_platforms_initialized;
DROP TABLE voucher_platform_templates,platform_catalog_mapping,platform_catalog_before,
 platform_catalog_before_users,platform_catalog_after,platform_catalog_after_users,platform_catalog_after_templates;

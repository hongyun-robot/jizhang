-- Run with application writes and workers stopped. Financial tables are read only.
DROP PROCEDURE IF EXISTS platform_catalog_preflight;
CREATE PROCEDURE platform_catalog_preflight()
BEGIN
 IF EXISTS(SELECT 1 FROM voucher_platforms WHERE
  CHAR_LENGTH(REGEXP_REPLACE(name,'^[[:space:]]+|[[:space:]]+$','')) NOT BETWEEN 1 AND 128)
 OR EXISTS(SELECT 1 FROM transactions WHERE
  CHAR_LENGTH(REGEXP_REPLACE(platform,'^[[:space:]]+|[[:space:]]+$',''))>128)
 OR EXISTS(SELECT 1 FROM vouchers WHERE
  CHAR_LENGTH(REGEXP_REPLACE(platform,'^[[:space:]]+|[[:space:]]+$',''))>128) THEN
  SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='Invalid platform names; review the catalog and financial records before migration';
 END IF;
 IF EXISTS(SELECT 1 FROM transactions t LEFT JOIN accounts a ON a.id=t.account_id
  LEFT JOIN users u ON u.id=a.owner_id
  WHERE REGEXP_REPLACE(t.platform,'^[[:space:]]+|[[:space:]]+$','')<>'' AND u.id IS NULL)
 OR EXISTS(SELECT 1 FROM vouchers v LEFT JOIN accounts a ON a.id=v.account_id
  LEFT JOIN users u ON u.id=a.owner_id
  WHERE REGEXP_REPLACE(v.platform,'^[[:space:]]+|[[:space:]]+$','')<>'' AND u.id IS NULL) THEN
  SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='Platform history has no valid account owner; review ownership before migration';
 END IF;
END;
CALL platform_catalog_preflight();
DROP PROCEDURE platform_catalog_preflight;

CREATE TABLE platform_catalog_before LIKE voucher_platforms;
INSERT INTO platform_catalog_before SELECT * FROM voucher_platforms;
CREATE TABLE platform_catalog_before_users AS SELECT id FROM users;
ALTER TABLE users ADD COLUMN voucher_platforms_initialized BOOLEAN NOT NULL DEFAULT FALSE;
CREATE TABLE voucher_platform_templates (
 name VARCHAR(128) COLLATE utf8mb4_bin NOT NULL PRIMARY KEY
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
INSERT INTO voucher_platform_templates VALUES
 ('支付宝'),('微信'),('淘宝'),('天猫'),('京东'),('拼多多'),('抖音'),('美团'),('饿了么');

ALTER TABLE voucher_platforms ADD COLUMN owner_id BIGINT NULL;
SET @platform_initial_owner=COALESCE((SELECT MIN(id) FROM users WHERE role='admin'),(SELECT MIN(id) FROM users));
-- Normalize only candidate names; never rewrite historical financial text.
CREATE TEMPORARY TABLE platform_catalog_normalized (
 id BIGINT PRIMARY KEY, name VARCHAR(128) COLLATE utf8mb4_bin NOT NULL,
 created_at DATETIME NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
INSERT INTO platform_catalog_normalized
SELECT normalized.id,normalized.name,original.created_at
FROM (
 SELECT MIN(id) AS id,MIN(REGEXP_REPLACE(name,'^[[:space:]]+|[[:space:]]+$','')) COLLATE utf8mb4_bin AS name
 FROM platform_catalog_before
 GROUP BY REGEXP_REPLACE(name,'^[[:space:]]+|[[:space:]]+$','') COLLATE utf8mb4_bin
) normalized JOIN platform_catalog_before original ON original.id=normalized.id;
DELETE FROM voucher_platforms;
INSERT INTO voucher_platforms(id,name,created_at,owner_id)
SELECT id,name,created_at,@platform_initial_owner FROM platform_catalog_normalized
WHERE @platform_initial_owner IS NOT NULL;
ALTER TABLE voucher_platforms DROP INDEX uk_voucher_platform_name,
 ADD UNIQUE KEY uk_voucher_platform_owner_name(owner_id,name);
INSERT INTO voucher_platforms(name,created_at,owner_id)
SELECT c.name,c.created_at,u.id FROM users u CROSS JOIN platform_catalog_normalized c
WHERE u.id<>@platform_initial_owner;
CREATE TABLE platform_catalog_mapping (
 source_id BIGINT NOT NULL, owner_id BIGINT NOT NULL, platform_id BIGINT NOT NULL,
 PRIMARY KEY(source_id,owner_id)
) ENGINE=InnoDB;
INSERT INTO platform_catalog_mapping
SELECT old.id,u.id,p.id FROM platform_catalog_before old CROSS JOIN users u
JOIN voucher_platforms p ON p.owner_id=u.id
AND p.name=REGEXP_REPLACE(old.name,'^[[:space:]]+|[[:space:]]+$','') COLLATE utf8mb4_bin;

INSERT INTO voucher_platforms(owner_id,name)
SELECT u.id,t.name FROM users u CROSS JOIN voucher_platform_templates t
WHERE NOT EXISTS(SELECT 1 FROM voucher_platforms p WHERE p.owner_id=u.id AND p.name=t.name);
INSERT INTO voucher_platforms(owner_id,name)
SELECT DISTINCT a.owner_id,REGEXP_REPLACE(t.platform,'^[[:space:]]+|[[:space:]]+$','') COLLATE utf8mb4_bin
FROM transactions t JOIN accounts a ON a.id=t.account_id
WHERE REGEXP_REPLACE(t.platform,'^[[:space:]]+|[[:space:]]+$','')<>''
ON DUPLICATE KEY UPDATE id=voucher_platforms.id;
INSERT INTO voucher_platforms(owner_id,name)
SELECT DISTINCT a.owner_id,REGEXP_REPLACE(v.platform,'^[[:space:]]+|[[:space:]]+$','') COLLATE utf8mb4_bin
FROM vouchers v JOIN accounts a ON a.id=v.account_id
WHERE REGEXP_REPLACE(v.platform,'^[[:space:]]+|[[:space:]]+$','')<>''
ON DUPLICATE KEY UPDATE id=voucher_platforms.id;
ALTER TABLE voucher_platforms MODIFY owner_id BIGINT NOT NULL,
 ADD CONSTRAINT fk_voucher_platform_owner FOREIGN KEY(owner_id) REFERENCES users(id) ON DELETE RESTRICT;
UPDATE users SET voucher_platforms_initialized=TRUE,updated_at=updated_at;
CREATE TABLE platform_catalog_after LIKE voucher_platforms;
INSERT INTO platform_catalog_after SELECT * FROM voucher_platforms;
CREATE TABLE platform_catalog_after_users AS SELECT id,voucher_platforms_initialized FROM users;
CREATE TABLE platform_catalog_after_templates LIKE voucher_platform_templates;
INSERT INTO platform_catalog_after_templates SELECT * FROM voucher_platform_templates;
DROP TEMPORARY TABLE platform_catalog_normalized;

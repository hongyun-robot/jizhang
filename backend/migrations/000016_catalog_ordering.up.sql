-- Apply after 000015 with application writes stopped. Only directory metadata changes.
ALTER TABLE voucher_platforms ADD COLUMN sort INT NOT NULL DEFAULT 0;
CREATE TEMPORARY TABLE catalog_ordering_positions AS
SELECT id, ROW_NUMBER() OVER (PARTITION BY owner_id ORDER BY BINARY name, id) - 1 AS position
FROM voucher_platforms;
UPDATE voucher_platforms p JOIN catalog_ordering_positions r ON r.id = p.id
SET p.sort = r.position;
DROP TEMPORARY TABLE catalog_ordering_positions;
CREATE INDEX idx_voucher_platform_owner_sort ON voucher_platforms(owner_id, sort, id);
CREATE TABLE catalog_order_requests (
 id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
 owner_id BIGINT NOT NULL,
 scope VARCHAR(64) COLLATE utf8mb4_bin NOT NULL,
 request_key VARCHAR(128) COLLATE utf8mb4_bin NOT NULL,
 digest CHAR(64) NOT NULL,
 created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
 UNIQUE KEY uk_catalog_order_request(owner_id,scope,request_key)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

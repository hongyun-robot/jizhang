CREATE TABLE voucher_platforms (
    id BIGINT NOT NULL AUTO_INCREMENT,
    name VARCHAR(128) COLLATE utf8mb4_bin NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    UNIQUE KEY uk_voucher_platform_name (name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

INSERT INTO voucher_platforms (name)
SELECT DISTINCT platform COLLATE utf8mb4_bin
FROM vouchers
WHERE deleted_at IS NULL AND platform <> '';

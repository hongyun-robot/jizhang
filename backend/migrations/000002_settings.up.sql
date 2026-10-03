-- 000002_settings.up.sql — 键值设置表（结算日等全局配置）

CREATE TABLE settings (
    `key`      VARCHAR(64)  NOT NULL,
    `value`    VARCHAR(255) NOT NULL,
    updated_at DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (`key`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

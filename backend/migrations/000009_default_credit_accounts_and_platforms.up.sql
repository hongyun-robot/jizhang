-- An empty database needs the original accounts too: this migration runs
-- before application bootstrap, which skips account seeding for nonempty books.
INSERT INTO accounts (name, type, opening_balance, repayment_day, status, sort, liability_type, billing_enabled)
SELECT defaults.name, defaults.type, 0, defaults.repayment_day, 'active', defaults.sort, defaults.liability_type, FALSE
FROM (
    SELECT '现金' AS name, 'asset' AS type, NULL AS repayment_day, 0 AS sort, '' AS liability_type
    UNION ALL SELECT '微信', 'asset', NULL, 1, ''
    UNION ALL SELECT '支付宝', 'asset', NULL, 2, ''
    UNION ALL SELECT '银行卡', 'asset', NULL, 3, ''
    UNION ALL SELECT '信用卡', 'liability', 10, 4, 'credit_card'
    UNION ALL SELECT '花呗', 'liability', NULL, 5, 'huabei'
    UNION ALL SELECT '京东白条', 'liability', NULL, 6, 'jd_baitiao'
    UNION ALL SELECT '抖音月付', 'liability', NULL, 7, 'douyin_monthly'
    UNION ALL SELECT '美团月付', 'liability', NULL, 8, 'meituan_monthly'
) AS defaults
WHERE NOT EXISTS (SELECT 1 FROM accounts);

-- Existing books receive only the four requested new defaults. Preserve any
-- existing same-name account, including its type, status, balance and settings.
INSERT INTO accounts (name, type, opening_balance, status, sort, liability_type, billing_enabled)
SELECT defaults.name, 'liability', 0, 'active', defaults.sort, defaults.liability_type, FALSE
FROM (
    SELECT '花呗' AS name, 5 AS sort, 'huabei' AS liability_type
    UNION ALL SELECT '京东白条', 6, 'jd_baitiao'
    UNION ALL SELECT '抖音月付', 7, 'douyin_monthly'
    UNION ALL SELECT '美团月付', 8, 'meituan_monthly'
) AS defaults
WHERE NOT EXISTS (SELECT 1 FROM accounts a WHERE a.name = defaults.name);

-- Shared by ordinary transactions and vouchers; exact existing names are reused.
INSERT INTO voucher_platforms (name)
VALUES ('支付宝'), ('微信'), ('淘宝'), ('天猫'), ('京东'), ('拼多多'), ('抖音'), ('美团'), ('饿了么')
ON DUPLICATE KEY UPDATE name = voucher_platforms.name;

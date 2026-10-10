-- Runtime consumer-credit types and labels are data, independent of Go enums.
CREATE TABLE IF NOT EXISTS liability_account_types (
    code VARCHAR(32) COLLATE utf8mb4_bin NOT NULL,
    name VARCHAR(64) NOT NULL,
    sort INT NOT NULL DEFAULT 0,
    PRIMARY KEY (code)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

INSERT INTO liability_account_types (code, name, sort)
VALUES ('credit_card', '信用卡', 0), ('huabei', '花呗', 1),
       ('jd_baitiao', '京东白条', 2), ('douyin_monthly', '抖音月付', 3),
       ('meituan_monthly', '美团月付', 4), ('other_consumer', '其他消费信用', 5)
ON DUPLICATE KEY UPDATE code = liability_account_types.code;

-- Default categories move out of startup code. Initialize only an empty
-- category tree; a populated or customized tree retains all identities.
SET @jizhang_initialize_categories := NOT EXISTS (SELECT 1 FROM categories);

INSERT INTO categories (kind, name, sort)
SELECT defaults.kind, defaults.name, defaults.sort
FROM (
    SELECT 'expense' AS kind, '餐饮' AS name, 0 AS sort
    UNION ALL SELECT 'expense', '交通', 1
    UNION ALL SELECT 'expense', '购物', 2
    UNION ALL SELECT 'expense', '居家', 3
    UNION ALL SELECT 'expense', '娱乐', 4
    UNION ALL SELECT 'expense', '医疗', 5
    UNION ALL SELECT 'expense', '人情', 6
    UNION ALL SELECT 'expense', '其他', 7
    UNION ALL SELECT 'income', '工资', 0
    UNION ALL SELECT 'income', '理财', 1
    UNION ALL SELECT 'income', '其他', 2
) AS defaults
WHERE @jizhang_initialize_categories;

INSERT INTO categories (parent_id, kind, name, sort)
SELECT parent.id, child.kind, child.name, child.sort
FROM (
    SELECT 'expense' AS kind, '餐饮' AS parent_name, '早餐' AS name, 0 AS sort
    UNION ALL SELECT 'expense', '餐饮', '午餐', 1
    UNION ALL SELECT 'expense', '餐饮', '晚餐', 2
    UNION ALL SELECT 'expense', '餐饮', '外卖', 3
    UNION ALL SELECT 'expense', '餐饮', '买菜', 4
    UNION ALL SELECT 'expense', '交通', '公交地铁', 0
    UNION ALL SELECT 'expense', '交通', '打车', 1
    UNION ALL SELECT 'expense', '交通', '加油', 2
    UNION ALL SELECT 'expense', '购物', '日用', 0
    UNION ALL SELECT 'expense', '购物', '服饰', 1
    UNION ALL SELECT 'expense', '购物', '数码', 2
    UNION ALL SELECT 'expense', '居家', '水电燃气', 0
    UNION ALL SELECT 'expense', '居家', '房租', 1
    UNION ALL SELECT 'expense', '居家', '物业', 2
    UNION ALL SELECT 'expense', '娱乐', '电影', 0
    UNION ALL SELECT 'expense', '娱乐', '游戏', 1
    UNION ALL SELECT 'expense', '娱乐', '旅行', 2
    UNION ALL SELECT 'expense', '医疗', '门诊', 0
    UNION ALL SELECT 'expense', '医疗', '药品', 1
    UNION ALL SELECT 'expense', '人情', '红包', 0
    UNION ALL SELECT 'expense', '人情', '礼物', 1
    UNION ALL SELECT 'income', '工资', '月薪', 0
    UNION ALL SELECT 'income', '工资', '奖金', 1
    UNION ALL SELECT 'income', '理财', '利息', 0
    UNION ALL SELECT 'income', '理财', '分红', 1
) AS child
JOIN categories AS parent ON parent.kind = child.kind AND parent.name = child.parent_name AND parent.parent_id IS NULL
WHERE @jizhang_initialize_categories;

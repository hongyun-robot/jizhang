ALTER TABLE accounts ADD COLUMN owner_id BIGINT NULL,
    ADD KEY idx_account_owner (owner_id, type, status),
    ADD CONSTRAINT fk_account_owner FOREIGN KEY (owner_id) REFERENCES users(id) ON DELETE RESTRICT;
ALTER TABLE users ADD COLUMN liability_accounts_initialized BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE liability_account_types ADD COLUMN default_account BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE notifications ADD COLUMN owner_id BIGINT NULL,
    ADD KEY idx_notification_owner (owner_id, kind),
    ADD CONSTRAINT fk_notification_owner FOREIGN KEY (owner_id) REFERENCES users(id) ON DELETE RESTRICT;
UPDATE liability_account_types SET default_account = TRUE
WHERE code IN ('credit_card','huabei','jd_baitiao','douyin_monthly','meituan_monthly');

-- Retain existing account identities and money. A single historical creator
-- owns their account; empty/shared legacy accounts remain with the initial admin.
UPDATE accounts a
LEFT JOIN (
    SELECT aid, MIN(created_by) AS creator, COUNT(DISTINCT created_by) AS creators
    FROM (
        SELECT account_id AS aid, created_by FROM transactions
        UNION ALL
        SELECT transfer_account_id AS aid, created_by FROM transactions WHERE transfer_account_id IS NOT NULL
    ) AS sources GROUP BY aid
) AS history ON history.aid = a.id
SET a.owner_id = CASE WHEN history.creators = 1 THEN history.creator ELSE
    COALESCE((SELECT MIN(id) FROM users WHERE role = 'admin'), (SELECT MIN(id) FROM users)) END
WHERE a.type = 'liability';
UPDATE accounts a JOIN liability_account_types t ON t.name = a.name AND t.default_account = TRUE
SET a.liability_type = t.code WHERE a.type = 'liability' AND a.liability_type = '';
UPDATE notifications n
LEFT JOIN liability_statements s ON s.id = n.statement_id
LEFT JOIN accounts a ON a.id = COALESCE(s.account_id, CAST(JSON_UNQUOTE(JSON_EXTRACT(n.payload, '$.account_id')) AS UNSIGNED))
SET n.owner_id = a.owner_id WHERE n.kind = 'repayment_due';

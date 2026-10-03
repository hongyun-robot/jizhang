ALTER TABLE transactions
    DROP KEY uk_txn_rule_date,
    ADD UNIQUE KEY uk_txn_rule_period (rule_id, period_key);

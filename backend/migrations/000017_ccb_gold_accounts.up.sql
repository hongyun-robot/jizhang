ALTER TABLE accounts DROP CHECK ck_account_asset_class;
ALTER TABLE accounts ADD CONSTRAINT ck_account_asset_class CHECK
 ((type='asset' AND (asset_class IS NULL OR asset_class IN ('','cash','fund','gold')))
 OR (type='liability' AND (asset_class IS NULL OR asset_class='')));
CREATE TABLE gold_positions (
 id BIGINT PRIMARY KEY AUTO_INCREMENT, owner_id BIGINT NOT NULL, account_id BIGINT NOT NULL,
 product_key VARCHAR(64) NOT NULL, event_version BIGINT NOT NULL DEFAULT 1, rebuild_pending BOOLEAN NOT NULL DEFAULT TRUE,
 created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3), updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
 UNIQUE KEY uk_gold_account(account_id), KEY idx_gold_owner(owner_id), KEY idx_gold_pending(rebuild_pending),
 CONSTRAINT fk_gold_position_owner FOREIGN KEY(account_id,owner_id) REFERENCES accounts(id,owner_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE gold_events (
 id BIGINT PRIMARY KEY AUTO_INCREMENT, owner_id BIGINT NOT NULL, account_id BIGINT NOT NULL, kind VARCHAR(16) NOT NULL,
 grams DECIMAL(24,8) NOT NULL, cash_amount DECIMAL(36,16) NOT NULL, fee DECIMAL(12,2) NULL,
 trade_date DATE NOT NULL, cash_account_id BIGINT NULL, note VARCHAR(255) NOT NULL DEFAULT '', created_by BIGINT NOT NULL,
 voided_at DATETIME(3) NULL, created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
 KEY idx_gold_event_account(account_id,trade_date,id), KEY idx_gold_event_owner(owner_id),
 CONSTRAINT fk_gold_event_owner FOREIGN KEY(account_id,owner_id) REFERENCES accounts(id,owner_id),
 CONSTRAINT fk_gold_cash_owner FOREIGN KEY(cash_account_id,owner_id) REFERENCES accounts(id,owner_id),
 CONSTRAINT ck_gold_event CHECK(kind IN ('opening','buy','sell') AND grams>0 AND cash_amount>=0 AND (fee IS NULL OR fee>=0))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE gold_mutations (
 id BIGINT PRIMARY KEY AUTO_INCREMENT, owner_id BIGINT NOT NULL, request_key VARCHAR(128) NOT NULL, payload_hash VARCHAR(64) NOT NULL,
 account_id BIGINT NOT NULL, event_id BIGINT NOT NULL, created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
 UNIQUE KEY uk_gold_mutation(owner_id,request_key)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE gold_audits (
 id BIGINT PRIMARY KEY AUTO_INCREMENT, owner_id BIGINT NOT NULL, account_id BIGINT NOT NULL, event_id BIGINT NOT NULL,
 actor_id BIGINT NOT NULL, action VARCHAR(32) NOT NULL, before_json LONGTEXT NOT NULL, after_json LONGTEXT NOT NULL,
 created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3), KEY idx_gold_audit_account(account_id), KEY idx_gold_audit_owner(owner_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE gold_quote_observations (
 id BIGINT PRIMARY KEY AUTO_INCREMENT, product_key VARCHAR(64) NOT NULL, buy_price DECIMAL(20,8) NOT NULL, sell_price DECIMAL(20,8) NOT NULL,
 quote_at DATETIME(3) NOT NULL, fetched_at DATETIME(3) NOT NULL, attempt_key VARCHAR(64) NOT NULL, revision_of BIGINT NULL,
 UNIQUE KEY uk_gold_quote_attempt(attempt_key), KEY idx_gold_observed(product_key,fetched_at,quote_at),
 CONSTRAINT ck_gold_quote CHECK(buy_price>0 AND sell_price>0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE gold_quote_syncs (
 product_key VARCHAR(64) PRIMARY KEY, observation_id BIGINT NULL, last_attempt_at DATETIME(3) NULL, last_success_at DATETIME(3) NULL,
 first_attempt_at DATETIME(3) NULL, scheduled_slot VARCHAR(16) NOT NULL DEFAULT '', lease_token VARCHAR(64) NOT NULL DEFAULT '',
 lease_until DATETIME(3) NULL, next_refresh_at DATETIME(3) NULL, last_error VARCHAR(255) NOT NULL DEFAULT '', source_unchanged BOOLEAN NOT NULL DEFAULT FALSE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE gold_valuations (
 id BIGINT PRIMARY KEY AUTO_INCREMENT, account_id BIGINT NOT NULL, date DATE NOT NULL, event_version BIGINT NOT NULL,
 observation_id BIGINT NULL, grams DECIMAL(24,8) NOT NULL, cost DECIMAL(36,16) NOT NULL, realized_pn_l DECIMAL(36,16) NOT NULL,
 market_value DECIMAL(12,2) NULL, status VARCHAR(32) NOT NULL, UNIQUE KEY uk_gold_valuation(account_id,date)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
ALTER TABLE transactions ADD COLUMN gold_event_id BIGINT NULL,
 ADD UNIQUE KEY uk_txn_gold_event(gold_event_id),
 ADD CONSTRAINT fk_txn_gold_event FOREIGN KEY(gold_event_id) REFERENCES gold_events(id),
 ADD CONSTRAINT ck_txn_gold_exclusive CHECK(gold_event_id IS NULL OR
 (fund_event_id IS NULL AND voucher_event_id IS NULL AND installment_plan_id IS NULL AND liability_operation_id IS NULL AND kind='transfer' AND purpose IN ('gold_purchase','gold_redemption')));

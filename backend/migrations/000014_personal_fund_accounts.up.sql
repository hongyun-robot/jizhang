-- Personal fund resources are created only by their owner, never seeded.
ALTER TABLE accounts ADD COLUMN asset_class VARCHAR(16) NULL,
 ADD UNIQUE KEY uk_account_id_owner(id,owner_id);
UPDATE accounts SET asset_class='cash' WHERE type='asset';
ALTER TABLE accounts ADD CONSTRAINT ck_account_asset_class CHECK
 ((type='asset' AND (asset_class IS NULL OR asset_class IN ('','cash','fund')))
 OR (type='liability' AND (asset_class IS NULL OR asset_class='')));

CREATE TABLE fund_instruments (
 id BIGINT PRIMARY KEY AUTO_INCREMENT, ths_code VARCHAR(32) NOT NULL,
 ticker VARCHAR(16) NOT NULL, name VARCHAR(128) NOT NULL DEFAULT '',
 asset_type VARCHAR(32) NOT NULL DEFAULT '', manager VARCHAR(128) NOT NULL DEFAULT '', management VARCHAR(128) NOT NULL DEFAULT '',
 sync_status VARCHAR(32) NOT NULL DEFAULT 'pending', last_error VARCHAR(255) NOT NULL DEFAULT '',
 last_attempt DATETIME NULL, next_attempt DATETIME NULL, lease_until DATETIME NULL,
 failure_count INT NOT NULL DEFAULT 0, history_loaded BOOLEAN NOT NULL DEFAULT FALSE,
 evening_completed_for DATE NULL, morning_attempted_for DATE NULL,
 created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP, updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
 UNIQUE KEY uk_fund_code(ths_code)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
CREATE TABLE fund_positions (
 id BIGINT PRIMARY KEY AUTO_INCREMENT, owner_id BIGINT NOT NULL, account_id BIGINT NOT NULL, instrument_id BIGINT NOT NULL,
 display_name VARCHAR(128) NOT NULL DEFAULT '', valuation_basis VARCHAR(32) NOT NULL,
 basis_confirmed_by BIGINT NOT NULL, basis_confirmed_at DATETIME NOT NULL,
 event_version BIGINT NOT NULL DEFAULT 1, rebuild_pending BOOLEAN NOT NULL DEFAULT TRUE,
 created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP, updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
 UNIQUE KEY uk_fund_position(account_id,instrument_id), UNIQUE KEY uk_fund_position_owner(id,owner_id,account_id),
 KEY idx_fund_position_owner(owner_id),
 CONSTRAINT fk_fund_position_account FOREIGN KEY(account_id,owner_id) REFERENCES accounts(id,owner_id),
 CONSTRAINT fk_fund_position_instrument FOREIGN KEY(instrument_id) REFERENCES fund_instruments(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
CREATE TABLE fund_events (
 id BIGINT PRIMARY KEY AUTO_INCREMENT, owner_id BIGINT NOT NULL, account_id BIGINT NOT NULL, position_id BIGINT NOT NULL,
 kind VARCHAR(24) NOT NULL, shares DECIMAL(24,8) NOT NULL DEFAULT 0,
 amount DECIMAL(36,16) NOT NULL DEFAULT 0, fee DECIMAL(12,2) NOT NULL DEFAULT 0, trade_nav DECIMAL(20,8) NOT NULL DEFAULT 0,
 effective_date DATE NOT NULL, cash_date DATE NULL, rights_date DATE NULL, registration_date DATE NULL,
 cash_account_id BIGINT NULL, source_event_id BIGINT NULL, settlement_key BIGINT NULL,
 corporate_key VARCHAR(128) NULL, rights_position_id BIGINT NOT NULL,
 note VARCHAR(255) NOT NULL DEFAULT '', created_by BIGINT NOT NULL, voided_at DATETIME NULL,
 created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
 UNIQUE KEY uk_fund_rights(rights_position_id,corporate_key), UNIQUE KEY uk_fund_settlement(settlement_key),
 KEY idx_fund_event_position(position_id,effective_date), UNIQUE KEY uk_fund_event_owner(id,owner_id,account_id),
 CONSTRAINT fk_fund_event_position FOREIGN KEY(position_id,owner_id,account_id) REFERENCES fund_positions(id,owner_id,account_id),
 CONSTRAINT fk_fund_event_cash FOREIGN KEY(cash_account_id,owner_id) REFERENCES accounts(id,owner_id),
 CONSTRAINT fk_fund_event_source FOREIGN KEY(source_event_id,owner_id,account_id) REFERENCES fund_events(id,owner_id,account_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
CREATE TABLE fund_mutations (
 id BIGINT PRIMARY KEY AUTO_INCREMENT, owner_id BIGINT NOT NULL, request_key VARCHAR(128) NOT NULL,
 payload_hash VARCHAR(64) NOT NULL, action VARCHAR(32) NOT NULL, position_id BIGINT NOT NULL DEFAULT 0, event_id BIGINT NOT NULL DEFAULT 0,
 created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
 UNIQUE KEY uk_fund_mutation(owner_id,request_key)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
CREATE TABLE fund_navs (
 id BIGINT PRIMARY KEY AUTO_INCREMENT, instrument_id BIGINT NOT NULL, nav_date DATE NOT NULL,
 unit_nav DECIMAL(20,8) NOT NULL, revision BIGINT NOT NULL DEFAULT 1, fetched_at DATETIME NOT NULL,
 UNIQUE KEY uk_fund_nav(instrument_id,nav_date),
 CONSTRAINT fk_fund_nav_instrument FOREIGN KEY(instrument_id) REFERENCES fund_instruments(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
CREATE TABLE fund_nav_revisions (
 id BIGINT PRIMARY KEY AUTO_INCREMENT, instrument_id BIGINT NOT NULL, nav_date DATE NOT NULL,
 unit_nav DECIMAL(20,8) NOT NULL, revision BIGINT NOT NULL, recorded_at DATETIME NOT NULL,
 KEY idx_fund_nav_revision(instrument_id,nav_date),
 CONSTRAINT fk_fund_nav_revision_instrument FOREIGN KEY(instrument_id) REFERENCES fund_instruments(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
CREATE TABLE fund_valuations (
 id BIGINT PRIMARY KEY AUTO_INCREMENT, position_id BIGINT NOT NULL, nav_date DATE NOT NULL, previous_nav_date DATE NULL,
 event_version BIGINT NOT NULL, quote_revision BIGINT NOT NULL, previous_quote_revision BIGINT NOT NULL DEFAULT 0,
 shares DECIMAL(24,8) NOT NULL, cost DECIMAL(36,16) NOT NULL, market_value DECIMAL(12,2) NOT NULL,
 daily_pn_l DECIMAL(12,2) NULL, status VARCHAR(32) NOT NULL, updated_at DATETIME NOT NULL,
 UNIQUE KEY uk_fund_valuation(position_id,nav_date),
 CONSTRAINT fk_fund_valuation_position FOREIGN KEY(position_id) REFERENCES fund_positions(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
CREATE TABLE fund_audits (
 id BIGINT PRIMARY KEY AUTO_INCREMENT, owner_id BIGINT NOT NULL, position_id BIGINT NOT NULL,
 event_id BIGINT NOT NULL DEFAULT 0, actor_id BIGINT NOT NULL, action VARCHAR(32) NOT NULL,
 before_json LONGTEXT NOT NULL, after_json LONGTEXT NOT NULL, created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
 KEY idx_fund_audit_position(position_id),
 CONSTRAINT fk_fund_audit_position FOREIGN KEY(position_id) REFERENCES fund_positions(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
CREATE TABLE fund_dividends (
 id BIGINT PRIMARY KEY AUTO_INCREMENT, instrument_id BIGINT NOT NULL, source_key VARCHAR(128) NOT NULL,
 registration_date DATE NOT NULL, ex_date DATE NOT NULL, payment_date DATE NULL, per_ten_cash DECIMAL(20,8) NOT NULL,
 UNIQUE KEY uk_fund_dividend(instrument_id,source_key),
 CONSTRAINT fk_fund_dividend_instrument FOREIGN KEY(instrument_id) REFERENCES fund_instruments(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
ALTER TABLE transactions ADD COLUMN fund_event_id BIGINT NULL,
 ADD UNIQUE KEY uk_txn_fund_event(fund_event_id),
 ADD CONSTRAINT fk_txn_fund_event FOREIGN KEY(fund_event_id) REFERENCES fund_events(id);

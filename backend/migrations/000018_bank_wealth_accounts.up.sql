-- Public metadata only. Admission is closed until real-time qualification passes.
ALTER TABLE accounts DROP CHECK ck_account_asset_class;
ALTER TABLE accounts ADD CONSTRAINT ck_account_asset_class CHECK
 ((type='asset' AND (asset_class IS NULL OR asset_class IN ('','cash','fund','gold','wealth')))
 OR (type='liability' AND (asset_class IS NULL OR asset_class='')));
CREATE TABLE wealth_products (
 id BIGINT PRIMARY KEY AUTO_INCREMENT, issuer VARCHAR(32) NOT NULL, registration_code VARCHAR(64) NOT NULL,
 code VARCHAR(32) NOT NULL, name VARCHAR(128) NOT NULL, currency CHAR(3) NOT NULL, valuation_basis VARCHAR(32) NOT NULL,
 inception_date DATE NOT NULL, source_adapter VARCHAR(32) NOT NULL, source_code VARCHAR(64) NOT NULL,
 admission_status VARCHAR(32) NOT NULL DEFAULT 'pending_validation', evidence_version VARCHAR(128) NOT NULL DEFAULT '',
 sync_status VARCHAR(32) NOT NULL DEFAULT 'pending', last_error VARCHAR(255) NOT NULL DEFAULT '',
 last_attempt DATETIME(3) NULL, last_success DATETIME(3) NULL, next_attempt DATETIME(3) NULL,
 lease_until DATETIME(3) NULL, lease_token VARCHAR(64) NOT NULL DEFAULT '', failure_count INT NOT NULL DEFAULT 0,
 created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3), updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
 UNIQUE KEY uk_wealth_identity(issuer,registration_code,code),
 CONSTRAINT ck_wealth_admission CHECK(admission_status IN ('pending_validation','enabled','retired') AND (admission_status<>'enabled' OR evidence_version<>'')),
 CONSTRAINT ck_wealth_currency CHECK(currency='CNY' AND valuation_basis='cny_wealth_unit_nav')
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
INSERT INTO wealth_products(issuer,registration_code,code,name,currency,valuation_basis,inception_date,source_adapter,source_code)
 VALUES('suyin','Z7003125000238','J07241','苏银理财恒源日申周赎1号A','CNY','cny_wealth_unit_nav','2025-08-29','suyin','Y0408Ux');
CREATE TABLE wealth_positions (
 id BIGINT PRIMARY KEY AUTO_INCREMENT, owner_id BIGINT NOT NULL, account_id BIGINT NOT NULL, product_id BIGINT NOT NULL,
 display_name VARCHAR(128) NOT NULL DEFAULT '', valuation_basis VARCHAR(32) NOT NULL,
 basis_confirmed_by BIGINT NOT NULL, basis_confirmed_at DATETIME NOT NULL,
 event_version BIGINT NOT NULL DEFAULT 1, rebuild_pending BOOLEAN NOT NULL DEFAULT TRUE,
 created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP, updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
 UNIQUE KEY uk_wealth_position(account_id,product_id), UNIQUE KEY uk_wealth_position_owner(id,owner_id,account_id),
 KEY idx_wealth_position_owner(owner_id), UNIQUE KEY uk_wealth_position_id_owner(id,owner_id),
 CONSTRAINT fk_wealth_position_account FOREIGN KEY(account_id,owner_id) REFERENCES accounts(id,owner_id),
 CONSTRAINT fk_wealth_position_product FOREIGN KEY(product_id) REFERENCES wealth_products(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
CREATE TABLE wealth_events (
 id BIGINT PRIMARY KEY AUTO_INCREMENT, owner_id BIGINT NOT NULL, account_id BIGINT NOT NULL, position_id BIGINT NOT NULL,
 kind VARCHAR(24) NOT NULL, shares DECIMAL(24,8) NOT NULL DEFAULT 0,
 amount DECIMAL(36,16) NOT NULL DEFAULT 0, fee DECIMAL(12,2) NOT NULL DEFAULT 0, trade_nav DECIMAL(20,8) NOT NULL DEFAULT 0,
 effective_date DATE NOT NULL, cash_date DATE NULL, rights_date DATE NULL, registration_date DATE NULL,
 cash_account_id BIGINT NULL, source_event_id BIGINT NULL, settlement_key BIGINT NULL,
 corporate_key VARCHAR(128) NULL, rights_position_id BIGINT NOT NULL,
 note VARCHAR(255) NOT NULL DEFAULT '', created_by BIGINT NOT NULL, voided_at DATETIME NULL,
 created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
 UNIQUE KEY uk_wealth_rights(rights_position_id,corporate_key), UNIQUE KEY uk_wealth_settlement(settlement_key),
 KEY idx_wealth_event_position(position_id,effective_date), UNIQUE KEY uk_wealth_event_owner(id,owner_id,account_id),
 CONSTRAINT fk_wealth_event_position FOREIGN KEY(position_id,owner_id,account_id) REFERENCES wealth_positions(id,owner_id,account_id),
 CONSTRAINT fk_wealth_event_cash FOREIGN KEY(cash_account_id,owner_id) REFERENCES accounts(id,owner_id),
 CONSTRAINT fk_wealth_event_source FOREIGN KEY(source_event_id,owner_id,account_id) REFERENCES wealth_events(id,owner_id,account_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
CREATE TABLE wealth_mutations (
 id BIGINT PRIMARY KEY AUTO_INCREMENT, owner_id BIGINT NOT NULL, request_key VARCHAR(128) NOT NULL,
 payload_hash VARCHAR(64) NOT NULL, action VARCHAR(32) NOT NULL, position_id BIGINT NOT NULL DEFAULT 0, event_id BIGINT NOT NULL DEFAULT 0, result_json LONGTEXT NOT NULL,
 created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
 UNIQUE KEY uk_wealth_mutation(owner_id,request_key)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
CREATE TABLE wealth_navs (
 id BIGINT PRIMARY KEY AUTO_INCREMENT, product_id BIGINT NOT NULL, nav_date DATE NOT NULL,
 unit_nav DECIMAL(20,8) NOT NULL, total_nav DECIMAL(20,8) NULL, revision BIGINT NOT NULL DEFAULT 1, fetched_at DATETIME NOT NULL,
 UNIQUE KEY uk_wealth_nav(product_id,nav_date),
 CONSTRAINT fk_wealth_nav_product FOREIGN KEY(product_id) REFERENCES wealth_products(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
CREATE TABLE wealth_nav_revisions (
 id BIGINT PRIMARY KEY AUTO_INCREMENT, product_id BIGINT NOT NULL, nav_date DATE NOT NULL,
 unit_nav DECIMAL(20,8) NOT NULL, total_nav DECIMAL(20,8) NULL, revision BIGINT NOT NULL, recorded_at DATETIME NOT NULL,
 KEY idx_wealth_nav_revision(product_id,nav_date),
 CONSTRAINT fk_wealth_nav_revision_product FOREIGN KEY(product_id) REFERENCES wealth_products(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
CREATE TABLE wealth_valuations (
 id BIGINT PRIMARY KEY AUTO_INCREMENT, position_id BIGINT NOT NULL, nav_date DATE NOT NULL, previous_nav_date DATE NULL,
 event_version BIGINT NOT NULL, series_version BIGINT NOT NULL, quote_source VARCHAR(16) NOT NULL, quote_record_id BIGINT NOT NULL, previous_quote_source VARCHAR(16) NOT NULL DEFAULT '', previous_quote_record_id BIGINT NOT NULL DEFAULT 0, quote_revision BIGINT NOT NULL, previous_quote_revision BIGINT NOT NULL DEFAULT 0,
 shares DECIMAL(24,8) NOT NULL, cost DECIMAL(36,16) NOT NULL, market_value DECIMAL(12,2) NOT NULL,
 daily_pn_l DECIMAL(12,2) NULL, status VARCHAR(32) NOT NULL, updated_at DATETIME NOT NULL,
 UNIQUE KEY uk_wealth_valuation(position_id,nav_date),
 CONSTRAINT fk_wealth_valuation_position FOREIGN KEY(position_id) REFERENCES wealth_positions(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
CREATE TABLE wealth_audits (
 id BIGINT PRIMARY KEY AUTO_INCREMENT, owner_id BIGINT NOT NULL, position_id BIGINT NOT NULL,
 event_id BIGINT NOT NULL DEFAULT 0, actor_id BIGINT NOT NULL, action VARCHAR(32) NOT NULL,
 before_json LONGTEXT NOT NULL, after_json LONGTEXT NOT NULL, created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
 KEY idx_wealth_audit_position(position_id),
 CONSTRAINT fk_wealth_audit_position FOREIGN KEY(position_id) REFERENCES wealth_positions(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE wealth_manual_navs (
 id BIGINT PRIMARY KEY AUTO_INCREMENT, owner_id BIGINT NOT NULL, product_id BIGINT NOT NULL, nav_date DATE NOT NULL,
 unit_nav DECIMAL(20,8) NOT NULL, revision BIGINT NOT NULL, source_note VARCHAR(255) NOT NULL, withdrawn BOOLEAN NOT NULL DEFAULT FALSE,
 created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3), updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
 UNIQUE KEY uk_wealth_manual(owner_id,product_id,nav_date), UNIQUE KEY uk_wealth_manual_owner(id,owner_id,product_id),
 CONSTRAINT fk_wealth_manual_owner FOREIGN KEY(owner_id) REFERENCES users(id),
 CONSTRAINT fk_wealth_manual_product FOREIGN KEY(product_id) REFERENCES wealth_products(id),
 CONSTRAINT ck_wealth_manual_price CHECK(unit_nav>0 AND revision>0 AND source_note<>'')
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
CREATE TABLE wealth_manual_audits (
 id BIGINT PRIMARY KEY AUTO_INCREMENT, owner_id BIGINT NOT NULL, product_id BIGINT NOT NULL, manual_nav_id BIGINT NOT NULL,
 actor_id BIGINT NOT NULL, action VARCHAR(32) NOT NULL, before_json LONGTEXT NOT NULL, after_json LONGTEXT NOT NULL,
 created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3), KEY idx_wealth_manual_audit(owner_id,manual_nav_id),
 CONSTRAINT fk_wealth_manual_audit FOREIGN KEY(manual_nav_id,owner_id,product_id) REFERENCES wealth_manual_navs(id,owner_id,product_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
CREATE TABLE wealth_series (
 owner_id BIGINT NOT NULL, product_id BIGINT NOT NULL, version BIGINT NOT NULL DEFAULT 1,
 PRIMARY KEY(owner_id,product_id),
 CONSTRAINT fk_wealth_series_owner FOREIGN KEY(owner_id) REFERENCES users(id),
 CONSTRAINT fk_wealth_series_product FOREIGN KEY(product_id) REFERENCES wealth_products(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
CREATE TABLE wealth_sync_slots (
 id BIGINT PRIMARY KEY AUTO_INCREMENT, product_id BIGINT NOT NULL, slot DATETIME(3) NOT NULL,
 status VARCHAR(24) NOT NULL, lease_token VARCHAR(64) NOT NULL DEFAULT '', completed_at DATETIME(3) NULL, merged_into DATETIME(3) NULL,
 created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3), UNIQUE KEY uk_wealth_slot(product_id,slot),
 CONSTRAINT fk_wealth_slot_product FOREIGN KEY(product_id) REFERENCES wealth_products(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
ALTER TABLE wealth_nav_revisions ADD UNIQUE KEY uk_wealth_nav_revision(product_id,nav_date,revision);
ALTER TABLE wealth_navs ADD CONSTRAINT ck_wealth_nav_price CHECK(unit_nav>0 AND (total_nav IS NULL OR total_nav>0));
ALTER TABLE wealth_positions ADD KEY idx_wealth_rebuild(rebuild_pending);
ALTER TABLE wealth_audits ADD CONSTRAINT fk_wealth_audit_owner FOREIGN KEY(position_id,owner_id) REFERENCES wealth_positions(id,owner_id);
ALTER TABLE transactions ADD COLUMN wealth_event_id BIGINT NULL,
 ADD UNIQUE KEY uk_txn_wealth_event(wealth_event_id),
 ADD CONSTRAINT fk_txn_wealth_event FOREIGN KEY(wealth_event_id) REFERENCES wealth_events(id),
 ADD CONSTRAINT ck_txn_wealth_exclusive CHECK(wealth_event_id IS NULL OR
 (fund_event_id IS NULL AND gold_event_id IS NULL AND voucher_event_id IS NULL AND installment_plan_id IS NULL AND liability_operation_id IS NULL AND kind='transfer' AND purpose IN ('wealth_purchase','wealth_redemption','wealth_dividend')));

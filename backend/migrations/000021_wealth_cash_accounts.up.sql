ALTER TABLE accounts DROP CHECK ck_account_asset_class;
ALTER TABLE accounts ADD CONSTRAINT ck_account_asset_class CHECK
 ((type='asset' AND (asset_class IS NULL OR asset_class IN ('','cash','fund','gold','wealth','wealth_cash')))
 OR (type='liability' AND (asset_class IS NULL OR asset_class='')));

CREATE TABLE wealth_cash_links (
 id BIGINT PRIMARY KEY AUTO_INCREMENT, owner_id BIGINT NOT NULL, account_id BIGINT NOT NULL,
 product_id BIGINT NOT NULL, policy VARCHAR(32) NOT NULL, start_date DATE NOT NULL, active BOOLEAN NOT NULL,
 revision BIGINT NOT NULL, sync_status VARCHAR(32) NOT NULL, calculation_hash CHAR(64) NOT NULL DEFAULT '', last_error VARCHAR(255) NOT NULL DEFAULT '',
 last_reconciled_at DATETIME(3) NULL, created_at DATETIME(3) NOT NULL, updated_at DATETIME(3) NOT NULL,
 UNIQUE KEY uk_cash_link_account(account_id), UNIQUE KEY uk_cash_link_owner(id,owner_id,account_id),
 CONSTRAINT fk_cash_link_account FOREIGN KEY(account_id,owner_id) REFERENCES accounts(id,owner_id),
 CONSTRAINT fk_cash_link_product FOREIGN KEY(product_id) REFERENCES wealth_products(id),
 CONSTRAINT ck_cash_link_revision CHECK(revision>0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE wealth_cash_flows (
 id BIGINT PRIMARY KEY AUTO_INCREMENT, txn_id BIGINT NOT NULL, account_id BIGINT NOT NULL, owner_id BIGINT NOT NULL,
 application_time CHAR(8) NOT NULL, transfer_mode VARCHAR(16) NOT NULL,
 confirmation_date DATE NULL, rights_end_date DATE NULL, created_at DATETIME(3) NOT NULL, updated_at DATETIME(3) NOT NULL,
 UNIQUE KEY uk_cash_flow(txn_id,account_id),
 CONSTRAINT fk_cash_flow_txn FOREIGN KEY(txn_id) REFERENCES transactions(id) ON DELETE CASCADE,
 CONSTRAINT fk_cash_flow_account FOREIGN KEY(account_id,owner_id) REFERENCES accounts(id,owner_id),
 CONSTRAINT ck_cash_flow_mode CHECK(transfer_mode IN ('quick','ordinary'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE wealth_cash_yields (
 id BIGINT PRIMARY KEY AUTO_INCREMENT, link_id BIGINT NOT NULL, owner_id BIGINT NOT NULL,
 account_id BIGINT NOT NULL, product_id BIGINT NOT NULL, nav_date DATE NOT NULL, previous_nav_date DATE NOT NULL,
 unit_nav DECIMAL(20,8) NOT NULL, previous_nav DECIMAL(20,8) NOT NULL,
 calculated_amount DECIMAL(12,2) NOT NULL, amount DECIMAL(12,2) NOT NULL,
 manual_override BOOLEAN NOT NULL DEFAULT FALSE, manual_start_date DATE NULL, txn_id BIGINT NULL, revision BIGINT NOT NULL,
 status VARCHAR(32) NOT NULL, note VARCHAR(255) NOT NULL DEFAULT '',
 created_at DATETIME(3) NOT NULL, updated_at DATETIME(3) NOT NULL,
 UNIQUE KEY uk_cash_yield(account_id,nav_date), UNIQUE KEY uk_cash_yield_txn(txn_id),
 UNIQUE KEY uk_cash_yield_owner(id,owner_id,account_id),
 CONSTRAINT fk_cash_yield_link FOREIGN KEY(link_id,owner_id,account_id) REFERENCES wealth_cash_links(id,owner_id,account_id),
 CONSTRAINT fk_cash_yield_product FOREIGN KEY(product_id) REFERENCES wealth_products(id),
 CONSTRAINT fk_cash_yield_txn FOREIGN KEY(txn_id) REFERENCES transactions(id),
 CONSTRAINT ck_cash_yield_nav CHECK(unit_nav>0 AND previous_nav>0 AND previous_nav_date<nav_date AND revision>0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE wealth_cash_audits (
 id BIGINT PRIMARY KEY AUTO_INCREMENT, owner_id BIGINT NOT NULL, link_id BIGINT NOT NULL, yield_id BIGINT NULL,
 action VARCHAR(32) NOT NULL, request_key VARCHAR(128) NULL, payload_hash CHAR(64) NOT NULL DEFAULT '',
 before_json LONGTEXT NOT NULL, after_json LONGTEXT NOT NULL, created_by BIGINT NOT NULL, created_at DATETIME(3) NOT NULL,
 UNIQUE KEY uk_cash_audit_request(owner_id,request_key), KEY idx_cash_audit_link(link_id),
 CONSTRAINT fk_cash_audit_link FOREIGN KEY(link_id) REFERENCES wealth_cash_links(id),
 CONSTRAINT fk_cash_audit_yield FOREIGN KEY(yield_id) REFERENCES wealth_cash_yields(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

ALTER TABLE transactions
 MODIFY source ENUM('manual','recurring','voucher_auto','liability_auto','wealth_auto') NOT NULL DEFAULT 'manual',
 ADD COLUMN wealth_cash_yield_id BIGINT NULL,
 ADD UNIQUE KEY uk_txn_cash_yield(wealth_cash_yield_id),
 ADD KEY idx_txn_cash_yield_owner(wealth_cash_yield_id,created_by,account_id),
 ADD CONSTRAINT fk_txn_cash_yield FOREIGN KEY(wealth_cash_yield_id,created_by,account_id) REFERENCES wealth_cash_yields(id,owner_id,account_id),
 ADD CONSTRAINT ck_txn_cash_yield CHECK
 ((wealth_cash_yield_id IS NULL AND purpose<>'wealth_cash_yield' AND source<>'wealth_auto') OR
  (wealth_cash_yield_id IS NOT NULL AND kind IN ('income','expense') AND purpose='wealth_cash_yield' AND source='wealth_auto'
   AND transfer_account_id IS NULL AND voucher_event_id IS NULL AND fund_event_id IS NULL AND gold_event_id IS NULL
   AND wealth_event_id IS NULL AND installment_plan_id IS NULL AND liability_operation_id IS NULL));

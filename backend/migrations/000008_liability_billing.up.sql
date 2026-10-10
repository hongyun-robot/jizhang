-- Consumer-credit billing; no history reconstruction or data reset.
ALTER TABLE accounts ADD COLUMN liability_type VARCHAR(32) NOT NULL DEFAULT '', ADD COLUMN billing_enabled BOOLEAN NOT NULL DEFAULT FALSE, MODIFY repayment_day TINYINT NULL COMMENT '还款日 1-31';
CREATE TABLE liability_bill_rules (
 id BIGINT NOT NULL AUTO_INCREMENT,
 account_id BIGINT NOT NULL,
 version INT NOT NULL,
 statement_day TINYINT NOT NULL,
 repayment_day TINYINT NOT NULL,
 repayment_month_offset TINYINT NOT NULL,
 effective_from DATE NOT NULL,
 created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
 PRIMARY KEY(id),
 UNIQUE KEY uk_bill_rule_version(account_id,version),
 KEY idx_rule_effective(account_id,effective_from),
 FOREIGN KEY(account_id) REFERENCES accounts(id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE liability_statements (
 id BIGINT NOT NULL AUTO_INCREMENT,
 account_id BIGINT NOT NULL,
 kind VARCHAR(32) NOT NULL DEFAULT 'monthly',
 bill_key VARCHAR(128) NOT NULL,
 rule_id BIGINT NOT NULL,
 cycle_start DATE NOT NULL,
 cycle_end DATE NOT NULL,
 due_date DATE NOT NULL,
 issued_at DATETIME NULL,
 snapshot_amount DECIMAL(12,2) NOT NULL DEFAULT 0,
 reference_amount DECIMAL(12,2) NULL,
 reconciled_at DATETIME NULL,
 created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
 PRIMARY KEY(id),
 UNIQUE KEY uk_statement_key(bill_key),
 KEY idx_statement_cycle(account_id,cycle_end),
 KEY idx_statement_due(due_date,account_id),
 FOREIGN KEY(account_id) REFERENCES accounts(id) ON DELETE RESTRICT,
 FOREIGN KEY(rule_id) REFERENCES liability_bill_rules(id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE installment_plans (
 id BIGINT NOT NULL AUTO_INCREMENT,
 source_txn_id BIGINT NOT NULL,
 account_id BIGINT NOT NULL,
 principal DECIMAL(12,2) NOT NULL,
 term_count INT NOT NULL,
 fee_mode VARCHAR(32) NOT NULL,
 fee_input DECIMAL(12,2) NOT NULL,
 fee_account_id BIGINT NOT NULL,
 fee_category_id BIGINT NOT NULL,
 created_by BIGINT NOT NULL,
 version INT NOT NULL,
 status VARCHAR(32) NOT NULL,
 created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
 updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
 PRIMARY KEY(id),
 UNIQUE KEY uk_plan_source(source_txn_id),
 KEY idx_plan_status(status),
 FOREIGN KEY(source_txn_id) REFERENCES transactions(id) ON DELETE RESTRICT,
 FOREIGN KEY(account_id) REFERENCES accounts(id) ON DELETE RESTRICT,
 FOREIGN KEY(fee_account_id) REFERENCES accounts(id) ON DELETE RESTRICT,
 FOREIGN KEY(fee_category_id) REFERENCES categories(id) ON DELETE RESTRICT,
 FOREIGN KEY(created_by) REFERENCES users(id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE installment_terms (
 id BIGINT NOT NULL AUTO_INCREMENT,
 plan_id BIGINT NOT NULL,
 number INT NOT NULL,
 statement_id BIGINT NOT NULL,
 principal DECIMAL(12,2) NOT NULL,
 fee_amount DECIMAL(12,2) NOT NULL,
 fee_date DATE NOT NULL,
 fee_txn_id BIGINT NULL,
 status VARCHAR(32) NOT NULL,
 fee_status VARCHAR(32) NOT NULL,
 blocked_reason VARCHAR(255) NOT NULL DEFAULT '',
 PRIMARY KEY(id),
 UNIQUE KEY uk_installment_term(plan_id,number),
 UNIQUE KEY uk_term_fee(fee_txn_id),
 KEY idx_fee_due(fee_status,fee_date),
 FOREIGN KEY(plan_id) REFERENCES installment_plans(id) ON DELETE RESTRICT,
 FOREIGN KEY(statement_id) REFERENCES liability_statements(id) ON DELETE RESTRICT,
 FOREIGN KEY(fee_txn_id) REFERENCES transactions(id) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE liability_operations (
 id BIGINT NOT NULL AUTO_INCREMENT,
 actor_id BIGINT NOT NULL,
 request_key VARCHAR(128) NOT NULL,
 digest VARCHAR(64) NOT NULL DEFAULT '',
 kind VARCHAR(32) NOT NULL,
 account_id BIGINT NOT NULL,
 plan_id BIGINT NULL,
 reason VARCHAR(255) NOT NULL DEFAULT '',
 before_json LONGTEXT NOT NULL,
 result_json LONGTEXT NOT NULL,
 reversed_at DATETIME NULL,
 created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
 PRIMARY KEY(id),
 UNIQUE KEY uk_liability_request(actor_id,request_key),
 KEY idx_operation_account(account_id,id),
 FOREIGN KEY(account_id) REFERENCES accounts(id) ON DELETE RESTRICT,
 FOREIGN KEY(plan_id) REFERENCES installment_plans(id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE liability_debt_items (
 id BIGINT NOT NULL AUTO_INCREMENT,
 account_id BIGINT NOT NULL,
 source_txn_id BIGINT NULL,
 source_id BIGINT NOT NULL,
 item_key VARCHAR(128) NOT NULL,
 kind VARCHAR(32) NOT NULL,
 statement_id BIGINT NOT NULL,
 plan_id BIGINT NULL,
 term_id BIGINT NULL,
 amount DECIMAL(12,2) NOT NULL,
 active BOOLEAN NOT NULL,
 manual_assignment BOOLEAN NOT NULL DEFAULT FALSE,
 created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
 updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
 PRIMARY KEY(id),
 UNIQUE KEY uk_debt_item(item_key),
 KEY idx_debt_account(account_id,active,statement_id),
 FOREIGN KEY(account_id) REFERENCES accounts(id) ON DELETE RESTRICT,
 FOREIGN KEY(source_txn_id) REFERENCES transactions(id) ON DELETE SET NULL,
 FOREIGN KEY(statement_id) REFERENCES liability_statements(id) ON DELETE RESTRICT,
 FOREIGN KEY(plan_id) REFERENCES installment_plans(id) ON DELETE RESTRICT,
 FOREIGN KEY(term_id) REFERENCES installment_terms(id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE liability_credit_events (
 id BIGINT NOT NULL AUTO_INCREMENT,
 account_id BIGINT NOT NULL,
 source_txn_id BIGINT NULL,
 source_id BIGINT NOT NULL,
 amount DECIMAL(12,2) NOT NULL,
 entry_date DATE NOT NULL,
 related_source_id BIGINT NOT NULL DEFAULT 0,
 mode VARCHAR(32) NOT NULL DEFAULT 'auto',
 targets TEXT NOT NULL,
 active BOOLEAN NOT NULL,
 created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
 PRIMARY KEY(id),
 UNIQUE KEY uk_credit_source(account_id,source_id),
 KEY idx_credit_account(account_id,active),
 FOREIGN KEY(account_id) REFERENCES accounts(id) ON DELETE RESTRICT,
 FOREIGN KEY(source_txn_id) REFERENCES transactions(id) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE liability_allocations (
 id BIGINT NOT NULL AUTO_INCREMENT,
 credit_event_id BIGINT NOT NULL,
 debt_item_id BIGINT NOT NULL,
 amount DECIMAL(12,2) NOT NULL,
 active BOOLEAN NOT NULL,
 created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
 voided_at DATETIME NULL,
 PRIMARY KEY(id),
 KEY idx_allocation_credit(credit_event_id,active),
 KEY idx_allocation_debt(debt_item_id,active),
 FOREIGN KEY(credit_event_id) REFERENCES liability_credit_events(id) ON DELETE RESTRICT,
 FOREIGN KEY(debt_item_id) REFERENCES liability_debt_items(id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

ALTER TABLE transactions MODIFY source ENUM('manual','recurring','voucher_auto','liability_auto') NOT NULL DEFAULT 'manual', ADD COLUMN installment_plan_id BIGINT NULL, ADD COLUMN liability_term_id BIGINT NULL, ADD COLUMN liability_operation_id BIGINT NULL, ADD KEY idx_txn_plan(installment_plan_id), ADD CONSTRAINT fk_txn_installment_plan FOREIGN KEY(installment_plan_id) REFERENCES installment_plans(id) ON DELETE RESTRICT, ADD CONSTRAINT fk_txn_liability_term FOREIGN KEY(liability_term_id) REFERENCES installment_terms(id) ON DELETE RESTRICT, ADD CONSTRAINT fk_txn_liability_operation FOREIGN KEY(liability_operation_id) REFERENCES liability_operations(id) ON DELETE RESTRICT;
ALTER TABLE notifications ADD COLUMN statement_id BIGINT NULL, ADD COLUMN dedup_key VARCHAR(128) NULL, ADD COLUMN resolved_at DATETIME NULL, ADD UNIQUE KEY uk_notification_dedup(dedup_key), ADD CONSTRAINT fk_notification_statement FOREIGN KEY(statement_id) REFERENCES liability_statements(id) ON DELETE RESTRICT;

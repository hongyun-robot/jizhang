-- 000001_init.up.sql — 9 张核心表

CREATE TABLE users (
    id            BIGINT       NOT NULL AUTO_INCREMENT,
    username      VARCHAR(64)  NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    role          ENUM('admin','recorder','member','viewer') NOT NULL DEFAULT 'viewer',
    status        ENUM('active','disabled') NOT NULL DEFAULT 'active',
    created_at    DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at    DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    UNIQUE KEY uk_users_username (username)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE accounts (
    id              BIGINT       NOT NULL AUTO_INCREMENT,
    name            VARCHAR(64)  NOT NULL,
    type            ENUM('asset','liability') NOT NULL DEFAULT 'asset',
    opening_balance DECIMAL(12,2) NOT NULL DEFAULT 0.00,
    repayment_day   TINYINT      NULL COMMENT '信用卡还款日 1-28',
    status          ENUM('active','disabled') NOT NULL DEFAULT 'active',
    sort            INT          NOT NULL DEFAULT 0,
    created_at      DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    KEY idx_accounts_status_sort (status, sort)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE categories (
    id        BIGINT      NOT NULL AUTO_INCREMENT,
    parent_id BIGINT      NULL,
    kind      ENUM('income','expense') NOT NULL,
    name      VARCHAR(64) NOT NULL,
    sort      INT         NOT NULL DEFAULT 0,
    created_at DATETIME   NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME   NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    KEY idx_categories_parent (parent_id),
    KEY idx_categories_kind (kind),
    CONSTRAINT fk_categories_parent FOREIGN KEY (parent_id) REFERENCES categories (id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE transactions (
    id                  BIGINT        NOT NULL AUTO_INCREMENT,
    kind                ENUM('expense','income','transfer') NOT NULL,
    amount              DECIMAL(12,2) NOT NULL,
    account_id          BIGINT        NOT NULL,
    transfer_account_id BIGINT        NULL COMMENT '转账对方账户',
    category_id         BIGINT        NULL COMMENT '转账无分类',
    entry_date          DATE          NOT NULL,
    period_key          CHAR(7)       NOT NULL COMMENT '归期物化 YYYY-MM',
    note                VARCHAR(255)  NULL,
    created_by          BIGINT        NOT NULL,
    source              ENUM('manual','recurring') NOT NULL DEFAULT 'manual',
    rule_id             BIGINT        NULL COMMENT '周期模板来源，幂等键',
    created_at          DATETIME      NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at          DATETIME      NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    UNIQUE KEY uk_txn_rule_period (rule_id, period_key),
    KEY idx_txn_entry_period (entry_date, period_key),
    KEY idx_txn_account (account_id),
    KEY idx_txn_category (category_id),
    KEY idx_txn_created_by (created_by),
    CONSTRAINT fk_txn_account FOREIGN KEY (account_id) REFERENCES accounts (id) ON DELETE RESTRICT,
    CONSTRAINT fk_txn_transfer_account FOREIGN KEY (transfer_account_id) REFERENCES accounts (id) ON DELETE RESTRICT,
    CONSTRAINT fk_txn_category FOREIGN KEY (category_id) REFERENCES categories (id) ON DELETE RESTRICT,
    CONSTRAINT fk_txn_creator FOREIGN KEY (created_by) REFERENCES users (id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE budgets (
    id          BIGINT        NOT NULL AUTO_INCREMENT,
    period_key  CHAR(7)       NOT NULL,
    category_id BIGINT        NULL COMMENT 'NULL = 总预算',
    amount      DECIMAL(12,2) NOT NULL,
    created_at  DATETIME      NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME      NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    UNIQUE KEY uk_budget_period_category (period_key, category_id),
    KEY idx_budget_period (period_key),
    CONSTRAINT fk_budget_category FOREIGN KEY (category_id) REFERENCES categories (id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE recurring_rules (
    id             BIGINT        NOT NULL AUTO_INCREMENT,
    amount         DECIMAL(12,2) NOT NULL,
    freq           ENUM('daily','weekly','monthly','yearly') NOT NULL,
    account_id     BIGINT        NOT NULL,
    category_id    BIGINT        NOT NULL,
    note           VARCHAR(255)  NULL,
    start_date     DATE          NOT NULL,
    next_run_date  DATE          NOT NULL,
    status         ENUM('enabled','paused') NOT NULL DEFAULT 'enabled',
    created_by     BIGINT        NOT NULL,
    created_at     DATETIME      NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at     DATETIME      NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    KEY idx_recurring_next (status, next_run_date),
    CONSTRAINT fk_recurring_account FOREIGN KEY (account_id) REFERENCES accounts (id) ON DELETE RESTRICT,
    CONSTRAINT fk_recurring_category FOREIGN KEY (category_id) REFERENCES categories (id) ON DELETE RESTRICT,
    CONSTRAINT fk_recurring_creator FOREIGN KEY (created_by) REFERENCES users (id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE period_closings (
    id         BIGINT      NOT NULL AUTO_INCREMENT,
    period_key CHAR(7)     NOT NULL,
    status     ENUM('closed') NOT NULL DEFAULT 'closed',
    closed_by  BIGINT      NOT NULL,
    closed_at  DATETIME    NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    UNIQUE KEY uk_closing_period (period_key),
    CONSTRAINT fk_closing_user FOREIGN KEY (closed_by) REFERENCES users (id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE notifications (
    id         BIGINT   NOT NULL AUTO_INCREMENT,
    kind       ENUM('budget_overdue','repayment_due','recurring_patched') NOT NULL,
    payload    JSON     NOT NULL,
    read_at    DATETIME NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    KEY idx_notif_read (read_at, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE audit_logs (
    id         BIGINT       NOT NULL AUTO_INCREMENT,
    user_id    BIGINT       NOT NULL,
    action     VARCHAR(32)  NOT NULL,
    detail     JSON         NULL,
    created_at DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    KEY idx_audit_created (created_at),
    CONSTRAINT fk_audit_user FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

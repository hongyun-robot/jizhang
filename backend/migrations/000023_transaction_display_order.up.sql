-- Presentation preferences only; existing journal rows and balances are untouched.
CREATE TABLE transaction_display_orders (
 owner_id BIGINT NOT NULL,
 transaction_id BIGINT NOT NULL,
 position BIGINT NOT NULL,
 PRIMARY KEY(owner_id, transaction_id),
 KEY idx_transaction_display_position(owner_id, position),
 CONSTRAINT fk_transaction_display_owner FOREIGN KEY(owner_id) REFERENCES users(id) ON DELETE CASCADE,
 CONSTRAINT fk_transaction_display_txn FOREIGN KEY(transaction_id) REFERENCES transactions(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

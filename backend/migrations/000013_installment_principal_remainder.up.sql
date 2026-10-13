ALTER TABLE accounts
    ADD COLUMN principal_remainder VARCHAR(8) NOT NULL DEFAULT 'last';

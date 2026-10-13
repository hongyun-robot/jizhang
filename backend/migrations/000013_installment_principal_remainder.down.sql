-- Existing installment amounts are already stored and remain unchanged.
-- Rolling back drops the preference; new plans use the legacy last-term rule.
ALTER TABLE accounts DROP COLUMN principal_remainder;

-- This is an additive data migration with no schema change. Retain accounts
-- and catalog entries on rollback: their identities may already be referenced
-- or customized by users, and name-based deletion cannot identify seed owners.
SELECT 1;

-- Read-only migration report. Run against the upgraded, write-paused ledger.
SELECT o.account_id,a.name,o.previous_owner_id,o.owner_id,o.component_id,o.reason
FROM catalog_account_ownership o JOIN accounts a ON a.id=o.account_id ORDER BY o.account_id;

SELECT original.id,original.name,original.opening_balance+COALESCE(old_net.net,0) AS previous_balance,
 current.opening_balance+COALESCE(new_net.net,0) AS current_balance,
 (current.opening_balance+COALESCE(new_net.net,0))-(original.opening_balance+COALESCE(old_net.net,0)) AS balance_difference
FROM catalog_before_accounts original JOIN accounts current ON current.id=original.id
LEFT JOIN (
 SELECT aid,SUM(amount) net FROM (
  SELECT account_id aid,CASE WHEN kind IN ('income','refund') THEN amount WHEN kind='transfer' THEN -amount WHEN kind='expense' AND purpose NOT IN ('voucher_redemption','voucher_writeoff') THEN -amount ELSE 0 END amount FROM catalog_before_transactions
  UNION ALL SELECT transfer_account_id,amount FROM catalog_before_transactions WHERE kind='transfer' AND transfer_account_id IS NOT NULL
 ) funds GROUP BY aid
) old_net ON old_net.aid=original.id
LEFT JOIN (
 SELECT aid,SUM(amount) net FROM (
  SELECT account_id aid,CASE WHEN kind IN ('income','refund') THEN amount WHEN kind='transfer' THEN -amount WHEN kind='expense' AND purpose NOT IN ('voucher_redemption','voucher_writeoff') THEN -amount ELSE 0 END amount FROM transactions
  UNION ALL SELECT transfer_account_id,amount FROM transactions WHERE kind='transfer' AND transfer_account_id IS NOT NULL
 ) funds GROUP BY aid
) new_net ON new_net.aid=original.id ORDER BY original.id;

SELECT a.id,a.name,a.owner_id,a.opening_balance FROM accounts a LEFT JOIN catalog_before_accounts old ON old.id=a.id WHERE old.id IS NULL;
SELECT c.id,c.name,c.owner_id,p.owner_id AS parent_owner FROM categories c JOIN categories p ON p.id=c.parent_id WHERE c.owner_id<>p.owner_id OR c.kind<>p.kind;
SELECT id,created_by,account_id,blocked_reason FROM recurring_rules WHERE blocked_reason<>'';
SELECT id,plan_id,blocked_reason FROM installment_terms WHERE blocked_reason<>'';

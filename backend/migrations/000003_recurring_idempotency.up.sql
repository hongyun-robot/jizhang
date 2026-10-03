-- 周期幂等键从 (rule_id, period_key) 收敛到 (rule_id, entry_date)（任务 9.2）：
-- period_key 粒度会让每日/每周模板在同一账期只能生成一笔，与四种频率语义冲突；
-- 「同一模板同一期只生成一笔」按发生时点理解——月/周/年模板每期归属日唯一，
-- 每日模板每日归属日唯一；cron 与懒补齐的重复触发同样由该约束兜底。
ALTER TABLE transactions
    DROP KEY uk_txn_rule_period,
    ADD UNIQUE KEY uk_txn_rule_date (rule_id, entry_date);

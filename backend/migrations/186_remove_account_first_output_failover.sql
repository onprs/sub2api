ALTER TABLE accounts
    DROP CONSTRAINT IF EXISTS accounts_first_output_failover_cooldown_valid,
    DROP CONSTRAINT IF EXISTS accounts_first_output_failover_timeout_valid,
    DROP COLUMN IF EXISTS first_output_failover_cooldown_minutes,
    DROP COLUMN IF EXISTS first_output_failover_timeout_seconds;

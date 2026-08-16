ALTER TABLE accounts
    ADD COLUMN IF NOT EXISTS first_output_failover_cooldown_minutes INTEGER;

ALTER TABLE accounts
    DROP CONSTRAINT IF EXISTS accounts_first_output_failover_cooldown_valid;

ALTER TABLE accounts
    ADD CONSTRAINT accounts_first_output_failover_cooldown_valid
    CHECK (
        first_output_failover_cooldown_minutes IS NULL
        OR (
            first_output_failover_cooldown_minutes > 0
            AND first_output_failover_cooldown_minutes <= 10080
            AND first_output_failover_timeout_seconds IS NOT NULL
            AND platform = 'openai'
            AND type = 'apikey'
        )
    );

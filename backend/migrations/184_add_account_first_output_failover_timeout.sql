ALTER TABLE accounts
    ADD COLUMN IF NOT EXISTS first_output_failover_timeout_seconds INTEGER;

ALTER TABLE accounts
    DROP CONSTRAINT IF EXISTS accounts_first_output_failover_timeout_valid;

ALTER TABLE accounts
    ADD CONSTRAINT accounts_first_output_failover_timeout_valid
    CHECK (
        first_output_failover_timeout_seconds IS NULL
        OR (
            first_output_failover_timeout_seconds > 0
            AND platform = 'openai'
            AND type = 'apikey'
        )
    );

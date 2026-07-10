ALTER TABLE redeem_codes
    ADD COLUMN IF NOT EXISTS subscription_plan_id BIGINT REFERENCES subscription_plans(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS subscription_quota_snapshot_version INT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS five_hour_limit_usd DECIMAL(20,8),
    ADD COLUMN IF NOT EXISTS seven_day_limit_usd DECIMAL(20,8),
    ADD COLUMN IF NOT EXISTS thirty_day_limit_usd DECIMAL(20,8);

CREATE INDEX IF NOT EXISTS idx_redeem_codes_subscription_plan_id
    ON redeem_codes(subscription_plan_id)
    WHERE subscription_plan_id IS NOT NULL;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'redeem_codes_five_hour_limit_usd_nonnegative'
    ) THEN
        ALTER TABLE redeem_codes
            ADD CONSTRAINT redeem_codes_five_hour_limit_usd_nonnegative
            CHECK (five_hour_limit_usd IS NULL OR five_hour_limit_usd >= 0);
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'redeem_codes_seven_day_limit_usd_nonnegative'
    ) THEN
        ALTER TABLE redeem_codes
            ADD CONSTRAINT redeem_codes_seven_day_limit_usd_nonnegative
            CHECK (seven_day_limit_usd IS NULL OR seven_day_limit_usd >= 0);
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'redeem_codes_thirty_day_limit_usd_nonnegative'
    ) THEN
        ALTER TABLE redeem_codes
            ADD CONSTRAINT redeem_codes_thirty_day_limit_usd_nonnegative
            CHECK (thirty_day_limit_usd IS NULL OR thirty_day_limit_usd >= 0);
    END IF;
END $$;

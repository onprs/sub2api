-- Subscription plan rolling quota limits.
-- NULL limit = unlimited, 0 = blocked, >0 = USD cap.
-- Existing daily/weekly/monthly fields are kept for compatibility.

ALTER TABLE subscription_plans
    ADD COLUMN IF NOT EXISTS five_hour_limit_usd DECIMAL(20,8),
    ADD COLUMN IF NOT EXISTS seven_day_limit_usd DECIMAL(20,8),
    ADD COLUMN IF NOT EXISTS thirty_day_limit_usd DECIMAL(20,8);

ALTER TABLE user_subscriptions
    ADD COLUMN IF NOT EXISTS five_hour_limit_usd DECIMAL(20,8),
    ADD COLUMN IF NOT EXISTS seven_day_limit_usd DECIMAL(20,8),
    ADD COLUMN IF NOT EXISTS thirty_day_limit_usd DECIMAL(20,8),
    ADD COLUMN IF NOT EXISTS five_hour_usage_usd DECIMAL(20,10) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS seven_day_usage_usd DECIMAL(20,10) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS thirty_day_usage_usd DECIMAL(20,10) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS five_hour_window_start TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS seven_day_window_start TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS thirty_day_window_start TIMESTAMPTZ;

-- Preserve legacy weekly/monthly subscription caps for existing plans and active subscriptions.
-- There is no exact legacy 5h equivalent, so five_hour_limit_usd intentionally remains NULL.
UPDATE subscription_plans sp
SET
    seven_day_limit_usd = COALESCE(sp.seven_day_limit_usd, g.weekly_limit_usd),
    thirty_day_limit_usd = COALESCE(sp.thirty_day_limit_usd, g.monthly_limit_usd)
FROM groups g
WHERE sp.group_id = g.id
  AND g.deleted_at IS NULL
  AND (
      (sp.seven_day_limit_usd IS NULL AND g.weekly_limit_usd IS NOT NULL)
      OR (sp.thirty_day_limit_usd IS NULL AND g.monthly_limit_usd IS NOT NULL)
  );

UPDATE user_subscriptions us
SET
    seven_day_limit_usd = COALESCE(us.seven_day_limit_usd, g.weekly_limit_usd),
    thirty_day_limit_usd = COALESCE(us.thirty_day_limit_usd, g.monthly_limit_usd)
FROM groups g
WHERE us.group_id = g.id
  AND us.deleted_at IS NULL
  AND g.deleted_at IS NULL
  AND (
      (us.seven_day_limit_usd IS NULL AND g.weekly_limit_usd IS NOT NULL)
      OR (us.thirty_day_limit_usd IS NULL AND g.monthly_limit_usd IS NOT NULL)
  );

DO $$
BEGIN
    ALTER TABLE subscription_plans
        ADD CONSTRAINT subscription_plans_five_hour_limit_usd_nonnegative
        CHECK (five_hour_limit_usd IS NULL OR five_hour_limit_usd >= 0);
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

DO $$
BEGIN
    ALTER TABLE subscription_plans
        ADD CONSTRAINT subscription_plans_seven_day_limit_usd_nonnegative
        CHECK (seven_day_limit_usd IS NULL OR seven_day_limit_usd >= 0);
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

DO $$
BEGIN
    ALTER TABLE subscription_plans
        ADD CONSTRAINT subscription_plans_thirty_day_limit_usd_nonnegative
        CHECK (thirty_day_limit_usd IS NULL OR thirty_day_limit_usd >= 0);
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

DO $$
BEGIN
    ALTER TABLE user_subscriptions
        ADD CONSTRAINT user_subscriptions_five_hour_limit_usd_nonnegative
        CHECK (five_hour_limit_usd IS NULL OR five_hour_limit_usd >= 0);
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

DO $$
BEGIN
    ALTER TABLE user_subscriptions
        ADD CONSTRAINT user_subscriptions_seven_day_limit_usd_nonnegative
        CHECK (seven_day_limit_usd IS NULL OR seven_day_limit_usd >= 0);
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

DO $$
BEGIN
    ALTER TABLE user_subscriptions
        ADD CONSTRAINT user_subscriptions_thirty_day_limit_usd_nonnegative
        CHECK (thirty_day_limit_usd IS NULL OR thirty_day_limit_usd >= 0);
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

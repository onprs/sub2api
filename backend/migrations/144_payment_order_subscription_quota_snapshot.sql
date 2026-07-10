-- Snapshot purchased subscription rolling quota terms on payment orders.
-- snapshot_version = 0 means legacy order without a quota snapshot.
-- snapshot_version >= 1 means NULL limit = unlimited, 0 = blocked, >0 = USD cap.

ALTER TABLE payment_orders
    ADD COLUMN IF NOT EXISTS subscription_quota_snapshot_version INT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS subscription_five_hour_limit_usd DECIMAL(20,8),
    ADD COLUMN IF NOT EXISTS subscription_seven_day_limit_usd DECIMAL(20,8),
    ADD COLUMN IF NOT EXISTS subscription_thirty_day_limit_usd DECIMAL(20,8);

DO $$
BEGIN
    ALTER TABLE payment_orders
        ADD CONSTRAINT payment_orders_subscription_five_hour_limit_usd_nonnegative
        CHECK (subscription_five_hour_limit_usd IS NULL OR subscription_five_hour_limit_usd >= 0);
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

DO $$
BEGIN
    ALTER TABLE payment_orders
        ADD CONSTRAINT payment_orders_subscription_seven_day_limit_usd_nonnegative
        CHECK (subscription_seven_day_limit_usd IS NULL OR subscription_seven_day_limit_usd >= 0);
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

DO $$
BEGIN
    ALTER TABLE payment_orders
        ADD CONSTRAINT payment_orders_subscription_thirty_day_limit_usd_nonnegative
        CHECK (subscription_thirty_day_limit_usd IS NULL OR subscription_thirty_day_limit_usd >= 0);
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

-- Existing subscription orders cannot be reconstructed from the original plan
-- at checkout time, but backfilling the current plan snapshot before go-live is
-- safer than letting later plan edits change pending fulfillment behavior.
UPDATE payment_orders po
SET
    subscription_quota_snapshot_version = 1,
    subscription_five_hour_limit_usd = sp.five_hour_limit_usd,
    subscription_seven_day_limit_usd = sp.seven_day_limit_usd,
    subscription_thirty_day_limit_usd = sp.thirty_day_limit_usd
FROM subscription_plans sp
WHERE po.order_type = 'subscription'
  AND po.plan_id = sp.id
  AND po.subscription_quota_snapshot_version = 0;

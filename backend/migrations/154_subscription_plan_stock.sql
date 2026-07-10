-- 154_subscription_plan_stock.sql
-- Adds nullable plan stock. NULL means unlimited, 0 means sold out.

ALTER TABLE subscription_plans
    ADD COLUMN IF NOT EXISTS stock INTEGER;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'subscription_plan_stock_non_negative'
    ) THEN
        ALTER TABLE subscription_plans
            ADD CONSTRAINT subscription_plan_stock_non_negative
            CHECK (stock IS NULL OR stock >= 0);
    END IF;
END $$;

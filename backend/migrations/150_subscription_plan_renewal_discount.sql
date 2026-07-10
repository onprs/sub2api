-- 150_subscription_plan_renewal_discount.sql
-- Adds plan-level renewal discounts and immutable order pricing snapshots.

ALTER TABLE subscription_plans
    ADD COLUMN IF NOT EXISTS renewal_discount_percent NUMERIC(5,2);

ALTER TABLE payment_orders
    ADD COLUMN IF NOT EXISTS subscription_plan_price NUMERIC(20,2),
    ADD COLUMN IF NOT EXISTS subscription_renewal_discount_percent NUMERIC(5,2);

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'subscription_plans_renewal_discount_percent_range'
    ) THEN
        ALTER TABLE subscription_plans
            ADD CONSTRAINT subscription_plans_renewal_discount_percent_range
            CHECK (
                renewal_discount_percent IS NULL OR
                (renewal_discount_percent >= 0 AND renewal_discount_percent < 100)
            );
    END IF;
END $$;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'payment_orders_subscription_plan_price_positive'
    ) THEN
        ALTER TABLE payment_orders
            ADD CONSTRAINT payment_orders_subscription_plan_price_positive
            CHECK (
                subscription_plan_price IS NULL OR subscription_plan_price > 0
            );
    END IF;
END $$;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'payment_orders_subscription_renewal_discount_percent_range'
    ) THEN
        ALTER TABLE payment_orders
            ADD CONSTRAINT payment_orders_subscription_renewal_discount_percent_range
            CHECK (
                subscription_renewal_discount_percent IS NULL OR
                (subscription_renewal_discount_percent >= 0 AND subscription_renewal_discount_percent < 100)
            );
    END IF;
END $$;

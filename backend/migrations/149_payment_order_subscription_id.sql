-- 149_payment_order_subscription_id.sql
-- Track the exact user_subscription affected by each subscription payment order.

ALTER TABLE payment_orders
    ADD COLUMN IF NOT EXISTS subscription_id BIGINT REFERENCES user_subscriptions(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS paymentorder_subscription_id
    ON payment_orders(subscription_id);

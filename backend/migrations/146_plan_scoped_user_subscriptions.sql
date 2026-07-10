-- 146_plan_scoped_user_subscriptions.sql
-- Make plan-backed subscriptions independent while preserving legacy group subscriptions.

ALTER TABLE user_subscriptions
    ADD COLUMN IF NOT EXISTS plan_id BIGINT REFERENCES subscription_plans(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_user_subscriptions_plan_id
    ON user_subscriptions(plan_id);

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM user_subscriptions
        WHERE deleted_at IS NULL
          AND plan_id IS NOT NULL
        GROUP BY user_id, group_id, plan_id
        HAVING COUNT(*) > 1
    ) THEN
        RAISE EXCEPTION 'duplicate active plan-scoped user subscriptions exist; aborting migration 146';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM user_subscriptions
        WHERE deleted_at IS NULL
          AND plan_id IS NULL
        GROUP BY user_id, group_id
        HAVING COUNT(*) > 1
    ) THEN
        RAISE EXCEPTION 'duplicate active legacy user subscriptions exist; aborting migration 146';
    END IF;
END $$;

CREATE UNIQUE INDEX IF NOT EXISTS user_subscriptions_user_group_plan_unique_active
    ON user_subscriptions(user_id, group_id, plan_id)
    WHERE deleted_at IS NULL AND plan_id IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS user_subscriptions_user_group_legacy_unique_active
    ON user_subscriptions(user_id, group_id)
    WHERE deleted_at IS NULL AND plan_id IS NULL;

ALTER TABLE user_subscriptions DROP CONSTRAINT IF EXISTS user_subscriptions_user_id_group_id_key;
DROP INDEX IF EXISTS user_subscriptions_user_id_group_id_key;
DROP INDEX IF EXISTS usersubscription_user_id_group_id;
DROP INDEX IF EXISTS user_subscriptions_user_group_unique_active;

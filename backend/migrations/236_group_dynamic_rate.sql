-- 分组动态倍率配置与请求级 Token 事件账本。
ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS dynamic_rate_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS dynamic_rate_max_multiplier DECIMAL(10,4) NOT NULL DEFAULT 1.0,
    ADD COLUMN IF NOT EXISTS dynamic_rate_min_multiplier DECIMAL(10,4) NOT NULL DEFAULT 1.0,
    ADD COLUMN IF NOT EXISTS dynamic_rate_target_tokens BIGINT NOT NULL DEFAULT 1000000,
    ADD COLUMN IF NOT EXISTS dynamic_rate_window_minutes INTEGER NOT NULL DEFAULT 1440;

COMMENT ON COLUMN groups.dynamic_rate_enabled IS 'Whether per-user rolling-window token usage dynamically lowers the group rate';
COMMENT ON COLUMN groups.dynamic_rate_max_multiplier IS 'Dynamic rate at zero accumulated tokens';
COMMENT ON COLUMN groups.dynamic_rate_min_multiplier IS 'Dynamic rate floor reached at target tokens';
COMMENT ON COLUMN groups.dynamic_rate_target_tokens IS 'Rolling-window tokens required to reach the dynamic rate floor';
COMMENT ON COLUMN groups.dynamic_rate_window_minutes IS 'Dynamic token accounting rolling window in minutes';

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'groups_dynamic_rate_bounds_valid'
          AND conrelid = 'groups'::regclass
    ) THEN
        ALTER TABLE groups
            ADD CONSTRAINT groups_dynamic_rate_bounds_valid CHECK (
                dynamic_rate_max_multiplier >= dynamic_rate_min_multiplier
                AND dynamic_rate_min_multiplier >= 0
            );
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'groups_dynamic_rate_target_valid'
          AND conrelid = 'groups'::regclass
    ) THEN
        ALTER TABLE groups
            ADD CONSTRAINT groups_dynamic_rate_target_valid CHECK (dynamic_rate_target_tokens > 0);
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'groups_dynamic_rate_window_valid'
          AND conrelid = 'groups'::regclass
    ) THEN
        ALTER TABLE groups
            ADD CONSTRAINT groups_dynamic_rate_window_valid CHECK (
                dynamic_rate_window_minutes BETWEEN 1 AND 43200
            );
    END IF;
END $$;

CREATE TABLE IF NOT EXISTS dynamic_rate_usage_metadata (
    singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (singleton),
    ledger_started_at TIMESTAMPTZ NOT NULL
);

INSERT INTO dynamic_rate_usage_metadata (singleton, ledger_started_at)
VALUES (TRUE, NOW())
ON CONFLICT (singleton) DO NOTHING;

CREATE TABLE IF NOT EXISTS dynamic_rate_usage_events (
    id BIGSERIAL PRIMARY KEY,
    request_id TEXT NOT NULL,
    api_key_id BIGINT NOT NULL,
    user_id BIGINT NOT NULL,
    group_id BIGINT NOT NULL,
    total_tokens BIGINT NOT NULL,
    resolved_multiplier DECIMAL(10,4) NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT dynamic_rate_usage_events_request_key UNIQUE (request_id, api_key_id),
    CONSTRAINT dynamic_rate_usage_events_total_tokens_nonnegative CHECK (total_tokens >= 0),
    CONSTRAINT dynamic_rate_usage_events_multiplier_nonnegative CHECK (resolved_multiplier >= 0)
);

CREATE INDEX IF NOT EXISTS idx_dynamic_rate_usage_user_group_time
    ON dynamic_rate_usage_events (user_id, group_id, occurred_at DESC);

CREATE INDEX IF NOT EXISTS idx_dynamic_rate_usage_occurred_at_brin
    ON dynamic_rate_usage_events USING BRIN (occurred_at);

COMMENT ON TABLE dynamic_rate_usage_metadata IS 'Stable activation watermark for historical usage-log backfill into the dynamic-rate ledger';
COMMENT ON TABLE dynamic_rate_usage_events IS 'Idempotent token events used to serialize and resolve per-user dynamic group rates';

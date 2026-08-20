-- 为 API Key 增加固定平台、调度策略和多候选分组配置。

ALTER TABLE api_keys
    ADD COLUMN IF NOT EXISTS routing_platform VARCHAR(50) NOT NULL DEFAULT '';

ALTER TABLE api_keys
    ADD COLUMN IF NOT EXISTS routing_strategy VARCHAR(32) NOT NULL DEFAULT 'manual';

UPDATE api_keys AS ak
SET routing_platform = g.platform
FROM groups AS g
WHERE ak.group_id = g.id
  AND ak.deleted_at IS NULL
  AND ak.routing_platform = '';

CREATE TABLE IF NOT EXISTS api_key_groups (
    api_key_id BIGINT NOT NULL REFERENCES api_keys(id) ON DELETE CASCADE,
    group_id BIGINT NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    priority INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (api_key_id, group_id)
);

CREATE INDEX IF NOT EXISTS idx_api_key_groups_group_id
    ON api_key_groups(group_id);

CREATE INDEX IF NOT EXISTS idx_api_key_groups_api_key_priority
    ON api_key_groups(api_key_id, priority);

INSERT INTO api_key_groups (api_key_id, group_id, priority)
SELECT id, group_id, 0
FROM api_keys
WHERE group_id IS NOT NULL
  AND deleted_at IS NULL
ON CONFLICT (api_key_id, group_id) DO NOTHING;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'api_keys_routing_strategy_check'
          AND conrelid = 'api_keys'::regclass
    ) THEN
        ALTER TABLE api_keys
            ADD CONSTRAINT api_keys_routing_strategy_check
            CHECK (routing_strategy IN ('balanced', 'stability_first', 'cost_first', 'manual'));
    END IF;
END
$$;

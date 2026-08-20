-- Migration: 190_add_channel_monitor_targets
-- Channel Monitor 支持本站分组与外站端点两种目标。
--
-- 旧记录先标记为 local，但 group_id 不能通过空 group_name 可靠推断；应用启动时会
-- 解密旧监控密钥、按 api_keys.key 找到真实分组，并在全部解析成功后用单事务完成映射。

ALTER TABLE channel_monitors
    ADD COLUMN IF NOT EXISTS target_type VARCHAR(16) NOT NULL DEFAULT 'local';

ALTER TABLE channel_monitors
    ADD COLUMN IF NOT EXISTS group_id BIGINT;

ALTER TABLE channel_monitors
    ALTER COLUMN endpoint SET DEFAULT '',
    ALTER COLUMN api_key_encrypted SET DEFAULT '';

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'channel_monitors_target_type_check'
          AND conrelid = 'channel_monitors'::regclass
    ) THEN
        ALTER TABLE channel_monitors
            ADD CONSTRAINT channel_monitors_target_type_check
            CHECK (target_type IN ('local', 'external'));
    END IF;
END $$;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'channel_monitors_group_id_fkey'
          AND conrelid = 'channel_monitors'::regclass
    ) THEN
        ALTER TABLE channel_monitors
            ADD CONSTRAINT channel_monitors_group_id_fkey
            FOREIGN KEY (group_id) REFERENCES groups(id) ON DELETE RESTRICT;
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS idx_channel_monitors_target_type
    ON channel_monitors (target_type);

CREATE INDEX IF NOT EXISTS idx_channel_monitors_group_id
    ON channel_monitors (group_id);

-- 一个本站分组只对应一个监控，确保路由健康状态不会出现歧义。
CREATE UNIQUE INDEX IF NOT EXISTS idx_channel_monitors_local_group_unique
    ON channel_monitors (group_id)
    WHERE target_type = 'local' AND group_id IS NOT NULL;

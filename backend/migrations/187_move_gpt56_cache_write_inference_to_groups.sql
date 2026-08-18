-- GPT-5.6 缓存写入推断属于请求分组的计费策略，不再由调度到的账号决定。
ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS infer_gpt56_cache_write BOOLEAN NOT NULL DEFAULT FALSE;

ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS infer_gpt56_cache_write_min_tokens INTEGER NOT NULL DEFAULT 1024;

COMMENT ON COLUMN groups.infer_gpt56_cache_write IS
    '上游未报告 GPT-5.6 缓存写入时，是否按未缓存输入量推断';
COMMENT ON COLUMN groups.infer_gpt56_cache_write_min_tokens IS
    '触发 GPT-5.6 缓存写入推断所需的最小未缓存输入 token 数';

-- 账号和分组是多对多关系。只要分组内任一 OpenAI 账号曾启用该策略，就为分组启用；
-- 多个已启用账号阈值不同时取最小有效值，避免迁移后漏记原本会推断的缓存写入。
WITH legacy_group_settings AS (
    SELECT
        ag.group_id,
        MIN(
            CASE
                WHEN COALESCE(a.extra->>'infer_gpt56_cache_write_min_tokens', '') ~ '^[1-9][0-9]{0,8}$'
                    THEN (a.extra->>'infer_gpt56_cache_write_min_tokens')::INTEGER
                ELSE 1024
            END
        ) AS min_tokens
    FROM account_groups AS ag
    JOIN accounts AS a ON a.id = ag.account_id
    JOIN groups AS g ON g.id = ag.group_id
    WHERE a.deleted_at IS NULL
      AND g.deleted_at IS NULL
      AND a.platform = 'openai'
      AND g.platform = 'openai'
      AND LOWER(COALESCE(a.extra->>'infer_gpt56_cache_write', 'false')) = 'true'
    GROUP BY ag.group_id
)
UPDATE groups AS g
SET
    infer_gpt56_cache_write = TRUE,
    infer_gpt56_cache_write_min_tokens = legacy.min_tokens
FROM legacy_group_settings AS legacy
WHERE g.id = legacy.group_id;

-- 回填完成后删除账号级旧键，确保配置只有分组级单一来源；重复执行时不会再次覆盖分组设置。
UPDATE accounts
SET extra = (COALESCE(extra, '{}'::jsonb) - 'infer_gpt56_cache_write') - 'infer_gpt56_cache_write_min_tokens'
WHERE COALESCE(extra, '{}'::jsonb) ? 'infer_gpt56_cache_write'
   OR COALESCE(extra, '{}'::jsonb) ? 'infer_gpt56_cache_write_min_tokens';

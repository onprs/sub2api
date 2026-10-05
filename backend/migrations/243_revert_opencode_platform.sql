-- 将 OpenCode 持久化平台标识回退为官方内部值 opencode_go，与代码常量
-- domain.PlatformOpenCodeGo 一致。反向执行 241_unify_opencode_platform.sql 的
-- 平台值统一：保留既有 account_mode（zen/go）语义、定制平台
-- （clinepass/openrouter/commandcode）与 TypeSafe 约束。

-- 1. user_platform_quotas：先放宽约束，改值，再收紧到包含 opencode_go 的最终集合。
ALTER TABLE user_platform_quotas
    DROP CONSTRAINT IF EXISTS user_platform_quotas_platform_check;

UPDATE user_platform_quotas
SET platform = 'opencode_go', updated_at = NOW()
WHERE platform = 'opencode';

ALTER TABLE user_platform_quotas
    ADD CONSTRAINT user_platform_quotas_platform_check
    CHECK (platform IN ('anthropic', 'openai', 'gemini', 'antigravity', 'grok',
                        'kimi', 'zhipu', 'deepseek', 'minimax', 'opencode_go',
                        'clinepass', 'openrouter', 'commandcode', 'typesafe'));

-- 2. accounts / groups。
UPDATE accounts
SET platform = 'opencode_go', updated_at = NOW()
WHERE platform = 'opencode';

UPDATE groups
SET platform = 'opencode_go', updated_at = NOW()
WHERE platform = 'opencode';

-- 3. api_keys 路由平台。
UPDATE api_keys
SET routing_platform = 'opencode_go', updated_at = NOW()
WHERE routing_platform = 'opencode';

-- 4. 平台定价表。
UPDATE channel_model_pricing
SET platform = 'opencode_go', updated_at = NOW()
WHERE platform = 'opencode';

UPDATE channel_account_stats_model_pricing
SET platform = 'opencode_go', updated_at = NOW()
WHERE platform = 'opencode';

-- 5. groups.model_pricing JSON 数组内的 platform 字段。
UPDATE groups AS group_row
SET model_pricing = (
        SELECT jsonb_agg(
            CASE
                WHEN item ->> 'platform' = 'opencode'
                    THEN jsonb_set(item, '{platform}', '"opencode_go"'::jsonb, TRUE)
                ELSE item
            END
            ORDER BY ordinal
        )
        FROM jsonb_array_elements(group_row.model_pricing) WITH ORDINALITY AS entries(item, ordinal)
    ),
    updated_at = NOW()
WHERE jsonb_typeof(group_row.model_pricing) = 'array'
  AND EXISTS (
      SELECT 1
      FROM jsonb_array_elements(group_row.model_pricing) AS entries(item)
      WHERE item ->> 'platform' = 'opencode'
  );

-- 6. channels.model_mapping 的平台键。
UPDATE channels AS channel_row
SET model_mapping = (channel_row.model_mapping - 'opencode') || jsonb_build_object(
        'opencode_go',
        (CASE
            WHEN jsonb_typeof(channel_row.model_mapping -> 'opencode') = 'object'
                THEN channel_row.model_mapping -> 'opencode'
            ELSE '{}'::jsonb
        END) ||
        (CASE
            WHEN jsonb_typeof(channel_row.model_mapping -> 'opencode_go') = 'object'
                THEN channel_row.model_mapping -> 'opencode_go'
            ELSE '{}'::jsonb
        END)
    ),
    updated_at = NOW()
WHERE jsonb_typeof(channel_row.model_mapping) = 'object'
  AND jsonb_typeof(channel_row.model_mapping -> 'opencode') = 'object';

-- 7. error_passthrough_rules.platforms 数组。
UPDATE error_passthrough_rules AS rule
SET platforms = (
        SELECT jsonb_agg(
            to_jsonb(CASE WHEN platform = 'opencode' THEN 'opencode_go' ELSE platform END)
            ORDER BY ordinal
        )
        FROM jsonb_array_elements_text(rule.platforms) WITH ORDINALITY AS entries(platform, ordinal)
    ),
    updated_at = NOW()
WHERE jsonb_typeof(rule.platforms) = 'array'
  AND rule.platforms ? 'opencode';

-- 8. channel_monitor_v2_config.platforms 数组项的 platform 字段。
UPDATE channel_monitor_v2_config AS config_row
SET platforms = (
        SELECT jsonb_agg(
            CASE
                WHEN item ->> 'platform' = 'opencode'
                    THEN jsonb_set(item, '{platform}', '"opencode_go"'::jsonb, TRUE)
                ELSE item
            END
            ORDER BY ordinal
        )
        FROM jsonb_array_elements(config_row.platforms) WITH ORDINALITY AS entries(item, ordinal)
    ),
    updated_at = NOW()
WHERE jsonb_typeof(config_row.platforms) = 'array'
  AND EXISTS (
      SELECT 1
      FROM jsonb_array_elements(config_row.platforms) AS entries(item)
      WHERE item ->> 'platform' = 'opencode'
  );

-- 9. 渠道监控 provider 与模板 provider。
ALTER TABLE channel_monitors
    DROP CONSTRAINT IF EXISTS channel_monitors_provider_check;
ALTER TABLE channel_monitor_request_templates
    DROP CONSTRAINT IF EXISTS channel_monitor_request_templates_provider_check;

UPDATE channel_monitors
SET provider = 'opencode_go', updated_at = NOW()
WHERE provider = 'opencode';

UPDATE channel_monitor_request_templates
SET provider = 'opencode_go', updated_at = NOW()
WHERE provider = 'opencode';

ALTER TABLE channel_monitors
    ADD CONSTRAINT channel_monitors_provider_check
    CHECK (provider IN ('openai', 'anthropic', 'gemini', 'grok', 'opencode_go',
                        'clinepass', 'openrouter', 'commandcode', 'antigravity',
                        'antigravity_claude', 'antigravity_gemini', 'kimi', 'zhipu',
                        'deepseek', 'minimax'));

ALTER TABLE channel_monitor_request_templates
    ADD CONSTRAINT channel_monitor_request_templates_provider_check
    CHECK (provider IN ('openai', 'anthropic', 'gemini', 'grok', 'opencode_go',
                        'clinepass', 'openrouter', 'commandcode', 'antigravity',
                        'antigravity_claude', 'antigravity_gemini', 'kimi', 'zhipu',
                        'deepseek', 'minimax'));

-- 10. composite_model_routes 目标平台。
ALTER TABLE composite_model_routes
    DROP CONSTRAINT IF EXISTS composite_model_routes_target_platform_check;

UPDATE composite_model_routes
SET target_platform = 'opencode_go', updated_at = NOW()
WHERE target_platform = 'opencode';

ALTER TABLE composite_model_routes
    ADD CONSTRAINT composite_model_routes_target_platform_check
    CHECK (target_platform IN ('anthropic', 'openai', 'gemini', 'antigravity', 'grok',
                               'kimi', 'zhipu', 'deepseek', 'minimax', 'opencode_go',
                               'typesafe'));

-- 11. Ops 日志、指标与告警。
UPDATE ops_error_logs SET platform = 'opencode_go' WHERE platform = 'opencode';
UPDATE ops_system_metrics SET platform = 'opencode_go' WHERE platform = 'opencode';
UPDATE ops_system_logs SET platform = 'opencode_go' WHERE platform = 'opencode';
UPDATE ops_alert_silences SET platform = 'opencode_go' WHERE platform = 'opencode';

UPDATE ops_alert_rules
SET filters = jsonb_set(filters, '{platform}', '"opencode_go"'::jsonb, TRUE),
    updated_at = NOW()
WHERE jsonb_typeof(filters) = 'object'
  AND filters ->> 'platform' = 'opencode';

UPDATE ops_alert_events
SET dimensions = jsonb_set(dimensions, '{platform}', '"opencode_go"'::jsonb, TRUE)
WHERE jsonb_typeof(dimensions) = 'object'
  AND dimensions ->> 'platform' = 'opencode';

UPDATE ops_metrics_hourly SET platform = 'opencode_go' WHERE platform = 'opencode';
UPDATE ops_metrics_daily SET platform = 'opencode_go' WHERE platform = 'opencode';

-- 12. V2 渠道监控指标。
UPDATE channel_monitor_v2_metrics_1m SET platform = 'opencode_go' WHERE platform = 'opencode';
UPDATE channel_monitor_v2_user_metrics_1m SET platform = 'opencode_go' WHERE platform = 'opencode';
UPDATE channel_monitor_v2_error_metrics_1m SET platform = 'opencode_go' WHERE platform = 'opencode';
UPDATE channel_monitor_v2_latency_histograms_1m SET platform = 'opencode_go' WHERE platform = 'opencode';
UPDATE channel_monitor_v2_metrics_rollup SET platform = 'opencode_go' WHERE platform = 'opencode';
UPDATE channel_monitor_v2_user_metrics_rollup SET platform = 'opencode_go' WHERE platform = 'opencode';
UPDATE channel_monitor_v2_error_metrics_rollup SET platform = 'opencode_go' WHERE platform = 'opencode';
UPDATE channel_monitor_v2_latency_histograms_rollup SET platform = 'opencode_go' WHERE platform = 'opencode';

-- 13. settings 中的平台额度与调度阈值映射键。
DO $$
DECLARE
    setting_row RECORD;
    document JSONB;
BEGIN
    FOR setting_row IN
        SELECT id, key, value
        FROM settings
        WHERE key IN ('default_platform_quotas', 'account_scheduling_thresholds')
           OR key LIKE 'auth_source_default_%_platform_quotas'
    LOOP
        BEGIN
            document := setting_row.value::jsonb;
        EXCEPTION WHEN others THEN
            CONTINUE;
        END;
        IF jsonb_typeof(document) <> 'object' OR NOT (document ? 'opencode') THEN
            CONTINUE;
        END IF;

        IF NOT (document ? 'opencode_go') THEN
            document := jsonb_set(document, '{opencode_go}', document -> 'opencode', TRUE);
        END IF;
        document := document - 'opencode';
        UPDATE settings SET value = document::text, updated_at = NOW() WHERE id = setting_row.id;
    END LOOP;
END $$;

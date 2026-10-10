-- 平台实现统一到官方清单：旧 ClinePass / Command Code ID 转为 cline / command_code。
-- 243 会重建旧平台 CHECK，因此此处再次移除；已发布迁移保持原字节和校验和。
ALTER TABLE user_platform_quotas
    DROP CONSTRAINT IF EXISTS user_platform_quotas_platform_check;
ALTER TABLE composite_model_routes
    DROP CONSTRAINT IF EXISTS composite_model_routes_target_platform_check;
ALTER TABLE channel_monitors
    DROP CONSTRAINT IF EXISTS channel_monitors_provider_check;
ALTER TABLE channel_monitor_request_templates
    DROP CONSTRAINT IF EXISTS channel_monitor_request_templates_provider_check;

-- 旧客户端在 base_url 后追加 /provider/v1；官方 profile 直接使用推理基址。
-- 只处理旧平台账号，重复执行不会再追加路径，也不改已采用官方 ID 的账号。
UPDATE accounts
SET credentials = jsonb_set(
        credentials,
        '{base_url}',
        to_jsonb(rtrim(COALESCE(NULLIF(btrim(credentials ->> 'base_url'), ''),
                               'https://api.commandcode.ai'), '/') || '/provider/v1'),
        TRUE
    ),
    updated_at = NOW()
WHERE platform = 'commandcode'
  AND type = 'apikey'
  AND jsonb_typeof(credentials) = 'object';

-- 用量窗口转为官方 CN 配额服务的字段；已有官方快照优先，保留历史诊断字段。
DO $$
DECLARE
    mapping RECORD;
BEGIN
    FOR mapping IN
        SELECT * FROM (VALUES
            ('clinepass_usage_updated_at', 'cline_usage_updated_at'),
            ('clinepass_usage_5h_used_percent', 'cline_5h_used_percent'),
            ('clinepass_usage_5h_resets_at', 'cline_5h_reset_at'),
            ('clinepass_usage_7d_used_percent', 'cline_weekly_used_percent'),
            ('clinepass_usage_7d_resets_at', 'cline_weekly_reset_at'),
            ('clinepass_usage_30d_used_percent', 'cline_monthly_used_percent'),
            ('clinepass_usage_30d_resets_at', 'cline_monthly_reset_at'),
            ('commandcode_usage_updated_at', 'command_code_usage_updated_at'),
            ('commandcode_usage_5h_used_percent', 'command_code_5h_used_percent'),
            ('commandcode_usage_5h_resets_at', 'command_code_5h_reset_at'),
            ('commandcode_usage_7d_used_percent', 'command_code_weekly_used_percent'),
            ('commandcode_usage_7d_resets_at', 'command_code_weekly_reset_at'),
            ('commandcode_usage_30d_used_percent', 'command_code_monthly_used_percent'),
            ('commandcode_usage_30d_resets_at', 'command_code_monthly_reset_at'),
            ('commandcode_usage_purchased_usd', 'command_code_purchased_credits')
        ) AS mappings(legacy_key, official_key)
    LOOP
        UPDATE accounts
        SET extra = extra || jsonb_build_object(mapping.official_key, extra -> mapping.legacy_key),
            updated_at = NOW()
        WHERE platform IN ('clinepass', 'commandcode')
          AND jsonb_typeof(extra) = 'object'
          AND extra ? mapping.legacy_key
          AND NOT (extra ? mapping.official_key);
    END LOOP;
END $$;

-- 包含账号、分组、Key、配额、渠道定价、组合路由、日志和各级聚合指标。
-- 仅更新本项目的已知平台列；唯一键冲突会使整次迁移回滚，避免丢弃额度或统计。
DO $$
DECLARE
    column_row RECORD;
BEGIN
    FOR column_row IN
        SELECT table_name, column_name
        FROM information_schema.columns
        WHERE table_schema = current_schema()
          AND (
              (column_name = 'platform' AND table_name IN (
                  'accounts', 'groups', 'user_platform_quotas',
                  'channel_model_pricing', 'channel_account_stats_model_pricing',
                  'ops_error_logs', 'ops_system_metrics', 'ops_system_logs', 'ops_alert_silences',
                  'ops_metrics_hourly', 'ops_metrics_daily',
                  'channel_monitor_v2_metrics_1m', 'channel_monitor_v2_user_metrics_1m',
                  'channel_monitor_v2_error_metrics_1m', 'channel_monitor_v2_latency_histograms_1m',
                  'channel_monitor_v2_metrics_rollup', 'channel_monitor_v2_user_metrics_rollup',
                  'channel_monitor_v2_error_metrics_rollup', 'channel_monitor_v2_latency_histograms_rollup'
              ))
              OR (table_name = 'api_keys' AND column_name = 'routing_platform')
              OR (table_name = 'composite_model_routes' AND column_name = 'target_platform')
              OR (table_name IN ('channel_monitors', 'channel_monitor_request_templates') AND column_name = 'provider')
          )
    LOOP
        EXECUTE format(
            'UPDATE %I SET %I = CASE %I WHEN ''clinepass'' THEN ''cline'' WHEN ''commandcode'' THEN ''command_code'' END WHERE %I IN (''clinepass'', ''commandcode'')',
            column_row.table_name, column_row.column_name, column_row.column_name, column_row.column_name
        );
    END LOOP;
END $$;

-- 仅转换平台字段、平台数组和明确的平台映射键；模型名与普通文本保持原值。
CREATE OR REPLACE FUNCTION pg_temp.sub2api_official_platform_json(
    document JSONB, platform_keys BOOLEAN, platform_value BOOLEAN
) RETURNS JSONB LANGUAGE plpgsql AS $$
DECLARE
    entry RECORD;
    official_key TEXT;
    legacy_key TEXT;
    value JSONB;
    result JSONB;
BEGIN
    IF jsonb_typeof(document) = 'object' THEN
        result := '{}'::jsonb;
        FOR entry IN SELECT key, val FROM jsonb_each(document) AS items(key, val)
        LOOP
            official_key := entry.key;
            value := entry.val;
            IF platform_keys THEN
                official_key := CASE entry.key WHEN 'clinepass' THEN 'cline'
                                               WHEN 'commandcode' THEN 'command_code' ELSE entry.key END;
                IF official_key <> entry.key AND document ? official_key THEN
                    CONTINUE;
                END IF;
                legacy_key := CASE entry.key WHEN 'cline' THEN 'clinepass'
                                            WHEN 'command_code' THEN 'commandcode' ELSE NULL END;
                IF legacy_key IS NOT NULL AND jsonb_typeof(document -> legacy_key) = 'object'
                   AND jsonb_typeof(value) = 'object' THEN
                    value := (document -> legacy_key) || value;
                END IF;
            END IF;
            result := result || jsonb_build_object(official_key,
                pg_temp.sub2api_official_platform_json(value, FALSE,
                    entry.key IN ('platform', 'provider', 'routing_platform', 'target_platform', 'platforms')));
        END LOOP;
        RETURN result;
    ELSIF jsonb_typeof(document) = 'array' THEN
        SELECT COALESCE(jsonb_agg(pg_temp.sub2api_official_platform_json(item, FALSE, platform_value)
                                 ORDER BY ordinal), '[]'::jsonb)
        INTO result
        FROM jsonb_array_elements(document) WITH ORDINALITY AS items(item, ordinal);
        RETURN result;
    ELSIF platform_value AND jsonb_typeof(document) = 'string' THEN
        RETURN to_jsonb(CASE document #>> '{}' WHEN 'clinepass' THEN 'cline'
                                             WHEN 'commandcode' THEN 'command_code' ELSE document #>> '{}' END);
    END IF;
    RETURN document;
END $$;

DO $$
DECLARE
    column_row RECORD;
BEGIN
    FOR column_row IN
        SELECT table_name, column_name,
               table_name = 'channels' AND column_name = 'model_mapping' AS platform_keys,
               table_name = 'error_passthrough_rules' AND column_name = 'platforms' AS platform_value
        FROM information_schema.columns
        WHERE table_schema = current_schema()
          AND (
              (table_name = 'groups' AND column_name = 'model_pricing')
              OR (table_name = 'channels' AND column_name = 'model_mapping')
              OR (table_name = 'error_passthrough_rules' AND column_name = 'platforms')
              OR (table_name = 'channel_monitor_v2_config' AND column_name = 'platforms')
              OR (table_name = 'ops_alert_rules' AND column_name = 'filters')
              OR (table_name = 'ops_alert_events' AND column_name = 'dimensions')
          )
    LOOP
        EXECUTE format(
            'UPDATE %I SET %I = pg_temp.sub2api_official_platform_json(%I, $1, $2) WHERE %I IS DISTINCT FROM pg_temp.sub2api_official_platform_json(%I, $1, $2)',
            column_row.table_name, column_row.column_name, column_row.column_name,
            column_row.column_name, column_row.column_name
        ) USING column_row.platform_keys, column_row.platform_value;
    END LOOP;
END $$;

DO $$
DECLARE
    setting_row RECORD;
    document JSONB;
    normalized JSONB;
BEGIN
    FOR setting_row IN
        SELECT id, value FROM settings
        WHERE key IN ('default_platform_quotas', 'account_scheduling_thresholds')
           OR key LIKE 'auth_source_default_%_platform_quotas'
    LOOP
        BEGIN
            document := setting_row.value::jsonb;
        EXCEPTION WHEN invalid_text_representation THEN
            CONTINUE;
        END;
        normalized := pg_temp.sub2api_official_platform_json(document, TRUE, FALSE);
        IF normalized IS DISTINCT FROM document THEN
            UPDATE settings SET value = normalized::text, updated_at = NOW() WHERE id = setting_row.id;
        END IF;
    END LOOP;
END $$;

DROP FUNCTION pg_temp.sub2api_official_platform_json(JSONB, BOOLEAN, BOOLEAN);

-- 监控 provider 的 CHECK 表示探测能力，继续保留；平台集合由官方应用清单校验。
ALTER TABLE channel_monitors
    ADD CONSTRAINT channel_monitors_provider_check
    CHECK (provider IN ('openai', 'anthropic', 'gemini', 'grok', 'opencode_go',
                        'cline', 'openrouter', 'command_code', 'antigravity',
                        'antigravity_claude', 'antigravity_gemini', 'kimi', 'zhipu',
                        'deepseek', 'minimax'));
ALTER TABLE channel_monitor_request_templates
    ADD CONSTRAINT channel_monitor_request_templates_provider_check
    CHECK (provider IN ('openai', 'anthropic', 'gemini', 'grok', 'opencode_go',
                        'cline', 'openrouter', 'command_code', 'antigravity',
                        'antigravity_claude', 'antigravity_gemini', 'kimi', 'zhipu',
                        'deepseek', 'minimax'));

-- 以官方 Go 已公布的价格初始化 OpenCode 原生渠道计费。
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '10min';

-- MiMo Pro 缓存读取价格需要保留小数点后 12 位的每 token 单价。
ALTER TABLE channel_model_pricing ALTER COLUMN cache_read_price TYPE NUMERIC(20,12);

ALTER TABLE channel_account_stats_pricing_rules ADD COLUMN IF NOT EXISTS pricing_source VARCHAR(50) NOT NULL DEFAULT '';
ALTER TABLE channel_account_stats_pricing_rules ADD COLUMN IF NOT EXISTS platform VARCHAR(50) NOT NULL DEFAULT '';
COMMENT ON COLUMN channel_account_stats_pricing_rules.pricing_source IS '空值为自定义价格；channel_model_multiplier 读取渠道原价并应用模型特有倍率';
COMMENT ON COLUMN channel_model_pricing.monthly_quota IS '模型基础月额度及限时额度活动，用于平台额度折算';

-- 仅升级完全匹配旧默认协议表的 Go 按模型分流账号，保留自定义规则和固定协议。
UPDATE accounts
SET credentials = jsonb_set(credentials, '{protocol_rules}', '[{"pattern":"grok-*","protocol":"responses"},{"pattern":"gpt-*","protocol":"responses"},{"pattern":"muse-spark-*","protocol":"responses"},{"pattern":"claude-haiku-5-5","protocol":"anthropic"},{"pattern":"minimax-*","protocol":"anthropic"},{"pattern":"qwen*","protocol":"anthropic"}]'::JSONB), updated_at = NOW()
WHERE platform = 'opencode_go' AND type = 'apikey' AND deleted_at IS NULL
  AND COALESCE(credentials->>'account_mode', 'go') = 'go'
  AND credentials->>'api_protocol' = 'adaptive'
  AND credentials->'protocol_rules' = '[{"pattern":"grok-*","protocol":"responses"},{"pattern":"gpt-*","protocol":"responses"},{"pattern":"muse-spark-*","protocol":"responses"},{"pattern":"minimax-*","protocol":"anthropic"},{"pattern":"qwen*","protocol":"anthropic"}]'::JSONB;

DO $migration$
DECLARE
    billing_channel_id BIGINT;
    billing_channel_name TEXT := 'OpenCode';
    suffix INTEGER := 1;
    seed JSONB := $seed$
[
    {"platform":"opencode_go","models":["grok-4.7"],"billing_mode":"token","input_price":0.000002,"output_price":0.000006,"cache_read_price":5e-7,"cache_write_price":0,"monthly_quota":{"base_credits_usd":15},"intervals":[{"min_tokens":0,"max_tokens":200000,"input_price":0.000002,"output_price":0.000006,"cache_read_price":5e-7,"cache_write_price":0,"sort_order":0},{"min_tokens":200000,"max_tokens":null,"input_price":0.000004,"output_price":0.000012,"cache_read_price":0.000001,"cache_write_price":0,"sort_order":1}],"time_pricing":null,"protocol":"responses"},
    {"platform":"opencode_go","models":["grok-4.6"],"billing_mode":"token","input_price":0.000002,"output_price":0.000006,"cache_read_price":5e-7,"cache_write_price":0,"monthly_quota":{"base_credits_usd":15},"intervals":[{"min_tokens":0,"max_tokens":200000,"input_price":0.000002,"output_price":0.000006,"cache_read_price":5e-7,"cache_write_price":0,"sort_order":0},{"min_tokens":200000,"max_tokens":null,"input_price":0.000004,"output_price":0.000012,"cache_read_price":0.000001,"cache_write_price":0,"sort_order":1}],"time_pricing":null,"protocol":"responses"},
    {"platform":"opencode_go","models":["gpt-6-luna"],"billing_mode":"token","input_price":1.0000000000000001e-7,"output_price":5e-7,"cache_read_price":1e-8,"cache_write_price":1.25e-7,"monthly_quota":{"base_credits_usd":15},"intervals":[{"min_tokens":0,"max_tokens":272000,"input_price":1.0000000000000001e-7,"output_price":5e-7,"cache_read_price":1e-8,"cache_write_price":1.25e-7,"sort_order":0},{"min_tokens":272000,"max_tokens":null,"input_price":2.0000000000000002e-7,"output_price":7.5e-7,"cache_read_price":2e-8,"cache_write_price":2.5e-7,"sort_order":1}],"time_pricing":null,"protocol":"responses"},
    {"platform":"opencode_go","models":["gpt-5.6-luna"],"billing_mode":"token","input_price":2.0000000000000002e-7,"output_price":0.0000012,"cache_read_price":2e-8,"cache_write_price":2.5e-7,"monthly_quota":{"base_credits_usd":15},"intervals":[{"min_tokens":0,"max_tokens":272000,"input_price":2.0000000000000002e-7,"output_price":0.0000012,"cache_read_price":2e-8,"cache_write_price":2.5e-7,"sort_order":0},{"min_tokens":272000,"max_tokens":null,"input_price":4.0000000000000003e-7,"output_price":0.0000018000000000000001,"cache_read_price":4e-8,"cache_write_price":5e-7,"sort_order":1}],"time_pricing":null,"protocol":"responses"},
    {"platform":"opencode_go","models":["claude-haiku-5-5"],"billing_mode":"token","input_price":1.0000000000000001e-7,"output_price":5e-7,"cache_read_price":1e-8,"cache_write_price":1.25e-7,"monthly_quota":{"base_credits_usd":15},"intervals":[{"min_tokens":0,"max_tokens":100000,"input_price":1.0000000000000001e-7,"output_price":5e-7,"cache_read_price":1e-8,"cache_write_price":1.25e-7,"sort_order":0},{"min_tokens":100000,"max_tokens":null,"input_price":5e-7,"output_price":0.0000025,"cache_read_price":5.0000000000000004e-8,"cache_write_price":6.25e-7,"sort_order":1}],"time_pricing":null,"protocol":"anthropic"},
    {"platform":"opencode_go","models":["glm-5.3-flash"],"billing_mode":"token","input_price":1.5e-7,"output_price":5e-7,"cache_read_price":3e-8,"cache_write_price":0,"monthly_quota":{"base_credits_usd":60},"intervals":[],"time_pricing":null,"protocol":"chat_completions"},
    {"platform":"opencode_go","models":["glm-5.3"],"billing_mode":"token","input_price":0.0000014,"output_price":0.0000044,"cache_read_price":2.6e-7,"cache_write_price":0,"monthly_quota":{"base_credits_usd":15},"intervals":[],"time_pricing":null,"protocol":"chat_completions"},
    {"platform":"opencode_go","models":["glm-5.2"],"billing_mode":"token","input_price":0.0000014,"output_price":0.0000044,"cache_read_price":2.6e-7,"cache_write_price":0,"monthly_quota":{"base_credits_usd":60},"intervals":[],"time_pricing":null,"protocol":"chat_completions"},
    {"platform":"opencode_go","models":["kimi-k3"],"billing_mode":"token","input_price":0.000003,"output_price":0.000015,"cache_read_price":3e-7,"cache_write_price":0,"monthly_quota":{"base_credits_usd":15},"intervals":[],"time_pricing":null,"protocol":"chat_completions"},
    {"platform":"opencode_go","models":["kimi-k2.7-code"],"billing_mode":"token","input_price":9.499999999999999e-7,"output_price":0.000004,"cache_read_price":1.9e-7,"cache_write_price":0,"monthly_quota":{"base_credits_usd":60},"intervals":[],"time_pricing":null,"protocol":"chat_completions"},
    {"platform":"opencode_go","models":["kimi-k2.6"],"billing_mode":"token","input_price":9.499999999999999e-7,"output_price":0.000004,"cache_read_price":1.6e-7,"cache_write_price":0,"monthly_quota":{"base_credits_usd":60},"intervals":[],"time_pricing":null,"protocol":"chat_completions"},
    {"platform":"opencode_go","models":["longcat-2.0"],"billing_mode":"token","input_price":3e-7,"output_price":0.0000012,"cache_read_price":6e-9,"cache_write_price":0,"monthly_quota":{"base_credits_usd":60},"intervals":[],"time_pricing":null,"protocol":"chat_completions"},
    {"platform":"opencode_go","models":["longcat-2.5-preview-free"],"billing_mode":"token","input_price":0,"output_price":0,"cache_read_price":0,"cache_write_price":0,"monthly_quota":null,"intervals":[],"time_pricing":null,"protocol":"chat_completions"},
    {"platform":"opencode_go","models":["step-5-preview-free"],"billing_mode":"token","input_price":0,"output_price":0,"cache_read_price":0,"cache_write_price":0,"monthly_quota":null,"intervals":[],"time_pricing":null,"protocol":"chat_completions"},
    {"platform":"opencode_go","models":["deepseek-v4.1-flash"],"billing_mode":"token","input_price":1.5e-7,"output_price":6e-7,"cache_read_price":3e-9,"cache_write_price":0,"monthly_quota":{"base_credits_usd":80},"intervals":[],"time_pricing":{"timezone":"UTC","weekdays_only":true,"periods":[{"start_time":"01:00:00","end_time":"04:00:00","multiplier":2},{"start_time":"06:00:00","end_time":"10:00:00","multiplier":2}]},"protocol":"chat_completions"},
    {"platform":"opencode_go","models":["deepseek-v4-pro"],"billing_mode":"token","input_price":6.6e-7,"output_price":0.00000198,"cache_read_price":2.2e-8,"cache_write_price":0,"monthly_quota":{"base_credits_usd":15},"intervals":[],"time_pricing":{"timezone":"UTC","weekdays_only":true,"periods":[{"start_time":"01:00:00","end_time":"04:00:00","multiplier":2},{"start_time":"06:00:00","end_time":"10:00:00","multiplier":2}]},"protocol":"chat_completions"},
    {"platform":"opencode_go","models":["deepseek-v4-flash"],"billing_mode":"token","input_price":1.5e-7,"output_price":6e-7,"cache_read_price":3e-9,"cache_write_price":0,"monthly_quota":{"base_credits_usd":30},"intervals":[],"time_pricing":{"timezone":"UTC","weekdays_only":true,"periods":[{"start_time":"01:00:00","end_time":"04:00:00","multiplier":2},{"start_time":"06:00:00","end_time":"10:00:00","multiplier":2}]},"protocol":"chat_completions"},
    {"platform":"opencode_go","models":["deepseek-v4-flash-vision-exp"],"billing_mode":"token","input_price":1.5e-7,"output_price":6e-7,"cache_read_price":3e-9,"cache_write_price":0,"monthly_quota":{"base_credits_usd":15},"intervals":[],"time_pricing":{"timezone":"UTC","weekdays_only":true,"periods":[{"start_time":"01:00:00","end_time":"04:00:00","multiplier":2},{"start_time":"06:00:00","end_time":"10:00:00","multiplier":2}]},"protocol":"chat_completions"},
    {"platform":"opencode_go","models":["mimo-v2.6-flash"],"billing_mode":"token","input_price":1.4e-7,"output_price":2.8e-7,"cache_read_price":2.8e-9,"cache_write_price":0,"monthly_quota":{"base_credits_usd":60},"intervals":[],"time_pricing":null,"protocol":"chat_completions"},
    {"platform":"opencode_go","models":["mimo-v2.6-pro"],"billing_mode":"token","input_price":4.35e-7,"output_price":8.7e-7,"cache_read_price":3.625e-9,"cache_write_price":0,"monthly_quota":{"base_credits_usd":15},"intervals":[],"time_pricing":null,"protocol":"chat_completions"},
    {"platform":"opencode_go","models":["mimo-v2.5"],"billing_mode":"token","input_price":1.4e-7,"output_price":2.8e-7,"cache_read_price":2.8e-9,"cache_write_price":0,"monthly_quota":{"base_credits_usd":60},"intervals":[],"time_pricing":null,"protocol":"chat_completions"},
    {"platform":"opencode_go","models":["mimo-v2.5-pro"],"billing_mode":"token","input_price":4.35e-7,"output_price":8.7e-7,"cache_read_price":3.625e-9,"cache_write_price":0,"monthly_quota":{"base_credits_usd":15},"intervals":[],"time_pricing":null,"protocol":"chat_completions"},
    {"platform":"opencode_go","models":["minimax-m3"],"billing_mode":"token","input_price":3e-7,"output_price":0.0000012,"cache_read_price":6e-8,"cache_write_price":0,"monthly_quota":{"base_credits_usd":60},"intervals":[],"time_pricing":null,"protocol":"anthropic"},
    {"platform":"opencode_go","models":["minimax-m2.7"],"billing_mode":"token","input_price":3e-7,"output_price":0.0000012,"cache_read_price":6e-8,"cache_write_price":3.75e-7,"monthly_quota":{"base_credits_usd":60},"intervals":[],"time_pricing":null,"protocol":"anthropic"},
    {"platform":"opencode_go","models":["muse-spark-1.3-contributor"],"billing_mode":"token","input_price":1.0000000000000001e-7,"output_price":2.0000000000000002e-7,"cache_read_price":2e-9,"cache_write_price":0,"monthly_quota":{"base_credits_usd":60},"intervals":[],"time_pricing":null,"protocol":"responses"},
    {"platform":"opencode_go","models":["muse-spark-1.2-contributor"],"billing_mode":"token","input_price":1.0000000000000001e-7,"output_price":2.0000000000000002e-7,"cache_read_price":2e-9,"cache_write_price":0,"monthly_quota":{"base_credits_usd":60},"intervals":[],"time_pricing":null,"protocol":"responses"},
    {"platform":"opencode_go","models":["qwen3.8-max"],"billing_mode":"token","input_price":0.000002,"output_price":0.000006,"cache_read_price":2.5e-7,"cache_write_price":0.0000025,"monthly_quota":{"base_credits_usd":15},"intervals":[],"time_pricing":null,"protocol":"anthropic"},
    {"platform":"opencode_go","models":["qwen3.8-flash"],"billing_mode":"token","input_price":1.5e-7,"output_price":4.6999999999999995e-7,"cache_read_price":1.6e-8,"cache_write_price":2.0000000000000002e-7,"monthly_quota":{"base_credits_usd":30},"intervals":[],"time_pricing":null,"protocol":"anthropic"},
    {"platform":"opencode_go","models":["qwen3.7-plus"],"billing_mode":"token","input_price":4.0000000000000003e-7,"output_price":0.0000016000000000000001,"cache_read_price":4e-8,"cache_write_price":5e-7,"monthly_quota":{"base_credits_usd":60},"intervals":[{"min_tokens":0,"max_tokens":256000,"input_price":4.0000000000000003e-7,"output_price":0.0000016000000000000001,"cache_read_price":4e-8,"cache_write_price":5e-7,"sort_order":0},{"min_tokens":256000,"max_tokens":null,"input_price":0.0000012,"output_price":0.0000048,"cache_read_price":1.2e-7,"cache_write_price":0.0000015,"sort_order":1}],"time_pricing":null,"protocol":"anthropic"},
    {"platform":"opencode_go","models":["hy4-preview"],"billing_mode":"token","input_price":8.34e-7,"output_price":0.000002501,"cache_read_price":4.2000000000000006e-8,"cache_write_price":0,"monthly_quota":{"base_credits_usd":30},"intervals":[],"time_pricing":null,"protocol":"chat_completions"},
    {"platform":"opencode_go","models":["hy3"],"billing_mode":"token","input_price":1.4e-7,"output_price":5.8e-7,"cache_read_price":3.5e-8,"cache_write_price":0,"monthly_quota":{"base_credits_usd":60},"intervals":[],"time_pricing":null,"protocol":"chat_completions"},
    {"platform":"opencode_go","models":["space-bunny"],"billing_mode":"token","input_price":1.5e-7,"output_price":6e-7,"cache_read_price":3e-8,"cache_write_price":0,"monthly_quota":{"base_credits_usd":30},"intervals":[],"time_pricing":null,"protocol":"chat_completions"}
]
$seed$::JSONB;
BEGIN
    SELECT id INTO billing_channel_id FROM channels
    WHERE features_config->>'opencode_channel_billing' = 'true' ORDER BY id LIMIT 1;
    IF billing_channel_id IS NOT NULL THEN
        RETURN;
    END IF;
    WHILE EXISTS (SELECT 1 FROM channels WHERE name = billing_channel_name) LOOP
        suffix := suffix + 1;
        billing_channel_name := 'OpenCode (' || suffix || ')';
    END LOOP;
    INSERT INTO channels (name, description, status, billing_model_source, restrict_models, features_config)
    VALUES (billing_channel_name, 'OpenCode Go 官方模型定价', 'active', 'upstream', TRUE, '{"opencode_channel_billing":true}'::JSONB)
    RETURNING id INTO billing_channel_id;

    WITH inserted_pricing AS (
        INSERT INTO channel_model_pricing (channel_id, platform, models, billing_mode, input_price, output_price, cache_write_price, cache_read_price, time_pricing, monthly_quota)
        SELECT billing_channel_id, 'opencode_go', entry->'models', 'token',
               (entry->>'input_price')::NUMERIC, (entry->>'output_price')::NUMERIC,
               (entry->>'cache_write_price')::NUMERIC, (entry->>'cache_read_price')::NUMERIC,
               NULLIF(entry->'time_pricing', 'null'::JSONB), NULLIF(entry->'monthly_quota', 'null'::JSONB)
        FROM jsonb_array_elements(seed) AS entry
        RETURNING id, models
    )
    INSERT INTO channel_pricing_intervals (pricing_id, min_tokens, max_tokens, tier_label, input_price, output_price, cache_write_price, cache_read_price, sort_order)
    SELECT pricing.id, (tier->>'min_tokens')::INTEGER, (tier->>'max_tokens')::INTEGER, '',
           (tier->>'input_price')::NUMERIC, (tier->>'output_price')::NUMERIC,
           (tier->>'cache_write_price')::NUMERIC, (tier->>'cache_read_price')::NUMERIC,
           (tier->>'sort_order')::INTEGER
    FROM inserted_pricing AS pricing
    JOIN jsonb_array_elements(seed) AS entry ON entry->'models' = pricing.models
    CROSS JOIN LATERAL jsonb_array_elements(entry->'intervals') AS tier;

    INSERT INTO channel_groups (channel_id, group_id)
    SELECT billing_channel_id, groups.id FROM groups
    WHERE groups.platform = 'opencode_go' AND groups.deleted_at IS NULL
      AND NOT EXISTS (SELECT 1 FROM channel_groups WHERE group_id = groups.id)
    ON CONFLICT (group_id) DO NOTHING;

    INSERT INTO channel_account_stats_pricing_rules (channel_id, name, pricing_source, platform, sort_order)
    VALUES (billing_channel_id, '渠道原价 × 模型特有倍率', 'channel_model_multiplier', 'opencode_go', 0);
END
$migration$;

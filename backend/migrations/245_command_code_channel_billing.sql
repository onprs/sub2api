-- 将项目现有 Command Code 规则迁入原生渠道计费，保留其他渠道和已有关联。
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '10min';

ALTER TABLE channel_model_pricing ADD COLUMN IF NOT EXISTS monthly_quota JSONB;
COMMENT ON COLUMN channel_model_pricing.monthly_quota IS '模型基础月额度及限时额度活动，用于 Command Code 额度折算';

DO $migration$
DECLARE
    billing_channel_id BIGINT;
    billing_channel_name TEXT := 'Command Code';
    suffix INTEGER := 1;
    seed JSONB := $seed$
[
    {"model":"claude-sonnet-5-5","input":2,"output":10,"cache_read":0.1,"cache_write":2.5,"monthly_quota":{"base_credits_usd":10},"intervals":[],"time_pricing":null},
    {"model":"claude-haiku-5-5","input":0.1,"output":0.5,"cache_read":0.01,"cache_write":0.125,"monthly_quota":{"base_credits_usd":20},"intervals":[{"min_tokens":0,"max_tokens":100000,"input":0.1,"output":0.5,"cache_read":0.01,"cache_write":0.125,"sort_order":0},{"min_tokens":100000,"max_tokens":null,"input":0.5,"output":2.5,"cache_read":0.05,"cache_write":0.625,"sort_order":1}],"time_pricing":null},
    {"model":"gpt-5.6-sol","input":5,"output":30,"cache_read":0.5,"cache_write":6.25,"monthly_quota":{"base_credits_usd":70},"intervals":[{"min_tokens":0,"max_tokens":272000,"input":5,"output":30,"cache_read":0.5,"cache_write":6.25,"sort_order":0},{"min_tokens":272000,"max_tokens":null,"input":10,"output":45,"cache_read":1,"cache_write":12.5,"sort_order":1}],"time_pricing":null},
    {"model":"gpt-5.6-luna","input":0.2,"output":1.2,"cache_read":0.02,"cache_write":0.25,"monthly_quota":{"base_credits_usd":20},"intervals":[{"min_tokens":0,"max_tokens":272000,"input":0.2,"output":1.2,"cache_read":0.02,"cache_write":0.25,"sort_order":0},{"min_tokens":272000,"max_tokens":null,"input":0.4,"output":1.8,"cache_read":0.04,"cache_write":0.5,"sort_order":1}],"time_pricing":null},
    {"model":"gpt-6-luna","input":0.1,"output":0.5,"cache_read":0.01,"cache_write":0.125,"monthly_quota":{"base_credits_usd":20},"intervals":[{"min_tokens":0,"max_tokens":272000,"input":0.1,"output":0.5,"cache_read":0.01,"cache_write":0.125,"sort_order":0},{"min_tokens":272000,"max_tokens":null,"input":0.2,"output":0.75,"cache_read":0.02,"cache_write":0.25,"sort_order":1}],"time_pricing":null},
    {"model":"stealth/glyph-cluster:free","input":0,"output":0,"cache_read":0,"cache_write":null,"monthly_quota":{"base_credits_usd":0},"intervals":[],"time_pricing":null},
    {"model":"google/gemini-3.8-flash","input":1.5,"output":7.5,"cache_read":0.15,"cache_write":null,"monthly_quota":{"base_credits_usd":40},"intervals":[],"time_pricing":null},
    {"model":"google/gemini-3.7-flash","input":1.5,"output":7.5,"cache_read":0.15,"cache_write":0.08334,"monthly_quota":{"base_credits_usd":40},"intervals":[],"time_pricing":null},
    {"model":"xai/grok-4.7","input":2,"output":6,"cache_read":0.5,"cache_write":null,"monthly_quota":{"base_credits_usd":20},"intervals":[{"min_tokens":0,"max_tokens":200000,"input":2,"output":6,"cache_read":0.5,"cache_write":null,"sort_order":0},{"min_tokens":200000,"max_tokens":null,"input":4,"output":12,"cache_read":1,"cache_write":null,"sort_order":1}],"time_pricing":null},
    {"model":"xai/grok-4.6","input":2,"output":6,"cache_read":0.5,"cache_write":null,"monthly_quota":{"base_credits_usd":20},"intervals":[{"min_tokens":0,"max_tokens":200000,"input":2,"output":6,"cache_read":0.5,"cache_write":null,"sort_order":0},{"min_tokens":200000,"max_tokens":null,"input":4,"output":12,"cache_read":1,"cache_write":null,"sort_order":1}],"time_pricing":null},
    {"model":"xai/grok-4.5","input":2,"output":6,"cache_read":0.5,"cache_write":null,"monthly_quota":{"base_credits_usd":20},"intervals":[],"time_pricing":null},
    {"model":"meta/muse-spark-1.3","input":1.25,"output":4.25,"cache_read":0.15,"cache_write":null,"monthly_quota":{"base_credits_usd":20},"intervals":[],"time_pricing":null},
    {"model":"meta/muse-spark-1.3-contributor","input":0.1,"output":0.2,"cache_read":0.002,"cache_write":null,"monthly_quota":{"base_credits_usd":20},"intervals":[],"time_pricing":null},
    {"model":"meta/muse-spark-1.2","input":1.25,"output":4.25,"cache_read":0.15,"cache_write":null,"monthly_quota":{"base_credits_usd":20},"intervals":[],"time_pricing":null},
    {"model":"meta/muse-spark-1.2-contributor","input":0.1,"output":0.2,"cache_read":0.002,"cache_write":null,"monthly_quota":{"base_credits_usd":20},"intervals":[],"time_pricing":null},
    {"model":"deepseek/deepseek-v4-pro","input":0.66,"output":1.98,"cache_read":0.022,"cache_write":null,"monthly_quota":{"base_credits_usd":20},"intervals":[],"time_pricing":{"timezone":"UTC","weekdays_only":true,"periods":[{"start_time":"01:00:00","end_time":"04:00:00","multiplier":2},{"start_time":"06:00:00","end_time":"10:00:00","multiplier":2}]}},
    {"model":"deepseek/deepseek-v4-flash","input":0.15,"output":0.6,"cache_read":0.003,"cache_write":null,"monthly_quota":{"base_credits_usd":60},"intervals":[],"time_pricing":{"timezone":"UTC","weekdays_only":true,"periods":[{"start_time":"01:00:00","end_time":"04:00:00","multiplier":2},{"start_time":"06:00:00","end_time":"10:00:00","multiplier":2}]}},
    {"model":"deepseek/deepseek-v4-flash-vision-exp","input":0.15,"output":0.6,"cache_read":0.003,"cache_write":null,"monthly_quota":{"base_credits_usd":20},"intervals":[],"time_pricing":{"timezone":"UTC","weekdays_only":true,"periods":[{"start_time":"01:00:00","end_time":"04:00:00","multiplier":2},{"start_time":"06:00:00","end_time":"10:00:00","multiplier":2}]}},
    {"model":"deepseek/deepseek-v4-flash-fast","input":0.28,"output":0.56,"cache_read":0.07,"cache_write":null,"monthly_quota":{"base_credits_usd":20},"intervals":[],"time_pricing":null},
    {"model":"deepseek/deepseek-v4.1-flash","input":0.15,"output":0.6,"cache_read":0.003,"cache_write":null,"monthly_quota":{"base_credits_usd":60},"intervals":[],"time_pricing":{"timezone":"UTC","weekdays_only":true,"periods":[{"start_time":"01:00:00","end_time":"04:00:00","multiplier":2},{"start_time":"06:00:00","end_time":"10:00:00","multiplier":2}]}},
    {"model":"deepseek/deepseek-v4.1-flash-fast","input":0.16,"output":0.58,"cache_read":0.016,"cache_write":null,"monthly_quota":{"base_credits_usd":60},"intervals":[],"time_pricing":{"timezone":"UTC","weekdays_only":true,"periods":[{"start_time":"01:00:00","end_time":"04:00:00","multiplier":2},{"start_time":"06:00:00","end_time":"10:00:00","multiplier":2}]}},
    {"model":"moonshotai/Kimi-K3","input":3,"output":15,"cache_read":0.3,"cache_write":null,"monthly_quota":{"base_credits_usd":20,"periods":[{"credits_usd":20,"starts_at":"2026-09-30T00:00:00Z","expires_at":"2026-10-08T00:00:00Z","term":"through Oct 7th"}]},"intervals":[],"time_pricing":null},
    {"model":"moonshotai/Kimi-K2.7-Code","input":0.95,"output":4,"cache_read":0.19,"cache_write":null,"monthly_quota":{"base_credits_usd":60},"intervals":[],"time_pricing":null},
    {"model":"moonshotai/Kimi-K2.7-Code-Highspeed","input":1.9,"output":8,"cache_read":0.38,"cache_write":null,"monthly_quota":{"base_credits_usd":20},"intervals":[],"time_pricing":null},
    {"model":"moonshotai/Kimi-K2.6","input":0.95,"output":4,"cache_read":0.16,"cache_write":null,"monthly_quota":{"base_credits_usd":20},"intervals":[],"time_pricing":null},
    {"model":"moonshotai/Kimi-K2.5","input":0.6,"output":3,"cache_read":0.1,"cache_write":null,"monthly_quota":{"base_credits_usd":20},"intervals":[],"time_pricing":null},
    {"model":"z-ai/glm-5.3-flash","input":0.15,"output":0.5,"cache_read":0.03,"cache_write":null,"monthly_quota":{"base_credits_usd":60},"intervals":[],"time_pricing":null},
    {"model":"z-ai/glm-5.3-flashx","input":0.37,"output":1.25,"cache_read":0.075,"cache_write":null,"monthly_quota":{"base_credits_usd":20},"intervals":[],"time_pricing":null},
    {"model":"zai-org/GLM-5.3","input":1.4,"output":4.4,"cache_read":0.26,"cache_write":null,"monthly_quota":{"base_credits_usd":20},"intervals":[],"time_pricing":null},
    {"model":"zai-org/GLM-5.2","input":1.4,"output":4.4,"cache_read":0.26,"cache_write":null,"monthly_quota":{"base_credits_usd":70},"intervals":[],"time_pricing":null},
    {"model":"zai-org/GLM-5.2-Fast","input":3,"output":10.25,"cache_read":0.5,"cache_write":null,"monthly_quota":{"base_credits_usd":20},"intervals":[],"time_pricing":null},
    {"model":"zai-org/GLM-5.1","input":1.4,"output":4.4,"cache_read":0.26,"cache_write":null,"monthly_quota":{"base_credits_usd":20},"intervals":[],"time_pricing":null},
    {"model":"zai-org/GLM-5","input":1,"output":3.2,"cache_read":0.2,"cache_write":null,"monthly_quota":{"base_credits_usd":20},"intervals":[],"time_pricing":null},
    {"model":"MiniMaxAI/MiniMax-M3","input":0.3,"output":1.2,"cache_read":0.06,"cache_write":null,"monthly_quota":{"base_credits_usd":47},"intervals":[{"min_tokens":0,"max_tokens":512000,"input":0.3,"output":1.2,"cache_read":0.06,"cache_write":null,"sort_order":0},{"min_tokens":512000,"max_tokens":null,"input":0.3,"output":1.2,"cache_read":0.06,"cache_write":null,"sort_order":1}],"time_pricing":null},
    {"model":"MiniMaxAI/MiniMax-M2.7","input":0.3,"output":1.2,"cache_read":0.06,"cache_write":null,"monthly_quota":{"base_credits_usd":20},"intervals":[],"time_pricing":null},
    {"model":"MiniMaxAI/MiniMax-M2.5","input":0.3,"output":1.2,"cache_read":0.03,"cache_write":null,"monthly_quota":{"base_credits_usd":20},"intervals":[],"time_pricing":null},
    {"model":"xiaomi/mimo-v2.6-pro","input":0.435,"output":0.87,"cache_read":0.0036,"cache_write":null,"monthly_quota":{"base_credits_usd":20},"intervals":[],"time_pricing":null},
    {"model":"xiaomi/mimo-v2.6-pro-ultraspeed","input":4.35,"output":8.7,"cache_read":0.036,"cache_write":null,"monthly_quota":{"base_credits_usd":10},"intervals":[],"time_pricing":null},
    {"model":"xiaomi/mimo-v2.6-flash","input":0.14,"output":0.28,"cache_read":0.0028,"cache_write":null,"monthly_quota":{"base_credits_usd":20},"intervals":[],"time_pricing":null},
    {"model":"xiaomi/mimo-v2.5-pro","input":0.435,"output":0.87,"cache_read":0.0036,"cache_write":null,"monthly_quota":{"base_credits_usd":20},"intervals":[],"time_pricing":null},
    {"model":"xiaomi/mimo-v2.5","input":0.14,"output":0.28,"cache_read":0.0028,"cache_write":null,"monthly_quota":{"base_credits_usd":30},"intervals":[],"time_pricing":null},
    {"model":"Qwen/Qwen3.8-Max-0902","input":2,"output":6,"cache_read":0.25,"cache_write":null,"monthly_quota":{"base_credits_usd":20},"intervals":[],"time_pricing":null},
    {"model":"Qwen/Qwen3.8-Max","input":2,"output":6,"cache_read":0.25,"cache_write":2.5,"monthly_quota":{"base_credits_usd":20},"intervals":[],"time_pricing":null},
    {"model":"Qwen/Qwen3.8-27B","input":0.4,"output":3,"cache_read":0.04,"cache_write":null,"monthly_quota":{"base_credits_usd":70},"intervals":[],"time_pricing":null},
    {"model":"Qwen/Qwen3.8-Omni-Flash","input":0.15,"output":0.47,"cache_read":0.016,"cache_write":null,"monthly_quota":{"base_credits_usd":20},"intervals":[],"time_pricing":null},
    {"model":"Qwen/Qwen3.8-Flash","input":0.16,"output":0.47,"cache_read":0.016,"cache_write":null,"monthly_quota":{"base_credits_usd":20},"intervals":[],"time_pricing":null},
    {"model":"Qwen/Qwen3.7-Max","input":2.5,"output":7.5,"cache_read":0.5,"cache_write":3.13,"monthly_quota":{"base_credits_usd":33},"intervals":[],"time_pricing":null},
    {"model":"Qwen/Qwen3.7-Plus","input":0.4,"output":1.6,"cache_read":0.08,"cache_write":0.5,"monthly_quota":{"base_credits_usd":33},"intervals":[{"min_tokens":0,"max_tokens":256000,"input":0.4,"output":1.6,"cache_read":0.08,"cache_write":0.5,"sort_order":0},{"min_tokens":256000,"max_tokens":null,"input":1.2,"output":4.8,"cache_read":0.24,"cache_write":1.5,"sort_order":1}],"time_pricing":null},
    {"model":"Qwen/Qwen3.7-Flash","input":0.03,"output":0.13,"cache_read":0.006,"cache_write":0.038,"monthly_quota":{"base_credits_usd":20},"intervals":[{"min_tokens":0,"max_tokens":32000,"input":0.03,"output":0.13,"cache_read":0.006,"cache_write":0.038,"sort_order":0},{"min_tokens":32000,"max_tokens":256000,"input":0.1,"output":0.4,"cache_read":0.02,"cache_write":0.125,"sort_order":1},{"min_tokens":256000,"max_tokens":null,"input":0.2,"output":0.8,"cache_read":0.04,"cache_write":0.25,"sort_order":2}],"time_pricing":null},
    {"model":"Qwen/Qwen3.6-Max-Preview","input":1.3,"output":7.8,"cache_read":0.26,"cache_write":1.63,"monthly_quota":{"base_credits_usd":20},"intervals":[],"time_pricing":null},
    {"model":"Qwen/Qwen3.6-Plus","input":0.5,"output":3,"cache_read":0.1,"cache_write":null,"monthly_quota":{"base_credits_usd":33},"intervals":[{"min_tokens":0,"max_tokens":256000,"input":0.5,"output":3,"cache_read":0.1,"cache_write":null,"sort_order":0},{"min_tokens":256000,"max_tokens":null,"input":2,"output":6,"cache_read":0.2,"cache_write":null,"sort_order":1}],"time_pricing":null},
    {"model":"stepfun/Step-5-Preview","input":1,"output":2.7,"cache_read":0.05,"cache_write":null,"monthly_quota":{"base_credits_usd":20},"intervals":[],"time_pricing":null},
    {"model":"stepfun/Step-3.7-Flash","input":0.2,"output":1.15,"cache_read":0.04,"cache_write":null,"monthly_quota":{"base_credits_usd":20},"intervals":[],"time_pricing":null},
    {"model":"stepfun/Step-3.5-Flash","input":0.09,"output":0.3,"cache_read":0.02,"cache_write":null,"monthly_quota":{"base_credits_usd":20},"intervals":[],"time_pricing":null},
    {"model":"tencent/hy3-paid","input":0.14,"output":0.58,"cache_read":0.035,"cache_write":null,"monthly_quota":{"base_credits_usd":70},"intervals":[],"time_pricing":null},
    {"model":"tencent/hy4-preview","input":0.834,"output":2.501,"cache_read":0.042,"cache_write":null,"monthly_quota":{"base_credits_usd":20},"intervals":[],"time_pricing":null},
    {"model":"meituan/LongCat-2.0","input":0.3,"output":1.2,"cache_read":0.006,"cache_write":null,"monthly_quota":{"base_credits_usd":50},"intervals":[],"time_pricing":null},
    {"model":"mistral/mistral-large-4","input":1.36,"output":4.18,"cache_read":0.14,"cache_write":null,"monthly_quota":{"base_credits_usd":20,"periods":[{"credits_usd":40,"starts_at":"2026-10-06T00:00:00Z","expires_at":"2026-10-21T00:00:00Z","term":"through Oct 20th"}]},"intervals":[],"time_pricing":null},
    {"model":"inclusionai/ling-3.0-flash-sante:free","input":0,"output":0,"cache_read":0,"cache_write":null,"monthly_quota":{"base_credits_usd":0},"intervals":[],"time_pricing":null},
    {"model":"inclusionai/ling-3.1-flash:free","input":0,"output":0,"cache_read":0,"cache_write":null,"monthly_quota":{"base_credits_usd":0},"intervals":[],"time_pricing":null},
    {"model":"nvidia/nemotron-3-ultra-550b-a55b","input":0.6,"output":2.4,"cache_read":0.12,"cache_write":null,"monthly_quota":{"base_credits_usd":20},"intervals":[],"time_pricing":null},
    {"model":"thinkingmachines/inkling","input":1,"output":4.05,"cache_read":0.17,"cache_write":null,"monthly_quota":{"base_credits_usd":20},"intervals":[],"time_pricing":null},
    {"model":"thinkingmachines/inkling-small","input":0.5,"output":1.2,"cache_read":0.1,"cache_write":null,"monthly_quota":{"base_credits_usd":20},"intervals":[],"time_pricing":null},
    {"model":"poolside/laguna-s-2.1-free","input":0,"output":0,"cache_read":0,"cache_write":null,"monthly_quota":{"base_credits_usd":0},"intervals":[],"time_pricing":null}
]
$seed$::jsonb;
BEGIN
    -- 重复执行时保留管理员已经保存的渠道规则。
    IF EXISTS (SELECT 1 FROM channels WHERE features_config->>'command_code_billing' = 'true') THEN
        RETURN;
    END IF;
    WHILE EXISTS (SELECT 1 FROM channels WHERE name = billing_channel_name) LOOP
        suffix := suffix + 1;
        billing_channel_name := 'Command Code ' || suffix;
    END LOOP;
    INSERT INTO channels (name, description, status, billing_model_source, restrict_models, features_config)
    VALUES (billing_channel_name, 'Command Code 模型计费', 'active', 'requested', TRUE, '{"command_code_billing":true}'::jsonb)
    RETURNING id INTO billing_channel_id;

    WITH inserted_pricing AS (
        INSERT INTO channel_model_pricing (channel_id, platform, models, billing_mode, input_price, output_price, cache_write_price, cache_read_price, time_pricing, monthly_quota)
        SELECT billing_channel_id, 'command_code', jsonb_build_array(entry->>'model'), 'token',
               (entry->>'input')::NUMERIC / 1000000,
               (entry->>'output')::NUMERIC / 1000000,
               (entry->>'cache_write')::NUMERIC / 1000000,
               (entry->>'cache_read')::NUMERIC / 1000000,
               NULLIF(entry->'time_pricing', 'null'::jsonb), entry->'monthly_quota'
        FROM jsonb_array_elements(seed) AS entry
        RETURNING id, models
    )
    INSERT INTO channel_pricing_intervals (pricing_id, min_tokens, max_tokens, tier_label, input_price, output_price, cache_write_price, cache_read_price, sort_order)
    SELECT pricing.id, (tier->>'min_tokens')::INTEGER, (tier->>'max_tokens')::INTEGER, '',
           (tier->>'input')::NUMERIC / 1000000, (tier->>'output')::NUMERIC / 1000000,
           (tier->>'cache_write')::NUMERIC / 1000000, (tier->>'cache_read')::NUMERIC / 1000000,
           (tier->>'sort_order')::INTEGER
    FROM inserted_pricing AS pricing
    JOIN jsonb_array_elements(seed) AS entry ON entry->>'model' = pricing.models->>0
    CROSS JOIN LATERAL jsonb_array_elements(entry->'intervals') AS tier;

    -- 仅接入尚未归属渠道的 Command Code 分组。
    INSERT INTO channel_groups (channel_id, group_id)
    SELECT billing_channel_id, groups.id FROM groups
    WHERE groups.platform = 'command_code' AND groups.deleted_at IS NULL
      AND NOT EXISTS (SELECT 1 FROM channel_groups WHERE group_id = groups.id)
    ON CONFLICT (group_id) DO NOTHING;
END
$migration$;

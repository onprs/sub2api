-- 同步 OpenCode 与 Command Code 2026-11-16 官方目录及渠道计费：
--   * OpenCode Go：新增 qwen4-max（$1.50/$4.50/$0.075/$1.875，月额度 $15，Anthropic 协议）与
--     hy4-flash-free（官方免费模型，按本站统一免费价 1e-9 计费，Chat Completions 协议）；
--     修正 step-5-preview 缓存读取价至官方 $0.10/M（仅更新保留旧默认 $0.05/M 的原生渠道条目），
--     并为使用默认 minimax-* 规则的 Go 自适应账号补充 minimax-m3.1-flash -> chat_completions 前置规则。
--   * Command Code GOAT：新增 Qwen/Qwen4-Max（$1.50/$4.50/$0.075/$1.875，月额度 $15）、
--     tencent/hy4-flash（$0.07/$0.281/$0.003，月额度 $15）与
--     thinkingmachines/inkling-2-small:free（官方免费模型，1e-9 固定免费价，月额度置空）；
--     更新 zai-org/GLM-5.3 月额度为 $70。
-- 仅更新带对应计费标记的原生渠道，保留自定义价卡、其它模型和历史账单。
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '10min';

DO $migration$
DECLARE
    opencode_additions JSONB := $opencode_additions$
[
  {"platform":"opencode_go","models":["qwen4-max"],"billing_mode":"token","input_price":0.0000015,"output_price":0.0000045,"cache_read_price":0.000000075,"cache_write_price":0.000001875,"monthly_quota":{"base_credits_usd":15},"intervals":[],"time_pricing":null},
  {"platform":"opencode_go","models":["hy4-flash-free"],"billing_mode":"token","input_price":0.000000001,"output_price":0.000000001,"cache_read_price":0.000000001,"cache_write_price":0.000000001,"monthly_quota":null,"intervals":[],"time_pricing":null}
]
$opencode_additions$::JSONB;

    opencode_price_updates JSONB := $opencode_price_updates$
[
  {"models":["step-5-preview"],"cache_read_price":0.0000001,"expected_old_cache_read_price":0.00000005}
]
$opencode_price_updates$::JSONB;

    commandcode_additions JSONB := $commandcode_additions$
[
  {"model":"Qwen/Qwen4-Max","input":1.5,"output":4.5,"cache_read":0.075,"cache_write":1.875,"time_pricing":null,"monthly_quota":{"base_credits_usd":15}},
  {"model":"tencent/hy4-flash","input":0.07,"output":0.281,"cache_read":0.003,"cache_write":0,"time_pricing":null,"monthly_quota":{"base_credits_usd":15}},
  {"model":"thinkingmachines/inkling-2-small:free","input":0.001,"output":0.001,"cache_read":0.001,"cache_write":0.001,"time_pricing":null,"monthly_quota":null}
]
$commandcode_additions$::JSONB;

    commandcode_quotas JSONB := $commandcode_quotas$
{
  "zai-org/GLM-5.3": {"base_credits_usd": 70}
}
$commandcode_quotas$::JSONB;
BEGIN
    -- OpenCode：仅插入尚未存在的官方新模型，保留自定义价格与已下架兼容模型。
    INSERT INTO channel_model_pricing (
        channel_id, platform, models, billing_mode, input_price, output_price,
        cache_write_price, cache_read_price, time_pricing, monthly_quota
    )
    SELECT channel.id, 'opencode_go', entry->'models', 'token',
           (entry->>'input_price')::NUMERIC, (entry->>'output_price')::NUMERIC,
           (entry->>'cache_write_price')::NUMERIC, (entry->>'cache_read_price')::NUMERIC,
           NULLIF(entry->'time_pricing', 'null'::JSONB), NULLIF(entry->'monthly_quota', 'null'::JSONB)
    FROM channels channel
    CROSS JOIN LATERAL jsonb_array_elements(opencode_additions) entry
    WHERE channel.features_config->>'opencode_channel_billing' = 'true'
      AND NOT EXISTS (
          SELECT 1 FROM channel_model_pricing existing
          WHERE existing.channel_id = channel.id AND existing.platform = 'opencode_go'
            AND existing.models @> (entry->'models')
      );

    -- OpenCode：hy4-flash-free 与既有免费模型保持 1e-9 固定免费价并清理区间。
    UPDATE channel_model_pricing pricing
    SET input_price = 0.000000001,
        output_price = 0.000000001,
        cache_write_price = 0.000000001,
        cache_write_1h_price = 0.000000001,
        cache_read_price = 0.000000001,
        image_input_price = 0.000000001,
        image_output_price = 0.000000001,
        fast_multiplier = 1,
        flex_multiplier = 1,
        max_reasoning_effort_multiplier = 1,
        reasoning_effort_multipliers = '{}'::JSONB,
        monthly_quota = NULL,
        time_pricing = NULL,
        updated_at = NOW()
    FROM channels channel
    WHERE pricing.channel_id = channel.id
      AND channel.features_config->>'opencode_channel_billing' = 'true'
      AND pricing.platform = 'opencode_go'
      AND pricing.models = '["hy4-flash-free"]'::JSONB
      AND COALESCE(pricing.billing_mode, 'token') = 'token';

    DELETE FROM channel_pricing_intervals intervals
    USING channel_model_pricing pricing, channels channel
    WHERE intervals.pricing_id = pricing.id
      AND pricing.channel_id = channel.id
      AND channel.features_config->>'opencode_channel_billing' = 'true'
      AND pricing.platform = 'opencode_go'
      AND pricing.models = '["hy4-flash-free"]'::JSONB;

    -- OpenCode：仅当 cache_read_price 仍为旧官方默认值时同步为 $0.10/M，保留管理员自定义修改。
    UPDATE channel_model_pricing pricing
    SET cache_read_price = COALESCE((entry->>'cache_read_price')::NUMERIC, pricing.cache_read_price),
        updated_at = NOW()
    FROM channels channel
    CROSS JOIN LATERAL jsonb_array_elements(opencode_price_updates) entry
    WHERE pricing.channel_id = channel.id
      AND channel.features_config->>'opencode_channel_billing' = 'true'
      AND pricing.platform = 'opencode_go'
      AND pricing.models @> (entry->'models')
      AND pricing.cache_read_price = (entry->>'expected_old_cache_read_price')::NUMERIC;

    -- OpenCode Go 账号：仅在仍使用 minimax-* -> anthropic 且未显式配置 minimax-m3.1-flash 规则时，
    -- 在 minimax-* 之前插入 minimax-m3.1-flash -> chat_completions，对齐官方 /docs/go/ 端点表。
    UPDATE accounts
    SET credentials = jsonb_set(
            credentials,
            '{protocol_rules}',
            (
                SELECT jsonb_agg(rule ORDER BY ord, sub_ord)
                FROM (
                    SELECT
                        elem AS rule,
                        idx AS ord,
                        1 AS sub_ord
                    FROM jsonb_array_elements(credentials->'protocol_rules') WITH ORDINALITY AS t(elem, idx)
                    UNION ALL
                    SELECT
                        '{"pattern":"minimax-m3.1-flash","protocol":"chat_completions"}'::JSONB AS rule,
                        idx AS ord,
                        0 AS sub_ord
                    FROM jsonb_array_elements(credentials->'protocol_rules') WITH ORDINALITY AS t(elem, idx)
                    WHERE elem->>'pattern' = 'minimax-*' AND elem->>'protocol' = 'anthropic'
                ) rebuilt
            )
        ),
        updated_at = NOW()
    WHERE platform = 'opencode_go'
      AND deleted_at IS NULL
      AND COALESCE(NULLIF(credentials->>'account_mode', ''), 'go') = 'go'
      AND credentials->>'api_protocol' = 'adaptive'
      AND jsonb_typeof(credentials->'protocol_rules') = 'array'
      AND credentials->'protocol_rules' @> '[{"pattern":"minimax-*","protocol":"anthropic"}]'::JSONB
      AND NOT EXISTS (
          SELECT 1
          FROM jsonb_array_elements(credentials->'protocol_rules') AS existing_rule
          WHERE existing_rule->>'pattern' = 'minimax-m3.1-flash'
      );

    -- Command Code：仅插入尚未存在的官方新模型。
    INSERT INTO channel_model_pricing (
        channel_id, platform, models, billing_mode, input_price, output_price,
        cache_write_price, cache_read_price, time_pricing, monthly_quota
    )
    SELECT channel.id, 'command_code', jsonb_build_array(entry->>'model'), 'token',
           (entry->>'input')::NUMERIC / 1000000,
           (entry->>'output')::NUMERIC / 1000000,
           (entry->>'cache_write')::NUMERIC / 1000000,
           (entry->>'cache_read')::NUMERIC / 1000000,
           NULLIF(entry->'time_pricing', 'null'::JSONB), NULLIF(entry->'monthly_quota', 'null'::JSONB)
    FROM channels channel
    CROSS JOIN LATERAL jsonb_array_elements(commandcode_additions) entry
    WHERE channel.features_config->>'command_code_billing' = 'true'
      AND NOT EXISTS (
          SELECT 1 FROM channel_model_pricing existing
          WHERE existing.channel_id = channel.id AND existing.platform = 'command_code'
            AND existing.models @> jsonb_build_array(entry->>'model')
      );

    -- Command Code：免费模型 thinkingmachines/inkling-2-small:free 统一 1e-9 固定免费价并清理区间。
    UPDATE channel_model_pricing pricing
    SET input_price = 0.000000001,
        output_price = 0.000000001,
        cache_write_price = 0.000000001,
        cache_write_1h_price = 0.000000001,
        cache_read_price = 0.000000001,
        image_input_price = 0.000000001,
        image_output_price = 0.000000001,
        fast_multiplier = 1,
        flex_multiplier = 1,
        max_reasoning_effort_multiplier = 1,
        reasoning_effort_multipliers = '{}'::JSONB,
        monthly_quota = NULL,
        time_pricing = NULL,
        updated_at = NOW()
    FROM channels channel
    WHERE pricing.channel_id = channel.id
      AND channel.features_config->>'command_code_billing' = 'true'
      AND pricing.platform = 'command_code'
      AND pricing.models = '["thinkingmachines/inkling-2-small:free"]'::JSONB
      AND COALESCE(pricing.billing_mode, 'token') = 'token';

    DELETE FROM channel_pricing_intervals intervals
    USING channel_model_pricing pricing, channels channel
    WHERE intervals.pricing_id = pricing.id
      AND pricing.channel_id = channel.id
      AND channel.features_config->>'command_code_billing' = 'true'
      AND pricing.platform = 'command_code'
      AND pricing.models = '["thinkingmachines/inkling-2-small:free"]'::JSONB;

    -- Command Code：按官方最新月额度更新已存在模型。
    UPDATE channel_model_pricing pricing
    SET monthly_quota = quota.value,
        updated_at = NOW()
    FROM channels channel
    CROSS JOIN LATERAL jsonb_each(commandcode_quotas) AS quota(key, value)
    WHERE pricing.channel_id = channel.id
      AND channel.features_config->>'command_code_billing' = 'true'
      AND pricing.platform = 'command_code'
      AND pricing.models = jsonb_build_array(quota.key);
END
$migration$;

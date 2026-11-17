-- 同步 Command Code 2026-11-17 官方 GOAT 目录新增模型：
--   * recursive/flint-v1.0:free：官方免费模型，按本站统一免费价 1e-9 计费，月额度置空。
--   * tencent/hy4：$0.834/$2.501/$0.042 每百万 token，月额度 $20。
-- 仅更新带官方渠道计费标记的原生 Command Code 渠道，保留自定义价卡、其它模型和历史账单。
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '10min';

DO $migration$
DECLARE
    commandcode_additions JSONB := $commandcode_additions$
[
  {"model":"recursive/flint-v1.0:free","input":0.001,"output":0.001,"cache_read":0.001,"cache_write":0.001,"monthly_quota":null},
  {"model":"tencent/hy4","input":0.834,"output":2.501,"cache_read":0.042,"cache_write":0,"monthly_quota":{"base_credits_usd":20}}
]
$commandcode_additions$::JSONB;
BEGIN
    INSERT INTO channel_model_pricing (
        channel_id, platform, models, billing_mode, input_price, output_price,
        cache_write_price, cache_read_price, time_pricing, monthly_quota
    )
    SELECT channel.id, 'command_code', jsonb_build_array(entry->>'model'), 'token',
           (entry->>'input')::NUMERIC / 1000000,
           (entry->>'output')::NUMERIC / 1000000,
           (entry->>'cache_write')::NUMERIC / 1000000,
           (entry->>'cache_read')::NUMERIC / 1000000,
           NULL, NULLIF(entry->'monthly_quota', 'null'::JSONB)
    FROM channels channel
    CROSS JOIN LATERAL jsonb_array_elements(commandcode_additions) entry
    WHERE channel.features_config->>'command_code_billing' = 'true'
      AND NOT EXISTS (
          SELECT 1 FROM channel_model_pricing existing
          WHERE existing.channel_id = channel.id AND existing.platform = 'command_code'
            AND existing.models @> jsonb_build_array(entry->>'model')
      );

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
      AND pricing.models = '["recursive/flint-v1.0:free"]'::JSONB
      AND COALESCE(pricing.billing_mode, 'token') = 'token';

    DELETE FROM channel_pricing_intervals intervals
    USING channel_model_pricing pricing, channels channel
    WHERE intervals.pricing_id = pricing.id
      AND pricing.channel_id = channel.id
      AND channel.features_config->>'command_code_billing' = 'true'
      AND pricing.platform = 'command_code'
      AND pricing.models = '["recursive/flint-v1.0:free"]'::JSONB;
END
$migration$;

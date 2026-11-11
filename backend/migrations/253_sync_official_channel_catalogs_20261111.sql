-- 同步 2026-11-11 官方目录：新增模型、下架模型、免费模型与额度变化。
-- OpenCode Go: https://opencode.ai/docs/go/
-- Command Code: https://commandcode.ai/docs/plans/goat
-- 仅更新对应的原生计费渠道，保留自定义渠道、历史账单和已有关联。
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '10min';

DO $migration$
DECLARE
    opencode_additions JSONB := $opencode_additions$
[
  {"platform":"opencode_go","models":["hy4"],"billing_mode":"token","input_price":8.34e-7,"output_price":0.000002501,"cache_read_price":4.2000000000000006e-8,"cache_write_price":0,"monthly_quota":{"base_credits_usd":60},"intervals":[],"time_pricing":null},
  {"platform":"opencode_go","models":["longcat-2.5-free"],"billing_mode":"token","input_price":0.000000001,"output_price":0.000000001,"cache_read_price":0.000000001,"cache_write_price":0.000000001,"monthly_quota":null,"intervals":[],"time_pricing":null}
]
$opencode_additions$::JSONB;
    opencode_price_updates JSONB := $opencode_price_updates$
[
  {"models":["kimi-k3"],"cache_write_price":0.000003},
  {"models":["deepseek-v4.1-flash"],"monthly_quota":{"base_credits_usd":100}}
]
$opencode_price_updates$::JSONB;
    commandcode_retired JSONB := $commandcode_retired$
["reflection/beam:free","poolside/laguna-s-2.1-free"]
$commandcode_retired$::JSONB;
    commandcode_additions JSONB := $commandcode_additions$
[
  {"model":"deepseek/deepseek-v4.1-flash-ultrafast","input":2.4,"output":9.6,"cache_read":0.048,"cache_write":null,"monthly_quota":{"base_credits_usd":20},"time_pricing":null},
  {"model":"inclusionai/ling-3.1-flash-1019","input":0.3,"output":0.9,"cache_read":0.06,"cache_write":null,"monthly_quota":{"base_credits_usd":100},"time_pricing":null},
  {"model":"meta/muse-ember","input":0.1,"output":0.5,"cache_read":0.004,"cache_write":null,"monthly_quota":{"base_credits_usd":20},"time_pricing":null}
]
$commandcode_additions$::JSONB;
    commandcode_quotas JSONB := $commandcode_quotas$
{
  "deepseek/deepseek-v4.1-flash": {"base_credits_usd":100},
  "z-ai/glm-5.3-flash": {"base_credits_usd":100}
}
$commandcode_quotas$::JSONB;
BEGIN
    -- OpenCode：仅插入尚未存在的官方新模型，保留管理员已保存的价卡。
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

    -- 新增的免费模型与固定免费价格保持一致。
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
      AND pricing.models = '["longcat-2.5-free"]'::JSONB
      AND COALESCE(pricing.billing_mode, 'token') = 'token';

    DELETE FROM channel_pricing_intervals intervals
    USING channel_model_pricing pricing, channels channel
    WHERE intervals.pricing_id = pricing.id
      AND pricing.channel_id = channel.id
      AND channel.features_config->>'opencode_channel_billing' = 'true'
      AND pricing.platform = 'opencode_go'
      AND pricing.models = '["longcat-2.5-free"]'::JSONB;

    -- OpenCode：按官方最新价格只更新对应字段，保留其它自定义计费配置。
    UPDATE channel_model_pricing pricing
    SET cache_write_price = COALESCE((entry->>'cache_write_price')::NUMERIC, pricing.cache_write_price),
        monthly_quota = COALESCE(NULLIF(entry->'monthly_quota', 'null'::JSONB), pricing.monthly_quota),
        updated_at = NOW()
    FROM channels channel
    CROSS JOIN LATERAL jsonb_array_elements(opencode_price_updates) entry
    WHERE pricing.channel_id = channel.id
      AND channel.features_config->>'opencode_channel_billing' = 'true'
      AND pricing.platform = 'opencode_go'
      AND pricing.models @> (entry->'models');

    -- Command Code：删除下架模型及其区间。
    DELETE FROM channel_pricing_intervals intervals
    USING channel_model_pricing pricing, channels channel
    WHERE intervals.pricing_id = pricing.id
      AND pricing.channel_id = channel.id
      AND channel.features_config->>'command_code_billing' = 'true'
      AND pricing.platform = 'command_code'
      AND jsonb_array_length(pricing.models) > 0
      AND pricing.models <@ commandcode_retired;

    DELETE FROM channel_model_pricing pricing
    USING channels channel
    WHERE pricing.channel_id = channel.id
      AND channel.features_config->>'command_code_billing' = 'true'
      AND pricing.platform = 'command_code'
      AND jsonb_array_length(pricing.models) > 0
      AND pricing.models <@ commandcode_retired;

    UPDATE channel_model_pricing pricing
    SET models = COALESCE((
            SELECT jsonb_agg(model.value ORDER BY model.ordinality)
            FROM jsonb_array_elements(pricing.models) WITH ORDINALITY AS model(value, ordinality)
            WHERE NOT commandcode_retired @> jsonb_build_array(model.value)
        ), '[]'::JSONB),
        updated_at = NOW()
    FROM channels channel
    WHERE pricing.channel_id = channel.id
      AND channel.features_config->>'command_code_billing' = 'true'
      AND pricing.platform = 'command_code'
      AND EXISTS (
          SELECT 1 FROM jsonb_array_elements(pricing.models) AS model(value)
          WHERE commandcode_retired @> jsonb_build_array(model.value)
      );

    -- 共享价卡全部模型下架时删除空价卡及其区间。
    DELETE FROM channel_pricing_intervals intervals
    USING channel_model_pricing pricing, channels channel
    WHERE intervals.pricing_id = pricing.id
      AND pricing.channel_id = channel.id
      AND channel.features_config->>'command_code_billing' = 'true'
      AND pricing.platform = 'command_code'
      AND pricing.models = '[]'::JSONB;

    DELETE FROM channel_model_pricing pricing
    USING channels channel
    WHERE pricing.channel_id = channel.id
      AND channel.features_config->>'command_code_billing' = 'true'
      AND pricing.platform = 'command_code'
      AND pricing.models = '[]'::JSONB;

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
           NULLIF(entry->'time_pricing', 'null'::JSONB), entry->'monthly_quota'
    FROM channels channel
    CROSS JOIN LATERAL jsonb_array_elements(commandcode_additions) entry
    WHERE channel.features_config->>'command_code_billing' = 'true'
      AND NOT EXISTS (
          SELECT 1 FROM channel_model_pricing existing
          WHERE existing.channel_id = channel.id AND existing.platform = 'command_code'
            AND existing.models @> jsonb_build_array(entry->>'model')
      );

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

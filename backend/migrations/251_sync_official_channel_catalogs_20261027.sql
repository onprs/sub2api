-- 同步 2026-10-27 官方目录：新增模型、下架模型、额度与免费价格。
-- OpenCode Go: https://opencode.ai/docs/go/（排除 Google SDK 专属端点模型）
-- OpenCode Zen: https://opencode.ai/v2/docs/console/models/
-- Command Code: https://commandcode.ai/docs/plans/goat
-- 仅更新对应的原生计费渠道，保留自定义渠道、历史账单和已有关联。
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '10min';

DO $migration$
DECLARE
    opencode_retired JSONB := $opencode_retired$
["grok-4.6","kimi-k2.6","longcat-2.5-preview-free","step-5-preview-free","cat-jam-free","minimax-m2.7","space-bunny"]
$opencode_retired$::JSONB;
    opencode_additions JSONB := $opencode_additions$
[
  {"platform":"opencode_go","models":["kimi-k3.1-pro"],"billing_mode":"token","input_price":0.000002,"output_price":0.00001,"cache_read_price":0.0000002,"cache_write_price":0.000002,"monthly_quota":{"base_credits_usd":15},"intervals":[],"time_pricing":null},
  {"platform":"opencode_go","models":["step-5-preview"],"billing_mode":"token","input_price":0.000001,"output_price":0.0000027,"cache_read_price":0.00000005,"cache_write_price":0,"monthly_quota":{"base_credits_usd":15},"intervals":[],"time_pricing":null},
  {"platform":"opencode_go","models":["beam-free"],"billing_mode":"token","input_price":0.000000001,"output_price":0.000000001,"cache_read_price":0.000000001,"cache_write_price":0.000000001,"monthly_quota":null,"intervals":[],"time_pricing":null},
  {"platform":"opencode_go","models":["minimax-m3.1-flash"],"billing_mode":"token","input_price":0.00000015,"output_price":0.0000006,"cache_read_price":0.000000015,"cache_write_price":0,"monthly_quota":{"base_credits_usd":20},"intervals":[],"time_pricing":null},
  {"platform":"opencode_go","models":["gemini-4-argon"],"billing_mode":"token","input_price":0.000002,"output_price":0.00001,"cache_read_price":0.0000001,"cache_write_price":0,"monthly_quota":{"base_credits_usd":30},"intervals":[],"time_pricing":null}
]
$opencode_additions$::JSONB;
    commandcode_retired JSONB := $commandcode_retired$
["inclusionai/ling-3.0-flash-sante:free","inclusionai/ling-3.1-flash:free","xiaomi/mimo-v2.5","xiaomi/mimo-v2.5-pro"]
$commandcode_retired$::JSONB;
    commandcode_additions JSONB := $commandcode_additions$
[
  {"model":"reflection/beam:free","input":0.001,"output":0.001,"cache_read":0.001,"cache_write":0.001,"monthly_quota":null,"time_pricing":null},
  {"model":"inclusionai/ling-3.1-flash","input":0.3,"output":0.9,"cache_read":0.06,"cache_write":null,"monthly_quota":{"base_credits_usd":100},"time_pricing":null},
  {"model":"meta/muse-spark-open","input":1.25,"output":4.25,"cache_read":0.15,"cache_write":null,"monthly_quota":{"base_credits_usd":20},"time_pricing":null},
  {"model":"minimax/minimax-m3.1-flash","input":0.15,"output":0.6,"cache_read":0.015,"cache_write":null,"monthly_quota":{"base_credits_usd":20},"time_pricing":null},
  {"model":"moonshotai/Kimi-K3.1-Pro","input":2,"output":10,"cache_read":0.2,"cache_write":null,"monthly_quota":{"base_credits_usd":20},"time_pricing":null},
  {"model":"nvidia/nemotron-3.5-super-vl","input":0.2,"output":0.5,"cache_read":0.04,"cache_write":null,"monthly_quota":{"base_credits_usd":20},"time_pricing":null},
  {"model":"ornith/ornith-1.5-122b","input":0.16,"output":0.64,"cache_read":0.02,"cache_write":null,"monthly_quota":{"base_credits_usd":20},"time_pricing":null},
  {"model":"Qwen/Qwen3.8-Max-Prime","input":4,"output":12,"cache_read":0.5,"cache_write":5,"monthly_quota":{"base_credits_usd":20},"time_pricing":null}
]
$commandcode_additions$::JSONB;
    commandcode_quotas JSONB := $commandcode_quotas$
{
  "gpt-5.6-sol": {"base_credits_usd":20},
  "deepseek/deepseek-v4.1-flash": {"base_credits_usd":80},
  "z-ai/glm-5.3-flash": {"base_credits_usd":80},
  "moonshotai/Kimi-K3": {"base_credits_usd":20},
  "mistral/mistral-large-4": {"base_credits_usd":20}
}
$commandcode_quotas$::JSONB;
BEGIN
    -- OpenCode：独占失效模型的价卡先删除区间，兼容没有级联删除的历史外键。
    DELETE FROM channel_pricing_intervals intervals
    USING channel_model_pricing pricing, channels channel
    WHERE intervals.pricing_id = pricing.id
      AND pricing.channel_id = channel.id
      AND channel.features_config->>'opencode_channel_billing' = 'true'
      AND pricing.platform = 'opencode_go'
      AND jsonb_array_length(pricing.models) > 0
      AND pricing.models <@ opencode_retired;

    DELETE FROM channel_model_pricing pricing
    USING channels channel
    WHERE pricing.channel_id = channel.id
      AND channel.features_config->>'opencode_channel_billing' = 'true'
      AND pricing.platform = 'opencode_go'
      AND jsonb_array_length(pricing.models) > 0
      AND pricing.models <@ opencode_retired;

    -- OpenCode：共享价卡仅移除失效 ID，其余模型及全部计费字段保持原值。
    UPDATE channel_model_pricing pricing
    SET models = COALESCE((
            SELECT jsonb_agg(model.value ORDER BY model.ordinality)
            FROM jsonb_array_elements(pricing.models) WITH ORDINALITY AS model(value, ordinality)
            WHERE NOT opencode_retired @> jsonb_build_array(model.value)
        ), '[]'::JSONB),
        updated_at = NOW()
    FROM channels channel
    WHERE pricing.channel_id = channel.id
      AND channel.features_config->>'opencode_channel_billing' = 'true'
      AND pricing.platform = 'opencode_go'
      AND EXISTS (
          SELECT 1 FROM jsonb_array_elements(pricing.models) AS model(value)
          WHERE opencode_retired @> jsonb_build_array(model.value)
      );

    -- 共享价卡全部模型下架时删除空价卡及其区间。
    DELETE FROM channel_pricing_intervals intervals
    USING channel_model_pricing pricing, channels channel
    WHERE intervals.pricing_id = pricing.id
      AND pricing.channel_id = channel.id
      AND channel.features_config->>'opencode_channel_billing' = 'true'
      AND pricing.platform = 'opencode_go'
      AND pricing.models = '[]'::JSONB;

    DELETE FROM channel_model_pricing pricing
    USING channels channel
    WHERE pricing.channel_id = channel.id
      AND channel.features_config->>'opencode_channel_billing' = 'true'
      AND pricing.platform = 'opencode_go'
      AND pricing.models = '[]'::JSONB;

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

    -- 新增的免费模型与迁移 248 的固定免费价格保持一致。
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
      AND pricing.models = '["beam-free"]'::JSONB
      AND COALESCE(pricing.billing_mode, 'token') = 'token';

    DELETE FROM channel_pricing_intervals intervals
    USING channel_model_pricing pricing, channels channel
    WHERE intervals.pricing_id = pricing.id
      AND pricing.channel_id = channel.id
      AND channel.features_config->>'opencode_channel_billing' = 'true'
      AND pricing.platform = 'opencode_go'
      AND pricing.models = '["beam-free"]'::JSONB;

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

    -- Command Code：新增免费模型与迁移 248 的固定免费价格保持一致。
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
      AND pricing.models = '["reflection/beam:free"]'::JSONB
      AND COALESCE(pricing.billing_mode, 'token') = 'token';

    DELETE FROM channel_pricing_intervals intervals
    USING channel_model_pricing pricing, channels channel
    WHERE intervals.pricing_id = pricing.id
      AND pricing.channel_id = channel.id
      AND channel.features_config->>'command_code_billing' = 'true'
      AND pricing.platform = 'command_code'
      AND pricing.models = '["reflection/beam:free"]'::JSONB;

    -- Command Code：仅修改官方有变更的月额度，保留模型单价、分档、分时与渠道自定义配置。
    UPDATE channel_model_pricing pricing
    SET monthly_quota = patch.value, updated_at = NOW()
    FROM channels channel, jsonb_each(commandcode_quotas) patch
    WHERE pricing.channel_id = channel.id
      AND channel.features_config->>'command_code_billing' = 'true'
      AND pricing.platform = 'command_code'
      AND pricing.models = jsonb_build_array(patch.key);
END
$migration$;

-- 对齐 2026-10-14 OpenCode 当前定价目录：API 中仍有模型，但官方不再公布价格。
-- https://opencode.ai/v2/docs/console/go/
-- https://opencode.ai/v2/docs/console/models/
-- 仅收紧原生 OpenCode 渠道模型范围，保留其他价卡、额度、分时和历史账单。
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '10min';

DO $migration$
DECLARE
    retired_models JSONB := $retired_models$
["deepseek-v4-flash","deepseek-v4-flash-vision-exp"]
$retired_models$::JSONB;
BEGIN
    -- 独占失效模型的价卡先删除区间，兼容没有级联删除的历史外键。
    DELETE FROM channel_pricing_intervals intervals
    USING channel_model_pricing pricing, channels channel
    WHERE intervals.pricing_id = pricing.id
      AND pricing.channel_id = channel.id
      AND channel.features_config->>'opencode_channel_billing' = 'true'
      AND pricing.platform = 'opencode_go'
      AND jsonb_array_length(pricing.models) > 0
      AND pricing.models <@ retired_models;

    DELETE FROM channel_model_pricing pricing
    USING channels channel
    WHERE pricing.channel_id = channel.id
      AND channel.features_config->>'opencode_channel_billing' = 'true'
      AND pricing.platform = 'opencode_go'
      AND jsonb_array_length(pricing.models) > 0
      AND pricing.models <@ retired_models;

    -- 共享价卡仅移除失效 ID，其余模型及全部计费字段保持原值。
    UPDATE channel_model_pricing pricing
    SET models = (
            SELECT jsonb_agg(model.value ORDER BY model.ordinality)
            FROM jsonb_array_elements(pricing.models) WITH ORDINALITY AS model(value, ordinality)
            WHERE NOT retired_models @> jsonb_build_array(model.value)
        ),
        updated_at = NOW()
    FROM channels channel
    WHERE pricing.channel_id = channel.id
      AND channel.features_config->>'opencode_channel_billing' = 'true'
      AND pricing.platform = 'opencode_go'
      AND EXISTS (
          SELECT 1 FROM jsonb_array_elements(pricing.models) AS model(value)
          WHERE retired_models @> jsonb_build_array(model.value)
      );
END
$migration$;

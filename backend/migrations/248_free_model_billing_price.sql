-- 免费模型统一按 0.001 USD / 百万 token 计费，仅更新两个平台的原生渠道。
-- 只更新已有免费模型条目，不向白名单添加模型，也不修改历史账单。
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '10min';

DO $migration$
DECLARE
    free_models JSONB := $free_models$
{
  "opencode_go": ["big-pickle","space-bunny-free","cat-jam-free","longcat-2.5-preview-free","step-5-preview-free","mimo-v2.6-flash-free","ling-3.1-flash-free","nemotron-3.5-lightning-free","muse-spark-1.3-contributor-free"],
  "command_code": ["inclusionai/ling-3.0-flash-sante:free","inclusionai/ling-3.1-flash:free","poolside/laguna-s-2.1-free","stealth/glyph-cluster:free"]
}
$free_models$::JSONB;
    fixed_price NUMERIC := $free_price$0.000000001$free_price$::NUMERIC;
BEGIN
    WITH changed AS (
        UPDATE channel_model_pricing pricing
        SET input_price = fixed_price,
            output_price = fixed_price,
            cache_write_price = fixed_price,
            cache_write_1h_price = fixed_price,
            cache_read_price = fixed_price,
            image_input_price = fixed_price,
            image_output_price = fixed_price,
            fast_multiplier = 1,
            flex_multiplier = 1,
            max_reasoning_effort_multiplier = 1,
            reasoning_effort_multipliers = '{}'::JSONB,
            monthly_quota = NULL,
            time_pricing = NULL,
            updated_at = NOW()
        FROM channels channel
        WHERE pricing.channel_id = channel.id
          AND COALESCE(pricing.billing_mode, 'token') = 'token'
          AND jsonb_array_length(pricing.models) > 0
          AND pricing.models <@ (free_models->pricing.platform)
          AND ((pricing.platform = 'opencode_go' AND channel.features_config->>'opencode_channel_billing' = 'true')
            OR (pricing.platform = 'command_code' AND channel.features_config->>'command_code_billing' = 'true'))
        RETURNING pricing.id
    )
    -- 固定单价不保留上下文档位或旧模型倍率。
    DELETE FROM channel_pricing_intervals intervals
    USING changed
    WHERE intervals.pricing_id = changed.id;
END
$migration$;

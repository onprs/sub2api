-- 同步 OpenCode Go 2026-10-29 官方目录：
--   * 新增 Iceman Contributor Free（官方免费模型，按本站统一免费价计费）。
--   * 确保 Gemini 4 Argon 的 Google GenAI 计费条目存在（官方 50% 折扣价，截至 2026-11-30）。
-- 仅更新带 opencode_channel_billing 标记的原生渠道；保留管理员自定义价卡、历史账单和关联。
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '10min';

DO $migration$
DECLARE
    opencode_additions JSONB := $opencode_additions$
[
  {"platform":"opencode_go","models":["iceman-contributor-free"],"billing_mode":"token","input_price":0.000000001,"output_price":0.000000001,"cache_read_price":0.000000001,"cache_write_price":0.000000001,"monthly_quota":null,"intervals":[],"time_pricing":null},
  {"platform":"opencode_go","models":["gemini-4-argon"],"billing_mode":"token","input_price":0.000002,"output_price":0.00001,"cache_read_price":0.0000001,"cache_write_price":0,"monthly_quota":{"base_credits_usd":30},"intervals":[],"time_pricing":null}
]
$opencode_additions$::JSONB;
BEGIN
    -- 仅插入尚未存在的官方模型价卡，保留管理员已保存的价格与额度。
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

    -- 新增免费模型与迁移 248 的固定免费价格保持一致。
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
      AND pricing.models = '["iceman-contributor-free"]'::JSONB
      AND COALESCE(pricing.billing_mode, 'token') = 'token';

    DELETE FROM channel_pricing_intervals intervals
    USING channel_model_pricing pricing, channels channel
    WHERE intervals.pricing_id = pricing.id
      AND pricing.channel_id = channel.id
      AND channel.features_config->>'opencode_channel_billing' = 'true'
      AND pricing.platform = 'opencode_go'
      AND pricing.models = '["iceman-contributor-free"]'::JSONB;
END
$migration$;

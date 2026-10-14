-- 同步 2026-10-14 官方目录与月额度，仅更新对应的原生计费渠道。
-- OpenCode Go: https://opencode.ai/docs/go/
-- Command Code: https://commandcode.ai/docs/plans/goat
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '10min';

DO $migration$
DECLARE
    opencode_additions JSONB := $opencode_additions$
[
  {"platform":"opencode_go","models":["cat-jam-free"],"billing_mode":"token","input_price":0,"output_price":0,"cache_read_price":0,"cache_write_price":0,"monthly_quota":null,"intervals":[],"time_pricing":null,"protocol":"chat_completions"}
]
$opencode_additions$::JSONB;
    commandcode_quotas JSONB := $commandcode_quotas$
{
  "moonshotai/Kimi-K3": {"base_credits_usd":20,"periods":[{"credits_usd":60,"starts_at":"2026-09-30T00:00:00Z","expires_at":"2026-10-08T00:00:00Z","term":"through Oct 7th"}]},
  "deepseek/deepseek-v4.1-flash": {"base_credits_usd":80,"periods":[{"credits_usd":100,"starts_at":"2026-10-13T14:00:00Z","expires_at":"2026-10-16T13:59:59Z","term":"through Oct 16th"}]},
  "z-ai/glm-5.3-flash": {"base_credits_usd":80,"periods":[{"credits_usd":60,"expires_at":"2026-10-14T12:30:00Z"},{"credits_usd":100,"starts_at":"2026-10-14T12:30:00Z","expires_at":"2026-10-17T12:29:59Z","term":"through Oct 17th"}]}
}
$commandcode_quotas$::JSONB;
BEGIN
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

    -- 仅修改官方有变更的月额度，保留模型单价、分档、分时与渠道自定义配置。
    UPDATE channel_model_pricing pricing
    SET monthly_quota = patch.value, updated_at = NOW()
    FROM channels channel, jsonb_each(commandcode_quotas) patch
    WHERE pricing.channel_id = channel.id
      AND channel.features_config->>'command_code_billing' = 'true'
      AND pricing.platform = 'command_code'
      AND pricing.models = jsonb_build_array(patch.key);
END
$migration$;

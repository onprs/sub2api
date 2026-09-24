-- Unify the persisted OpenCode platform ID while retaining Go/Zen in account_mode.

ALTER TABLE user_platform_quotas
    DROP CONSTRAINT IF EXISTS user_platform_quotas_platform_check;

-- A prior official OpenCode row and a custom OpenCode Go row may coexist for one
-- user. Keep the stricter limits and combine usage only when both rows share a window.
UPDATE user_platform_quotas AS canonical
SET daily_limit_usd = COALESCE(LEAST(canonical.daily_limit_usd, legacy.daily_limit_usd), canonical.daily_limit_usd, legacy.daily_limit_usd),
    weekly_limit_usd = COALESCE(LEAST(canonical.weekly_limit_usd, legacy.weekly_limit_usd), canonical.weekly_limit_usd, legacy.weekly_limit_usd),
    monthly_limit_usd = COALESCE(LEAST(canonical.monthly_limit_usd, legacy.monthly_limit_usd), canonical.monthly_limit_usd, legacy.monthly_limit_usd),
    daily_usage_usd = CASE
        WHEN canonical.daily_window_start IS NOT DISTINCT FROM legacy.daily_window_start THEN canonical.daily_usage_usd + legacy.daily_usage_usd
        WHEN canonical.daily_window_start IS NULL OR legacy.daily_window_start > canonical.daily_window_start THEN legacy.daily_usage_usd
        ELSE canonical.daily_usage_usd
    END,
    weekly_usage_usd = CASE
        WHEN canonical.weekly_window_start IS NOT DISTINCT FROM legacy.weekly_window_start THEN canonical.weekly_usage_usd + legacy.weekly_usage_usd
        WHEN canonical.weekly_window_start IS NULL OR legacy.weekly_window_start > canonical.weekly_window_start THEN legacy.weekly_usage_usd
        ELSE canonical.weekly_usage_usd
    END,
    monthly_usage_usd = CASE
        WHEN canonical.monthly_window_start IS NOT DISTINCT FROM legacy.monthly_window_start THEN canonical.monthly_usage_usd + legacy.monthly_usage_usd
        WHEN canonical.monthly_window_start IS NULL OR legacy.monthly_window_start > canonical.monthly_window_start THEN legacy.monthly_usage_usd
        ELSE canonical.monthly_usage_usd
    END,
    daily_window_start = GREATEST(canonical.daily_window_start, legacy.daily_window_start),
    weekly_window_start = GREATEST(canonical.weekly_window_start, legacy.weekly_window_start),
    monthly_window_start = GREATEST(canonical.monthly_window_start, legacy.monthly_window_start),
    updated_at = NOW()
FROM user_platform_quotas AS legacy
WHERE canonical.user_id = legacy.user_id
  AND canonical.platform = 'opencode'
  AND legacy.platform = 'opencode_go'
  AND canonical.deleted_at IS NULL
  AND legacy.deleted_at IS NULL;

DELETE FROM user_platform_quotas AS legacy
WHERE legacy.platform = 'opencode_go'
  AND legacy.deleted_at IS NULL
  AND EXISTS (
      SELECT 1
      FROM user_platform_quotas AS canonical
      WHERE canonical.user_id = legacy.user_id
        AND canonical.platform = 'opencode'
        AND canonical.deleted_at IS NULL
  );

UPDATE accounts
SET credentials = jsonb_set(COALESCE(credentials, '{}'::jsonb), '{account_mode}', '"zen"'::jsonb, TRUE),
    updated_at = NOW()
WHERE platform = 'opencode'
  AND NULLIF(btrim(COALESCE(credentials, '{}'::jsonb) ->> 'account_mode'), '') IS NULL;

UPDATE accounts
SET credentials = CASE
        WHEN NULLIF(btrim(COALESCE(credentials, '{}'::jsonb) ->> 'account_mode'), '') IS NOT NULL
            THEN COALESCE(credentials, '{}'::jsonb)
        ELSE jsonb_set(COALESCE(credentials, '{}'::jsonb), '{account_mode}', '"go"'::jsonb, TRUE)
    END,
    platform = 'opencode',
    updated_at = NOW()
WHERE platform = 'opencode_go';

UPDATE accounts
SET extra = jsonb_strip_nulls(COALESCE(extra, '{}'::jsonb) - ARRAY[
        'opencode_go_5h_used_percent', 'opencode_go_5h_reset_at',
        'opencode_go_weekly_used_percent', 'opencode_go_weekly_reset_at',
        'opencode_go_monthly_used_percent', 'opencode_go_monthly_reset_at'
    ]::text[]) || jsonb_strip_nulls(jsonb_build_object(
        'opencode_5h_used_percent', CASE
            WHEN COALESCE(extra -> 'opencode_5h_used_percent', 'null'::jsonb) = 'null'::jsonb
                THEN extra -> 'opencode_go_5h_used_percent'
            ELSE extra -> 'opencode_5h_used_percent'
        END,
        'opencode_5h_reset_at', CASE
            WHEN COALESCE(extra -> 'opencode_5h_reset_at', 'null'::jsonb) = 'null'::jsonb
                THEN extra -> 'opencode_go_5h_reset_at'
            ELSE extra -> 'opencode_5h_reset_at'
        END,
        'opencode_weekly_used_percent', CASE
            WHEN COALESCE(extra -> 'opencode_weekly_used_percent', 'null'::jsonb) = 'null'::jsonb
                THEN extra -> 'opencode_go_weekly_used_percent'
            ELSE extra -> 'opencode_weekly_used_percent'
        END,
        'opencode_weekly_reset_at', CASE
            WHEN COALESCE(extra -> 'opencode_weekly_reset_at', 'null'::jsonb) = 'null'::jsonb
                THEN extra -> 'opencode_go_weekly_reset_at'
            ELSE extra -> 'opencode_weekly_reset_at'
        END,
        'opencode_monthly_used_percent', CASE
            WHEN COALESCE(extra -> 'opencode_monthly_used_percent', 'null'::jsonb) = 'null'::jsonb
                THEN extra -> 'opencode_go_monthly_used_percent'
            ELSE extra -> 'opencode_monthly_used_percent'
        END,
        'opencode_monthly_reset_at', CASE
            WHEN COALESCE(extra -> 'opencode_monthly_reset_at', 'null'::jsonb) = 'null'::jsonb
                THEN extra -> 'opencode_go_monthly_reset_at'
            ELSE extra -> 'opencode_monthly_reset_at'
        END
    ))
WHERE extra ?| ARRAY[
    'opencode_go_5h_used_percent', 'opencode_go_5h_reset_at',
    'opencode_go_weekly_used_percent', 'opencode_go_weekly_reset_at',
    'opencode_go_monthly_used_percent', 'opencode_go_monthly_reset_at'
];

UPDATE groups
SET platform = 'opencode', updated_at = NOW()
WHERE platform = 'opencode_go';

-- Rewrite remaining persisted platform selectors and platform-scoped configuration.
UPDATE api_keys
SET routing_platform = 'opencode', updated_at = NOW()
WHERE routing_platform = 'opencode_go';

UPDATE channel_model_pricing
SET platform = 'opencode', updated_at = NOW()
WHERE platform = 'opencode_go';

UPDATE channel_account_stats_model_pricing
SET platform = 'opencode', updated_at = NOW()
WHERE platform = 'opencode_go';

UPDATE groups AS group_row
SET model_pricing = (
        SELECT jsonb_agg(
            CASE
                WHEN item ->> 'platform' = 'opencode_go'
                    THEN jsonb_set(item, '{platform}', '"opencode"'::jsonb, TRUE)
                ELSE item
            END
            ORDER BY ordinal
        )
        FROM jsonb_array_elements(group_row.model_pricing) WITH ORDINALITY AS entries(item, ordinal)
    ),
    updated_at = NOW()
WHERE jsonb_typeof(group_row.model_pricing) = 'array'
  AND EXISTS (
      SELECT 1
      FROM jsonb_array_elements(group_row.model_pricing) AS entries(item)
      WHERE item ->> 'platform' = 'opencode_go'
  );

UPDATE channels AS channel_row
SET model_mapping = (channel_row.model_mapping - 'opencode_go') || jsonb_build_object(
        'opencode',
        (CASE
            WHEN jsonb_typeof(channel_row.model_mapping -> 'opencode_go') = 'object'
                THEN channel_row.model_mapping -> 'opencode_go'
            ELSE '{}'::jsonb
        END) ||
        (CASE
            WHEN jsonb_typeof(channel_row.model_mapping -> 'opencode') = 'object'
                THEN channel_row.model_mapping -> 'opencode'
            ELSE '{}'::jsonb
        END)
    ),
    updated_at = NOW()
WHERE jsonb_typeof(channel_row.model_mapping) = 'object'
  AND jsonb_typeof(channel_row.model_mapping -> 'opencode_go') = 'object';

UPDATE error_passthrough_rules AS rule
SET platforms = (
        SELECT jsonb_agg(
            to_jsonb(CASE WHEN platform = 'opencode_go' THEN 'opencode' ELSE platform END)
            ORDER BY ordinal
        )
        FROM jsonb_array_elements_text(rule.platforms) WITH ORDINALITY AS entries(platform, ordinal)
    ),
    updated_at = NOW()
WHERE jsonb_typeof(rule.platforms) = 'array'
  AND rule.platforms ? 'opencode_go';

-- Unify the V2 monitor selector. When both modes were configured, retain the
-- canonical entry's order/settings, enable either requested mode, and union models.
DO $$
DECLARE
    config_row RECORD;
    legacy_config JSONB;
    canonical_config JSONB;
    canonical_models JSONB;
    legacy_models JSONB;
    merged_models JSONB;
    merged_config JSONB;
    merged_platforms JSONB;
BEGIN
    FOR config_row IN
        SELECT id, platforms
        FROM channel_monitor_v2_config
        WHERE jsonb_typeof(platforms) = 'array'
          AND EXISTS (
              SELECT 1
              FROM jsonb_array_elements(platforms) AS entries(item)
              WHERE item ->> 'platform' = 'opencode_go'
          )
    LOOP
        SELECT item INTO legacy_config
        FROM jsonb_array_elements(config_row.platforms) AS entries(item)
        WHERE item ->> 'platform' = 'opencode_go'
        LIMIT 1;

        SELECT item INTO canonical_config
        FROM jsonb_array_elements(config_row.platforms) AS entries(item)
        WHERE item ->> 'platform' = 'opencode'
        LIMIT 1;

        IF canonical_config IS NULL THEN
            SELECT COALESCE(jsonb_agg(
                CASE
                    WHEN item ->> 'platform' = 'opencode_go'
                        THEN jsonb_set(item, '{platform}', '"opencode"'::jsonb, TRUE)
                    ELSE item
                END
                ORDER BY ordinal
            ), '[]'::jsonb)
            INTO merged_platforms
            FROM jsonb_array_elements(config_row.platforms) WITH ORDINALITY AS entries(item, ordinal);
        ELSE
            canonical_models := CASE
                WHEN jsonb_typeof(canonical_config -> 'models') = 'array' THEN canonical_config -> 'models'
                ELSE '[]'::jsonb
            END;
            legacy_models := CASE
                WHEN jsonb_typeof(legacy_config -> 'models') = 'array' THEN legacy_config -> 'models'
                ELSE '[]'::jsonb
            END;

            SELECT COALESCE(jsonb_agg(to_jsonb(model) ORDER BY first_ordinal), '[]'::jsonb)
            INTO merged_models
            FROM (
                SELECT model, MIN(source_ordinal) AS first_ordinal
                FROM (
                    SELECT model, ordinal AS source_ordinal
                    FROM jsonb_array_elements_text(canonical_models) WITH ORDINALITY AS models(model, ordinal)
                    UNION ALL
                    SELECT model, 1000000000 + ordinal AS source_ordinal
                    FROM jsonb_array_elements_text(legacy_models) WITH ORDINALITY AS models(model, ordinal)
                ) AS all_models
                GROUP BY model
            ) AS distinct_models;

            merged_config := jsonb_set(
                jsonb_set(
                    canonical_config,
                    '{enabled}',
                    to_jsonb(COALESCE((canonical_config ->> 'enabled')::boolean, FALSE)
                        OR COALESCE((legacy_config ->> 'enabled')::boolean, FALSE)),
                    TRUE
                ),
                '{models}', merged_models, TRUE
            );

            SELECT COALESCE(jsonb_agg(
                CASE WHEN item ->> 'platform' = 'opencode' THEN merged_config ELSE item END
                ORDER BY ordinal
            ), '[]'::jsonb)
            INTO merged_platforms
            FROM jsonb_array_elements(config_row.platforms) WITH ORDINALITY AS entries(item, ordinal)
            WHERE item ->> 'platform' <> 'opencode_go';
        END IF;

        UPDATE channel_monitor_v2_config
        SET platforms = merged_platforms,
            version = version + 1,
            updated_at = NOW()
        WHERE id = config_row.id;
    END LOOP;
END $$;

-- Preserve existing monitor V2 history when canonical and legacy aggregate keys collide.
INSERT INTO channel_monitor_v2_metrics_1m AS current (
    bucket_start, platform, group_id, model, success_requests, error_requests,
    upstream_affected_requests, upstream_attempt_count, input_tokens, output_tokens,
    cache_creation_tokens, cache_read_tokens, ttft_sum_ms, ttft_count,
    duration_sum_ms, duration_count, computed_at
)
SELECT bucket_start, 'opencode', group_id, model, success_requests, error_requests,
       upstream_affected_requests, upstream_attempt_count, input_tokens, output_tokens,
       cache_creation_tokens, cache_read_tokens, ttft_sum_ms, ttft_count,
       duration_sum_ms, duration_count, computed_at
FROM channel_monitor_v2_metrics_1m
WHERE platform = 'opencode_go'
ON CONFLICT (bucket_start, platform, group_id, model) DO UPDATE SET
    success_requests = current.success_requests + EXCLUDED.success_requests,
    error_requests = current.error_requests + EXCLUDED.error_requests,
    upstream_affected_requests = current.upstream_affected_requests + EXCLUDED.upstream_affected_requests,
    upstream_attempt_count = current.upstream_attempt_count + EXCLUDED.upstream_attempt_count,
    input_tokens = current.input_tokens + EXCLUDED.input_tokens,
    output_tokens = current.output_tokens + EXCLUDED.output_tokens,
    cache_creation_tokens = current.cache_creation_tokens + EXCLUDED.cache_creation_tokens,
    cache_read_tokens = current.cache_read_tokens + EXCLUDED.cache_read_tokens,
    ttft_sum_ms = current.ttft_sum_ms + EXCLUDED.ttft_sum_ms,
    ttft_count = current.ttft_count + EXCLUDED.ttft_count,
    duration_sum_ms = current.duration_sum_ms + EXCLUDED.duration_sum_ms,
    duration_count = current.duration_count + EXCLUDED.duration_count,
    computed_at = GREATEST(current.computed_at, EXCLUDED.computed_at);
DELETE FROM channel_monitor_v2_metrics_1m WHERE platform = 'opencode_go';

INSERT INTO channel_monitor_v2_user_metrics_1m AS current (
    bucket_start, platform, group_id, model, user_id, success_requests, error_requests,
    input_tokens, output_tokens, cache_creation_tokens, cache_read_tokens,
    ttft_sum_ms, ttft_count, duration_sum_ms, duration_count, computed_at
)
SELECT bucket_start, 'opencode', group_id, model, user_id, success_requests, error_requests,
       input_tokens, output_tokens, cache_creation_tokens, cache_read_tokens,
       ttft_sum_ms, ttft_count, duration_sum_ms, duration_count, computed_at
FROM channel_monitor_v2_user_metrics_1m
WHERE platform = 'opencode_go'
ON CONFLICT (bucket_start, platform, group_id, model, user_id) DO UPDATE SET
    success_requests = current.success_requests + EXCLUDED.success_requests,
    error_requests = current.error_requests + EXCLUDED.error_requests,
    input_tokens = current.input_tokens + EXCLUDED.input_tokens,
    output_tokens = current.output_tokens + EXCLUDED.output_tokens,
    cache_creation_tokens = current.cache_creation_tokens + EXCLUDED.cache_creation_tokens,
    cache_read_tokens = current.cache_read_tokens + EXCLUDED.cache_read_tokens,
    ttft_sum_ms = current.ttft_sum_ms + EXCLUDED.ttft_sum_ms,
    ttft_count = current.ttft_count + EXCLUDED.ttft_count,
    duration_sum_ms = current.duration_sum_ms + EXCLUDED.duration_sum_ms,
    duration_count = current.duration_count + EXCLUDED.duration_count,
    computed_at = GREATEST(current.computed_at, EXCLUDED.computed_at);
DELETE FROM channel_monitor_v2_user_metrics_1m WHERE platform = 'opencode_go';

INSERT INTO channel_monitor_v2_error_metrics_1m AS current (
    bucket_start, platform, group_id, model, error_category, taxonomy_version, error_requests
)
SELECT bucket_start, 'opencode', group_id, model, error_category, taxonomy_version, error_requests
FROM channel_monitor_v2_error_metrics_1m
WHERE platform = 'opencode_go'
ON CONFLICT (bucket_start, platform, group_id, model, error_category, taxonomy_version) DO UPDATE SET
    error_requests = current.error_requests + EXCLUDED.error_requests;
DELETE FROM channel_monitor_v2_error_metrics_1m WHERE platform = 'opencode_go';

INSERT INTO channel_monitor_v2_latency_histograms_1m AS current (
    bucket_start, platform, group_id, model, user_id, metric, upper_bound_ms, sample_count
)
SELECT bucket_start, 'opencode', group_id, model, user_id, metric, upper_bound_ms, sample_count
FROM channel_monitor_v2_latency_histograms_1m
WHERE platform = 'opencode_go'
ON CONFLICT (bucket_start, platform, group_id, model, user_id, metric, upper_bound_ms) DO UPDATE SET
    sample_count = current.sample_count + EXCLUDED.sample_count;
DELETE FROM channel_monitor_v2_latency_histograms_1m WHERE platform = 'opencode_go';

INSERT INTO channel_monitor_v2_metrics_rollup AS target_row (
    bucket_start, bucket_seconds, platform, group_id, model, success_requests, error_requests,
    upstream_affected_requests, upstream_attempt_count, input_tokens, output_tokens,
    cache_creation_tokens, cache_read_tokens, ttft_sum_ms, ttft_count,
    duration_sum_ms, duration_count, computed_at
)
SELECT bucket_start, bucket_seconds, 'opencode', group_id, model, success_requests, error_requests,
       upstream_affected_requests, upstream_attempt_count, input_tokens, output_tokens,
       cache_creation_tokens, cache_read_tokens, ttft_sum_ms, ttft_count,
       duration_sum_ms, duration_count, computed_at
FROM channel_monitor_v2_metrics_rollup
WHERE platform = 'opencode_go'
ON CONFLICT (bucket_seconds, bucket_start, platform, group_id, model) DO UPDATE SET
    success_requests = target_row.success_requests + EXCLUDED.success_requests,
    error_requests = target_row.error_requests + EXCLUDED.error_requests,
    upstream_affected_requests = target_row.upstream_affected_requests + EXCLUDED.upstream_affected_requests,
    upstream_attempt_count = target_row.upstream_attempt_count + EXCLUDED.upstream_attempt_count,
    input_tokens = target_row.input_tokens + EXCLUDED.input_tokens,
    output_tokens = target_row.output_tokens + EXCLUDED.output_tokens,
    cache_creation_tokens = target_row.cache_creation_tokens + EXCLUDED.cache_creation_tokens,
    cache_read_tokens = target_row.cache_read_tokens + EXCLUDED.cache_read_tokens,
    ttft_sum_ms = target_row.ttft_sum_ms + EXCLUDED.ttft_sum_ms,
    ttft_count = target_row.ttft_count + EXCLUDED.ttft_count,
    duration_sum_ms = target_row.duration_sum_ms + EXCLUDED.duration_sum_ms,
    duration_count = target_row.duration_count + EXCLUDED.duration_count,
    computed_at = GREATEST(target_row.computed_at, EXCLUDED.computed_at);
DELETE FROM channel_monitor_v2_metrics_rollup WHERE platform = 'opencode_go';

INSERT INTO channel_monitor_v2_user_metrics_rollup AS target_row (
    bucket_start, bucket_seconds, platform, group_id, model, user_id,
    success_requests, error_requests, input_tokens, output_tokens,
    cache_creation_tokens, cache_read_tokens, ttft_sum_ms, ttft_count,
    duration_sum_ms, duration_count, computed_at
)
SELECT bucket_start, bucket_seconds, 'opencode', group_id, model, user_id,
       success_requests, error_requests, input_tokens, output_tokens,
       cache_creation_tokens, cache_read_tokens, ttft_sum_ms, ttft_count,
       duration_sum_ms, duration_count, computed_at
FROM channel_monitor_v2_user_metrics_rollup
WHERE platform = 'opencode_go'
ON CONFLICT (bucket_seconds, bucket_start, platform, group_id, model, user_id) DO UPDATE SET
    success_requests = target_row.success_requests + EXCLUDED.success_requests,
    error_requests = target_row.error_requests + EXCLUDED.error_requests,
    input_tokens = target_row.input_tokens + EXCLUDED.input_tokens,
    output_tokens = target_row.output_tokens + EXCLUDED.output_tokens,
    cache_creation_tokens = target_row.cache_creation_tokens + EXCLUDED.cache_creation_tokens,
    cache_read_tokens = target_row.cache_read_tokens + EXCLUDED.cache_read_tokens,
    ttft_sum_ms = target_row.ttft_sum_ms + EXCLUDED.ttft_sum_ms,
    ttft_count = target_row.ttft_count + EXCLUDED.ttft_count,
    duration_sum_ms = target_row.duration_sum_ms + EXCLUDED.duration_sum_ms,
    duration_count = target_row.duration_count + EXCLUDED.duration_count,
    computed_at = GREATEST(target_row.computed_at, EXCLUDED.computed_at);
DELETE FROM channel_monitor_v2_user_metrics_rollup WHERE platform = 'opencode_go';

INSERT INTO channel_monitor_v2_error_metrics_rollup AS target_row (
    bucket_start, bucket_seconds, platform, group_id, model, error_category,
    taxonomy_version, error_requests
)
SELECT bucket_start, bucket_seconds, 'opencode', group_id, model, error_category,
       taxonomy_version, error_requests
FROM channel_monitor_v2_error_metrics_rollup
WHERE platform = 'opencode_go'
ON CONFLICT (bucket_seconds, bucket_start, platform, group_id, model, error_category, taxonomy_version) DO UPDATE SET
    error_requests = target_row.error_requests + EXCLUDED.error_requests;
DELETE FROM channel_monitor_v2_error_metrics_rollup WHERE platform = 'opencode_go';

INSERT INTO channel_monitor_v2_latency_histograms_rollup AS target_row (
    bucket_start, bucket_seconds, platform, group_id, model, user_id,
    metric, upper_bound_ms, sample_count
)
SELECT bucket_start, bucket_seconds, 'opencode', group_id, model, user_id,
       metric, upper_bound_ms, sample_count
FROM channel_monitor_v2_latency_histograms_rollup
WHERE platform = 'opencode_go'
ON CONFLICT (bucket_seconds, bucket_start, platform, group_id, model, user_id, metric, upper_bound_ms) DO UPDATE SET
    sample_count = target_row.sample_count + EXCLUDED.sample_count;
DELETE FROM channel_monitor_v2_latency_histograms_rollup WHERE platform = 'opencode_go';

-- Rewrite platform selectors and searchable historical platform labels in Ops data.
UPDATE ops_error_logs SET platform = 'opencode' WHERE platform = 'opencode_go';
UPDATE ops_system_metrics SET platform = 'opencode' WHERE platform = 'opencode_go';
UPDATE ops_system_logs SET platform = 'opencode' WHERE platform = 'opencode_go';
UPDATE ops_alert_silences SET platform = 'opencode' WHERE platform = 'opencode_go';

UPDATE ops_alert_rules
SET filters = jsonb_set(filters, '{platform}', '"opencode"'::jsonb, TRUE),
    updated_at = NOW()
WHERE jsonb_typeof(filters) = 'object'
  AND filters ->> 'platform' = 'opencode_go';

UPDATE ops_alert_events
SET dimensions = jsonb_set(dimensions, '{platform}', '"opencode"'::jsonb, TRUE)
WHERE jsonb_typeof(dimensions) = 'object'
  AND dimensions ->> 'platform' = 'opencode_go';

-- Merge colliding Ops hourly metrics; percentile merges follow the repository's
-- established weighted-p50/p90 and conservative-max-p95/p99 approximation.
UPDATE ops_metrics_hourly AS canonical
SET success_count = canonical.success_count + legacy.success_count,
    ttft_sample_count = canonical.ttft_sample_count + legacy.ttft_sample_count,
    error_count_total = canonical.error_count_total + legacy.error_count_total,
    business_limited_count = canonical.business_limited_count + legacy.business_limited_count,
    error_count_sla = canonical.error_count_sla + legacy.error_count_sla,
    upstream_error_count_excl_429_529 = canonical.upstream_error_count_excl_429_529 + legacy.upstream_error_count_excl_429_529,
    upstream_429_count = canonical.upstream_429_count + legacy.upstream_429_count,
    upstream_529_count = canonical.upstream_529_count + legacy.upstream_529_count,
    token_consumed = canonical.token_consumed + legacy.token_consumed,
    duration_p50_ms = CASE
        WHEN canonical.duration_p50_ms IS NULL THEN legacy.duration_p50_ms
        WHEN legacy.duration_p50_ms IS NULL THEN canonical.duration_p50_ms
        WHEN canonical.success_count + legacy.success_count > 0 THEN ROUND(
            (canonical.duration_p50_ms::numeric * canonical.success_count + legacy.duration_p50_ms::numeric * legacy.success_count)
            / (canonical.success_count + legacy.success_count)
        )::integer
        ELSE GREATEST(canonical.duration_p50_ms, legacy.duration_p50_ms)
    END,
    duration_p90_ms = CASE
        WHEN canonical.duration_p90_ms IS NULL THEN legacy.duration_p90_ms
        WHEN legacy.duration_p90_ms IS NULL THEN canonical.duration_p90_ms
        WHEN canonical.success_count + legacy.success_count > 0 THEN ROUND(
            (canonical.duration_p90_ms::numeric * canonical.success_count + legacy.duration_p90_ms::numeric * legacy.success_count)
            / (canonical.success_count + legacy.success_count)
        )::integer
        ELSE GREATEST(canonical.duration_p90_ms, legacy.duration_p90_ms)
    END,
    duration_p95_ms = GREATEST(canonical.duration_p95_ms, legacy.duration_p95_ms),
    duration_p99_ms = GREATEST(canonical.duration_p99_ms, legacy.duration_p99_ms),
    duration_avg_ms = CASE
        WHEN canonical.duration_avg_ms IS NULL THEN legacy.duration_avg_ms
        WHEN legacy.duration_avg_ms IS NULL THEN canonical.duration_avg_ms
        WHEN canonical.success_count + legacy.success_count > 0 THEN
            (canonical.duration_avg_ms * canonical.success_count + legacy.duration_avg_ms * legacy.success_count)
            / (canonical.success_count + legacy.success_count)
        ELSE canonical.duration_avg_ms
    END,
    duration_max_ms = GREATEST(canonical.duration_max_ms, legacy.duration_max_ms),
    ttft_p50_ms = CASE
        WHEN canonical.ttft_p50_ms IS NULL THEN legacy.ttft_p50_ms
        WHEN legacy.ttft_p50_ms IS NULL THEN canonical.ttft_p50_ms
        WHEN canonical.ttft_sample_count + legacy.ttft_sample_count > 0 THEN ROUND(
            (canonical.ttft_p50_ms::numeric * canonical.ttft_sample_count + legacy.ttft_p50_ms::numeric * legacy.ttft_sample_count)
            / (canonical.ttft_sample_count + legacy.ttft_sample_count)
        )::integer
        ELSE GREATEST(canonical.ttft_p50_ms, legacy.ttft_p50_ms)
    END,
    ttft_p90_ms = CASE
        WHEN canonical.ttft_p90_ms IS NULL THEN legacy.ttft_p90_ms
        WHEN legacy.ttft_p90_ms IS NULL THEN canonical.ttft_p90_ms
        WHEN canonical.ttft_sample_count + legacy.ttft_sample_count > 0 THEN ROUND(
            (canonical.ttft_p90_ms::numeric * canonical.ttft_sample_count + legacy.ttft_p90_ms::numeric * legacy.ttft_sample_count)
            / (canonical.ttft_sample_count + legacy.ttft_sample_count)
        )::integer
        ELSE GREATEST(canonical.ttft_p90_ms, legacy.ttft_p90_ms)
    END,
    ttft_p95_ms = GREATEST(canonical.ttft_p95_ms, legacy.ttft_p95_ms),
    ttft_p99_ms = GREATEST(canonical.ttft_p99_ms, legacy.ttft_p99_ms),
    ttft_avg_ms = CASE
        WHEN canonical.ttft_avg_ms IS NULL THEN legacy.ttft_avg_ms
        WHEN legacy.ttft_avg_ms IS NULL THEN canonical.ttft_avg_ms
        WHEN canonical.ttft_sample_count + legacy.ttft_sample_count > 0 THEN
            (canonical.ttft_avg_ms * canonical.ttft_sample_count + legacy.ttft_avg_ms * legacy.ttft_sample_count)
            / (canonical.ttft_sample_count + legacy.ttft_sample_count)
        ELSE canonical.ttft_avg_ms
    END,
    ttft_max_ms = GREATEST(canonical.ttft_max_ms, legacy.ttft_max_ms),
    computed_at = GREATEST(canonical.computed_at, legacy.computed_at)
FROM ops_metrics_hourly AS legacy
WHERE canonical.bucket_start = legacy.bucket_start
  AND COALESCE(canonical.group_id, 0) = COALESCE(legacy.group_id, 0)
  AND canonical.platform = 'opencode'
  AND legacy.platform = 'opencode_go';
DELETE FROM ops_metrics_hourly AS legacy
USING ops_metrics_hourly AS canonical
WHERE canonical.bucket_start = legacy.bucket_start
  AND COALESCE(canonical.group_id, 0) = COALESCE(legacy.group_id, 0)
  AND canonical.platform = 'opencode'
  AND legacy.platform = 'opencode_go';
UPDATE ops_metrics_hourly SET platform = 'opencode' WHERE platform = 'opencode_go';

UPDATE ops_metrics_daily AS canonical
SET success_count = canonical.success_count + legacy.success_count,
    ttft_sample_count = canonical.ttft_sample_count + legacy.ttft_sample_count,
    error_count_total = canonical.error_count_total + legacy.error_count_total,
    business_limited_count = canonical.business_limited_count + legacy.business_limited_count,
    error_count_sla = canonical.error_count_sla + legacy.error_count_sla,
    upstream_error_count_excl_429_529 = canonical.upstream_error_count_excl_429_529 + legacy.upstream_error_count_excl_429_529,
    upstream_429_count = canonical.upstream_429_count + legacy.upstream_429_count,
    upstream_529_count = canonical.upstream_529_count + legacy.upstream_529_count,
    token_consumed = canonical.token_consumed + legacy.token_consumed,
    duration_p50_ms = CASE
        WHEN canonical.duration_p50_ms IS NULL THEN legacy.duration_p50_ms
        WHEN legacy.duration_p50_ms IS NULL THEN canonical.duration_p50_ms
        WHEN canonical.success_count + legacy.success_count > 0 THEN ROUND(
            (canonical.duration_p50_ms::numeric * canonical.success_count + legacy.duration_p50_ms::numeric * legacy.success_count)
            / (canonical.success_count + legacy.success_count)
        )::integer
        ELSE GREATEST(canonical.duration_p50_ms, legacy.duration_p50_ms)
    END,
    duration_p90_ms = CASE
        WHEN canonical.duration_p90_ms IS NULL THEN legacy.duration_p90_ms
        WHEN legacy.duration_p90_ms IS NULL THEN canonical.duration_p90_ms
        WHEN canonical.success_count + legacy.success_count > 0 THEN ROUND(
            (canonical.duration_p90_ms::numeric * canonical.success_count + legacy.duration_p90_ms::numeric * legacy.success_count)
            / (canonical.success_count + legacy.success_count)
        )::integer
        ELSE GREATEST(canonical.duration_p90_ms, legacy.duration_p90_ms)
    END,
    duration_p95_ms = GREATEST(canonical.duration_p95_ms, legacy.duration_p95_ms),
    duration_p99_ms = GREATEST(canonical.duration_p99_ms, legacy.duration_p99_ms),
    duration_avg_ms = CASE
        WHEN canonical.duration_avg_ms IS NULL THEN legacy.duration_avg_ms
        WHEN legacy.duration_avg_ms IS NULL THEN canonical.duration_avg_ms
        WHEN canonical.success_count + legacy.success_count > 0 THEN
            (canonical.duration_avg_ms * canonical.success_count + legacy.duration_avg_ms * legacy.success_count)
            / (canonical.success_count + legacy.success_count)
        ELSE canonical.duration_avg_ms
    END,
    duration_max_ms = GREATEST(canonical.duration_max_ms, legacy.duration_max_ms),
    ttft_p50_ms = CASE
        WHEN canonical.ttft_p50_ms IS NULL THEN legacy.ttft_p50_ms
        WHEN legacy.ttft_p50_ms IS NULL THEN canonical.ttft_p50_ms
        WHEN canonical.ttft_sample_count + legacy.ttft_sample_count > 0 THEN ROUND(
            (canonical.ttft_p50_ms::numeric * canonical.ttft_sample_count + legacy.ttft_p50_ms::numeric * legacy.ttft_sample_count)
            / (canonical.ttft_sample_count + legacy.ttft_sample_count)
        )::integer
        ELSE GREATEST(canonical.ttft_p50_ms, legacy.ttft_p50_ms)
    END,
    ttft_p90_ms = CASE
        WHEN canonical.ttft_p90_ms IS NULL THEN legacy.ttft_p90_ms
        WHEN legacy.ttft_p90_ms IS NULL THEN canonical.ttft_p90_ms
        WHEN canonical.ttft_sample_count + legacy.ttft_sample_count > 0 THEN ROUND(
            (canonical.ttft_p90_ms::numeric * canonical.ttft_sample_count + legacy.ttft_p90_ms::numeric * legacy.ttft_sample_count)
            / (canonical.ttft_sample_count + legacy.ttft_sample_count)
        )::integer
        ELSE GREATEST(canonical.ttft_p90_ms, legacy.ttft_p90_ms)
    END,
    ttft_p95_ms = GREATEST(canonical.ttft_p95_ms, legacy.ttft_p95_ms),
    ttft_p99_ms = GREATEST(canonical.ttft_p99_ms, legacy.ttft_p99_ms),
    ttft_avg_ms = CASE
        WHEN canonical.ttft_avg_ms IS NULL THEN legacy.ttft_avg_ms
        WHEN legacy.ttft_avg_ms IS NULL THEN canonical.ttft_avg_ms
        WHEN canonical.ttft_sample_count + legacy.ttft_sample_count > 0 THEN
            (canonical.ttft_avg_ms * canonical.ttft_sample_count + legacy.ttft_avg_ms * legacy.ttft_sample_count)
            / (canonical.ttft_sample_count + legacy.ttft_sample_count)
        ELSE canonical.ttft_avg_ms
    END,
    ttft_max_ms = GREATEST(canonical.ttft_max_ms, legacy.ttft_max_ms),
    computed_at = GREATEST(canonical.computed_at, legacy.computed_at)
FROM ops_metrics_daily AS legacy
WHERE canonical.bucket_date = legacy.bucket_date
  AND COALESCE(canonical.group_id, 0) = COALESCE(legacy.group_id, 0)
  AND canonical.platform = 'opencode'
  AND legacy.platform = 'opencode_go';
DELETE FROM ops_metrics_daily AS legacy
USING ops_metrics_daily AS canonical
WHERE canonical.bucket_date = legacy.bucket_date
  AND COALESCE(canonical.group_id, 0) = COALESCE(legacy.group_id, 0)
  AND canonical.platform = 'opencode'
  AND legacy.platform = 'opencode_go';
UPDATE ops_metrics_daily SET platform = 'opencode' WHERE platform = 'opencode_go';

ALTER TABLE composite_model_routes
    DROP CONSTRAINT IF EXISTS composite_model_routes_target_platform_check;

UPDATE composite_model_routes
SET target_platform = 'opencode', updated_at = NOW()
WHERE target_platform = 'opencode_go';

ALTER TABLE composite_model_routes
    ADD CONSTRAINT composite_model_routes_target_platform_check
    CHECK (target_platform IN ('anthropic', 'openai', 'gemini', 'antigravity', 'grok',
                               'kimi', 'zhipu', 'deepseek', 'minimax', 'opencode'));

ALTER TABLE channel_monitors
    DROP CONSTRAINT IF EXISTS channel_monitors_provider_check;
ALTER TABLE channel_monitor_request_templates
    DROP CONSTRAINT IF EXISTS channel_monitor_request_templates_provider_check;

-- Keep same-named Go and Zen templates as separate reusable configurations. Pick
-- a suffix unused by either provider so user-created names cannot block migration.
DO $$
DECLARE
    template_row RECORD;
    candidate_name TEXT;
    suffix TEXT;
    attempt INTEGER;
BEGIN
    FOR template_row IN
        SELECT legacy.id, legacy.name
        FROM channel_monitor_request_templates AS legacy
        WHERE legacy.provider = 'opencode_go'
          AND EXISTS (
              SELECT 1
              FROM channel_monitor_request_templates AS canonical
              WHERE canonical.provider = 'opencode'
                AND canonical.name = legacy.name
          )
        ORDER BY legacy.id
    LOOP
        attempt := 0;
        LOOP
            suffix := ' [Go#' || template_row.id::text || '-' || attempt::text || ']';
            candidate_name := LEFT(template_row.name, GREATEST(0, 100 - length(suffix))) || suffix;
            EXIT WHEN NOT EXISTS (
                SELECT 1
                FROM channel_monitor_request_templates AS existing
                WHERE existing.provider IN ('opencode', 'opencode_go')
                  AND existing.id <> template_row.id
                  AND existing.name = candidate_name
            );
            attempt := attempt + 1;
        END LOOP;

        UPDATE channel_monitor_request_templates
        SET name = candidate_name
        WHERE id = template_row.id;
    END LOOP;
END $$;

UPDATE channel_monitors
SET provider = 'opencode', updated_at = NOW()
WHERE provider = 'opencode_go';
UPDATE channel_monitor_request_templates
SET provider = 'opencode', updated_at = NOW()
WHERE provider = 'opencode_go';

ALTER TABLE channel_monitors
    ADD CONSTRAINT channel_monitors_provider_check
    CHECK (provider IN ('openai', 'anthropic', 'gemini', 'grok', 'opencode',
                        'clinepass', 'openrouter', 'commandcode', 'antigravity',
                        'antigravity_claude', 'antigravity_gemini', 'kimi', 'zhipu',
                        'deepseek', 'minimax'));
ALTER TABLE channel_monitor_request_templates
    ADD CONSTRAINT channel_monitor_request_templates_provider_check
    CHECK (provider IN ('openai', 'anthropic', 'gemini', 'grok', 'opencode',
                        'clinepass', 'openrouter', 'commandcode', 'antigravity',
                        'antigravity_claude', 'antigravity_gemini', 'kimi', 'zhipu',
                        'deepseek', 'minimax'));

UPDATE user_platform_quotas
SET platform = 'opencode', updated_at = NOW()
WHERE platform = 'opencode_go';

ALTER TABLE user_platform_quotas
    ADD CONSTRAINT user_platform_quotas_platform_check
    CHECK (platform IN ('anthropic', 'openai', 'opencode', 'clinepass', 'openrouter',
                        'commandcode', 'gemini', 'antigravity', 'grok', 'kimi',
                        'zhipu', 'deepseek', 'minimax'));

-- Rename old JSON map keys. If both IDs exist, retain the more restrictive
-- platform quota/threshold while preserving other canonical fields.
DO $$
DECLARE
    setting_row RECORD;
    document JSONB;
    legacy_value JSONB;
    canonical_value JSONB;
    legacy_field JSONB;
    canonical_field JSONB;
    merged_value JSONB;
    field_name TEXT;
BEGIN
    FOR setting_row IN
        SELECT id, key, value
        FROM settings
        WHERE key IN ('default_platform_quotas', 'account_scheduling_thresholds')
           OR key LIKE 'auth_source_default_%_platform_quotas'
    LOOP
        BEGIN
            document := setting_row.value::jsonb;
        EXCEPTION WHEN others THEN
            CONTINUE;
        END;
        IF jsonb_typeof(document) <> 'object' OR NOT (document ? 'opencode_go') THEN
            CONTINUE;
        END IF;

        legacy_value := document -> 'opencode_go';
        canonical_value := document -> 'opencode';
        IF canonical_value IS NULL OR canonical_value = 'null'::jsonb THEN
            document := jsonb_set(document, '{opencode}', legacy_value, TRUE);
        ELSIF setting_row.key = 'account_scheduling_thresholds'
          AND jsonb_typeof(legacy_value) = 'number'
          AND jsonb_typeof(canonical_value) = 'number' THEN
            document := jsonb_set(document, '{opencode}', to_jsonb(LEAST(legacy_value::text::numeric, canonical_value::text::numeric)), TRUE);
        ELSIF setting_row.key <> 'account_scheduling_thresholds'
          AND jsonb_typeof(legacy_value) = 'object'
          AND jsonb_typeof(canonical_value) = 'object' THEN
            merged_value := legacy_value || canonical_value;
            FOREACH field_name IN ARRAY ARRAY['daily', 'weekly', 'monthly'] LOOP
                legacy_field := legacy_value -> field_name;
                canonical_field := canonical_value -> field_name;
                IF legacy_field IS NULL OR legacy_field = 'null'::jsonb THEN
                    CONTINUE;
                ELSIF canonical_field IS NULL OR canonical_field = 'null'::jsonb THEN
                    merged_value := jsonb_set(merged_value, ARRAY[field_name], legacy_field, TRUE);
                ELSIF jsonb_typeof(legacy_field) = 'number' AND jsonb_typeof(canonical_field) = 'number' THEN
                    merged_value := jsonb_set(merged_value, ARRAY[field_name], to_jsonb(LEAST(legacy_field::text::numeric, canonical_field::text::numeric)), TRUE);
                END IF;
            END LOOP;
            document := jsonb_set(document, '{opencode}', merged_value, TRUE);
        END IF;

        document := document - 'opencode_go';
        UPDATE settings SET value = document::text, updated_at = NOW() WHERE id = setting_row.id;
    END LOOP;
END $$;

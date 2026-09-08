-- 保存请求实际采用的动态分组倍率，并回填动态账本覆盖范围内的现有使用记录。
ALTER TABLE usage_logs
    ADD COLUMN IF NOT EXISTS dynamic_rate_multiplier DECIMAL(10,4);

UPDATE usage_logs AS log
SET dynamic_rate_multiplier = event.resolved_multiplier
FROM dynamic_rate_usage_events AS event
WHERE log.dynamic_rate_multiplier IS NULL
  AND log.request_id = event.request_id
  AND log.api_key_id = event.api_key_id;

COMMENT ON COLUMN usage_logs.dynamic_rate_multiplier IS 'Resolved dynamic group rate for this token request; NULL when dynamic group billing was not applied';

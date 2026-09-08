-- 把 MiniMax 加入国产供应商平台白名单：
--   1. user_platform_quotas.platform CHECK
--   2. composite_model_routes.target_platform CHECK
--   3. channel_monitors / channel_monitor_request_templates.provider CHECK
--
-- 重建约束时必须保留此前迁移加入的本地平台；否则存量配额或监控记录会阻断升级。

ALTER TABLE user_platform_quotas
    DROP CONSTRAINT IF EXISTS user_platform_quotas_platform_check;

ALTER TABLE user_platform_quotas
    ADD CONSTRAINT user_platform_quotas_platform_check
    CHECK (platform IN (
        'anthropic',
        'openai',
        'opencode_go',
        'clinepass',
        'openrouter',
        'commandcode',
        'gemini',
        'antigravity',
        'grok',
        'kimi',
        'zhipu',
        'deepseek',
        'minimax'
    ));

ALTER TABLE composite_model_routes
    DROP CONSTRAINT IF EXISTS composite_model_routes_target_platform_check;

ALTER TABLE composite_model_routes
    ADD CONSTRAINT composite_model_routes_target_platform_check
    CHECK (target_platform IN ('anthropic', 'openai', 'gemini', 'antigravity', 'grok',
                               'kimi', 'zhipu', 'deepseek', 'minimax'));

DO $$
DECLARE
    monitor_constraint_def TEXT;
    template_constraint_def TEXT;
BEGIN
    SELECT pg_get_constraintdef(c.oid)
      INTO monitor_constraint_def
      FROM pg_constraint c
     WHERE c.conrelid = 'channel_monitors'::regclass
       AND c.conname = 'channel_monitors_provider_check';

    IF monitor_constraint_def IS NULL OR position('minimax' IN monitor_constraint_def) = 0 THEN
        ALTER TABLE channel_monitors
            DROP CONSTRAINT IF EXISTS channel_monitors_provider_check;
        ALTER TABLE channel_monitors
            ADD CONSTRAINT channel_monitors_provider_check
            CHECK (provider IN ('openai', 'anthropic', 'gemini', 'grok',
                                'opencode_go', 'clinepass', 'openrouter', 'commandcode',
                                'antigravity', 'antigravity_claude', 'antigravity_gemini',
                                'kimi', 'zhipu', 'deepseek', 'minimax'));
    END IF;

    SELECT pg_get_constraintdef(c.oid)
      INTO template_constraint_def
      FROM pg_constraint c
     WHERE c.conrelid = 'channel_monitor_request_templates'::regclass
       AND c.conname = 'channel_monitor_request_templates_provider_check';

    IF template_constraint_def IS NULL OR position('minimax' IN template_constraint_def) = 0 THEN
        ALTER TABLE channel_monitor_request_templates
            DROP CONSTRAINT IF EXISTS channel_monitor_request_templates_provider_check;
        ALTER TABLE channel_monitor_request_templates
            ADD CONSTRAINT channel_monitor_request_templates_provider_check
            CHECK (provider IN ('openai', 'anthropic', 'gemini', 'grok',
                                'opencode_go', 'clinepass', 'openrouter', 'commandcode',
                                'antigravity', 'antigravity_claude', 'antigravity_gemini',
                                'kimi', 'zhipu', 'deepseek', 'minimax'));
    END IF;
END $$;

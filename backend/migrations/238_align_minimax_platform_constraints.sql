-- 修复旧版 237_add_minimax_platform.sql 缩窄平台约束的问题。
--
-- 旧版 237 在没有本地平台存量数据的环境中可能已经成功应用并被记录，后续不会重跑；
-- 本迁移重新对齐完整应用平台集合。所有约束都是现有合法集合的超集，可安全重复执行。

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

ALTER TABLE channel_monitors
    DROP CONSTRAINT IF EXISTS channel_monitors_provider_check;

ALTER TABLE channel_monitors
    ADD CONSTRAINT channel_monitors_provider_check
    CHECK (provider IN ('openai', 'anthropic', 'gemini', 'grok',
                        'opencode_go', 'clinepass', 'openrouter', 'commandcode',
                        'antigravity', 'antigravity_claude', 'antigravity_gemini',
                        'kimi', 'zhipu', 'deepseek', 'minimax'));

ALTER TABLE channel_monitor_request_templates
    DROP CONSTRAINT IF EXISTS channel_monitor_request_templates_provider_check;

ALTER TABLE channel_monitor_request_templates
    ADD CONSTRAINT channel_monitor_request_templates_provider_check
    CHECK (provider IN ('openai', 'anthropic', 'gemini', 'grok',
                        'opencode_go', 'clinepass', 'openrouter', 'commandcode',
                        'antigravity', 'antigravity_claude', 'antigravity_gemini',
                        'kimi', 'zhipu', 'deepseek', 'minimax'));

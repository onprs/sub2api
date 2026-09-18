-- 修复已经记录旧版 238_opencode_go_platform.sql 的数据库平台约束。
-- 旧版迁移可能已经执行，但没有恢复定制平台白名单；本迁移同时适用于
-- 官方数据库和定制数据库，并且可以安全重复执行。

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
    CHECK (target_platform IN (
        'anthropic',
        'openai',
        'gemini',
        'antigravity',
        'grok',
        'kimi',
        'zhipu',
        'deepseek',
        'minimax',
        'opencode_go'
    ));

ALTER TABLE channel_monitors
    DROP CONSTRAINT IF EXISTS channel_monitors_provider_check;

ALTER TABLE channel_monitors
    ADD CONSTRAINT channel_monitors_provider_check
    CHECK (provider IN (
        'openai',
        'anthropic',
        'gemini',
        'grok',
        'opencode_go',
        'clinepass',
        'openrouter',
        'commandcode',
        'antigravity',
        'antigravity_claude',
        'antigravity_gemini',
        'kimi',
        'zhipu',
        'deepseek',
        'minimax'
    ));

ALTER TABLE channel_monitor_request_templates
    DROP CONSTRAINT IF EXISTS channel_monitor_request_templates_provider_check;

ALTER TABLE channel_monitor_request_templates
    ADD CONSTRAINT channel_monitor_request_templates_provider_check
    CHECK (provider IN (
        'openai',
        'anthropic',
        'gemini',
        'grok',
        'opencode_go',
        'clinepass',
        'openrouter',
        'commandcode',
        'antigravity',
        'antigravity_claude',
        'antigravity_gemini',
        'kimi',
        'zhipu',
        'deepseek',
        'minimax'
    ));

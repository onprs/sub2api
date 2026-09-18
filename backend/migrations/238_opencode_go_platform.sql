-- 将 OpenCode 作为一等平台加入账号（Zen / GO 模式）。
--
-- 定制版本同时支持 ClinePass、OpenRouter、Command Code 和国产供应商，
-- quota 与监控约束必须与完整应用平台集合保持一致。组合路由只允许代码中
-- 定义的 concrete 请求平台。
--
-- 在 237_add_minimax_platform.sql 后执行。DROP ... IF EXISTS 保证幂等，
-- 同时保留 MiniMax 及更早加入的本地平台。

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

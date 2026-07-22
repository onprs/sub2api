-- Add the independent ClinePass platform to quota and Channel Monitor contracts.

ALTER TABLE user_platform_quotas
    DROP CONSTRAINT IF EXISTS user_platform_quotas_platform_check;

ALTER TABLE user_platform_quotas
    ADD CONSTRAINT user_platform_quotas_platform_check
    CHECK (platform IN ('anthropic', 'openai', 'opencode_go', 'clinepass', 'gemini', 'antigravity', 'grok'));

ALTER TABLE channel_monitors
    DROP CONSTRAINT IF EXISTS channel_monitors_provider_check;

ALTER TABLE channel_monitors
    ADD CONSTRAINT channel_monitors_provider_check
    CHECK (provider IN (
        'openai',
        'anthropic',
        'gemini',
        'opencode_go',
        'clinepass',
        'antigravity_claude',
        'antigravity_gemini'
    ));

ALTER TABLE channel_monitor_request_templates
    DROP CONSTRAINT IF EXISTS channel_monitor_request_templates_provider_check;

ALTER TABLE channel_monitor_request_templates
    ADD CONSTRAINT channel_monitor_request_templates_provider_check
    CHECK (provider IN (
        'openai',
        'anthropic',
        'gemini',
        'opencode_go',
        'clinepass',
        'antigravity_claude',
        'antigravity_gemini'
    ));

INSERT INTO channel_monitor_request_templates (
    name, provider, api_mode, description, extra_headers, body_override_mode, body_override
)
VALUES (
    'ClinePass Chat Completions default check',
    'clinepass',
    'chat_completions',
    'Checks ClinePass through POST /chat/completions. Use endpoint https://api.cline.bot/api/v1.',
    '{}'::jsonb,
    'off',
    NULL
)
ON CONFLICT (provider, name) DO NOTHING;

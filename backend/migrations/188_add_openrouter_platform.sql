-- Add the independent OpenRouter platform to quota and Channel Monitor contracts.

ALTER TABLE user_platform_quotas
    DROP CONSTRAINT IF EXISTS user_platform_quotas_platform_check;

ALTER TABLE user_platform_quotas
    ADD CONSTRAINT user_platform_quotas_platform_check
    CHECK (platform IN ('anthropic', 'openai', 'opencode_go', 'clinepass', 'openrouter', 'gemini', 'antigravity', 'grok'));

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
        'openrouter',
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
        'openrouter',
        'antigravity_claude',
        'antigravity_gemini'
    ));

INSERT INTO channel_monitor_request_templates (
    name, provider, api_mode, description, extra_headers, body_override_mode, body_override
)
VALUES (
    'OpenRouter Chat Completions default check',
    'openrouter',
    'chat_completions',
    'Checks OpenRouter through POST /chat/completions. Prefer the current Sub2API service with a local OpenRouter group key; use https://openrouter.ai/api/v1 only with an OpenRouter-issued API key.',
    '{}'::jsonb,
    'off',
    NULL
)
ON CONFLICT (provider, name) DO NOTHING;

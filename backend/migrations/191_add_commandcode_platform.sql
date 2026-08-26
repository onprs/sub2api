-- Migration: 191_add_commandcode_platform
-- Add Command Code to quota and Channel Monitor database contracts.

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
        'grok'
    ));

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
        'commandcode',
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
        'commandcode',
        'antigravity_claude',
        'antigravity_gemini'
    ));

INSERT INTO channel_monitor_request_templates (
    name, provider, api_mode, description, extra_headers, body_override_mode, body_override
)
VALUES
(
    'Command Code Chat Completions default check',
    'commandcode',
    'chat_completions',
    'Checks Command Code through POST /provider/v1/chat/completions. Use a Command Code Provider API key and https://api.commandcode.ai as the endpoint.',
    '{}'::jsonb,
    'off',
    NULL
),
(
    'Command Code Messages default check',
    'commandcode',
    'messages',
    'Checks Command Code Anthropic models through POST /provider/v1/messages. Use a claude-* model with a Command Code Provider API key.',
    '{}'::jsonb,
    'off',
    NULL
)
ON CONFLICT (provider, name) DO NOTHING;

-- Migration: 153_channel_monitor_opencode_go_provider
-- Allow Channel Monitor to probe OpenCode Go with chat_completions or messages api_mode.

ALTER TABLE channel_monitors
    DROP CONSTRAINT IF EXISTS channel_monitors_provider_check;

ALTER TABLE channel_monitors
    ADD CONSTRAINT channel_monitors_provider_check
    CHECK (provider IN (
        'openai',
        'anthropic',
        'gemini',
        'opencode_go',
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
        'antigravity_claude',
        'antigravity_gemini'
    ));

ALTER TABLE channel_monitors
    DROP CONSTRAINT IF EXISTS channel_monitors_api_mode_check;

ALTER TABLE channel_monitors
    ADD CONSTRAINT channel_monitors_api_mode_check
    CHECK (api_mode IN ('chat_completions', 'responses', 'messages'));

ALTER TABLE channel_monitor_request_templates
    DROP CONSTRAINT IF EXISTS channel_monitor_request_templates_api_mode_check;

ALTER TABLE channel_monitor_request_templates
    ADD CONSTRAINT channel_monitor_request_templates_api_mode_check
    CHECK (api_mode IN ('chat_completions', 'responses', 'messages'));

INSERT INTO channel_monitor_request_templates (
    name, provider, api_mode, description, extra_headers, body_override_mode, body_override
)
VALUES
(
    'OpenCode Go Chat Completions 默认检测',
    'opencode_go',
    'chat_completions',
    '适用于 OpenCode Go OpenAI-compatible 模型：POST /chat/completions，Endpoint 建议填写 https://opencode.ai/zen/go/v1。',
    '{}'::jsonb,
    'off',
    NULL
),
(
    'OpenCode Go Messages 默认检测',
    'opencode_go',
    'messages',
    '适用于 OpenCode Go Anthropic-style 模型：POST /messages，Endpoint 建议填写 https://opencode.ai/zen/go/v1。',
    '{}'::jsonb,
    'off',
    NULL
)
ON CONFLICT (provider, name) DO NOTHING;

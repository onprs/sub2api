-- Migration: 151_channel_monitor_antigravity_providers
-- 让渠道监控支持 Antigravity 的 Claude / Gemini 两种协议。
-- provider 字符串保持在 VARCHAR(20) 内：
--   antigravity_claude / antigravity_gemini

ALTER TABLE channel_monitors
    DROP CONSTRAINT IF EXISTS channel_monitors_provider_check;

ALTER TABLE channel_monitors
    ADD CONSTRAINT channel_monitors_provider_check
    CHECK (provider IN (
        'openai',
        'anthropic',
        'gemini',
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
        'antigravity_claude',
        'antigravity_gemini'
    ));

INSERT INTO channel_monitor_request_templates (
    name, provider, api_mode, description, extra_headers, body_override_mode, body_override
)
VALUES
(
    'Antigravity Claude 默认检测',
    'antigravity_claude',
    'chat_completions',
    '适用于本站 Antigravity Claude：POST /antigravity/v1/messages，Endpoint 只填写 https://api.onprs.top。',
    '{}'::jsonb,
    'off',
    NULL
),
(
    'Antigravity Gemini 默认检测',
    'antigravity_gemini',
    'chat_completions',
    '适用于本站 Antigravity Gemini：POST /antigravity/v1beta/models/{model}:generateContent，Endpoint 只填写 https://api.onprs.top。',
    '{}'::jsonb,
    'off',
    NULL
)
ON CONFLICT (provider, name) DO NOTHING;

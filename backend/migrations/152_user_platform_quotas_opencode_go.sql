-- Migration: 152_user_platform_quotas_opencode_go
-- Allow OpenCode Go to participate in user x platform quota limits.

ALTER TABLE user_platform_quotas
    DROP CONSTRAINT IF EXISTS user_platform_quotas_platform_check;

ALTER TABLE user_platform_quotas
    ADD CONSTRAINT user_platform_quotas_platform_check
    CHECK (platform IN ('anthropic', 'openai', 'opencode_go', 'gemini', 'antigravity'));

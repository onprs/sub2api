-- Align user_platform_quotas.platform CHECK with the full supported quota platform list.
--
-- This migration is intentionally kept after 157 so environments that already applied
-- the older grok migration variant (which omitted opencode_go) are repaired without
-- manual schema_migrations edits. It is also harmless after the fixed 157.
ALTER TABLE user_platform_quotas
    DROP CONSTRAINT IF EXISTS user_platform_quotas_platform_check;

ALTER TABLE user_platform_quotas
    ADD CONSTRAINT user_platform_quotas_platform_check
    CHECK (platform IN ('anthropic', 'openai', 'opencode_go', 'gemini', 'antigravity', 'grok'));

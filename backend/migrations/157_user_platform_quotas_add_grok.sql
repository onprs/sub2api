-- 把 grok 平台加入 user_platform_quotas.platform 的 CHECK 约束。
--
-- 背景：grok 自 2026-06 起进入默认平台配额（default_platform_quotas /
-- auth_source_default_*_platform_quotas），但 142 建表时的 CHECK 仅允许
-- anthropic/openai/gemini/antigravity；本地定制迁移 152 额外加入了 opencode_go。
-- 官方 157 若直接重建为 anthropic/openai/gemini/antigravity/grok，会把线上已有
-- opencode_go 配额行排除在外，导致 ADD CONSTRAINT 校验存量数据失败并阻断启动迁移。
--
-- 修复：把约束与当前代码平台列表对齐，保留 opencode_go 并加入 grok。
-- DROP ... IF EXISTS 保证可重入；新约束是 152 约束的超集，存量 opencode_go 行可通过校验。
ALTER TABLE user_platform_quotas
    DROP CONSTRAINT IF EXISTS user_platform_quotas_platform_check;

ALTER TABLE user_platform_quotas
    ADD CONSTRAINT user_platform_quotas_platform_check
    CHECK (platform IN ('anthropic', 'openai', 'opencode_go', 'gemini', 'antigravity', 'grok'));

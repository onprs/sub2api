-- 将 Antigravity 渠道监控统一为官方平台 ID，使用 api_mode 保留原请求协议。
-- 账号、分组、渠道定价和用户配额本来就使用 antigravity，无需迁移计费数据。
ALTER TABLE channel_monitors
    DROP CONSTRAINT IF EXISTS channel_monitors_provider_check,
    DROP CONSTRAINT IF EXISTS channel_monitors_api_mode_check;
ALTER TABLE channel_monitor_request_templates
    DROP CONSTRAINT IF EXISTS channel_monitor_request_templates_provider_check,
    DROP CONSTRAINT IF EXISTS channel_monitor_request_templates_api_mode_check;

UPDATE channel_monitors
SET provider = 'antigravity',
    api_mode = CASE WHEN provider = 'antigravity_gemini' THEN 'generate_content' ELSE 'messages' END
WHERE provider IN ('antigravity_claude', 'antigravity_gemini')
   OR (provider = 'antigravity' AND api_mode = 'chat_completions');

-- 合并前两个 provider 允许同名模板。逐条迁移，保留模板 ID、请求快照和关联关系。
-- 发生名称冲突时追加唯一后缀，同时满足原来的 100 字符限制。
DO $$
DECLARE
    template RECORD;
    candidate_name TEXT;
    name_suffix TEXT;
    collision_number INTEGER;
BEGIN
    FOR template IN
        SELECT id, name, provider
        FROM channel_monitor_request_templates
        WHERE provider IN ('antigravity_claude', 'antigravity_gemini')
        ORDER BY id
    LOOP
        candidate_name := template.name;
        collision_number := 0;
        WHILE EXISTS (
            SELECT 1 FROM channel_monitor_request_templates
            WHERE provider = 'antigravity' AND name = candidate_name
        ) LOOP
            collision_number := collision_number + 1;
            name_suffix := ' [' || template.id::TEXT
                || CASE WHEN collision_number > 1 THEN '-' || collision_number::TEXT ELSE '' END || ']';
            candidate_name := LEFT(template.name, 100 - LENGTH(name_suffix)) || name_suffix;
        END LOOP;

        UPDATE channel_monitor_request_templates
        SET provider = 'antigravity',
            api_mode = CASE WHEN template.provider = 'antigravity_gemini' THEN 'generate_content' ELSE 'messages' END,
            name = candidate_name
        WHERE id = template.id;
    END LOOP;
END $$;

UPDATE channel_monitor_request_templates
SET api_mode = 'messages'
WHERE provider = 'antigravity' AND api_mode = 'chat_completions';

ALTER TABLE channel_monitors
    ADD CONSTRAINT channel_monitors_provider_check CHECK (provider IN (
        'openai', 'anthropic', 'gemini', 'grok', 'opencode_go', 'cline', 'openrouter',
        'command_code', 'antigravity', 'kimi', 'zhipu', 'deepseek', 'minimax'
    )),
    ADD CONSTRAINT channel_monitors_api_mode_check
        CHECK (api_mode IN ('chat_completions', 'responses', 'messages', 'generate_content'));
ALTER TABLE channel_monitor_request_templates
    ADD CONSTRAINT channel_monitor_request_templates_provider_check CHECK (provider IN (
        'openai', 'anthropic', 'gemini', 'grok', 'opencode_go', 'cline', 'openrouter',
        'command_code', 'antigravity', 'kimi', 'zhipu', 'deepseek', 'minimax'
    )),
    ADD CONSTRAINT channel_monitor_request_templates_api_mode_check
        CHECK (api_mode IN ('chat_completions', 'responses', 'messages', 'generate_content'));

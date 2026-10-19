//go:build integration

package migrations

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestUnifyAntigravityChannelMonitor(t *testing.T) {
	dsn := os.Getenv("SUB2API_MIGRATION_TEST_DSN")
	if dsn == "" {
		t.Skip("需要 SUB2API_MIGRATION_TEST_DSN 指向独立 PostgreSQL 测试库")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	schema := fmt.Sprintf("antigravity_monitor_test_%d", time.Now().UnixNano())
	_, err = tx.ExecContext(ctx, `CREATE SCHEMA "`+schema+`"; SET LOCAL search_path = "`+schema+`";`)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `
CREATE TABLE channel_monitor_request_templates (
    id BIGINT PRIMARY KEY, name VARCHAR(100), provider TEXT, api_mode TEXT,
    body_override JSONB, UNIQUE (provider, name)
);
CREATE TABLE channel_monitors (
    id BIGINT PRIMARY KEY, provider TEXT, api_mode TEXT,
    template_id BIGINT REFERENCES channel_monitor_request_templates(id),
    group_id BIGINT, account_id BIGINT, primary_model TEXT, extra_models JSONB,
    api_key TEXT, extra_headers JSONB, body_override JSONB, check_mode TEXT
);
CREATE TABLE channel_monitor_history (monitor_id BIGINT REFERENCES channel_monitors(id), status TEXT);
CREATE TABLE groups (id BIGINT, platform TEXT, rate_multiplier NUMERIC, model_pricing JSONB);
CREATE TABLE user_platform_quotas (platform TEXT, daily_usage_usd NUMERIC);
CREATE TABLE channel_model_pricing (platform TEXT, model TEXT, input_price NUMERIC);
INSERT INTO channel_monitor_request_templates VALUES
    (1, '探测模板', 'antigravity', 'chat_completions', '{}'),
    (2, '探测模板', 'antigravity_claude', 'chat_completions', '{"messages":[{"role":"user","content":"ping"}]}'),
    (3, '探测模板', 'antigravity_gemini', 'chat_completions', '{"contents":[{"parts":[{"text":"ping"}]}]}'),
    (4, '探测模板 [2]', 'antigravity', 'messages', '{}'),
    (5, repeat('模', 100), 'antigravity_claude', 'chat_completions', '{}'),
    (6, repeat('模', 100), 'antigravity_gemini', 'chat_completions', '{}'),
    (7, 'OpenAI', 'openai', 'responses', '{}');
INSERT INTO channel_monitors VALUES
    (1, 'antigravity_claude', 'chat_completions', 2, 41, 51, 'claude-sonnet-4-6', '["custom-alias"]', 'encrypted:key', '{"X-Client":"monitor"}', '{"max_tokens":20}', 'probe'),
    (2, 'antigravity_gemini', 'chat_completions', 3, 42, 52, 'custom-alias', '["gemini-3.1-pro-high"]', 'encrypted:key', '{}', '{"generationConfig":{"temperature":0}}', 'quota_probe'),
    (3, 'antigravity', 'chat_completions', NULL, 43, 53, 'quota', '[]', '', '{}', '{}', 'quota'),
    (4, 'openai', 'responses', 7, 44, 54, 'gpt-5.4', '[]', 'encrypted:openai', '{}', '{}', 'probe');
INSERT INTO channel_monitor_history VALUES (1, 'operational'), (2, 'degraded');
INSERT INTO groups VALUES (41, 'antigravity', 1.25, '[{"platform":"antigravity","models":["claude-sonnet-4-6"],"input_price":0.000003}]');
INSERT INTO user_platform_quotas VALUES ('antigravity', 12.75);
INSERT INTO channel_model_pricing VALUES ('antigravity', 'gemini-3.1-pro-high', 0.000002);
`)
	require.NoError(t, err)

	queryText := func(query string) string {
		t.Helper()
		var value string
		require.NoError(t, tx.QueryRowContext(ctx, query).Scan(&value))
		return value
	}
	// 除 provider、协议和冲突名称外，所有快照、关联、历史与计费数据必须逐字保留。
	unchangedQueries := []string{
		`SELECT jsonb_agg(to_jsonb(t) - 'provider' - 'api_mode' - 'name' ORDER BY id)::TEXT FROM channel_monitor_request_templates t`,
		`SELECT jsonb_agg(to_jsonb(m) - 'provider' - 'api_mode' ORDER BY id)::TEXT FROM channel_monitors m`,
		`SELECT jsonb_agg(to_jsonb(h) ORDER BY monitor_id)::TEXT FROM channel_monitor_history h`,
		`SELECT jsonb_agg(to_jsonb(g))::TEXT FROM groups g`,
		`SELECT jsonb_agg(to_jsonb(q))::TEXT FROM user_platform_quotas q`,
		`SELECT jsonb_agg(to_jsonb(p))::TEXT FROM channel_model_pricing p`,
	}
	before := make([]string, len(unchangedQueries))
	for i, query := range unchangedQueries {
		before[i] = queryText(query)
	}
	migration, err := FS.ReadFile("250_unify_antigravity_channel_monitor.sql")
	require.NoError(t, err)
	for range 2 {
		_, err = tx.ExecContext(ctx, string(migration))
		require.NoError(t, err, "迁移应可重复执行")
	}
	for i, query := range unchangedQueries {
		require.JSONEq(t, before[i], queryText(query), query)
	}
	require.Equal(t, "antigravity:messages", queryText(`SELECT provider || ':' || api_mode FROM channel_monitors WHERE id = 1`))
	require.Equal(t, "antigravity:generate_content", queryText(`SELECT provider || ':' || api_mode FROM channel_monitors WHERE id = 2`))
	require.Equal(t, "antigravity:messages", queryText(`SELECT provider || ':' || api_mode FROM channel_monitors WHERE id = 3`))
	require.Equal(t, "openai:responses", queryText(`SELECT provider || ':' || api_mode FROM channel_monitors WHERE id = 4`))
	require.Equal(t, "探测模板 [2-2]", queryText(`SELECT name FROM channel_monitor_request_templates WHERE id = 2`))
	require.Equal(t, "探测模板 [3]", queryText(`SELECT name FROM channel_monitor_request_templates WHERE id = 3`))
	require.Equal(t, "generate_content", queryText(`SELECT api_mode FROM channel_monitor_request_templates WHERE id = 3`))
	require.Equal(t, "100", queryText(`SELECT length(name)::TEXT FROM channel_monitor_request_templates WHERE id = 6`))
	require.Equal(t, "0", queryText(`SELECT count(*)::TEXT FROM channel_monitor_request_templates WHERE provider IN ('antigravity_claude', 'antigravity_gemini')`))

	// 数据迁移完成后拒绝重新写入旧平台 ID。
	_, err = tx.ExecContext(ctx, `SAVEPOINT legacy_provider; INSERT INTO channel_monitors (id, provider, api_mode) VALUES (99, 'antigravity_gemini', 'chat_completions')`)
	require.Error(t, err)
	_, err = tx.ExecContext(ctx, `ROLLBACK TO SAVEPOINT legacy_provider`)
	require.NoError(t, err)
}

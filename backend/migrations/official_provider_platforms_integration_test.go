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

// 使用独立 PostgreSQL 测试库，所有夹具均位于回滚事务内的临时 schema。
func TestUnifyOfficialProviderPlatforms(t *testing.T) {
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
	schema := fmt.Sprintf("official_platform_test_%d", time.Now().UnixNano())
	_, err = tx.ExecContext(ctx, `CREATE SCHEMA "`+schema+`"; SET LOCAL search_path = "`+schema+`";`)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `
CREATE TABLE accounts (id BIGINT PRIMARY KEY, platform TEXT, type TEXT, credentials JSONB, extra JSONB, updated_at TIMESTAMPTZ DEFAULT NOW());
CREATE TABLE groups (id BIGINT PRIMARY KEY, platform TEXT, model_pricing JSONB);
CREATE TABLE user_platform_quotas (user_id BIGINT, platform TEXT, daily_limit_usd NUMERIC, daily_usage_usd NUMERIC,
    UNIQUE (user_id, platform), CONSTRAINT user_platform_quotas_platform_check CHECK (platform IN ('clinepass', 'commandcode', 'cline', 'command_code')));
CREATE TABLE composite_model_routes (target_platform TEXT, CONSTRAINT composite_model_routes_target_platform_check CHECK (target_platform IN ('clinepass', 'commandcode')));
CREATE TABLE channel_monitors (provider TEXT, CONSTRAINT channel_monitors_provider_check CHECK (provider IN ('clinepass', 'commandcode')));
CREATE TABLE channel_monitor_request_templates (provider TEXT, CONSTRAINT channel_monitor_request_templates_provider_check CHECK (provider IN ('clinepass', 'commandcode')));
CREATE TABLE api_keys (routing_platform TEXT);
CREATE TABLE channel_model_pricing (platform TEXT);
CREATE TABLE channels (model_mapping JSONB);
CREATE TABLE error_passthrough_rules (platforms JSONB);
CREATE TABLE channel_monitor_v2_config (platforms JSONB);
CREATE TABLE ops_metrics_hourly (platform TEXT);
CREATE TABLE ops_alert_rules (filters JSONB);
CREATE TABLE ops_alert_events (dimensions JSONB);
CREATE TABLE settings (id BIGINT, key TEXT, value TEXT, updated_at TIMESTAMPTZ DEFAULT NOW());
INSERT INTO accounts VALUES
    (1, 'commandcode', 'apikey', '{"base_url":"https://api.commandcode.ai/","api_key":"fixture"}', '{"commandcode_usage_5h_used_percent":25,"commandcode_usage_5h_resets_at":"2026-10-10T15:00:00Z"}', NOW()),
    (2, 'clinepass', 'apikey', '{"base_url":"https://api.cline.bot/api/v1"}', '{"clinepass_usage_7d_used_percent":75,"cline_weekly_used_percent":10}', NOW()),
    (3, 'command_code', 'apikey', '{"base_url":"https://api.commandcode.ai/provider/v1"}', '{}', NOW()),
    (4, 'commandcode', 'apikey', '{"base_url":"https://relay.example.com/prefix/"}', '{}', NOW());
INSERT INTO groups VALUES (1, 'clinepass', '[{"platform":"commandcode","model":"commandcode"}]');
INSERT INTO user_platform_quotas VALUES (1, 'commandcode', 20, 7.5), (2, 'clinepass', 10, 3.5);
INSERT INTO composite_model_routes VALUES ('commandcode'), ('clinepass');
INSERT INTO channel_monitors VALUES ('commandcode');
INSERT INTO channel_monitor_request_templates VALUES ('clinepass');
INSERT INTO api_keys VALUES ('commandcode');
INSERT INTO channel_model_pricing VALUES ('clinepass');
INSERT INTO channels VALUES ('{"commandcode":{"commandcode":"wire-model","legacy":"legacy-wire"},"command_code":{"commandcode":"official-wire"}}');
INSERT INTO error_passthrough_rules VALUES ('["clinepass","commandcode","openai"]');
INSERT INTO channel_monitor_v2_config VALUES ('[{"platform":"clinepass","name":"commandcode"}]');
INSERT INTO ops_metrics_hourly VALUES ('commandcode');
INSERT INTO ops_alert_rules VALUES ('{"platform":"commandcode","model":"commandcode"}');
INSERT INTO ops_alert_events VALUES ('{"platform":"clinepass"}');
INSERT INTO settings VALUES
    (1, 'default_platform_quotas', '{"commandcode":{"daily":10,"weekly":20},"command_code":{"daily":0}}', NOW()),
    (2, 'auth_source_default_email_platform_quotas', '{"clinepass":{"monthly":30}}', NOW()),
    (3, 'account_scheduling_thresholds', 'invalid-json', NOW());
`)
	require.NoError(t, err)
	migration, err := FS.ReadFile("244_unify_official_provider_platforms.sql")
	require.NoError(t, err)
	for range 2 {
		_, err = tx.ExecContext(ctx, string(migration))
		require.NoError(t, err, "迁移应可重复执行")
	}
	assertText := func(query, expected string) {
		t.Helper()
		var actual string
		require.NoError(t, tx.QueryRowContext(ctx, query).Scan(&actual))
		require.Equal(t, expected, actual)
	}
	assertText(`SELECT platform FROM accounts WHERE id = 1`, "command_code")
	assertText(`SELECT platform FROM accounts WHERE id = 2`, "cline")
	assertText(`SELECT credentials->>'base_url' FROM accounts WHERE id = 1`, "https://api.commandcode.ai/provider/v1")
	assertText(`SELECT credentials->>'base_url' FROM accounts WHERE id = 3`, "https://api.commandcode.ai/provider/v1")
	assertText(`SELECT credentials->>'base_url' FROM accounts WHERE id = 4`, "https://relay.example.com/prefix/provider/v1")
	assertText(`SELECT extra->>'command_code_5h_used_percent' FROM accounts WHERE id = 1`, "25")
	assertText(`SELECT extra->>'cline_weekly_used_percent' FROM accounts WHERE id = 2`, "10")
	assertText(`SELECT daily_usage_usd::TEXT FROM user_platform_quotas WHERE user_id = 1 AND platform = 'command_code'`, "7.5")
	assertText(`SELECT platform FROM groups`, "cline")
	assertText(`SELECT model_pricing->0->>'platform' FROM groups`, "command_code")
	assertText(`SELECT model_pricing->0->>'model' FROM groups`, "commandcode")
	assertText(`SELECT model_mapping->'command_code'->>'commandcode' FROM channels`, "official-wire")
	assertText(`SELECT model_mapping->'command_code'->>'legacy' FROM channels`, "legacy-wire")
	assertText(`SELECT platforms->>0 FROM error_passthrough_rules`, "cline")
	assertText(`SELECT platforms->0->>'platform' FROM channel_monitor_v2_config`, "cline")
	assertText(`SELECT platforms->0->>'name' FROM channel_monitor_v2_config`, "commandcode")
	assertText(`SELECT routing_platform FROM api_keys`, "command_code")
	assertText(`SELECT provider FROM channel_monitors`, "command_code")
	assertText(`SELECT provider FROM channel_monitor_request_templates`, "cline")
	assertText(`SELECT platform FROM channel_model_pricing`, "cline")
	assertText(`SELECT platform FROM ops_metrics_hourly`, "command_code")
	assertText(`SELECT filters->>'platform' FROM ops_alert_rules`, "command_code")
	assertText(`SELECT filters->>'model' FROM ops_alert_rules`, "commandcode")
	assertText(`SELECT dimensions->>'platform' FROM ops_alert_events`, "cline")
	assertText(`SELECT value::JSONB->'command_code'->>'daily' FROM settings WHERE id = 1`, "0")
	assertText(`SELECT value::JSONB->'command_code'->>'weekly' FROM settings WHERE id = 1`, "20")
	assertText(`SELECT value::JSONB->'cline'->>'monthly' FROM settings WHERE id = 2`, "30")
	assertText(`SELECT value FROM settings WHERE id = 3`, "invalid-json")
	_, err = tx.ExecContext(ctx, `INSERT INTO user_platform_quotas VALUES (3, 'future_platform', 1, 0); INSERT INTO composite_model_routes VALUES ('future_platform')`)
	require.NoError(t, err, "官方平台清单独立于数据库白名单")

	// 同一用户同时具有旧、新 ID 的配额时应回滚，不能静默覆盖余额或用量。
	_, err = tx.ExecContext(ctx, `INSERT INTO user_platform_quotas VALUES (4, 'commandcode', 10, 2), (4, 'command_code', 20, 5); SAVEPOINT duplicate_quota`)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, string(migration))
	require.Error(t, err)
	_, err = tx.ExecContext(ctx, `ROLLBACK TO SAVEPOINT duplicate_quota`)
	require.NoError(t, err)
	assertText(`SELECT COUNT(*)::TEXT FROM user_platform_quotas WHERE user_id = 4`, "2")
}

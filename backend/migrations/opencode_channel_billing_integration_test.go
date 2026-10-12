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

const openCodeBillingMigrationFixture = `
CREATE TABLE channels (id BIGSERIAL PRIMARY KEY, name TEXT UNIQUE, description TEXT, status TEXT,
    billing_model_source TEXT, restrict_models BOOLEAN, features_config JSONB);
CREATE TABLE groups (id BIGINT PRIMARY KEY, platform TEXT, deleted_at TIMESTAMPTZ);
CREATE TABLE channel_groups (channel_id BIGINT REFERENCES channels(id), group_id BIGINT UNIQUE REFERENCES groups(id));
CREATE TABLE channel_model_pricing (id BIGSERIAL PRIMARY KEY, channel_id BIGINT REFERENCES channels(id),
    platform TEXT, models JSONB, billing_mode TEXT, input_price NUMERIC(20,10), output_price NUMERIC(20,10),
    cache_write_price NUMERIC(20,10), cache_read_price NUMERIC(20,10), time_pricing JSONB, monthly_quota JSONB);
CREATE TABLE channel_pricing_intervals (id BIGSERIAL PRIMARY KEY, pricing_id BIGINT REFERENCES channel_model_pricing(id),
    min_tokens INTEGER, max_tokens INTEGER, tier_label TEXT, input_price NUMERIC(20,10), output_price NUMERIC(20,10),
    cache_write_price NUMERIC(20,10), cache_read_price NUMERIC(20,10), sort_order INTEGER);
CREATE TABLE channel_account_stats_pricing_rules (id BIGSERIAL PRIMARY KEY, channel_id BIGINT REFERENCES channels(id),
    name TEXT, group_ids BIGINT[] NOT NULL DEFAULT '{}', account_ids BIGINT[] NOT NULL DEFAULT '{}', sort_order INTEGER);
CREATE TABLE accounts (id BIGINT PRIMARY KEY, platform TEXT, type TEXT, credentials JSONB, deleted_at TIMESTAMPTZ, updated_at TIMESTAMPTZ DEFAULT NOW());
INSERT INTO accounts (id, platform, type, credentials) VALUES
    (1, 'opencode_go', 'apikey', '{"account_mode":"go","api_protocol":"adaptive","protocol_rules":[{"pattern":"grok-*","protocol":"responses"},{"pattern":"gpt-*","protocol":"responses"},{"pattern":"muse-spark-*","protocol":"responses"},{"pattern":"minimax-*","protocol":"anthropic"},{"pattern":"qwen*","protocol":"anthropic"}]}'),
    (2, 'opencode_go', 'apikey', '{"account_mode":"go","api_protocol":"adaptive","protocol_rules":[{"pattern":"*","protocol":"responses"}]}');
INSERT INTO accounts (id, platform, type, credentials)
SELECT 3, platform, type, credentials || '{"account_mode":"zen"}'::JSONB FROM accounts WHERE id = 1;
INSERT INTO accounts (id, platform, type, credentials)
SELECT 4, platform, type, credentials || '{"api_protocol":"chat_completions"}'::JSONB FROM accounts WHERE id = 1;
INSERT INTO channels (name, features_config) VALUES ('OpenCode', '{}');
INSERT INTO groups VALUES (1, 'opencode_go', NULL), (2, 'opencode_go', NULL),
    (3, 'opencode_go', NOW()), (4, 'command_code', NULL);
INSERT INTO channel_groups VALUES (1, 2);
`

func TestOpenCodeChannelBillingMigration(t *testing.T) {
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
	schema := fmt.Sprintf("opencode_billing_test_%d", time.Now().UnixNano())
	_, err = tx.ExecContext(ctx, `CREATE SCHEMA "`+schema+`"; SET LOCAL search_path = "`+schema+`";`)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, openCodeBillingMigrationFixture)
	require.NoError(t, err)
	migration, err := FS.ReadFile("246_opencode_channel_billing.sql")
	require.NoError(t, err)
	for range 2 {
		_, err = tx.ExecContext(ctx, string(migration))
		require.NoError(t, err, "重复执行不能重复创建渠道或规则")
	}
	assertText := func(query, expected string) {
		t.Helper()
		var actual string
		require.NoError(t, tx.QueryRowContext(ctx, query).Scan(&actual))
		require.Equal(t, expected, actual)
	}
	assertText(`SELECT jsonb_array_length(credentials->'protocol_rules')::TEXT FROM accounts WHERE id = 1`, "6")
	assertText(`SELECT jsonb_array_length(credentials->'protocol_rules')::TEXT FROM accounts WHERE id = 2`, "1")
	assertText(`SELECT jsonb_array_length(credentials->'protocol_rules')::TEXT FROM accounts WHERE id = 3`, "5")
	assertText(`SELECT jsonb_array_length(credentials->'protocol_rules')::TEXT FROM accounts WHERE id = 4`, "5")
	assertText(`SELECT COUNT(*)::TEXT FROM channels`, "2")
	assertText(`SELECT name FROM channels WHERE id = 2`, "OpenCode (2)")
	assertText(`SELECT COUNT(*)::TEXT FROM channel_model_pricing`, "32")
	assertText(`SELECT COUNT(*)::TEXT FROM channel_pricing_intervals`, "12")
	assertText(`SELECT cache_read_price::TEXT FROM channel_model_pricing WHERE models->>0 = 'mimo-v2.6-pro'`, "0.000000003625")
	assertText(`SELECT COUNT(*)::TEXT FROM channel_model_pricing WHERE input_price = 0 AND output_price = 0`, "2")
	assertText(`SELECT channel_id::TEXT FROM channel_groups WHERE group_id = 1`, "2")
	assertText(`SELECT channel_id::TEXT FROM channel_groups WHERE group_id = 2`, "1")
	assertText(`SELECT COUNT(*)::TEXT FROM channel_groups WHERE group_id IN (3, 4)`, "0")
	assertText(`SELECT COUNT(*)::TEXT FROM channel_account_stats_pricing_rules`, "1")
	assertText(`SELECT pricing_source || ':' || platform FROM channel_account_stats_pricing_rules`, "channel_model_multiplier:opencode_go")
	assertText(`SELECT cardinality(group_ids)::TEXT || ':' || cardinality(account_ids)::TEXT FROM channel_account_stats_pricing_rules`, "0:0")
}

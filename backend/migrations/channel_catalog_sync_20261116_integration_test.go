//go:build integration

package migrations

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestOfficialChannelCatalogSync20261116Migration(t *testing.T) {
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
	schema := fmt.Sprintf("catalog_sync_20261116_test_%d", time.Now().UnixNano())
	_, err = tx.ExecContext(ctx, fmt.Sprintf("CREATE SCHEMA %q; SET LOCAL search_path = %q;", schema, schema))
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, openCodeBillingMigrationFixture)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `ALTER TABLE channel_model_pricing
 ADD COLUMN updated_at TIMESTAMPTZ DEFAULT NOW(),
 ADD COLUMN cache_write_1h_price NUMERIC(20,10),
 ADD COLUMN image_input_price NUMERIC(20,12),
 ADD COLUMN image_output_price NUMERIC(20,12),
 ADD COLUMN fast_multiplier DOUBLE PRECISION,
 ADD COLUMN flex_multiplier DOUBLE PRECISION,
 ADD COLUMN max_reasoning_effort_multiplier DOUBLE PRECISION,
 ADD COLUMN reasoning_effort_multipliers JSONB DEFAULT '{}';
ALTER TABLE channel_pricing_intervals ADD COLUMN cache_write_1h_price NUMERIC(20,10)`)
	require.NoError(t, err)
	for _, name := range []string{
		"245_command_code_channel_billing.sql",
		"246_opencode_channel_billing.sql",
		"247_sync_official_channel_catalogs.sql",
		"248_free_model_billing_price.sql",
		"249_refresh_opencode_priced_catalog.sql",
		"251_sync_official_channel_catalogs_20261027.sql",
		"252_sync_opencode_gemini_catalog.sql",
		"253_sync_official_channel_catalogs_20261111.sql",
	} {
		body, readErr := FS.ReadFile(name)
		require.NoError(t, readErr)
		_, err = tx.ExecContext(ctx, string(body))
		require.NoError(t, err, name)
	}

	// 插入带默认 minimax-* 规则与已自定义 minimax-m3.1-flash 规则的 OpenCode Go 账号验证规则迁移。
	_, err = tx.ExecContext(ctx, `INSERT INTO accounts (id, platform, type, credentials) VALUES
 (101, 'opencode_go', 'apikey', '{"account_mode":"go","api_protocol":"adaptive","protocol_rules":[{"pattern":"grok-*","protocol":"responses"},{"pattern":"minimax-*","protocol":"anthropic"},{"pattern":"qwen*","protocol":"anthropic"}]}'),
 (102, 'opencode_go', 'apikey', '{"account_mode":"go","api_protocol":"adaptive","protocol_rules":[{"pattern":"minimax-m3.1-flash","protocol":"anthropic"},{"pattern":"minimax-*","protocol":"anthropic"}]}'),
 (103, 'opencode_go', 'apikey', '{"account_mode":"zen","api_protocol":"adaptive","protocol_rules":[{"pattern":"minimax-*","protocol":"anthropic"}]}')`)
	require.NoError(t, err)

	var openCodeID, commandCodeID int64
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT id FROM channels WHERE features_config->>'opencode_channel_billing'='true'`).Scan(&openCodeID))
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT id FROM channels WHERE features_config->>'command_code_billing'='true'`).Scan(&commandCodeID))

	// 插入自定义非原生渠道保留校验项。
	_, err = tx.ExecContext(ctx, `INSERT INTO channel_model_pricing (channel_id,platform,models,billing_mode,input_price,output_price,cache_read_price)
 VALUES (1,'opencode_go','["step-5-preview"]','token',0.5,0.75,0.00000005),
        (1,'command_code','["zai-org/GLM-5.3"]','token',0.9,0.9,0.1)`)
	require.NoError(t, err)

	migration, err := FS.ReadFile("254_sync_official_channel_catalogs_20261116.sql")
	require.NoError(t, err)
	for range 2 {
		_, err = tx.ExecContext(ctx, string(migration))
		require.NoError(t, err)
	}
	assertText := func(query, expected string, args ...any) {
		t.Helper()
		var actual string
		require.NoError(t, tx.QueryRowContext(ctx, query, args...).Scan(&actual))
		require.Equal(t, expected, actual)
	}

	assertText(`SELECT count(*)::TEXT FROM channel_model_pricing p JOIN channels c ON c.id=p.channel_id
 WHERE c.features_config->>'opencode_channel_billing'='true'`, "34")
	assertText(`SELECT count(*)::TEXT FROM channel_model_pricing p JOIN channels c ON c.id=p.channel_id
 WHERE c.features_config->>'command_code_billing'='true'`, "72")

	assertText(`SELECT input_price::TEXT || ':' || output_price::TEXT || ':' || cache_read_price::TEXT || ':' || cache_write_price::TEXT || ':' || (monthly_quota->>'base_credits_usd') FROM channel_model_pricing WHERE channel_id=$1 AND models='["qwen4-max"]'`, "0.0000015000:0.0000045000:0.000000075000:0.0000018750:15", openCodeID)
	assertText(`SELECT cache_read_price::TEXT FROM channel_model_pricing WHERE channel_id=$1 AND models='["step-5-preview"]'`, "0.000000100000", openCodeID)
	assertText(`SELECT count(*)::TEXT FROM channel_model_pricing WHERE channel_id=$1 AND models='["hy4-flash-free"]'
 AND input_price=0.000000001 AND output_price=0.000000001 AND cache_read_price=0.000000001
 AND cache_write_price=0.000000001 AND cache_write_1h_price=0.000000001
 AND image_input_price=0.000000001 AND image_output_price=0.000000001
 AND monthly_quota IS NULL AND time_pricing IS NULL`, "1", openCodeID)

	assertText(`SELECT input_price::TEXT || ':' || output_price::TEXT || ':' || cache_read_price::TEXT || ':' || cache_write_price::TEXT || ':' || (monthly_quota->>'base_credits_usd') FROM channel_model_pricing WHERE channel_id=$1 AND models='["Qwen/Qwen4-Max"]'`, "0.0000015000:0.0000045000:0.000000075000:0.0000018750:15", commandCodeID)
	assertText(`SELECT input_price::TEXT || ':' || output_price::TEXT || ':' || cache_read_price::TEXT || ':' || (monthly_quota->>'base_credits_usd') FROM channel_model_pricing WHERE channel_id=$1 AND models='["tencent/hy4-flash"]'`, "0.0000000700:0.0000002810:0.000000003000:15", commandCodeID)
	assertText(`SELECT count(*)::TEXT FROM channel_model_pricing WHERE channel_id=$1 AND models='["thinkingmachines/inkling-2-small:free"]'
 AND input_price=0.000000001 AND output_price=0.000000001 AND cache_read_price=0.000000001
 AND cache_write_price=0.000000001 AND cache_write_1h_price=0.000000001
 AND image_input_price=0.000000001 AND image_output_price=0.000000001
 AND monthly_quota IS NULL AND time_pricing IS NULL`, "1", commandCodeID)
	assertText(`SELECT monthly_quota->>'base_credits_usd' FROM channel_model_pricing WHERE channel_id=$1 AND models='["zai-org/GLM-5.3"]'`, "70", commandCodeID)

	// 验证账号 protocol_rules：101 在 minimax-* 前插入了 minimax-m3.1-flash -> chat_completions，102/103 不动。
	assertText(`SELECT jsonb_array_length(credentials->'protocol_rules')::TEXT || ':' || (credentials->'protocol_rules'->1->>'pattern') || ':' || (credentials->'protocol_rules'->1->>'protocol') FROM accounts WHERE id = 101`, "4:minimax-m3.1-flash:chat_completions")
	assertText(`SELECT jsonb_array_length(credentials->'protocol_rules')::TEXT || ':' || (credentials->'protocol_rules'->0->>'protocol') FROM accounts WHERE id = 102`, "2:anthropic")
	assertText(`SELECT jsonb_array_length(credentials->'protocol_rules')::TEXT || ':' || (credentials->'protocol_rules'->0->>'protocol') FROM accounts WHERE id = 103`, "1:anthropic")

	// 验证自定义非原生渠道保留原样。
	assertText(`SELECT cache_read_price::TEXT FROM channel_model_pricing WHERE channel_id=1 AND platform='opencode_go' AND models='["step-5-preview"]'`, "0.000000050000")
	assertText(`SELECT (monthly_quota IS NULL)::TEXT FROM channel_model_pricing WHERE channel_id=1 AND platform='command_code' AND models='["zai-org/GLM-5.3"]'`, "true")
}

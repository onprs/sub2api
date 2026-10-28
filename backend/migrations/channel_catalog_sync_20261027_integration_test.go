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

func TestOfficialChannelCatalogSync20261027Migration(t *testing.T) {
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
	schema := fmt.Sprintf("catalog_sync_20261027_test_%d", time.Now().UnixNano())
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
	} {
		body, readErr := FS.ReadFile(name)
		require.NoError(t, readErr)
		_, err = tx.ExecContext(ctx, string(body))
		require.NoError(t, err, name)
	}
	var openCodeID, commandCodeID int64
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT id FROM channels WHERE features_config->>'opencode_channel_billing'='true'`).Scan(&openCodeID))
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT id FROM channels WHERE features_config->>'command_code_billing'='true'`).Scan(&commandCodeID))
	// 原生渠道的共享价卡只移除下架模型；自定义渠道的同名价卡完全保留。
	_, err = tx.ExecContext(ctx, `INSERT INTO channel_model_pricing (channel_id,platform,models,billing_mode,input_price,output_price)
 VALUES ($1,'opencode_go','["grok-4.6","glm-5.3"]','token',0.5,0.75),
        ($1,'opencode_go','["grok-4.6"]','token',0.4,0.6),
        ($2,'command_code','["xiaomi/mimo-v2.5","gpt-5.6-luna"]','token',0.25,0.5),
        ($2,'command_code','["xiaomi/mimo-v2.5"]','token',0.2,0.4),
        (1,'opencode_go','["grok-4.6","glm-5.3"]','token',0.9,0.9),
        (1,'command_code','["xiaomi/mimo-v2.5","gpt-5.6-luna"]','token',0.9,0.9)`, openCodeID, commandCodeID)
	require.NoError(t, err)
	migration, err := FS.ReadFile("251_sync_official_channel_catalogs_20261027.sql")
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
 WHERE c.features_config->>'opencode_channel_billing'='true'`, "30")
	assertText(`SELECT count(*)::TEXT FROM channel_model_pricing p JOIN channels c ON c.id=p.channel_id
 WHERE c.features_config->>'command_code_billing'='true'`, "69")
	assertText(`SELECT count(*)::TEXT FROM channel_model_pricing WHERE models ?| ARRAY['grok-4.6','kimi-k2.6','longcat-2.5-preview-free','step-5-preview-free','cat-jam-free','minimax-m2.7','space-bunny'] AND channel_id=$1`, "0", openCodeID)
	assertText(`SELECT count(*)::TEXT FROM channel_model_pricing WHERE channel_id=$1 AND models='[]'::JSONB`, "0", openCodeID)
	assertText(`SELECT models::TEXT FROM channel_model_pricing WHERE channel_id=$1 AND models ? 'glm-5.3' AND jsonb_array_length(models)=1`, `["glm-5.3"]`, openCodeID)
	assertText(`SELECT count(*)::TEXT FROM channel_model_pricing WHERE models ?| ARRAY['inclusionai/ling-3.0-flash-sante:free','inclusionai/ling-3.1-flash:free','xiaomi/mimo-v2.5','xiaomi/mimo-v2.5-pro'] AND channel_id=$1`, "0", commandCodeID)
	assertText(`SELECT count(*)::TEXT FROM channel_model_pricing WHERE channel_id=$1 AND models='[]'::JSONB`, "0", commandCodeID)
	assertText(`SELECT monthly_quota::TEXT FROM channel_model_pricing WHERE channel_id=$1 AND models='["gpt-5.6-sol"]'`, `{"base_credits_usd": 20}`, commandCodeID)
	assertText(`SELECT monthly_quota->>'base_credits_usd' FROM channel_model_pricing WHERE channel_id=$1 AND models='["deepseek/deepseek-v4.1-flash"]'`, "80", commandCodeID)
	assertText(`SELECT (monthly_quota ? 'periods')::TEXT FROM channel_model_pricing WHERE channel_id=$1 AND models='["deepseek/deepseek-v4.1-flash"]'`, "false", commandCodeID)
	assertText(`SELECT input_price::TEXT || ':' || output_price::TEXT || ':' || cache_read_price::TEXT FROM channel_model_pricing WHERE channel_id=$1 AND models='["kimi-k3.1-pro"]'`, "0.0000020000:0.0000100000:0.000000200000", openCodeID)
	assertText(`SELECT input_price::TEXT || ':' || output_price::TEXT || ':' || cache_read_price::TEXT || ':' || cache_write_price::TEXT FROM channel_model_pricing WHERE channel_id=$1 AND models='["step-5-preview"]'`, "0.0000010000:0.0000027000:0.000000050000:0.0000000000", openCodeID)
	assertText(`SELECT monthly_quota->>'base_credits_usd' FROM channel_model_pricing WHERE channel_id=$1 AND models='["minimax-m3.1-flash"]'`, "20", openCodeID)
	assertText(`SELECT input_price::TEXT || ':' || output_price::TEXT || ':' || cache_read_price::TEXT FROM channel_model_pricing WHERE channel_id=$1 AND models='["gemini-4-argon"]'`, "0.0000020000:0.0000100000:0.000000100000", openCodeID)
	assertText(`SELECT count(*)::TEXT FROM channel_model_pricing WHERE channel_id=$1 AND models='["beam-free"]'
 AND input_price=0.000000001 AND output_price=0.000000001 AND cache_read_price=0.000000001
 AND cache_write_price=0.000000001 AND cache_write_1h_price=0.000000001
 AND image_input_price=0.000000001 AND image_output_price=0.000000001
 AND monthly_quota IS NULL AND time_pricing IS NULL`, "1", openCodeID)
	assertText(`SELECT count(*)::TEXT FROM channel_model_pricing WHERE channel_id=$1 AND models='["reflection/beam:free"]'
 AND input_price=0.000000001 AND output_price=0.000000001 AND cache_read_price=0.000000001
 AND cache_write_price=0.000000001 AND cache_write_1h_price=0.000000001
 AND image_input_price=0.000000001 AND image_output_price=0.000000001
 AND monthly_quota IS NULL AND time_pricing IS NULL`, "1", commandCodeID)
	// 自定义渠道的同名价卡和价格保持原样。
	assertText(`SELECT count(*)::TEXT FROM channel_model_pricing WHERE channel_id=1 AND platform='opencode_go' AND models='["grok-4.6","glm-5.3"]' AND input_price=0.9`, "1")
	assertText(`SELECT count(*)::TEXT FROM channel_model_pricing WHERE channel_id=1 AND platform='command_code' AND models='["xiaomi/mimo-v2.5","gpt-5.6-luna"]' AND input_price=0.9`, "1")
}

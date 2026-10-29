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

func TestOpenCodeGeminiCatalogSync20261029Migration(t *testing.T) {
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
	schema := fmt.Sprintf("gemini_catalog_sync_20261029_test_%d", time.Now().UnixNano())
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
	} {
		body, readErr := FS.ReadFile(name)
		require.NoError(t, readErr)
		_, err = tx.ExecContext(ctx, string(body))
		require.NoError(t, err, name)
	}

	var openCodeID int64
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT id FROM channels WHERE features_config->>'opencode_channel_billing'='true'`).Scan(&openCodeID))
	// 第二个原生计费渠道缺少 Gemini 与 Iceman 条目，用于验证补齐；自定义渠道不应被修改。
	_, err = tx.ExecContext(ctx, `INSERT INTO channels (id, name, features_config) VALUES
        (50, 'OpenCode Gemini missing', '{"opencode_channel_billing":true}'),
        (60, 'OpenCode custom', '{}')`)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `INSERT INTO channel_model_pricing (channel_id,platform,models,billing_mode,input_price,output_price)
 VALUES (50,'opencode_go','["glm-5.3"]','token',1,1),
        (60,'opencode_go','["gemini-4-argon","iceman-contributor-free"]','token',7,7)`)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `UPDATE channel_model_pricing SET input_price=9, output_price=9
 WHERE channel_id=$1 AND models='["gemini-4-argon"]'`, openCodeID)
	require.NoError(t, err)

	// 已存在的渠道 Gemini 条目自定义价格必须保留；Iceman 的旧区间应被清理。
	var customGeminiID int64
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT id FROM channel_model_pricing WHERE channel_id=$1 AND models='["gemini-4-argon"]'`, openCodeID).Scan(&customGeminiID))
	_, err = tx.ExecContext(ctx, `INSERT INTO channel_pricing_intervals (pricing_id,min_tokens,input_price,output_price) VALUES ($1,0,1,1)`, customGeminiID)
	require.NoError(t, err)

	migration, err := FS.ReadFile("252_sync_opencode_gemini_catalog.sql")
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
	// 两个原生渠道各补齐一条 Iceman 免费条目，重复执行不产生重复行。
	assertText(`SELECT count(*)::TEXT FROM channel_model_pricing WHERE channel_id IN ($1,50) AND models='["iceman-contributor-free"]'`, "2", openCodeID)
	assertText(`SELECT count(*)::TEXT FROM channel_model_pricing WHERE models='["iceman-contributor-free"]' AND
 input_price=0.000000001 AND output_price=0.000000001 AND cache_read_price=0.000000001 AND
 cache_write_price=0.000000001 AND image_input_price=0.000000001 AND image_output_price=0.000000001 AND
 monthly_quota IS NULL AND time_pricing IS NULL`, "2")
	assertText(`SELECT count(*)::TEXT FROM channel_pricing_intervals i JOIN channel_model_pricing p ON p.id=i.pricing_id
 WHERE p.models='["iceman-contributor-free"]'`, "0")
	// 缺失 Gemini 的原生渠道补齐官方折扣价与 30 USD 月额度。
	assertText(`SELECT round(input_price::NUMERIC*1000000,2)::TEXT FROM channel_model_pricing WHERE channel_id=50 AND models='["gemini-4-argon"]'`, "2.00")
	assertText(`SELECT round(output_price::NUMERIC*1000000,2)::TEXT FROM channel_model_pricing WHERE channel_id=50 AND models='["gemini-4-argon"]'`, "10.00")
	assertText(`SELECT round(cache_read_price::NUMERIC*1000000,2)::TEXT FROM channel_model_pricing WHERE channel_id=50 AND models='["gemini-4-argon"]'`, "0.10")
	assertText(`SELECT monthly_quota->>'base_credits_usd' FROM channel_model_pricing WHERE channel_id=50 AND models='["gemini-4-argon"]'`, "30")
	// 管理员自定义价格和自定义渠道保持原值。
	assertText(`SELECT (input_price::FLOAT)::TEXT FROM channel_model_pricing WHERE channel_id=$1 AND models='["gemini-4-argon"]'`, "9", openCodeID)
	assertText(`SELECT count(*)::TEXT FROM channel_model_pricing WHERE channel_id=60`, "1")
	assertText(`SELECT (input_price::FLOAT)::TEXT FROM channel_model_pricing WHERE channel_id=60 AND models='["gemini-4-argon","iceman-contributor-free"]'`, "7")
}

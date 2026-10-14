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

func TestOfficialChannelCatalogSyncMigration(t *testing.T) {
	dsn := os.Getenv("SUB2API_MIGRATION_TEST_DSN")
	if dsn == "" {
		t.Skip("需要独立 PostgreSQL 测试库")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	schema := fmt.Sprintf("catalog_sync_test_%d", time.Now().UnixNano())
	_, err = tx.ExecContext(ctx, fmt.Sprintf("CREATE SCHEMA %q; SET LOCAL search_path = %q;", schema, schema))
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, openCodeBillingMigrationFixture)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, "ALTER TABLE channel_model_pricing ADD COLUMN updated_at TIMESTAMPTZ DEFAULT NOW()")
	require.NoError(t, err)
	for _, name := range []string{"245_command_code_channel_billing.sql", "246_opencode_channel_billing.sql"} {
		body, readErr := FS.ReadFile(name)
		require.NoError(t, readErr)
		_, err = tx.ExecContext(ctx, string(body))
		require.NoError(t, err)
	}
	// 无计费标记的自定义渠道应保留同名模型的独立价格与额度。
	_, err = tx.ExecContext(ctx, `INSERT INTO channel_model_pricing (channel_id,platform,models,monthly_quota,input_price)
 VALUES (1,'command_code','["deepseek/deepseek-v4.1-flash"]','{"base_credits_usd":7}',0.123)`)
	require.NoError(t, err)
	body, err := FS.ReadFile("247_sync_official_channel_catalogs.sql")
	require.NoError(t, err)
	for range 2 {
		_, err = tx.ExecContext(ctx, string(body))
		require.NoError(t, err)
	}
	assertText := func(query, expected string) {
		t.Helper()
		var actual string
		require.NoError(t, tx.QueryRowContext(ctx, query).Scan(&actual))
		require.Equal(t, expected, actual)
	}
	assertText(`SELECT count(*)::TEXT FROM channel_model_pricing p JOIN channels c ON c.id=p.channel_id WHERE c.features_config->>'opencode_channel_billing'='true'`, "33")
	assertText(`SELECT count(*)::TEXT FROM channel_model_pricing p JOIN channels c ON c.id=p.channel_id WHERE c.features_config->>'command_code_billing'='true'`, "64")
	assertText(`SELECT count(*)::TEXT FROM channel_model_pricing WHERE models='["cat-jam-free"]' AND input_price=0 AND output_price=0 AND cache_read_price=0 AND monthly_quota IS NULL`, "1")
	assertText(`SELECT count(*)::TEXT FROM channel_model_pricing WHERE models ?| ARRAY['omen-alpha','deepseek-flash','glm-5.1','minimax-m2.5','qwen3.6-plus','qwen3.7-max','typesafe/jev','stealth/pixel-canary']`, "0")
	assertText(`SELECT count(*)::TEXT FROM channel_pricing_intervals`, "33")
	assertText(`SELECT monthly_quota->>'base_credits_usd' FROM channel_model_pricing WHERE channel_id=1`, "7")
	for _, model := range []string{"deepseek/deepseek-v4.1-flash", "z-ai/glm-5.3-flash"} {
		var base, promotional float64
		err = tx.QueryRowContext(ctx, `SELECT (monthly_quota->>'base_credits_usd')::FLOAT,
    (monthly_quota->'periods'->-1->>'credits_usd')::FLOAT
    FROM channel_model_pricing p JOIN channels c ON c.id=p.channel_id
    WHERE c.features_config->>'command_code_billing'='true' AND p.models=jsonb_build_array($1::TEXT)`, model).Scan(&base, &promotional)
		require.NoError(t, err)
		require.Equal(t, 80.0, base)
		require.Equal(t, 100.0, promotional)
	}
}

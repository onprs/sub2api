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

func TestOfficialCommandCodeCatalogSync20261117Migration(t *testing.T) {
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
	schema := fmt.Sprintf("catalog_sync_20261117_test_%d", time.Now().UnixNano())
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
 ADD COLUMN reasoning_effort_multipliers JSONB DEFAULT '{}';`)
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
		"254_sync_official_channel_catalogs_20261116.sql",
	} {
		body, readErr := FS.ReadFile(name)
		require.NoError(t, readErr)
		_, err = tx.ExecContext(ctx, string(body))
		require.NoError(t, err, name)
	}

	var commandCodeID int64
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT id FROM channels WHERE features_config->>'command_code_billing'='true'`).Scan(&commandCodeID))

	migration, err := FS.ReadFile("255_sync_official_commandcode_catalog_20261117.sql")
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
 WHERE c.features_config->>'command_code_billing'='true'`, "74")
	assertText(`SELECT input_price::TEXT || ':' || output_price::TEXT || ':' || cache_read_price::TEXT || ':' || (monthly_quota->>'base_credits_usd') FROM channel_model_pricing WHERE channel_id=$1 AND models='["tencent/hy4"]'`, "0.0000008340:0.0000025010:0.000000042000:20", commandCodeID)
	assertText(`SELECT count(*)::TEXT FROM channel_model_pricing WHERE channel_id=$1 AND models='["recursive/flint-v1.0:free"]'
 AND input_price=0.000000001 AND output_price=0.000000001 AND cache_read_price=0.000000001
 AND cache_write_price=0.000000001 AND cache_write_1h_price=0.000000001
 AND image_input_price=0.000000001 AND image_output_price=0.000000001
 AND monthly_quota IS NULL AND time_pricing IS NULL`, "1", commandCodeID)
	assertText(`SELECT count(*)::TEXT FROM channel_pricing_intervals intervals
 JOIN channel_model_pricing pricing ON pricing.id=intervals.pricing_id
 WHERE pricing.channel_id=$1 AND pricing.models='["recursive/flint-v1.0:free"]'`, "0", commandCodeID)
}

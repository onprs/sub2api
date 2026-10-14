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

func TestOpenCodePricedCatalogRefreshMigration(t *testing.T) {
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
	schema := fmt.Sprintf("opencode_refresh_test_%d", time.Now().UnixNano())
	_, err = tx.ExecContext(ctx, `CREATE SCHEMA "`+schema+`"; SET LOCAL search_path = "`+schema+`";`)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, openCodeBillingMigrationFixture+`
ALTER TABLE channel_model_pricing
 ADD COLUMN updated_at TIMESTAMPTZ DEFAULT NOW(),
 ADD COLUMN cache_write_1h_price NUMERIC(20,10),
 ADD COLUMN image_input_price NUMERIC(20,12),
 ADD COLUMN image_output_price NUMERIC(20,12),
 ADD COLUMN fast_multiplier DOUBLE PRECISION,
 ADD COLUMN flex_multiplier DOUBLE PRECISION,
 ADD COLUMN max_reasoning_effort_multiplier DOUBLE PRECISION,
 ADD COLUMN reasoning_effort_multipliers JSONB DEFAULT '{}';
ALTER TABLE channel_pricing_intervals ADD COLUMN cache_write_1h_price NUMERIC(20,10);
CREATE TABLE usage_logs (id BIGINT PRIMARY KEY, model TEXT, actual_cost NUMERIC(20,12));
INSERT INTO usage_logs VALUES (1,'deepseek-v4-flash',1.234567);
`)
	require.NoError(t, err)
	for _, name := range []string{"245_command_code_channel_billing.sql", "246_opencode_channel_billing.sql", "247_sync_official_channel_catalogs.sql", "248_free_model_billing_price.sql"} {
		body, readErr := FS.ReadFile(name)
		require.NoError(t, readErr)
		_, err = tx.ExecContext(ctx, string(body))
		require.NoError(t, err, name)
	}
	var nativeID int64
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT id FROM channels WHERE features_config->>'opencode_channel_billing'='true'`).Scan(&nativeID))
	var mixedID, pureID int64
	require.NoError(t, tx.QueryRowContext(ctx, `
INSERT INTO channel_model_pricing (channel_id,platform,models,billing_mode,input_price,output_price,cache_write_price,cache_read_price,monthly_quota,time_pricing,fast_multiplier)
VALUES ($1,'opencode_go','["deepseek-v4-flash","glm-5.3","deepseek-v4-flash-vision-exp","glm-5.2"]','token',0.000003,0.000007,0.000004,0.000002,'{"base_credits_usd":17}','{"timezone":"UTC","periods":[{"start_time":"01:00:00","end_time":"02:00:00","multiplier":2}]}',1.5)
RETURNING id`, nativeID).Scan(&mixedID))
	require.NoError(t, tx.QueryRowContext(ctx, `
INSERT INTO channel_model_pricing (channel_id,platform,models,billing_mode,input_price,output_price)
VALUES ($1,'opencode_go','["deepseek-v4-flash","deepseek-v4-flash-vision-exp"]','token',0.000009,0.000019) RETURNING id`, nativeID).Scan(&pureID))
	_, err = tx.ExecContext(ctx, `
INSERT INTO channel_pricing_intervals (pricing_id,min_tokens,input_price,output_price,sort_order) VALUES ($1,0,0.000004,0.000009,0),($2,0,0.000007,0.000017,0);
`, mixedID, pureID)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `
INSERT INTO channel_model_pricing (channel_id,platform,models,billing_mode,input_price,output_price)
VALUES (1,'opencode_go','["deepseek-v4-flash"]','token',0.000011,0.000021);
`)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `INSERT INTO channel_model_pricing (channel_id,platform,models,billing_mode,input_price,output_price) VALUES ($1,'command_code','["deepseek-v4-flash"]','token',0.000013,0.000023)`, nativeID)
	require.NoError(t, err)
	queryText := func(query string, args ...any) string {
		t.Helper()
		var value string
		require.NoError(t, tx.QueryRowContext(ctx, query, args...).Scan(&value))
		return value
	}
	protectedQuery := `SELECT jsonb_agg(to_jsonb(p) ORDER BY p.id)::TEXT FROM channel_model_pricing p JOIN channels c ON c.id=p.channel_id
WHERE NOT (COALESCE(c.features_config->>'opencode_channel_billing','false')='true' AND p.platform='opencode_go' AND p.models ?| ARRAY['deepseek-v4-flash','deepseek-v4-flash-vision-exp'])`
	protected := queryText(protectedQuery)
	mixedBefore := queryText(`SELECT (to_jsonb(p)-'models'-'updated_at')::TEXT FROM channel_model_pricing p WHERE id=$1`, mixedID)
	intervalBefore := queryText(`SELECT to_jsonb(i)::TEXT FROM channel_pricing_intervals i WHERE pricing_id=$1`, mixedID)
	historyBefore := queryText(`SELECT jsonb_agg(to_jsonb(u))::TEXT FROM usage_logs u`)
	body, err := FS.ReadFile("249_refresh_opencode_priced_catalog.sql")
	require.NoError(t, err)
	for range 2 {
		_, err = tx.ExecContext(ctx, string(body))
		require.NoError(t, err, "重复执行不得扩大删除范围")
		require.Equal(t, "0", queryText(`SELECT count(*)::TEXT FROM channel_model_pricing WHERE channel_id=$1 AND platform='opencode_go' AND models ?| ARRAY['deepseek-v4-flash','deepseek-v4-flash-vision-exp']`, nativeID))
		require.Equal(t, "0", queryText(`SELECT count(*)::TEXT FROM channel_pricing_intervals WHERE pricing_id=$1`, pureID))
		require.Equal(t, `["glm-5.3", "glm-5.2"]`, queryText(`SELECT models::TEXT FROM channel_model_pricing WHERE id=$1`, mixedID))
		require.Equal(t, mixedBefore, queryText(`SELECT (to_jsonb(p)-'models'-'updated_at')::TEXT FROM channel_model_pricing p WHERE id=$1`, mixedID))
		require.Equal(t, intervalBefore, queryText(`SELECT to_jsonb(i)::TEXT FROM channel_pricing_intervals i WHERE pricing_id=$1`, mixedID))
		require.Equal(t, historyBefore, queryText(`SELECT jsonb_agg(to_jsonb(u))::TEXT FROM usage_logs u`))
		// 混合卡只改变模型范围，从保护快照中排除该卡后其余配置须完整一致。
		require.Equal(t, protected, queryText(protectedQuery+` AND p.id<>$1`, mixedID))
		require.Equal(t, "31", queryText(`SELECT count(*)::TEXT FROM channel_model_pricing WHERE channel_id=$1 AND platform='opencode_go' AND jsonb_array_length(models)=1`, nativeID))
		require.Equal(t, "3", queryText(`SELECT count(*)::TEXT FROM channel_model_pricing WHERE channel_id=$1 AND platform='opencode_go' AND input_price=0.000000001 AND output_price=0.000000001 AND cache_read_price=0.000000001 AND cache_write_price=0.000000001 AND cache_write_1h_price=0.000000001 AND monthly_quota IS NULL`, nativeID))
	}
}

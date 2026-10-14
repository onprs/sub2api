//go:build integration

package migrations

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestFreeModelBillingPriceMigration(t *testing.T) {
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
	schema := fmt.Sprintf("free_model_billing_test_%d", time.Now().UnixNano())
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
 ADD COLUMN reasoning_effort_multipliers JSONB DEFAULT '{}'`)
	require.NoError(t, err)
	for _, name := range []string{"245_command_code_channel_billing.sql", "246_opencode_channel_billing.sql", "247_sync_official_channel_catalogs.sql"} {
		body, readErr := FS.ReadFile(name)
		require.NoError(t, readErr)
		_, err = tx.ExecContext(ctx, string(body))
		require.NoError(t, err)
	}
	migration, err := FS.ReadFile("248_free_model_billing_price.sql")
	require.NoError(t, err)
	parts := strings.Split(string(migration), "$free_models$")
	require.Len(t, parts, 3)
	var models map[string][]string
	require.NoError(t, json.Unmarshal([]byte(parts[1]), &models))
	// Zen 独立渠道已有的六个免费模型也应同步，不新增任何模型条目。
	var zenChannelID int64
	err = tx.QueryRowContext(ctx, `INSERT INTO channels (name,features_config) VALUES ('OpenCode Zen','{"opencode_channel_billing":true}') RETURNING id`).Scan(&zenChannelID)
	require.NoError(t, err)
	goFree := map[string]bool{"cat-jam-free": true, "longcat-2.5-preview-free": true, "step-5-preview-free": true}
	for _, model := range models["opencode_go"] {
		if goFree[model] {
			continue
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO channel_model_pricing (channel_id,platform,models,billing_mode,input_price,output_price,cache_write_price,cache_read_price,monthly_quota)
   VALUES ($1,'opencode_go',jsonb_build_array($2::TEXT),'token',0,0,0,0,'{"base_credits_usd":0}')`, zenChannelID, model)
		require.NoError(t, err)
	}
	// 无原生计费标记的自定义渠道保留自己的免费模型售价。
	_, err = tx.ExecContext(ctx, `INSERT INTO channel_model_pricing (channel_id,platform,models,billing_mode,input_price,output_price)
  VALUES (1,'command_code','["stealth/glyph-cluster:free"]','token',0.25,0.5)`)
	require.NoError(t, err)
	// 混合免费与付费模型的共享价卡不能被整体改成固定免费模型价。
	_, err = tx.ExecContext(ctx, `INSERT INTO channel_model_pricing (channel_id,platform,models,billing_mode,input_price,output_price)
  VALUES ($1,'opencode_go','["cat-jam-free","glm-5.3"]','token',0.5,0.75)`, zenChannelID)
	require.NoError(t, err)
	// 免费模型旧缓存档位与月额度必须清除，避免额外倍率或零额度拒绝扣费。
	_, err = tx.ExecContext(ctx, `INSERT INTO channel_pricing_intervals (pricing_id,min_tokens,input_price,output_price)
  SELECT p.id,0,0,0 FROM channel_model_pricing p JOIN channels c ON c.id=p.channel_id
  WHERE c.features_config->>'command_code_billing'='true' AND p.models='["stealth/glyph-cluster:free"]'`)
	require.NoError(t, err)
	target := `COALESCE(((p.platform='opencode_go' AND c.features_config->>'opencode_channel_billing'='true')
  OR (p.platform='command_code' AND c.features_config->>'command_code_billing'='true'))
  AND p.models <@ ($1::JSONB->p.platform),false)`
	snapshot := func(nonTarget bool) string {
		t.Helper()
		query := `SELECT COALESCE(jsonb_agg(to_jsonb(p)-'updated_at' ORDER BY p.id),'[]'::JSONB)::TEXT FROM channel_model_pricing p JOIN channels c ON c.id=p.channel_id`
		if nonTarget {
			query += " WHERE NOT (" + target + ")"
		}
		var value string
		var queryErr error
		if nonTarget {
			queryErr = tx.QueryRowContext(ctx, query, parts[1]).Scan(&value)
		} else {
			queryErr = tx.QueryRowContext(ctx, query).Scan(&value)
		}
		require.NoError(t, queryErr)
		return value
	}
	unchanged := snapshot(true)
	var beforeCount int
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT count(*) FROM channel_model_pricing`).Scan(&beforeCount))
	_, err = tx.ExecContext(ctx, string(migration))
	require.NoError(t, err)
	require.JSONEq(t, unchanged, snapshot(true))
	after := snapshot(false)
	_, err = tx.ExecContext(ctx, string(migration))
	require.NoError(t, err)
	require.JSONEq(t, after, snapshot(false))
	var afterCount, freeCount, intervalCount int
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT count(*) FROM channel_model_pricing`).Scan(&afterCount))
	require.Equal(t, beforeCount, afterCount)
	query := `SELECT count(*) FROM channel_model_pricing p JOIN channels c ON c.id=p.channel_id WHERE ` + target + `
  AND p.input_price=0.000000001 AND p.output_price=0.000000001
  AND p.cache_read_price=0.000000001 AND p.cache_write_price=0.000000001
  AND p.cache_write_1h_price=0.000000001 AND p.image_input_price=0.000000001 AND p.image_output_price=0.000000001
  AND p.monthly_quota IS NULL AND p.time_pricing IS NULL
  AND p.fast_multiplier=1 AND p.flex_multiplier=1 AND p.max_reasoning_effort_multiplier=1
  AND p.reasoning_effort_multipliers='{}'::JSONB`
	require.NoError(t, tx.QueryRowContext(ctx, query, parts[1]).Scan(&freeCount))
	require.Equal(t, 13, freeCount)
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT count(*) FROM channel_pricing_intervals`).Scan(&intervalCount))
	require.Equal(t, 33, intervalCount)
}

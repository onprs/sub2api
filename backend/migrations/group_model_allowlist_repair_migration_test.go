package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGroupModelAllowlistRepairMigration(t *testing.T) {
	content, err := FS.ReadFile("236_group_model_allowlist_repair.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")

	// 三种残留状态都要收敛到 model_allowlist。
	require.Contains(t, sql, "ALTER TABLE groups RENAME COLUMN models_list_config TO model_allowlist")
	require.Contains(t, sql, "ADD COLUMN IF NOT EXISTS model_allowlist JSONB NOT NULL DEFAULT '{}'::jsonb")
	require.Contains(t, sql, "SET model_allowlist = models_list_config")
	require.Contains(t, sql, "ALTER TABLE groups ALTER COLUMN model_allowlist SET NOT NULL")
	require.Contains(t, sql, "COMMENT ON COLUMN groups.model_allowlist")

	// 235 用 table_schema = 'public' 判定列是否存在，而 ALTER TABLE 走的是 search_path；
	// 修复迁移必须用 regclass 解析，两者才不会在非 public schema 上分叉。
	require.NotContains(t, sql, "table_schema = 'public'")
	require.Contains(t, sql, "attrelid = 'groups'::regclass")
}

func TestGroupAuthCacheModelAllowlistMigration(t *testing.T) {
	content, err := FS.ReadFile("238b_fix_group_auth_cache_model_allowlist.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql, "CREATE OR REPLACE FUNCTION enqueue_group_auth_cache_invalidation()")
	require.Contains(t, sql, "OLD.model_allowlist IS NOT DISTINCT FROM NEW.model_allowlist")
	require.Contains(t, sql, "FROM api_key_groups AS akg")
	require.NotContains(t, sql, "OLD.models_list_config")
	require.NotContains(t, sql, "NEW.models_list_config")
}

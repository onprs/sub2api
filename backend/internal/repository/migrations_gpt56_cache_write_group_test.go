package repository

import (
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
)

func TestGPT56CacheWriteInferenceMovesFromAccountsToGroups(t *testing.T) {
	content, err := migrations.FS.ReadFile("187_move_gpt56_cache_write_inference_to_groups.sql")
	require.NoError(t, err)

	sql := strings.ToLower(string(content))
	require.Contains(t, sql, "add column if not exists infer_gpt56_cache_write boolean not null default false")
	require.Contains(t, sql, "add column if not exists infer_gpt56_cache_write_min_tokens integer not null default 1024")
	require.Contains(t, sql, "from account_groups as ag")
	require.Contains(t, sql, "min(")
	require.Contains(t, sql, "a.platform = 'openai'")
	require.Contains(t, sql, "g.platform = 'openai'")
	require.Contains(t, sql, "a.extra->>'infer_gpt56_cache_write'")
	require.Contains(t, sql, "set extra = (coalesce(extra, '{}'::jsonb) - 'infer_gpt56_cache_write') - 'infer_gpt56_cache_write_min_tokens'")

	backfillAt := strings.Index(sql, "update groups as g")
	cleanupAt := strings.Index(sql, "update accounts")
	require.GreaterOrEqual(t, backfillAt, 0)
	require.Greater(t, cleanupAt, backfillAt, "账号旧键只能在分组回填完成后清理")
}

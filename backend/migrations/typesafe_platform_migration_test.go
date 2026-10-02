package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTypeSafePlatformMigration(t *testing.T) {
	content, err := FS.ReadFile("241_add_typesafe_platform.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql, "DROP CONSTRAINT IF EXISTS user_platform_quotas_platform_check")
	require.Contains(t, sql, "DROP CONSTRAINT IF EXISTS composite_model_routes_target_platform_check")
	require.Contains(t, sql,
		"CHECK (platform IN ('anthropic', 'openai', 'gemini', 'antigravity', 'grok', 'kimi', 'zhipu', 'deepseek', 'minimax', 'opencode', 'clinepass', 'openrouter', 'commandcode', 'opencode_go', 'typesafe'))")
	require.Contains(t, sql,
		"CHECK (target_platform IN ('anthropic', 'openai', 'gemini', 'antigravity', 'grok', 'kimi', 'zhipu', 'deepseek', 'minimax', 'opencode', 'opencode_go', 'typesafe'))")
}

func TestTypeSafeCanonicalConstraintsPreserveCustomPlatforms(t *testing.T) {
	content, err := FS.ReadFile("242_add_typesafe_canonical_constraints.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	quotaConstraint, compositeConstraint, ok := strings.Cut(sql, "ALTER TABLE composite_model_routes")
	require.True(t, ok)
	for _, platform := range []string{"opencode", "clinepass", "openrouter", "commandcode", "typesafe"} {
		require.Contains(t, quotaConstraint, "'"+platform+"'")
	}
	for _, platform := range []string{"opencode", "typesafe"} {
		require.Contains(t, compositeConstraint, "'"+platform+"'")
	}
	require.NotContains(t, sql, "'opencode_go'")
}

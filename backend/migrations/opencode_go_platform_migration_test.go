package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

var openCodeQuotaPlatforms = []string{
	"anthropic", "openai", "opencode_go", "clinepass", "openrouter", "commandcode",
	"gemini", "antigravity", "grok", "kimi", "zhipu", "deepseek", "minimax",
}

var openCodeCompositePlatforms = []string{
	"anthropic", "openai", "gemini", "antigravity", "grok", "kimi", "zhipu", "deepseek", "minimax", "opencode_go",
}

var openCodeMonitorProviders = []string{
	"openai", "anthropic", "gemini", "grok", "opencode_go", "clinepass", "openrouter", "commandcode",
	"antigravity", "antigravity_claude", "antigravity_gemini", "kimi", "zhipu", "deepseek", "minimax",
}

func migrationCheckList(t *testing.T, sql, marker string) string {
	t.Helper()
	start := strings.Index(sql, marker)
	require.GreaterOrEqual(t, start, 0, "missing check marker %s", marker)
	rest := sql[start:]
	end := strings.Index(rest, "))")
	require.Greater(t, end, 0, "unterminated check marker %s", marker)
	return rest[:end]
}

func assertOpenCodePlatformContracts(t *testing.T, content []byte) {
	t.Helper()
	sql := strings.Join(strings.Fields(string(content)), " ")
	quota := migrationCheckList(t, sql, "CHECK (platform IN (")
	target := migrationCheckList(t, sql, "CHECK (target_platform IN (")
	provider := migrationCheckList(t, sql, "CHECK (provider IN (")

	for _, platform := range openCodeQuotaPlatforms {
		require.Contains(t, quota, "'"+platform+"'")
	}
	for _, platform := range openCodeCompositePlatforms {
		require.Contains(t, target, "'"+platform+"'")
	}
	for _, providerName := range openCodeMonitorProviders {
		require.Contains(t, provider, "'"+providerName+"'")
	}
}

func TestOpenCodeGoPlatformMigration(t *testing.T) {
	content, err := FS.ReadFile("238_opencode_go_platform.sql")
	require.NoError(t, err)
	assertOpenCodePlatformContracts(t, content)
}

func TestOpenCodeGoPlatformConstraintRepairMigration(t *testing.T) {
	content, err := FS.ReadFile("239_restore_custom_platform_constraints.sql")
	require.NoError(t, err)
	assertOpenCodePlatformContracts(t, content)
}

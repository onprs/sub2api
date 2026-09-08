package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMiniMaxPlatformMigration(t *testing.T) {
	content, err := FS.ReadFile("237_add_minimax_platform.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql,
		"CHECK (platform IN ( 'anthropic', 'openai', 'opencode_go', 'clinepass', 'openrouter', 'commandcode', 'gemini', 'antigravity', 'grok', 'kimi', 'zhipu', 'deepseek', 'minimax' ))")
	require.Contains(t, sql,
		"CHECK (target_platform IN ('anthropic', 'openai', 'gemini', 'antigravity', 'grok', 'kimi', 'zhipu', 'deepseek', 'minimax'))")
	require.Contains(t, sql,
		"CHECK (provider IN ('openai', 'anthropic', 'gemini', 'grok', 'opencode_go', 'clinepass', 'openrouter', 'commandcode', 'antigravity', 'antigravity_claude', 'antigravity_gemini', 'kimi', 'zhipu', 'deepseek', 'minimax'))")
	require.Contains(t, sql, "c.conrelid = 'channel_monitors'::regclass")
	require.Contains(t, sql, "c.conrelid = 'channel_monitor_request_templates'::regclass")
}

func TestMiniMaxPlatformConstraintRepairMigration(t *testing.T) {
	content, err := FS.ReadFile("238_align_minimax_platform_constraints.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql,
		"CHECK (platform IN ( 'anthropic', 'openai', 'opencode_go', 'clinepass', 'openrouter', 'commandcode', 'gemini', 'antigravity', 'grok', 'kimi', 'zhipu', 'deepseek', 'minimax' ))")
	require.Contains(t, sql,
		"CHECK (target_platform IN ('anthropic', 'openai', 'gemini', 'antigravity', 'grok', 'kimi', 'zhipu', 'deepseek', 'minimax'))")
	require.Contains(t, sql,
		"CHECK (provider IN ('openai', 'anthropic', 'gemini', 'grok', 'opencode_go', 'clinepass', 'openrouter', 'commandcode', 'antigravity', 'antigravity_claude', 'antigravity_gemini', 'kimi', 'zhipu', 'deepseek', 'minimax'))")
}

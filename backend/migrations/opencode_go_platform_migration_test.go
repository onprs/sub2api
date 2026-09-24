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

func TestUnifiedOpenCodePlatformMigrationRenamesPersistedIdentifiersAndModes(t *testing.T) {
	content, err := FS.ReadFile("241_unify_opencode_platform.sql")
	require.NoError(t, err)
	sql := string(content)

	normalizedSQL := strings.Join(strings.Fields(sql), " ")
	updateRoutes := strings.Index(normalizedSQL, "UPDATE composite_model_routes SET target_platform = 'opencode'")
	dropRoutesCheck := strings.Index(normalizedSQL, "ALTER TABLE composite_model_routes DROP CONSTRAINT IF EXISTS composite_model_routes_target_platform_check")
	addRoutesCheck := strings.Index(normalizedSQL, "ALTER TABLE composite_model_routes ADD CONSTRAINT composite_model_routes_target_platform_check")
	require.GreaterOrEqual(t, dropRoutesCheck, 0)
	require.Greater(t, updateRoutes, dropRoutesCheck, "drop the legacy check before writing the canonical platform")
	require.Greater(t, addRoutesCheck, updateRoutes, "restore the canonical check after rewriting route values")

	quotaContract := migrationCheckList(t, normalizedSQL, "CHECK (platform IN (")
	routeContract := migrationCheckList(t, normalizedSQL, "CHECK (target_platform IN (")
	providerContract := migrationCheckList(t, normalizedSQL, "CHECK (provider IN (")
	for _, contract := range []string{quotaContract, routeContract, providerContract} {
		require.Contains(t, contract, "'opencode'")
		require.NotContains(t, contract, "'opencode_go'")
	}

	for _, fragment := range []string{
		"WHERE platform = 'opencode_go'",
		"'{account_mode}', '\"go\"'::jsonb",
		"'{account_mode}', '\"zen\"'::jsonb",
		"UPDATE groups",
		"UPDATE api_keys",
		"UPDATE channel_model_pricing",
		"UPDATE channel_account_stats_model_pricing",
		"UPDATE channels AS channel_row",
		"UPDATE error_passthrough_rules AS rule",
		"channel_monitor_v2_config",
		"channel_monitor_v2_metrics_1m",
		"channel_monitor_v2_user_metrics_1m",
		"channel_monitor_v2_error_metrics_1m",
		"channel_monitor_v2_latency_histograms_1m",
		"channel_monitor_v2_metrics_rollup",
		"channel_monitor_v2_user_metrics_rollup",
		"channel_monitor_v2_error_metrics_rollup",
		"channel_monitor_v2_latency_histograms_rollup",
		"UPDATE ops_error_logs SET platform = 'opencode'",
		"UPDATE ops_system_metrics SET platform = 'opencode'",
		"UPDATE ops_system_logs SET platform = 'opencode'",
		"UPDATE ops_alert_silences SET platform = 'opencode'",
		"UPDATE ops_alert_rules",
		"UPDATE ops_alert_events",
		"UPDATE ops_metrics_hourly AS canonical",
		"UPDATE ops_metrics_daily AS canonical",
		"ON CONFLICT (bucket_start, platform, group_id, model) DO UPDATE SET",
		"UPDATE composite_model_routes",
		"UPDATE channel_monitors",
		"UPDATE channel_monitor_request_templates",
		"provider IN ('opencode', 'opencode_go')",
		"candidate_name := LEFT(template_row.name",
		"attempt := attempt + 1",
		"UPDATE user_platform_quotas",
		"default_platform_quotas",
		"opencode_go_5h_used_percent",
		"opencode_5h_used_percent",
	} {
		require.Contains(t, normalizedSQL, fragment)
	}
}

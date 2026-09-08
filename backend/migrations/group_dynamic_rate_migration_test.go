//go:build unit

package migrations

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMigration236AddsDynamicGroupRateAndUsageLedger(t *testing.T) {
	content, err := FS.ReadFile("236_group_dynamic_rate.sql")
	require.NoError(t, err)

	sql := string(content)
	require.Contains(t, sql, "ADD COLUMN IF NOT EXISTS dynamic_rate_enabled")
	require.Contains(t, sql, "ADD COLUMN IF NOT EXISTS dynamic_rate_max_multiplier")
	require.Contains(t, sql, "ADD COLUMN IF NOT EXISTS dynamic_rate_min_multiplier")
	require.Contains(t, sql, "ADD COLUMN IF NOT EXISTS dynamic_rate_target_tokens")
	require.Contains(t, sql, "ADD COLUMN IF NOT EXISTS dynamic_rate_window_minutes")
	require.Contains(t, sql, "groups_dynamic_rate_bounds_valid")
	require.Contains(t, sql, "dynamic_rate_window_minutes BETWEEN 1 AND 43200")
	require.Contains(t, sql, "CREATE TABLE IF NOT EXISTS dynamic_rate_usage_metadata")
	require.Contains(t, sql, "ledger_started_at TIMESTAMPTZ NOT NULL")
	require.Contains(t, sql, "ON CONFLICT (singleton) DO NOTHING")
	require.Contains(t, sql, "CREATE TABLE IF NOT EXISTS dynamic_rate_usage_events")
	require.Contains(t, sql, "UNIQUE (request_id, api_key_id)")
	require.Contains(t, sql, "idx_dynamic_rate_usage_user_group_time")
	require.Contains(t, sql, "idx_dynamic_rate_usage_occurred_at_brin")
}

func TestMigration237SnapshotsResolvedDynamicRateOnUsageLogs(t *testing.T) {
	content, err := FS.ReadFile("237_usage_log_dynamic_rate_multiplier.sql")
	require.NoError(t, err)

	sql := string(content)
	require.Contains(t, sql, "ADD COLUMN IF NOT EXISTS dynamic_rate_multiplier DECIMAL(10,4)")
	require.Contains(t, sql, "SET dynamic_rate_multiplier = event.resolved_multiplier")
	require.Contains(t, sql, "log.request_id = event.request_id")
	require.Contains(t, sql, "log.api_key_id = event.api_key_id")
	require.Contains(t, sql, "log.dynamic_rate_multiplier IS NULL")
}

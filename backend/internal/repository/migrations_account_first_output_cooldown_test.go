package repository

import (
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
)

func TestAccountFirstOutputFailoverCooldownMigrationKeepsAccountScope(t *testing.T) {
	content, err := migrations.FS.ReadFile("185_add_account_first_output_failover_cooldown.sql")
	require.NoError(t, err)

	sql := strings.ToLower(string(content))
	require.Contains(t, sql, "add column if not exists first_output_failover_cooldown_minutes integer")
	require.Contains(t, sql, "first_output_failover_cooldown_minutes > 0")
	require.Contains(t, sql, "first_output_failover_cooldown_minutes <= 10080")
	require.Contains(t, sql, "first_output_failover_timeout_seconds is not null")
	require.Contains(t, sql, "platform = 'openai'")
	require.Contains(t, sql, "type = 'apikey'")
}

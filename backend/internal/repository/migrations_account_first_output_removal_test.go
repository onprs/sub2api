package repository

import (
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
)

func TestAccountFirstOutputFailoverRemovalMigrationDropsCustomFields(t *testing.T) {
	content, err := migrations.FS.ReadFile("186_remove_account_first_output_failover.sql")
	require.NoError(t, err)

	sql := strings.ToLower(string(content))
	require.Contains(t, sql, "drop constraint if exists accounts_first_output_failover_cooldown_valid")
	require.Contains(t, sql, "drop constraint if exists accounts_first_output_failover_timeout_valid")
	require.Contains(t, sql, "drop column if exists first_output_failover_cooldown_minutes")
	require.Contains(t, sql, "drop column if exists first_output_failover_timeout_seconds")
}

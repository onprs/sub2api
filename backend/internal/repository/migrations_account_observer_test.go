package repository

import (
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
)

func TestAccountObserverMigrationSecurityContract(t *testing.T) {
	content, err := migrations.FS.ReadFile("176_account_observer.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))

	require.Contains(t, sql, "account_observer:read")
	require.Contains(t, sql, "token_hash")
	require.Contains(t, sql, "allowed_cidrs")
	require.Contains(t, sql, "revoked_at")
	require.NotContains(t, sql, "plaintext_token")
	require.NotContains(t, sql, "credentials")
}

func TestAccountObserverDeleteScopeMigration(t *testing.T) {
	content, err := migrations.FS.ReadFile("183_account_observer_delete_scope.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))

	require.Contains(t, sql, "account_observer:read account_observer:delete")
	require.Contains(t, sql, "account_observer_tokens_scope_check")
	require.NotContains(t, sql, "admin")
}

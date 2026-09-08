//go:build integration

package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	dbmigrations "github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
)

func TestMigration237BackfillsResolvedDynamicRateByRequestKey(t *testing.T) {
	ctx := context.Background()
	user := mustCreateUser(t, integrationEntClient, &service.User{})
	group := mustCreateGroup(t, integrationEntClient, &service.Group{
		Name:           fmt.Sprintf("dynamic-snapshot-%d", time.Now().UnixNano()),
		RateMultiplier: 1,
	})
	groupID := group.ID
	apiKey := mustCreateApiKey(t, integrationEntClient, &service.APIKey{
		UserID:  user.ID,
		GroupID: &groupID,
	})
	account := mustCreateAccount(t, integrationEntClient, &service.Account{
		Name: fmt.Sprintf("dynamic-snapshot-%d", time.Now().UnixNano()),
	})

	tx := testTx(t)
	requestID := fmt.Sprintf("dynamic-snapshot:%d", time.Now().UnixNano())
	unmatchedRequestID := requestID + ":unmatched"
	createdAt := time.Now().UTC()
	for _, id := range []string{requestID, unmatchedRequestID} {
		_, err := tx.ExecContext(ctx, `
INSERT INTO usage_logs (
    user_id, api_key_id, account_id, request_id, model, group_id,
    input_tokens, total_cost, actual_cost, rate_multiplier, created_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
`, user.ID, apiKey.ID, account.ID, id, "gpt-dynamic", group.ID, 100, 1.0, 0.1437, 0.1437, createdAt)
		require.NoError(t, err)
	}

	_, err := tx.ExecContext(ctx, `
INSERT INTO dynamic_rate_usage_events (
    request_id, api_key_id, user_id, group_id, total_tokens,
    resolved_multiplier, occurred_at
) VALUES ($1, $2, $3, $4, $5, $6, $7)
`, requestID, apiKey.ID, user.ID, group.ID, 100, 0.1437, createdAt)
	require.NoError(t, err)

	migrationSQL, err := dbmigrations.FS.ReadFile("237_usage_log_dynamic_rate_multiplier.sql")
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, string(migrationSQL))
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, string(migrationSQL))
	require.NoError(t, err, "migration must remain idempotent")

	var resolved *float64
	err = tx.QueryRowContext(ctx, `
SELECT dynamic_rate_multiplier
FROM usage_logs
WHERE request_id = $1 AND api_key_id = $2
`, requestID, apiKey.ID).Scan(&resolved)
	require.NoError(t, err)
	require.NotNil(t, resolved)
	require.InDelta(t, 0.1437, *resolved, 0.000001)

	var unmatched *float64
	err = tx.QueryRowContext(ctx, `
SELECT dynamic_rate_multiplier
FROM usage_logs
WHERE request_id = $1 AND api_key_id = $2
`, unmatchedRequestID, apiKey.ID).Scan(&unmatched)
	require.NoError(t, err)
	require.Nil(t, unmatched)
}

//go:build integration

package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
)

func TestGPT56CacheWriteInferenceMigrationBackfillsAndCleansLegacyAccountSettings(t *testing.T) {
	ctx := context.Background()
	tx := testTx(t)
	suffix := time.Now().UnixNano()

	var groupID int64
	err := tx.QueryRowContext(
		ctx,
		"INSERT INTO groups (name, platform) VALUES ($1, 'openai') RETURNING id",
		fmt.Sprintf("gpt56-cache-write-migration-%d", suffix),
	).Scan(&groupID)
	require.NoError(t, err)

	insertAccount := func(name string, extra string) int64 {
		t.Helper()
		var accountID int64
		err := tx.QueryRowContext(
			ctx,
			"INSERT INTO accounts (name, platform, type, extra) VALUES ($1, 'openai', 'api_key', $2::jsonb) RETURNING id",
			name,
			extra,
		).Scan(&accountID)
		require.NoError(t, err)
		_, err = tx.ExecContext(
			ctx,
			"INSERT INTO account_groups (account_id, group_id, priority, created_at) VALUES ($1, $2, 1, NOW())",
			accountID,
			groupID,
		)
		require.NoError(t, err)
		return accountID
	}

	accountA := insertAccount(
		fmt.Sprintf("gpt56-cache-write-account-a-%d", suffix),
		`{"infer_gpt56_cache_write":true,"infer_gpt56_cache_write_min_tokens":2048,"retained":"yes"}`,
	)
	accountB := insertAccount(
		fmt.Sprintf("gpt56-cache-write-account-b-%d", suffix),
		`{"infer_gpt56_cache_write":true,"infer_gpt56_cache_write_min_tokens":1536}`,
	)
	accountDisabled := insertAccount(
		fmt.Sprintf("gpt56-cache-write-account-disabled-%d", suffix),
		`{"infer_gpt56_cache_write":false,"infer_gpt56_cache_write_min_tokens":512}`,
	)

	migrationSQL, err := migrations.FS.ReadFile("187_move_gpt56_cache_write_inference_to_groups.sql")
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, string(migrationSQL))
	require.NoError(t, err)

	var enabled bool
	var minTokens int
	err = tx.QueryRowContext(
		ctx,
		"SELECT infer_gpt56_cache_write, infer_gpt56_cache_write_min_tokens FROM groups WHERE id = $1",
		groupID,
	).Scan(&enabled, &minTokens)
	require.NoError(t, err)
	require.True(t, enabled)
	require.Equal(t, 1536, minTokens)

	for _, accountID := range []int64{accountA, accountB, accountDisabled} {
		var hasEnabledKey bool
		var hasThresholdKey bool
		err = tx.QueryRowContext(
			ctx,
			"SELECT extra ? 'infer_gpt56_cache_write', extra ? 'infer_gpt56_cache_write_min_tokens' FROM accounts WHERE id = $1",
			accountID,
		).Scan(&hasEnabledKey, &hasThresholdKey)
		require.NoError(t, err)
		require.False(t, hasEnabledKey)
		require.False(t, hasThresholdKey)
	}

	var retained string
	require.NoError(t, tx.QueryRowContext(ctx, "SELECT extra->>'retained' FROM accounts WHERE id = $1", accountA).Scan(&retained))
	require.Equal(t, "yes", retained)

	_, err = tx.ExecContext(
		ctx,
		"UPDATE groups SET infer_gpt56_cache_write = FALSE, infer_gpt56_cache_write_min_tokens = 4096 WHERE id = $1",
		groupID,
	)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, string(migrationSQL))
	require.NoError(t, err)

	err = tx.QueryRowContext(
		ctx,
		"SELECT infer_gpt56_cache_write, infer_gpt56_cache_write_min_tokens FROM groups WHERE id = $1",
		groupID,
	).Scan(&enabled, &minTokens)
	require.NoError(t, err)
	require.False(t, enabled)
	require.Equal(t, 4096, minTokens)
}

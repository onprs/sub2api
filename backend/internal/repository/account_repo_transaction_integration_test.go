//go:build integration

package repository

import (
	"context"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAccountRepositoryBindGroupsReusesOuterTransaction(t *testing.T) {
	ctx := context.Background()
	suffix := time.Now().UnixNano()
	group := mustCreateGroup(t, integrationEntClient, &service.Group{
		Name:           "account-group-tx-" + time.Unix(0, suffix).Format("150405.000000000"),
		RateMultiplier: 1,
	})
	account := mustCreateAccount(t, integrationEntClient, &service.Account{
		Name: "account-group-tx-original",
	})
	repo := newAccountRepositoryWithSQL(integrationEntClient, integrationDB, nil)
	require.NoError(t, repo.BindGroups(ctx, account.ID, []int64{group.ID}))

	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM scheduler_outbox WHERE account_id = $1", account.ID)
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM account_groups WHERE account_id = $1", account.ID)
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM accounts WHERE id = $1", account.ID)
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM groups WHERE id = $1", group.ID)
	})

	tx, err := integrationEntClient.Tx(ctx)
	require.NoError(t, err)
	txCtx := dbent.NewTxContext(ctx, tx)
	changedName := "account-group-tx-changed"
	rows, err := repo.BulkUpdate(txCtx, []int64{account.ID}, service.AccountBulkUpdate{Name: &changedName})
	require.NoError(t, err)
	require.Equal(t, int64(1), rows)

	err = repo.BindGroups(txCtx, account.ID, []int64{1 << 60})
	require.Error(t, err)
	require.NoError(t, tx.Rollback())

	loaded, err := integrationEntClient.Account.Get(ctx, account.ID)
	require.NoError(t, err)
	require.Equal(t, "account-group-tx-original", loaded.Name)
	groups, err := repo.GetGroups(ctx, account.ID)
	require.NoError(t, err)
	require.Len(t, groups, 1)
	require.Equal(t, group.ID, groups[0].ID)
}

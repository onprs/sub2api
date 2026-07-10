package service

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/enttest"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func TestWithSubscriptionUpdateTxReusesExistingTransaction(t *testing.T) {
	ctx := context.Background()
	client := newSubscriptionTransactionTestClient(t)
	outerTx, err := client.Tx(ctx)
	require.NoError(t, err)
	defer func() { _ = outerTx.Rollback() }()

	svc := NewSubscriptionService(nil, nil, nil, client, nil)
	txCtx := dbent.NewTxContext(ctx, outerTx)

	var gotTx *dbent.Tx
	err = svc.withSubscriptionUpdateTx(txCtx, func(ctx context.Context) error {
		gotTx = dbent.TxFromContext(ctx)
		if gotTx == nil {
			return fmt.Errorf("missing transaction in context")
		}
		if gotTx != outerTx {
			return fmt.Errorf("started nested subscription transaction")
		}
		return nil
	})

	require.NoError(t, err)
	require.Same(t, outerTx, gotTx)
}

func newSubscriptionTransactionTestClient(t *testing.T) *dbent.Client {
	t.Helper()

	db, err := sql.Open("sqlite", fmt.Sprintf("file:%s?mode=memory&cache=shared&_fk=1", t.Name()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	_, err = db.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)

	drv := entsql.OpenDB(dialect.SQLite, db)
	client := enttest.NewClient(t, enttest.WithOptions(dbent.Driver(drv)))
	t.Cleanup(func() { _ = client.Close() })
	return client
}

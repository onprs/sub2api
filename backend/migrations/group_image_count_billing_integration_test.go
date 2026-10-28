//go:build integration

package migrations

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestGroupImageCountBillingMigration(t *testing.T) {
	dsn := os.Getenv("SUB2API_MIGRATION_TEST_DSN")
	if dsn == "" {
		t.Skip("需要 SUB2API_MIGRATION_TEST_DSN 指向独立 PostgreSQL 测试库")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	schema := fmt.Sprintf("image_count_billing_test_%d", time.Now().UnixNano())
	_, err = tx.ExecContext(ctx, fmt.Sprintf("CREATE SCHEMA %q; SET LOCAL search_path = %q", schema, schema))
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `CREATE TABLE groups (id BIGINT PRIMARY KEY, name TEXT NOT NULL); INSERT INTO groups (id, name) VALUES (1, 'existing')`)
	require.NoError(t, err)
	migration, err := FS.ReadFile("248_group_image_count_billing.sql")
	require.NoError(t, err)
	for range 2 {
		_, err = tx.ExecContext(ctx, string(migration))
		require.NoError(t, err)
	}
	var name string
	var enabled bool
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT name, image_count_billing FROM groups WHERE id = 1`).Scan(&name, &enabled))
	require.Equal(t, "existing", name)
	require.False(t, enabled)
	_, err = tx.ExecContext(ctx, `UPDATE groups SET image_count_billing = true WHERE id = 1; INSERT INTO groups (id, name) VALUES (2, 'new')`)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, string(migration))
	require.NoError(t, err)
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT image_count_billing FROM groups WHERE id = 1`).Scan(&enabled))
	require.True(t, enabled)
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT image_count_billing FROM groups WHERE id = 2`).Scan(&enabled))
	require.False(t, enabled)
}

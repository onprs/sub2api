//go:build unit

package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

const (
	previousPlanScopedMigrationChecksum  = "97647c6f0273cfcb026dccd1dfbb3b4847b633f1e8f167ad4060fab4e179ca81"
	liveOnprsPlanScopedMigrationChecksum = "0fe1a6ddd92f10c6cb6a399cf0b8a0d0cc92b45f0c32dd834a67db9ad609ecf7"
)

func TestPlanScopedSubscriptionMigrationCreatesNewUniqueIndexesBeforeDroppingOldProtection(t *testing.T) {
	sqlBytes, err := os.ReadFile(filepath.Join("..", "..", "migrations", "146_plan_scoped_user_subscriptions.sql"))
	require.NoError(t, err)

	migrationSQL := string(sqlBytes)
	createPlanIdx := strings.Index(migrationSQL, "CREATE UNIQUE INDEX IF NOT EXISTS user_subscriptions_user_group_plan_unique_active")
	createLegacyIdx := strings.Index(migrationSQL, "CREATE UNIQUE INDEX IF NOT EXISTS user_subscriptions_user_group_legacy_unique_active")
	dropConstraint := strings.Index(migrationSQL, "ALTER TABLE user_subscriptions DROP CONSTRAINT IF EXISTS user_subscriptions_user_id_group_id_key")
	dropOldIdx := strings.Index(migrationSQL, "DROP INDEX IF EXISTS usersubscription_user_id_group_id")

	require.NotEqual(t, -1, createPlanIdx)
	require.NotEqual(t, -1, createLegacyIdx)
	require.NotEqual(t, -1, dropConstraint)
	require.NotEqual(t, -1, dropOldIdx)
	require.Less(t, createPlanIdx, dropConstraint)
	require.Less(t, createLegacyIdx, dropConstraint)
	require.Less(t, createPlanIdx, dropOldIdx)
	require.Less(t, createLegacyIdx, dropOldIdx)
	require.Contains(t, migrationSQL, "RAISE EXCEPTION 'duplicate active plan-scoped user subscriptions exist")
	require.Contains(t, migrationSQL, "RAISE EXCEPTION 'duplicate active legacy user subscriptions exist")
}

func TestPlanScopedSubscriptionMigrationChecksumCompatibilityMatchesCurrentFile(t *testing.T) {
	sqlBytes, err := os.ReadFile(filepath.Join("..", "..", "migrations", "146_plan_scoped_user_subscriptions.sql"))
	require.NoError(t, err)

	currentChecksum := sha256Hex(strings.TrimSpace(string(sqlBytes)))

	rule, ok := migrationChecksumCompatibilityRules["146_plan_scoped_user_subscriptions.sql"]
	require.True(t, ok)
	require.Equal(t, currentChecksum, rule.fileChecksum)
	require.True(t, isMigrationChecksumCompatible("146_plan_scoped_user_subscriptions.sql", previousPlanScopedMigrationChecksum, currentChecksum))
	require.True(t, isMigrationChecksumCompatible("146_plan_scoped_user_subscriptions.sql", liveOnprsPlanScopedMigrationChecksum, currentChecksum))
}

func TestApplyMigrationsFSSkipsLiveOnprsPlanScopedChecksumAndAppliesPaymentOrderSubscriptionID(t *testing.T) {
	planScopedSQL, err := os.ReadFile(filepath.Join("..", "..", "migrations", "146_plan_scoped_user_subscriptions.sql"))
	require.NoError(t, err)
	paymentOrderSubscriptionSQL, err := os.ReadFile(filepath.Join("..", "..", "migrations", "149_payment_order_subscription_id.sql"))
	require.NoError(t, err)

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	prepareMigrationsBootstrapExpectations(mock)
	mock.ExpectQuery("SELECT checksum FROM schema_migrations WHERE filename = \\$1").
		WithArgs("146_plan_scoped_user_subscriptions.sql").
		WillReturnRows(sqlmock.NewRows([]string{"checksum"}).AddRow(liveOnprsPlanScopedMigrationChecksum))
	mock.ExpectQuery("SELECT checksum FROM schema_migrations WHERE filename = \\$1").
		WithArgs("149_payment_order_subscription_id.sql").
		WillReturnError(sql.ErrNoRows)
	mock.ExpectBegin()
	mock.ExpectExec("ALTER TABLE payment_orders").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("INSERT INTO schema_migrations \\(filename, checksum\\) VALUES \\(\\$1, \\$2\\)").
		WithArgs("149_payment_order_subscription_id.sql", sha256Hex(strings.TrimSpace(string(paymentOrderSubscriptionSQL)))).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()
	mock.ExpectExec("SELECT pg_advisory_unlock\\(\\$1\\)").
		WithArgs(migrationsAdvisoryLockID).
		WillReturnResult(sqlmock.NewResult(0, 1))

	fsys := fstest.MapFS{
		"146_plan_scoped_user_subscriptions.sql": &fstest.MapFile{Data: planScopedSQL},
		"149_payment_order_subscription_id.sql":  &fstest.MapFile{Data: paymentOrderSubscriptionSQL},
	}

	require.NoError(t, applyMigrationsFS(context.Background(), db, fsys))
	require.NoError(t, mock.ExpectationsWereMet())
}

func sha256Hex(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

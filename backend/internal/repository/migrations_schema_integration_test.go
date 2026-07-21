//go:build integration

package repository

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMigrationsRunner_IsIdempotent_AndSchemaIsUpToDate(t *testing.T) {
	tx := testTx(t)

	// Re-apply migrations to verify idempotency (no errors, no duplicate rows).
	require.NoError(t, ApplyMigrations(context.Background(), integrationDB))

	// schema_migrations should have at least the current migration set.
	var applied int
	require.NoError(t, tx.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM schema_migrations").Scan(&applied))
	require.GreaterOrEqual(t, applied, 7, "expected schema_migrations to contain applied migrations")

	// users: columns required by repository queries
	requireColumn(t, tx, "users", "username", "character varying", 100, false)
	requireColumn(t, tx, "users", "notes", "text", 0, false)

	// accounts: schedulable and rate-limit fields
	requireColumn(t, tx, "accounts", "notes", "text", 0, true)
	requireColumn(t, tx, "accounts", "schedulable", "boolean", 0, false)
	requireColumn(t, tx, "accounts", "rate_limited_at", "timestamp with time zone", 0, true)
	requireColumn(t, tx, "accounts", "rate_limit_reset_at", "timestamp with time zone", 0, true)
	requireColumn(t, tx, "accounts", "overload_until", "timestamp with time zone", 0, true)
	requireColumn(t, tx, "accounts", "session_window_status", "character varying", 20, true)
	requireIndex(t, tx, "accounts", "idx_accounts_autopause_expiry_due")

	// api_keys: key length should be 128
	requireColumn(t, tx, "api_keys", "key", "character varying", 128, false)

	// redeem_codes: subscription fields
	requireColumn(t, tx, "redeem_codes", "group_id", "bigint", 0, true)
	requireColumn(t, tx, "redeem_codes", "validity_days", "integer", 0, false)
	requireColumn(t, tx, "redeem_codes", "subscription_plan_id", "bigint", 0, true)
	requireColumn(t, tx, "redeem_codes", "subscription_quota_snapshot_version", "integer", 0, false)
	requireColumn(t, tx, "redeem_codes", "five_hour_limit_usd", "numeric", 0, true)
	requireColumn(t, tx, "redeem_codes", "seven_day_limit_usd", "numeric", 0, true)
	requireColumn(t, tx, "redeem_codes", "thirty_day_limit_usd", "numeric", 0, true)
	requireIndex(t, tx, "redeem_codes", "idx_redeem_codes_subscription_plan_id")

	// usage_logs: billing_type used by filters/stats
	requireColumn(t, tx, "usage_logs", "billing_type", "smallint", 0, false)
	requireColumn(t, tx, "usage_logs", "request_type", "smallint", 0, false)
	requireColumn(t, tx, "usage_logs", "openai_ws_mode", "boolean", 0, false)
	requireColumn(t, tx, "usage_logs", "image_input_size", "character varying", 32, true)
	requireColumn(t, tx, "usage_logs", "image_output_size", "character varying", 32, true)
	requireColumn(t, tx, "usage_logs", "image_size_source", "character varying", 16, true)
	requireColumn(t, tx, "usage_logs", "image_size_breakdown", "jsonb", 0, true)
	requireColumn(t, tx, "usage_logs", "video_count", "integer", 0, false)
	requireColumn(t, tx, "usage_logs", "video_resolution", "character varying", 10, true)
	requireColumn(t, tx, "usage_logs", "video_duration_seconds", "integer", 0, true)
	requireConstraintDefinitionContains(
		t,
		tx,
		"usage_logs",
		"usage_logs_image_size_source_check",
		"image_size_source",
		"'output'",
		"'input'",
		"'default'",
		"'legacy'",
	)
	requireConstraintDefinitionContains(
		t,
		tx,
		"usage_logs",
		"usage_logs_image_billing_size_check",
		"image_count",
		"billing_mode",
		"'video'",
		"video_count",
		"image_size IS NOT NULL",
		"'1K'",
		"'2K'",
		"'4K'",
		"'mixed'",
	)

	// usage_billing_dedup: billing idempotency narrow table
	var usageBillingDedupRegclass sql.NullString
	require.NoError(t, tx.QueryRowContext(context.Background(), "SELECT to_regclass('public.usage_billing_dedup')").Scan(&usageBillingDedupRegclass))
	require.True(t, usageBillingDedupRegclass.Valid, "expected usage_billing_dedup table to exist")
	requireColumn(t, tx, "usage_billing_dedup", "request_fingerprint", "character varying", 64, false)
	requireIndex(t, tx, "usage_billing_dedup", "idx_usage_billing_dedup_request_api_key")
	requireIndex(t, tx, "usage_billing_dedup", "idx_usage_billing_dedup_created_at_brin")

	var usageBillingDedupArchiveRegclass sql.NullString
	require.NoError(t, tx.QueryRowContext(context.Background(), "SELECT to_regclass('public.usage_billing_dedup_archive')").Scan(&usageBillingDedupArchiveRegclass))
	require.True(t, usageBillingDedupArchiveRegclass.Valid, "expected usage_billing_dedup_archive table to exist")
	requireColumn(t, tx, "usage_billing_dedup_archive", "request_fingerprint", "character varying", 64, false)
	requireIndex(t, tx, "usage_billing_dedup_archive", "usage_billing_dedup_archive_pkey")

	// settings table should exist
	var settingsRegclass sql.NullString
	require.NoError(t, tx.QueryRowContext(context.Background(), "SELECT to_regclass('public.settings')").Scan(&settingsRegclass))
	require.True(t, settingsRegclass.Valid, "expected settings table to exist")

	// security_secrets table should exist
	var securitySecretsRegclass sql.NullString
	require.NoError(t, tx.QueryRowContext(context.Background(), "SELECT to_regclass('public.security_secrets')").Scan(&securitySecretsRegclass))
	require.True(t, securitySecretsRegclass.Valid, "expected security_secrets table to exist")

	// scheduler_outbox pending dedup support
	requireColumn(t, tx, "scheduler_outbox", "dedup_key", "text", 0, true)
	requireIndex(t, tx, "scheduler_outbox", "idx_scheduler_outbox_pending_dedup_key")

	// ops_system_logs: API key id index for operational log triage
	requireColumn(t, tx, "ops_system_logs", "api_key_id", "bigint", 0, true)
	requireIndex(t, tx, "ops_system_logs", "idx_ops_system_logs_api_key_id_created_at")

	// user_allowed_groups table should exist
	var uagRegclass sql.NullString
	require.NoError(t, tx.QueryRowContext(context.Background(), "SELECT to_regclass('public.user_allowed_groups')").Scan(&uagRegclass))
	require.True(t, uagRegclass.Valid, "expected user_allowed_groups table to exist")

	// user_subscriptions: deleted_at for soft delete support (migration 012)
	requireColumn(t, tx, "user_subscriptions", "deleted_at", "timestamp with time zone", 0, true)

	// subscription rolling quota limits
	requireColumn(t, tx, "subscription_plans", "five_hour_limit_usd", "numeric", 0, true)
	requireColumn(t, tx, "subscription_plans", "seven_day_limit_usd", "numeric", 0, true)
	requireColumn(t, tx, "subscription_plans", "thirty_day_limit_usd", "numeric", 0, true)
	requireColumn(t, tx, "subscription_plans", "renewal_discount_percent", "numeric", 0, true)
	requireColumn(t, tx, "subscription_plans", "stock", "integer", 0, true)
	requireConstraintDefinitionContains(
		t,
		tx,
		"subscription_plans",
		"subscription_plans_renewal_discount_percent_range",
		"renewal_discount_percent",
		">=",
		"<",
		"100",
	)
	requireConstraintDefinitionContains(
		t,
		tx,
		"subscription_plans",
		"subscription_plan_stock_non_negative",
		"stock",
		">=",
		"0",
	)
	requireColumn(t, tx, "user_subscriptions", "five_hour_limit_usd", "numeric", 0, true)
	requireColumn(t, tx, "user_subscriptions", "seven_day_limit_usd", "numeric", 0, true)
	requireColumn(t, tx, "user_subscriptions", "thirty_day_limit_usd", "numeric", 0, true)
	requireColumn(t, tx, "user_subscriptions", "five_hour_usage_usd", "numeric", 0, false)
	requireColumn(t, tx, "user_subscriptions", "seven_day_usage_usd", "numeric", 0, false)
	requireColumn(t, tx, "user_subscriptions", "thirty_day_usage_usd", "numeric", 0, false)
	requireColumn(t, tx, "user_subscriptions", "five_hour_window_start", "timestamp with time zone", 0, true)
	requireColumn(t, tx, "user_subscriptions", "seven_day_window_start", "timestamp with time zone", 0, true)
	requireColumn(t, tx, "user_subscriptions", "thirty_day_window_start", "timestamp with time zone", 0, true)
	requireColumn(t, tx, "user_subscriptions", "plan_id", "bigint", 0, true)
	requireIndex(t, tx, "user_subscriptions", "idx_user_subscriptions_plan_id")
	requirePartialUniqueIndexDefinition(t, tx, "user_subscriptions", "user_subscriptions_user_group_plan_unique_active", "plan_id", "WHERE")
	requirePartialUniqueIndexDefinition(t, tx, "user_subscriptions", "user_subscriptions_user_group_legacy_unique_active", "group_id", "WHERE")
	requireColumn(t, tx, "payment_orders", "subscription_quota_snapshot_version", "integer", 0, false)
	requireColumn(t, tx, "payment_orders", "subscription_five_hour_limit_usd", "numeric", 0, true)
	requireColumn(t, tx, "payment_orders", "subscription_seven_day_limit_usd", "numeric", 0, true)
	requireColumn(t, tx, "payment_orders", "subscription_thirty_day_limit_usd", "numeric", 0, true)
	requireColumn(t, tx, "payment_orders", "subscription_id", "bigint", 0, true)
	requireColumn(t, tx, "payment_orders", "subscription_plan_price", "numeric", 0, true)
	requireColumn(t, tx, "payment_orders", "subscription_renewal_discount_percent", "numeric", 0, true)
	requireIndex(t, tx, "payment_orders", "paymentorder_subscription_id")
	requireConstraintDefinitionContains(
		t,
		tx,
		"payment_orders",
		"payment_orders_subscription_plan_price_positive",
		"subscription_plan_price",
		">",
		"0",
	)
	requireConstraintDefinitionContains(
		t,
		tx,
		"payment_orders",
		"payment_orders_subscription_renewal_discount_percent_range",
		"subscription_renewal_discount_percent",
		">=",
		"<",
		"100",
	)

	// orphan_allowed_groups_audit table should exist (migration 013)
	var orphanAuditRegclass sql.NullString
	require.NoError(t, tx.QueryRowContext(context.Background(), "SELECT to_regclass('public.orphan_allowed_groups_audit')").Scan(&orphanAuditRegclass))
	require.True(t, orphanAuditRegclass.Valid, "expected orphan_allowed_groups_audit table to exist")

	// account_groups: created_at should be timestamptz
	requireColumn(t, tx, "account_groups", "created_at", "timestamp with time zone", 0, false)

	// user_allowed_groups: created_at should be timestamptz
	requireColumn(t, tx, "user_allowed_groups", "created_at", "timestamp with time zone", 0, false)
}

func TestMigrationsRunner_AuthIdentityAndPaymentSchemaStayAligned(t *testing.T) {
	tx := testTx(t)

	requireColumn(t, tx, "auth_identity_migration_reports", "report_type", "character varying", 80, false)
	requireColumn(t, tx, "users", "signup_source", "character varying", 20, false)
	requireColumnDefaultContains(t, tx, "users", "signup_source", "email")
	requireConstraintDefinitionContains(
		t,
		tx,
		"users",
		"users_signup_source_check",
		"signup_source",
		"'email'",
		"'linuxdo'",
		"'wechat'",
		"'oidc'",
	)

	requireForeignKeyOnDelete(t, tx, "auth_identities", "user_id", "users", "CASCADE")
	requireForeignKeyOnDelete(t, tx, "auth_identity_channels", "identity_id", "auth_identities", "CASCADE")
	requireForeignKeyOnDelete(t, tx, "pending_auth_sessions", "target_user_id", "users", "SET NULL")
	requireForeignKeyOnDelete(t, tx, "identity_adoption_decisions", "pending_auth_session_id", "pending_auth_sessions", "CASCADE")
	requireForeignKeyOnDelete(t, tx, "identity_adoption_decisions", "identity_id", "auth_identities", "SET NULL")

	requireIndex(t, tx, "payment_orders", "paymentorder_out_trade_no")
	requirePartialUniqueIndexDefinition(t, tx, "payment_orders", "paymentorder_out_trade_no", "out_trade_no", "WHERE")
	requireIndexAbsent(t, tx, "payment_orders", "paymentorder_out_trade_no_unique")
}

func TestMigrationsRunner_TicketingSchemaStaysAligned(t *testing.T) {
	tx := testTx(t)

	requireColumn(t, tx, "tickets", "ticket_no", "character varying", 32, false)
	requireColumn(t, tx, "tickets", "user_id", "bigint", 0, true)
	requireColumn(t, tx, "tickets", "assignee_id", "bigint", 0, true)
	requireColumn(t, tx, "tickets", "status", "character varying", 24, false)
	requireColumn(t, tx, "tickets", "action_required_since", "timestamp with time zone", 0, true)
	requireColumn(t, tx, "tickets", "user_notification_seq", "bigint", 0, false)
	requireColumn(t, tx, "tickets", "user_last_read_seq", "bigint", 0, false)
	requireColumn(t, tx, "tickets", "version", "bigint", 0, false)
	requireForeignKeyOnDelete(t, tx, "tickets", "user_id", "users", "SET NULL")
	requireForeignKeyOnDelete(t, tx, "tickets", "assignee_id", "users", "SET NULL")
	requireConstraintDefinitionContains(t, tx, "tickets", "tickets_category_check", "api_issue", "feature_request")
	requireConstraintDefinitionContains(t, tx, "tickets", "tickets_status_check", "waiting_user", "resolved", "closed")
	requireConstraintDefinitionContains(t, tx, "tickets", "tickets_notification_seq_check", "user_last_read_seq", "user_notification_seq")
	requireIndex(t, tx, "tickets", "idx_tickets_user_public_activity")
	requireIndex(t, tx, "tickets", "idx_tickets_admin_queue")
	requireIndex(t, tx, "tickets", "idx_tickets_resolved_auto_close")

	requireColumn(t, tx, "ticket_messages", "ticket_id", "bigint", 0, false)
	requireColumn(t, tx, "ticket_messages", "body", "text", 0, false)
	requireColumn(t, tx, "ticket_messages", "visibility", "character varying", 16, false)
	requireForeignKeyOnDelete(t, tx, "ticket_messages", "ticket_id", "tickets", "CASCADE")
	requireForeignKeyOnDelete(t, tx, "ticket_messages", "author_id", "users", "SET NULL")
	requireConstraintDefinitionContains(t, tx, "ticket_messages", "ticket_messages_user_visibility_check", "author_role", "visibility", "public")
	requireIndex(t, tx, "ticket_messages", "idx_ticket_messages_ticket_visibility_id")

	requireColumn(t, tx, "ticket_events", "payload", "jsonb", 0, false)
	requireColumn(t, tx, "ticket_events", "event_type", "character varying", 40, false)
	requireForeignKeyOnDelete(t, tx, "ticket_events", "ticket_id", "tickets", "CASCADE")
	requireForeignKeyOnDelete(t, tx, "ticket_events", "actor_id", "users", "SET NULL")
	requireIndex(t, tx, "ticket_events", "idx_ticket_events_ticket_visibility_id")

	requireColumn(t, tx, "ticket_attachments", "upload_token", "character varying", 64, false)
	requireColumn(t, tx, "ticket_attachments", "storage_provider", "character varying", 16, false)
	requireColumn(t, tx, "ticket_attachments", "object_key", "text", 0, false)
	requireColumn(t, tx, "ticket_attachments", "expires_at", "timestamp with time zone", 0, true)
	requireForeignKeyOnDelete(t, tx, "ticket_attachments", "uploaded_by", "users", "SET NULL")
	requireForeignKeyOnDelete(t, tx, "ticket_attachments", "ticket_id", "tickets", "CASCADE")
	requireForeignKeyOnDelete(t, tx, "ticket_attachments", "message_id", "ticket_messages", "CASCADE")
	requireConstraintDefinitionContains(t, tx, "ticket_attachments", "ticket_attachments_lifecycle_check", "pending", "attached", "deleting")
	requireIndex(t, tx, "ticket_attachments", "idx_ticket_attachments_pending_expiry")
	requireIndex(t, tx, "ticket_attachments", "idx_ticket_attachments_uploader_activity")
	requireIndex(t, tx, "ticket_attachments", "ticket_attachments_storage_object_unique")
}

func requireIndex(t *testing.T, tx *sql.Tx, table, index string) {
	t.Helper()

	var exists bool
	err := tx.QueryRowContext(context.Background(), `
SELECT EXISTS (
	SELECT 1
	FROM pg_indexes
	WHERE schemaname = 'public'
	  AND tablename = $1
	  AND indexname = $2
)
`, table, index).Scan(&exists)
	require.NoError(t, err, "query pg_indexes for %s.%s", table, index)
	require.True(t, exists, "expected index %s on %s", index, table)
}

func requireIndexAbsent(t *testing.T, tx *sql.Tx, table, index string) {
	t.Helper()

	var exists bool
	err := tx.QueryRowContext(context.Background(), `
SELECT EXISTS (
	SELECT 1
	FROM pg_indexes
	WHERE schemaname = 'public'
	  AND tablename = $1
	  AND indexname = $2
)
`, table, index).Scan(&exists)
	require.NoError(t, err, "query pg_indexes for %s.%s", table, index)
	require.False(t, exists, "expected index %s on %s to be absent", index, table)
}

func requirePartialUniqueIndexDefinition(t *testing.T, tx *sql.Tx, table, index string, fragments ...string) {
	t.Helper()

	var (
		unique bool
		def    string
	)

	err := tx.QueryRowContext(context.Background(), `
SELECT
	i.indisunique,
	pg_get_indexdef(i.indexrelid)
FROM pg_class idx
JOIN pg_index i ON i.indexrelid = idx.oid
JOIN pg_class tbl ON tbl.oid = i.indrelid
JOIN pg_namespace ns ON ns.oid = tbl.relnamespace
WHERE ns.nspname = 'public'
  AND tbl.relname = $1
  AND idx.relname = $2
`, table, index).Scan(&unique, &def)
	require.NoError(t, err, "query index definition for %s.%s", table, index)
	require.True(t, unique, "expected index %s on %s to be unique", index, table)

	for _, fragment := range fragments {
		require.Contains(t, def, fragment, "expected index definition for %s.%s to contain %q", table, index, fragment)
	}
}

func requireForeignKeyOnDelete(t *testing.T, tx *sql.Tx, table, column, refTable, expected string) {
	t.Helper()

	var actual string
	err := tx.QueryRowContext(context.Background(), `
SELECT CASE c.confdeltype
	WHEN 'a' THEN 'NO ACTION'
	WHEN 'r' THEN 'RESTRICT'
	WHEN 'c' THEN 'CASCADE'
	WHEN 'n' THEN 'SET NULL'
	WHEN 'd' THEN 'SET DEFAULT'
END
FROM pg_constraint c
JOIN pg_class tbl ON tbl.oid = c.conrelid
JOIN pg_namespace ns ON ns.oid = tbl.relnamespace
JOIN pg_class ref_tbl ON ref_tbl.oid = c.confrelid
JOIN pg_attribute attr ON attr.attrelid = tbl.oid AND attr.attnum = ANY(c.conkey)
WHERE ns.nspname = 'public'
  AND c.contype = 'f'
  AND tbl.relname = $1
  AND attr.attname = $2
  AND ref_tbl.relname = $3
LIMIT 1
`, table, column, refTable).Scan(&actual)
	require.NoError(t, err, "query foreign key action for %s.%s -> %s", table, column, refTable)
	require.Equal(t, expected, actual, "unexpected ON DELETE action for %s.%s -> %s", table, column, refTable)
}

func requireConstraintDefinitionContains(t *testing.T, tx *sql.Tx, table, constraint string, fragments ...string) {
	t.Helper()

	var def string
	err := tx.QueryRowContext(context.Background(), `
SELECT pg_get_constraintdef(c.oid)
FROM pg_constraint c
JOIN pg_class tbl ON tbl.oid = c.conrelid
JOIN pg_namespace ns ON ns.oid = tbl.relnamespace
WHERE ns.nspname = 'public'
  AND tbl.relname = $1
  AND c.conname = $2
`, table, constraint).Scan(&def)
	require.NoError(t, err, "query constraint definition for %s.%s", table, constraint)

	for _, fragment := range fragments {
		require.Contains(t, def, fragment, "expected constraint definition for %s.%s to contain %q", table, constraint, fragment)
	}
}

func requireColumnDefaultContains(t *testing.T, tx *sql.Tx, table, column string, fragments ...string) {
	t.Helper()

	var columnDefault sql.NullString
	err := tx.QueryRowContext(context.Background(), `
SELECT column_default
FROM information_schema.columns
WHERE table_schema = 'public'
  AND table_name = $1
  AND column_name = $2
`, table, column).Scan(&columnDefault)
	require.NoError(t, err, "query column_default for %s.%s", table, column)
	require.True(t, columnDefault.Valid, "expected column_default for %s.%s", table, column)

	for _, fragment := range fragments {
		require.Contains(t, columnDefault.String, fragment, "expected default for %s.%s to contain %q", table, column, fragment)
	}
}

func requireColumn(t *testing.T, tx *sql.Tx, table, column, dataType string, maxLen int, nullable bool) {
	t.Helper()

	var row struct {
		DataType string
		MaxLen   sql.NullInt64
		Nullable string
	}

	err := tx.QueryRowContext(context.Background(), `
SELECT
  data_type,
  character_maximum_length,
  is_nullable
FROM information_schema.columns
WHERE table_schema = 'public'
  AND table_name = $1
  AND column_name = $2
`, table, column).Scan(&row.DataType, &row.MaxLen, &row.Nullable)
	require.NoError(t, err, "query information_schema.columns for %s.%s", table, column)
	require.Equal(t, dataType, row.DataType, "data_type mismatch for %s.%s", table, column)

	if maxLen > 0 {
		require.True(t, row.MaxLen.Valid, "expected maxLen for %s.%s", table, column)
		require.Equal(t, int64(maxLen), row.MaxLen.Int64, "maxLen mismatch for %s.%s", table, column)
	}

	if nullable {
		require.Equal(t, "YES", row.Nullable, "nullable mismatch for %s.%s", table, column)
	} else {
		require.Equal(t, "NO", row.Nullable, "nullable mismatch for %s.%s", table, column)
	}
}

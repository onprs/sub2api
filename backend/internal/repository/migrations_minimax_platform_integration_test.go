//go:build integration

package repository

import (
	"context"
	"database/sql"
	"testing"

	dbmigrations "github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
)

const (
	miniMaxPlatformMigration = "237_add_minimax_platform.sql"
	miniMaxConstraintRepair  = "238_align_minimax_platform_constraints.sql"
)

func TestMigration237PreservesExistingCustomPlatforms(t *testing.T) {
	ctx := context.Background()
	tx := testTx(t)

	createMiniMaxMigrationTestTables(ctx, t, tx, true)
	applyEmbeddedMigration(ctx, t, tx, miniMaxPlatformMigration)

	assertCustomPlatformRowsPreserved(ctx, t, tx)
	insertMiniMaxPlatformRows(ctx, t, tx)

	// 237 的保护分支和无条件重建约束都必须可重放。
	applyEmbeddedMigration(ctx, t, tx, miniMaxPlatformMigration)
	assertMiniMaxPlatformRowsPresent(ctx, t, tx)
}

func TestMigration238RepairsPreviouslyNarrowedConstraints(t *testing.T) {
	ctx := context.Background()
	tx := testTx(t)

	createMiniMaxMigrationTestTables(ctx, t, tx, false)
	applyEmbeddedMigration(ctx, t, tx, miniMaxConstraintRepair)

	insertCustomPlatformRows(ctx, t, tx)
	insertMiniMaxPlatformRows(ctx, t, tx)
	assertCustomPlatformRowsPreserved(ctx, t, tx)
	assertMiniMaxPlatformRowsPresent(ctx, t, tx)

	applyEmbeddedMigration(ctx, t, tx, miniMaxConstraintRepair)
}

func createMiniMaxMigrationTestTables(ctx context.Context, t *testing.T, tx *sql.Tx, includeCustomPlatforms bool) {
	t.Helper()

	quotaPlatforms := "'anthropic', 'openai', 'gemini', 'antigravity', 'grok', 'kimi', 'zhipu', 'deepseek'"
	monitorProviders := "'openai', 'anthropic', 'gemini', 'grok', 'antigravity', 'kimi', 'zhipu', 'deepseek'"
	if includeCustomPlatforms {
		quotaPlatforms = "'anthropic', 'openai', 'opencode_go', 'clinepass', 'openrouter', 'commandcode', 'gemini', 'antigravity', 'grok', 'kimi', 'zhipu', 'deepseek'"
		monitorProviders = "'openai', 'anthropic', 'gemini', 'grok', 'opencode_go', 'clinepass', 'openrouter', 'commandcode', 'antigravity', 'antigravity_claude', 'antigravity_gemini', 'kimi', 'zhipu', 'deepseek'"
	}

	_, err := tx.ExecContext(ctx, `
CREATE TEMP TABLE user_platform_quotas (
    platform VARCHAR(32) NOT NULL
) ON COMMIT DROP;
CREATE TEMP TABLE composite_model_routes (
    target_platform VARCHAR(32) NOT NULL
) ON COMMIT DROP;
CREATE TEMP TABLE channel_monitors (
    provider VARCHAR(32) NOT NULL
) ON COMMIT DROP;
CREATE TEMP TABLE channel_monitor_request_templates (
    provider VARCHAR(32) NOT NULL
) ON COMMIT DROP;

ALTER TABLE user_platform_quotas
    ADD CONSTRAINT user_platform_quotas_platform_check
    CHECK (platform IN (`+quotaPlatforms+`));
ALTER TABLE composite_model_routes
    ADD CONSTRAINT composite_model_routes_target_platform_check
    CHECK (target_platform IN ('anthropic', 'openai', 'gemini', 'antigravity', 'grok', 'kimi', 'zhipu', 'deepseek'));
ALTER TABLE channel_monitors
    ADD CONSTRAINT channel_monitors_provider_check
    CHECK (provider IN (`+monitorProviders+`));
ALTER TABLE channel_monitor_request_templates
    ADD CONSTRAINT channel_monitor_request_templates_provider_check
    CHECK (provider IN (`+monitorProviders+`));
`)
	require.NoError(t, err)

	if includeCustomPlatforms {
		insertCustomPlatformRows(ctx, t, tx)
	}
}

func insertCustomPlatformRows(ctx context.Context, t *testing.T, tx *sql.Tx) {
	t.Helper()

	_, err := tx.ExecContext(ctx, `
INSERT INTO user_platform_quotas (platform)
VALUES ('opencode_go'), ('clinepass'), ('openrouter'), ('commandcode');
INSERT INTO channel_monitors (provider)
VALUES ('opencode_go'), ('clinepass'), ('openrouter'), ('commandcode'),
       ('antigravity_claude'), ('antigravity_gemini');
INSERT INTO channel_monitor_request_templates (provider)
VALUES ('opencode_go'), ('clinepass'), ('openrouter'), ('commandcode'),
       ('antigravity_claude'), ('antigravity_gemini');
`)
	require.NoError(t, err)
}

func insertMiniMaxPlatformRows(ctx context.Context, t *testing.T, tx *sql.Tx) {
	t.Helper()

	_, err := tx.ExecContext(ctx, `
INSERT INTO user_platform_quotas (platform) VALUES ('minimax');
INSERT INTO composite_model_routes (target_platform) VALUES ('minimax');
INSERT INTO channel_monitors (provider) VALUES ('minimax');
INSERT INTO channel_monitor_request_templates (provider) VALUES ('minimax');
`)
	require.NoError(t, err)
}

func assertCustomPlatformRowsPreserved(ctx context.Context, t *testing.T, tx *sql.Tx) {
	t.Helper()

	var quotaCount, monitorCount, templateCount int
	require.NoError(t, tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM user_platform_quotas WHERE platform IN ('opencode_go', 'clinepass', 'openrouter', 'commandcode')").Scan(&quotaCount))
	require.NoError(t, tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM channel_monitors WHERE provider IN ('opencode_go', 'clinepass', 'openrouter', 'commandcode', 'antigravity_claude', 'antigravity_gemini')").Scan(&monitorCount))
	require.NoError(t, tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM channel_monitor_request_templates WHERE provider IN ('opencode_go', 'clinepass', 'openrouter', 'commandcode', 'antigravity_claude', 'antigravity_gemini')").Scan(&templateCount))
	require.Equal(t, 4, quotaCount)
	require.Equal(t, 6, monitorCount)
	require.Equal(t, 6, templateCount)
}

func assertMiniMaxPlatformRowsPresent(ctx context.Context, t *testing.T, tx *sql.Tx) {
	t.Helper()

	for _, query := range []string{
		"SELECT COUNT(*) FROM user_platform_quotas WHERE platform = 'minimax'",
		"SELECT COUNT(*) FROM composite_model_routes WHERE target_platform = 'minimax'",
		"SELECT COUNT(*) FROM channel_monitors WHERE provider = 'minimax'",
		"SELECT COUNT(*) FROM channel_monitor_request_templates WHERE provider = 'minimax'",
	} {
		var count int
		require.NoError(t, tx.QueryRowContext(ctx, query).Scan(&count))
		require.Equal(t, 1, count)
	}
}

func applyEmbeddedMigration(ctx context.Context, t *testing.T, tx *sql.Tx, name string) {
	t.Helper()

	migrationSQL, err := dbmigrations.FS.ReadFile(name)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, string(migrationSQL))
	require.NoError(t, err)
}

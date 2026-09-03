//go:build integration

package repository

import (
	"context"
	"testing"

	dbmigrations "github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
)

func TestMigration224PreservesExistingQuotaPlatforms(t *testing.T) {
	ctx := context.Background()
	tx := testTx(t)

	_, err := tx.ExecContext(ctx, `
CREATE TEMP TABLE user_platform_quotas (
    platform VARCHAR(32) NOT NULL
) ON COMMIT DROP;

ALTER TABLE user_platform_quotas
    ADD CONSTRAINT user_platform_quotas_platform_check
    CHECK (platform IN (
        'anthropic',
        'openai',
        'opencode_go',
        'clinepass',
        'openrouter',
        'commandcode',
        'gemini',
        'antigravity',
        'grok'
    ));

INSERT INTO user_platform_quotas (platform)
VALUES
    ('anthropic'),
    ('openai'),
    ('opencode_go'),
    ('clinepass'),
    ('openrouter'),
    ('commandcode'),
    ('gemini'),
    ('antigravity'),
    ('grok');
`)
	require.NoError(t, err)

	migrationSQL, err := dbmigrations.FS.ReadFile("224_user_platform_quotas_add_cn_providers.sql")
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, string(migrationSQL))
	require.NoError(t, err)

	_, err = tx.ExecContext(ctx, `
INSERT INTO user_platform_quotas (platform)
VALUES ('kimi'), ('zhipu'), ('deepseek');
`)
	require.NoError(t, err)

	var rowCount int
	require.NoError(t, tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM user_platform_quotas").Scan(&rowCount))
	require.Equal(t, 12, rowCount)

	// DROP IF EXISTS + ADD CONSTRAINT 必须保持可重入。
	_, err = tx.ExecContext(ctx, string(migrationSQL))
	require.NoError(t, err)
}

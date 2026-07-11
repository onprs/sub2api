package repository

import (
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
)

func TestUserPlatformQuotaPlatformCheckMigrationsKeepOpenCodeGoAndAllowGrok(t *testing.T) {
	for _, name := range []string{
		"157_user_platform_quotas_add_grok.sql",
		"174_align_user_platform_quotas_platform_check.sql",
	} {
		contentBytes, err := migrations.FS.ReadFile(name)
		require.NoError(t, err)
		content := string(contentBytes)

		require.Contains(t, content, "DROP CONSTRAINT IF EXISTS user_platform_quotas_platform_check")
		require.Contains(t, content, "ADD CONSTRAINT user_platform_quotas_platform_check")
		require.Contains(t, content, "'opencode_go'")
		require.Contains(t, content, "'grok'")
		require.NotContains(t, compactSQL(content), "'anthropic','openai','gemini','antigravity','grok'")
	}
}

func TestUserPlatformQuotaGrokMigrationChecksumCompatibility(t *testing.T) {
	ok := isMigrationChecksumCompatible(
		"157_user_platform_quotas_add_grok.sql",
		"5cace8fa32c6174a72721cd9b01f28f4545de1fd7bcd9ca196a4225056ec4fb8",
		"237b7df1a8bbed36dd082319599ba5eaadd22f29f1324e3a387873b60d464fa0",
	)
	require.True(t, ok)
}

func compactSQL(s string) string {
	replacer := strings.NewReplacer("\n", "", "\t", "", " ", "")
	return replacer.Replace(s)
}

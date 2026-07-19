package repository

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
)

func TestChannelMonitorOnlineOriginMigrationUpdatesLegacyInternalTargets(t *testing.T) {
	contentBytes, err := migrations.FS.ReadFile("177_update_channel_monitor_online_origin.sql")
	require.NoError(t, err)
	content := string(contentBytes)

	require.Contains(t, content, "provider = 'opencode_go'")
	require.Contains(t, content, "provider IN ('antigravity_claude', 'antigravity_gemini')")
	require.Contains(t, content, "'https://cdn-api.onprs.online/v1'")
	require.Contains(t, content, "'https://cdn-api.onprs.online'")
	require.Contains(t, content, "description = REPLACE(")
	require.Contains(t, content, "description LIKE '%https://api.onprs.top%'")
	require.Contains(t, content, "Account upstream base URLs are intentionally unchanged.")
	require.NotContains(t, content, "https://opencode.ai/zen/go/v1")
	require.NotContains(t, content, "cloudcode-pa.googleapis.com")
}

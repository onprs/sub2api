package repository

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestSchedulerMetadataKeepsOpenCodeGoHotPathCredentials(t *testing.T) {
	account := service.Account{
		ID:       42,
		Platform: service.PlatformOpenCodeGo,
		Type:     service.AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":  "ocg-secret",
			"base_url": "https://opencode.ai/zen/go/v1",
			"model_mapping": map[string]any{
				"opencode-go/kimi-k2.7-code": "kimi-k2.7-code",
			},
			"model_protocols": map[string]any{
				"kimi-k2.7-code": "chat_completions",
			},
			"unneeded": "drop-me",
		},
	}

	meta := buildSchedulerMetadataAccount(account)

	require.Equal(t, "ocg-secret", meta.Credentials["api_key"])
	require.Equal(t, "https://opencode.ai/zen/go/v1", meta.Credentials["base_url"])
	require.Equal(t, account.Credentials["model_mapping"], meta.Credentials["model_mapping"])
	require.Equal(t, account.Credentials["model_protocols"], meta.Credentials["model_protocols"])
	require.NotContains(t, meta.Credentials, "unneeded")
}

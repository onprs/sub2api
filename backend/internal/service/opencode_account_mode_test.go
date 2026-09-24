package service

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestOpenCodeZenUsesCanonicalPlatformWithoutGoSubscriptionBehavior(t *testing.T) {
	account := &Account{
		Platform: PlatformOpenCode,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"account_mode": "zen",
			"api_key":      "zen-key",
		},
		Extra: map[string]any{
			"opencode_go_usage_source":          openCodeGoUsageSourceOfficialConsole,
			"opencode_go_console_auth_status":   OpenCodeGoConsoleAuthStatusReady,
			"opencode_go_usage_7d_used_percent": 100.0,
			"opencode_go_usage_7d_resets_at":    time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
		},
	}

	require.True(t, account.IsOpenCode())
	require.False(t, IsOpenCodeGo(PlatformOpenCode))
	require.True(t, IsOpenCodeGo(OpenCodeGoPricingPlatform))
	require.False(t, account.IsOpenCodeGo())
	require.False(t, account.IsOpenCodeGoPlan())
	require.True(t, account.IsOpenAICompatible())
	require.Equal(t, "", account.GetOpenCodeGoAPIKey())
	require.Equal(t, "zen-key", account.GetOpenCodeAPIKey())
	require.Equal(t, DefaultOpenCodeZenBaseURL, account.GetOpenCodeBaseURL())
	require.Equal(t, "", account.GetOpenCodeGoBaseURL())
	require.Equal(t, APIProtocolAnthropic, account.ResolveOpenCodeGoUpstreamProtocol("claude-sonnet-4-6"))
	require.Equal(t, OpenCodeGoPricingPlatform, pricingPlatformForAccount(&Account{
		Platform: PlatformOpenCode,
		Credentials: map[string]any{
			"account_mode": "go",
		},
	}))
	require.Equal(t, PlatformOpenCode, pricingPlatformForAccount(account))
	require.False(t, account.IsQuotaExceeded())

	unsupportedModelError := []byte(`{"error":{"message":"Model is not supported"}}`)
	require.False(t, isUpstreamModelNotFoundErrorForAccount(account, http.StatusForbidden, unsupportedModelError))
	goAccount := &Account{
		Platform: PlatformOpenCode,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"account_mode": AccountModeGo,
			"api_key":      "go-key",
		},
	}
	require.True(t, isUpstreamModelNotFoundErrorForAccount(goAccount, http.StatusForbidden, unsupportedModelError))
}

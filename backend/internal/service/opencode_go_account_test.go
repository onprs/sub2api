package service

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestOpenCodeGoAccountHelpers(t *testing.T) {
	account := &Account{
		Platform: PlatformOpenCodeGo,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":  "ocg-secret",
			"base_url": "https://proxy.example.com/zen/go/v1/",
			"model_protocols": map[string]any{
				"gpt-5.6-luna":   "responses",
				"kimi-k2.7-code": "chat_completions",
				"qwen3.7-plus":   "messages",
				"bad":            "not-a-protocol",
			},
		},
	}

	require.True(t, account.IsOpenCodeGo())
	require.Equal(t, "ocg-secret", account.GetOpenCodeGoAPIKey())
	require.Equal(t, "https://proxy.example.com/zen/go/v1", account.GetOpenCodeGoBaseURL())
	require.Equal(t, map[string]string{
		"gpt-5.6-luna":   "responses",
		"kimi-k2.7-code": "chat_completions",
		"qwen3.7-plus":   "messages",
	}, account.GetOpenCodeGoModelProtocols())

	protocol, ok := account.ResolveOpenCodeGoModelProtocol("gpt-5.6-luna")
	require.True(t, ok)
	require.Equal(t, OpenCodeGoProtocolResponses, protocol)

	protocol, ok = account.ResolveOpenCodeGoModelProtocol("kimi-k2.7")
	require.True(t, ok)
	require.Equal(t, OpenCodeGoProtocolChatCompletions, protocol)

	protocol, ok = account.ResolveOpenCodeGoModelProtocol("kimi-k2.7-code")
	require.True(t, ok)
	require.Equal(t, OpenCodeGoProtocolChatCompletions, protocol)

	protocol, ok = account.ResolveOpenCodeGoModelProtocol("qwen3.7-plus")
	require.True(t, ok)
	require.Equal(t, OpenCodeGoProtocolMessages, protocol)
}

func TestOpenCodeGoDefaultCatalogIncludesCurrentOfficialModels(t *testing.T) {
	models := OpenCodeGoDefaultModelIDs()

	require.Contains(t, models, "gpt-5.6-luna")
	require.Contains(t, models, "glm-5.2")
	require.Contains(t, models, "kimi-k2.5")
	require.Contains(t, models, "qwen3.8-max")
	require.Contains(t, models, "qwen3.5-plus")
	require.Contains(t, models, "mimo-v2-pro")
	require.Contains(t, models, "mimo-v2-omni")
	require.Contains(t, models, "hy3-preview")
}

func TestOpenCodeGoAccountProtocolFallbackCoversNewOfficialModels(t *testing.T) {
	account := &Account{
		Platform: PlatformOpenCodeGo,
		Type:     AccountTypeAPIKey,
	}

	protocol, ok := account.ResolveOpenCodeGoModelProtocol("gpt-5.6-luna")
	require.True(t, ok)
	require.Equal(t, OpenCodeGoProtocolResponses, protocol)

	protocol, ok = account.ResolveOpenCodeGoModelProtocol("glm-5.2")
	require.True(t, ok)
	require.Equal(t, OpenCodeGoProtocolChatCompletions, protocol)

	protocol, ok = account.ResolveOpenCodeGoModelProtocol("kimi-k2.5")
	require.True(t, ok)
	require.Equal(t, OpenCodeGoProtocolChatCompletions, protocol)

	protocol, ok = account.ResolveOpenCodeGoModelProtocol("qwen3.8-max")
	require.True(t, ok)
	require.Equal(t, OpenCodeGoProtocolMessages, protocol)

	protocol, ok = account.ResolveOpenCodeGoModelProtocol("qwen3.5-plus")
	require.True(t, ok)
	require.Equal(t, OpenCodeGoProtocolMessages, protocol)

	protocol, ok = account.ResolveOpenCodeGoModelProtocol("mimo-v2-pro")
	require.True(t, ok)
	require.Equal(t, OpenCodeGoProtocolChatCompletions, protocol)

	_, ok = account.ResolveOpenCodeGoModelProtocol("hy3-preview")
	require.False(t, ok, "official model list entries without known protocol metadata must not be guessed")
}

func TestOpenCodeGoAccountFamilyProtocolFallbackCoversKnownSeriesOnly(t *testing.T) {
	account := &Account{
		Platform: PlatformOpenCodeGo,
		Type:     AccountTypeAPIKey,
	}

	tests := []struct {
		model    string
		protocol string
	}{
		{model: "glm-6", protocol: OpenCodeGoProtocolChatCompletions},
		{model: "kimi-k2.8-code", protocol: OpenCodeGoProtocolChatCompletions},
		{model: "deepseek-v5-pro", protocol: OpenCodeGoProtocolChatCompletions},
		{model: "mimo-v3-omni", protocol: OpenCodeGoProtocolChatCompletions},
		{model: "qwen3.8-plus", protocol: OpenCodeGoProtocolMessages},
		{model: "minimax-m4", protocol: OpenCodeGoProtocolMessages},
	}
	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			protocol, ok := account.ResolveOpenCodeGoModelProtocol(tt.model)
			require.True(t, ok)
			require.Equal(t, tt.protocol, protocol)
		})
	}

	for _, model := range []string{"hy3-preview", "kimi-k3-preview", "qwen-plus", "minimax-next"} {
		t.Run("reject_"+model, func(t *testing.T) {
			_, ok := account.ResolveOpenCodeGoModelProtocol(model)
			require.False(t, ok)
		})
	}
}

func TestOpenCodeGoAccountCredentialProtocolsStillWinOverCatalog(t *testing.T) {
	account := &Account{
		Platform: PlatformOpenCodeGo,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"model_protocols": map[string]any{
				"glm-5.2": OpenCodeGoProtocolMessages,
			},
		},
	}

	protocol, ok := account.ResolveOpenCodeGoModelProtocol("glm-5.2")
	require.True(t, ok)
	require.Equal(t, OpenCodeGoProtocolMessages, protocol)
}

func TestOpenCodeGoIsAllowedQuotaPlatform(t *testing.T) {
	require.Contains(t, AllowedQuotaPlatforms, PlatformOpenCodeGo)
	require.True(t, IsAllowedQuotaPlatform(PlatformOpenCodeGo))
}

func TestOpenCodeGoOfficialUsageExhaustionMarksAPIKeyQuotaExceeded(t *testing.T) {
	resetAt := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)

	for _, window := range []string{"5h", "7d", "30d"} {
		t.Run(window, func(t *testing.T) {
			account := &Account{
				Platform:    PlatformOpenCodeGo,
				Type:        AccountTypeAPIKey,
				Status:      StatusActive,
				Schedulable: true,
				Extra: map[string]any{
					"opencode_go_usage_source":                               openCodeGoUsageSourceOfficialConsole,
					"opencode_go_console_auth_status":                        OpenCodeGoConsoleAuthStatusReady,
					fmt.Sprintf("opencode_go_usage_%s_used_percent", window): 100.0,
					fmt.Sprintf("opencode_go_usage_%s_resets_at", window):    resetAt,
				},
			}

			require.True(t, account.IsQuotaExceeded())
			require.False(t, account.IsSchedulable())
		})
	}
}

func TestOpenCodeGoOfficialUsageExhaustionIgnoresStaleOrNonOfficialSnapshots(t *testing.T) {
	futureReset := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	pastReset := time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)

	tests := []struct {
		name  string
		extra map[string]any
	}{
		{
			name: "estimated snapshot",
			extra: map[string]any{
				"opencode_go_usage_source":          openCodeGoUsageSourceEstimated,
				"opencode_go_usage_7d_used_percent": 100.0,
				"opencode_go_usage_7d_resets_at":    futureReset,
			},
		},
		{
			name: "expired reset",
			extra: map[string]any{
				"opencode_go_usage_source":          openCodeGoUsageSourceOfficialConsole,
				"opencode_go_console_auth_status":   OpenCodeGoConsoleAuthStatusReady,
				"opencode_go_usage_7d_used_percent": 100.0,
				"opencode_go_usage_7d_resets_at":    pastReset,
			},
		},
		{
			name: "console auth expired",
			extra: map[string]any{
				"opencode_go_usage_source":          openCodeGoUsageSourceOfficialConsole,
				"opencode_go_console_auth_status":   OpenCodeGoConsoleAuthStatusExpired,
				"opencode_go_usage_7d_used_percent": 100.0,
				"opencode_go_usage_7d_resets_at":    futureReset,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			account := &Account{
				Platform:    PlatformOpenCodeGo,
				Type:        AccountTypeAPIKey,
				Status:      StatusActive,
				Schedulable: true,
				Extra:       tt.extra,
			}

			require.False(t, account.IsQuotaExceeded())
			require.True(t, account.IsSchedulable())
		})
	}
}

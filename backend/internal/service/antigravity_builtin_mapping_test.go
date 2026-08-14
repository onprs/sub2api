//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveAntigravityModel_OfficialRoutesAndAccountBoundaries(t *testing.T) {
	tests := []struct {
		name       string
		account    *Account
		requested  string
		want       string
		wantMapped bool
	}{
		{
			name:       "oauth uses built-in pro high route without account mapping",
			account:    &Account{Platform: PlatformAntigravity, Type: AccountTypeOAuth},
			requested:  "gemini-3.1-pro-high",
			want:       "gemini-pro-agent",
			wantMapped: true,
		},
		{
			name:       "setup token uses built-in pro high route",
			account:    &Account{Platform: PlatformAntigravity, Type: AccountTypeSetupToken},
			requested:  "models/gemini-3.1-pro-high",
			want:       "gemini-pro-agent",
			wantMapped: true,
		},
		{
			name: "explicit oauth route overrides built-in route",
			account: &Account{Platform: PlatformAntigravity, Type: AccountTypeOAuth, Credentials: map[string]any{
				"model_mapping": map[string]any{"gemini-3.1-pro-high": "custom-pro"},
			}},
			requested:  "gemini-3.1-pro-high",
			want:       "custom-pro",
			wantMapped: true,
		},
		{
			name: "explicit wildcard overrides built-in exact route",
			account: &Account{Platform: PlatformAntigravity, Type: AccountTypeOAuth, Credentials: map[string]any{
				"model_mapping": map[string]any{"gemini-3.1-*": "custom-wildcard"},
			}},
			requested:  "gemini-3.1-pro-high",
			want:       "custom-wildcard",
			wantMapped: true,
		},
		{
			name:       "oauth keeps verified gemini 3.6 wire id",
			account:    &Account{Platform: PlatformAntigravity, Type: AccountTypeOAuth},
			requested:  "gemini-3.6-flash-medium",
			want:       "gemini-3.6-flash-medium",
			wantMapped: true,
		},
		{
			name:       "gemini 3.7 high uses public wire id",
			account:    &Account{Platform: PlatformAntigravity, Type: AccountTypeOAuth},
			requested:  "gemini-3.7-flash-high",
			want:       "gemini-3.7-flash-high",
			wantMapped: true,
		},
		{
			name:       "gemini 3.7 medium remains a distinct public wire id",
			account:    &Account{Platform: PlatformAntigravity, Type: AccountTypeOAuth},
			requested:  "gemini-3.7-flash-medium",
			want:       "gemini-3.7-flash-medium",
			wantMapped: true,
		},
		{
			name:       "gemini 3.7 low remains a distinct public wire id",
			account:    &Account{Platform: PlatformAntigravity, Type: AccountTypeOAuth},
			requested:  "gemini-3.7-flash-low",
			want:       "gemini-3.7-flash-low",
			wantMapped: true,
		},
		{
			name:       "internal gemini 3.7 fingerprint is never a wire alias",
			account:    &Account{Platform: PlatformAntigravity, Type: AccountTypeOAuth},
			requested:  "MODEL_PLACEHOLDER_M298",
			wantMapped: false,
		},
		{
			name:       "gemini 3.5 high resolves to historical agent wire",
			account:    &Account{Platform: PlatformAntigravity, Type: AccountTypeOAuth},
			requested:  "gemini-3.5-flash-high",
			want:       "gemini-3-flash-agent",
			wantMapped: true,
		},
		{
			name:       "gemini 3.5 medium resolves to historical low wire",
			account:    &Account{Platform: PlatformAntigravity, Type: AccountTypeOAuth},
			requested:  "gemini-3.5-flash-medium",
			want:       "gemini-3.5-flash-low",
			wantMapped: true,
		},
		{
			name:       "gemini 3.5 public low wins over colliding historical raw id",
			account:    &Account{Platform: PlatformAntigravity, Type: AccountTypeOAuth},
			requested:  "gemini-3.5-flash-low",
			want:       "gemini-3.5-flash-extra-low",
			wantMapped: true,
		},
		{
			name:       "oauth does not invent unverified bare gemini 3.6 route",
			account:    &Account{Platform: PlatformAntigravity, Type: AccountTypeOAuth},
			requested:  "gemini-3.6-flash",
			wantMapped: false,
		},
		{
			name:       "oauth does not infer opaque future catalog id",
			account:    &Account{Platform: PlatformAntigravity, Type: AccountTypeOAuth},
			requested:  "future_opaque_42",
			wantMapped: false,
		},
		{
			name: "oauth preserves explicitly routed opaque id",
			account: &Account{Platform: PlatformAntigravity, Type: AccountTypeOAuth, Credentials: map[string]any{
				"model_mapping": map[string]any{"future_opaque_42": "future_opaque_42"},
			}},
			requested:  "future_opaque_42",
			want:       "future_opaque_42",
			wantMapped: true,
		},
		{
			name:       "api key does not inherit official pro high route",
			account:    &Account{Platform: PlatformAntigravity, Type: AccountTypeAPIKey},
			requested:  "gemini-3.1-pro-high",
			wantMapped: false,
		},
		{
			name: "api key uses only explicit mapping without canonicalizing target",
			account: &Account{Platform: PlatformAntigravity, Type: AccountTypeAPIKey, Credentials: map[string]any{
				"model_mapping": map[string]any{"client-pro": "gemini-3.1-pro-high"},
			}},
			requested:  "client-pro",
			want:       "gemini-3.1-pro-high",
			wantMapped: true,
		},
		{
			name:       "upstream does not inherit official catalog",
			account:    &Account{Platform: PlatformAntigravity, Type: AccountTypeUpstream},
			requested:  "gemini-3.1-pro-high",
			wantMapped: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, mapped := tt.account.ResolveAntigravityModel(tt.requested)
			require.Equal(t, tt.wantMapped, mapped)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestResolveAntigravityRoute_SeparatesCanonicalSourceAndWire(t *testing.T) {
	account := &Account{Platform: PlatformAntigravity, Type: AccountTypeOAuth}

	route, ok := account.ResolveAntigravityRoute("gemini-3.5-flash-high")
	require.True(t, ok)
	require.Equal(t, "gemini-3.5-flash-high", route.ModelID)
	require.Equal(t, "gemini-3-flash-agent", route.WireModel)
	require.Equal(t, "MODEL_PLACEHOLDER_M84", route.InternalModel)

	route, ok = account.ResolveAntigravityRoute("gemini-pro-agent")
	require.True(t, ok)
	require.Equal(t, "gemini-3.1-pro-high", route.ModelID)
	require.Equal(t, "gemini-pro-agent", route.WireModel)

	route, ok = account.ResolveAntigravityRoute("gemini-3.7-flash-low")
	require.True(t, ok)
	require.Equal(t, "MODEL_PLACEHOLDER_M300", route.InternalModel)
	require.Equal(t, "gemini-3.7-flash", route.ResponseModel)
	require.Equal(t, 1000, route.ThinkingBudget)
}

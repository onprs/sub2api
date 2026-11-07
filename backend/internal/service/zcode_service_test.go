package service

import (
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/zcode"
	"github.com/stretchr/testify/require"
)

func TestZCodeValidationAndLegacyRegression(t *testing.T) {
	for _, mode := range []string{AccountModePayG, AccountModeCoding} {
		require.NoError(t, validateZCodeAccount(PlatformZhipu, AccountTypeAPIKey, map[string]any{"api_key": "synthetic-legacy", "account_mode": mode}))
		a := &Account{Platform: PlatformZhipu, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "synthetic-legacy", "account_mode": mode}}
		require.False(t, a.IsZCodeOAuth())
		require.Equal(t, mode, a.GetAccountMode())
		require.Equal(t, "synthetic-legacy", a.GetOpenAIProtocolAPIKey())
	}
	creds := map[string]any{"auth_mode": "zcode_oauth", "account_mode": AccountModeStart, "zcode_provider": "zai", zcodeTokensKey: "synthetic-cipher"}
	require.NoError(t, validateZCodeAccount(PlatformZhipu, AccountTypeOAuth, creds))
	a := &Account{Platform: PlatformZhipu, Type: AccountTypeOAuth, Credentials: creds}
	require.True(t, a.IsAnthropicProtocol())
	require.False(t, a.UsesOpenAICodexProtocol())
	require.Equal(t, AccountModeStart, a.GetAccountMode())
	require.Equal(t, zcode.StartBase, a.GetAnthropicProtocolBaseURL())
	creds["api_key"] = "synthetic-jwt"
	require.Error(t, validateZCodeAccount(PlatformZhipu, AccountTypeOAuth, creds))
	require.Error(t, validateZCodeAccount(PlatformKimi, AccountTypeOAuth, creds))
}

func TestZCodeRateLimitKeepsSchedulingUnlessQuotaExhausted(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	remaining, zero := 10.0, 0.0
	cases := []struct {
		name  string
		extra map[string]any
		want  bool
	}{
		{"无额度快照", nil, true},
		{"额度仍有剩余", map[string]any{zcodeQuotaKey: zcode.Quota{Plan: zcode.PlanStart, Status: "active", UpdatedAt: now.Unix(), Balances: []zcode.Balance{{Name: "GLM-5.3", Remaining: &remaining}}}}, true},
		{"额度已耗尽", map[string]any{zcodeQuotaKey: zcode.Quota{Plan: zcode.PlanStart, Status: "exhausted", UpdatedAt: now.Unix(), Balances: []zcode.Balance{{Name: "GLM-5.3", Remaining: &zero}}}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := &Account{Platform: PlatformZhipu, Type: AccountTypeOAuth, Extra: tc.extra}
			require.Equal(t, tc.want, zcodeRateLimitKeepsScheduling(a, now))
		})
	}
}

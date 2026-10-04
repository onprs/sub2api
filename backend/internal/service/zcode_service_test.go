package service

import (
	"testing"

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

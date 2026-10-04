package dto

import (
	"encoding/json"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestZCodeAccountResponseMasksEveryCredential(t *testing.T) {
	creds := map[string]any{"auth_mode": "zcode_oauth", "account_mode": "start", "zcode_provider": "zai", "zcode_tokens_encrypted": "synthetic-encrypted-token", "zcode_oauth_session_id": "synthetic-session", "jwt": "synthetic-jwt", "captcha_token": "synthetic-captcha", "poll_token": "synthetic-poll"}
	account := AccountFromServiceShallow(&service.Account{ID: 1, Platform: service.PlatformZhipu, Type: service.AccountTypeOAuth, Credentials: creds})
	raw, e := json.Marshal(account)
	require.NoError(t, e)
	for _, value := range []string{"synthetic-encrypted-token", "synthetic-session", "synthetic-jwt", "synthetic-captcha", "synthetic-poll"} {
		require.NotContains(t, string(raw), value)
	}
	require.True(t, account.CredentialsStatus["has_zcode_tokens_encrypted"])
	require.Equal(t, "zcode_oauth", account.Credentials["auth_mode"])
}

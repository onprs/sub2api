package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/imroc/req/v3"
	"github.com/stretchr/testify/require"
)

func TestDisableOpenAITrainingSendsAccountContext(t *testing.T) {
	var received bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received = true
		require.Equal(t, http.MethodPatch, r.Method)
		require.Equal(t, "Bearer access-token", r.Header.Get("Authorization"))
		require.Equal(t, "account-123", r.Header.Get("chatgpt-account-id"))
		require.Equal(t, "training_allowed", r.URL.Query().Get("feature"))
		require.Equal(t, "false", r.URL.Query().Get("value"))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	mode := disableOpenAITraining(
		context.Background(),
		func(proxyURL string) (*req.Client, error) {
			require.Equal(t, "http://proxy.example:8080", proxyURL)
			return newOpenAIPrivacyTestClient(server.URL), nil
		},
		"access-token",
		"http://proxy.example:8080",
		" account-123 ",
	)

	require.True(t, received)
	require.Equal(t, PrivacyModeTrainingOff, mode)
}

func TestDisableOpenAITrainingClassifiesChallengeHTML(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=UTF-8")
		w.Header().Set("Cf-Ray", "test-ray")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("<html><head><style>body{font-family:Arial}</style></head><body>Access denied</body></html>"))
	}))
	defer server.Close()

	mode := disableOpenAITraining(
		context.Background(),
		func(string) (*req.Client, error) { return newOpenAIPrivacyTestClient(server.URL), nil },
		"access-token",
		"",
		"account-123",
	)

	require.Equal(t, PrivacyModeCFBlocked, mode)
}

func TestDisableOpenAITrainingKeepsStructuredForbiddenAsFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"message":"forbidden"}}`))
	}))
	defer server.Close()

	mode := disableOpenAITraining(
		context.Background(),
		func(string) (*req.Client, error) { return newOpenAIPrivacyTestClient(server.URL), nil },
		"access-token",
		"",
		"account-123",
	)

	require.Equal(t, PrivacyModeFailed, mode)
}

func newOpenAIPrivacyTestClient(targetURL string) *req.Client {
	return req.C().OnBeforeRequest(func(_ *req.Client, request *req.Request) error {
		request.RawURL = targetURL
		return nil
	})
}

func TestResolveOpenAIPrivacyAccountID(t *testing.T) {
	t.Run("prefers chatgpt account id", func(t *testing.T) {
		account := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{
			"chatgpt_account_id": " account-123 ",
			"organization_id":    "org-456",
		}}
		require.Equal(t, "account-123", resolveOpenAIPrivacyAccountID(account))
	})

	t.Run("falls back to organization id", func(t *testing.T) {
		account := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{
			"organization_id": " org-456 ",
		}}
		require.Equal(t, "org-456", resolveOpenAIPrivacyAccountID(account))
	})
}

//go:build unit

package service

import (
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestAccountTestService_OpenCodeGoUsesModelsProbe(t *testing.T) {
	account := &Account{
		ID:       88,
		Name:     "opencode-go",
		Platform: PlatformOpenCodeGo,
		Type:     AccountTypeAPIKey,
		Status:   StatusActive,
		Credentials: map[string]any{
			"api_key":  "ocg-secret",
			"base_url": "https://opencode.ai/zen/go/v1",
		},
	}
	repo := &mockAccountRepoForGemini{
		accountsByID: map[int64]*Account{account.ID: account},
	}
	upstream := &queuedHTTPUpstream{
		responses: []*http.Response{
			newJSONResponse(http.StatusOK, `{"data":[{"id":"kimi-k2.7-code"}]}`),
		},
	}
	svc := &AccountTestService{
		accountRepo:  repo,
		httpUpstream: upstream,
		cfg: &config.Config{Security: config.SecurityConfig{
			URLAllowlist: config.URLAllowlistConfig{Enabled: false},
		}},
	}
	ctx, recorder := newTestContext()

	err := svc.TestAccountConnection(ctx, account.ID, "", "", AccountTestModeDefault)

	require.NoError(t, err)
	require.Len(t, upstream.requests, 1)
	req := upstream.requests[0]
	require.Equal(t, http.MethodGet, req.Method)
	require.Equal(t, "https://opencode.ai/zen/go/v1/models", req.URL.String())
	require.Equal(t, "Bearer ocg-secret", req.Header.Get("Authorization"))
	require.Contains(t, recorder.Body.String(), `"type":"test_complete"`)
	require.Contains(t, recorder.Body.String(), `"success":true`)
}

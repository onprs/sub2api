package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/stretchr/testify/require"
)

type mockHTTPUpstream struct {
	client *http.Client
}

func (m *mockHTTPUpstream) Do(req *http.Request, proxyURL string, accountID int64, concurrency int) (*http.Response, error) {
	return m.client.Do(req)
}

func (m *mockHTTPUpstream) DoWithTLS(req *http.Request, proxyURL string, accountID int64, concurrency int, profile *tlsfingerprint.Profile) (*http.Response, error) {
	return m.client.Do(req)
}

func TestOpenRouterFetchUsage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/key", r.URL.Path)
		require.Equal(t, "Bearer test-sk-key", r.Header.Get("Authorization"))

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"data": {
				"label": "my-test-key",
				"usage": 12.34,
				"limit": 50.0,
				"limit_remaining": 37.66,
				"is_free_tier": false,
				"rate_limit": {
					"requests": 500,
					"interval": "1h"
				}
			}
		}`))
	}))
	defer server.Close()

	cfg := &config.Config{
		Security: config.SecurityConfig{
			URLAllowlist: config.URLAllowlistConfig{
				Enabled:           false,
				AllowInsecureHTTP: true,
			},
		},
	}
	client := NewOpenRouterClient(&mockHTTPUpstream{client: server.Client()}, cfg, nil)
	account := &Account{
		ID:          10,
		Platform:    PlatformOpenRouter,
		Type:        AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "test-sk-key", "base_url": server.URL},
	}

	snapshot, err := client.FetchUsage(context.Background(), account)
	require.NoError(t, err)
	require.NotNil(t, snapshot)
	require.Equal(t, "my-test-key", snapshot.Label)
	require.Equal(t, 12.34, snapshot.UsageUSD)
	require.NotNil(t, snapshot.LimitUSD)
	require.Equal(t, 50.0, *snapshot.LimitUSD)
	require.NotNil(t, snapshot.LimitRemaining)
	require.Equal(t, 37.66, *snapshot.LimitRemaining)

	// Test extra conversion
	extra := buildOpenRouterUsageExtra(snapshot)
	require.Equal(t, "official_api", extra["openrouter_usage_source"])
	require.Equal(t, 12.34, extra["openrouter_usage_used_usd"])

	usageInfo := buildOpenRouterUsageFromExtra(extra, time.Now())
	require.NotNil(t, usageInfo)
	require.NotNil(t, usageInfo.ThirtyDay)
	require.InDelta(t, 24.68, usageInfo.ThirtyDay.Utilization, 0.01)
}

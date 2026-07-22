package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestAccountUsageServiceClinePassPersistsAuthoritativeWindowsAndKeepsLastGood(t *testing.T) {
	reset := time.Now().UTC().Add(2 * time.Hour).Truncate(time.Nanosecond)
	upstream := &clinePassHTTPUpstreamStub{response: clinePassTestResponse(http.StatusOK, `{
		"success":true,
		"data":{"limits":[
			{"type":"five_hour","percentUsed":45.5,"resetsAt":"`+reset.Format(time.RFC3339Nano)+`"},
			{"type":"monthly","percentUsed":12}
		]}
	}`)}
	repo := &accountUsageCodexProbeRepo{
		stubOpenAIAccountRepo: stubOpenAIAccountRepo{accounts: []Account{{
			ID: 91, Platform: PlatformClinePass, Type: AccountTypeAPIKey, Concurrency: 1,
			Credentials: map[string]any{"api_key": "cp-key", "base_url": DefaultClinePassBaseURL},
			Extra: map[string]any{
				"clinepass_usage_source":          clinePassUsageSourceOfficialAPI,
				"clinepass_usage_7d_used_percent": 88.0,
				"clinepass_usage_7d_resets_at":    reset.Add(time.Hour).Format(time.RFC3339Nano),
			},
		}}},
		updateExtraCh: make(chan map[string]any, 3),
	}
	client := NewClinePassClient(upstream, &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{Enabled: false}}}, nil)
	svc := &AccountUsageService{accountRepo: repo, clinePassClient: client}

	usage, err := svc.GetUsage(context.Background(), 91, true)
	require.NoError(t, err)
	require.Equal(t, 45.5, usage.FiveHour.Utilization)
	require.Nil(t, usage.SevenDay)
	require.Equal(t, 12.0, usage.ThirtyDay.Utilization)
	updates := <-repo.updateExtraCh
	require.Contains(t, updates, "clinepass_usage_7d_used_percent")
	require.Nil(t, updates["clinepass_usage_7d_used_percent"])
	require.Nil(t, updates["clinepass_usage_7d_resets_at"])

	upstream.response = &http.Response{
		StatusCode: http.StatusTooManyRequests,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(`{"success":false,"error":"temporarily rate limited"}`)),
	}
	usage, err = svc.GetUsage(context.Background(), 91, true)
	require.NoError(t, err)
	require.NotNil(t, usage.FiveHour)
	require.Equal(t, 45.5, usage.FiveHour.Utilization)
	require.Equal(t, "rate_limited", usage.ErrorCode)
	errorUpdate := <-repo.updateExtraCh
	require.Equal(t, "rate_limited", errorUpdate["clinepass_usage_auth_status"])
}

func TestClinePassUsageRateLimitResetUsesLatestWindowAndBoundedMissingReset(t *testing.T) {
	now := time.Now().UTC()
	account := &Account{
		Platform: PlatformClinePass,
		Type:     AccountTypeAPIKey,
		Extra: map[string]any{
			"clinepass_usage_source":           clinePassUsageSourceOfficialAPI,
			"clinepass_usage_updated_at":       now.Format(time.RFC3339Nano),
			"clinepass_usage_5h_used_percent":  100.0,
			"clinepass_usage_5h_resets_at":     now.Add(20 * time.Minute).Format(time.RFC3339Nano),
			"clinepass_usage_7d_used_percent":  100.0,
			"clinepass_usage_7d_resets_at":     now.Add(2 * time.Hour).Format(time.RFC3339Nano),
			"clinepass_usage_30d_used_percent": 100.0,
		},
	}

	reset := account.ClinePassOfficialUsageRateLimitResetAt(now)
	require.NotNil(t, reset)
	require.WithinDuration(t, now.Add(2*time.Hour), *reset, time.Second)
	require.True(t, account.IsQuotaExceeded())

	delete(account.Extra, "clinepass_usage_5h_resets_at")
	delete(account.Extra, "clinepass_usage_7d_resets_at")
	reset = account.ClinePassOfficialUsageRateLimitResetAt(now)
	require.NotNil(t, reset)
	require.WithinDuration(t, now.Add(clinePassMissingResetBackoff), *reset, time.Second)

	require.Nil(t, account.ClinePassOfficialUsageRateLimitResetAt(now.Add(clinePassMissingResetBackoff+time.Second)))
}

func TestBuildClinePassUsageFromExtraDoesNotShowExpiredReset(t *testing.T) {
	now := time.Now().UTC()
	usage := buildClinePassUsageFromExtra(map[string]any{
		"clinepass_usage_source":          clinePassUsageSourceOfficialAPI,
		"clinepass_usage_updated_at":      now.Add(-time.Hour).Format(time.RFC3339Nano),
		"clinepass_usage_5h_used_percent": 100.0,
		"clinepass_usage_5h_resets_at":    now.Add(-time.Minute).Format(time.RFC3339Nano),
	}, now)
	require.NotNil(t, usage)
	require.NotNil(t, usage.FiveHour)
	require.Zero(t, usage.FiveHour.Utilization)
	require.Nil(t, usage.FiveHour.ResetsAt)
}

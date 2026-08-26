package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/stretchr/testify/require"
)

type commandCodeHTTPUpstreamStub struct {
	responses map[string]*http.Response
	requests  []*http.Request
}

func (s *commandCodeHTTPUpstreamStub) DoWithTLS(req *http.Request, _ string, _ int64, _ int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	s.requests = append(s.requests, req)
	path := req.URL.Path
	if resp, ok := s.responses[path]; ok {
		return resp, nil
	}
	return &http.Response{
		StatusCode: http.StatusNotFound,
		Header:     make(http.Header),
		Body:       http.NoBody,
	}, nil
}

func (s *commandCodeHTTPUpstreamStub) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	return s.DoWithTLS(req, "", 0, 0, nil)
}

func commandCodeJSONResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestCommandCodeClientFetchUsageParsesWindowsAndSubscription(t *testing.T) {
	now := time.Now().UTC()
	fiveHourReset := now.Add(90 * time.Minute)
	weeklyReset := now.Add(3 * 24 * time.Hour)
	periodEnd := now.Add(20 * 24 * time.Hour)

	upstream := &commandCodeHTTPUpstreamStub{responses: map[string]*http.Response{
		"/alpha/whoami": commandCodeJSONResponse(http.StatusOK, `{"org":{"id":"org-cc-101"}}`),
		"/alpha/billing/credits": commandCodeJSONResponse(http.StatusOK, fmt.Sprintf(`{
			"credits": {
				"planId": "individual-goat",
				"monthlyCredits": 51.5,
				"purchasedCredits": 0,
				"freeCredits": 0,
				"windowLimits": {
					"limited": true,
					"fiveHour": {"used": 3.5, "cap": 14, "resetAt": %d},
					"weekly": {"used": 10.25, "cap": 35, "resetAt": %d}
				}
			}
		}`, fiveHourReset.UnixMilli(), weeklyReset.UnixMilli())),
		"/alpha/billing/subscriptions": commandCodeJSONResponse(http.StatusOK, fmt.Sprintf(`{
			"data": {
				"planId": "individual-goat",
				"status": "active",
				"currentPeriodStart": %q,
				"currentPeriodEnd": %q
			}
		}`, now.Add(-10*24*time.Hour).Format(time.RFC3339), periodEnd.Format(time.RFC3339))),
	}}
	account := &Account{
		ID: 101, Platform: PlatformCommandCode, Type: AccountTypeAPIKey, Concurrency: 1,
		Credentials: map[string]any{"api_key": "cc-key", "base_url": DefaultCommandCodeBaseURL},
	}
	client := NewCommandCodeClient(upstream, &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{Enabled: false}}}, nil)

	snapshot, err := client.FetchUsage(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, "individual-goat", snapshot.PlanID)
	require.Equal(t, 51.5, snapshot.MonthlyRemaining)
	require.Equal(t, 70.0, snapshot.PlanMonthlyCap)

	require.NotNil(t, snapshot.FiveHour)
	require.Equal(t, 3.5, snapshot.FiveHour.Used)
	require.Equal(t, 14.0, snapshot.FiveHour.Cap)
	require.NotNil(t, snapshot.FiveHour.ResetAt)
	require.WithinDuration(t, fiveHourReset, *snapshot.FiveHour.ResetAt, time.Second)

	require.NotNil(t, snapshot.Weekly)
	require.Equal(t, 10.25, snapshot.Weekly.Used)
	require.Equal(t, 35.0, snapshot.Weekly.Cap)

	require.NotNil(t, snapshot.PeriodEnd)
	require.WithinDuration(t, periodEnd, *snapshot.PeriodEnd, time.Second)

	// 鉴权与请求路径
	require.Len(t, upstream.requests, 3)
	require.Equal(t, "/alpha/whoami", upstream.requests[0].URL.Path)
	for _, req := range upstream.requests {
		require.Equal(t, "Bearer cc-key", req.Header.Get("Authorization"))
		if req.URL.Path == "/alpha/billing/credits" || req.URL.Path == "/alpha/billing/subscriptions" {
			require.Equal(t, "org-cc-101", req.URL.Query().Get("orgId"))
		}
	}
}

func TestCommandCodeClientFetchUsageToleratesMissingSubscription(t *testing.T) {
	upstream := &commandCodeHTTPUpstreamStub{responses: map[string]*http.Response{
		"/alpha/whoami": commandCodeJSONResponse(http.StatusOK, `{"org":{"id":"org-cc-102"}}`),
		"/alpha/billing/credits": commandCodeJSONResponse(http.StatusOK, `{
			"credits": {
				"planId": "individual-provider",
				"monthlyCredits": 12,
				"purchasedCredits": 5,
				"freeCredits": 0,
				"windowLimits": {"limited": false}
			}
		}`),
	}}
	account := &Account{
		ID: 102, Platform: PlatformCommandCode, Type: AccountTypeAPIKey, Concurrency: 1,
		Credentials: map[string]any{"api_key": "cc-key"},
	}
	client := NewCommandCodeClient(upstream, &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{Enabled: false}}}, nil)

	snapshot, err := client.FetchUsage(context.Background(), account)
	require.NoError(t, err)
	require.Nil(t, snapshot.FiveHour)
	require.Nil(t, snapshot.Weekly)
	require.Equal(t, 15.0, commandCodePlanMonthlyCredits["individual-provider"])
	require.Equal(t, 12.0, snapshot.MonthlyRemaining)
	require.Equal(t, 5.0, snapshot.PurchasedRemaining)
}

func TestAccountUsageServiceCommandCodeBuildsUsageInfo(t *testing.T) {
	now := time.Now().UTC()
	periodEnd := now.Add(20 * 24 * time.Hour).Truncate(time.Nanosecond)
	extra := map[string]any{
		"commandcode_usage_source":        commandCodeUsageSourceOfficialAPI,
		"commandcode_usage_updated_at":    now.Format(time.RFC3339Nano),
		"commandcode_usage_plan_id":       "individual-goat",
		"commandcode_usage_period_end":    periodEnd.Format(time.RFC3339Nano),
		"commandcode_usage_monthly_usd":   51.5,
		"commandcode_usage_purchased_usd": 0.0,
		"commandcode_usage_free_usd":      0.0,
		"commandcode_usage_balance_usd":   51.5,

		"commandcode_usage_5h_used_usd":     3.5,
		"commandcode_usage_5h_cap_usd":      14.0,
		"commandcode_usage_5h_used_percent": 25.0,
		"commandcode_usage_5h_resets_at":    now.Add(90 * time.Minute).Format(time.RFC3339Nano),

		"commandcode_usage_7d_used_usd":     10.25,
		"commandcode_usage_7d_cap_usd":      35.0,
		"commandcode_usage_7d_used_percent": 29.2857,

		"commandcode_usage_30d_used_usd":     18.5,
		"commandcode_usage_30d_cap_usd":      70.0,
		"commandcode_usage_30d_used_percent": 26.4286,
		"commandcode_usage_30d_resets_at":    periodEnd.Format(time.RFC3339Nano),
	}

	usage := buildCommandCodeUsageFromExtra(extra, now)
	require.NotNil(t, usage)
	require.NotNil(t, usage.FiveHour)
	require.InDelta(t, 25.0, usage.FiveHour.Utilization, 1e-9)
	require.NotNil(t, usage.FiveHour.ResetsAt)
	require.Equal(t, "$3.50 / $14.00", usage.FiveHour.SourceLabel)
	require.NotNil(t, usage.SevenDay)
	require.InDelta(t, 29.2857, usage.SevenDay.Utilization, 1e-3)
	require.NotNil(t, usage.ThirtyDay)
	require.InDelta(t, 26.4286, usage.ThirtyDay.Utilization, 1e-3)
	require.NotNil(t, usage.ThirtyDay.ResetsAt)
}

func TestAccountCommandCodeOfficialUsageRateLimitRequiresExhaustedBalance(t *testing.T) {
	now := time.Now().UTC()
	windowReset := now.Add(2 * time.Hour)
	periodEnd := now.Add(15 * 24 * time.Hour)

	// 窗口用满但存在充值余额：不限流。
	account := &Account{
		ID: 110, Platform: PlatformCommandCode, Type: AccountTypeAPIKey,
		Extra: map[string]any{
			"commandcode_usage_source":          commandCodeUsageSourceOfficialAPI,
			"commandcode_usage_5h_used_percent": 100.0,
			"commandcode_usage_5h_resets_at":    windowReset.Format(time.RFC3339Nano),
			"commandcode_usage_purchased_usd":   10.0,
			"commandcode_usage_free_usd":        0.0,
		},
	}
	require.Nil(t, account.CommandCodeOfficialUsageRateLimitResetAt(now))
	require.False(t, account.IsCommandCodeOfficialUsageExhausted())

	// 窗口用满且无充值余额：限流到窗口重置。
	account.Extra["commandcode_usage_purchased_usd"] = 0.0
	resetAt := account.CommandCodeOfficialUsageRateLimitResetAt(now)
	require.NotNil(t, resetAt)
	require.WithinDuration(t, windowReset, *resetAt, time.Second)

	// 月度额度耗尽且无充值余额：限流到订阅周期结束。
	account.Extra["commandcode_usage_5h_used_percent"] = 30.0
	account.Extra["commandcode_usage_monthly_usd"] = 0.0
	account.Extra["commandcode_usage_30d_used_percent"] = 100.0
	account.Extra["commandcode_usage_period_end"] = periodEnd.Format(time.RFC3339Nano)
	resetAt = account.CommandCodeOfficialUsageRateLimitResetAt(now)
	require.NotNil(t, resetAt)
	require.WithinDuration(t, periodEnd, *resetAt, time.Second)

	// 非 Command Code 账号不受影响。
	other := &Account{ID: 111, Platform: PlatformOpenRouter, Type: AccountTypeAPIKey, Extra: account.Extra}
	require.Nil(t, other.CommandCodeOfficialUsageRateLimitResetAt(now))
}

func TestParseCommandCodeRateLimitResetTime(t *testing.T) {
	reset := time.Now().Add(2 * time.Hour).Unix()
	ts := parseCommandCodeRateLimitResetTime([]byte(fmt.Sprintf(
		`{"error":{"code":"RATE_LIMITED","message":"You've reached your 5-hour usage limit.","rateLimit":{"window":"fiveHour","reset":%d}}}`,
		reset,
	)))
	require.NotNil(t, ts)
	require.Equal(t, reset, *ts)

	weekly := parseCommandCodeRateLimitResetTime([]byte(fmt.Sprintf(
		`{"error":{"rateLimit":{"window":"weekly","reset":%d}}}`, reset,
	)))
	require.NotNil(t, weekly)

	// 无窗口信息（普通上游限流）不返回重置时间。
	require.Nil(t, parseCommandCodeRateLimitResetTime([]byte(`{"error":{"code":"RATE_LIMITED","message":"Rate-limited by the upstream"}}`)))
	require.Nil(t, parseCommandCodeRateLimitResetTime([]byte(`{}`)))
}

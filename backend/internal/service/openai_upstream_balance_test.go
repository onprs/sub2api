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

func openAIUpstreamBalanceTestAccount(id int64, baseURL string) Account {
	return Account{
		ID:          id,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Concurrency: 2,
		Credentials: map[string]any{
			"api_key":  "sk-upstream-test",
			"base_url": baseURL,
		},
	}
}

func openAIUpstreamBalanceResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestAccountUsageServiceGetUsageOpenAIAPIKeyReadsSub2APIWalletBalance(t *testing.T) {
	t.Parallel()

	account := openAIUpstreamBalanceTestAccount(8101, "https://relay.example/v1")
	repo := &stubOpenAIAccountRepo{accounts: []Account{account}}
	upstream := &httpUpstreamRecorder{resp: openAIUpstreamBalanceResponse(http.StatusOK, `{
		"object":"sub2api.usage",
		"schema_version":1,
		"mode":"unrestricted",
		"isValid":true,
		"planName":"钱包余额",
		"remaining":12.34,
		"unit":"USD",
		"balance":12.34
	}`)}
	svc := &AccountUsageService{
		accountRepo:  repo,
		httpUpstream: upstream,
		cfg: &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{
			Enabled: false,
		}}},
	}

	usage, err := svc.GetUsage(context.Background(), account.ID)
	require.NoError(t, err)
	require.NotNil(t, usage)
	require.NotNil(t, usage.UpstreamBalance)
	require.Equal(t, openAIUpstreamBalanceStatusAvailable, usage.UpstreamBalance.Status)
	require.Equal(t, openAIUpstreamBalanceKindWallet, usage.UpstreamBalance.Kind)
	require.NotNil(t, usage.UpstreamBalance.Amount)
	require.InDelta(t, 12.34, *usage.UpstreamBalance.Amount, 1e-9)
	require.Equal(t, "sub2api", usage.UpstreamBalance.Source)
	require.NotNil(t, usage.UpstreamBalance.IsValid)
	require.True(t, *usage.UpstreamBalance.IsValid)

	require.NotNil(t, upstream.lastReq)
	require.Equal(t, http.MethodGet, upstream.lastReq.Method)
	require.Equal(t, "https", upstream.lastReq.URL.Scheme)
	require.Equal(t, "relay.example", upstream.lastReq.URL.Host)
	require.Equal(t, "/v1/usage", upstream.lastReq.URL.Path)
	require.NotNil(t, usage.UpdatedAt)
	require.Equal(t, "1", upstream.lastReq.URL.Query().Get("days"))
	require.Equal(t, "true", upstream.lastReq.URL.Query().Get("summary_only"))
	require.Equal(t, usage.UpdatedAt.Format(time.DateOnly), upstream.lastReq.URL.Query().Get("start_date"))
	require.Equal(t, usage.UpdatedAt.Format(time.DateOnly), upstream.lastReq.URL.Query().Get("end_date"))
	require.Equal(t, "Bearer sk-upstream-test", upstream.lastReq.Header.Get("Authorization"))
	require.Equal(t, "application/json", upstream.lastReq.Header.Get("Accept"))
	require.Equal(t, HTTPUpstreamProfileOpenAI, HTTPUpstreamProfileFromContext(upstream.lastReq.Context()))
	require.True(t, HTTPUpstreamRedirectsDisabled(upstream.lastReq.Context()))
}

func TestAccountUsageServiceGetUsageGrokAPIKeyReadsSub2APIWalletBalance(t *testing.T) {
	t.Parallel()

	account := Account{
		ID:          8103,
		Platform:    PlatformGrok,
		Type:        AccountTypeAPIKey,
		Concurrency: 2,
		Credentials: map[string]any{
			"api_key":  "xai-upstream-test",
			"base_url": "https://relay.example/v1",
		},
	}
	repo := &stubOpenAIAccountRepo{accounts: []Account{account}}
	upstream := &httpUpstreamRecorder{resp: openAIUpstreamBalanceResponse(http.StatusOK, `{
		"object":"sub2api.usage",
		"schema_version":1,
		"mode":"unrestricted",
		"isValid":true,
		"remaining":8.76,
		"unit":"USD",
		"balance":8.76
	}`)}
	svc := &AccountUsageService{
		accountRepo:  repo,
		httpUpstream: upstream,
		cfg: &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{
			Enabled: false,
		}}},
	}

	usage, err := svc.GetUsage(context.Background(), account.ID)
	require.NoError(t, err)
	require.NotNil(t, usage)
	require.NotNil(t, usage.UpstreamBalance)
	require.Equal(t, openAIUpstreamBalanceStatusAvailable, usage.UpstreamBalance.Status)
	require.Equal(t, openAIUpstreamBalanceKindWallet, usage.UpstreamBalance.Kind)
	require.NotNil(t, usage.UpstreamBalance.Amount)
	require.NotNil(t, upstream.lastReq)
	require.Equal(t, "https://relay.example/v1/usage", upstream.lastReq.URL.Scheme+"://"+upstream.lastReq.URL.Host+upstream.lastReq.URL.Path)
	require.Equal(t, "true", upstream.lastReq.URL.Query().Get("summary_only"))
	require.Equal(t, "Bearer xai-upstream-test", upstream.lastReq.Header.Get("Authorization"))
	require.Equal(t, HTTPUpstreamProfileGrok, HTTPUpstreamProfileFromContext(upstream.lastReq.Context()))
}

func TestAccountUsageServiceGetUsageGrokAPIKeySkipsOfficialXAI(t *testing.T) {
	t.Parallel()

	account := Account{
		ID:       8104,
		Platform: PlatformGrok,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":  "xai-official-test",
			"base_url": "https://api.x.ai/v1",
		},
	}
	upstream := &httpUpstreamRecorder{}
	svc := &AccountUsageService{
		accountRepo:  &stubOpenAIAccountRepo{accounts: []Account{account}},
		httpUpstream: upstream,
		cfg:          &config.Config{},
	}

	usage, err := svc.GetUsage(context.Background(), account.ID)
	require.NoError(t, err)
	require.NotNil(t, usage.UpstreamBalance)
	require.Equal(t, openAIUpstreamBalanceStatusUnsupported, usage.UpstreamBalance.Status)
	require.Nil(t, upstream.lastReq)
}

func TestAccountUsageServiceGetUsageOpenAIAPIKeyReadsQuotaRemainingAndUsesProxy(t *testing.T) {
	t.Parallel()

	proxyID := int64(91)
	account := openAIUpstreamBalanceTestAccount(8102, "https://relay.example")
	account.ProxyID = &proxyID
	account.Proxy = &Proxy{
		ID:       proxyID,
		Protocol: "http",
		Host:     "proxy.example",
		Port:     8080,
		Username: "proxy-user",
		Password: "proxy-pass",
	}
	account.Credentials["header_override_enabled"] = true
	account.Credentials["header_overrides"] = map[string]any{"x-relay-client": "balance-probe"}
	require.NoError(t, NormalizeHeaderOverrideCredentials(account.Credentials))

	repo := &stubOpenAIAccountRepo{accounts: []Account{account}}
	upstream := &httpUpstreamRecorder{resp: openAIUpstreamBalanceResponse(http.StatusOK, `{
		"mode":"quota_limited",
		"isValid":false,
		"status":"quota_exhausted",
		"remaining":0,
		"unit":"USD",
		"quota":{"limit":20,"used":20,"remaining":0,"unit":"USD"}
	}`)}
	svc := &AccountUsageService{
		accountRepo:  repo,
		httpUpstream: upstream,
		cfg: &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{
			Enabled: false,
		}}},
	}

	usage, err := svc.GetUsage(context.Background(), account.ID)
	require.NoError(t, err)
	require.NotNil(t, usage.UpstreamBalance)
	require.Equal(t, openAIUpstreamBalanceStatusAvailable, usage.UpstreamBalance.Status)
	require.Equal(t, openAIUpstreamBalanceKindAPIKeyQuota, usage.UpstreamBalance.Kind)
	require.NotNil(t, usage.UpstreamBalance.Amount)
	require.Zero(t, *usage.UpstreamBalance.Amount)
	require.NotNil(t, usage.UpstreamBalance.Limit)
	require.Equal(t, 20.0, *usage.UpstreamBalance.Limit)
	require.NotNil(t, usage.UpstreamBalance.Used)
	require.Equal(t, 20.0, *usage.UpstreamBalance.Used)
	require.NotNil(t, usage.UpstreamBalance.IsValid)
	require.False(t, *usage.UpstreamBalance.IsValid)
	require.Equal(t, "quota_exhausted", usage.UpstreamBalance.RemoteStatus)
	require.Equal(t, "http://proxy-user:proxy-pass@proxy.example:8080", upstream.lastProxyURL)
	var overrideValues []string
	for name, values := range upstream.lastReq.Header {
		if strings.EqualFold(name, "x-relay-client") {
			overrideValues = values
			break
		}
	}
	require.Equal(t, []string{"balance-probe"}, overrideValues)
}

func TestAccountUsageServiceGetUsageOpenAIAPIKeyUnsupportedResponsesAreSilent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status int
		body   string
	}{
		{name: "not found", status: http.StatusNotFound, body: `{"detail":"not found"}`},
		{name: "method not allowed", status: http.StatusMethodNotAllowed, body: `{"detail":"not allowed"}`},
		{name: "unrelated success body", status: http.StatusOK, body: `{"mode":"unrestricted","balance":100}`},
		{name: "invalid json", status: http.StatusOK, body: `<html>login</html>`},
	}

	for index, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			account := openAIUpstreamBalanceTestAccount(int64(8200+index), "https://compat.example/v1")
			svc := &AccountUsageService{
				accountRepo:  &stubOpenAIAccountRepo{accounts: []Account{account}},
				httpUpstream: &httpUpstreamRecorder{resp: openAIUpstreamBalanceResponse(tt.status, tt.body)},
				cfg: &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{
					Enabled: false,
				}}},
			}

			usage, err := svc.GetUsage(context.Background(), account.ID)
			require.NoError(t, err)
			require.NotNil(t, usage.UpstreamBalance)
			require.Equal(t, openAIUpstreamBalanceStatusUnsupported, usage.UpstreamBalance.Status)
			require.Nil(t, usage.UpstreamBalance.Amount)
		})
	}
}

func TestAccountUsageServiceGetUsageOpenAIAPIKeyReturnsSanitizedErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		status    int
		wantError string
	}{
		{name: "unauthorized", status: http.StatusUnauthorized, wantError: "unauthorized"},
		{name: "forbidden", status: http.StatusForbidden, wantError: "unauthorized"},
		{name: "rate limited", status: http.StatusTooManyRequests, wantError: "rate_limited"},
		{name: "upstream error", status: http.StatusBadGateway, wantError: "http_error"},
	}

	for index, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			account := openAIUpstreamBalanceTestAccount(int64(8300+index), "https://relay.example")
			secretBody := `{"error":{"message":"invalid sk-secret-upstream-token"}}`
			svc := &AccountUsageService{
				accountRepo:  &stubOpenAIAccountRepo{accounts: []Account{account}},
				httpUpstream: &httpUpstreamRecorder{resp: openAIUpstreamBalanceResponse(tt.status, secretBody)},
				cfg: &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{
					Enabled: false,
				}}},
			}

			usage, err := svc.GetUsage(context.Background(), account.ID)
			require.NoError(t, err)
			require.Equal(t, openAIUpstreamBalanceStatusError, usage.UpstreamBalance.Status)
			require.Equal(t, tt.wantError, usage.UpstreamBalance.ErrorCode)
			require.NotContains(t, usage.UpstreamBalance.ErrorCode, "secret")
		})
	}
}

func TestAccountUsageServiceGetUsageAnthropicAPIKeyReadsSub2APIWalletBalance(t *testing.T) {
	t.Parallel()

	account := Account{
		ID:          8501,
		Platform:    PlatformAnthropic,
		Type:        AccountTypeAPIKey,
		Concurrency: 2,
		Credentials: map[string]any{
			"api_key":  "sk-anthropic-sub2api",
			"base_url": "https://relay.example.com/v1",
		},
	}
	upstream := &httpUpstreamRecorder{resp: openAIUpstreamBalanceResponse(http.StatusOK, `{
		"object":"sub2api.usage",
		"schema_version":1,
		"mode":"unrestricted",
		"isValid":true,
		"remaining":7.65,
		"unit":"USD",
		"balance":7.65
	}`)}
	svc := &AccountUsageService{
		accountRepo:  &stubOpenAIAccountRepo{accounts: []Account{account}},
		httpUpstream: upstream,
		cfg: &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{
			Enabled:                         true,
			UpstreamHosts:                   []string{"api.anthropic.com"},
			AllowAnthropicAPIKeyCustomHosts: true,
		}}},
	}

	usage, err := svc.GetUsage(context.Background(), account.ID)
	require.NoError(t, err)
	require.NotNil(t, usage.UpstreamBalance)
	require.Equal(t, openAIUpstreamBalanceStatusAvailable, usage.UpstreamBalance.Status)
	require.Equal(t, openAIUpstreamBalanceKindWallet, usage.UpstreamBalance.Kind)
	require.NotNil(t, usage.UpstreamBalance.Amount)
	require.InDelta(t, 7.65, *usage.UpstreamBalance.Amount, 1e-9)
	require.Equal(t, "https://relay.example.com/v1/usage", upstream.lastReq.URL.Scheme+"://"+upstream.lastReq.URL.Host+upstream.lastReq.URL.Path)
	require.Equal(t, "1", upstream.lastReq.URL.Query().Get("days"))
	require.Equal(t, "true", upstream.lastReq.URL.Query().Get("summary_only"))
	require.Equal(t, "Bearer sk-anthropic-sub2api", upstream.lastReq.Header.Get("Authorization"))
	require.True(t, HTTPUpstreamRedirectsDisabled(upstream.lastReq.Context()))
}

func TestAccountUsageServiceGetUsageAnthropicAPIKeyRespectsStrictCustomHostPolicy(t *testing.T) {
	t.Parallel()

	account := Account{
		ID:          8502,
		Platform:    PlatformAnthropic,
		Type:        AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "sk-anthropic", "base_url": "https://relay.example.com/v1"},
	}
	upstream := &httpUpstreamRecorder{}
	svc := &AccountUsageService{
		accountRepo:  &stubOpenAIAccountRepo{accounts: []Account{account}},
		httpUpstream: upstream,
		cfg: &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{
			Enabled:                         true,
			UpstreamHosts:                   []string{"api.anthropic.com"},
			AllowAnthropicAPIKeyCustomHosts: false,
		}}},
	}

	usage, err := svc.GetUsage(context.Background(), account.ID)
	require.NoError(t, err)
	require.NotNil(t, usage.UpstreamBalance)
	require.Equal(t, openAIUpstreamBalanceStatusError, usage.UpstreamBalance.Status)
	require.Equal(t, "invalid_base_url", usage.UpstreamBalance.ErrorCode)
	require.Nil(t, upstream.lastReq)
}

func TestAccountUsageServiceGetUsageAnthropicAPIKeySkipsOfficialAnthropic(t *testing.T) {
	t.Parallel()

	account := Account{
		ID:       8503,
		Platform: PlatformAnthropic,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":  "sk-anthropic-official",
			"base_url": "https://api.anthropic.com/v1",
		},
	}
	upstream := &httpUpstreamRecorder{}
	svc := &AccountUsageService{
		accountRepo:  &stubOpenAIAccountRepo{accounts: []Account{account}},
		httpUpstream: upstream,
		cfg:          &config.Config{},
	}

	usage, err := svc.GetUsage(context.Background(), account.ID)
	require.NoError(t, err)
	require.NotNil(t, usage.UpstreamBalance)
	require.Equal(t, openAIUpstreamBalanceStatusUnsupported, usage.UpstreamBalance.Status)
	require.Nil(t, upstream.lastReq)
}

func TestAccountUsageServiceGetUsageOpenAIAPIKeySkipsOfficialOpenAI(t *testing.T) {
	t.Parallel()

	account := openAIUpstreamBalanceTestAccount(8401, "https://api.openai.com/v1")
	upstream := &httpUpstreamRecorder{}
	svc := &AccountUsageService{
		accountRepo:  &stubOpenAIAccountRepo{accounts: []Account{account}},
		httpUpstream: upstream,
		cfg:          &config.Config{},
	}

	usage, err := svc.GetUsage(context.Background(), account.ID)
	require.NoError(t, err)
	require.Equal(t, openAIUpstreamBalanceStatusUnsupported, usage.UpstreamBalance.Status)
	require.Nil(t, upstream.lastReq)
}

func TestParseOpenAIUpstreamUsageBalanceAcceptsLegacySub2APIAndSubscriptionWithoutFiniteAmount(t *testing.T) {
	t.Parallel()

	wallet, ok := parseOpenAIUpstreamUsageBalance([]byte(`{
		"mode":"unrestricted","isValid":true,"unit":"USD","balance":0
	}`), testNowUTC())
	require.True(t, ok)
	require.Equal(t, openAIUpstreamBalanceKindWallet, wallet.Kind)
	require.NotNil(t, wallet.Amount)
	require.Zero(t, *wallet.Amount)

	subscription, ok := parseOpenAIUpstreamUsageBalance([]byte(`{
		"object":"sub2api.usage","schema_version":1,"mode":"unrestricted",
		"isValid":true,"unit":"USD","remaining":-1,"subscription":{}
	}`), testNowUTC())
	require.True(t, ok)
	require.Equal(t, openAIUpstreamBalanceKindSubscription, subscription.Kind)
	require.Nil(t, subscription.Amount)
}

func testNowUTC() (now time.Time) {
	return time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
}

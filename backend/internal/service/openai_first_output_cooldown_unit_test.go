//go:build unit

package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type firstOutputCooldownCacheRecorder struct {
	calls     int
	accountID int64
	state     *TempUnschedState
}

func (r *firstOutputCooldownCacheRecorder) SetTempUnsched(_ context.Context, accountID int64, state *TempUnschedState) error {
	r.calls++
	r.accountID = accountID
	r.state = state
	return nil
}

func (r *firstOutputCooldownCacheRecorder) GetTempUnsched(context.Context, int64) (*TempUnschedState, error) {
	return nil, nil
}

func (r *firstOutputCooldownCacheRecorder) DeleteTempUnsched(context.Context, int64) error {
	return nil
}

func TestOpenAIFirstOutputTimeoutImmediatelyCoolsConfiguredAccount(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &rateLimitAccountRepoStub{}
	cache := &firstOutputCooldownCacheRecorder{}
	blocker := &runtimeBlockRecorder{}
	rateLimits := NewRateLimitService(repo, nil, &config.Config{}, nil, cache)
	rateLimits.SetAccountRuntimeBlocker(blocker)
	gateway := &OpenAIGatewayService{rateLimitService: rateLimits}

	timeoutSeconds := 12
	cooldownMinutes := 10
	account := &Account{
		ID:                                 7101,
		Name:                               "configured-openai-apikey",
		Platform:                           PlatformOpenAI,
		Type:                               AccountTypeAPIKey,
		FirstOutputFailoverTimeoutSeconds:  &timeoutSeconds,
		FirstOutputFailoverCooldownMinutes: &cooldownMinutes,
	}
	recorder := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(recorder)
	requestCtx, cancel := context.WithCancel(context.Background())
	cancel()

	before := time.Now()
	failoverErr := gateway.newOpenAIFirstOutputTimeoutError(
		requestCtx,
		ginCtx,
		account,
		before.Add(-12*time.Second),
		"gpt-test",
		"medium",
		12*time.Second,
		"response_headers",
		http.Header{},
	)

	require.Equal(t, http.StatusGatewayTimeout, failoverErr.StatusCode)
	require.True(t, failoverErr.SafeToFailoverAfterWrite)
	require.Equal(t, 1, repo.tempCalls)
	require.Equal(t, account.ID, repo.lastTempID)
	require.NoError(t, repo.lastTempContextErr)
	require.WithinDuration(t, before.Add(10*time.Minute), repo.lastTempUntil, 2*time.Second)
	require.Contains(t, repo.lastTempReason, `"matched_keyword":"first_output_timeout"`)
	require.Contains(t, repo.lastTempReason, "gpt-test")

	require.Equal(t, 1, cache.calls)
	require.Equal(t, account.ID, cache.accountID)
	require.NotNil(t, cache.state)
	require.Equal(t, http.StatusGatewayTimeout, cache.state.StatusCode)
	require.Equal(t, "first_output_timeout", cache.state.MatchedKeyword)

	require.Len(t, blocker.accounts, 1)
	require.Equal(t, account.ID, blocker.accounts[0].ID)
	require.Equal(t, "openai_first_output_timeout_cooldown", blocker.reasons[0])
	require.WithinDuration(t, repo.lastTempUntil, blocker.until[0], time.Second)
}

func TestHandleFirstOutputFailoverTimeoutBlocksOpenAISchedulerImmediately(t *testing.T) {
	repo := &rateLimitAccountRepoStub{}
	rateLimits := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
	gateway := &OpenAIGatewayService{rateLimitService: rateLimits}
	rateLimits.SetAccountRuntimeBlocker(gateway)
	account := &Account{ID: 7101, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}

	handled := rateLimits.HandleFirstOutputFailoverTimeout(
		context.Background(), account, "gpt-test", 10*time.Minute,
	)

	require.True(t, handled)
	require.True(t, gateway.isOpenAIAccountRuntimeBlocked(account))
	require.Equal(t, 1, repo.tempCalls)
}

func TestOpenAIUpstreamGatewayFailuresImmediatelyCoolConfiguredPoolAccount(t *testing.T) {
	for _, statusCode := range []int{
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout,
	} {
		t.Run(http.StatusText(statusCode), func(t *testing.T) {
			repo := &rateLimitAccountRepoStub{}
			cache := &firstOutputCooldownCacheRecorder{}
			rateLimits := NewRateLimitService(repo, nil, &config.Config{}, nil, cache)
			gateway := &OpenAIGatewayService{rateLimitService: rateLimits}
			rateLimits.SetAccountRuntimeBlocker(gateway)

			timeoutSeconds := 15
			cooldownMinutes := 30
			account := &Account{
				ID:                                 7101,
				Platform:                           PlatformOpenAI,
				Type:                               AccountTypeAPIKey,
				Credentials:                        map[string]any{"pool_mode": true},
				FirstOutputFailoverTimeoutSeconds:  &timeoutSeconds,
				FirstOutputFailoverCooldownMinutes: &cooldownMinutes,
			}
			requestCtx, cancel := context.WithCancel(context.Background())
			cancel()

			before := time.Now()
			shouldDisable := gateway.handleOpenAIAccountUpstreamError(
				requestCtx,
				account,
				statusCode,
				http.Header{},
				[]byte(`{"error":{"message":"temporary gateway failure"}}`),
				"gpt-test",
			)

			require.True(t, shouldDisable)
			require.Equal(t, 1, repo.tempCalls)
			require.Equal(t, account.ID, repo.lastTempID)
			require.NoError(t, repo.lastTempContextErr)
			require.WithinDuration(t, before.Add(30*time.Minute), repo.lastTempUntil, 2*time.Second)
			require.Contains(t, repo.lastTempReason, `"status_code":`+strconv.Itoa(statusCode))
			require.Contains(t, repo.lastTempReason, `"matched_keyword":"upstream_http_`+strconv.Itoa(statusCode)+`"`)

			require.Equal(t, 1, cache.calls)
			require.NotNil(t, cache.state)
			require.Equal(t, statusCode, cache.state.StatusCode)
			require.True(t, gateway.isOpenAIAccountRuntimeBlocked(account))
		})
	}
}

func TestOpenAIUpstreamGatewayFailureCooldownRequiresExplicitSupportedConfiguration(t *testing.T) {
	timeoutSeconds := 15
	cooldownMinutes := 30
	tests := []struct {
		name       string
		statusCode int
		account    *Account
	}{
		{
			name:       "未配置冷却",
			statusCode: http.StatusGatewayTimeout,
			account: &Account{
				ID:                                7102,
				Platform:                          PlatformOpenAI,
				Type:                              AccountTypeAPIKey,
				FirstOutputFailoverTimeoutSeconds: &timeoutSeconds,
			},
		},
		{
			name:       "非目标状态码",
			statusCode: http.StatusInternalServerError,
			account: &Account{
				ID:                                 7103,
				Platform:                           PlatformOpenAI,
				Type:                               AccountTypeAPIKey,
				FirstOutputFailoverTimeoutSeconds:  &timeoutSeconds,
				FirstOutputFailoverCooldownMinutes: &cooldownMinutes,
			},
		},
		{
			name:       "非 API Key 账号",
			statusCode: http.StatusServiceUnavailable,
			account: &Account{
				ID:                                 7104,
				Platform:                           PlatformOpenAI,
				Type:                               AccountTypeOAuth,
				FirstOutputFailoverTimeoutSeconds:  &timeoutSeconds,
				FirstOutputFailoverCooldownMinutes: &cooldownMinutes,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &rateLimitAccountRepoStub{}
			rateLimits := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
			gateway := &OpenAIGatewayService{rateLimitService: rateLimits}
			rateLimits.SetAccountRuntimeBlocker(gateway)

			shouldDisable := gateway.handleOpenAIAccountUpstreamError(
				context.Background(), tt.account, tt.statusCode, http.Header{}, nil, "gpt-test",
			)

			require.False(t, shouldDisable)
			require.Zero(t, repo.tempCalls)
			require.False(t, gateway.isOpenAIAccountRuntimeBlocked(tt.account))
		})
	}
}

func TestOpenAIFirstOutputTimeoutWithoutAccountCooldownKeepsLegacyHandling(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &rateLimitAccountRepoStub{}
	blocker := &runtimeBlockRecorder{}
	rateLimits := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
	rateLimits.SetAccountRuntimeBlocker(blocker)
	gateway := &OpenAIGatewayService{rateLimitService: rateLimits}

	timeoutSeconds := 12
	account := &Account{
		ID:                                7105,
		Platform:                          PlatformOpenAI,
		Type:                              AccountTypeAPIKey,
		FirstOutputFailoverTimeoutSeconds: &timeoutSeconds,
	}
	ginCtx, _ := gin.CreateTestContext(httptest.NewRecorder())

	failoverErr := gateway.newOpenAIFirstOutputTimeoutError(
		context.Background(), ginCtx, account, time.Now().Add(-12*time.Second),
		"gpt-test", "medium", 12*time.Second, "semantic_output", http.Header{},
	)

	require.Equal(t, http.StatusGatewayTimeout, failoverErr.StatusCode)
	require.Zero(t, repo.tempCalls)
	require.Empty(t, blocker.accounts)
}

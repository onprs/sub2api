//go:build unit

package service

import (
	"context"
	"net/http"
	"net/http/httptest"
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
		ID:                                 5338,
		Name:                               "low-cost-primary",
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
	account := &Account{ID: 5338, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}

	handled := rateLimits.HandleFirstOutputFailoverTimeout(
		context.Background(), account, "gpt-test", 10*time.Minute,
	)

	require.True(t, handled)
	require.True(t, gateway.isOpenAIAccountRuntimeBlocked(account))
	require.Equal(t, 1, repo.tempCalls)
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
		ID:                                5343,
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

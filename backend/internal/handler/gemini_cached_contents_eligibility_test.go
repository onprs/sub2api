//go:build unit

package handler

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type cachedContentEligibilityFunc func(context.Context, int64, int64, *int64, string) (*service.UserSubscription, error)

func (f cachedContentEligibilityFunc) ResolveUsableSubscriptionForRequest(ctx context.Context, userID, apiKeyID int64, groupID *int64, platform string) (*service.UserSubscription, error) {
	return f(ctx, userID, apiKeyID, groupID, platform)
}

func TestGeminiCachedContentsCreate_RevalidatesAfterConcurrencySlots(t *testing.T) {
	for _, tc := range []struct {
		name   string
		failAt int
	}{
		{name: "用户槽之后", failAt: 1},
		{name: "账号槽之后", failAt: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := newCachedBillingEnv(t, 500, 900)
			checks := 0
			env.h.billingEligibilityService = cachedContentEligibilityFunc(func(_ context.Context, userID, apiKeyID int64, groupID *int64, platform string) (*service.UserSubscription, error) {
				checks++
				require.Equal(t, env.apiKey.UserID, userID)
				require.Equal(t, env.apiKey.ID, apiKeyID)
				require.Equal(t, env.apiKey.GroupID, groupID)
				require.Equal(t, service.PlatformGemini, platform)
				if checks == tc.failAt {
					return nil, service.ErrBillingServiceUnavailable
				}
				return nil, nil
			})

			rec := env.create()
			require.Equal(t, http.StatusServiceUnavailable, rec.Code, rec.Body.String())
			require.Equal(t, tc.failAt, checks)
			require.Zero(t, env.upstream.methodCalls(http.MethodPost))
			require.Empty(t, env.repo.records)
			require.Empty(t, env.usage.totals())
		})
	}
}

func TestGeminiCachedContentsCreate_RejectsMissingBillingEligibility(t *testing.T) {
	env := newCachedBillingEnv(t, 500, 900)
	env.h.billingEligibilityService = nil
	rec := env.create()
	require.Equal(t, http.StatusServiceUnavailable, rec.Code, rec.Body.String())
	require.Zero(t, env.upstream.methodCalls(http.MethodPost))
	require.Empty(t, env.usage.totals())
}

func TestGeminiCachedContentsPatch_RevalidatesBeforeExtensionCharge(t *testing.T) {
	env := newCachedBillingEnv(t, 500, 900)
	env.seed(time.Now().Add(time.Hour))
	env.h.billingEligibilityService = cachedContentEligibilityFunc(func(context.Context, int64, int64, *int64, string) (*service.UserSubscription, error) {
		return nil, service.ErrBillingServiceUnavailable
	})
	rec := env.patch()
	require.Equal(t, http.StatusServiceUnavailable, rec.Code, rec.Body.String())
	require.Zero(t, env.upstream.methodCalls(http.MethodPatch))
	require.False(t, env.repo.pendingMutation(100))
	require.Empty(t, env.usage.totals())
}

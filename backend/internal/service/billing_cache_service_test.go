package service

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

type billingCacheWorkerStub struct {
	balanceUpdates        int64
	subscriptionUpdates   int64
	subscriptionData      *SubscriptionCacheData
	subscriptionGetErr    error
	subscriptionUpdateErr error
	lastSubscriptionSet   *SubscriptionCacheData
}

func (b *billingCacheWorkerStub) GetUserBalance(ctx context.Context, userID int64) (float64, error) {
	return 0, errors.New("not implemented")
}

func (b *billingCacheWorkerStub) SetUserBalance(ctx context.Context, userID int64, balance float64) error {
	atomic.AddInt64(&b.balanceUpdates, 1)
	return nil
}

func (b *billingCacheWorkerStub) DeductUserBalance(ctx context.Context, userID int64, amount float64) error {
	atomic.AddInt64(&b.balanceUpdates, 1)
	return nil
}

func (b *billingCacheWorkerStub) InvalidateUserBalance(ctx context.Context, userID int64) error {
	return nil
}

func (b *billingCacheWorkerStub) GetSubscriptionCache(ctx context.Context, userID, groupID int64) (*SubscriptionCacheData, error) {
	if b.subscriptionGetErr != nil {
		return nil, b.subscriptionGetErr
	}
	if b.subscriptionData != nil {
		return b.subscriptionData, nil
	}
	return nil, errors.New("not implemented")
}

func (b *billingCacheWorkerStub) SetSubscriptionCache(ctx context.Context, userID, groupID int64, data *SubscriptionCacheData) error {
	atomic.AddInt64(&b.subscriptionUpdates, 1)
	if data != nil {
		cp := *data
		b.lastSubscriptionSet = &cp
	}
	return nil
}

func (b *billingCacheWorkerStub) UpdateSubscriptionUsage(ctx context.Context, userID, groupID int64, cost float64) error {
	atomic.AddInt64(&b.subscriptionUpdates, 1)
	return b.subscriptionUpdateErr
}

func (b *billingCacheWorkerStub) InvalidateSubscriptionCache(ctx context.Context, userID, groupID int64) error {
	return nil
}

func (b *billingCacheWorkerStub) GetAPIKeyRateLimit(ctx context.Context, keyID int64) (*APIKeyRateLimitCacheData, error) {
	return nil, errors.New("not implemented")
}

func (b *billingCacheWorkerStub) SetAPIKeyRateLimit(ctx context.Context, keyID int64, data *APIKeyRateLimitCacheData) error {
	return nil
}

func (b *billingCacheWorkerStub) UpdateAPIKeyRateLimitUsage(ctx context.Context, keyID int64, cost float64) error {
	return nil
}

func (b *billingCacheWorkerStub) InvalidateAPIKeyRateLimit(ctx context.Context, keyID int64) error {
	return nil
}

func (b *billingCacheWorkerStub) GetUserPlatformQuotaCache(ctx context.Context, userID int64, platform string) (*UserPlatformQuotaCacheEntry, bool, error) {
	return nil, false, nil
}

func (b *billingCacheWorkerStub) SetUserPlatformQuotaCache(ctx context.Context, userID int64, platform string, entry *UserPlatformQuotaCacheEntry, ttl time.Duration) error {
	return nil
}

func (b *billingCacheWorkerStub) DeleteUserPlatformQuotaCache(ctx context.Context, userID int64, platform string) error {
	return nil
}

func (b *billingCacheWorkerStub) IncrUserPlatformQuotaUsageCache(ctx context.Context, userID int64, platform string, cost float64, ttl time.Duration, markDirty bool) error {
	return nil
}

func (b *billingCacheWorkerStub) PopDirtyUserPlatformQuotaKeys(ctx context.Context, n int) ([]UserPlatformQuotaKey, error) {
	return nil, nil
}

func (b *billingCacheWorkerStub) ReaddDirtyUserPlatformQuotaKeys(ctx context.Context, keys []UserPlatformQuotaKey) error {
	return nil
}

func (b *billingCacheWorkerStub) BatchGetUserPlatformQuotaCache(ctx context.Context, keys []UserPlatformQuotaKey) ([]*UserPlatformQuotaCacheEntry, error) {
	return nil, nil
}

func TestBillingCacheServiceQueueHighLoad(t *testing.T) {
	cache := &billingCacheWorkerStub{}
	svc := NewBillingCacheService(cache, nil, nil, nil, nil, nil, &config.Config{}, nil)
	t.Cleanup(svc.Stop)

	start := time.Now()
	for i := 0; i < cacheWriteBufferSize*2; i++ {
		svc.QueueDeductBalance(1, 1)
	}
	require.Less(t, time.Since(start), 2*time.Second)

	svc.QueueUpdateSubscriptionUsage(1, 2, 1.5)

	require.Eventually(t, func() bool {
		return atomic.LoadInt64(&cache.balanceUpdates) > 0
	}, 2*time.Second, 10*time.Millisecond)

	require.Eventually(t, func() bool {
		return atomic.LoadInt64(&cache.subscriptionUpdates) > 0
	}, 2*time.Second, 10*time.Millisecond)
}

func TestBillingCacheServiceGetSubscriptionStatusSeedsCacheSynchronouslyOnMiss(t *testing.T) {
	limit := 5.0
	windowStart := time.Now().Add(-time.Hour)
	subRepo := newSubscriptionUserSubRepoStub()
	subRepo.seed(&UserSubscription{
		ID:                  10,
		UserID:              100,
		GroupID:             200,
		Status:              SubscriptionStatusActive,
		ExpiresAt:           time.Now().Add(24 * time.Hour),
		FiveHourLimitUSD:    &limit,
		FiveHourUsageUSD:    1.25,
		FiveHourWindowStart: &windowStart,
		UpdatedAt:           time.Now(),
	})
	cache := &billingCacheWorkerStub{}
	svc := NewBillingCacheService(cache, nil, subRepo, nil, nil, nil, &config.Config{}, nil)
	svc.Stop()

	data, err := svc.GetSubscriptionStatus(context.Background(), 100, 200)

	require.NoError(t, err)
	require.Equal(t, 1.25, data.FiveHourUsage)
	require.NotNil(t, cache.lastSubscriptionSet)
	require.Equal(t, 1.25, cache.lastSubscriptionSet.FiveHourUsage)
}

func TestBillingCacheServiceGetSubscriptionStatusReloadsAfterInvalidSubscriptionCache(t *testing.T) {
	limit := 5.0
	windowStart := time.Now().Add(-time.Hour)
	subRepo := newSubscriptionUserSubRepoStub()
	subRepo.seed(&UserSubscription{
		ID:                  10,
		UserID:              100,
		GroupID:             200,
		Status:              SubscriptionStatusActive,
		ExpiresAt:           time.Now().Add(24 * time.Hour),
		FiveHourLimitUSD:    &limit,
		FiveHourUsageUSD:    1.25,
		FiveHourWindowStart: &windowStart,
		UpdatedAt:           time.Now(),
	})
	cache := &billingCacheWorkerStub{
		subscriptionGetErr: errors.New("invalid cache: missing rolling quota field five_hour_limit_usd"),
	}
	svc := NewBillingCacheService(cache, nil, subRepo, nil, nil, nil, &config.Config{}, nil)
	svc.Stop()

	data, err := svc.GetSubscriptionStatus(context.Background(), 100, 200)

	require.NoError(t, err)
	require.Equal(t, 1.25, data.FiveHourUsage)
	require.NotNil(t, cache.lastSubscriptionSet)
	require.Equal(t, 1.25, cache.lastSubscriptionSet.FiveHourUsage)
}

func TestBillingCacheServiceUpdateSubscriptionUsageReloadsCacheOnMiss(t *testing.T) {
	limit := 5.0
	windowStart := time.Now().Add(-time.Hour)
	subRepo := newSubscriptionUserSubRepoStub()
	subRepo.seed(&UserSubscription{
		ID:                  10,
		UserID:              100,
		GroupID:             200,
		Status:              SubscriptionStatusActive,
		ExpiresAt:           time.Now().Add(24 * time.Hour),
		FiveHourLimitUSD:    &limit,
		FiveHourUsageUSD:    2.75,
		FiveHourWindowStart: &windowStart,
		UpdatedAt:           time.Now(),
	})
	cache := &billingCacheWorkerStub{subscriptionUpdateErr: redis.Nil}
	svc := NewBillingCacheService(cache, nil, subRepo, nil, nil, nil, &config.Config{}, nil)
	t.Cleanup(svc.Stop)

	err := svc.UpdateSubscriptionUsage(context.Background(), 100, 200, 0.5)

	require.NoError(t, err)
	require.NotNil(t, cache.lastSubscriptionSet)
	require.Equal(t, 2.75, cache.lastSubscriptionSet.FiveHourUsage)
}

func TestBillingCacheServiceEnqueueAfterStopReturnsFalse(t *testing.T) {
	cache := &billingCacheWorkerStub{}
	svc := NewBillingCacheService(cache, nil, nil, nil, nil, nil, &config.Config{}, nil)
	svc.Stop()

	enqueued := svc.enqueueCacheWrite(cacheWriteTask{
		kind:   cacheWriteDeductBalance,
		userID: 1,
		amount: 1,
	})
	require.False(t, enqueued)
}

func TestCheckBillingEligibility_SubscriptionPreflightIgnoresCachedQuotaUsage(t *testing.T) {
	limit := 2.5
	windowStart := time.Now().Add(-1 * time.Hour)
	cache := &billingCacheWorkerStub{
		subscriptionData: &SubscriptionCacheData{
			Status:              SubscriptionStatusActive,
			ExpiresAt:           time.Now().Add(24 * time.Hour),
			FiveHourLimitUSD:    &limit,
			FiveHourUsage:       limit,
			FiveHourWindowStart: &windowStart,
		},
	}
	svc := NewBillingCacheService(cache, nil, nil, nil, nil, nil, &config.Config{}, nil)
	t.Cleanup(svc.Stop)

	err := svc.CheckBillingEligibility(
		context.Background(),
		&User{ID: 1},
		nil,
		&Group{ID: 2, SubscriptionType: SubscriptionTypeSubscription, Status: StatusActive},
		&UserSubscription{Status: SubscriptionStatusActive, ExpiresAt: time.Now().Add(24 * time.Hour)},
		"anthropic",
	)

	require.NoError(t, err)
}

func TestCheckBillingEligibility_SubscriptionIgnoresLegacyGroupDailyLimit(t *testing.T) {
	dailyLimit := 1.0
	fiveHourLimit := 10.0
	windowStart := time.Now().Add(-1 * time.Hour)
	cache := &billingCacheWorkerStub{
		subscriptionData: &SubscriptionCacheData{
			Status:              SubscriptionStatusActive,
			ExpiresAt:           time.Now().Add(24 * time.Hour),
			DailyUsage:          dailyLimit,
			FiveHourLimitUSD:    &fiveHourLimit,
			FiveHourUsage:       2.0,
			FiveHourWindowStart: &windowStart,
		},
	}
	svc := NewBillingCacheService(cache, nil, nil, nil, nil, nil, &config.Config{}, nil)
	t.Cleanup(svc.Stop)

	err := svc.CheckBillingEligibility(
		context.Background(),
		&User{ID: 1},
		nil,
		&Group{
			ID:               2,
			SubscriptionType: SubscriptionTypeSubscription,
			Status:           StatusActive,
			DailyLimitUSD:    &dailyLimit,
		},
		&UserSubscription{Status: SubscriptionStatusActive, ExpiresAt: time.Now().Add(24 * time.Hour)},
		"anthropic",
	)

	require.NoError(t, err)
}

func TestCheckBillingEligibility_SubscriptionAllowsExpiredFiveHourWindowWithHighUsage(t *testing.T) {
	limit := 2.5
	expiredWindowStart := time.Now().Add(-SubscriptionWindowFiveHour - time.Minute)
	cache := &billingCacheWorkerStub{
		subscriptionData: &SubscriptionCacheData{
			Status:              SubscriptionStatusActive,
			ExpiresAt:           time.Now().Add(24 * time.Hour),
			FiveHourLimitUSD:    &limit,
			FiveHourUsage:       99,
			FiveHourWindowStart: &expiredWindowStart,
		},
	}
	svc := NewBillingCacheService(cache, nil, nil, nil, nil, nil, &config.Config{}, nil)
	t.Cleanup(svc.Stop)

	err := svc.CheckBillingEligibility(
		context.Background(),
		&User{ID: 1},
		nil,
		&Group{ID: 2, SubscriptionType: SubscriptionTypeSubscription, Status: StatusActive},
		&UserSubscription{Status: SubscriptionStatusActive, ExpiresAt: time.Now().Add(24 * time.Hour)},
		"anthropic",
	)

	require.NoError(t, err)
}

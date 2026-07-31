package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type billingEligibilityAPIKeyRepoStub struct {
	APIKeyRepository
	key          *APIKey
	err          error
	rateLimitErr error
}

func (s *billingEligibilityAPIKeyRepoStub) GetByID(context.Context, int64) (*APIKey, error) {
	return s.key, s.err
}

func (s *billingEligibilityAPIKeyRepoStub) GetRateLimitData(context.Context, int64) (*APIKeyRateLimitData, error) {
	if s.rateLimitErr != nil {
		return nil, s.rateLimitErr
	}
	return &APIKeyRateLimitData{}, nil
}

type billingEligibilitySubscriptionRepoStub struct {
	UserSubscriptionRepository
	subscriptions []UserSubscription
	err           error
}

func (s *billingEligibilitySubscriptionRepoStub) ListActiveByUserIDAndGroupID(context.Context, int64, int64) ([]UserSubscription, error) {
	if s.err != nil {
		return nil, s.err
	}
	out := make([]UserSubscription, len(s.subscriptions))
	copy(out, s.subscriptions)
	return out, nil
}

type billingEligibilityGroupRateRepoStub struct {
	UserGroupRateRepository
	err error
}

func (s *billingEligibilityGroupRateRepoStub) GetRPMOverrideByUserAndGroup(context.Context, int64, int64) (*int, error) {
	return nil, s.err
}

func newBillingEligibilityServiceForTest(t *testing.T, key *APIKey, subscriptions []UserSubscription) *BillingEligibilityService {
	t.Helper()
	cfg := &config.Config{RunMode: config.RunModeStandard}
	apiKeyRepo := &billingEligibilityAPIKeyRepoStub{key: key}
	subRepo := &billingEligibilitySubscriptionRepoStub{subscriptions: subscriptions}
	billingCache := NewBillingCacheService(nil, nil, subRepo, apiKeyRepo, nil, &billingEligibilityGroupRateRepoStub{}, cfg, nil)
	t.Cleanup(billingCache.Stop)
	subscriptionService := NewSubscriptionService(nil, subRepo, billingCache, nil, nil)
	t.Cleanup(subscriptionService.Stop)
	return NewBillingEligibilityService(subscriptionService, billingCache, apiKeyRepo, cfg)
}

func billingEligibilityKey(group *Group) *APIKey {
	groupID := group.ID
	return &APIKey{
		ID:      22,
		UserID:  11,
		GroupID: &groupID,
		Status:  StatusAPIKeyActive,
		User: &User{
			ID:     11,
			Status: StatusActive,
		},
		Group: group,
	}
}

func TestBillingEligibilityResolveSelectsLatestUsableSubscription(t *testing.T) {
	now := time.Now()
	limit := 1.0
	group := &Group{ID: 7, Status: StatusActive, SubscriptionType: SubscriptionTypeSubscription}
	svc := newBillingEligibilityServiceForTest(t, billingEligibilityKey(group), []UserSubscription{
		{
			ID:                  1,
			UserID:              11,
			GroupID:             7,
			Status:              SubscriptionStatusActive,
			ExpiresAt:           now.Add(time.Hour),
			DailyWindowStart:    &now,
			FiveHourLimitUSD:    &limit,
			FiveHourUsageUSD:    limit,
			FiveHourWindowStart: &now,
		},
		{
			ID:                  2,
			UserID:              11,
			GroupID:             7,
			Status:              SubscriptionStatusActive,
			ExpiresAt:           now.Add(2 * time.Hour),
			DailyWindowStart:    &now,
			FiveHourLimitUSD:    &limit,
			FiveHourWindowStart: &now,
		},
	})
	groupID := int64(7)

	subscription, err := svc.ResolveUsableSubscriptionForRequest(context.Background(), 11, 22, &groupID, PlatformAnthropic)

	require.NoError(t, err)
	require.NotNil(t, subscription)
	require.Equal(t, int64(2), subscription.ID)
}

func TestBillingEligibilityResolveRejectsAPIKeyDisabledWhileQueued(t *testing.T) {
	group := &Group{ID: 7, Status: StatusActive}
	key := billingEligibilityKey(group)
	key.Status = StatusAPIKeyDisabled
	svc := newBillingEligibilityServiceForTest(t, key, nil)
	groupID := int64(7)

	_, err := svc.ResolveUsableSubscriptionForRequest(context.Background(), 11, 22, &groupID, PlatformAnthropic)

	require.ErrorIs(t, err, ErrAPIKeyNotActive)
}

type billingEligibilityFailingRPMCache struct {
	UserRPMCache
	err error
}

func (s *billingEligibilityFailingRPMCache) IncrementUserGroupRPM(context.Context, int64, int64) (int, error) {
	return 0, s.err
}

func (s *billingEligibilityFailingRPMCache) IncrementUserRPM(context.Context, int64) (int, error) {
	return 0, s.err
}

func TestBillingEligibilityResolveFailsClosedOnAPIKeyRateLimitReloadError(t *testing.T) {
	now := time.Now()
	cfg := &config.Config{RunMode: config.RunModeStandard}
	group := &Group{ID: 7, Status: StatusActive, SubscriptionType: SubscriptionTypeSubscription}
	key := billingEligibilityKey(group)
	key.RateLimit5h = 1
	apiKeyRepo := &billingEligibilityAPIKeyRepoStub{key: key, rateLimitErr: errors.New("rate limit database unavailable")}
	subRepo := &billingEligibilitySubscriptionRepoStub{subscriptions: []UserSubscription{{
		ID:               1,
		UserID:           11,
		GroupID:          7,
		Status:           SubscriptionStatusActive,
		ExpiresAt:        now.Add(time.Hour),
		DailyWindowStart: &now,
	}}}
	billingCache := NewBillingCacheService(nil, nil, subRepo, apiKeyRepo, nil, &billingEligibilityGroupRateRepoStub{}, cfg, nil)
	t.Cleanup(billingCache.Stop)
	subscriptionService := NewSubscriptionService(nil, subRepo, billingCache, nil, nil)
	t.Cleanup(subscriptionService.Stop)
	svc := NewBillingEligibilityService(subscriptionService, billingCache, apiKeyRepo, cfg)
	groupID := int64(7)

	_, err := svc.ResolveUsableSubscriptionForRequest(context.Background(), 11, 22, &groupID, PlatformAnthropic)

	require.ErrorIs(t, err, ErrBillingServiceUnavailable)
}

func TestBillingEligibilityResolveFailsClosedOnRPMDependencyError(t *testing.T) {
	now := time.Now()
	group := &Group{ID: 7, Status: StatusActive, SubscriptionType: SubscriptionTypeSubscription, RPMLimit: 10}
	svc := newBillingEligibilityServiceForTest(t, billingEligibilityKey(group), []UserSubscription{{
		ID:               1,
		UserID:           11,
		GroupID:          7,
		Status:           SubscriptionStatusActive,
		ExpiresAt:        now.Add(time.Hour),
		DailyWindowStart: &now,
	}})
	svc.billingCacheService.userRPMCache = &billingEligibilityFailingRPMCache{err: errors.New("redis unavailable")}
	groupID := int64(7)

	_, err := svc.ResolveUsableSubscriptionForRequest(context.Background(), 11, 22, &groupID, PlatformAnthropic)

	require.ErrorIs(t, err, ErrBillingServiceUnavailable)
}

func TestBillingEligibilityResolveFailsClosedOnSubscriptionReloadError(t *testing.T) {
	cfg := &config.Config{RunMode: config.RunModeStandard}
	group := &Group{ID: 7, Status: StatusActive, SubscriptionType: SubscriptionTypeSubscription}
	key := billingEligibilityKey(group)
	apiKeyRepo := &billingEligibilityAPIKeyRepoStub{key: key}
	subRepo := &billingEligibilitySubscriptionRepoStub{err: errors.New("database unavailable")}
	billingCache := NewBillingCacheService(nil, nil, subRepo, apiKeyRepo, nil, &billingEligibilityGroupRateRepoStub{}, cfg, nil)
	t.Cleanup(billingCache.Stop)
	subscriptionService := NewSubscriptionService(nil, subRepo, billingCache, nil, nil)
	t.Cleanup(subscriptionService.Stop)
	svc := NewBillingEligibilityService(subscriptionService, billingCache, apiKeyRepo, cfg)
	groupID := int64(7)

	_, err := svc.ResolveUsableSubscriptionForRequest(context.Background(), 11, 22, &groupID, PlatformAnthropic)

	require.ErrorIs(t, err, ErrBillingServiceUnavailable)
}

func TestBillingEligibilityResolveFailsClosedOnAPIKeyReloadError(t *testing.T) {
	cfg := &config.Config{RunMode: config.RunModeStandard}
	apiKeyRepo := &billingEligibilityAPIKeyRepoStub{err: errors.New("database unavailable")}
	billingCache := NewBillingCacheService(nil, nil, nil, apiKeyRepo, nil, nil, cfg, nil)
	t.Cleanup(billingCache.Stop)
	svc := NewBillingEligibilityService(nil, billingCache, apiKeyRepo, cfg)

	_, err := svc.ResolveUsableSubscriptionForRequest(context.Background(), 11, 22, nil, PlatformAnthropic)

	require.ErrorIs(t, err, ErrBillingServiceUnavailable)
}

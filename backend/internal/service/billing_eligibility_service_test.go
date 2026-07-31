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
	key *APIKey
	err error
}

func (s *billingEligibilityAPIKeyRepoStub) GetByID(context.Context, int64) (*APIKey, error) {
	return s.key, s.err
}

type billingEligibilitySubscriptionRepoStub struct {
	UserSubscriptionRepository
	subscriptions []UserSubscription
}

func (s *billingEligibilitySubscriptionRepoStub) ListActiveByUserIDAndGroupID(context.Context, int64, int64) ([]UserSubscription, error) {
	out := make([]UserSubscription, len(s.subscriptions))
	copy(out, s.subscriptions)
	return out, nil
}

func newBillingEligibilityServiceForTest(t *testing.T, key *APIKey, subscriptions []UserSubscription) *BillingEligibilityService {
	t.Helper()
	cfg := &config.Config{RunMode: config.RunModeStandard}
	apiKeyRepo := &billingEligibilityAPIKeyRepoStub{key: key}
	subRepo := &billingEligibilitySubscriptionRepoStub{subscriptions: subscriptions}
	billingCache := NewBillingCacheService(nil, nil, subRepo, apiKeyRepo, nil, nil, cfg, nil)
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

func TestBillingEligibilityResolveFailsClosedOnAPIKeyReloadError(t *testing.T) {
	cfg := &config.Config{RunMode: config.RunModeStandard}
	apiKeyRepo := &billingEligibilityAPIKeyRepoStub{err: errors.New("database unavailable")}
	billingCache := NewBillingCacheService(nil, nil, nil, apiKeyRepo, nil, nil, cfg, nil)
	t.Cleanup(billingCache.Stop)
	svc := NewBillingEligibilityService(nil, billingCache, apiKeyRepo, cfg)

	_, err := svc.ResolveUsableSubscriptionForRequest(context.Background(), 11, 22, nil, PlatformAnthropic)

	require.ErrorIs(t, err, ErrBillingServiceUnavailable)
}

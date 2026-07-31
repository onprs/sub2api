package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

var (
	ErrBillingAuthorizationChanged = infraerrors.Forbidden("BILLING_AUTHORIZATION_CHANGED", "billing authorization changed while the request was waiting")
	ErrAPIKeyNotActive             = infraerrors.Forbidden("API_KEY_NOT_ACTIVE", "api key is not active")
)

// BillingEligibilityResolver performs the authoritative billing check after a
// request leaves a concurrency queue and before it can reach an upstream.
type BillingEligibilityResolver interface {
	ResolveUsableSubscriptionForRequest(ctx context.Context, userID, apiKeyID int64, expectedGroupID *int64, platform string) (*UserSubscription, error)
}

// BillingEligibilityService combines fresh authorization state with the
// existing subscription selection and billing-cache checks.
type BillingEligibilityService struct {
	subscriptionService *SubscriptionService
	billingCacheService *BillingCacheService
	apiKeyRepo          APIKeyRepository
	cfg                 *config.Config
}

func NewBillingEligibilityService(
	subscriptionService *SubscriptionService,
	billingCacheService *BillingCacheService,
	apiKeyRepo APIKeyRepository,
	cfg *config.Config,
) *BillingEligibilityService {
	return &BillingEligibilityService{
		subscriptionService: subscriptionService,
		billingCacheService: billingCacheService,
		apiKeyRepo:          apiKeyRepo,
		cfg:                 cfg,
	}
}

func (s *BillingEligibilityService) ResolveUsableSubscriptionForRequest(
	ctx context.Context,
	userID, apiKeyID int64,
	expectedGroupID *int64,
	platform string,
) (*UserSubscription, error) {
	if s != nil && s.cfg != nil && s.cfg.RunMode == config.RunModeSimple {
		return nil, nil
	}
	if s == nil || s.apiKeyRepo == nil || s.billingCacheService == nil {
		return nil, ErrBillingServiceUnavailable
	}

	apiKey, err := s.apiKeyRepo.GetByID(ctx, apiKeyID)
	if err != nil {
		if errors.Is(err, ErrAPIKeyNotFound) {
			return nil, ErrBillingAuthorizationChanged.WithCause(err)
		}
		return nil, ErrBillingServiceUnavailable.WithCause(fmt.Errorf("reload api key: %w", err))
	}
	if apiKey == nil || apiKey.User == nil || apiKey.UserID != userID || apiKey.User.ID != userID || !sameOptionalID(apiKey.GroupID, expectedGroupID) {
		return nil, ErrBillingAuthorizationChanged
	}
	if !apiKey.User.IsActive() {
		return nil, ErrUserNotActive
	}
	if !apiKey.IsActive() {
		return nil, ErrAPIKeyNotActive
	}
	if apiKey.IsExpired() {
		return nil, ErrAPIKeyExpired
	}
	if apiKey.IsQuotaExhausted() {
		return nil, ErrAPIKeyQuotaExhausted
	}
	if apiKey.GroupID != nil {
		if apiKey.Group == nil || !apiKey.Group.IsActive() {
			return nil, ErrBillingAuthorizationChanged
		}
		if !apiKey.Group.IsSubscriptionType() && !apiKey.User.CanBindGroup(apiKey.Group.ID, apiKey.Group.IsExclusive) {
			return nil, ErrBillingAuthorizationChanged
		}
	}

	var subscription *UserSubscription
	if apiKey.Group != nil && apiKey.Group.IsSubscriptionType() {
		if s.subscriptionService == nil {
			return nil, ErrBillingServiceUnavailable
		}
		subscription, err = s.resolveLatestSubscription(ctx, userID, apiKey.Group)
		if err != nil {
			return nil, err
		}
	}

	if err := s.billingCacheService.CheckBillingEligibilityStrict(ctx, apiKey.User, apiKey, apiKey.Group, subscription, platform); err != nil {
		return nil, err
	}
	return subscription, nil
}

func (s *BillingEligibilityService) resolveLatestSubscription(ctx context.Context, userID int64, group *Group) (*UserSubscription, error) {
	subscription, needsMaintenance, err := s.subscriptionService.GetUsableActiveSubscription(ctx, userID, group.ID, group)
	if err != nil {
		return nil, normalizeBillingEligibilityError(err)
	}
	if needsMaintenance {
		if _, err := s.subscriptionService.EnsureWindowMaintenance(ctx, subscription); err != nil {
			return nil, ErrBillingServiceUnavailable.WithCause(fmt.Errorf("maintain subscription windows: %w", err))
		}
		// Re-run selection after the CAS and DB read. A concurrent charge may have
		// exhausted this package while maintenance was in progress.
		subscription, needsMaintenance, err = s.subscriptionService.GetUsableActiveSubscription(ctx, userID, group.ID, group)
		if err != nil {
			return nil, normalizeBillingEligibilityError(err)
		}
		if needsMaintenance {
			return nil, ErrBillingServiceUnavailable.WithCause(errors.New("subscription windows remained stale after maintenance"))
		}
	}
	return subscription, nil
}

func normalizeBillingEligibilityError(err error) error {
	if err == nil {
		return nil
	}
	var appErr *infraerrors.ApplicationError
	if errors.As(err, &appErr) {
		return err
	}
	return ErrBillingServiceUnavailable.WithCause(err)
}

func sameOptionalID(left, right *int64) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

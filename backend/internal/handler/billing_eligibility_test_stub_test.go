package handler

import (
	"context"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type allowBillingEligibilityResolver struct{}

func (allowBillingEligibilityResolver) ResolveUsableSubscriptionForRequest(context.Context, int64, int64, *int64, string) (*service.UserSubscription, error) {
	return nil, nil
}

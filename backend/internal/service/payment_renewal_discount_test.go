//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/require"
)

func TestResolveSubscriptionOrderPricingAppliesRenewalDiscountForActiveSamePlan(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	user := client.User.Create().
		SetEmail("renewal-discount@example.com").
		SetPasswordHash("hash").
		SetUsername("renewal-discount").
		SaveX(ctx)
	group := client.Group.Create().
		SetName("Renewal Group").
		SetPlatform(PlatformAnthropic).
		SetSubscriptionType(SubscriptionTypeSubscription).
		SaveX(ctx)
	discount := 15.0
	plan := client.SubscriptionPlan.Create().
		SetGroupID(group.ID).
		SetName("Weekly Pro").
		SetPrice(8.70).
		SetRenewalDiscountPercent(discount).
		SetValidityDays(7).
		SetValidityUnit("days").
		SaveX(ctx)
	otherPlan := client.SubscriptionPlan.Create().
		SetGroupID(group.ID).
		SetName("Weekly Other").
		SetPrice(8.70).
		SetRenewalDiscountPercent(discount).
		SetValidityDays(7).
		SetValidityUnit("days").
		SaveX(ctx)
	now := time.Now()
	client.UserSubscription.Create().
		SetUserID(user.ID).
		SetGroupID(group.ID).
		SetPlanID(plan.ID).
		SetStartsAt(now.Add(-24 * time.Hour)).
		SetExpiresAt(now.Add(24 * time.Hour)).
		SetStatus(SubscriptionStatusActive).
		SaveX(ctx)

	svc := &PaymentService{entClient: client}
	pricing, err := svc.resolveSubscriptionOrderPricing(ctx, CreateOrderRequest{
		UserID:    user.ID,
		OrderType: payment.OrderTypeSubscription,
		PlanID:    plan.ID,
	}, plan)

	require.NoError(t, err)
	require.True(t, pricing.RenewalEligible)
	require.Equal(t, plan.Price, pricing.PlanPrice)
	require.Equal(t, discount, *pricing.RenewalDiscountPercent)
	require.InDelta(t, 7.40, pricing.EffectivePrice, 1e-9)

	otherPricing, err := svc.resolveSubscriptionOrderPricing(ctx, CreateOrderRequest{
		UserID:    user.ID,
		OrderType: payment.OrderTypeSubscription,
		PlanID:    otherPlan.ID,
	}, otherPlan)

	require.NoError(t, err)
	require.False(t, otherPricing.RenewalEligible)
	require.Nil(t, otherPricing.RenewalDiscountPercent)
	require.Equal(t, otherPlan.Price, otherPricing.EffectivePrice)
}

func TestResolveSubscriptionOrderPricingDoesNotDiscountExpiredSamePlan(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	user := client.User.Create().
		SetEmail("expired-renewal@example.com").
		SetPasswordHash("hash").
		SetUsername("expired-renewal").
		SaveX(ctx)
	group := client.Group.Create().
		SetName("Expired Renewal Group").
		SetPlatform(PlatformAnthropic).
		SetSubscriptionType(SubscriptionTypeSubscription).
		SaveX(ctx)
	discount := 20.0
	plan := client.SubscriptionPlan.Create().
		SetGroupID(group.ID).
		SetName("Expired Plan").
		SetPrice(10).
		SetRenewalDiscountPercent(discount).
		SetValidityDays(7).
		SetValidityUnit("days").
		SaveX(ctx)
	now := time.Now()
	client.UserSubscription.Create().
		SetUserID(user.ID).
		SetGroupID(group.ID).
		SetPlanID(plan.ID).
		SetStartsAt(now.Add(-14 * 24 * time.Hour)).
		SetExpiresAt(now.Add(-24 * time.Hour)).
		SetStatus(SubscriptionStatusActive).
		SaveX(ctx)

	svc := &PaymentService{entClient: client}
	pricing, err := svc.resolveSubscriptionOrderPricing(ctx, CreateOrderRequest{
		UserID:    user.ID,
		OrderType: payment.OrderTypeSubscription,
		PlanID:    plan.ID,
	}, plan)

	require.NoError(t, err)
	require.False(t, pricing.RenewalEligible)
	require.Nil(t, pricing.RenewalDiscountPercent)
	require.Equal(t, plan.Price, pricing.EffectivePrice)
}

func TestCreateOrderInTxSnapshotsRenewalDiscountPricing(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	user := client.User.Create().
		SetEmail("snapshot-renewal@example.com").
		SetPasswordHash("hash").
		SetUsername("snapshot-renewal").
		SaveX(ctx)
	group := client.Group.Create().
		SetName("Snapshot Renewal Group").
		SetPlatform(PlatformAnthropic).
		SetSubscriptionType(SubscriptionTypeSubscription).
		SaveX(ctx)
	discount := 15.0
	plan := client.SubscriptionPlan.Create().
		SetGroupID(group.ID).
		SetName("Snapshot Renewal Plan").
		SetPrice(8.70).
		SetRenewalDiscountPercent(discount).
		SetValidityDays(7).
		SetValidityUnit("days").
		SaveX(ctx)

	svc := &PaymentService{entClient: client}
	order, err := svc.createOrderInTx(
		ctx,
		CreateOrderRequest{
			UserID:      user.ID,
			PaymentType: payment.TypeAlipay,
			OrderType:   payment.OrderTypeSubscription,
			ClientIP:    "127.0.0.1",
			SrcHost:     "app.example.com",
		},
		&User{
			ID:       user.ID,
			Email:    user.Email,
			Username: user.Username,
		},
		plan,
		&PaymentConfig{MaxPendingOrders: 3, OrderTimeoutMin: 30},
		subscriptionOrderPricing{
			PlanPrice:              plan.Price,
			EffectivePrice:         7.40,
			RenewalEligible:        true,
			RenewalDiscountPercent: &discount,
		},
		0,
		7.40,
		nil,
	)

	require.NoError(t, err)
	require.Equal(t, 7.40, order.Amount)
	require.Equal(t, 7.40, order.PayAmount)
	require.NotNil(t, order.SubscriptionPlanPrice)
	require.Equal(t, plan.Price, *order.SubscriptionPlanPrice)
	require.NotNil(t, order.SubscriptionRenewalDiscountPercent)
	require.Equal(t, discount, *order.SubscriptionRenewalDiscountPercent)
}

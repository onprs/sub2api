//go:build unit

package service

import (
	"context"
	"strconv"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func TestValidateRefundRequestRejectsLegacyGuessedProviderInstance(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)

	user, err := client.User.Create().
		SetEmail("refund-legacy@example.com").
		SetPasswordHash("hash").
		SetUsername("refund-legacy-user").
		Save(ctx)
	require.NoError(t, err)

	_, err = client.PaymentProviderInstance.Create().
		SetProviderKey(payment.TypeAlipay).
		SetName("alipay-refund-instance").
		SetConfig("{}").
		SetSupportedTypes("alipay").
		SetEnabled(true).
		SetAllowUserRefund(true).
		SetRefundEnabled(true).
		Save(ctx)
	require.NoError(t, err)

	order, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(88).
		SetPayAmount(88).
		SetFeeRate(0).
		SetRechargeCode("REFUND-LEGACY-ORDER").
		SetOutTradeNo("sub2_refund_legacy_order").
		SetPaymentType(payment.TypeAlipay).
		SetPaymentTradeNo("trade-legacy-refund").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusCompleted).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetPaidAt(time.Now()).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		Save(ctx)
	require.NoError(t, err)

	svc := &PaymentService{
		entClient: client,
	}

	_, err = svc.validateRefundRequest(ctx, order.ID, user.ID)
	require.Error(t, err)
	require.Equal(t, "USER_REFUND_DISABLED", infraerrors.Reason(err))
}

func TestPrepareRefundRejectsLegacyGuessedProviderInstance(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)

	user, err := client.User.Create().
		SetEmail("refund-legacy-admin@example.com").
		SetPasswordHash("hash").
		SetUsername("refund-legacy-admin-user").
		Save(ctx)
	require.NoError(t, err)

	_, err = client.PaymentProviderInstance.Create().
		SetProviderKey(payment.TypeAlipay).
		SetName("alipay-refund-admin-instance").
		SetConfig("{}").
		SetSupportedTypes("alipay").
		SetEnabled(true).
		SetAllowUserRefund(true).
		SetRefundEnabled(true).
		Save(ctx)
	require.NoError(t, err)

	order, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(188).
		SetPayAmount(188).
		SetFeeRate(0).
		SetRechargeCode("REFUND-LEGACY-ADMIN-ORDER").
		SetOutTradeNo("sub2_refund_legacy_admin_order").
		SetPaymentType(payment.TypeAlipay).
		SetPaymentTradeNo("trade-legacy-admin-refund").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusCompleted).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetPaidAt(time.Now()).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		Save(ctx)
	require.NoError(t, err)

	svc := &PaymentService{
		entClient: client,
	}

	plan, result, err := svc.PrepareRefund(ctx, order.ID, 0, "", false, false)
	require.Nil(t, plan)
	require.Nil(t, result)
	require.Error(t, err)
	require.Equal(t, "REFUND_DISABLED", infraerrors.Reason(err))
}

func TestGwRefundRejectsAlipayMerchantIdentitySnapshotMismatch(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)

	user, err := client.User.Create().
		SetEmail("refund-snapshot-mismatch@example.com").
		SetPasswordHash("hash").
		SetUsername("refund-snapshot-mismatch-user").
		Save(ctx)
	require.NoError(t, err)

	inst, err := client.PaymentProviderInstance.Create().
		SetProviderKey(payment.TypeAlipay).
		SetName("alipay-refund-mismatch-instance").
		SetConfig(encryptWebhookProviderConfig(t, map[string]string{
			"appId":      "runtime-alipay-app",
			"privateKey": "runtime-private-key",
		})).
		SetSupportedTypes("alipay").
		SetEnabled(true).
		SetRefundEnabled(true).
		Save(ctx)
	require.NoError(t, err)

	instID := strconv.FormatInt(inst.ID, 10)
	order, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(88).
		SetPayAmount(88).
		SetFeeRate(0).
		SetRechargeCode("REFUND-SNAPSHOT-MISMATCH-ORDER").
		SetOutTradeNo("sub2_refund_snapshot_mismatch_order").
		SetPaymentType(payment.TypeAlipay).
		SetPaymentTradeNo("trade-refund-snapshot-mismatch").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusCompleted).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetPaidAt(time.Now()).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		SetProviderInstanceID(instID).
		SetProviderKey(payment.TypeAlipay).
		SetProviderSnapshot(map[string]any{
			"schema_version":       2,
			"provider_instance_id": instID,
			"provider_key":         payment.TypeAlipay,
			"merchant_app_id":      "expected-alipay-app",
		}).
		Save(ctx)
	require.NoError(t, err)

	svc := &PaymentService{
		entClient:    client,
		loadBalancer: newWebhookProviderTestLoadBalancer(client),
	}

	err = svc.gwRefund(ctx, &RefundPlan{
		OrderID:       order.ID,
		Order:         order,
		RefundAmount:  order.Amount,
		GatewayAmount: order.Amount,
		Reason:        "snapshot mismatch",
	})
	require.ErrorContains(t, err, "alipay app_id mismatch")
}

func TestCalculateGatewayRefundAmountUsesCurrencyPrecision(t *testing.T) {
	require.InDelta(t, 6.173, calculateGatewayRefundAmount(100, 12.345, 50, "KWD"), 1e-12)
	require.InDelta(t, 12.345, calculateGatewayRefundAmount(100, 12.345, 100, "KWD"), 1e-12)
	require.InDelta(t, 52, calculateGatewayRefundAmount(100, 103, 50, "JPY"), 1e-12)
}

func TestFormatGatewayRefundAmountUsesOrderCurrency(t *testing.T) {
	order := &dbent.PaymentOrder{
		ProviderSnapshot: map[string]any{
			"currency": "KWD",
		},
	}

	require.Equal(t, "12.345", formatGatewayRefundAmount(12.345, order))
}

func TestValidateRefundProviderResponseAcceptsPending(t *testing.T) {
	require.NoError(t, validateRefundProviderResponse(&payment.RefundResponse{Status: payment.ProviderStatusPending}))
	require.NoError(t, validateRefundProviderResponse(&payment.RefundResponse{Status: payment.ProviderStatusSuccess}))
	require.Error(t, validateRefundProviderResponse(&payment.RefundResponse{Status: payment.ProviderStatusFailed}))
	require.Error(t, validateRefundProviderResponse(nil))
}

func TestPrepareRefundDeductsSubscriptionMatchedByOrderPlan(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)

	user, err := client.User.Create().
		SetEmail("refund-plan@example.com").
		SetPasswordHash("hash").
		SetUsername("refund-plan-user").
		Save(ctx)
	require.NoError(t, err)

	inst, err := client.PaymentProviderInstance.Create().
		SetProviderKey(payment.TypeAlipay).
		SetName("alipay-refund-plan-instance").
		SetConfig("{}").
		SetSupportedTypes("alipay").
		SetEnabled(true).
		SetRefundEnabled(true).
		Save(ctx)
	require.NoError(t, err)

	planA := int64(101)
	planB := int64(102)
	groupID := int64(42)
	days := 30
	order, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(88).
		SetPayAmount(88).
		SetFeeRate(0).
		SetRechargeCode("REFUND-PLAN-ORDER").
		SetOutTradeNo("sub2_refund_plan_order").
		SetPaymentType(payment.TypeAlipay).
		SetPaymentTradeNo("trade-refund-plan").
		SetOrderType(payment.OrderTypeSubscription).
		SetStatus(OrderStatusCompleted).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetPaidAt(time.Now()).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		SetProviderInstanceID(strconv.FormatInt(inst.ID, 10)).
		SetProviderKey(payment.TypeAlipay).
		SetPlanID(planB).
		SetSubscriptionGroupID(groupID).
		SetSubscriptionDays(days).
		Save(ctx)
	require.NoError(t, err)

	subRepo := newSubscriptionUserSubRepoStub()
	subRepo.seed(&UserSubscription{
		ID:        5,
		UserID:    user.ID,
		GroupID:   groupID,
		PlanID:    &planA,
		StartsAt:  time.Now().Add(-24 * time.Hour),
		ExpiresAt: time.Now().Add(24 * time.Hour),
		Status:    SubscriptionStatusActive,
	})
	subRepo.seed(&UserSubscription{
		ID:        6,
		UserID:    user.ID,
		GroupID:   groupID,
		PlanID:    &planB,
		StartsAt:  time.Now().Add(-24 * time.Hour),
		ExpiresAt: time.Now().Add(48 * time.Hour),
		Status:    SubscriptionStatusActive,
	})
	svc := &PaymentService{
		entClient:       client,
		subscriptionSvc: NewSubscriptionService(&subscriptionGroupRepoStub{}, subRepo, nil, nil, nil),
	}

	plan, result, err := svc.PrepareRefund(ctx, order.ID, 0, "", false, true)

	require.NoError(t, err)
	require.Nil(t, result)
	require.NotNil(t, plan)
	require.Equal(t, payment.DeductionTypeSubscription, plan.DeductionType)
	require.Equal(t, days, plan.SubDaysToDeduct)
	require.Equal(t, int64(6), plan.SubscriptionID)
}

func TestPrepareRefundRejectsSubscriptionIDWithMismatchedOrderPlan(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)

	user, err := client.User.Create().
		SetEmail("refund-mismatch-plan@example.com").
		SetPasswordHash("hash").
		SetUsername("refund-mismatch-plan-user").
		Save(ctx)
	require.NoError(t, err)

	inst, err := client.PaymentProviderInstance.Create().
		SetProviderKey(payment.TypeAlipay).
		SetName("alipay-refund-mismatch-plan-instance").
		SetConfig("{}").
		SetSupportedTypes("alipay").
		SetEnabled(true).
		SetRefundEnabled(true).
		Save(ctx)
	require.NoError(t, err)

	planA := int64(101)
	planB := int64(102)
	groupID := int64(42)
	days := 30
	wrongSubscriptionID := int64(5)
	order, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(88).
		SetPayAmount(88).
		SetFeeRate(0).
		SetRechargeCode("REFUND-MISMATCH-PLAN-ORDER").
		SetOutTradeNo("sub2_refund_mismatch_plan_order").
		SetPaymentType(payment.TypeAlipay).
		SetPaymentTradeNo("trade-refund-mismatch-plan").
		SetOrderType(payment.OrderTypeSubscription).
		SetStatus(OrderStatusCompleted).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetPaidAt(time.Now()).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		SetProviderInstanceID(strconv.FormatInt(inst.ID, 10)).
		SetProviderKey(payment.TypeAlipay).
		SetPlanID(planB).
		SetSubscriptionGroupID(groupID).
		SetSubscriptionDays(days).
		SetSubscriptionID(wrongSubscriptionID).
		Save(ctx)
	require.NoError(t, err)

	subRepo := newSubscriptionUserSubRepoStub()
	subRepo.seed(&UserSubscription{
		ID:        wrongSubscriptionID,
		UserID:    user.ID,
		GroupID:   groupID,
		PlanID:    &planA,
		StartsAt:  time.Now().Add(-24 * time.Hour),
		ExpiresAt: time.Now().Add(24 * time.Hour),
		Status:    SubscriptionStatusActive,
	})
	subRepo.seed(&UserSubscription{
		ID:        6,
		UserID:    user.ID,
		GroupID:   groupID,
		PlanID:    &planB,
		StartsAt:  time.Now().Add(-24 * time.Hour),
		ExpiresAt: time.Now().Add(48 * time.Hour),
		Status:    SubscriptionStatusActive,
	})
	svc := &PaymentService{
		entClient:       client,
		subscriptionSvc: NewSubscriptionService(&subscriptionGroupRepoStub{}, subRepo, nil, nil, nil),
	}

	plan, result, err := svc.PrepareRefund(ctx, order.ID, 0, "", false, true)

	require.NoError(t, err)
	require.Nil(t, plan)
	require.NotNil(t, result)
	require.True(t, result.RequireForce)
}

func TestPrepareRefundRequiresForceWhenSubscriptionOrderCannotBeMatchedExactly(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)

	user, err := client.User.Create().
		SetEmail("refund-unmatched@example.com").
		SetPasswordHash("hash").
		SetUsername("refund-unmatched-user").
		Save(ctx)
	require.NoError(t, err)

	inst, err := client.PaymentProviderInstance.Create().
		SetProviderKey(payment.TypeAlipay).
		SetName("alipay-refund-unmatched-instance").
		SetConfig("{}").
		SetSupportedTypes("alipay").
		SetEnabled(true).
		SetRefundEnabled(true).
		Save(ctx)
	require.NoError(t, err)

	days := 30
	order, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(88).
		SetPayAmount(88).
		SetFeeRate(0).
		SetRechargeCode("REFUND-UNMATCHED-ORDER").
		SetOutTradeNo("sub2_refund_unmatched_order").
		SetPaymentType(payment.TypeAlipay).
		SetPaymentTradeNo("trade-refund-unmatched").
		SetOrderType(payment.OrderTypeSubscription).
		SetStatus(OrderStatusCompleted).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetPaidAt(time.Now()).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		SetProviderInstanceID(strconv.FormatInt(inst.ID, 10)).
		SetProviderKey(payment.TypeAlipay).
		SetSubscriptionDays(days).
		Save(ctx)
	require.NoError(t, err)

	svc := &PaymentService{
		entClient:       client,
		subscriptionSvc: NewSubscriptionService(&subscriptionGroupRepoStub{}, newSubscriptionUserSubRepoStub(), nil, nil, nil),
	}

	plan, result, err := svc.PrepareRefund(ctx, order.ID, 0, "", false, true)
	require.NoError(t, err)
	require.Nil(t, plan)
	require.NotNil(t, result)
	require.True(t, result.RequireForce)

	plan, result, err = svc.PrepareRefund(ctx, order.ID, 0, "", true, true)
	require.NoError(t, err)
	require.Nil(t, result)
	require.NotNil(t, plan)
	require.Equal(t, payment.DeductionTypeSubscription, plan.DeductionType)
	require.Zero(t, plan.SubDaysToDeduct)
	require.Zero(t, plan.SubscriptionID)
}

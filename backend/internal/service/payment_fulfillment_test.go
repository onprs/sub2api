//go:build unit

package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/usersubscription"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type paymentFulfillmentTestProvider struct {
	key            string
	supportedTypes []payment.PaymentType
}

func (p paymentFulfillmentTestProvider) Name() string        { return p.key }
func (p paymentFulfillmentTestProvider) ProviderKey() string { return p.key }
func (p paymentFulfillmentTestProvider) SupportedTypes() []payment.PaymentType {
	return p.supportedTypes
}
func (p paymentFulfillmentTestProvider) CreatePayment(ctx context.Context, req payment.CreatePaymentRequest) (*payment.CreatePaymentResponse, error) {
	panic("unexpected call")
}
func (p paymentFulfillmentTestProvider) QueryOrder(ctx context.Context, tradeNo string) (*payment.QueryOrderResponse, error) {
	panic("unexpected call")
}
func (p paymentFulfillmentTestProvider) VerifyNotification(ctx context.Context, rawBody string, headers map[string]string) (*payment.PaymentNotification, error) {
	panic("unexpected call")
}
func (p paymentFulfillmentTestProvider) Refund(ctx context.Context, req payment.RefundRequest) (*payment.RefundResponse, error) {
	panic("unexpected call")
}

// ---------------------------------------------------------------------------
// resolveRedeemAction — pure idempotency decision logic
// ---------------------------------------------------------------------------

func TestResolveRedeemAction_CodeNotFound(t *testing.T) {
	t.Parallel()
	action := resolveRedeemAction(nil, nil)
	assert.Equal(t, redeemActionCreate, action, "nil code with nil error should create")
}

func TestResolveRedeemAction_LookupError(t *testing.T) {
	t.Parallel()
	action := resolveRedeemAction(nil, errors.New("db connection lost"))
	assert.Equal(t, redeemActionCreate, action, "lookup error should fall back to create")
}

func TestResolveRedeemAction_LookupErrorWithNonNilCode(t *testing.T) {
	t.Parallel()
	// Edge case: both code and error are non-nil (shouldn't happen in practice,
	// but the function should still treat error as authoritative)
	code := &RedeemCode{Status: StatusUnused}
	action := resolveRedeemAction(code, errors.New("partial error"))
	assert.Equal(t, redeemActionCreate, action, "non-nil error should always result in create regardless of code")
}

func TestResolveRedeemAction_CodeExistsAndUsed(t *testing.T) {
	t.Parallel()
	code := &RedeemCode{
		Code:   "test-code-123",
		Status: StatusUsed,
		Type:   RedeemTypeBalance,
		Value:  10.0,
	}
	action := resolveRedeemAction(code, nil)
	assert.Equal(t, redeemActionSkipCompleted, action, "used code should skip to completed")
}

func TestResolveRedeemAction_CodeExistsAndUnused(t *testing.T) {
	t.Parallel()
	code := &RedeemCode{
		Code:   "test-code-456",
		Status: StatusUnused,
		Type:   RedeemTypeBalance,
		Value:  25.0,
	}
	action := resolveRedeemAction(code, nil)
	assert.Equal(t, redeemActionRedeem, action, "unused code should skip creation and proceed to redeem")
}

func TestResolveRedeemAction_CodeExistsWithExpiredStatus(t *testing.T) {
	t.Parallel()
	// A code with a non-standard status (neither "unused" nor "used")
	// should NOT be treated as used, so it falls through to redeemActionRedeem.
	code := &RedeemCode{
		Code:   "expired-code",
		Status: StatusExpired,
	}
	action := resolveRedeemAction(code, nil)
	assert.Equal(t, redeemActionRedeem, action, "expired-status code is not IsUsed(), should redeem")
}

// ---------------------------------------------------------------------------
// Table-driven comprehensive test
// ---------------------------------------------------------------------------

func TestResolveRedeemAction_Table(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		code     *RedeemCode
		err      error
		expected redeemAction
	}{
		{
			name:     "nil code, nil error — first run",
			code:     nil,
			err:      nil,
			expected: redeemActionCreate,
		},
		{
			name:     "nil code, lookup error — treat as not found",
			code:     nil,
			err:      ErrRedeemCodeNotFound,
			expected: redeemActionCreate,
		},
		{
			name:     "nil code, generic DB error — treat as not found",
			code:     nil,
			err:      errors.New("connection refused"),
			expected: redeemActionCreate,
		},
		{
			name:     "code exists, used — previous run completed redeem",
			code:     &RedeemCode{Status: StatusUsed},
			err:      nil,
			expected: redeemActionSkipCompleted,
		},
		{
			name:     "code exists, unused — previous run created code but crashed before redeem",
			code:     &RedeemCode{Status: StatusUnused},
			err:      nil,
			expected: redeemActionRedeem,
		},
		{
			name:     "code exists but error also set — error takes precedence",
			code:     &RedeemCode{Status: StatusUsed},
			err:      errors.New("unexpected"),
			expected: redeemActionCreate,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := resolveRedeemAction(tt.code, tt.err)
			assert.Equal(t, tt.expected, got)
		})
	}
}

// ---------------------------------------------------------------------------
// redeemAction enum value sanity
// ---------------------------------------------------------------------------

func TestRedeemAction_DistinctValues(t *testing.T) {
	t.Parallel()
	// Ensure the three actions have distinct values (iota correctness)
	assert.NotEqual(t, redeemActionCreate, redeemActionRedeem)
	assert.NotEqual(t, redeemActionCreate, redeemActionSkipCompleted)
	assert.NotEqual(t, redeemActionRedeem, redeemActionSkipCompleted)
}

// ---------------------------------------------------------------------------
// RedeemCode.IsUsed / CanUse interaction with resolveRedeemAction
// ---------------------------------------------------------------------------

func TestResolveRedeemAction_IsUsedCanUseConsistency(t *testing.T) {
	t.Parallel()

	usedCode := &RedeemCode{Status: StatusUsed}
	unusedCode := &RedeemCode{Status: StatusUnused}

	// Verify our decision function is consistent with the domain model methods
	assert.True(t, usedCode.IsUsed())
	assert.False(t, usedCode.CanUse())
	assert.Equal(t, redeemActionSkipCompleted, resolveRedeemAction(usedCode, nil))

	assert.False(t, unusedCode.IsUsed())
	assert.True(t, unusedCode.CanUse())
	assert.Equal(t, redeemActionRedeem, resolveRedeemAction(unusedCode, nil))
}

func TestExpectedNotificationProviderKeyPrefersOrderInstanceProvider(t *testing.T) {
	t.Parallel()

	registry := payment.NewRegistry()
	registry.Register(paymentFulfillmentTestProvider{
		key:            payment.TypeAlipay,
		supportedTypes: []payment.PaymentType{payment.TypeAlipay},
	})

	assert.Equal(t,
		payment.TypeEasyPay,
		expectedNotificationProviderKey(registry, payment.TypeAlipay, "", payment.TypeEasyPay),
	)
}

func TestExpectedNotificationProviderKeyUsesRegistryMappingForLegacyOrders(t *testing.T) {
	t.Parallel()

	registry := payment.NewRegistry()
	registry.Register(paymentFulfillmentTestProvider{
		key:            payment.TypeEasyPay,
		supportedTypes: []payment.PaymentType{payment.TypeAlipay},
	})

	assert.Equal(t,
		payment.TypeEasyPay,
		expectedNotificationProviderKey(registry, payment.TypeAlipay, "", ""),
	)
}

func TestExpectedNotificationProviderKeyFallsBackToPaymentType(t *testing.T) {
	t.Parallel()

	assert.Equal(t,
		payment.TypeWxpay,
		expectedNotificationProviderKey(nil, payment.TypeWxpay, "", ""),
	)
}

func TestExpectedNotificationProviderKeyPrefersOrderSnapshotProviderKey(t *testing.T) {
	t.Parallel()

	registry := payment.NewRegistry()
	registry.Register(paymentFulfillmentTestProvider{
		key:            payment.TypeAlipay,
		supportedTypes: []payment.PaymentType{payment.TypeAlipay},
	})

	assert.Equal(t,
		payment.TypeEasyPay,
		expectedNotificationProviderKey(registry, payment.TypeAlipay, payment.TypeEasyPay, ""),
	)
}

func TestExpectedNotificationProviderKeyForOrderUsesSnapshotProviderKey(t *testing.T) {
	t.Parallel()

	registry := payment.NewRegistry()
	registry.Register(paymentFulfillmentTestProvider{
		key:            payment.TypeAlipay,
		supportedTypes: []payment.PaymentType{payment.TypeAlipay},
	})

	order := &dbent.PaymentOrder{
		PaymentType: payment.TypeAlipay,
		ProviderSnapshot: map[string]any{
			"schema_version": 1,
			"provider_key":   payment.TypeEasyPay,
		},
	}

	assert.Equal(t,
		payment.TypeEasyPay,
		expectedNotificationProviderKeyForOrder(registry, order, ""),
	)
}

func TestValidateProviderNotificationMetadataRejectsWxpaySnapshotMismatch(t *testing.T) {
	t.Parallel()

	order := &dbent.PaymentOrder{
		PaymentType: payment.TypeWxpay,
		ProviderSnapshot: map[string]any{
			"schema_version":  1,
			"merchant_app_id": "wx-app-expected",
			"merchant_id":     "mch-expected",
			"currency":        "CNY",
		},
	}

	err := validateProviderNotificationMetadata(order, payment.TypeWxpay, map[string]string{
		"appid":       "wx-app-other",
		"mchid":       "mch-expected",
		"currency":    "CNY",
		"trade_state": "SUCCESS",
	})
	assert.ErrorContains(t, err, "wxpay appid mismatch")
}

func TestValidateProviderNotificationMetadataAllowsLegacyOrdersWithoutSnapshotFields(t *testing.T) {
	t.Parallel()

	order := &dbent.PaymentOrder{
		PaymentType: payment.TypeWxpay,
		ProviderSnapshot: map[string]any{
			"schema_version":       1,
			"provider_instance_id": "9",
			"provider_key":         payment.TypeWxpay,
		},
	}

	err := validateProviderNotificationMetadata(order, payment.TypeWxpay, map[string]string{
		"appid":       "wx-app-runtime",
		"mchid":       "mch-runtime",
		"currency":    "CNY",
		"trade_state": "SUCCESS",
	})
	assert.NoError(t, err)
}

func TestParseLegacyPaymentOrderID(t *testing.T) {
	t.Parallel()

	oid, ok := parseLegacyPaymentOrderID("sub2_42", &dbent.NotFoundError{})
	assert.True(t, ok)
	assert.EqualValues(t, 42, oid)

	_, ok = parseLegacyPaymentOrderID("42", &dbent.NotFoundError{})
	assert.False(t, ok)

	_, ok = parseLegacyPaymentOrderID("sub2_42", errors.New("db down"))
	assert.False(t, ok)
}

func TestIsValidProviderAmount(t *testing.T) {
	t.Parallel()

	assert.True(t, isValidProviderAmount(0.01))
	assert.False(t, isValidProviderAmount(0))
	assert.False(t, isValidProviderAmount(-1))
	assert.False(t, isValidProviderAmount(math.NaN()))
	assert.False(t, isValidProviderAmount(math.Inf(1)))
}

func TestValidateProviderNotificationMetadataRejectsAlipaySnapshotMismatch(t *testing.T) {
	t.Parallel()

	order := &dbent.PaymentOrder{
		PaymentType: payment.TypeAlipay,
		ProviderSnapshot: map[string]any{
			"schema_version":  2,
			"merchant_app_id": "alipay-app-expected",
		},
	}

	err := validateProviderNotificationMetadata(order, payment.TypeAlipay, map[string]string{
		"app_id": "alipay-app-other",
	})
	assert.ErrorContains(t, err, "alipay app_id mismatch")
}

func TestValidateProviderNotificationMetadataRejectsEasyPaySnapshotMismatch(t *testing.T) {
	t.Parallel()

	order := &dbent.PaymentOrder{
		PaymentType: payment.TypeAlipay,
		ProviderSnapshot: map[string]any{
			"schema_version": 2,
			"merchant_id":    "pid-expected",
		},
	}

	err := validateProviderNotificationMetadata(order, payment.TypeEasyPay, map[string]string{
		"pid": "pid-other",
	})
	assert.ErrorContains(t, err, "easypay pid mismatch")
}

func TestValidateProviderNotificationMetadataRejectsAirwallexSnapshotMismatch(t *testing.T) {
	t.Parallel()

	order := &dbent.PaymentOrder{
		PaymentType: payment.TypeAirwallex,
		ProviderSnapshot: map[string]any{
			"schema_version": 2,
			"merchant_id":    "acct_expected",
			"currency":       "CNY",
		},
	}

	err := validateProviderNotificationMetadata(order, payment.TypeAirwallex, map[string]string{
		"account_id": "acct_other",
		"currency":   "CNY",
		"status":     "SUCCEEDED",
	})
	assert.ErrorContains(t, err, "airwallex account_id mismatch")

	err = validateProviderNotificationMetadata(order, payment.TypeAirwallex, map[string]string{
		"account_id": "acct_expected",
		"currency":   "USD",
		"status":     "SUCCEEDED",
	})
	assert.ErrorContains(t, err, "airwallex currency mismatch")
}

func TestValidateProviderNotificationMetadataRejectsStripeCurrencyMismatch(t *testing.T) {
	t.Parallel()

	order := &dbent.PaymentOrder{
		PaymentType: payment.TypeStripe,
		ProviderSnapshot: map[string]any{
			"schema_version": 2,
			"currency":       "HKD",
		},
	}

	err := validateProviderNotificationMetadata(order, payment.TypeStripe, map[string]string{
		"currency": "USD",
	})
	assert.ErrorContains(t, err, "stripe currency mismatch")
}

func TestPaymentAmountToleranceForThreeDecimalCurrency(t *testing.T) {
	t.Parallel()

	assert.Equal(t, amountToleranceCNY, paymentAmountToleranceForCurrency("CNY"))
	assert.Equal(t, amountToleranceCNY, paymentAmountToleranceForCurrency("JPY"))
	assert.InDelta(t, 0.0005, paymentAmountToleranceForCurrency("KWD"), 1e-12)
}

func TestDoSubSnapshotsRollingQuotaLimitsFromPurchasedOrder(t *testing.T) {
	ctx := context.Background()
	client := newOrderNotFoundTestClient(t)
	user := client.User.Create().
		SetEmail("quota-buyer@example.com").
		SetPasswordHash("hash").
		SetUsername("quota-buyer").
		SaveX(ctx)
	five := 1.25
	seven := 7.5
	thirty := 30.75
	plan := client.SubscriptionPlan.Create().
		SetGroupID(42).
		SetName("Quota Pro").
		SetPrice(9.99).
		SetValidityDays(30).
		SetValidityUnit("days").
		SetFiveHourLimitUsd(99).
		SetSevenDayLimitUsd(99).
		SetThirtyDayLimitUsd(99).
		SaveX(ctx)
	days := 30
	order := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(plan.Price).
		SetPayAmount(plan.Price).
		SetRechargeCode("PAY-ROLLING-QUOTA").
		SetOutTradeNo("sub2_rolling_quota").
		SetPaymentType(payment.TypeAlipay).
		SetPaymentTradeNo("trade_rolling_quota").
		SetOrderType(payment.OrderTypeSubscription).
		SetStatus(OrderStatusRecharging).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("example.com").
		SetPlanID(plan.ID).
		SetSubscriptionGroupID(plan.GroupID).
		SetSubscriptionDays(days).
		SetSubscriptionQuotaSnapshotVersion(1).
		SetNillableSubscriptionFiveHourLimitUsd(&five).
		SetNillableSubscriptionSevenDayLimitUsd(&seven).
		SetNillableSubscriptionThirtyDayLimitUsd(&thirty).
		SaveX(ctx)

	groupRepo := &subscriptionGroupRepoStub{
		group: &Group{
			ID:               plan.GroupID,
			Status:           payment.EntityStatusActive,
			SubscriptionType: SubscriptionTypeSubscription,
		},
	}
	subRepo := newSubscriptionUserSubRepoStub()
	subscriptionSvc := NewSubscriptionService(groupRepo, subRepo, nil, nil, nil)
	svc := &PaymentService{
		entClient:       client,
		groupRepo:       groupRepo,
		subscriptionSvc: subscriptionSvc,
	}

	require.NoError(t, svc.doSub(ctx, order))

	sub, err := subRepo.GetByUserIDGroupIDAndPlanID(ctx, user.ID, plan.GroupID, &plan.ID)
	require.NoError(t, err)
	require.Equal(t, plan.ID, *sub.PlanID)
	require.Equal(t, five, *sub.FiveHourLimitUSD)
	require.Equal(t, seven, *sub.SevenDayLimitUSD)
	require.Equal(t, thirty, *sub.ThirtyDayLimitUSD)
	require.Zero(t, sub.FiveHourUsageUSD)
	require.Nil(t, sub.FiveHourWindowStart)

	reloadedOrder, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.NotNil(t, reloadedOrder.SubscriptionID)
	require.Equal(t, sub.ID, *reloadedOrder.SubscriptionID)
}

func TestDoSubRollsBackSubscriptionWhenOrderCompletionFails(t *testing.T) {
	ctx := context.Background()
	client := newOrderNotFoundTestClient(t)
	user := client.User.Create().
		SetEmail("rollback-buyer@example.com").
		SetPasswordHash("hash").
		SetUsername("rollback-buyer").
		SaveX(ctx)

	group := client.Group.Create().
		SetName("Rollback Plan Group").
		SetStatus(payment.EntityStatusActive).
		SetSubscriptionType(SubscriptionTypeSubscription).
		SaveX(ctx)
	groupID := group.ID
	days := 30
	order := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(9.99).
		SetPayAmount(9.99).
		SetRechargeCode("PAY-SUB-ROLLBACK").
		SetOutTradeNo("sub2_subscription_rollback").
		SetPaymentType(payment.TypeAlipay).
		SetPaymentTradeNo("trade_subscription_rollback").
		SetOrderType(payment.OrderTypeSubscription).
		SetStatus(OrderStatusRecharging).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("example.com").
		SetSubscriptionGroupID(groupID).
		SetSubscriptionDays(days).
		SetSubscriptionQuotaSnapshotVersion(1).
		SaveX(ctx)

	failCompletion := true
	client.PaymentOrder.Use(func(next dbent.Mutator) dbent.Mutator {
		return dbent.MutateFunc(func(ctx context.Context, m dbent.Mutation) (dbent.Value, error) {
			pm, ok := m.(*dbent.PaymentOrderMutation)
			if ok && m.Op().Is(dbent.OpUpdate) {
				if status, exists := pm.Status(); exists && status == OrderStatusCompleted && failCompletion {
					return nil, errors.New("forced completed update failure")
				}
			}
			return next.Mutate(ctx, m)
		})
	})

	groupRepo := &subscriptionGroupRepoStub{
		group: &Group{
			ID:               groupID,
			Status:           payment.EntityStatusActive,
			SubscriptionType: SubscriptionTypeSubscription,
		},
	}
	subRepo := &paymentFulfillmentEntSubRepo{client: client}
	subscriptionSvc := NewSubscriptionService(groupRepo, subRepo, nil, nil, nil)
	svc := &PaymentService{
		entClient:       client,
		groupRepo:       groupRepo,
		subscriptionSvc: subscriptionSvc,
	}

	err := svc.doSub(ctx, order)

	require.ErrorContains(t, err, "forced completed update failure")
	count := client.UserSubscription.Query().
		Where(usersubscription.UserIDEQ(user.ID), usersubscription.GroupIDEQ(groupID)).
		CountX(ctx)
	require.Zero(t, count, "subscription entitlement must roll back when order completion fails")
	auditCount := client.PaymentAuditLog.Query().CountX(ctx)
	require.Zero(t, auditCount, "success audit must roll back with failed fulfillment")

	failCompletion = false
	require.NoError(t, svc.doSub(ctx, order))

	count = client.UserSubscription.Query().
		Where(usersubscription.UserIDEQ(user.ID), usersubscription.GroupIDEQ(groupID)).
		CountX(ctx)
	require.Equal(t, 1, count)
	reloaded, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusCompleted, reloaded.Status)
	auditCount = client.PaymentAuditLog.Query().CountX(ctx)
	require.Equal(t, 1, auditCount)
}

func TestValidateSubOrderRejectsSoldOutPlan(t *testing.T) {
	ctx := context.Background()
	client := newOrderNotFoundTestClient(t)
	stock := 0
	plan := client.SubscriptionPlan.Create().
		SetGroupID(42).
		SetName("Sold Out").
		SetPrice(9.99).
		SetValidityDays(30).
		SetValidityUnit("days").
		SetForSale(true).
		SetStock(stock).
		SaveX(ctx)
	groupRepo := &subscriptionGroupRepoStub{
		group: &Group{
			ID:               plan.GroupID,
			Status:           payment.EntityStatusActive,
			SubscriptionType: SubscriptionTypeSubscription,
		},
	}
	svc := &PaymentService{
		entClient:     client,
		configService: NewPaymentConfigService(client, nil, nil),
		groupRepo:     groupRepo,
	}

	_, err := svc.validateSubOrder(ctx, CreateOrderRequest{
		UserID:    100,
		OrderType: payment.OrderTypeSubscription,
		PlanID:    plan.ID,
	})

	require.Error(t, err)
	require.Equal(t, "PLAN_SOLD_OUT", infraerrors.Reason(err))
}

func TestDoSubConsumesFinitePlanStock(t *testing.T) {
	ctx := context.Background()
	client := newOrderNotFoundTestClient(t)
	plan, order, userID := createSubscriptionFulfillmentOrderWithStock(t, ctx, client, 1)
	svc := newPaymentFulfillmentStockTestService(client, plan)

	require.NoError(t, svc.doSub(ctx, order))

	reloadedPlan, err := client.SubscriptionPlan.Get(ctx, plan.ID)
	require.NoError(t, err)
	require.NotNil(t, reloadedPlan.Stock)
	require.Equal(t, 0, *reloadedPlan.Stock)
	count := client.UserSubscription.Query().
		Where(usersubscription.UserIDEQ(userID), usersubscription.GroupIDEQ(plan.GroupID)).
		CountX(ctx)
	require.Equal(t, 1, count)
}

func TestDoSubRejectsSoldOutPlanAndDoesNotCreateSubscription(t *testing.T) {
	ctx := context.Background()
	client := newOrderNotFoundTestClient(t)
	plan, order, userID := createSubscriptionFulfillmentOrderWithStock(t, ctx, client, 0)
	svc := newPaymentFulfillmentStockTestService(client, plan)

	err := svc.doSub(ctx, order)

	require.Error(t, err)
	require.Equal(t, "PLAN_SOLD_OUT", infraerrors.Reason(err))
	count := client.UserSubscription.Query().
		Where(usersubscription.UserIDEQ(userID), usersubscription.GroupIDEQ(plan.GroupID)).
		CountX(ctx)
	require.Zero(t, count)
}

func TestDoSubRollsBackStockWhenOrderCompletionFails(t *testing.T) {
	ctx := context.Background()
	client := newOrderNotFoundTestClient(t)
	plan, order, userID := createSubscriptionFulfillmentOrderWithStock(t, ctx, client, 1)
	failCompletion := true
	client.PaymentOrder.Use(func(next dbent.Mutator) dbent.Mutator {
		return dbent.MutateFunc(func(ctx context.Context, m dbent.Mutation) (dbent.Value, error) {
			pm, ok := m.(*dbent.PaymentOrderMutation)
			if ok && m.Op().Is(dbent.OpUpdate) {
				if status, exists := pm.Status(); exists && status == OrderStatusCompleted && failCompletion {
					return nil, errors.New("forced completed update failure")
				}
			}
			return next.Mutate(ctx, m)
		})
	})
	svc := newPaymentFulfillmentStockTestService(client, plan)

	err := svc.doSub(ctx, order)

	require.ErrorContains(t, err, "forced completed update failure")
	reloadedPlan, err := client.SubscriptionPlan.Get(ctx, plan.ID)
	require.NoError(t, err)
	require.NotNil(t, reloadedPlan.Stock)
	require.Equal(t, 1, *reloadedPlan.Stock, "stock decrement must roll back with failed order completion")
	count := client.UserSubscription.Query().
		Where(usersubscription.UserIDEQ(userID), usersubscription.GroupIDEQ(plan.GroupID)).
		CountX(ctx)
	require.Zero(t, count)
}

func TestDoSubSuccessRetryDoesNotConsumeStockAgain(t *testing.T) {
	ctx := context.Background()
	client := newOrderNotFoundTestClient(t)
	plan, order, _ := createSubscriptionFulfillmentOrderWithStock(t, ctx, client, 1)
	client.PaymentAuditLog.Create().
		SetOrderID(strconv.FormatInt(order.ID, 10)).
		SetAction("SUBSCRIPTION_SUCCESS").
		SetOperator("system").
		SaveX(ctx)
	svc := newPaymentFulfillmentStockTestService(client, plan)

	require.NoError(t, svc.doSub(ctx, order))

	reloadedPlan, err := client.SubscriptionPlan.Get(ctx, plan.ID)
	require.NoError(t, err)
	require.NotNil(t, reloadedPlan.Stock)
	require.Equal(t, 1, *reloadedPlan.Stock)
}

type paymentFulfillmentEntSubRepo struct {
	userSubRepoNoop
	client *dbent.Client
}

func (r *paymentFulfillmentEntSubRepo) entClient(ctx context.Context) *dbent.Client {
	if tx := dbent.TxFromContext(ctx); tx != nil {
		return tx.Client()
	}
	return r.client
}

func (r *paymentFulfillmentEntSubRepo) Create(ctx context.Context, sub *UserSubscription) error {
	created, err := r.entClient(ctx).UserSubscription.Create().
		SetUserID(sub.UserID).
		SetGroupID(sub.GroupID).
		SetStartsAt(sub.StartsAt).
		SetExpiresAt(sub.ExpiresAt).
		SetStatus(sub.Status).
		SetAssignedAt(sub.AssignedAt).
		SetNotes(sub.Notes).
		SetNillablePlanID(sub.PlanID).
		SetNillableFiveHourLimitUsd(sub.FiveHourLimitUSD).
		SetNillableSevenDayLimitUsd(sub.SevenDayLimitUSD).
		SetNillableThirtyDayLimitUsd(sub.ThirtyDayLimitUSD).
		Save(ctx)
	if err != nil {
		return err
	}
	sub.ID = created.ID
	return nil
}

func (r *paymentFulfillmentEntSubRepo) GetByID(ctx context.Context, id int64) (*UserSubscription, error) {
	sub, err := r.entClient(ctx).UserSubscription.Get(ctx, id)
	if err != nil {
		if dbent.IsNotFound(err) {
			return nil, ErrSubscriptionNotFound
		}
		return nil, err
	}
	return paymentFulfillmentEntSubToService(sub), nil
}

func (r *paymentFulfillmentEntSubRepo) GetByUserIDAndGroupID(ctx context.Context, userID, groupID int64) (*UserSubscription, error) {
	sub, err := r.entClient(ctx).UserSubscription.Query().
		Where(usersubscription.UserIDEQ(userID), usersubscription.GroupIDEQ(groupID)).
		Only(ctx)
	if err != nil {
		if dbent.IsNotFound(err) {
			return nil, ErrSubscriptionNotFound
		}
		return nil, err
	}
	return paymentFulfillmentEntSubToService(sub), nil
}

func (r *paymentFulfillmentEntSubRepo) GetByUserIDGroupIDAndPlanID(ctx context.Context, userID, groupID int64, planID *int64) (*UserSubscription, error) {
	query := r.entClient(ctx).UserSubscription.Query().
		Where(usersubscription.UserIDEQ(userID), usersubscription.GroupIDEQ(groupID))
	if planID == nil {
		query = query.Where(usersubscription.PlanIDIsNil())
	} else {
		query = query.Where(usersubscription.PlanIDEQ(*planID))
	}
	sub, err := query.Only(ctx)
	if err != nil {
		if dbent.IsNotFound(err) {
			return nil, ErrSubscriptionNotFound
		}
		return nil, err
	}
	return paymentFulfillmentEntSubToService(sub), nil
}

func paymentFulfillmentEntSubToService(sub *dbent.UserSubscription) *UserSubscription {
	if sub == nil {
		return nil
	}
	return &UserSubscription{
		ID:                   sub.ID,
		UserID:               sub.UserID,
		GroupID:              sub.GroupID,
		PlanID:               sub.PlanID,
		StartsAt:             sub.StartsAt,
		ExpiresAt:            sub.ExpiresAt,
		Status:               sub.Status,
		FiveHourLimitUSD:     sub.FiveHourLimitUsd,
		SevenDayLimitUSD:     sub.SevenDayLimitUsd,
		ThirtyDayLimitUSD:    sub.ThirtyDayLimitUsd,
		FiveHourUsageUSD:     sub.FiveHourUsageUsd,
		SevenDayUsageUSD:     sub.SevenDayUsageUsd,
		ThirtyDayUsageUSD:    sub.ThirtyDayUsageUsd,
		FiveHourWindowStart:  sub.FiveHourWindowStart,
		SevenDayWindowStart:  sub.SevenDayWindowStart,
		ThirtyDayWindowStart: sub.ThirtyDayWindowStart,
	}
}

func createSubscriptionFulfillmentOrderWithStock(t *testing.T, ctx context.Context, client *dbent.Client, stock int) (*dbent.SubscriptionPlan, *dbent.PaymentOrder, int64) {
	t.Helper()
	user := client.User.Create().
		SetEmail(fmt.Sprintf("stock-buyer-%d@example.com", time.Now().UnixNano())).
		SetPasswordHash("hash").
		SetUsername(fmt.Sprintf("stock-buyer-%d", time.Now().UnixNano())).
		SaveX(ctx)
	group := client.Group.Create().
		SetName(fmt.Sprintf("Stock Group %d", time.Now().UnixNano())).
		SetStatus(payment.EntityStatusActive).
		SetSubscriptionType(SubscriptionTypeSubscription).
		SaveX(ctx)
	plan := client.SubscriptionPlan.Create().
		SetGroupID(group.ID).
		SetName("Stocked Plan").
		SetPrice(9.99).
		SetValidityDays(30).
		SetValidityUnit("days").
		SetForSale(true).
		SetStock(stock).
		SaveX(ctx)
	days := 30
	order := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(plan.Price).
		SetPayAmount(plan.Price).
		SetRechargeCode(fmt.Sprintf("PAY-STOCK-%d", time.Now().UnixNano())).
		SetOutTradeNo(fmt.Sprintf("sub2_stock_%d", time.Now().UnixNano())).
		SetPaymentType(payment.TypeAlipay).
		SetPaymentTradeNo(fmt.Sprintf("trade_stock_%d", time.Now().UnixNano())).
		SetOrderType(payment.OrderTypeSubscription).
		SetStatus(OrderStatusRecharging).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("example.com").
		SetPlanID(plan.ID).
		SetSubscriptionGroupID(plan.GroupID).
		SetSubscriptionDays(days).
		SetSubscriptionQuotaSnapshotVersion(1).
		SaveX(ctx)
	return plan, order, user.ID
}

func newPaymentFulfillmentStockTestService(client *dbent.Client, plan *dbent.SubscriptionPlan) *PaymentService {
	groupRepo := &subscriptionGroupRepoStub{
		group: &Group{
			ID:               plan.GroupID,
			Status:           payment.EntityStatusActive,
			SubscriptionType: SubscriptionTypeSubscription,
		},
	}
	subRepo := &paymentFulfillmentEntSubRepo{client: client}
	return &PaymentService{
		entClient:       client,
		configService:   NewPaymentConfigService(client, nil, nil),
		groupRepo:       groupRepo,
		subscriptionSvc: NewSubscriptionService(groupRepo, subRepo, nil, nil, nil),
	}
}

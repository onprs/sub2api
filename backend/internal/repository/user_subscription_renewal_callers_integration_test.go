//go:build integration

package repository

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/paymentauditlog"
	"github.com/Wei-Shaw/sub2api/ent/paymentorder"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type concurrentRenewalFixture struct {
	client     *dbent.Client
	user       *dbent.User
	group      *dbent.Group
	plan       *dbent.SubscriptionPlan
	sub        *dbent.UserSubscription
	redeemCode *dbent.RedeemCode
	baseExpiry time.Time
}

func newConcurrentRenewalFixture(t *testing.T) *concurrentRenewalFixture {
	t.Helper()
	ctx := context.Background()
	client := testEntClient(t)
	suffix := time.Now().UnixNano()

	user, err := client.User.Create().
		SetEmail(fmt.Sprintf("renew-callers-%d@test.com", suffix)).
		SetPasswordHash("test-password-hash").
		SetStatus(service.StatusActive).
		SetRole(service.RoleUser).
		Save(ctx)
	require.NoError(t, err)

	group, err := client.Group.Create().
		SetName(fmt.Sprintf("g-renew-callers-%d", suffix)).
		SetStatus(service.StatusActive).
		SetSubscriptionType(service.SubscriptionTypeSubscription).
		Save(ctx)
	require.NoError(t, err)

	plan, err := client.SubscriptionPlan.Create().
		SetGroupID(group.ID).
		SetName(fmt.Sprintf("renew-plan-%d", suffix)).
		SetPrice(10).
		SetValidityDays(30).
		SetValidityUnit("day").
		Save(ctx)
	require.NoError(t, err)

	baseExpiry := time.Now().UTC().AddDate(0, 0, 10).Truncate(time.Microsecond)
	sub, err := client.UserSubscription.Create().
		SetUserID(user.ID).
		SetGroupID(group.ID).
		SetPlanID(plan.ID).
		SetStartsAt(baseExpiry.AddDate(0, 0, -20)).
		SetExpiresAt(baseExpiry).
		SetStatus(service.SubscriptionStatusActive).
		SetAssignedAt(time.Now()).
		SetNotes("").
		Save(ctx)
	require.NoError(t, err)

	fixture := &concurrentRenewalFixture{
		client:     client,
		user:       user,
		group:      group,
		plan:       plan,
		sub:        sub,
		baseExpiry: baseExpiry,
	}
	t.Cleanup(func() { fixture.cleanup(context.Background()) })
	return fixture
}

func (f *concurrentRenewalFixture) cleanup(ctx context.Context) {
	orders, _ := f.client.PaymentOrder.Query().
		Where(paymentorder.UserIDEQ(f.user.ID)).
		All(ctx)
	for _, order := range orders {
		_, _ = f.client.PaymentAuditLog.Delete().
			Where(paymentauditlog.OrderIDEQ(strconv.FormatInt(order.ID, 10))).
			Exec(ctx)
		_ = f.client.PaymentOrder.DeleteOneID(order.ID).Exec(ctx)
	}
	if f.redeemCode != nil {
		_ = f.client.RedeemCode.DeleteOneID(f.redeemCode.ID).Exec(ctx)
	}
	_ = f.client.UserSubscription.DeleteOneID(f.sub.ID).Exec(ctx)
	_ = f.client.SubscriptionPlan.DeleteOneID(f.plan.ID).Exec(ctx)
	_ = f.client.Group.DeleteOneID(f.group.ID).Exec(ctx)
	_ = f.client.User.DeleteOneID(f.user.ID).Exec(ctx)
}

func (f *concurrentRenewalFixture) createPaidOrder(t *testing.T, label string) *dbent.PaymentOrder {
	t.Helper()
	now := time.Now()
	order, err := f.client.PaymentOrder.Create().
		SetUserID(f.user.ID).
		SetUserEmail(f.user.Email).
		SetUserName(f.user.Username).
		SetAmount(10).
		SetPayAmount(10).
		SetFeeRate(0).
		SetRechargeCode("PAY-" + label).
		SetOutTradeNo(fmt.Sprintf("sub2_%s_%d", label, now.UnixNano())).
		SetPaymentType(payment.TypeAlipay).
		SetPaymentTradeNo("trade-" + label).
		SetOrderType(payment.OrderTypeSubscription).
		SetPlanID(f.plan.ID).
		SetSubscriptionGroupID(f.group.ID).
		SetSubscriptionDays(30).
		SetStatus(service.OrderStatusPaid).
		SetPaidAt(now).
		SetExpiresAt(now.Add(time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		Save(context.Background())
	require.NoError(t, err)
	return order
}

func (f *concurrentRenewalFixture) createRedeemCode(t *testing.T) *dbent.RedeemCode {
	t.Helper()
	code, err := f.client.RedeemCode.Create().
		SetCode(fmt.Sprintf("RENEW-%d", time.Now().UnixNano())).
		SetType(service.RedeemTypeSubscription).
		SetStatus(service.StatusUnused).
		SetGroupID(f.group.ID).
		SetSubscriptionPlanID(f.plan.ID).
		SetValidityDays(30).
		SetValue(0).
		SetNotes("concurrent renewal test").
		Save(context.Background())
	require.NoError(t, err)
	f.redeemCode = code
	return code
}

func (f *concurrentRenewalFixture) services() (*service.SubscriptionService, *service.PaymentService, *service.RedeemService) {
	groupRepo := NewGroupRepository(f.client, integrationDB)
	subscriptionSvc := service.NewSubscriptionService(
		groupRepo,
		NewUserSubscriptionRepository(f.client),
		nil,
		f.client,
		nil,
	)
	redeemSvc := service.NewRedeemService(
		NewRedeemCodeRepository(f.client),
		NewUserRepository(f.client, integrationDB),
		subscriptionSvc,
		nil,
		nil,
		f.client,
		nil,
		nil,
	)
	paymentSvc := service.NewPaymentService(
		f.client,
		nil,
		nil,
		redeemSvc,
		subscriptionSvc,
		nil,
		nil,
		groupRepo,
		nil,
	)
	return subscriptionSvc, paymentSvc, redeemSvc
}

func TestConcurrentPaymentFulfillmentsAccumulateSubscriptionTerm(t *testing.T) {
	fixture := newConcurrentRenewalFixture(t)
	first := fixture.createPaidOrder(t, "concurrent-first")
	second := fixture.createPaidOrder(t, "concurrent-second")
	_, paymentSvc, _ := fixture.services()

	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for _, orderID := range []int64{first.ID, second.ID} {
		wg.Add(1)
		go func(id int64) {
			defer wg.Done()
			<-start
			errs <- paymentSvc.ExecuteSubscriptionFulfillment(context.Background(), id)
		}(orderID)
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}

	renewed, err := NewUserSubscriptionRepository(fixture.client).GetByID(context.Background(), fixture.sub.ID)
	require.NoError(t, err)
	require.Equal(t, fixture.baseExpiry.AddDate(0, 0, 60), renewed.ExpiresAt)

	for _, order := range []*dbent.PaymentOrder{first, second} {
		reloaded, reloadErr := fixture.client.PaymentOrder.Get(context.Background(), order.ID)
		require.NoError(t, reloadErr)
		require.Equal(t, service.OrderStatusCompleted, reloaded.Status)
		for _, action := range []string{"SUBSCRIPTION_ASSIGNED", "SUBSCRIPTION_SUCCESS"} {
			count, countErr := fixture.client.PaymentAuditLog.Query().
				Where(
					paymentauditlog.OrderIDEQ(strconv.FormatInt(order.ID, 10)),
					paymentauditlog.ActionEQ(action),
				).
				Count(context.Background())
			require.NoError(t, countErr)
			require.Equal(t, 1, count, "order %d action %s", order.ID, action)
		}
	}
}

func TestConcurrentPaymentAndRedeemRenewalsAccumulateSubscriptionTerm(t *testing.T) {
	fixture := newConcurrentRenewalFixture(t)
	order := fixture.createPaidOrder(t, "payment-redeem")
	code := fixture.createRedeemCode(t)
	_, paymentSvc, redeemSvc := fixture.services()

	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		errs <- paymentSvc.ExecuteSubscriptionFulfillment(context.Background(), order.ID)
	}()
	go func() {
		defer wg.Done()
		<-start
		_, err := redeemSvc.Redeem(context.Background(), fixture.user.ID, code.Code)
		errs <- err
	}()
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}

	renewed, err := NewUserSubscriptionRepository(fixture.client).GetByID(context.Background(), fixture.sub.ID)
	require.NoError(t, err)
	require.Equal(t, fixture.baseExpiry.AddDate(0, 0, 60), renewed.ExpiresAt)
	require.Contains(t, renewed.Notes, fmt.Sprintf("payment order %d", order.ID))
	require.Contains(t, renewed.Notes, code.Code)

	reloadedCode, err := fixture.client.RedeemCode.Get(context.Background(), code.ID)
	require.NoError(t, err)
	require.Equal(t, service.StatusUsed, reloadedCode.Status)
	require.NotNil(t, reloadedCode.UsedBy)
	require.Equal(t, fixture.user.ID, *reloadedCode.UsedBy)
}

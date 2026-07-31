//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type redeemPlanSnapshotUserSubRepo struct {
	userSubRepoNoop

	sub              *UserSubscription
	requestedUserID  int64
	requestedGroupID int64
	requestedPlanID  *int64
	renewInputs      []*RenewSubscriptionTermInput
}

func (r *redeemPlanSnapshotUserSubRepo) GetByUserIDAndGroupID(_ context.Context, userID, groupID int64) (*UserSubscription, error) {
	return r.GetByUserIDGroupIDAndPlanID(context.Background(), userID, groupID, nil)
}

func (r *redeemPlanSnapshotUserSubRepo) GetByUserIDGroupIDAndPlanID(_ context.Context, userID, groupID int64, planID *int64) (*UserSubscription, error) {
	r.requestedUserID = userID
	r.requestedGroupID = groupID
	r.requestedPlanID = planID
	if r.sub == nil || r.sub.UserID != userID || r.sub.GroupID != groupID {
		return nil, ErrSubscriptionNotFound
	}
	if (r.sub.PlanID == nil) != (planID == nil) {
		return nil, ErrSubscriptionNotFound
	}
	if r.sub.PlanID != nil && *r.sub.PlanID != *planID {
		return nil, ErrSubscriptionNotFound
	}
	cp := *r.sub
	return &cp, nil
}

func (r *redeemPlanSnapshotUserSubRepo) RenewTerm(_ context.Context, input *RenewSubscriptionTermInput) (*UserSubscription, error) {
	inputCopy := *input
	r.renewInputs = append(r.renewInputs, &inputCopy)

	r.sub.ExpiresAt = r.sub.ExpiresAt.AddDate(0, 0, input.ValidityDays)
	if input.HasRollingQuotaSnapshot {
		r.sub.FiveHourLimitUSD = input.FiveHourLimitUSD
		r.sub.SevenDayLimitUSD = input.SevenDayLimitUSD
		r.sub.ThirtyDayLimitUSD = input.ThirtyDayLimitUSD
	}
	subscriptionCopy := *r.sub
	return &subscriptionCopy, nil
}

func (r *redeemPlanSnapshotUserSubRepo) GetByID(_ context.Context, id int64) (*UserSubscription, error) {
	if r.sub == nil || r.sub.ID != id {
		return nil, ErrSubscriptionNotFound
	}
	cp := *r.sub
	return &cp, nil
}

func TestRedeemService_RedeemSubscriptionPlanSnapshotAppliesRollingLimits(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)

	const userID int64 = 100
	five := 1.0
	seven := 7.0
	thirty := 30.0
	planID := int64(9)
	groupID := int64(20)
	code := &RedeemCode{
		ID:                               1,
		Code:                             "PLAN-CODE",
		Type:                             RedeemTypeSubscription,
		Status:                           StatusUnused,
		GroupID:                          &groupID,
		ValidityDays:                     30,
		SubscriptionPlanID:               &planID,
		SubscriptionQuotaSnapshotVersion: 1,
		FiveHourLimitUSD:                 &five,
		SevenDayLimitUSD:                 &seven,
		ThirtyDayLimitUSD:                &thirty,
	}

	redeemRepo := &paymentOrderLifecycleRedeemRepo{
		codesByCode: map[string]*RedeemCode{
			code.Code: code,
		},
	}
	userRepo := &mockUserRepo{
		getByIDUser: &User{ID: userID, Email: "plan@example.com", Username: "plan-user"},
	}
	subRepo := &redeemPlanSnapshotUserSubRepo{
		sub: &UserSubscription{
			ID:        77,
			UserID:    userID,
			GroupID:   groupID,
			PlanID:    &planID,
			StartsAt:  time.Now().Add(-24 * time.Hour),
			ExpiresAt: time.Now().Add(24 * time.Hour),
			Status:    SubscriptionStatusActive,
		},
	}
	subscriptionSvc := NewSubscriptionService(
		&subscriptionGroupRepoStub{group: &Group{ID: groupID, SubscriptionType: SubscriptionTypeSubscription}},
		subRepo,
		nil,
		nil,
		nil,
	)
	svc := NewRedeemService(redeemRepo, userRepo, subscriptionSvc, nil, nil, client, nil, nil)

	_, err := svc.Redeem(ctx, userID, code.Code)
	require.NoError(t, err)

	require.Equal(t, userID, subRepo.requestedUserID)
	require.Equal(t, groupID, subRepo.requestedGroupID)
	require.NotNil(t, subRepo.requestedPlanID)
	require.Equal(t, planID, *subRepo.requestedPlanID)
	require.Len(t, subRepo.renewInputs, 1)
	got := subRepo.renewInputs[0]
	require.Equal(t, int64(77), got.SubscriptionID)
	require.Equal(t, 30, got.ValidityDays)
	require.True(t, got.HasRollingQuotaSnapshot)
	require.NotNil(t, got.FiveHourLimitUSD)
	require.Equal(t, five, *got.FiveHourLimitUSD)
	require.NotNil(t, got.SevenDayLimitUSD)
	require.Equal(t, seven, *got.SevenDayLimitUSD)
	require.NotNil(t, got.ThirtyDayLimitUSD)
	require.Equal(t, thirty, *got.ThirtyDayLimitUSD)
}

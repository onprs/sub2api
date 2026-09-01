package service

import (
	"context"
	"testing"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func TestAssignSubscriptionPlanSnapshotsPlanQuotas(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	groupID := int64(42)
	five := 5.0
	seven := 70.0
	thirty := 300.0
	plan, err := client.SubscriptionPlan.Create().
		SetGroupID(groupID).
		SetName("Codex Plus 7d").
		SetDescription("7 day plan").
		SetPrice(19.9).
		SetValidityDays(1).
		SetValidityUnit("weeks").
		SetFeatures("[]").
		SetProductName("Codex Plus").
		SetForSale(true).
		SetSortOrder(1).
		SetFiveHourLimitUsd(five).
		SetSevenDayLimitUsd(seven).
		SetThirtyDayLimitUsd(thirty).
		Save(ctx)
	require.NoError(t, err)

	subRepo := newSubscriptionUserSubRepoStub()
	svc := NewSubscriptionService(&subscriptionGroupRepoStub{
		group: &Group{ID: groupID, SubscriptionType: SubscriptionTypeSubscription, Status: StatusActive},
	}, subRepo, nil, client, nil)

	sub, err := svc.AssignSubscriptionPlan(ctx, &AssignSubscriptionPlanInput{
		UserID:             1001,
		SubscriptionPlanID: plan.ID,
		AssignedBy:         9,
		Notes:              "manual plan grant",
	})

	require.NoError(t, err)
	require.Equal(t, int64(1001), sub.UserID)
	require.Equal(t, groupID, sub.GroupID)
	require.NotNil(t, sub.PlanID)
	require.Equal(t, plan.ID, *sub.PlanID)
	require.Equal(t, five, *sub.FiveHourLimitUSD)
	require.Equal(t, seven, *sub.SevenDayLimitUSD)
	require.Equal(t, thirty, *sub.ThirtyDayLimitUSD)
	require.WithinDuration(t, sub.StartsAt.AddDate(0, 0, 7), sub.ExpiresAt, time.Second)
	require.Equal(t, 1, subRepo.createCalls)
}

func TestBulkAssignSubscriptionPlanSnapshotsPlanQuotas(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	groupID := int64(43)
	five := 2.5
	seven := 25.0
	thirty := 100.0
	plan, err := client.SubscriptionPlan.Create().
		SetGroupID(groupID).
		SetName("Codex Plus 30d").
		SetDescription("30 day plan").
		SetPrice(49.9).
		SetValidityDays(1).
		SetValidityUnit("month").
		SetFeatures("[]").
		SetProductName("Codex Plus").
		SetForSale(true).
		SetSortOrder(1).
		SetFiveHourLimitUsd(five).
		SetSevenDayLimitUsd(seven).
		SetThirtyDayLimitUsd(thirty).
		Save(ctx)
	require.NoError(t, err)

	subRepo := newSubscriptionUserSubRepoStub()
	svc := NewSubscriptionService(&subscriptionGroupRepoStub{
		group: &Group{ID: groupID, SubscriptionType: SubscriptionTypeSubscription, Status: StatusActive},
	}, subRepo, nil, client, nil)

	result, err := svc.BulkAssignSubscriptionPlan(ctx, &BulkAssignSubscriptionPlanInput{
		UserIDs:            []int64{1, 2},
		SubscriptionPlanID: plan.ID,
		AssignedBy:         9,
		Notes:              "manual bulk plan grant",
	})

	require.NoError(t, err)
	require.Equal(t, 2, result.SuccessCount)
	require.Equal(t, 2, result.CreatedCount)
	require.Equal(t, 0, result.FailedCount)
	require.Equal(t, 2, subRepo.createCalls)
	for _, sub := range result.Subscriptions {
		require.Equal(t, groupID, sub.GroupID)
		require.NotNil(t, sub.PlanID)
		require.Equal(t, plan.ID, *sub.PlanID)
		require.Equal(t, five, *sub.FiveHourLimitUSD)
		require.Equal(t, seven, *sub.SevenDayLimitUSD)
		require.Equal(t, thirty, *sub.ThirtyDayLimitUSD)
		require.WithinDuration(t, sub.StartsAt.AddDate(0, 0, 30), sub.ExpiresAt, time.Second)
	}
}

func TestAssignSubscriptionPlanMissingPlanReturnsNotFound(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	svc := NewSubscriptionService(&subscriptionGroupRepoStub{
		group: &Group{ID: 1, SubscriptionType: SubscriptionTypeSubscription, Status: StatusActive},
	}, newSubscriptionUserSubRepoStub(), nil, client, nil)

	_, err := svc.AssignSubscriptionPlan(ctx, &AssignSubscriptionPlanInput{
		UserID:             1001,
		SubscriptionPlanID: 9999,
		AssignedBy:         9,
	})

	require.Error(t, err)
	require.Equal(t, 404, infraerrors.Code(err))
	require.Equal(t, "PLAN_NOT_FOUND", infraerrors.Reason(err))
}

func TestAssignSubscriptionPlanConflictsWhenExistingSnapshotDiffers(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	groupID := int64(44)
	planFive := 5.0
	planSeven := 70.0
	planThirty := 300.0
	plan, err := client.SubscriptionPlan.Create().
		SetGroupID(groupID).
		SetName("Codex Plus 7d").
		SetDescription("7 day plan").
		SetPrice(19.9).
		SetValidityDays(7).
		SetValidityUnit("days").
		SetFeatures("[]").
		SetProductName("Codex Plus").
		SetForSale(true).
		SetSortOrder(1).
		SetFiveHourLimitUsd(planFive).
		SetSevenDayLimitUsd(planSeven).
		SetThirtyDayLimitUsd(planThirty).
		Save(ctx)
	require.NoError(t, err)

	start := time.Now().Add(-time.Hour)
	oldFive := 1.0
	subRepo := newSubscriptionUserSubRepoStub()
	subRepo.seed(&UserSubscription{
		ID:                50,
		UserID:            1001,
		GroupID:           groupID,
		PlanID:            &plan.ID,
		StartsAt:          start,
		ExpiresAt:         start.AddDate(0, 0, 7),
		Status:            SubscriptionStatusActive,
		FiveHourLimitUSD:  &oldFive,
		SevenDayLimitUSD:  &planSeven,
		ThirtyDayLimitUSD: &planThirty,
		Notes:             "manual plan grant",
	})
	svc := NewSubscriptionService(&subscriptionGroupRepoStub{
		group: &Group{ID: groupID, SubscriptionType: SubscriptionTypeSubscription, Status: StatusActive},
	}, subRepo, nil, client, nil)

	_, err = svc.AssignSubscriptionPlan(ctx, &AssignSubscriptionPlanInput{
		UserID:             1001,
		SubscriptionPlanID: plan.ID,
		AssignedBy:         9,
		Notes:              "manual plan grant",
	})

	require.Error(t, err)
	require.Equal(t, "SUBSCRIPTION_ASSIGN_CONFLICT", infraerrors.Reason(err))
	appErr := infraerrors.FromError(err)
	require.Equal(t, "five_hour_limit_mismatch", appErr.Metadata["conflict_reason"])
	require.Equal(t, 0, subRepo.createCalls)
}

func TestBulkAssignSubscriptionPlanCreatedReusedAndSnapshotConflict(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	groupID := int64(45)
	five := 5.0
	seven := 70.0
	thirty := 300.0
	plan, err := client.SubscriptionPlan.Create().
		SetGroupID(groupID).
		SetName("Codex Plus 7d").
		SetDescription("7 day plan").
		SetPrice(19.9).
		SetValidityDays(7).
		SetValidityUnit("days").
		SetFeatures("[]").
		SetProductName("Codex Plus").
		SetForSale(true).
		SetSortOrder(1).
		SetFiveHourLimitUsd(five).
		SetSevenDayLimitUsd(seven).
		SetThirtyDayLimitUsd(thirty).
		Save(ctx)
	require.NoError(t, err)

	start := time.Now().Add(-time.Hour)
	oldFive := 1.0
	subRepo := newSubscriptionUserSubRepoStub()
	subRepo.seed(&UserSubscription{
		ID:                61,
		UserID:            1,
		GroupID:           groupID,
		PlanID:            &plan.ID,
		StartsAt:          start,
		ExpiresAt:         start.AddDate(0, 0, 7),
		Status:            SubscriptionStatusActive,
		FiveHourLimitUSD:  &five,
		SevenDayLimitUSD:  &seven,
		ThirtyDayLimitUSD: &thirty,
		Notes:             "same-note",
	})
	subRepo.seed(&UserSubscription{
		ID:                63,
		UserID:            3,
		GroupID:           groupID,
		PlanID:            &plan.ID,
		StartsAt:          start,
		ExpiresAt:         start.AddDate(0, 0, 7),
		Status:            SubscriptionStatusActive,
		FiveHourLimitUSD:  &oldFive,
		SevenDayLimitUSD:  &seven,
		ThirtyDayLimitUSD: &thirty,
		Notes:             "same-note",
	})
	svc := NewSubscriptionService(&subscriptionGroupRepoStub{
		group: &Group{ID: groupID, SubscriptionType: SubscriptionTypeSubscription, Status: StatusActive},
	}, subRepo, nil, client, nil)

	result, err := svc.BulkAssignSubscriptionPlan(ctx, &BulkAssignSubscriptionPlanInput{
		UserIDs:            []int64{1, 2, 3},
		SubscriptionPlanID: plan.ID,
		AssignedBy:         9,
		Notes:              "same-note",
	})

	require.NoError(t, err)
	require.Equal(t, 2, result.SuccessCount)
	require.Equal(t, 1, result.CreatedCount)
	require.Equal(t, 1, result.ReusedCount)
	require.Equal(t, 1, result.FailedCount)
	require.Equal(t, "reused", result.Statuses[1])
	require.Equal(t, "created", result.Statuses[2])
	require.Equal(t, "failed", result.Statuses[3])
	require.Len(t, result.Errors, 1)
	require.Contains(t, result.Errors[0], "five_hour_limit_mismatch")
	require.Equal(t, 1, subRepo.createCalls)
}

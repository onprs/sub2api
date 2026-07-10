package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAssignOrExtendSubscription_NewSubscriptionSnapshotsRollingLimits(t *testing.T) {
	five := 1.5
	seven := 7.0
	thirty := 30.0
	groupRepo := &subscriptionGroupRepoStub{
		group: &Group{ID: 2, SubscriptionType: SubscriptionTypeSubscription, Status: StatusActive},
	}
	subRepo := newSubscriptionUserSubRepoStub()
	svc := NewSubscriptionService(groupRepo, subRepo, nil, nil, nil)

	sub, reused, err := svc.AssignOrExtendSubscription(context.Background(), &AssignSubscriptionInput{
		UserID: 10, GroupID: 2, ValidityDays: 30,
		FiveHourLimitUSD: &five, SevenDayLimitUSD: &seven, ThirtyDayLimitUSD: &thirty,
		HasRollingQuotaSnapshot: true,
	})

	require.NoError(t, err)
	require.False(t, reused)
	require.Equal(t, five, *sub.FiveHourLimitUSD)
	require.Equal(t, seven, *sub.SevenDayLimitUSD)
	require.Equal(t, thirty, *sub.ThirtyDayLimitUSD)
	require.Zero(t, sub.FiveHourUsageUSD)
	require.Zero(t, sub.SevenDayUsageUSD)
	require.Zero(t, sub.ThirtyDayUsageUSD)
	require.Nil(t, sub.FiveHourWindowStart)
	require.Nil(t, sub.SevenDayWindowStart)
	require.Nil(t, sub.ThirtyDayWindowStart)
}

func TestAssignOrExtendSubscription_DifferentPlansSameGroupCreateIndependentSubscriptions(t *testing.T) {
	planA := int64(101)
	planB := int64(102)
	groupRepo := &subscriptionGroupRepoStub{
		group: &Group{ID: 2, SubscriptionType: SubscriptionTypeSubscription, Status: StatusActive},
	}
	subRepo := newSubscriptionUserSubRepoStub()
	svc := NewSubscriptionService(groupRepo, subRepo, nil, nil, nil)

	first, reused, err := svc.AssignOrExtendSubscription(context.Background(), &AssignSubscriptionInput{
		UserID: 10, GroupID: 2, PlanID: &planA, ValidityDays: 30,
		HasRollingQuotaSnapshot: true,
	})

	require.NoError(t, err)
	require.False(t, reused)
	require.NotNil(t, first.PlanID)
	require.Equal(t, planA, *first.PlanID)

	second, reused, err := svc.AssignOrExtendSubscription(context.Background(), &AssignSubscriptionInput{
		UserID: 10, GroupID: 2, PlanID: &planB, ValidityDays: 30,
		HasRollingQuotaSnapshot: true,
	})

	require.NoError(t, err)
	require.False(t, reused)
	require.NotEqual(t, first.ID, second.ID)
	require.NotNil(t, second.PlanID)
	require.Equal(t, planB, *second.PlanID)
}

func TestGetUsableActiveSubscriptionSkipsExhaustedEarlierPlan(t *testing.T) {
	now := time.Now()
	limit := 1.0
	planA := int64(101)
	planB := int64(102)
	group := &Group{ID: 2, SubscriptionType: SubscriptionTypeSubscription, Status: StatusActive}
	groupRepo := &subscriptionGroupRepoStub{group: group}
	subRepo := newSubscriptionUserSubRepoStub()
	subRepo.seed(&UserSubscription{
		ID:                  5,
		UserID:              10,
		GroupID:             2,
		PlanID:              &planA,
		StartsAt:            now.Add(-24 * time.Hour),
		ExpiresAt:           now.Add(24 * time.Hour),
		Status:              SubscriptionStatusActive,
		FiveHourLimitUSD:    &limit,
		FiveHourUsageUSD:    limit,
		FiveHourWindowStart: &now,
	})
	subRepo.seed(&UserSubscription{
		ID:                  6,
		UserID:              10,
		GroupID:             2,
		PlanID:              &planB,
		StartsAt:            now.Add(-24 * time.Hour),
		ExpiresAt:           now.Add(48 * time.Hour),
		Status:              SubscriptionStatusActive,
		FiveHourLimitUSD:    &limit,
		FiveHourUsageUSD:    0,
		FiveHourWindowStart: &now,
	})
	svc := NewSubscriptionService(groupRepo, subRepo, nil, nil, nil)

	selected, _, err := svc.GetUsableActiveSubscription(context.Background(), 10, 2, group)

	require.NoError(t, err)
	require.Equal(t, int64(6), selected.ID)
	require.NotNil(t, selected.PlanID)
	require.Equal(t, planB, *selected.PlanID)
}

func TestAssignOrExtendSubscription_SamePlanRenewsSameSubscription(t *testing.T) {
	planID := int64(101)
	groupRepo := &subscriptionGroupRepoStub{
		group: &Group{ID: 2, SubscriptionType: SubscriptionTypeSubscription, Status: StatusActive},
	}
	subRepo := newSubscriptionUserSubRepoStub()
	svc := NewSubscriptionService(groupRepo, subRepo, nil, nil, nil)

	first, reused, err := svc.AssignOrExtendSubscription(context.Background(), &AssignSubscriptionInput{
		UserID: 10, GroupID: 2, PlanID: &planID, ValidityDays: 30,
		HasRollingQuotaSnapshot: true,
	})

	require.NoError(t, err)
	require.False(t, reused)

	second, reused, err := svc.AssignOrExtendSubscription(context.Background(), &AssignSubscriptionInput{
		UserID: 10, GroupID: 2, PlanID: &planID, ValidityDays: 7,
		HasRollingQuotaSnapshot: true,
	})

	require.NoError(t, err)
	require.True(t, reused)
	require.Equal(t, first.ID, second.ID)
}

func TestAssignOrExtendSubscription_ActiveRenewalRefreshesRollingLimitSnapshot(t *testing.T) {
	oldLimit := 1.0
	newLimit := 2.0
	start := time.Now().Add(-time.Hour)
	groupRepo := &subscriptionGroupRepoStub{
		group: &Group{ID: 2, SubscriptionType: SubscriptionTypeSubscription, Status: StatusActive},
	}
	subRepo := newSubscriptionUserSubRepoStub()
	oldExpiresAt := time.Now().Add(24 * time.Hour)
	subRepo.seed(&UserSubscription{
		ID:                   5,
		UserID:               10,
		GroupID:              2,
		StartsAt:             start.Add(-24 * time.Hour),
		ExpiresAt:            oldExpiresAt,
		Status:               SubscriptionStatusActive,
		FiveHourLimitUSD:     &oldLimit,
		SevenDayLimitUSD:     &oldLimit,
		ThirtyDayLimitUSD:    &oldLimit,
		FiveHourUsageUSD:     0.5,
		SevenDayUsageUSD:     0.6,
		ThirtyDayUsageUSD:    0.7,
		FiveHourWindowStart:  &start,
		SevenDayWindowStart:  &start,
		ThirtyDayWindowStart: &start,
	})

	svc := NewSubscriptionService(groupRepo, subRepo, nil, nil, nil)
	sub, reused, err := svc.AssignOrExtendSubscription(context.Background(), &AssignSubscriptionInput{
		UserID: 10, GroupID: 2, ValidityDays: 30,
		FiveHourLimitUSD: &newLimit, SevenDayLimitUSD: &newLimit, ThirtyDayLimitUSD: &newLimit,
		HasRollingQuotaSnapshot: true,
	})

	require.NoError(t, err)
	require.True(t, reused)
	require.Equal(t, newLimit, *sub.FiveHourLimitUSD)
	require.Equal(t, newLimit, *sub.SevenDayLimitUSD)
	require.Equal(t, newLimit, *sub.ThirtyDayLimitUSD)
	require.Equal(t, 0.5, sub.FiveHourUsageUSD)
	require.Equal(t, 0.6, sub.SevenDayUsageUSD)
	require.Equal(t, 0.7, sub.ThirtyDayUsageUSD)
	require.Equal(t, start, *sub.FiveHourWindowStart)
	require.WithinDuration(t, oldExpiresAt.AddDate(0, 0, 30), sub.ExpiresAt, time.Second)
}

func TestAssignOrExtendSubscription_LegacyRedeemRenewalPreservesRollingLimitSnapshot(t *testing.T) {
	oldLimit := 4.0
	now := time.Now()
	groupRepo := &subscriptionGroupRepoStub{
		group: &Group{ID: 2, SubscriptionType: SubscriptionTypeSubscription, Status: StatusActive},
	}
	subRepo := newSubscriptionUserSubRepoStub()
	subRepo.seed(&UserSubscription{
		ID:                5,
		UserID:            10,
		GroupID:           2,
		StartsAt:          now.Add(-24 * time.Hour),
		ExpiresAt:         now.Add(24 * time.Hour),
		Status:            SubscriptionStatusActive,
		FiveHourLimitUSD:  &oldLimit,
		SevenDayLimitUSD:  &oldLimit,
		ThirtyDayLimitUSD: &oldLimit,
	})

	svc := NewSubscriptionService(groupRepo, subRepo, nil, nil, nil)
	sub, reused, err := svc.AssignOrExtendSubscription(context.Background(), &AssignSubscriptionInput{
		UserID: 10, GroupID: 2, ValidityDays: 7, Notes: "legacy redeem",
	})

	require.NoError(t, err)
	require.True(t, reused)
	require.Equal(t, oldLimit, *sub.FiveHourLimitUSD)
	require.Equal(t, oldLimit, *sub.SevenDayLimitUSD)
	require.Equal(t, oldLimit, *sub.ThirtyDayLimitUSD)
}

func TestAssignOrExtendSubscription_PlanSnapshotCanClearRollingLimitsToUnlimited(t *testing.T) {
	oldLimit := 4.0
	now := time.Now()
	groupRepo := &subscriptionGroupRepoStub{
		group: &Group{ID: 2, SubscriptionType: SubscriptionTypeSubscription, Status: StatusActive},
	}
	subRepo := newSubscriptionUserSubRepoStub()
	subRepo.seed(&UserSubscription{
		ID:                5,
		UserID:            10,
		GroupID:           2,
		StartsAt:          now.Add(-24 * time.Hour),
		ExpiresAt:         now.Add(24 * time.Hour),
		Status:            SubscriptionStatusActive,
		FiveHourLimitUSD:  &oldLimit,
		SevenDayLimitUSD:  &oldLimit,
		ThirtyDayLimitUSD: &oldLimit,
	})

	svc := NewSubscriptionService(groupRepo, subRepo, nil, nil, nil)
	sub, reused, err := svc.AssignOrExtendSubscription(context.Background(), &AssignSubscriptionInput{
		UserID: 10, GroupID: 2, ValidityDays: 7, Notes: "plan redeem",
		HasRollingQuotaSnapshot: true,
	})

	require.NoError(t, err)
	require.True(t, reused)
	require.Nil(t, sub.FiveHourLimitUSD)
	require.Nil(t, sub.SevenDayLimitUSD)
	require.Nil(t, sub.ThirtyDayLimitUSD)
}

func TestAssignOrExtendSubscription_CreateConflictFallsBackToRenewal(t *testing.T) {
	oldLimit := 1.0
	newLimit := 3.0
	now := time.Now()
	oldExpiresAt := now.Add(10 * 24 * time.Hour)
	groupRepo := &subscriptionGroupRepoStub{
		group: &Group{ID: 2, SubscriptionType: SubscriptionTypeSubscription, Status: StatusActive},
	}
	subRepo := newSubscriptionUserSubRepoStub()
	subRepo.createConflictSub = &UserSubscription{
		ID:                   7,
		UserID:               10,
		GroupID:              2,
		StartsAt:             now.Add(-24 * time.Hour),
		ExpiresAt:            oldExpiresAt,
		Status:               SubscriptionStatusActive,
		FiveHourLimitUSD:     &oldLimit,
		SevenDayLimitUSD:     &oldLimit,
		ThirtyDayLimitUSD:    &oldLimit,
		FiveHourUsageUSD:     0.25,
		SevenDayUsageUSD:     0.50,
		ThirtyDayUsageUSD:    0.75,
		FiveHourWindowStart:  &now,
		SevenDayWindowStart:  &now,
		ThirtyDayWindowStart: &now,
	}

	svc := NewSubscriptionService(groupRepo, subRepo, nil, nil, nil)
	sub, reused, err := svc.AssignOrExtendSubscription(context.Background(), &AssignSubscriptionInput{
		UserID: 10, GroupID: 2, ValidityDays: 14, Notes: "payment order 2",
		FiveHourLimitUSD: &newLimit, SevenDayLimitUSD: &newLimit, ThirtyDayLimitUSD: &newLimit,
		HasRollingQuotaSnapshot: true,
	})

	require.NoError(t, err)
	require.True(t, reused)
	require.Equal(t, int64(7), sub.ID)
	require.Equal(t, 1, subRepo.createCalls)
	require.Equal(t, newLimit, *sub.FiveHourLimitUSD)
	require.Equal(t, newLimit, *sub.SevenDayLimitUSD)
	require.Equal(t, newLimit, *sub.ThirtyDayLimitUSD)
	require.Equal(t, 0.25, sub.FiveHourUsageUSD)
	require.WithinDuration(t, oldExpiresAt.AddDate(0, 0, 14), sub.ExpiresAt, time.Second)
	require.Contains(t, sub.Notes, "payment order 2")
}

func TestAssignOrExtendSubscription_ExpiredRenewalResetsRollingQuotaCycle(t *testing.T) {
	oldLimit := 1.0
	newLimit := 2.0
	start := time.Now().Add(-48 * time.Hour)
	groupRepo := &subscriptionGroupRepoStub{
		group: &Group{ID: 2, SubscriptionType: SubscriptionTypeSubscription, Status: StatusActive},
	}
	subRepo := newSubscriptionUserSubRepoStub()
	subRepo.seed(&UserSubscription{
		ID:                   6,
		UserID:               10,
		GroupID:              2,
		StartsAt:             start,
		ExpiresAt:            time.Now().Add(-time.Hour),
		Status:               SubscriptionStatusExpired,
		FiveHourLimitUSD:     &oldLimit,
		SevenDayLimitUSD:     &oldLimit,
		ThirtyDayLimitUSD:    &oldLimit,
		FiveHourUsageUSD:     0.5,
		SevenDayUsageUSD:     0.6,
		ThirtyDayUsageUSD:    0.7,
		FiveHourWindowStart:  &start,
		SevenDayWindowStart:  &start,
		ThirtyDayWindowStart: &start,
	})

	svc := NewSubscriptionService(groupRepo, subRepo, nil, nil, nil)
	sub, reused, err := svc.AssignOrExtendSubscription(context.Background(), &AssignSubscriptionInput{
		UserID: 10, GroupID: 2, ValidityDays: 30,
		FiveHourLimitUSD: &newLimit, SevenDayLimitUSD: &newLimit, ThirtyDayLimitUSD: &newLimit,
		HasRollingQuotaSnapshot: true,
	})

	require.NoError(t, err)
	require.True(t, reused)
	require.Equal(t, newLimit, *sub.FiveHourLimitUSD)
	require.Equal(t, newLimit, *sub.SevenDayLimitUSD)
	require.Equal(t, newLimit, *sub.ThirtyDayLimitUSD)
	require.Zero(t, sub.FiveHourUsageUSD)
	require.Zero(t, sub.SevenDayUsageUSD)
	require.Zero(t, sub.ThirtyDayUsageUSD)
	require.Nil(t, sub.FiveHourWindowStart)
	require.Nil(t, sub.SevenDayWindowStart)
	require.Nil(t, sub.ThirtyDayWindowStart)
	require.Equal(t, SubscriptionStatusActive, sub.Status)
}

func TestValidateAndCheckLimits_BlocksFiveHourQuotaAtLimit(t *testing.T) {
	limit := 1.0
	start := time.Now().Add(-time.Hour)
	sub := &UserSubscription{
		Status:              SubscriptionStatusActive,
		ExpiresAt:           time.Now().Add(time.Hour),
		FiveHourLimitUSD:    &limit,
		FiveHourUsageUSD:    1.0,
		FiveHourWindowStart: &start,
	}
	svc := NewSubscriptionService(nil, nil, nil, nil, nil)

	_, err := svc.ValidateAndCheckLimits(sub, &Group{})

	require.ErrorIs(t, err, ErrFiveHourLimitExceeded)
}

func TestValidateAndCheckLimits_BlocksZeroFiveHourQuota(t *testing.T) {
	limit := 0.0
	start := time.Now().Add(-time.Hour)
	sub := &UserSubscription{
		Status:              SubscriptionStatusActive,
		ExpiresAt:           time.Now().Add(time.Hour),
		FiveHourLimitUSD:    &limit,
		FiveHourUsageUSD:    0,
		FiveHourWindowStart: &start,
	}
	svc := NewSubscriptionService(nil, nil, nil, nil, nil)

	_, err := svc.ValidateAndCheckLimits(sub, &Group{})

	require.ErrorIs(t, err, ErrFiveHourLimitExceeded)
}

func TestValidateAndCheckLimits_BlocksSevenDayQuotaAtDisplayLimit(t *testing.T) {
	limit := 65.0
	start := time.Now().Add(-48 * time.Hour)
	sub := &UserSubscription{
		Status:              SubscriptionStatusActive,
		ExpiresAt:           time.Now().Add(time.Hour),
		SevenDayLimitUSD:    &limit,
		SevenDayUsageUSD:    64.9986108508,
		SevenDayWindowStart: &start,
	}
	svc := NewSubscriptionService(nil, nil, nil, nil, nil)

	_, err := svc.ValidateAndCheckLimits(sub, &Group{})

	require.ErrorIs(t, err, ErrSevenDayLimitExceeded)
}

func TestValidateAndCheckLimits_AllowsSevenDayQuotaAbovePreflightReserve(t *testing.T) {
	limit := 65.0
	start := time.Now().Add(-48 * time.Hour)
	sub := &UserSubscription{
		Status:              SubscriptionStatusActive,
		ExpiresAt:           time.Now().Add(time.Hour),
		SevenDayLimitUSD:    &limit,
		SevenDayUsageUSD:    64.99,
		SevenDayWindowStart: &start,
	}
	svc := NewSubscriptionService(nil, nil, nil, nil, nil)

	_, err := svc.ValidateAndCheckLimits(sub, &Group{})

	require.NoError(t, err)
}

func TestCheckSevenDayLimit_ExactAdditionalCostDoesNotUsePreflightReserve(t *testing.T) {
	limit := 65.0
	sub := &UserSubscription{
		SevenDayLimitUSD: &limit,
		SevenDayUsageUSD: 64.9986108508,
	}

	require.True(t, sub.CheckSevenDayLimit(0.001))
	require.False(t, sub.CheckSevenDayLimit(0.002))
}

func TestValidateAndCheckLimits_AllowsExpiredFiveHourWindow(t *testing.T) {
	limit := 1.0
	start := time.Now().Add(-6 * time.Hour)
	legacyWindowStart := startOfDay(time.Now())
	sub := &UserSubscription{
		Status:              SubscriptionStatusActive,
		ExpiresAt:           time.Now().Add(time.Hour),
		DailyWindowStart:    &legacyWindowStart,
		FiveHourLimitUSD:    &limit,
		FiveHourUsageUSD:    10.0,
		FiveHourWindowStart: &start,
	}
	svc := NewSubscriptionService(nil, nil, nil, nil, nil)

	needsMaintenance, err := svc.ValidateAndCheckLimits(sub, &Group{})

	require.NoError(t, err)
	require.False(t, needsMaintenance)
	require.Zero(t, sub.FiveHourUsageUSD)
}

func TestDoWindowMaintenanceDoesNotResetExpiredRollingQuotaWindows(t *testing.T) {
	now := time.Now()
	legacyWindowStart := startOfDay(now)
	rollingWindowStart := now.Add(-SubscriptionWindowFiveHour - time.Minute)
	limit := 5.0
	subRepo := newSubscriptionUserSubRepoStub()
	svc := NewSubscriptionService(groupRepoNoop{}, subRepo, nil, nil, nil)

	require.NotPanics(t, func() {
		svc.DoWindowMaintenance(&UserSubscription{
			ID:                  1,
			UserID:              10,
			GroupID:             20,
			Status:              SubscriptionStatusActive,
			ExpiresAt:           now.Add(24 * time.Hour),
			DailyWindowStart:    &legacyWindowStart,
			FiveHourLimitUSD:    &limit,
			FiveHourUsageUSD:    5,
			FiveHourWindowStart: &rollingWindowStart,
		})
	})
}

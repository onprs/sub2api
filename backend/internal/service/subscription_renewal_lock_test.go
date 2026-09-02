package service

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type lockingRenewalRepo struct {
	userSubRepoNoop
	mu        sync.Mutex
	stale     UserSubscription
	current   UserSubscription
	lockReads int
}

func (r *lockingRenewalRepo) ExistsByUserIDAndGroupID(context.Context, int64, int64) (bool, error) {
	return true, nil
}

func (r *lockingRenewalRepo) ExistsByUserIDGroupIDAndPlanID(context.Context, int64, int64, *int64) (bool, error) {
	return true, nil
}

func (r *lockingRenewalRepo) GetByUserIDAndGroupID(context.Context, int64, int64) (*UserSubscription, error) {
	copy := r.stale
	return &copy, nil
}

func (r *lockingRenewalRepo) GetByUserIDGroupIDAndPlanID(context.Context, int64, int64, *int64) (*UserSubscription, error) {
	copy := r.stale
	return &copy, nil
}

func (r *lockingRenewalRepo) GetByID(_ context.Context, _ int64) (*UserSubscription, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	copy := r.current
	return &copy, nil
}

func (r *lockingRenewalRepo) GetByIDForUpdate(_ context.Context, _ int64) (*UserSubscription, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lockReads++
	copy := r.current
	return &copy, nil
}

func (r *lockingRenewalRepo) RenewTerm(_ context.Context, input *RenewSubscriptionTermInput) (*UserSubscription, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lockReads++
	if input == nil {
		return nil, ErrSubscriptionNilInput
	}
	if r.current.Status == SubscriptionStatusSuspended {
		return nil, ErrSubscriptionSuspended
	}

	renewed := r.current
	activeTerm := renewed.Status != SubscriptionStatusExpired && renewed.ExpiresAt.After(input.Now)
	if activeTerm {
		renewed.ExpiresAt = renewed.ExpiresAt.AddDate(0, 0, input.ValidityDays)
	} else {
		renewed.StartsAt = input.Now
		renewed.ExpiresAt = input.Now.AddDate(0, 0, input.ValidityDays)
	}
	if renewed.ExpiresAt.After(input.MaxExpiresAt) {
		renewed.ExpiresAt = input.MaxExpiresAt
	}
	if !activeTerm {
		dailyWindowStart := InitialSubscriptionDailyWindowStart(renewed.StartsAt, renewed.ExpiresAt)
		periodicWindowStart := renewed.StartsAt
		renewed.DailyWindowStart = &dailyWindowStart
		renewed.WeeklyWindowStart = &periodicWindowStart
		renewed.MonthlyWindowStart = &periodicWindowStart
		renewed.FiveHourWindowStart = nil
		renewed.SevenDayWindowStart = nil
		renewed.ThirtyDayWindowStart = nil
		renewed.DailyUsageUSD = 0
		renewed.WeeklyUsageUSD = 0
		renewed.MonthlyUsageUSD = 0
		renewed.FiveHourUsageUSD = 0
		renewed.SevenDayUsageUSD = 0
		renewed.ThirtyDayUsageUSD = 0
	}
	if input.HasRollingQuotaSnapshot {
		renewed.FiveHourLimitUSD = input.FiveHourLimitUSD
		renewed.SevenDayLimitUSD = input.SevenDayLimitUSD
		renewed.ThirtyDayLimitUSD = input.ThirtyDayLimitUSD
	}
	notes := input.Notes
	if input.SkipDuplicateNotes && strings.TrimSpace(renewed.Notes) == strings.TrimSpace(notes) {
		notes = ""
	}
	renewed.Status = SubscriptionStatusActive
	renewed.Notes = appendSubscriptionNotes(renewed.Notes, notes)
	r.current = renewed
	copy := renewed
	return &copy, nil
}

func (r *lockingRenewalRepo) ExtendExpiry(_ context.Context, _ int64, expiresAt time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.current.ExpiresAt = expiresAt
	return nil
}

func (r *lockingRenewalRepo) UpdateStatus(_ context.Context, _ int64, status string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.current.Status = status
	return nil
}

func (r *lockingRenewalRepo) UpdateNotes(_ context.Context, _ int64, notes string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.current.Notes = notes
	return nil
}

func (r *lockingRenewalRepo) Update(_ context.Context, sub *UserSubscription) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.current = *sub
	return nil
}

func TestAssignOrExtendSubscriptionDoesNotReactivateLockedSuspendedRow(t *testing.T) {
	now := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	lockedExpiry := now.AddDate(0, 0, 20)
	windowStart := now.Add(-24 * time.Hour)
	repo := &lockingRenewalRepo{
		stale: UserSubscription{ID: 7, UserID: 11, GroupID: 13, ExpiresAt: now.Add(-time.Hour), Status: SubscriptionStatusExpired, Notes: "stale"},
		current: UserSubscription{
			ID: 7, UserID: 11, GroupID: 13, StartsAt: now.AddDate(0, 0, -10), ExpiresAt: lockedExpiry,
			Status: SubscriptionStatusSuspended, Notes: "current", DailyWindowStart: &windowStart, DailyUsageUSD: 4,
		},
	}
	svc := NewSubscriptionService(&subscriptionGroupRepoStub{group: &Group{ID: 13, SubscriptionType: SubscriptionTypeSubscription}}, repo, nil, nil, nil)
	svc.now = func() time.Time { return now }

	sub, extended, err := svc.AssignOrExtendSubscription(context.Background(), &AssignSubscriptionInput{
		UserID: 11, GroupID: 13, ValidityDays: 5, Notes: "renewed",
	})

	require.ErrorIs(t, err, ErrSubscriptionSuspended)
	require.True(t, extended)
	require.Nil(t, sub)
	require.Equal(t, 1, repo.lockReads)
	require.Equal(t, lockedExpiry, repo.current.ExpiresAt)
	require.Equal(t, SubscriptionStatusSuspended, repo.current.Status)
	require.Equal(t, "current", repo.current.Notes)
	require.Equal(t, windowStart, *repo.current.DailyWindowStart)
	require.Equal(t, float64(4), repo.current.DailyUsageUSD)
}

func TestAssignOrExtendSubscriptionSerializedRenewalsAccumulateDays(t *testing.T) {
	now := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	initialExpiry := now.AddDate(0, 0, 10)
	stale := UserSubscription{ID: 17, UserID: 21, GroupID: 23, StartsAt: now, ExpiresAt: initialExpiry, Status: SubscriptionStatusActive}
	repo := &lockingRenewalRepo{stale: stale, current: stale}
	svc := NewSubscriptionService(&subscriptionGroupRepoStub{group: &Group{ID: 23, SubscriptionType: SubscriptionTypeSubscription}}, repo, nil, nil, nil)
	svc.now = func() time.Time { return now }
	input := &AssignSubscriptionInput{UserID: 21, GroupID: 23, ValidityDays: 7}

	_, _, err := svc.AssignOrExtendSubscription(context.Background(), input)
	require.NoError(t, err)
	second, _, err := svc.AssignOrExtendSubscription(context.Background(), input)
	require.NoError(t, err)

	require.Equal(t, 2, repo.lockReads)
	require.Equal(t, initialExpiry.AddDate(0, 0, 14), second.ExpiresAt)
}

func TestAssignSubscriptionDoesNotReactivateRowSuspendedAfterStaleRead(t *testing.T) {
	now := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	windowStart := now.Add(-24 * time.Hour)
	current := UserSubscription{
		ID: 27, UserID: 31, GroupID: 33, StartsAt: now.AddDate(0, 0, -10), ExpiresAt: now.Add(-time.Hour),
		Status: SubscriptionStatusSuspended, Notes: "suspended", DailyWindowStart: &windowStart, DailyUsageUSD: 4,
	}
	repo := &lockingRenewalRepo{
		stale:   UserSubscription{ID: 27, UserID: 31, GroupID: 33, ExpiresAt: now.Add(-time.Hour), Status: SubscriptionStatusExpired},
		current: current,
	}
	svc := NewSubscriptionService(&subscriptionGroupRepoStub{group: &Group{ID: 33, SubscriptionType: SubscriptionTypeSubscription}}, repo, nil, nil, nil)
	svc.now = func() time.Time { return now }

	sub, reused, err := svc.assignSubscriptionWithReuse(context.Background(), &AssignSubscriptionInput{
		UserID: 31, GroupID: 33, ValidityDays: 5, Notes: "renewed",
	})

	require.NoError(t, err)
	require.True(t, reused)
	require.Equal(t, 1, repo.lockReads)
	require.Equal(t, current, repo.current)
	require.Equal(t, SubscriptionStatusSuspended, sub.Status)
	require.Equal(t, current.ExpiresAt, sub.ExpiresAt)
	require.Equal(t, current.Notes, sub.Notes)
}

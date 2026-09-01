package service

import (
	"context"
	"strconv"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/dgraph-io/ristretto"
	"github.com/stretchr/testify/require"
)

func TestWithSubscriptionUpdateTx_ReusesExistingTransaction(t *testing.T) {
	existingTx := &dbent.Tx{}
	ctx := dbent.NewTxContext(context.Background(), existingTx)
	svc := &SubscriptionService{entClient: &dbent.Client{}}

	called := false
	err := svc.withSubscriptionUpdateTx(ctx, func(txCtx context.Context) error {
		called = true
		require.Same(t, existingTx, dbent.TxFromContext(txCtx))
		return nil
	})

	require.NoError(t, err)
	require.True(t, called)
}

func TestMaybeInvalidateAssignmentCaches_DefersForOuterTransactionOwner(t *testing.T) {
	cache, err := ristretto.NewCache(&ristretto.Config{NumCounters: 1_000, MaxCost: 100, BufferItems: 64})
	require.NoError(t, err)
	t.Cleanup(cache.Close)

	svc := &SubscriptionService{subCacheL1: cache}
	key := subCacheKey(7, 9)
	require.True(t, cache.Set(key, &UserSubscription{ID: 42}, 1))
	cache.Wait()

	svc.maybeInvalidateAssignmentCaches(7, 9, true)
	_, cachedBeforeCommit := cache.Get(key)
	require.True(t, cachedBeforeCommit, "outer transaction must retain caches until its owner commits")

	svc.maybeInvalidateAssignmentCaches(7, 9, false)
	cache.Wait()
	_, cachedAfterCommit := cache.Get(key)
	require.False(t, cachedAfterCommit, "post-commit invalidation must remove the cached subscription")
}

type groupRepoNoop struct{}

func (groupRepoNoop) Create(context.Context, *Group) error { panic("unexpected Create call") }
func (groupRepoNoop) GetByID(context.Context, int64) (*Group, error) {
	panic("unexpected GetByID call")
}
func (groupRepoNoop) GetByIDLite(context.Context, int64) (*Group, error) {
	panic("unexpected GetByIDLite call")
}
func (groupRepoNoop) Update(context.Context, *Group) error { panic("unexpected Update call") }
func (groupRepoNoop) Delete(context.Context, int64) error  { panic("unexpected Delete call") }
func (groupRepoNoop) DeleteCascade(context.Context, int64) ([]int64, error) {
	panic("unexpected DeleteCascade call")
}
func (groupRepoNoop) List(context.Context, pagination.PaginationParams) ([]Group, *pagination.PaginationResult, error) {
	panic("unexpected List call")
}
func (groupRepoNoop) ListWithFilters(context.Context, pagination.PaginationParams, string, string, string, *bool) ([]Group, *pagination.PaginationResult, error) {
	panic("unexpected ListWithFilters call")
}
func (groupRepoNoop) ListActive(context.Context) ([]Group, error) {
	panic("unexpected ListActive call")
}
func (groupRepoNoop) ListActiveByPlatform(context.Context, string) ([]Group, error) {
	panic("unexpected ListActiveByPlatform call")
}
func (groupRepoNoop) ExistsByName(context.Context, string) (bool, error) {
	panic("unexpected ExistsByName call")
}
func (groupRepoNoop) GetAccountCount(context.Context, int64) (int64, int64, error) {
	panic("unexpected GetAccountCount call")
}
func (groupRepoNoop) DeleteAccountGroupsByGroupID(context.Context, int64) (int64, error) {
	panic("unexpected DeleteAccountGroupsByGroupID call")
}
func (groupRepoNoop) GetAccountIDsByGroupIDs(context.Context, []int64) ([]int64, error) {
	panic("unexpected GetAccountIDsByGroupIDs call")
}
func (groupRepoNoop) BindAccountsToGroup(context.Context, int64, []int64) error {
	panic("unexpected BindAccountsToGroup call")
}
func (groupRepoNoop) UpdateSortOrders(context.Context, []GroupSortOrderUpdate) error {
	panic("unexpected UpdateSortOrders call")
}

type subscriptionGroupRepoStub struct {
	groupRepoNoop
	group *Group
}

func (s *subscriptionGroupRepoStub) GetByID(context.Context, int64) (*Group, error) {
	return s.group, nil
}

type userSubRepoNoop struct{}

func (userSubRepoNoop) Create(context.Context, *UserSubscription) error {
	panic("unexpected Create call")
}
func (userSubRepoNoop) GetByID(context.Context, int64) (*UserSubscription, error) {
	panic("unexpected GetByID call")
}
func (userSubRepoNoop) GetByIDIncludeDeleted(context.Context, int64) (*UserSubscription, error) {
	panic("unexpected GetByIDIncludeDeleted call")
}
func (userSubRepoNoop) GetByUserIDAndGroupID(context.Context, int64, int64) (*UserSubscription, error) {
	panic("unexpected GetByUserIDAndGroupID call")
}
func (userSubRepoNoop) GetByUserIDGroupIDAndPlanID(context.Context, int64, int64, *int64) (*UserSubscription, error) {
	panic("unexpected GetByUserIDGroupIDAndPlanID call")
}
func (userSubRepoNoop) GetActiveByUserIDAndGroupID(context.Context, int64, int64) (*UserSubscription, error) {
	panic("unexpected GetActiveByUserIDAndGroupID call")
}
func (userSubRepoNoop) Update(context.Context, *UserSubscription) error {
	panic("unexpected Update call")
}
func (userSubRepoNoop) Delete(context.Context, int64) error { panic("unexpected Delete call") }
func (userSubRepoNoop) Restore(context.Context, int64, string) (*UserSubscription, error) {
	panic("unexpected Restore call")
}
func (userSubRepoNoop) ListByUserID(context.Context, int64) ([]UserSubscription, error) {
	panic("unexpected ListByUserID call")
}
func (userSubRepoNoop) ListActiveByUserID(context.Context, int64) ([]UserSubscription, error) {
	panic("unexpected ListActiveByUserID call")
}
func (userSubRepoNoop) ListActiveByUserIDAndGroupID(context.Context, int64, int64) ([]UserSubscription, error) {
	panic("unexpected ListActiveByUserIDAndGroupID call")
}
func (userSubRepoNoop) ListByGroupID(context.Context, int64, pagination.PaginationParams) ([]UserSubscription, *pagination.PaginationResult, error) {
	panic("unexpected ListByGroupID call")
}
func (userSubRepoNoop) List(context.Context, pagination.PaginationParams, *int64, *int64, string, string, string, string) ([]UserSubscription, *pagination.PaginationResult, error) {
	panic("unexpected List call")
}
func (userSubRepoNoop) ExistsByUserIDAndGroupID(context.Context, int64, int64) (bool, error) {
	panic("unexpected ExistsByUserIDAndGroupID call")
}
func (userSubRepoNoop) ExistsByUserIDGroupIDAndPlanID(context.Context, int64, int64, *int64) (bool, error) {
	panic("unexpected ExistsByUserIDGroupIDAndPlanID call")
}
func (userSubRepoNoop) ExistsActiveByUserIDAndGroupID(context.Context, int64, int64) (bool, error) {
	panic("unexpected ExistsActiveByUserIDAndGroupID call")
}
func (userSubRepoNoop) ExtendExpiry(context.Context, int64, time.Time) error {
	panic("unexpected ExtendExpiry call")
}
func (userSubRepoNoop) RenewTerm(context.Context, *RenewSubscriptionTermInput) (*UserSubscription, error) {
	panic("unexpected RenewTerm call")
}
func (userSubRepoNoop) UpdateStatus(context.Context, int64, string) error {
	panic("unexpected UpdateStatus call")
}
func (userSubRepoNoop) UpdateNotes(context.Context, int64, string) error {
	panic("unexpected UpdateNotes call")
}
func (userSubRepoNoop) UpdateRollingQuotaSnapshot(context.Context, int64, *float64, *float64, *float64) error {
	panic("unexpected UpdateRollingQuotaSnapshot call")
}
func (userSubRepoNoop) ActivateWindows(context.Context, int64, time.Time) error {
	panic("unexpected ActivateWindows call")
}
func (userSubRepoNoop) ResetUsageWindows(context.Context, int64, bool, bool, bool, time.Time) error {
	panic("unexpected ResetUsageWindows call")
}
func (userSubRepoNoop) ResetDailyUsage(context.Context, int64, *time.Time, time.Time) error {
	panic("unexpected ResetDailyUsage call")
}
func (userSubRepoNoop) ResetWeeklyUsage(context.Context, int64, *time.Time, time.Time) error {
	panic("unexpected ResetWeeklyUsage call")
}
func (userSubRepoNoop) ResetMonthlyUsage(context.Context, int64, *time.Time, time.Time) error {
	panic("unexpected ResetMonthlyUsage call")
}
func (userSubRepoNoop) ResetFiveHourUsage(context.Context, int64, time.Time) error {
	panic("unexpected ResetFiveHourUsage call")
}
func (userSubRepoNoop) ResetSevenDayUsage(context.Context, int64, time.Time) error {
	panic("unexpected ResetSevenDayUsage call")
}
func (userSubRepoNoop) ResetThirtyDayUsage(context.Context, int64, time.Time) error {
	panic("unexpected ResetThirtyDayUsage call")
}
func (userSubRepoNoop) ListIDs(context.Context, pagination.PaginationParams, *int64, *int64, string, string, string, string) ([]int64, *pagination.PaginationResult, error) {
	panic("unexpected ListIDs call")
}
func (userSubRepoNoop) IncrementUsage(context.Context, int64, float64) error {
	panic("unexpected IncrementUsage call")
}
func (userSubRepoNoop) BatchUpdateExpiredStatus(context.Context) (int64, error) {
	panic("unexpected BatchUpdateExpiredStatus call")
}

type subscriptionUserSubRepoStub struct {
	userSubRepoNoop

	nextID      int64
	byID        map[int64]*UserSubscription
	byUserGroup map[string]*UserSubscription
	createCalls int

	createConflictSub *UserSubscription
}

func newSubscriptionUserSubRepoStub() *subscriptionUserSubRepoStub {
	return &subscriptionUserSubRepoStub{
		nextID:      1,
		byID:        make(map[int64]*UserSubscription),
		byUserGroup: make(map[string]*UserSubscription),
	}
}

func (s *subscriptionUserSubRepoStub) key(userID, groupID int64, planID *int64) string {
	planPart := "legacy"
	if planID != nil {
		planPart = strconv.FormatInt(*planID, 10)
	}
	return strconvFormatInt(userID) + ":" + strconvFormatInt(groupID) + ":" + planPart
}

func (s *subscriptionUserSubRepoStub) seed(sub *UserSubscription) {
	if sub == nil {
		return
	}
	cp := *sub
	if cp.ID == 0 {
		cp.ID = s.nextID
		s.nextID++
	}
	s.byID[cp.ID] = &cp
	s.byUserGroup[s.key(cp.UserID, cp.GroupID, cp.PlanID)] = &cp
}

func (s *subscriptionUserSubRepoStub) ExistsByUserIDAndGroupID(_ context.Context, userID, groupID int64) (bool, error) {
	for _, sub := range s.byUserGroup {
		if sub.UserID == userID && sub.GroupID == groupID {
			return true, nil
		}
	}
	return false, nil
}

func (s *subscriptionUserSubRepoStub) ExistsByUserIDGroupIDAndPlanID(_ context.Context, userID, groupID int64, planID *int64) (bool, error) {
	_, ok := s.byUserGroup[s.key(userID, groupID, planID)]
	return ok, nil
}

func (s *subscriptionUserSubRepoStub) GetByUserIDAndGroupID(ctx context.Context, userID, groupID int64) (*UserSubscription, error) {
	return s.GetByUserIDGroupIDAndPlanID(ctx, userID, groupID, nil)
}

func (s *subscriptionUserSubRepoStub) GetByUserIDGroupIDAndPlanID(_ context.Context, userID, groupID int64, planID *int64) (*UserSubscription, error) {
	sub := s.byUserGroup[s.key(userID, groupID, planID)]
	if sub == nil {
		return nil, ErrSubscriptionNotFound
	}
	cp := *sub
	return &cp, nil
}

func (s *subscriptionUserSubRepoStub) GetActiveByUserIDAndGroupID(_ context.Context, userID, groupID int64) (*UserSubscription, error) {
	var selected *UserSubscription
	for _, sub := range s.byUserGroup {
		if sub.UserID != userID || sub.GroupID != groupID || sub.Status != SubscriptionStatusActive || !time.Now().Before(sub.ExpiresAt) {
			continue
		}
		if selected == nil || sub.ExpiresAt.Before(selected.ExpiresAt) || (sub.ExpiresAt.Equal(selected.ExpiresAt) && sub.ID < selected.ID) {
			selected = sub
		}
	}
	if selected == nil {
		return nil, ErrSubscriptionNotFound
	}
	cp := *selected
	return &cp, nil
}

func (s *subscriptionUserSubRepoStub) ListActiveByUserIDAndGroupID(_ context.Context, userID, groupID int64) ([]UserSubscription, error) {
	subs := make([]UserSubscription, 0)
	for _, sub := range s.byUserGroup {
		if sub.UserID == userID && sub.GroupID == groupID && sub.Status == SubscriptionStatusActive && time.Now().Before(sub.ExpiresAt) {
			subs = append(subs, *sub)
		}
	}
	return subs, nil
}

func (s *subscriptionUserSubRepoStub) Create(_ context.Context, sub *UserSubscription) error {
	if sub == nil {
		return nil
	}
	s.createCalls++
	if s.createConflictSub != nil {
		s.seed(s.createConflictSub)
		s.createConflictSub = nil
		return ErrSubscriptionAlreadyExists
	}
	if _, ok := s.byUserGroup[s.key(sub.UserID, sub.GroupID, sub.PlanID)]; ok {
		return ErrSubscriptionAlreadyExists
	}
	cp := *sub
	if cp.ID == 0 {
		cp.ID = s.nextID
		s.nextID++
	}
	sub.ID = cp.ID
	s.byID[cp.ID] = &cp
	s.byUserGroup[s.key(cp.UserID, cp.GroupID, cp.PlanID)] = &cp
	return nil
}

func (s *subscriptionUserSubRepoStub) GetByID(_ context.Context, id int64) (*UserSubscription, error) {
	sub := s.byID[id]
	if sub == nil {
		return nil, ErrSubscriptionNotFound
	}
	cp := *sub
	return &cp, nil
}

func (s *subscriptionUserSubRepoStub) Update(_ context.Context, sub *UserSubscription) error {
	if sub == nil {
		return ErrSubscriptionNilInput
	}
	existing := s.byID[sub.ID]
	if existing == nil {
		return ErrSubscriptionNotFound
	}
	oldKey := s.key(existing.UserID, existing.GroupID, existing.PlanID)
	cp := *sub
	s.byID[cp.ID] = &cp
	if oldKey != s.key(cp.UserID, cp.GroupID, cp.PlanID) {
		delete(s.byUserGroup, oldKey)
	}
	s.byUserGroup[s.key(cp.UserID, cp.GroupID, cp.PlanID)] = &cp
	return nil
}

func (s *subscriptionUserSubRepoStub) ExtendExpiry(_ context.Context, subscriptionID int64, newExpiresAt time.Time) error {
	existing := s.byID[subscriptionID]
	if existing == nil {
		return ErrSubscriptionNotFound
	}
	existing.ExpiresAt = newExpiresAt
	return nil
}

func (s *subscriptionUserSubRepoStub) RenewTerm(_ context.Context, input *RenewSubscriptionTermInput) (*UserSubscription, error) {
	if input == nil {
		return nil, ErrSubscriptionNilInput
	}
	existing := s.byID[input.SubscriptionID]
	if existing == nil {
		return nil, ErrSubscriptionNotFound
	}
	if existing.Status == SubscriptionStatusSuspended {
		return nil, ErrSubscriptionSuspended
	}
	wasActive := existing.Status != SubscriptionStatusExpired && existing.ExpiresAt.After(input.Now)
	if wasActive {
		existing.ExpiresAt = existing.ExpiresAt.AddDate(0, 0, input.ValidityDays)
	} else {
		existing.StartsAt = input.Now
		existing.ExpiresAt = input.Now.AddDate(0, 0, input.ValidityDays)
		existing.DailyUsageUSD = 0
		existing.WeeklyUsageUSD = 0
		existing.MonthlyUsageUSD = 0
		existing.FiveHourUsageUSD = 0
		existing.SevenDayUsageUSD = 0
		existing.ThirtyDayUsageUSD = 0
		existing.DailyWindowStart = &input.LegacyWindowStart
		existing.WeeklyWindowStart = &input.LegacyWindowStart
		existing.MonthlyWindowStart = &input.LegacyWindowStart
		existing.FiveHourWindowStart = nil
		existing.SevenDayWindowStart = nil
		existing.ThirtyDayWindowStart = nil
	}
	if existing.ExpiresAt.After(input.MaxExpiresAt) {
		existing.ExpiresAt = input.MaxExpiresAt
	}
	existing.Status = SubscriptionStatusActive
	if input.HasRollingQuotaSnapshot {
		existing.FiveHourLimitUSD = input.FiveHourLimitUSD
		existing.SevenDayLimitUSD = input.SevenDayLimitUSD
		existing.ThirtyDayLimitUSD = input.ThirtyDayLimitUSD
	}
	existing.Notes = appendSubscriptionNotes(existing.Notes, input.Notes)
	copy := *existing
	return &copy, nil
}

func (s *subscriptionUserSubRepoStub) UpdateRollingQuotaSnapshot(_ context.Context, subscriptionID int64, fiveHourLimitUSD, sevenDayLimitUSD, thirtyDayLimitUSD *float64) error {
	existing := s.byID[subscriptionID]
	if existing == nil {
		return ErrSubscriptionNotFound
	}
	existing.FiveHourLimitUSD = fiveHourLimitUSD
	existing.SevenDayLimitUSD = sevenDayLimitUSD
	existing.ThirtyDayLimitUSD = thirtyDayLimitUSD
	return nil
}

func TestAssignSubscriptionReuseWhenSemanticsMatch(t *testing.T) {
	start := time.Now().Add(-time.Hour)
	groupRepo := &subscriptionGroupRepoStub{
		group: &Group{ID: 1, SubscriptionType: SubscriptionTypeSubscription},
	}
	subRepo := newSubscriptionUserSubRepoStub()
	subRepo.seed(&UserSubscription{
		ID:        10,
		UserID:    1001,
		GroupID:   1,
		StartsAt:  start,
		ExpiresAt: start.AddDate(0, 0, 30),
		Status:    SubscriptionStatusActive,
		Notes:     "init",
	})

	svc := NewSubscriptionService(groupRepo, subRepo, nil, nil, nil)
	sub, err := svc.AssignSubscription(context.Background(), &AssignSubscriptionInput{
		UserID:       1001,
		GroupID:      1,
		ValidityDays: 30,
		Notes:        "init",
	})
	require.NoError(t, err)
	require.Equal(t, int64(10), sub.ID)
	require.Equal(t, 0, subRepo.createCalls, "reuse should not create new subscription")
	require.Equal(t, start, sub.StartsAt)
	require.Equal(t, start.AddDate(0, 0, 30), sub.ExpiresAt)
}

func TestAssignSubscriptionDoesNotReactivateFutureSuspendedSubscription(t *testing.T) {
	start := time.Now().Add(-time.Hour)
	groupRepo := &subscriptionGroupRepoStub{
		group: &Group{ID: 1, SubscriptionType: SubscriptionTypeSubscription},
	}
	subRepo := newSubscriptionUserSubRepoStub()
	subRepo.seed(&UserSubscription{
		ID:        13,
		UserID:    1003,
		GroupID:   1,
		StartsAt:  start,
		ExpiresAt: start.AddDate(0, 0, 30),
		Status:    SubscriptionStatusSuspended,
		Notes:     "assignment",
	})

	svc := NewSubscriptionService(groupRepo, subRepo, nil, nil, nil)
	sub, err := svc.AssignSubscription(context.Background(), &AssignSubscriptionInput{
		UserID:       1003,
		GroupID:      1,
		ValidityDays: 30,
		Notes:        "assignment",
	})

	require.NoError(t, err)
	require.Equal(t, int64(13), sub.ID)
	require.Equal(t, SubscriptionStatusSuspended, sub.Status)
	require.Equal(t, start, sub.StartsAt)
	require.Equal(t, start.AddDate(0, 0, 30), sub.ExpiresAt)
	require.Equal(t, "assignment", sub.Notes)
	require.Equal(t, 0, subRepo.createCalls)
}

func TestAssignSubscriptionDoesNotReactivatePastExpirySuspendedSubscription(t *testing.T) {
	start := time.Now().AddDate(0, 0, -31)
	expiresAt := start.AddDate(0, 0, 30)
	windowStart := startOfDay(start)
	groupRepo := &subscriptionGroupRepoStub{
		group: &Group{ID: 1, SubscriptionType: SubscriptionTypeSubscription},
	}
	subRepo := newSubscriptionUserSubRepoStub()
	subRepo.seed(&UserSubscription{
		ID:                 15,
		UserID:             1005,
		GroupID:            1,
		StartsAt:           start,
		ExpiresAt:          expiresAt,
		Status:             SubscriptionStatusSuspended,
		DailyWindowStart:   &windowStart,
		WeeklyWindowStart:  &windowStart,
		MonthlyWindowStart: &windowStart,
		DailyUsageUSD:      1,
		WeeklyUsageUSD:     2,
		MonthlyUsageUSD:    3,
		Notes:              "suspended assignment",
	})

	svc := NewSubscriptionService(groupRepo, subRepo, nil, nil, nil)
	sub, err := svc.AssignSubscription(context.Background(), &AssignSubscriptionInput{
		UserID:       1005,
		GroupID:      1,
		ValidityDays: 30,
		Notes:        "suspended assignment",
	})

	require.NoError(t, err)
	require.Equal(t, int64(15), sub.ID)
	require.Equal(t, SubscriptionStatusSuspended, sub.Status)
	require.Equal(t, start, sub.StartsAt)
	require.Equal(t, expiresAt, sub.ExpiresAt)
	require.Equal(t, "suspended assignment", sub.Notes)
	require.Equal(t, &windowStart, sub.DailyWindowStart)
	require.Equal(t, &windowStart, sub.WeeklyWindowStart)
	require.Equal(t, &windowStart, sub.MonthlyWindowStart)
	require.Equal(t, float64(1), sub.DailyUsageUSD)
	require.Equal(t, float64(2), sub.WeeklyUsageUSD)
	require.Equal(t, float64(3), sub.MonthlyUsageUSD)
	require.Equal(t, 0, subRepo.createCalls)
}

func TestAssignOrExtendSubscriptionDoesNotReactivateSuspendedSubscription(t *testing.T) {
	for _, tc := range []struct {
		name      string
		expiresAt time.Time
	}{
		{name: "future term", expiresAt: time.Now().Add(24 * time.Hour)},
		{name: "past term", expiresAt: time.Now().Add(-24 * time.Hour)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			groupRepo := &subscriptionGroupRepoStub{
				group: &Group{ID: 1, SubscriptionType: SubscriptionTypeSubscription},
			}
			subRepo := newSubscriptionUserSubRepoStub()
			subRepo.seed(&UserSubscription{
				ID:        16,
				UserID:    1006,
				GroupID:   1,
				StartsAt:  tc.expiresAt.Add(-24 * time.Hour),
				ExpiresAt: tc.expiresAt,
				Status:    SubscriptionStatusSuspended,
				Notes:     "admin suspended",
			})

			svc := NewSubscriptionService(groupRepo, subRepo, nil, nil, nil)
			sub, reused, err := svc.AssignOrExtendSubscription(context.Background(), &AssignSubscriptionInput{
				UserID:       1006,
				GroupID:      1,
				ValidityDays: 30,
				Notes:        "non-admin renewal",
			})

			require.ErrorIs(t, err, ErrSubscriptionSuspended)
			require.True(t, reused)
			require.Nil(t, sub)
			stored := subRepo.byID[16]
			require.Equal(t, SubscriptionStatusSuspended, stored.Status)
			require.Equal(t, tc.expiresAt, stored.ExpiresAt)
			require.Equal(t, "admin suspended", stored.Notes)
		})
	}
}

func TestAssignSubscriptionRenewsExpiredSemanticMatch(t *testing.T) {
	groupRepo := &subscriptionGroupRepoStub{
		group: &Group{ID: 1, SubscriptionType: SubscriptionTypeSubscription},
	}
	subRepo := newSubscriptionUserSubRepoStub()
	oldStart := time.Now().Add(-time.Hour)
	oldWindowStart := startOfDay(oldStart)
	subRepo.seed(&UserSubscription{
		ID:                 12,
		UserID:             1002,
		GroupID:            1,
		StartsAt:           oldStart,
		ExpiresAt:          oldStart.AddDate(0, 0, 30),
		Status:             SubscriptionStatusExpired,
		DailyWindowStart:   &oldWindowStart,
		WeeklyWindowStart:  &oldWindowStart,
		MonthlyWindowStart: &oldWindowStart,
		DailyUsageUSD:      1,
		WeeklyUsageUSD:     2,
		MonthlyUsageUSD:    3,
		Notes:              " assignment ",
	})

	svc := NewSubscriptionService(groupRepo, subRepo, nil, nil, nil)
	before := time.Now()
	sub, err := svc.AssignSubscription(context.Background(), &AssignSubscriptionInput{
		UserID:       1002,
		GroupID:      1,
		ValidityDays: 30,
		Notes:        "assignment",
	})
	after := time.Now()

	require.NoError(t, err)
	require.Equal(t, int64(12), sub.ID)
	require.Equal(t, 0, subRepo.createCalls)
	require.Equal(t, SubscriptionStatusActive, sub.Status)
	require.False(t, sub.StartsAt.Before(before))
	require.False(t, sub.StartsAt.After(after))
	require.Equal(t, sub.StartsAt.AddDate(0, 0, 30), sub.ExpiresAt)
	require.Equal(t, startOfDay(sub.StartsAt), *sub.DailyWindowStart)
	require.Equal(t, startOfDay(sub.StartsAt), *sub.WeeklyWindowStart)
	require.Equal(t, startOfDay(sub.StartsAt), *sub.MonthlyWindowStart)
	require.Zero(t, sub.DailyUsageUSD)
	require.Zero(t, sub.WeeklyUsageUSD)
	require.Zero(t, sub.MonthlyUsageUSD)
	require.Equal(t, " assignment ", sub.Notes)
}

func TestAssignSubscriptionRenewsExpiredAndAppendsDifferentNotes(t *testing.T) {
	groupRepo := &subscriptionGroupRepoStub{
		group: &Group{ID: 1, SubscriptionType: SubscriptionTypeSubscription},
	}
	subRepo := newSubscriptionUserSubRepoStub()
	oldStart := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	subRepo.seed(&UserSubscription{
		ID:        14,
		UserID:    1004,
		GroupID:   1,
		StartsAt:  oldStart,
		ExpiresAt: oldStart.AddDate(0, 0, 30),
		Status:    SubscriptionStatusExpired,
		Notes:     "old assignment",
	})

	svc := NewSubscriptionService(groupRepo, subRepo, nil, nil, nil)
	sub, err := svc.AssignSubscription(context.Background(), &AssignSubscriptionInput{
		UserID:       1004,
		GroupID:      1,
		ValidityDays: 30,
		Notes:        "new assignment",
	})

	require.NoError(t, err)
	require.Equal(t, "old assignment\nnew assignment", sub.Notes)
}

func TestAssignSubscriptionConflictWhenSemanticsMismatch(t *testing.T) {
	start := time.Now().Add(-time.Hour)
	groupRepo := &subscriptionGroupRepoStub{
		group: &Group{ID: 1, SubscriptionType: SubscriptionTypeSubscription},
	}
	subRepo := newSubscriptionUserSubRepoStub()
	subRepo.seed(&UserSubscription{
		ID:        11,
		UserID:    2001,
		GroupID:   1,
		StartsAt:  start,
		ExpiresAt: start.AddDate(0, 0, 30),
		Status:    SubscriptionStatusActive,
		Notes:     "old-note",
	})

	svc := NewSubscriptionService(groupRepo, subRepo, nil, nil, nil)
	_, err := svc.AssignSubscription(context.Background(), &AssignSubscriptionInput{
		UserID:       2001,
		GroupID:      1,
		ValidityDays: 30,
		Notes:        "new-note",
	})
	require.Error(t, err)
	require.Equal(t, "SUBSCRIPTION_ASSIGN_CONFLICT", infraerrorsReason(err))
	require.Equal(t, 0, subRepo.createCalls, "conflict should not create or mutate existing subscription")
}

func TestBulkAssignSubscriptionCreatedReusedAndConflict(t *testing.T) {
	start := time.Now().Add(-time.Hour)
	groupRepo := &subscriptionGroupRepoStub{
		group: &Group{ID: 1, SubscriptionType: SubscriptionTypeSubscription},
	}
	subRepo := newSubscriptionUserSubRepoStub()
	// user 1: 语义一致，可 reused
	subRepo.seed(&UserSubscription{
		ID:        21,
		UserID:    1,
		GroupID:   1,
		StartsAt:  start,
		ExpiresAt: start.AddDate(0, 0, 30),
		Status:    SubscriptionStatusActive,
		Notes:     "same-note",
	})
	// user 3: 语义冲突（有效期不一致），应 failed
	subRepo.seed(&UserSubscription{
		ID:        23,
		UserID:    3,
		GroupID:   1,
		StartsAt:  start,
		ExpiresAt: start.AddDate(0, 0, 60),
		Status:    SubscriptionStatusActive,
		Notes:     "same-note",
	})

	svc := NewSubscriptionService(groupRepo, subRepo, nil, nil, nil)
	result, err := svc.BulkAssignSubscription(context.Background(), &BulkAssignSubscriptionInput{
		UserIDs:      []int64{1, 2, 3},
		GroupID:      1,
		ValidityDays: 30,
		AssignedBy:   9,
		Notes:        "same-note",
	})
	require.NoError(t, err)
	require.Equal(t, 2, result.SuccessCount)
	require.Equal(t, 1, result.CreatedCount)
	require.Equal(t, 1, result.ReusedCount)
	require.Equal(t, 1, result.FailedCount)
	require.Equal(t, "reused", result.Statuses[1])
	require.Equal(t, "created", result.Statuses[2])
	require.Equal(t, "failed", result.Statuses[3])
	require.Equal(t, 1, subRepo.createCalls)
}

func TestBulkAssignSubscriptionRenewsExpiredSemanticMatch(t *testing.T) {
	groupRepo := &subscriptionGroupRepoStub{
		group: &Group{ID: 1, SubscriptionType: SubscriptionTypeSubscription},
	}
	subRepo := newSubscriptionUserSubRepoStub()
	oldStart := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	subRepo.seed(&UserSubscription{
		ID:              24,
		UserID:          4,
		GroupID:         1,
		StartsAt:        oldStart,
		ExpiresAt:       oldStart.AddDate(0, 0, 7),
		Status:          SubscriptionStatusExpired,
		DailyUsageUSD:   1,
		WeeklyUsageUSD:  2,
		MonthlyUsageUSD: 3,
		Notes:           "bulk",
	})

	svc := NewSubscriptionService(groupRepo, subRepo, nil, nil, nil)
	before := time.Now()
	result, err := svc.BulkAssignSubscription(context.Background(), &BulkAssignSubscriptionInput{
		UserIDs:      []int64{4},
		GroupID:      1,
		ValidityDays: 7,
		Notes:        "bulk",
	})
	after := time.Now()

	require.NoError(t, err)
	require.Equal(t, 1, result.SuccessCount)
	require.Equal(t, 0, result.CreatedCount)
	require.Equal(t, 1, result.ReusedCount)
	require.Equal(t, "reused", result.Statuses[4])
	require.Len(t, result.Subscriptions, 1)
	renewed := result.Subscriptions[0]
	require.Equal(t, SubscriptionStatusActive, renewed.Status)
	require.False(t, renewed.StartsAt.Before(before))
	require.False(t, renewed.StartsAt.After(after))
	require.Equal(t, renewed.StartsAt.AddDate(0, 0, 7), renewed.ExpiresAt)
	require.Zero(t, renewed.DailyUsageUSD)
	require.Zero(t, renewed.WeeklyUsageUSD)
	require.Zero(t, renewed.MonthlyUsageUSD)
	require.Equal(t, "bulk", renewed.Notes)
}

func TestAssignSubscriptionKeepsWorkingWhenIdempotencyStoreUnavailable(t *testing.T) {
	groupRepo := &subscriptionGroupRepoStub{
		group: &Group{ID: 1, SubscriptionType: SubscriptionTypeSubscription},
	}
	subRepo := newSubscriptionUserSubRepoStub()
	SetDefaultIdempotencyCoordinator(NewIdempotencyCoordinator(failingIdempotencyRepo{}, DefaultIdempotencyConfig()))
	t.Cleanup(func() {
		SetDefaultIdempotencyCoordinator(nil)
	})

	svc := NewSubscriptionService(groupRepo, subRepo, nil, nil, nil)
	sub, err := svc.AssignSubscription(context.Background(), &AssignSubscriptionInput{
		UserID:       9001,
		GroupID:      1,
		ValidityDays: 30,
		Notes:        "new",
	})
	require.NoError(t, err)
	require.NotNil(t, sub)
	require.Equal(t, 1, subRepo.createCalls, "semantic idempotent endpoint should not depend on idempotency store availability")
}

func TestNormalizeAssignValidityDays(t *testing.T) {
	require.Equal(t, 30, normalizeAssignValidityDays(0))
	require.Equal(t, 30, normalizeAssignValidityDays(-5))
	require.Equal(t, MaxValidityDays, normalizeAssignValidityDays(MaxValidityDays+100))
	require.Equal(t, 7, normalizeAssignValidityDays(7))
}

func TestDetectAssignSemanticConflictCases(t *testing.T) {
	start := time.Date(2026, 2, 20, 10, 0, 0, 0, time.UTC)
	base := &UserSubscription{
		UserID:    1,
		GroupID:   1,
		StartsAt:  start,
		ExpiresAt: start.AddDate(0, 0, 30),
		Notes:     "same",
	}

	reason, conflict := detectAssignSemanticConflict(base, &AssignSubscriptionInput{
		UserID:       1,
		GroupID:      1,
		ValidityDays: 30,
		Notes:        "same",
	})
	require.False(t, conflict)
	require.Equal(t, "", reason)

	reason, conflict = detectAssignSemanticConflict(base, &AssignSubscriptionInput{
		UserID:       1,
		GroupID:      1,
		ValidityDays: 60,
		Notes:        "same",
	})
	require.True(t, conflict)
	require.Equal(t, "validity_days_mismatch", reason)

	reason, conflict = detectAssignSemanticConflict(base, &AssignSubscriptionInput{
		UserID:       1,
		GroupID:      1,
		ValidityDays: 30,
		Notes:        "other",
	})
	require.True(t, conflict)
	require.Equal(t, "notes_mismatch", reason)
}

func TestAssignSubscriptionGroupTypeValidation(t *testing.T) {
	groupRepo := &subscriptionGroupRepoStub{
		group: &Group{ID: 1, SubscriptionType: SubscriptionTypeStandard},
	}
	subRepo := newSubscriptionUserSubRepoStub()
	svc := NewSubscriptionService(groupRepo, subRepo, nil, nil, nil)

	_, err := svc.AssignSubscription(context.Background(), &AssignSubscriptionInput{
		UserID:       1,
		GroupID:      1,
		ValidityDays: 30,
	})
	require.Error(t, err)
	require.Equal(t, infraerrors.Code(ErrGroupNotSubscriptionType), infraerrors.Code(err))
}

func strconvFormatInt(v int64) string {
	return strconv.FormatInt(v, 10)
}

func infraerrorsReason(err error) string {
	return infraerrors.Reason(err)
}

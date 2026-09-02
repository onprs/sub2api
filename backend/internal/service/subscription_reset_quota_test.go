//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/stretchr/testify/require"
)

// resetQuotaUserSubRepoStub 支持 GetByID、ResetUsageWindows，
// 其余方法继承 userSubRepoNoop（panic）。
type resetQuotaUserSubRepoStub struct {
	userSubRepoNoop

	sub  *UserSubscription
	subs map[int64]*UserSubscription

	lastListIDsFilter BulkResetSubscriptionQuotaFilter
	listIDsCalls      int
	listIDsErr        error
	resetCallOrder    []string

	resetDailyCalled     bool
	resetWeeklyCalled    bool
	resetMonthlyCalled   bool
	resetFiveHourCalled  bool
	resetSevenDayCalled  bool
	resetThirtyDayCalled bool
	resetDailyErr        error
	resetWeeklyErr       error
	resetMonthlyErr      error
	resetFiveHourErr     error
	resetSevenDayErr     error
	resetThirtyDayErr    error
	dailyStart           time.Time
	periodicStart        time.Time
}

func (r *resetQuotaUserSubRepoStub) GetByID(_ context.Context, id int64) (*UserSubscription, error) {
	if r.subs != nil {
		sub := r.subs[id]
		if sub == nil {
			return nil, ErrSubscriptionNotFound
		}
		cp := *sub
		return &cp, nil
	}
	if r.sub == nil || r.sub.ID != id {
		return nil, ErrSubscriptionNotFound
	}
	cp := *r.sub
	return &cp, nil
}

func (r *resetQuotaUserSubRepoStub) storedSub(id int64) *UserSubscription {
	if r.subs != nil {
		return r.subs[id]
	}
	return r.sub
}

func (r *resetQuotaUserSubRepoStub) ResetUsageWindows(_ context.Context, id int64, resetDaily, resetWeekly, resetMonthly bool, dailyStart, periodicStart time.Time) error {
	r.resetDailyCalled = resetDaily
	r.resetWeeklyCalled = resetWeekly
	r.resetMonthlyCalled = resetMonthly
	r.dailyStart = dailyStart
	r.periodicStart = periodicStart
	if resetDaily && r.resetDailyErr != nil {
		return r.resetDailyErr
	}
	if resetWeekly && r.resetWeeklyErr != nil {
		return r.resetWeeklyErr
	}
	if resetMonthly && r.resetMonthlyErr != nil {
		return r.resetMonthlyErr
	}
	sub := r.storedSub(id)
	if sub == nil {
		return nil
	}
	if resetDaily {
		sub.DailyUsageUSD = 0
		sub.DailyWindowStart = &dailyStart
	}
	if resetWeekly {
		sub.WeeklyUsageUSD = 0
		sub.WeeklyWindowStart = &periodicStart
	}
	if resetMonthly {
		sub.MonthlyUsageUSD = 0
		sub.MonthlyWindowStart = &periodicStart
	}
	return nil
}

func (r *resetQuotaUserSubRepoStub) ResetDailyUsage(_ context.Context, _ int64, _ *time.Time, windowStart time.Time) error {
	r.resetDailyCalled = true
	if r.resetDailyErr == nil && r.sub != nil {
		r.sub.DailyUsageUSD = 0
		r.sub.DailyWindowStart = &windowStart
	}
	return r.resetDailyErr
}

func (r *resetQuotaUserSubRepoStub) ResetWeeklyUsage(_ context.Context, _ int64, _ *time.Time, _ time.Time) error {
	r.resetWeeklyCalled = true
	return r.resetWeeklyErr
}

func (r *resetQuotaUserSubRepoStub) ResetMonthlyUsage(_ context.Context, _ int64, _ *time.Time, _ time.Time) error {
	r.resetMonthlyCalled = true
	return r.resetMonthlyErr
}

func (r *resetQuotaUserSubRepoStub) ResetFiveHourUsage(_ context.Context, id int64, windowStart time.Time) error {
	r.resetFiveHourCalled = true
	r.resetCallOrder = append(r.resetCallOrder, "5h")
	if r.resetFiveHourErr == nil {
		if sub := r.storedSub(id); sub != nil {
			amount := sub.FiveHourUsageUSD
			sub.FiveHourUsageUSD = 0
			sub.SevenDayUsageUSD = maxFloat64(sub.SevenDayUsageUSD-amount, 0)
			sub.ThirtyDayUsageUSD = maxFloat64(sub.ThirtyDayUsageUSD-amount, 0)
			sub.FiveHourWindowStart = &windowStart
		}
	}
	return r.resetFiveHourErr
}

func (r *resetQuotaUserSubRepoStub) ResetSevenDayUsage(_ context.Context, id int64, windowStart time.Time) error {
	r.resetSevenDayCalled = true
	r.resetCallOrder = append(r.resetCallOrder, "7d")
	if r.resetSevenDayErr == nil {
		if sub := r.storedSub(id); sub != nil {
			amount := sub.SevenDayUsageUSD
			sub.FiveHourUsageUSD = 0
			sub.SevenDayUsageUSD = 0
			sub.ThirtyDayUsageUSD = maxFloat64(sub.ThirtyDayUsageUSD-amount, 0)
			sub.FiveHourWindowStart = &windowStart
			sub.SevenDayWindowStart = &windowStart
		}
	}
	return r.resetSevenDayErr
}

func (r *resetQuotaUserSubRepoStub) ResetThirtyDayUsage(_ context.Context, id int64, windowStart time.Time) error {
	r.resetThirtyDayCalled = true
	r.resetCallOrder = append(r.resetCallOrder, "30d")
	if r.resetThirtyDayErr == nil {
		if sub := r.storedSub(id); sub != nil {
			sub.FiveHourUsageUSD = 0
			sub.SevenDayUsageUSD = 0
			sub.ThirtyDayUsageUSD = 0
			sub.FiveHourWindowStart = &windowStart
			sub.SevenDayWindowStart = &windowStart
			sub.ThirtyDayWindowStart = &windowStart
		}
	}
	return r.resetThirtyDayErr
}

func (r *resetQuotaUserSubRepoStub) ListIDs(_ context.Context, params pagination.PaginationParams, userID, groupID *int64, status, platform, sortBy, sortOrder string) ([]int64, *pagination.PaginationResult, error) {
	r.listIDsCalls++
	r.lastListIDsFilter = BulkResetSubscriptionQuotaFilter{UserID: userID, GroupID: groupID, Status: status, Platform: platform, SortBy: sortBy, SortOrder: sortOrder}
	if r.listIDsErr != nil {
		return nil, nil, r.listIDsErr
	}
	ids := []int64{}
	if r.subs != nil {
		for id := range r.subs {
			ids = append(ids, id)
		}
	} else if r.sub != nil {
		ids = append(ids, r.sub.ID)
	}
	total := len(ids)
	start := params.Offset()
	if start > total {
		start = total
	}
	end := start + params.Limit()
	if end > total {
		end = total
	}
	pages := total / params.Limit()
	if total%params.Limit() > 0 {
		pages++
	}
	return ids[start:end], &pagination.PaginationResult{Page: params.Page, PageSize: params.Limit(), Pages: pages, Total: int64(total)}, nil
}

func maxFloat64(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func newResetQuotaSvc(stub *resetQuotaUserSubRepoStub) *SubscriptionService {
	return NewSubscriptionService(groupRepoNoop{}, stub, nil, nil, nil)
}

func TestAdminResetQuota_ResetBoth(t *testing.T) {
	stub := &resetQuotaUserSubRepoStub{
		sub: &UserSubscription{ID: 1, UserID: 10, GroupID: 20},
	}
	svc := newResetQuotaSvc(stub)
	resetAt := time.Date(2026, 7, 1, 10, 37, 42, 123, time.UTC)
	svc.now = func() time.Time { return resetAt }

	result, err := svc.AdminResetQuota(context.Background(), 1, true, true, false, false, false, false)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, stub.resetDailyCalled, "应调用 ResetDailyUsage")
	require.True(t, stub.resetWeeklyCalled, "应调用 ResetWeeklyUsage")
	require.False(t, stub.resetMonthlyCalled, "不应调用 ResetMonthlyUsage")
	// 手动重置后日窗口锚定当天 0 点（保持 0 点刷新节奏），周窗口锚定重置时刻。
	require.Equal(t, timezone.StartOfDay(resetAt), stub.dailyStart)
	require.Equal(t, resetAt, stub.periodicStart)
	require.Equal(t, timezone.StartOfDay(resetAt), *result.DailyWindowStart)
	require.Equal(t, resetAt, *result.WeeklyWindowStart)
}

func TestAdminResetQuota_ResetDailyOnly(t *testing.T) {
	stub := &resetQuotaUserSubRepoStub{
		sub: &UserSubscription{ID: 2, UserID: 10, GroupID: 20},
	}
	svc := newResetQuotaSvc(stub)

	result, err := svc.AdminResetQuota(context.Background(), 2, true, false, false, false, false, false)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, stub.resetDailyCalled, "应调用 ResetDailyUsage")
	require.False(t, stub.resetWeeklyCalled, "不应调用 ResetWeeklyUsage")
	require.False(t, stub.resetMonthlyCalled, "不应调用 ResetMonthlyUsage")
}

func TestAdminResetQuota_ResetAllRollingQuotaWindowsUsesLargestWindow(t *testing.T) {
	oldStart := time.Date(2026, 1, 1, 8, 0, 0, 0, time.UTC)
	stub := &resetQuotaUserSubRepoStub{
		sub: &UserSubscription{
			ID:                   21,
			UserID:               10,
			GroupID:              20,
			FiveHourUsageUSD:     1.25,
			SevenDayUsageUSD:     2.50,
			ThirtyDayUsageUSD:    3.75,
			FiveHourWindowStart:  &oldStart,
			SevenDayWindowStart:  &oldStart,
			ThirtyDayWindowStart: &oldStart,
		},
	}
	svc := newResetQuotaSvc(stub)

	result, err := svc.AdminResetQuota(context.Background(), 21, false, false, false, true, true, true)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.False(t, stub.resetDailyCalled, "不应调用 legacy ResetDailyUsage")
	require.False(t, stub.resetWeeklyCalled, "不应调用 legacy ResetWeeklyUsage")
	require.False(t, stub.resetMonthlyCalled, "不应调用 legacy ResetMonthlyUsage")
	require.False(t, stub.resetFiveHourCalled, "选择父窗口时不应单独调用子窗口 reset，避免重复扣减")
	require.False(t, stub.resetSevenDayCalled, "选择 30d 时由 30d reset 一次性清理 5h/7d/30d")
	require.True(t, stub.resetThirtyDayCalled, "应调用 ResetThirtyDayUsage")
	require.Equal(t, []string{"30d"}, stub.resetCallOrder, "应只执行最大被选中的滚动窗口")
	require.Equal(t, float64(0), result.FiveHourUsageUSD)
	require.Equal(t, float64(0), result.SevenDayUsageUSD)
	require.Equal(t, float64(0), result.ThirtyDayUsageUSD)
	require.NotNil(t, result.FiveHourWindowStart)
	require.NotNil(t, result.SevenDayWindowStart)
	require.NotNil(t, result.ThirtyDayWindowStart)
}

func TestAdminResetQuota_ResetFiveHourDeductsLargerWindows(t *testing.T) {
	stub := &resetQuotaUserSubRepoStub{
		sub: &UserSubscription{
			ID:                22,
			UserID:            10,
			GroupID:           20,
			FiveHourUsageUSD:  1.25,
			SevenDayUsageUSD:  3.50,
			ThirtyDayUsageUSD: 9.25,
		},
	}
	svc := newResetQuotaSvc(stub)

	result, err := svc.AdminResetQuota(context.Background(), 22, false, false, false, true, false, false)

	require.NoError(t, err)
	require.True(t, stub.resetFiveHourCalled)
	require.False(t, stub.resetSevenDayCalled)
	require.False(t, stub.resetThirtyDayCalled)
	require.InDelta(t, 0, result.FiveHourUsageUSD, 0.0000001)
	require.InDelta(t, 2.25, result.SevenDayUsageUSD, 0.0000001)
	require.InDelta(t, 8.0, result.ThirtyDayUsageUSD, 0.0000001)
}

func TestAdminResetQuota_ResetSevenDayResetsFiveHourAndDeductsThirtyDay(t *testing.T) {
	stub := &resetQuotaUserSubRepoStub{
		sub: &UserSubscription{
			ID:                23,
			UserID:            10,
			GroupID:           20,
			FiveHourUsageUSD:  0.75,
			SevenDayUsageUSD:  4.50,
			ThirtyDayUsageUSD: 7.00,
		},
	}
	svc := newResetQuotaSvc(stub)

	result, err := svc.AdminResetQuota(context.Background(), 23, false, false, false, false, true, false)

	require.NoError(t, err)
	require.False(t, stub.resetFiveHourCalled)
	require.True(t, stub.resetSevenDayCalled)
	require.False(t, stub.resetThirtyDayCalled)
	require.InDelta(t, 0, result.FiveHourUsageUSD, 0.0000001)
	require.InDelta(t, 0, result.SevenDayUsageUSD, 0.0000001)
	require.InDelta(t, 2.50, result.ThirtyDayUsageUSD, 0.0000001)
}

func TestAdminResetQuota_RollingDeductionDoesNotGoNegative(t *testing.T) {
	stub := &resetQuotaUserSubRepoStub{
		sub: &UserSubscription{
			ID:                24,
			UserID:            10,
			GroupID:           20,
			FiveHourUsageUSD:  5.00,
			SevenDayUsageUSD:  2.00,
			ThirtyDayUsageUSD: 1.00,
		},
	}
	svc := newResetQuotaSvc(stub)

	result, err := svc.AdminResetQuota(context.Background(), 24, false, false, false, true, false, false)

	require.NoError(t, err)
	require.InDelta(t, 0, result.FiveHourUsageUSD, 0.0000001)
	require.InDelta(t, 0, result.SevenDayUsageUSD, 0.0000001)
	require.InDelta(t, 0, result.ThirtyDayUsageUSD, 0.0000001)
}

func TestBulkAdminResetQuota_SelectedSubscriptions(t *testing.T) {
	stub := &resetQuotaUserSubRepoStub{
		subs: map[int64]*UserSubscription{
			101: {ID: 101, UserID: 10, GroupID: 20, FiveHourUsageUSD: 1.00, SevenDayUsageUSD: 4.00, ThirtyDayUsageUSD: 8.00},
			102: {ID: 102, UserID: 11, GroupID: 20, FiveHourUsageUSD: 2.00, SevenDayUsageUSD: 3.00, ThirtyDayUsageUSD: 6.00},
		},
	}
	svc := newResetQuotaSvc(stub)

	result, err := svc.BulkAdminResetQuota(context.Background(), BulkResetSubscriptionQuotaInput{
		SubscriptionIDs: []int64{101, 102, 101},
		ResetFiveHour:   true,
	})

	require.NoError(t, err)
	require.Equal(t, 2, result.SuccessCount)
	require.Equal(t, 0, result.FailedCount)
	require.Equal(t, "reset", result.Statuses[101])
	require.Equal(t, "reset", result.Statuses[102])
	require.Equal(t, 0, stub.listIDsCalls, "显式选择不应走筛选查询")
	require.InDelta(t, 0, stub.subs[101].FiveHourUsageUSD, 0.0000001)
	require.InDelta(t, 3.00, stub.subs[101].SevenDayUsageUSD, 0.0000001)
	require.InDelta(t, 7.00, stub.subs[101].ThirtyDayUsageUSD, 0.0000001)
	require.InDelta(t, 0, stub.subs[102].FiveHourUsageUSD, 0.0000001)
	require.InDelta(t, 1.00, stub.subs[102].SevenDayUsageUSD, 0.0000001)
	require.InDelta(t, 4.00, stub.subs[102].ThirtyDayUsageUSD, 0.0000001)
}

func TestBulkAdminResetQuota_AllFilteredUsesFilter(t *testing.T) {
	userID := int64(10)
	groupID := int64(20)
	stub := &resetQuotaUserSubRepoStub{
		subs: map[int64]*UserSubscription{
			201: {ID: 201, UserID: userID, GroupID: groupID, SevenDayUsageUSD: 2.00, ThirtyDayUsageUSD: 5.00},
		},
	}
	svc := newResetQuotaSvc(stub)

	result, err := svc.BulkAdminResetQuota(context.Background(), BulkResetSubscriptionQuotaInput{
		AllFiltered:   true,
		Filter:        BulkResetSubscriptionQuotaFilter{UserID: &userID, GroupID: &groupID, Status: SubscriptionStatusActive, Platform: "opencode_go", SortBy: "expires_at", SortOrder: "asc"},
		ResetSevenDay: true,
	})

	require.NoError(t, err)
	require.Equal(t, 1, result.SuccessCount)
	require.Equal(t, 1, stub.listIDsCalls)
	require.NotNil(t, stub.lastListIDsFilter.UserID)
	require.NotNil(t, stub.lastListIDsFilter.GroupID)
	require.Equal(t, userID, *stub.lastListIDsFilter.UserID)
	require.Equal(t, groupID, *stub.lastListIDsFilter.GroupID)
	require.Equal(t, SubscriptionStatusActive, stub.lastListIDsFilter.Status)
	require.Equal(t, "opencode_go", stub.lastListIDsFilter.Platform)
	require.Equal(t, "expires_at", stub.lastListIDsFilter.SortBy)
	require.Equal(t, "asc", stub.lastListIDsFilter.SortOrder)
	require.InDelta(t, 0, stub.subs[201].FiveHourUsageUSD, 0.0000001)
	require.InDelta(t, 0, stub.subs[201].SevenDayUsageUSD, 0.0000001)
	require.InDelta(t, 3.00, stub.subs[201].ThirtyDayUsageUSD, 0.0000001)
}

func TestBulkAdminResetQuota_RequiresSelectionOrFilteredMode(t *testing.T) {
	stub := &resetQuotaUserSubRepoStub{}
	svc := newResetQuotaSvc(stub)

	_, err := svc.BulkAdminResetQuota(context.Background(), BulkResetSubscriptionQuotaInput{ResetFiveHour: true})

	require.ErrorIs(t, err, ErrInvalidInput)
	require.Equal(t, 0, stub.listIDsCalls)
}

func TestAdminResetQuota_ResetWeeklyOnly(t *testing.T) {
	stub := &resetQuotaUserSubRepoStub{
		sub: &UserSubscription{ID: 3, UserID: 10, GroupID: 20},
	}
	svc := newResetQuotaSvc(stub)

	result, err := svc.AdminResetQuota(context.Background(), 3, false, true, false, false, false, false)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.False(t, stub.resetDailyCalled, "不应调用 ResetDailyUsage")
	require.True(t, stub.resetWeeklyCalled, "应调用 ResetWeeklyUsage")
	require.False(t, stub.resetMonthlyCalled, "不应调用 ResetMonthlyUsage")
}

func TestAdminResetQuota_BothFalseReturnsError(t *testing.T) {
	stub := &resetQuotaUserSubRepoStub{
		sub: &UserSubscription{ID: 7, UserID: 10, GroupID: 20},
	}
	svc := newResetQuotaSvc(stub)

	_, err := svc.AdminResetQuota(context.Background(), 7, false, false, false, false, false, false)

	require.ErrorIs(t, err, ErrInvalidInput)
	require.False(t, stub.resetDailyCalled)
	require.False(t, stub.resetWeeklyCalled)
	require.False(t, stub.resetMonthlyCalled)
}

func TestAdminResetQuota_SubscriptionNotFound(t *testing.T) {
	stub := &resetQuotaUserSubRepoStub{sub: nil}
	svc := newResetQuotaSvc(stub)

	_, err := svc.AdminResetQuota(context.Background(), 999, true, true, true, false, false, false)

	require.ErrorIs(t, err, ErrSubscriptionNotFound)
	require.False(t, stub.resetDailyCalled)
	require.False(t, stub.resetWeeklyCalled)
	require.False(t, stub.resetMonthlyCalled)
}

func TestAdminResetQuota_ResetDailyUsageError(t *testing.T) {
	dbErr := errors.New("db error")
	stub := &resetQuotaUserSubRepoStub{
		sub:           &UserSubscription{ID: 4, UserID: 10, GroupID: 20},
		resetDailyErr: dbErr,
	}
	svc := newResetQuotaSvc(stub)

	_, err := svc.AdminResetQuota(context.Background(), 4, true, true, false, false, false, false)

	require.ErrorIs(t, err, dbErr)
	require.True(t, stub.resetDailyCalled)
	require.True(t, stub.resetWeeklyCalled, "原子重置应在一次调用中提交所选窗口")
}

func TestAdminResetQuota_ResetWeeklyUsageError(t *testing.T) {
	dbErr := errors.New("db error")
	stub := &resetQuotaUserSubRepoStub{
		sub:            &UserSubscription{ID: 5, UserID: 10, GroupID: 20},
		resetWeeklyErr: dbErr,
	}
	svc := newResetQuotaSvc(stub)

	_, err := svc.AdminResetQuota(context.Background(), 5, false, true, false, false, false, false)

	require.ErrorIs(t, err, dbErr)
	require.True(t, stub.resetWeeklyCalled)
}

func TestAdminResetQuota_ResetMonthlyOnly(t *testing.T) {
	stub := &resetQuotaUserSubRepoStub{
		sub: &UserSubscription{ID: 8, UserID: 10, GroupID: 20},
	}
	svc := newResetQuotaSvc(stub)

	result, err := svc.AdminResetQuota(context.Background(), 8, false, false, true, false, false, false)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.False(t, stub.resetDailyCalled, "不应调用 ResetDailyUsage")
	require.False(t, stub.resetWeeklyCalled, "不应调用 ResetWeeklyUsage")
	require.True(t, stub.resetMonthlyCalled, "应调用 ResetMonthlyUsage")
}

func TestAdminResetQuota_BeforeStartsAtSameDayPreservesAutomaticBoundary(t *testing.T) {
	startsAt := time.Date(2026, 7, 1, 15, 0, 0, 0, time.UTC)
	resetAt := time.Date(2026, 7, 1, 10, 37, 42, 123, time.UTC)
	stub := &resetQuotaUserSubRepoStub{
		sub: &UserSubscription{
			ID:        10,
			UserID:    10,
			GroupID:   20,
			StartsAt:  startsAt,
			ExpiresAt: startsAt.Add(45 * 24 * time.Hour),
		},
	}
	svc := newResetQuotaSvc(stub)
	svc.now = func() time.Time { return resetAt }

	result, err := svc.AdminResetQuota(context.Background(), 10, false, false, true, false, false, false)

	require.NoError(t, err)
	require.Equal(t, resetAt, *result.MonthlyWindowStart)
	boundary, ok := result.automaticWindowStartAt(result.MonthlyWindowStart, 30*24*time.Hour, resetAt.Add(30*24*time.Hour))
	require.True(t, ok)
	require.Equal(t, resetAt.Add(30*24*time.Hour), boundary)
}

func TestAdminResetQuota_ResetMonthlyUsageError(t *testing.T) {
	dbErr := errors.New("db error")
	stub := &resetQuotaUserSubRepoStub{
		sub:             &UserSubscription{ID: 9, UserID: 10, GroupID: 20},
		resetMonthlyErr: dbErr,
	}
	svc := newResetQuotaSvc(stub)

	_, err := svc.AdminResetQuota(context.Background(), 9, false, false, true, false, false, false)

	require.ErrorIs(t, err, dbErr)
	require.True(t, stub.resetMonthlyCalled)
}

func TestAdminResetQuota_ReturnsRefreshedSub(t *testing.T) {
	stub := &resetQuotaUserSubRepoStub{
		sub: &UserSubscription{
			ID:            6,
			UserID:        10,
			GroupID:       20,
			DailyUsageUSD: 99.9,
		},
	}

	svc := newResetQuotaSvc(stub)
	result, err := svc.AdminResetQuota(context.Background(), 6, true, false, false, false, false, false)

	require.NoError(t, err)
	// ResetUsageWindows stub 会将 sub.DailyUsageUSD 归零，
	// 服务应返回第二次 GetByID 的刷新值而非初始的 99.9
	require.Equal(t, float64(0), result.DailyUsageUSD, "返回的订阅应反映已归零的用量")
	require.True(t, stub.resetDailyCalled)
}

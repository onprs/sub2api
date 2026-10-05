package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type readOnlyUsageRepo struct {
	AccountRepository
	accounts map[int64]*Account
	writes   int
}

func (r *readOnlyUsageRepo) GetByID(_ context.Context, id int64) (*Account, error) {
	account := r.accounts[id]
	if account == nil {
		return nil, ErrAccountNotFound
	}
	copy := *account
	return &copy, nil
}
func (r *readOnlyUsageRepo) GetByIDs(_ context.Context, ids []int64) ([]*Account, error) {
	result := make([]*Account, 0, len(ids))
	for _, id := range ids {
		if account := r.accounts[id]; account != nil {
			copy := *account
			result = append(result, &copy)
		}
	}
	return result, nil
}
func (r *readOnlyUsageRepo) UpdateExtra(context.Context, int64, map[string]any) error {
	r.writes++
	return nil
}
func (r *readOnlyUsageRepo) UpdateSessionWindowEnd(context.Context, int64, time.Time) error {
	r.writes++
	return nil
}
func (r *readOnlyUsageRepo) ClearError(context.Context, int64) error { r.writes++; return nil }

type readOnlyUsageFetcher struct {
	response *ClaudeUsageResponse
	err      error
	calls    int
}

func (f *readOnlyUsageFetcher) FetchUsage(context.Context, string, string) (*ClaudeUsageResponse, error) {
	f.calls++
	return f.response, f.err
}
func (f *readOnlyUsageFetcher) FetchUsageWithOptions(context.Context, *ClaudeUsageFetchOptions) (*ClaudeUsageResponse, error) {
	f.calls++
	return f.response, f.err
}

func TestAccountUsageReadOnlyActiveDoesNotWriteAndKeepsCacheTimestamp(t *testing.T) {
	now := time.Now()
	reset := now.Add(2 * time.Hour).UTC().Truncate(time.Second)
	fetched := now.Add(-time.Minute)
	response := &ClaudeUsageResponse{}
	response.FiveHour.Utilization = 45
	response.FiveHour.ResetsAt = reset.Format(time.RFC3339)
	response.SevenDay.Utilization = 70
	response.SevenDay.ResetsAt = reset.Add(4 * 24 * time.Hour).Format(time.RFC3339)
	repo := &readOnlyUsageRepo{accounts: map[int64]*Account{1: {ID: 1, Platform: PlatformAnthropic, Type: AccountTypeOAuth, Status: StatusError, ErrorMessage: "token refresh failed", Extra: map[string]any{}}}}
	cache := NewUsageCache()
	cache.apiCache.Store(int64(1), &apiUsageCache{response: response, timestamp: fetched})
	cache.windowStatsCache.Store(int64(1), &windowStatsCache{stats: &WindowStats{}, timestamp: now})
	fetcher := &readOnlyUsageFetcher{}
	svc := &AccountUsageService{accountRepo: repo, cache: cache, usageFetcher: fetcher}

	usage, err := svc.GetReadOnlyUsage(t.Context(), 1, "active")
	require.NoError(t, err)
	require.Equal(t, "active", usage.Source)
	require.Equal(t, fetched, *usage.UpdatedAt)
	require.Equal(t, 45.0, usage.FiveHour.Utilization)
	require.Equal(t, 70.0, usage.SevenDay.Utilization)
	require.InDelta(t, 7200, usage.FiveHour.RemainingSeconds, 2)
	require.Zero(t, repo.writes)
	require.Zero(t, fetcher.calls)
	_, err = svc.GetReadOnlyUsage(t.Context(), 1, "active")
	require.NoError(t, err)
	require.Zero(t, repo.writes)

	// 完整管理员路径仍保留已有的采样回写和错误恢复行为。
	_, err = svc.GetUsage(t.Context(), 1)
	require.NoError(t, err)
	require.GreaterOrEqual(t, repo.writes, 3)
	require.Zero(t, fetcher.calls)
}

func TestAccountUsageReadOnlyFreshQueryCachesAndSanitizesFailure(t *testing.T) {
	reset := time.Now().Add(time.Hour).UTC()
	response := &ClaudeUsageResponse{}
	response.FiveHour.Utilization = 12
	response.FiveHour.ResetsAt = reset.Format(time.RFC3339)
	repo := &readOnlyUsageRepo{accounts: map[int64]*Account{1: {ID: 1, Platform: PlatformAnthropic, Type: AccountTypeOAuth, Credentials: map[string]any{"access_token": "synthetic-token"}}}}
	fetcher := &readOnlyUsageFetcher{response: response}
	svc := &AccountUsageService{accountRepo: repo, cache: NewUsageCache(), usageFetcher: fetcher}
	usage, err := svc.GetReadOnlyUsage(t.Context(), 1, "active")
	require.NoError(t, err)
	require.Equal(t, 12.0, usage.FiveHour.Utilization)
	require.Equal(t, 1, fetcher.calls)
	_, err = svc.GetReadOnlyUsage(t.Context(), 1, "active")
	require.NoError(t, err)
	require.Equal(t, 1, fetcher.calls)
	require.Zero(t, repo.writes)

	svc.cache = NewUsageCache()
	fetcher.err = errors.New("private-upstream-detail")
	_, err = svc.GetReadOnlyUsage(t.Context(), 1, "active")
	require.Error(t, err)
	require.NotContains(t, err.Error(), "private-upstream-detail")
	_, err = svc.GetReadOnlyUsage(t.Context(), 1, "active")
	require.Error(t, err)
	require.Equal(t, 2, fetcher.calls)
	require.Zero(t, repo.writes)
}

func TestAccountUsageReadOnlyBatchUsesSnapshotsOnly(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	reset := now.Add(2 * time.Hour)
	repo := &readOnlyUsageRepo{accounts: map[int64]*Account{
		1: {ID: 1, Platform: PlatformAnthropic, Type: AccountTypeOAuth, SessionWindowEnd: &reset, Extra: map[string]any{"session_window_utilization": 0.42, "passive_usage_sampled_at": now.Format(time.RFC3339), "passive_usage_7d_utilization": 0.7}},
		2: {ID: 2, Platform: PlatformOpenCodeGo, Type: AccountTypeAPIKey, Credentials: map[string]any{"account_mode": "go"}, Extra: map[string]any{"opencode_go_usage_source": "official_console", "opencode_go_usage_5h_used_percent": 90.0}},
		3: {ID: 3, Platform: PlatformAnthropic, Type: AccountTypeSetupToken, SessionWindowEnd: &reset, Extra: map[string]any{"session_window_utilization": 0.1}},
		4: {ID: 4, Platform: PlatformAnthropic, Type: AccountTypeOAuth},
		5: {ID: 5, Platform: PlatformAnthropic, Type: AccountTypeAPIKey},
	}}
	svc := &AccountUsageService{accountRepo: repo}
	usage, failures, err := svc.GetReadOnlyUsageBatch(t.Context(), []int64{1, 2, 3, 4, 5, 99, 1, -1, 0})
	require.NoError(t, err)
	require.Len(t, usage, 2)
	require.Len(t, failures, 4)
	require.Contains(t, failures, int64(2))
	require.Contains(t, failures, int64(4))
	require.Contains(t, failures, int64(5))
	require.Contains(t, failures, int64(99))
	require.Equal(t, 42.0, usage[1].FiveHour.Utilization)
	require.Equal(t, now, *usage[1].UpdatedAt)
	require.Equal(t, 10.0, usage[3].FiveHour.Utilization)
	require.Zero(t, repo.writes)
	// 没有配置任何上游查询器、奖励兑换器或统计查询器，批量查询仍可完成。
	passive, err := svc.GetReadOnlyUsage(t.Context(), 1, "passive")
	require.NoError(t, err)
	require.Equal(t, 42.0, passive.FiveHour.Utilization)
	_, err = svc.GetReadOnlyUsage(t.Context(), 99, "active")
	require.ErrorIs(t, err, ErrAccountNotFound)
}

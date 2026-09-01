package service

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/stretchr/testify/require"
)

type schedulerSnapshotRetryCacheStub struct {
	SchedulerCache
	mu               sync.Mutex
	snapshots        map[string][]Account
	accountsByID     map[int64]*Account
	getSnapshotCalls int
	setSnapshotCalls int
	setSnapshotErr   error
}

func newSchedulerSnapshotRetryCacheStub() *schedulerSnapshotRetryCacheStub {
	return &schedulerSnapshotRetryCacheStub{
		snapshots:    make(map[string][]Account),
		accountsByID: make(map[int64]*Account),
	}
}

func (c *schedulerSnapshotRetryCacheStub) seed(bucket SchedulerBucket, accounts ...Account) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.snapshots[bucket.String()] = append([]Account(nil), accounts...)
	for i := range accounts {
		account := accounts[i]
		c.accountsByID[account.ID] = &account
	}
}

func (c *schedulerSnapshotRetryCacheStub) GetSnapshot(_ context.Context, bucket SchedulerBucket) ([]*Account, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.getSnapshotCalls++
	accounts := c.snapshots[bucket.String()]
	if len(accounts) == 0 {
		return nil, false, nil
	}
	result := make([]*Account, 0, len(accounts))
	for i := range accounts {
		account := accounts[i]
		result = append(result, &account)
	}
	return result, true, nil
}

func (c *schedulerSnapshotRetryCacheStub) SetSnapshot(_ context.Context, bucket SchedulerBucket, _ SchedulerBucketWriteToken, accounts []Account) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.setSnapshotCalls++
	if c.setSnapshotErr != nil {
		return c.setSnapshotErr
	}
	c.snapshots[bucket.String()] = append([]Account(nil), accounts...)
	for i := range accounts {
		account := accounts[i]
		c.accountsByID[account.ID] = &account
	}
	return nil
}

func (c *schedulerSnapshotRetryCacheStub) CaptureBucketWriteToken(_ context.Context, _ SchedulerBucket) (SchedulerBucketWriteToken, error) {
	return SchedulerBucketWriteToken{}, nil
}

func (c *schedulerSnapshotRetryCacheStub) RetireBucket(_ context.Context, _ SchedulerBucket) error {
	return nil
}

func (c *schedulerSnapshotRetryCacheStub) ReopenBucket(_ context.Context, _ SchedulerBucket) (SchedulerBucketWriteToken, error) {
	return SchedulerBucketWriteToken{}, nil
}

func (c *schedulerSnapshotRetryCacheStub) TryAcquireGroupLifecycleLease(_ context.Context, _ int64, _ time.Duration) (SchedulerGroupLifecycleLease, bool, error) {
	return SchedulerGroupLifecycleLease{}, true, nil
}

func (c *schedulerSnapshotRetryCacheStub) ReleaseGroupLifecycleLease(_ context.Context, _ SchedulerGroupLifecycleLease) error {
	return nil
}

func (c *schedulerSnapshotRetryCacheStub) GetAccount(_ context.Context, accountID int64) (*Account, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	account := c.accountsByID[accountID]
	if account == nil {
		return nil, nil
	}
	cloned := *account
	return &cloned, nil
}

func (c *schedulerSnapshotRetryCacheStub) setAccount(account Account) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.accountsByID[account.ID] = &account
}

func (c *schedulerSnapshotRetryCacheStub) callCounts() (getSnapshot, setSnapshot int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.getSnapshotCalls, c.setSnapshotCalls
}

type schedulerSnapshotRetryAccountRepoStub struct {
	AccountRepository
	byGroup   map[int64][]Account
	listCalls atomic.Int32
	getCalls  atomic.Int32
}

func (r *schedulerSnapshotRetryAccountRepoStub) accountsForGroup(groupID int64) []Account {
	return append([]Account(nil), r.byGroup[groupID]...)
}

func (r *schedulerSnapshotRetryAccountRepoStub) GetByID(_ context.Context, id int64) (*Account, error) {
	r.getCalls.Add(1)
	for _, accounts := range r.byGroup {
		for i := range accounts {
			if accounts[i].ID == id {
				account := accounts[i]
				return &account, nil
			}
		}
	}
	return nil, ErrAccountNotFound
}

func (r *schedulerSnapshotRetryAccountRepoStub) ListSchedulableByGroupIDAndPlatform(_ context.Context, groupID int64, platform string) ([]Account, error) {
	r.listCalls.Add(1)
	return filterSchedulerSnapshotRetryAccounts(r.accountsForGroup(groupID), map[string]struct{}{platform: {}}), nil
}

func (r *schedulerSnapshotRetryAccountRepoStub) ListSchedulableByGroupIDAndPlatforms(_ context.Context, groupID int64, platforms []string) ([]Account, error) {
	r.listCalls.Add(1)
	allowed := make(map[string]struct{}, len(platforms))
	for _, platform := range platforms {
		allowed[platform] = struct{}{}
	}
	return filterSchedulerSnapshotRetryAccounts(r.accountsForGroup(groupID), allowed), nil
}

func (r *schedulerSnapshotRetryAccountRepoStub) ListSchedulableByPlatform(_ context.Context, platform string) ([]Account, error) {
	r.listCalls.Add(1)
	return filterSchedulerSnapshotRetryAccounts(r.accountsForGroup(0), map[string]struct{}{platform: {}}), nil
}

func (r *schedulerSnapshotRetryAccountRepoStub) ListSchedulableByPlatforms(_ context.Context, platforms []string) ([]Account, error) {
	r.listCalls.Add(1)
	allowed := make(map[string]struct{}, len(platforms))
	for _, platform := range platforms {
		allowed[platform] = struct{}{}
	}
	return filterSchedulerSnapshotRetryAccounts(r.accountsForGroup(0), allowed), nil
}

func (r *schedulerSnapshotRetryAccountRepoStub) ListSchedulableUngroupedByPlatform(ctx context.Context, platform string) ([]Account, error) {
	return r.ListSchedulableByPlatform(ctx, platform)
}

func (r *schedulerSnapshotRetryAccountRepoStub) ListSchedulableUngroupedByPlatforms(ctx context.Context, platforms []string) ([]Account, error) {
	return r.ListSchedulableByPlatforms(ctx, platforms)
}

func (r *schedulerSnapshotRetryAccountRepoStub) listCallCount() int {
	return int(r.listCalls.Load())
}

type schedulerSnapshotRetryBlockingRepoStub struct {
	*schedulerSnapshotRetryAccountRepoStub
	started   chan struct{}
	release   chan struct{}
	startOnce sync.Once
}

func (r *schedulerSnapshotRetryBlockingRepoStub) ListSchedulableByGroupIDAndPlatforms(_ context.Context, groupID int64, platforms []string) ([]Account, error) {
	r.listCalls.Add(1)
	r.startOnce.Do(func() { close(r.started) })
	<-r.release
	allowed := make(map[string]struct{}, len(platforms))
	for _, platform := range platforms {
		allowed[platform] = struct{}{}
	}
	return filterSchedulerSnapshotRetryAccounts(r.accountsForGroup(groupID), allowed), nil
}

func filterSchedulerSnapshotRetryAccounts(accounts []Account, allowed map[string]struct{}) []Account {
	result := make([]Account, 0, len(accounts))
	for _, account := range accounts {
		if _, ok := allowed[account.Platform]; ok {
			result = append(result, account)
		}
	}
	return result
}

func newSchedulerSnapshotRetryConfig() *config.Config {
	cfg := &config.Config{}
	cfg.Gateway.Scheduling.DbFallbackEnabled = true
	return cfg
}

func newSchedulerSnapshotRetryAccount(id int64, platform, model string) Account {
	account := Account{
		ID:          id,
		Platform:    platform,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
	}
	if model != "" {
		account.Credentials = map[string]any{
			"model_mapping": map[string]any{model: model},
		}
	}
	return account
}

func setSchedulerSnapshotRetryModelLimit(account *Account, model string) {
	account.Extra = map[string]any{
		"model_rate_limits": map[string]any{
			model: map[string]any{
				"rate_limit_reset_at": time.Now().Add(time.Hour).Format(time.RFC3339),
			},
		},
	}
}

func newSchedulerSnapshotRetryGroup(id int64, platform string) *Group {
	return &Group{
		ID:               id,
		Name:             "snapshot retry group",
		Platform:         platform,
		Status:           StatusActive,
		Hydrated:         true,
		SubscriptionType: SubscriptionTypeStandard,
		RateMultiplier:   1,
	}
}

func TestShouldPersistSchedulerPrivacyFailureWaitsForAuthoritativeReadAfterCacheHit(t *testing.T) {
	ctx, _ := withSchedulerSnapshotRetryState(context.Background())
	require.True(t, shouldPersistSchedulerPrivacyFailure(ctx))

	markSchedulerSnapshotCacheHit(ctx)
	require.False(t, shouldPersistSchedulerPrivacyFailure(ctx))

	authoritativeCtx := context.WithValue(ctx, schedulerSnapshotAuthoritativeReadKey{}, true)
	require.True(t, shouldPersistSchedulerPrivacyFailure(authoritativeCtx))
}

func TestGatewaySchedulingRetriesNonEmptyStaleSnapshotOnce(t *testing.T) {
	groupID := int64(701)
	model := "claude-snapshot-retry"
	group := newSchedulerSnapshotRetryGroup(groupID, PlatformAnthropic)
	stale := newSchedulerSnapshotRetryAccount(7101, PlatformAnthropic, model)
	limitedUntil := time.Now().Add(time.Hour)
	stale.RateLimitResetAt = &limitedUntil
	fresh := newSchedulerSnapshotRetryAccount(7102, PlatformAnthropic, model)

	cache := newSchedulerSnapshotRetryCacheStub()
	cache.seed(SchedulerBucket{GroupID: groupID, Platform: PlatformAnthropic, Mode: SchedulerModeMixed}, stale)
	cache.setSnapshotErr = errors.New("cache write failed")
	repo := &schedulerSnapshotRetryAccountRepoStub{byGroup: map[int64][]Account{groupID: {fresh}}}
	cfg := newSchedulerSnapshotRetryConfig()
	cfg.Gateway.Scheduling.DbFallbackMaxQPS = 1
	snapshot := NewSchedulerSnapshotService(cache, nil, repo, nil, cfg)
	svc := &GatewayService{accountRepo: repo, schedulerSnapshot: snapshot, cfg: cfg}
	ctx := context.WithValue(context.Background(), ctxkey.Group, group)

	for range 2 {
		selection, err := svc.SelectAccountWithLoadAwareness(ctx, &groupID, "", model, nil, "", 0)
		require.NoError(t, err)
		require.NotNil(t, selection)
		require.NotNil(t, selection.Account)
		require.Equal(t, fresh.ID, selection.Account.ID)
	}
	getSnapshotCalls, setSnapshotCalls := cache.callCounts()
	require.Equal(t, 2, getSnapshotCalls, "权威重读必须绕过旧快照")
	require.Equal(t, 1, setSnapshotCalls)
	require.Equal(t, 1, repo.listCallCount(), "嵌套调度入口只能回源一次")
}

func TestGatewaySchedulingDoesNotFallbackWhenSnapshotCanServeRequest(t *testing.T) {
	groupID := int64(702)
	model := "claude-snapshot-hit"
	group := newSchedulerSnapshotRetryGroup(groupID, PlatformAnthropic)
	cached := newSchedulerSnapshotRetryAccount(7201, PlatformAnthropic, model)
	cache := newSchedulerSnapshotRetryCacheStub()
	cache.seed(SchedulerBucket{GroupID: groupID, Platform: PlatformAnthropic, Mode: SchedulerModeMixed}, cached)
	repo := &schedulerSnapshotRetryAccountRepoStub{byGroup: map[int64][]Account{groupID: {
		newSchedulerSnapshotRetryAccount(7202, PlatformAnthropic, model),
	}}}
	cfg := newSchedulerSnapshotRetryConfig()
	snapshot := NewSchedulerSnapshotService(cache, nil, repo, nil, cfg)
	svc := &GatewayService{accountRepo: repo, schedulerSnapshot: snapshot, cfg: cfg}
	ctx := context.WithValue(context.Background(), ctxkey.Group, group)

	account, err := svc.SelectAccountForModelWithExclusions(ctx, &groupID, "", model, nil)
	require.NoError(t, err)
	require.Equal(t, cached.ID, account.ID)
	_, setSnapshotCalls := cache.callCounts()
	require.Equal(t, 0, repo.listCallCount())
	require.Equal(t, 0, setSnapshotCalls)
}

func TestGatewaySchedulingStopsAfterOneAuthoritativeRead(t *testing.T) {
	groupID := int64(703)
	model := "claude-no-account"
	group := newSchedulerSnapshotRetryGroup(groupID, PlatformAnthropic)
	stale := newSchedulerSnapshotRetryAccount(7301, PlatformAnthropic, model)
	limitedUntil := time.Now().Add(time.Hour)
	stale.RateLimitResetAt = &limitedUntil
	cache := newSchedulerSnapshotRetryCacheStub()
	cache.seed(SchedulerBucket{GroupID: groupID, Platform: PlatformAnthropic, Mode: SchedulerModeMixed}, stale)
	repo := &schedulerSnapshotRetryAccountRepoStub{byGroup: map[int64][]Account{groupID: {}}}
	cfg := newSchedulerSnapshotRetryConfig()
	snapshot := NewSchedulerSnapshotService(cache, nil, repo, nil, cfg)
	svc := &GatewayService{accountRepo: repo, schedulerSnapshot: snapshot, cfg: cfg}
	ctx := context.WithValue(context.Background(), ctxkey.Group, group)

	account, err := svc.SelectAccountForModelWithExclusions(ctx, &groupID, "", model, nil)
	require.Nil(t, account)
	require.ErrorIs(t, err, ErrNoAvailableAccounts)
	getSnapshotCalls, setSnapshotCalls := cache.callCounts()
	require.Equal(t, 1, repo.listCallCount())
	require.Equal(t, 1, getSnapshotCalls)
	require.Equal(t, 1, setSnapshotCalls)
}

func TestSchedulerAuthoritativeRefreshCooldownAvoidsRepeatedFallback(t *testing.T) {
	groupID := int64(707)
	model := "claude-model-limited"
	group := newSchedulerSnapshotRetryGroup(groupID, PlatformAnthropic)
	limited := newSchedulerSnapshotRetryAccount(7701, PlatformAnthropic, model)
	setSchedulerSnapshotRetryModelLimit(&limited, model)
	cache := newSchedulerSnapshotRetryCacheStub()
	cache.seed(SchedulerBucket{GroupID: groupID, Platform: PlatformAnthropic, Mode: SchedulerModeMixed}, limited)
	repo := &schedulerSnapshotRetryAccountRepoStub{byGroup: map[int64][]Account{groupID: {limited}}}
	cfg := newSchedulerSnapshotRetryConfig()
	snapshot := NewSchedulerSnapshotService(cache, nil, repo, nil, cfg)
	svc := &GatewayService{accountRepo: repo, schedulerSnapshot: snapshot, cfg: cfg}
	ctx := context.WithValue(context.Background(), ctxkey.Group, group)

	for range 2 {
		account, err := svc.SelectAccountForModelWithExclusions(ctx, &groupID, "", model, nil)
		require.Nil(t, account)
		require.ErrorIs(t, err, ErrNoAvailableAccounts)
	}

	getSnapshotCalls, setSnapshotCalls := cache.callCounts()
	require.Equal(t, 2, getSnapshotCalls)
	require.Equal(t, 1, setSnapshotCalls)
	require.Equal(t, 1, repo.listCallCount(), "冷却窗口内相同桶不能重复回源")
}

func TestSchedulerAuthoritativeRefreshCoalescesConcurrentFallbacks(t *testing.T) {
	groupID := int64(708)
	model := "claude-concurrent-model-limit"
	group := newSchedulerSnapshotRetryGroup(groupID, PlatformAnthropic)
	limited := newSchedulerSnapshotRetryAccount(7801, PlatformAnthropic, model)
	setSchedulerSnapshotRetryModelLimit(&limited, model)
	cache := newSchedulerSnapshotRetryCacheStub()
	cache.seed(SchedulerBucket{GroupID: groupID, Platform: PlatformAnthropic, Mode: SchedulerModeMixed}, limited)
	baseRepo := &schedulerSnapshotRetryAccountRepoStub{byGroup: map[int64][]Account{groupID: {limited}}}
	repo := &schedulerSnapshotRetryBlockingRepoStub{
		schedulerSnapshotRetryAccountRepoStub: baseRepo,
		started:                               make(chan struct{}),
		release:                               make(chan struct{}),
	}
	cfg := newSchedulerSnapshotRetryConfig()
	snapshot := NewSchedulerSnapshotService(cache, nil, repo, nil, cfg)
	svc := &GatewayService{accountRepo: repo, schedulerSnapshot: snapshot, cfg: cfg}
	ctx := context.WithValue(context.Background(), ctxkey.Group, group)

	const requestCount = 12
	errorsCh := make(chan error, requestCount)
	var wg sync.WaitGroup
	for range requestCount {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := svc.SelectAccountForModelWithExclusions(ctx, &groupID, "", model, nil)
			errorsCh <- err
		}()
	}

	select {
	case <-repo.started:
	case <-time.After(time.Second):
		t.Fatal("权威回源未启动")
	}
	close(repo.release)
	wg.Wait()
	close(errorsCh)
	for err := range errorsCh {
		require.ErrorIs(t, err, ErrNoAvailableAccounts)
	}

	_, setSnapshotCalls := cache.callCounts()
	require.Equal(t, 1, setSnapshotCalls)
	require.Equal(t, 1, repo.listCallCount(), "同一桶的并发权威回源必须合并")
}

func TestSchedulerAuthoritativeRefreshReturnsIsolatedAccounts(t *testing.T) {
	groupID := int64(715)
	bucket := SchedulerBucket{GroupID: groupID, Platform: PlatformAnthropic, Mode: SchedulerModeMixed}
	account := newSchedulerSnapshotRetryAccount(71501, PlatformAnthropic, "claude-isolated")
	account.Credentials["access_token"] = "initial-token"
	account.Extra = map[string]any{"mutable": map[string]any{"value": "initial"}}
	cache := newSchedulerSnapshotRetryCacheStub()
	cache.setSnapshotErr = errors.New("cache write failed")
	baseRepo := &schedulerSnapshotRetryAccountRepoStub{byGroup: map[int64][]Account{groupID: {account}}}
	repo := &schedulerSnapshotRetryBlockingRepoStub{
		schedulerSnapshotRetryAccountRepoStub: baseRepo,
		started:                               make(chan struct{}),
		release:                               make(chan struct{}),
	}
	cfg := newSchedulerSnapshotRetryConfig()
	snapshot := NewSchedulerSnapshotService(cache, nil, repo, nil, cfg)

	const requestCount = 12
	errorsCh := make(chan error, requestCount)
	mutate := make(chan struct{})
	var loaded sync.WaitGroup
	var finished sync.WaitGroup
	loaded.Add(requestCount)
	finished.Add(requestCount)
	for i := 0; i < requestCount; i++ {
		go func(value int) {
			defer finished.Done()
			accounts, authoritative, err := snapshot.loadAccountsAuthoritatively(context.Background(), bucket, true)
			if err == nil && (!authoritative || len(accounts) != 1) {
				err = errors.New("权威结果无效")
			}
			loaded.Done()
			<-mutate
			if err == nil {
				accounts[0].Credentials["access_token"] = value
				if mutable, ok := accounts[0].Extra["mutable"].(map[string]any); ok {
					mutable["value"] = value
				}
			}
			errorsCh <- err
		}(i)
	}

	select {
	case <-repo.started:
	case <-time.After(time.Second):
		t.Fatal("权威回源未启动")
	}
	close(repo.release)
	loaded.Wait()
	close(mutate)
	finished.Wait()
	close(errorsCh)
	for err := range errorsCh {
		require.NoError(t, err)
	}
	require.Equal(t, "initial-token", baseRepo.byGroup[groupID][0].Credentials["access_token"])
	if mutable, ok := baseRepo.byGroup[groupID][0].Extra["mutable"].(map[string]any); ok {
		require.Equal(t, "initial", mutable["value"])
	}
	require.Equal(t, 1, repo.listCallCount())
}

func TestGeminiSchedulingRetriesNonEmptyStaleSnapshot(t *testing.T) {
	groupID := int64(704)
	model := "gemini-snapshot-retry"
	group := newSchedulerSnapshotRetryGroup(groupID, PlatformGemini)
	stale := newSchedulerSnapshotRetryAccount(7401, PlatformGemini, model)
	limitedUntil := time.Now().Add(time.Hour)
	stale.RateLimitResetAt = &limitedUntil
	fresh := newSchedulerSnapshotRetryAccount(7402, PlatformGemini, model)
	cache := newSchedulerSnapshotRetryCacheStub()
	cache.seed(SchedulerBucket{GroupID: groupID, Platform: PlatformGemini, Mode: SchedulerModeMixed}, stale)
	repo := &schedulerSnapshotRetryAccountRepoStub{byGroup: map[int64][]Account{groupID: {fresh}}}
	cfg := newSchedulerSnapshotRetryConfig()
	snapshot := NewSchedulerSnapshotService(cache, nil, repo, nil, cfg)
	svc := &GeminiMessagesCompatService{accountRepo: repo, schedulerSnapshot: snapshot, cfg: cfg}
	ctx := context.WithValue(context.Background(), ctxkey.Group, group)

	account, err := svc.SelectAccountForModelWithExclusions(ctx, &groupID, "", model, nil)
	require.NoError(t, err)
	require.NotNil(t, account)
	require.Equal(t, fresh.ID, account.ID)
	getSnapshotCalls, setSnapshotCalls := cache.callCounts()
	require.Equal(t, 1, repo.listCallCount())
	require.Equal(t, 1, getSnapshotCalls)
	require.Equal(t, 1, setSnapshotCalls)
}

func TestGeminiAIStudioSchedulingRetriesNonEmptyStaleSnapshot(t *testing.T) {
	groupID := int64(709)
	stale := newSchedulerSnapshotRetryAccount(7901, PlatformGemini, "")
	stale.Credentials = map[string]any{"api_key": "stale-key"}
	limitedUntil := time.Now().Add(time.Hour)
	stale.RateLimitResetAt = &limitedUntil
	fresh := newSchedulerSnapshotRetryAccount(7902, PlatformGemini, "")
	fresh.Credentials = map[string]any{"api_key": "fresh-key"}
	cache := newSchedulerSnapshotRetryCacheStub()
	cache.seed(SchedulerBucket{GroupID: groupID, Platform: PlatformGemini, Mode: SchedulerModeForced}, stale)
	cache.setSnapshotErr = errors.New("cache write failed")
	repo := &schedulerSnapshotRetryAccountRepoStub{byGroup: map[int64][]Account{groupID: {fresh}}}
	cfg := newSchedulerSnapshotRetryConfig()
	snapshot := NewSchedulerSnapshotService(cache, nil, repo, nil, cfg)
	svc := &GeminiMessagesCompatService{accountRepo: repo, schedulerSnapshot: snapshot, cfg: cfg}

	account, err := svc.SelectAccountForAIStudioEndpoints(context.Background(), &groupID)
	require.NoError(t, err)
	require.NotNil(t, account)
	require.Equal(t, fresh.ID, account.ID)
	getSnapshotCalls, setSnapshotCalls := cache.callCounts()
	require.Equal(t, 1, repo.listCallCount())
	require.Equal(t, 1, getSnapshotCalls)
	require.Equal(t, 1, setSnapshotCalls)
}

func TestGeminiAIStudioRejectsAccountsWithoutUsableCredentials(t *testing.T) {
	groupID := int64(710)
	missingAPIKey := newSchedulerSnapshotRetryAccount(71001, PlatformGemini, "")
	unknownType := newSchedulerSnapshotRetryAccount(71002, PlatformGemini, "")
	unknownType.Type = "unknown"
	cache := newSchedulerSnapshotRetryCacheStub()
	cache.seed(SchedulerBucket{GroupID: groupID, Platform: PlatformGemini, Mode: SchedulerModeForced}, missingAPIKey, unknownType)
	repo := &schedulerSnapshotRetryAccountRepoStub{byGroup: map[int64][]Account{groupID: {missingAPIKey, unknownType}}}
	cfg := newSchedulerSnapshotRetryConfig()
	snapshot := NewSchedulerSnapshotService(cache, nil, repo, nil, cfg)
	svc := &GeminiMessagesCompatService{accountRepo: repo, schedulerSnapshot: snapshot, cfg: cfg}

	account, err := svc.SelectAccountForAIStudioEndpoints(context.Background(), &groupID)
	require.Nil(t, account)
	require.ErrorContains(t, err, "no available Gemini accounts")
	require.Equal(t, 1, repo.listCallCount())
}

func TestGeminiAIStudioHydrationFailureUsesAuthoritativeAccount(t *testing.T) {
	groupID := int64(711)
	cached := newSchedulerSnapshotRetryAccount(71101, PlatformGemini, "")
	cached.Credentials = map[string]any{"api_key": "cached-key"}
	hydratedWithoutKey := cached
	hydratedWithoutKey.Credentials = nil
	fresh := newSchedulerSnapshotRetryAccount(71102, PlatformGemini, "")
	fresh.Credentials = map[string]any{"api_key": "fresh-key"}
	cache := newSchedulerSnapshotRetryCacheStub()
	cache.seed(SchedulerBucket{GroupID: groupID, Platform: PlatformGemini, Mode: SchedulerModeForced}, cached)
	cache.setAccount(hydratedWithoutKey)
	cache.setSnapshotErr = errors.New("cache write failed")
	repo := &schedulerSnapshotRetryAccountRepoStub{byGroup: map[int64][]Account{groupID: {fresh}}}
	cfg := newSchedulerSnapshotRetryConfig()
	snapshot := NewSchedulerSnapshotService(cache, nil, repo, nil, cfg)
	svc := &GeminiMessagesCompatService{accountRepo: repo, schedulerSnapshot: snapshot, cfg: cfg}

	account, err := svc.SelectAccountForAIStudioEndpoints(context.Background(), &groupID)
	require.NoError(t, err)
	require.NotNil(t, account)
	require.Equal(t, fresh.ID, account.ID)
	require.Equal(t, 1, repo.listCallCount())
}

func TestOpenAIAdvancedSchedulingRetriesNonEmptyStaleSnapshot(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()
	defer resetOpenAIAdvancedSchedulerSettingCacheForTest()

	groupID := int64(704)
	model := "gpt-snapshot-retry"
	stale := newSchedulerSnapshotRetryAccount(7401, PlatformOpenAI, model)
	limitedUntil := time.Now().Add(time.Hour)
	stale.RateLimitResetAt = &limitedUntil
	fresh := newSchedulerSnapshotRetryAccount(7402, PlatformOpenAI, model)
	cache := newSchedulerSnapshotRetryCacheStub()
	cache.seed(SchedulerBucket{GroupID: groupID, Platform: PlatformOpenAI, Mode: SchedulerModeSingle}, stale)
	cache.setSnapshotErr = errors.New("cache write failed")
	repo := &schedulerSnapshotRetryAccountRepoStub{byGroup: map[int64][]Account{groupID: {fresh}}}
	cfg := newSchedulerSnapshotRetryConfig()
	snapshot := NewSchedulerSnapshotService(cache, nil, repo, nil, cfg)
	svc := &OpenAIGatewayService{
		accountRepo:       repo,
		schedulerSnapshot: snapshot,
		cfg:               cfg,
		rateLimitService:  newOpenAIAdvancedSchedulerRateLimitService("true"),
	}

	selection, decision, err := svc.SelectAccountWithScheduler(
		context.Background(),
		&groupID,
		"",
		"",
		model,
		nil,
		OpenAIUpstreamTransportAny,
		false,
	)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, fresh.ID, selection.Account.ID)
	require.Equal(t, openAIAccountScheduleLayerLoadBalance, decision.Layer)
	getSnapshotCalls, setSnapshotCalls := cache.callCounts()
	require.Equal(t, 1, repo.listCallCount())
	require.Equal(t, 1, getSnapshotCalls)
	require.Equal(t, 1, setSnapshotCalls)
}

func TestOpenAIAdvancedSchedulingUsesAuthoritativeCandidateWhenProxyQuarantineFailsOpen(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()
	defer resetOpenAIAdvancedSchedulerSettingCacheForTest()

	groupID := int64(714)
	model := "gpt-snapshot-quarantine-fail-open"
	stale := newSchedulerSnapshotRetryAccount(71401, PlatformOpenAI, model)
	limitedUntil := time.Now().Add(time.Hour)
	stale.RateLimitResetAt = &limitedUntil

	proxyID := int64(714)
	fresh := newSchedulerSnapshotRetryAccount(71402, PlatformOpenAI, model)
	fresh.ProxyID = &proxyID

	cache := newSchedulerSnapshotRetryCacheStub()
	cache.seed(SchedulerBucket{GroupID: groupID, Platform: PlatformOpenAI, Mode: SchedulerModeSingle}, stale)
	cache.setSnapshotErr = errors.New("cache write failed")
	repo := &schedulerSnapshotRetryAccountRepoStub{byGroup: map[int64][]Account{groupID: {fresh}}}
	cfg := newSchedulerSnapshotRetryConfig()
	snapshot := NewSchedulerSnapshotService(cache, nil, repo, nil, cfg)
	circuit := newOpenAIProxyStreamCircuit(openAIProxyStreamCircuitSettings{
		failureThreshold: 1,
		failureWindow:    time.Minute,
		quarantineTTL:    10 * time.Minute,
		maxEntries:       16,
	})
	tripped, _ := circuit.recordFailure(proxyID, time.Now())
	require.True(t, tripped)

	svc := &OpenAIGatewayService{
		accountRepo:              repo,
		schedulerSnapshot:        snapshot,
		cfg:                      cfg,
		rateLimitService:         newOpenAIAdvancedSchedulerRateLimitService("true"),
		openaiProxyStreamCircuit: circuit,
	}

	selection, _, err := svc.SelectAccountWithScheduler(
		context.Background(),
		&groupID,
		"",
		"",
		model,
		nil,
		OpenAIUpstreamTransportAny,
		false,
	)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, fresh.ID, selection.Account.ID)
	require.NotEqual(t, stale.ID, selection.Account.ID)
	require.GreaterOrEqual(t, repo.listCallCount(), 1)
	require.True(t, circuit.isBlocked(proxyID, time.Now()), "fail-open must not clear quarantine")
}

func TestResolveAPIKeyRoutingGroupRetriesWhenStaleCandidatesAreUnschedulable(t *testing.T) {
	first := newSchedulerSnapshotRetryGroup(705, PlatformOpenAI)
	second := newSchedulerSnapshotRetryGroup(706, PlatformOpenAI)
	model := "gpt-routing-snapshot-retry"
	limitedUntil := time.Now().Add(time.Hour)
	staleFirst := newSchedulerSnapshotRetryAccount(7501, PlatformOpenAI, model)
	staleFirst.RateLimitResetAt = &limitedUntil
	staleSecond := newSchedulerSnapshotRetryAccount(7502, PlatformOpenAI, model)
	staleSecond.RateLimitResetAt = &limitedUntil
	freshSecond := newSchedulerSnapshotRetryAccount(7503, PlatformOpenAI, model)

	cache := newSchedulerSnapshotRetryCacheStub()
	cache.seed(SchedulerBucket{GroupID: first.ID, Platform: PlatformOpenAI, Mode: SchedulerModeSingle}, staleFirst)
	cache.seed(SchedulerBucket{GroupID: second.ID, Platform: PlatformOpenAI, Mode: SchedulerModeSingle}, staleSecond)
	repo := &schedulerSnapshotRetryAccountRepoStub{byGroup: map[int64][]Account{
		first.ID:  {},
		second.ID: {freshSecond},
	}}
	cfg := newSchedulerSnapshotRetryConfig()
	snapshot := NewSchedulerSnapshotService(cache, nil, repo, nil, cfg)
	svc := &GatewayService{accountRepo: repo, schedulerSnapshot: snapshot, cfg: cfg}
	key := &APIKey{
		ID:              7601,
		UserID:          7602,
		GroupID:         &first.ID,
		Group:           first,
		RoutingPlatform: PlatformOpenAI,
		RoutingStrategy: APIKeyRoutingStrategyManual,
		RoutingGroups: []APIKeyGroupBinding{
			{GroupID: first.ID, Priority: 0, Group: first},
			{GroupID: second.ID, Priority: 1, Group: second},
		},
		Status: StatusActive,
		User:   &User{ID: 7602, Status: StatusActive, Balance: 100},
	}

	resolved, err := svc.ResolveAPIKeyRoutingGroup(context.Background(), key, APIKeyRoutingResolveInput{Model: model})
	require.NoError(t, err)
	require.NotNil(t, resolved)
	require.Equal(t, second.ID, *resolved.GroupID)
	getSnapshotCalls, setSnapshotCalls := cache.callCounts()
	require.Equal(t, 2, getSnapshotCalls)
	require.Equal(t, 2, setSnapshotCalls)
	require.Equal(t, 2, repo.listCallCount())
}

func TestResolveAPIKeyRoutingGroupStopsAuthoritativeScanAfterCancellation(t *testing.T) {
	first := newSchedulerSnapshotRetryGroup(712, PlatformAnthropic)
	second := newSchedulerSnapshotRetryGroup(713, PlatformAnthropic)
	model := "claude-routing-cancel"
	limitedUntil := time.Now().Add(time.Hour)
	staleFirst := newSchedulerSnapshotRetryAccount(71201, PlatformAnthropic, model)
	staleFirst.RateLimitResetAt = &limitedUntil
	staleSecond := newSchedulerSnapshotRetryAccount(71301, PlatformAnthropic, model)
	staleSecond.RateLimitResetAt = &limitedUntil
	freshFirst := newSchedulerSnapshotRetryAccount(71202, PlatformAnthropic, model)
	freshSecond := newSchedulerSnapshotRetryAccount(71302, PlatformAnthropic, model)

	cache := newSchedulerSnapshotRetryCacheStub()
	cache.seed(SchedulerBucket{GroupID: first.ID, Platform: PlatformAnthropic, Mode: SchedulerModeMixed}, staleFirst)
	cache.seed(SchedulerBucket{GroupID: second.ID, Platform: PlatformAnthropic, Mode: SchedulerModeMixed}, staleSecond)
	baseRepo := &schedulerSnapshotRetryAccountRepoStub{byGroup: map[int64][]Account{
		first.ID:  {freshFirst},
		second.ID: {freshSecond},
	}}
	repo := &schedulerSnapshotRetryBlockingRepoStub{
		schedulerSnapshotRetryAccountRepoStub: baseRepo,
		started:                               make(chan struct{}),
		release:                               make(chan struct{}),
	}
	released := false
	defer func() {
		if !released {
			close(repo.release)
		}
	}()
	cfg := newSchedulerSnapshotRetryConfig()
	snapshot := NewSchedulerSnapshotService(cache, nil, repo, nil, cfg)
	svc := &GatewayService{accountRepo: repo, schedulerSnapshot: snapshot, cfg: cfg}
	key := &APIKey{
		ID:              71401,
		UserID:          71402,
		GroupID:         &first.ID,
		Group:           first,
		RoutingPlatform: PlatformAnthropic,
		RoutingStrategy: APIKeyRoutingStrategyManual,
		RoutingGroups: []APIKeyGroupBinding{
			{GroupID: first.ID, Priority: 0, Group: first},
			{GroupID: second.ID, Priority: 1, Group: second},
		},
		Status: StatusActive,
		User:   &User{ID: 71402, Status: StatusActive, Balance: 100},
	}
	ctx, cancel := context.WithCancel(context.Background())
	resultCh := make(chan error, 1)
	go func() {
		_, err := svc.ResolveAPIKeyRoutingGroup(ctx, key, APIKeyRoutingResolveInput{Model: model})
		resultCh <- err
	}()

	select {
	case <-repo.started:
	case <-time.After(time.Second):
		t.Fatal("权威回源未启动")
	}
	cancel()
	select {
	case err := <-resultCh:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("取消后动态分组解析未及时返回")
	}
	require.Equal(t, 1, repo.listCallCount(), "取消后不能继续扫描后续分组")

	close(repo.release)
	released = true
	require.Eventually(t, func() bool {
		_, setSnapshotCalls := cache.callCounts()
		return setSnapshotCalls == 1
	}, time.Second, 10*time.Millisecond)
}

func TestSchedulerAuthoritativeRecentEntriesExpireAndStayBounded(t *testing.T) {
	svc := NewSchedulerSnapshotService(nil, nil, nil, nil, nil)
	now := time.Now()
	expiredBucket := SchedulerBucket{GroupID: 1, Platform: PlatformAnthropic, Mode: SchedulerModeMixed}
	require.NoError(t, svc.recordAuthoritativeRefresh(expiredBucket, []Account{{ID: 1}}, nil, now.Add(-2*schedulerAuthoritativeRefreshCooldown)))
	_, _, ok := svc.recentAuthoritativeRefresh(expiredBucket, now)
	require.False(t, ok)

	for i := 0; i < schedulerAuthoritativeRefreshMaxEntries+20; i++ {
		bucket := SchedulerBucket{GroupID: int64(i + 1), Platform: PlatformOpenAI, Mode: SchedulerModeSingle}
		require.NoError(t, svc.recordAuthoritativeRefresh(bucket, []Account{{ID: int64(i + 1)}}, nil, now))
	}
	svc.authoritativeMu.Lock()
	entryCount := len(svc.authoritativeRecent)
	svc.authoritativeMu.Unlock()
	require.LessOrEqual(t, entryCount, schedulerAuthoritativeRefreshMaxEntries)
}

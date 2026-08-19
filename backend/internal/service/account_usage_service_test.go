package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
)

type accountUsageCodexProbeRepo struct {
	stubOpenAIAccountRepo
	updateExtraCh chan map[string]any
	rateLimitCh   chan time.Time
}

type stubOpenCodeGoConsoleSummaryFetcher struct {
	calls       int
	workspaceID string
	cookie      string
	summary     *OpenCodeGoConsoleSummary
	err         error
}

func (f *stubOpenCodeGoConsoleSummaryFetcher) FetchSummary(_ context.Context, workspaceID, consoleCookie string) (*OpenCodeGoConsoleSummary, error) {
	f.calls++
	f.workspaceID = workspaceID
	f.cookie = consoleCookie
	return f.summary, f.err
}

func (r *accountUsageCodexProbeRepo) UpdateExtra(_ context.Context, _ int64, updates map[string]any) error {
	if r.updateExtraCh != nil {
		copied := make(map[string]any, len(updates))
		for k, v := range updates {
			copied[k] = v
		}
		r.updateExtraCh <- copied
	}
	return nil
}

func (r *accountUsageCodexProbeRepo) ClearRateLimit(_ context.Context, id int64) error {
	for i := range r.accounts {
		if r.accounts[i].ID == id {
			r.accounts[i].RateLimitedAt = nil
			r.accounts[i].RateLimitResetAt = nil
			r.accounts[i].OverloadUntil = nil
			break
		}
	}
	return nil
}

func (r *accountUsageCodexProbeRepo) SetRateLimited(_ context.Context, _ int64, resetAt time.Time) error {
	if r.rateLimitCh != nil {
		r.rateLimitCh <- resetAt
	}
	return nil
}

func TestShouldRefreshOpenAICodexSnapshot(t *testing.T) {
	t.Parallel()

	rateLimitedUntil := time.Now().Add(5 * time.Minute)
	now := time.Now()
	usage := &UsageInfo{
		FiveHour: &UsageProgress{Utilization: 0},
		SevenDay: &UsageProgress{Utilization: 0},
	}

	if !shouldRefreshOpenAICodexSnapshot(&Account{RateLimitResetAt: &rateLimitedUntil}, usage, now) {
		t.Fatal("expected rate-limited account to force codex snapshot refresh")
	}

	if shouldRefreshOpenAICodexSnapshot(&Account{}, usage, now) {
		t.Fatal("expected complete non-rate-limited usage to skip codex snapshot refresh")
	}

	if !shouldRefreshOpenAICodexSnapshot(&Account{}, &UsageInfo{FiveHour: nil, SevenDay: &UsageProgress{}}, now) {
		t.Fatal("expected missing 5h snapshot to require refresh")
	}

	staleAt := now.Add(-(openAIProbeCacheTTL + time.Minute)).Format(time.RFC3339)
	if !shouldRefreshOpenAICodexSnapshot(&Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Extra: map[string]any{
			"openai_oauth_responses_websockets_v2_enabled": true,
			"codex_usage_updated_at":                       staleAt,
		},
	}, usage, now) {
		t.Fatal("expected stale ws snapshot to trigger refresh")
	}
}

// TestShouldRefreshOpenAICodexSnapshot_SparkShadowIgnoresWSv2 外审第9轮 P1:spark 影子用量走
// QueryUsage(/wham/usage,与 WSv2 无关),staleness 不得被 WSv2 门控,否则首刷后窗口永久冻结。
func TestShouldRefreshOpenAICodexSnapshot_SparkShadowIgnoresWSv2(t *testing.T) {
	t.Parallel()

	now := time.Now()
	usage := &UsageInfo{
		FiveHour: &UsageProgress{Utilization: 0},
		SevenDay: &UsageProgress{Utilization: 0},
	}
	staleAt := now.Add(-(openAIProbeCacheTTL + time.Minute)).Format(time.RFC3339)
	freshAt := now.Add(-time.Minute).Format(time.RFC3339)
	parentID := int64(7001)

	// 影子无 WSv2,但首刷后窗口已存在;过期 codex_usage_updated_at 必须触发再刷新。
	shadowStale := &Account{
		Platform:        PlatformOpenAI,
		Type:            AccountTypeOAuth,
		ParentAccountID: &parentID,
		QuotaDimension:  QuotaDimensionSpark,
		Extra:           map[string]any{"codex_usage_updated_at": staleAt},
	}
	if !shouldRefreshOpenAICodexSnapshot(shadowStale, usage, now) {
		t.Fatal("expected stale spark shadow (no WSv2) to trigger refresh")
	}

	// 影子时间戳仍新鲜→不刷(TTL 生效)。
	shadowFresh := &Account{
		Platform:        PlatformOpenAI,
		Type:            AccountTypeOAuth,
		ParentAccountID: &parentID,
		QuotaDimension:  QuotaDimensionSpark,
		Extra:           map[string]any{"codex_usage_updated_at": freshAt},
	}
	if shouldRefreshOpenAICodexSnapshot(shadowFresh, usage, now) {
		t.Fatal("expected fresh spark shadow to skip refresh (TTL not elapsed)")
	}

	// 反向对照:普通账号无 WSv2 + 过期时间戳→仍不刷(WSv2 门控普通账号的 probe 刷新)。
	normalNoWS := &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Extra:    map[string]any{"codex_usage_updated_at": staleAt},
	}
	if shouldRefreshOpenAICodexSnapshot(normalNoWS, usage, now) {
		t.Fatal("expected non-WSv2 normal account to skip codex probe refresh")
	}
}

func TestExtractOpenAICodexProbeUpdatesAccepts429WithCodexHeaders(t *testing.T) {
	t.Parallel()

	headers := make(http.Header)
	headers.Set("x-codex-primary-used-percent", "100")
	headers.Set("x-codex-primary-reset-after-seconds", "604800")
	headers.Set("x-codex-primary-window-minutes", "10080")
	headers.Set("x-codex-secondary-used-percent", "100")
	headers.Set("x-codex-secondary-reset-after-seconds", "18000")
	headers.Set("x-codex-secondary-window-minutes", "300")

	updates, err := extractOpenAICodexProbeUpdates(&http.Response{StatusCode: http.StatusTooManyRequests, Header: headers})
	if err != nil {
		t.Fatalf("extractOpenAICodexProbeUpdates() error = %v", err)
	}
	if len(updates) == 0 {
		t.Fatal("expected codex probe updates from 429 headers")
	}
	if got := updates["codex_5h_used_percent"]; got != 100.0 {
		t.Fatalf("codex_5h_used_percent = %v, want 100", got)
	}
	if got := updates["codex_7d_used_percent"]; got != 100.0 {
		t.Fatalf("codex_7d_used_percent = %v, want 100", got)
	}
}

func TestAccountUsageService_PersistOpenAICodexProbeSnapshotOnlyUpdatesExtra(t *testing.T) {
	t.Parallel()

	repo := &accountUsageCodexProbeRepo{
		updateExtraCh: make(chan map[string]any, 1),
		rateLimitCh:   make(chan time.Time, 1),
	}
	svc := &AccountUsageService{accountRepo: repo}
	svc.persistOpenAICodexProbeSnapshot(321, map[string]any{
		"codex_7d_used_percent": 100.0,
		"codex_7d_reset_at":     time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second).Format(time.RFC3339),
	})

	select {
	case updates := <-repo.updateExtraCh:
		if got := updates["codex_7d_used_percent"]; got != 100.0 {
			t.Fatalf("codex_7d_used_percent = %v, want 100", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("等待 codex 探测快照写入 extra 超时")
	}

	select {
	case got := <-repo.rateLimitCh:
		t.Fatalf("不应将探测快照写入运行时限流状态: %v", got)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestAccountUsageService_GetOpenAIUsage_DoesNotPromoteCodexExtraToRateLimit(t *testing.T) {
	t.Parallel()

	resetAt := time.Now().Add(6 * 24 * time.Hour).UTC().Truncate(time.Second)
	repo := &accountUsageCodexProbeRepo{
		rateLimitCh: make(chan time.Time, 1),
	}
	svc := &AccountUsageService{accountRepo: repo}
	account := &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Extra: map[string]any{
			"codex_5h_used_percent": 1.0,
			"codex_5h_reset_at":     time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second).Format(time.RFC3339),
			"codex_7d_used_percent": 100.0,
			"codex_7d_reset_at":     resetAt.Format(time.RFC3339),
		},
	}

	usage, err := svc.getOpenAIUsage(context.Background(), account, false)
	if err != nil {
		t.Fatalf("getOpenAIUsage() error = %v", err)
	}
	if usage.SevenDay == nil || usage.SevenDay.Utilization != 100.0 {
		t.Fatalf("预期 7 天用量仍然可见，实际为 %#v", usage.SevenDay)
	}
	if account.RateLimitResetAt != nil {
		t.Fatalf("不应让已耗尽的 codex extra 改写运行时限流状态: %v", account.RateLimitResetAt)
	}
	select {
	case got := <-repo.rateLimitCh:
		t.Fatalf("不应将已耗尽的 codex extra 持久化为运行时限流状态: %v", got)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestBuildCodexUsageProgressFromExtra_ZerosExpiredWindow(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 3, 16, 12, 0, 0, 0, time.UTC)

	t.Run("expired 5h window zeroes utilization", func(t *testing.T) {
		extra := map[string]any{
			"codex_5h_used_percent": 42.0,
			"codex_5h_reset_at":     "2026-03-16T10:00:00Z", // 2h ago
		}
		progress := buildCodexUsageProgressFromExtra(extra, "5h", now)
		if progress == nil {
			t.Fatal("expected non-nil progress")
		}
		if progress.Utilization != 0 {
			t.Fatalf("expected Utilization=0 for expired window, got %v", progress.Utilization)
		}
		if progress.RemainingSeconds != 0 {
			t.Fatalf("expected RemainingSeconds=0, got %v", progress.RemainingSeconds)
		}
	})

	t.Run("active 5h window keeps utilization", func(t *testing.T) {
		resetAt := now.Add(2 * time.Hour).Format(time.RFC3339)
		extra := map[string]any{
			"codex_5h_used_percent": 42.0,
			"codex_5h_reset_at":     resetAt,
		}
		progress := buildCodexUsageProgressFromExtra(extra, "5h", now)
		if progress == nil {
			t.Fatal("expected non-nil progress")
		}
		if progress.Utilization != 42.0 {
			t.Fatalf("expected Utilization=42, got %v", progress.Utilization)
		}
	})

	t.Run("expired 7d window zeroes utilization", func(t *testing.T) {
		extra := map[string]any{
			"codex_7d_used_percent": 88.0,
			"codex_7d_reset_at":     "2026-03-15T00:00:00Z", // yesterday
		}
		progress := buildCodexUsageProgressFromExtra(extra, "7d", now)
		if progress == nil {
			t.Fatal("expected non-nil progress")
		}
		if progress.Utilization != 0 {
			t.Fatalf("expected Utilization=0 for expired 7d window, got %v", progress.Utilization)
		}
	})
}

type openCodeGoUsageLogRepo struct {
	UsageLogRepository
	stats map[string]*usagestats.AccountStats
}

func (r *openCodeGoUsageLogRepo) GetAccountWindowStats(_ context.Context, _ int64, startTime time.Time) (*usagestats.AccountStats, error) {
	age := time.Since(startTime)
	switch {
	case age < 6*time.Hour:
		return r.stats["5h"], nil
	case age < 8*24*time.Hour:
		return r.stats["7d"], nil
	default:
		return r.stats["30d"], nil
	}
}

func TestAccountUsageService_GetUsage_OpenCodeGoAPIKeyReturnsEstimatedRollingWindows(t *testing.T) {
	t.Parallel()

	repo := &accountUsageCodexProbeRepo{
		stubOpenAIAccountRepo: stubOpenAIAccountRepo{
			accounts: []Account{{
				ID:       77,
				Platform: PlatformOpenCodeGo,
				Type:     AccountTypeAPIKey,
			}},
		},
		updateExtraCh: make(chan map[string]any, 1),
	}
	usageRepo := &openCodeGoUsageLogRepo{
		stats: map[string]*usagestats.AccountStats{
			"5h":  {Requests: 12, Tokens: 1200, Cost: 6, StandardCost: 5, UserCost: 7},
			"7d":  {Requests: 30, Tokens: 3000, Cost: 15, StandardCost: 14, UserCost: 16},
			"30d": {Requests: 60, Tokens: 6000, Cost: 30, StandardCost: 29, UserCost: 31},
		},
	}
	svc := &AccountUsageService{
		accountRepo:  repo,
		usageLogRepo: usageRepo,
		cache:        NewUsageCache(),
	}

	usage, err := svc.GetUsage(context.Background(), 77, true)

	if err != nil {
		t.Fatalf("GetUsage() error = %v", err)
	}
	if usage == nil || usage.FiveHour == nil || usage.SevenDay == nil || usage.ThirtyDay == nil {
		t.Fatalf("expected 5h/7d/30d usage, got %#v", usage)
	}
	if usage.FiveHour.Source != "estimated" || !usage.FiveHour.Estimated {
		t.Fatalf("expected 5h estimated source, got %#v", usage.FiveHour)
	}
	if usage.FiveHour.SourceLabel == "" {
		t.Fatalf("expected source label")
	}
	if usage.FiveHour.WindowStats == nil || usage.FiveHour.WindowStats.Cost != 6 {
		t.Fatalf("expected 5h account-cost window stats, got %#v", usage.FiveHour.WindowStats)
	}
	if got := usage.FiveHour.Utilization; got != 50 {
		t.Fatalf("5h utilization = %v, want 50", got)
	}
	if got := usage.SevenDay.Utilization; got != 50 {
		t.Fatalf("7d utilization = %v, want 50", got)
	}
	if got := usage.ThirtyDay.Utilization; got != 50 {
		t.Fatalf("30d utilization = %v, want 50", got)
	}

	select {
	case updates := <-repo.updateExtraCh:
		if updates["opencode_go_usage_source"] != "estimated" {
			t.Fatalf("expected estimated usage snapshot, got %#v", updates)
		}
		if updates["opencode_go_usage_30d_used_percent"] != 50.0 {
			t.Fatalf("expected 30d snapshot percent, got %#v", updates)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected OpenCode Go usage snapshot to be stored in account extra")
	}
}

func TestAccountUsageService_GetUsage_OpenCodeGoAPIKeyPrefersOfficialConsoleSnapshot(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC().Truncate(time.Second)
	repo := &accountUsageCodexProbeRepo{
		stubOpenAIAccountRepo: stubOpenAIAccountRepo{
			accounts: []Account{{
				ID:       78,
				Platform: PlatformOpenCodeGo,
				Type:     AccountTypeAPIKey,
				Extra: map[string]any{
					"opencode_go_usage_source":                    "official_console",
					"opencode_go_usage_updated_at":                now.Format(time.RFC3339),
					"opencode_go_usage_5h_used_percent":           19.0,
					"opencode_go_usage_5h_reset_in_sec":           5590,
					"opencode_go_usage_5h_resets_at":              now.Add(5590 * time.Second).Format(time.RFC3339),
					"opencode_go_usage_7d_used_percent":           7.0,
					"opencode_go_usage_7d_reset_in_sec":           588490,
					"opencode_go_usage_7d_resets_at":              now.Add(588490 * time.Second).Format(time.RFC3339),
					"opencode_go_usage_30d_used_percent":          10.0,
					"opencode_go_usage_30d_reset_in_sec":          2265176,
					"opencode_go_usage_30d_resets_at":             now.Add(2265176 * time.Second).Format(time.RFC3339),
					"opencode_go_console_auth_status":             "ready",
					"opencode_go_console_auth_checked_at":         now.Format(time.RFC3339),
					"opencode_go_console_auth_imported_at":        now.Format(time.RFC3339),
					"opencode_go_console_auth_expires_at":         now.Add(24 * time.Hour).Format(time.RFC3339),
					"opencode_go_referral_available_count":        1,
					"opencode_go_referral_applied_count":          0,
					"opencode_go_referral_available_amount_cents": 500,
				},
			}},
		},
		updateExtraCh: make(chan map[string]any, 1),
	}
	svc := &AccountUsageService{
		accountRepo:  repo,
		usageLogRepo: &openCodeGoUsageLogRepo{},
		cache:        NewUsageCache(),
	}

	usage, err := svc.GetUsage(context.Background(), 78, true)

	if err != nil {
		t.Fatalf("GetUsage() error = %v", err)
	}
	if usage == nil || usage.FiveHour == nil || usage.SevenDay == nil || usage.ThirtyDay == nil {
		t.Fatalf("expected official 5h/7d/30d usage, got %#v", usage)
	}
	if usage.Source != "official_console" {
		t.Fatalf("usage.Source = %q, want official_console", usage.Source)
	}
	if usage.FiveHour.Source != "official_console" || usage.FiveHour.Estimated {
		t.Fatalf("expected 5h official source, got %#v", usage.FiveHour)
	}
	if usage.FiveHour.Utilization != 19 || usage.SevenDay.Utilization != 7 || usage.ThirtyDay.Utilization != 10 {
		t.Fatalf("unexpected official usage: %#v", usage)
	}
	if usage.FiveHour.ResetsAt == nil || usage.FiveHour.RemainingSeconds <= 0 {
		t.Fatalf("expected official reset metadata, got %#v", usage.FiveHour)
	}

	select {
	case updates := <-repo.updateExtraCh:
		t.Fatalf("official snapshot should not be overwritten by estimated data: %#v", updates)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestAccountUsageService_GetUsage_OpenCodeGoAPIKeyForceRefreshesOfficialConsole(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC().Truncate(time.Second)
	reset5h := now.Add(5 * time.Hour)
	reset7d := now.Add(7 * 24 * time.Hour)
	reset30d := now.Add(30 * 24 * time.Hour)
	repo := &accountUsageCodexProbeRepo{
		stubOpenAIAccountRepo: stubOpenAIAccountRepo{
			accounts: []Account{{
				ID:       80,
				Platform: PlatformOpenCodeGo,
				Type:     AccountTypeAPIKey,
				Credentials: map[string]any{
					"console_cookie":       "auth=console-cookie",
					"console_workspace_id": "wrk_force",
				},
				Extra: map[string]any{
					"opencode_go_usage_source":           "official_console",
					"opencode_go_usage_updated_at":       now.Format(time.RFC3339),
					"opencode_go_usage_5h_used_percent":  1.0,
					"opencode_go_usage_5h_resets_at":     reset5h.Format(time.RFC3339),
					"opencode_go_usage_7d_used_percent":  2.0,
					"opencode_go_usage_7d_resets_at":     reset7d.Format(time.RFC3339),
					"opencode_go_usage_30d_used_percent": 3.0,
					"opencode_go_usage_30d_resets_at":    reset30d.Format(time.RFC3339),
					"opencode_go_console_auth_status":    "ready",
				},
			}},
		},
		updateExtraCh: make(chan map[string]any, 1),
	}
	fetcher := &stubOpenCodeGoConsoleSummaryFetcher{
		summary: buildTestOpenCodeGoConsoleSummary("wrk_force", now, 33, 44, 55),
	}
	svc := &AccountUsageService{
		accountRepo:                   repo,
		usageLogRepo:                  &openCodeGoUsageLogRepo{},
		cache:                         NewUsageCache(),
		openCodeGoConsoleSummaryFetch: fetcher,
	}

	usage, err := svc.GetUsage(context.Background(), 80, true)

	if err != nil {
		t.Fatalf("GetUsage() error = %v", err)
	}
	if fetcher.calls != 1 {
		t.Fatalf("FetchSummary calls = %d, want 1", fetcher.calls)
	}
	if fetcher.workspaceID != "wrk_force" || fetcher.cookie != "auth=console-cookie" {
		t.Fatalf("FetchSummary credentials = (%q, %q)", fetcher.workspaceID, fetcher.cookie)
	}
	if usage.FiveHour.Utilization != 33 || usage.SevenDay.Utilization != 44 || usage.ThirtyDay.Utilization != 55 {
		t.Fatalf("expected forced official refresh usage, got %#v", usage)
	}

	select {
	case updates := <-repo.updateExtraCh:
		if updates["opencode_go_usage_source"] != "official_console" {
			t.Fatalf("expected official snapshot update, got %#v", updates)
		}
		if updates["opencode_go_usage_5h_used_percent"] != 33.0 {
			t.Fatalf("expected refreshed 5h percent, got %#v", updates)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected refreshed official snapshot to be stored")
	}
}

func TestAccountUsageService_GetUsage_OpenCodeGoAPIKeyRefreshesStaleOfficialConsoleSnapshot(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC().Truncate(time.Second)
	repo := &accountUsageCodexProbeRepo{
		stubOpenAIAccountRepo: stubOpenAIAccountRepo{
			accounts: []Account{{
				ID:       81,
				Platform: PlatformOpenCodeGo,
				Type:     AccountTypeAPIKey,
				Credentials: map[string]any{
					"console_cookie":       "auth=stale-cookie",
					"console_workspace_id": "wrk_stale",
				},
				Extra: buildTestOpenCodeGoOfficialExtra(now.Add(-10*time.Minute), 4, 5, 6),
			}},
		},
		updateExtraCh: make(chan map[string]any, 1),
	}
	fetcher := &stubOpenCodeGoConsoleSummaryFetcher{
		summary: buildTestOpenCodeGoConsoleSummary("wrk_stale", now, 14, 15, 16),
	}
	svc := &AccountUsageService{
		accountRepo:                   repo,
		usageLogRepo:                  &openCodeGoUsageLogRepo{},
		cache:                         NewUsageCache(),
		openCodeGoConsoleSummaryFetch: fetcher,
	}

	usage, err := svc.GetUsage(context.Background(), 81, false)

	if err != nil {
		t.Fatalf("GetUsage() error = %v", err)
	}
	if fetcher.calls != 1 {
		t.Fatalf("FetchSummary calls = %d, want 1", fetcher.calls)
	}
	if usage.FiveHour.Utilization != 14 || usage.SevenDay.Utilization != 15 || usage.ThirtyDay.Utilization != 16 {
		t.Fatalf("expected stale snapshot refresh, got %#v", usage)
	}
}

func TestAccountUsageService_GetUsage_OpenCodeGoAPIKeyRefreshesAuthorizedAccountWithoutSnapshot(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC().Truncate(time.Second)
	repo := &accountUsageCodexProbeRepo{
		stubOpenAIAccountRepo: stubOpenAIAccountRepo{
			accounts: []Account{{
				ID:       83,
				Platform: PlatformOpenCodeGo,
				Type:     AccountTypeAPIKey,
				Credentials: map[string]any{
					"console_cookie":       "auth=no-snapshot-cookie",
					"console_workspace_id": "wrk_no_snapshot",
				},
				Extra: map[string]any{},
			}},
		},
		updateExtraCh: make(chan map[string]any, 1),
	}
	fetcher := &stubOpenCodeGoConsoleSummaryFetcher{
		summary: buildTestOpenCodeGoConsoleSummary("wrk_no_snapshot", now, 64, 65, 66),
	}
	svc := &AccountUsageService{
		accountRepo:                   repo,
		usageLogRepo:                  &openCodeGoUsageLogRepo{},
		cache:                         NewUsageCache(),
		openCodeGoConsoleSummaryFetch: fetcher,
	}

	usage, err := svc.GetUsage(context.Background(), 83, false)

	if err != nil {
		t.Fatalf("GetUsage() error = %v", err)
	}
	if fetcher.calls != 1 {
		t.Fatalf("FetchSummary calls = %d, want 1", fetcher.calls)
	}
	if usage.FiveHour.Utilization != 64 || usage.SevenDay.Utilization != 65 || usage.ThirtyDay.Utilization != 66 {
		t.Fatalf("expected missing snapshot refresh, got %#v", usage)
	}
}

func TestAccountUsageService_GetUsage_OpenCodeGoAPIKeyKeepsFreshOfficialConsoleSnapshot(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC().Truncate(time.Second)
	repo := &accountUsageCodexProbeRepo{
		stubOpenAIAccountRepo: stubOpenAIAccountRepo{
			accounts: []Account{{
				ID:       82,
				Platform: PlatformOpenCodeGo,
				Type:     AccountTypeAPIKey,
				Credentials: map[string]any{
					"console_cookie":       "auth=fresh-cookie",
					"console_workspace_id": "wrk_fresh",
				},
				Extra: buildTestOpenCodeGoOfficialExtra(now, 24, 25, 26),
			}},
		},
		updateExtraCh: make(chan map[string]any, 1),
	}
	fetcher := &stubOpenCodeGoConsoleSummaryFetcher{
		summary: buildTestOpenCodeGoConsoleSummary("wrk_fresh", now, 34, 35, 36),
	}
	svc := &AccountUsageService{
		accountRepo:                   repo,
		usageLogRepo:                  &openCodeGoUsageLogRepo{},
		cache:                         NewUsageCache(),
		openCodeGoConsoleSummaryFetch: fetcher,
	}

	usage, err := svc.GetUsage(context.Background(), 82, false)

	if err != nil {
		t.Fatalf("GetUsage() error = %v", err)
	}
	if fetcher.calls != 0 {
		t.Fatalf("FetchSummary calls = %d, want 0", fetcher.calls)
	}
	if usage.FiveHour.Utilization != 24 || usage.SevenDay.Utilization != 25 || usage.ThirtyDay.Utilization != 26 {
		t.Fatalf("expected fresh snapshot usage, got %#v", usage)
	}
}

func buildTestOpenCodeGoOfficialExtra(updatedAt time.Time, p5h, p7d, p30d float64) map[string]any {
	return map[string]any{
		"opencode_go_usage_source":           "official_console",
		"opencode_go_usage_updated_at":       updatedAt.Format(time.RFC3339),
		"opencode_go_usage_5h_used_percent":  p5h,
		"opencode_go_usage_5h_resets_at":     updatedAt.Add(5 * time.Hour).Format(time.RFC3339),
		"opencode_go_usage_7d_used_percent":  p7d,
		"opencode_go_usage_7d_resets_at":     updatedAt.Add(7 * 24 * time.Hour).Format(time.RFC3339),
		"opencode_go_usage_30d_used_percent": p30d,
		"opencode_go_usage_30d_resets_at":    updatedAt.Add(30 * 24 * time.Hour).Format(time.RFC3339),
		"opencode_go_console_auth_status":    "ready",
	}
}

func buildTestOpenCodeGoConsoleSummary(workspaceID string, fetchedAt time.Time, p5h, p7d, p30d float64) *OpenCodeGoConsoleSummary {
	reset5h := fetchedAt.Add(5 * time.Hour)
	reset7d := fetchedAt.Add(7 * 24 * time.Hour)
	reset30d := fetchedAt.Add(30 * 24 * time.Hour)
	return &OpenCodeGoConsoleSummary{
		WorkspaceID: workspaceID,
		FetchedAt:   fetchedAt,
		Usage: OpenCodeGoConsoleUsage{
			FiveHour:  OpenCodeGoUsageWindow{Status: "ok", UsagePercent: p5h, ResetInSec: int(reset5h.Sub(fetchedAt).Seconds()), ResetsAt: &reset5h},
			SevenDay:  OpenCodeGoUsageWindow{Status: "ok", UsagePercent: p7d, ResetInSec: int(reset7d.Sub(fetchedAt).Seconds()), ResetsAt: &reset7d},
			ThirtyDay: OpenCodeGoUsageWindow{Status: "ok", UsagePercent: p30d, ResetInSec: int(reset30d.Sub(fetchedAt).Seconds()), ResetsAt: &reset30d},
		},
		Referral: OpenCodeGoReferralSummary{},
	}
}

func TestAccountUsageService_GetUsage_OpenCodeGoAPIKeyFallsBackWhenOfficialSnapshotExpired(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC().Truncate(time.Second)
	repo := &accountUsageCodexProbeRepo{
		stubOpenAIAccountRepo: stubOpenAIAccountRepo{
			accounts: []Account{{
				ID:       79,
				Platform: PlatformOpenCodeGo,
				Type:     AccountTypeAPIKey,
				Extra: map[string]any{
					"opencode_go_usage_source":           "official_console",
					"opencode_go_usage_updated_at":       now.Add(-2 * time.Hour).Format(time.RFC3339),
					"opencode_go_usage_5h_used_percent":  99.0,
					"opencode_go_usage_5h_resets_at":     now.Add(-1 * time.Minute).Format(time.RFC3339),
					"opencode_go_usage_7d_used_percent":  99.0,
					"opencode_go_usage_7d_resets_at":     now.Add(-1 * time.Minute).Format(time.RFC3339),
					"opencode_go_usage_30d_used_percent": 99.0,
					"opencode_go_usage_30d_resets_at":    now.Add(-1 * time.Minute).Format(time.RFC3339),
				},
			}},
		},
		updateExtraCh: make(chan map[string]any, 1),
	}
	svc := &AccountUsageService{
		accountRepo: repo,
		usageLogRepo: &openCodeGoUsageLogRepo{
			stats: map[string]*usagestats.AccountStats{
				"5h":  {Cost: 6},
				"7d":  {Cost: 15},
				"30d": {Cost: 30},
			},
		},
		cache: NewUsageCache(),
	}

	usage, err := svc.GetUsage(context.Background(), 79, true)

	if err != nil {
		t.Fatalf("GetUsage() error = %v", err)
	}
	if usage.FiveHour.Source != "estimated" || !usage.FiveHour.Estimated {
		t.Fatalf("expected estimated fallback, got %#v", usage.FiveHour)
	}
	if got := usage.FiveHour.Utilization; got != 50 {
		t.Fatalf("fallback 5h utilization = %v, want 50", got)
	}
}

func TestAccountUsageService_GetUsage_OpenCodeGoAutoAppliesRewardWhenUsageExceeds80Percent(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC().Truncate(time.Second)
	reset5h := now.Add(5 * time.Hour)
	reset7d := now.Add(7 * 24 * time.Hour)
	reset30d := now.Add(30 * 24 * time.Hour)

	repo := &accountUsageCodexProbeRepo{
		stubOpenAIAccountRepo: stubOpenAIAccountRepo{
			accounts: []Account{{
				ID:       85,
				Platform: PlatformOpenCodeGo,
				Type:     AccountTypeAPIKey,
				Credentials: map[string]any{
					"console_cookie":       "auth=console-cookie",
					"console_workspace_id": "wrk_auto",
				},
				Extra: map[string]any{
					"opencode_go_usage_source":           "official_console",
					"opencode_go_usage_updated_at":       now.Add(-10 * time.Minute).Format(time.RFC3339),
					"opencode_go_usage_5h_used_percent":  85.0,
					"opencode_go_usage_5h_resets_at":     reset5h.Format(time.RFC3339),
					"opencode_go_usage_7d_used_percent":  10.0,
					"opencode_go_usage_7d_resets_at":     reset7d.Format(time.RFC3339),
					"opencode_go_usage_30d_used_percent": 10.0,
					"opencode_go_usage_30d_resets_at":    reset30d.Format(time.RFC3339),
					"opencode_go_console_auth_status":    "ready",
				},
			}},
		},
		updateExtraCh: make(chan map[string]any, 2),
	}

	initialSummary := &OpenCodeGoConsoleSummary{
		WorkspaceID: "wrk_auto",
		FetchedAt:   now,
		Usage: OpenCodeGoConsoleUsage{
			FiveHour:  OpenCodeGoUsageWindow{Status: "ok", UsagePercent: 85.0, ResetInSec: int(reset5h.Sub(now).Seconds()), ResetsAt: &reset5h},
			SevenDay:  OpenCodeGoUsageWindow{Status: "ok", UsagePercent: 10.0, ResetInSec: int(reset7d.Sub(now).Seconds()), ResetsAt: &reset7d},
			ThirtyDay: OpenCodeGoUsageWindow{Status: "ok", UsagePercent: 10.0, ResetInSec: int(reset30d.Sub(now).Seconds()), ResetsAt: &reset30d},
		},
		Referral: OpenCodeGoReferralSummary{
			AvailableCount: 1,
			Rewards: []OpenCodeGoReferralReward{
				{ID: "rew_auto_1", Status: "available", AmountCents: 500},
			},
		},
	}

	afterApplySummary := &OpenCodeGoConsoleSummary{
		WorkspaceID: "wrk_auto",
		FetchedAt:   now.Add(time.Second),
		Usage: OpenCodeGoConsoleUsage{
			FiveHour:  OpenCodeGoUsageWindow{Status: "ok", UsagePercent: 42.0, ResetInSec: int(reset5h.Sub(now).Seconds()), ResetsAt: &reset5h},
			SevenDay:  OpenCodeGoUsageWindow{Status: "ok", UsagePercent: 10.0, ResetInSec: int(reset7d.Sub(now).Seconds()), ResetsAt: &reset7d},
			ThirtyDay: OpenCodeGoUsageWindow{Status: "ok", UsagePercent: 10.0, ResetInSec: int(reset30d.Sub(now).Seconds()), ResetsAt: &reset30d},
		},
		Referral: OpenCodeGoReferralSummary{
			AvailableCount: 0,
			Rewards: []OpenCodeGoReferralReward{
				{ID: "rew_auto_1", Status: "applied", AmountCents: 500},
			},
		},
	}

	applier := &stubOpenCodeGoReferralApplier{}
	fetcher := &sequentialSummaryFetcher{summaries: []*OpenCodeGoConsoleSummary{initialSummary, afterApplySummary}}

	svc := &AccountUsageService{
		accountRepo:                     repo,
		usageLogRepo:                    &openCodeGoUsageLogRepo{},
		cache:                           NewUsageCache(),
		openCodeGoConsoleSummaryFetch:   fetcher,
		openCodeGoReferralActionApplier: applier,
	}

	usage, err := svc.GetUsage(context.Background(), 85, true)
	if err != nil {
		t.Fatalf("GetUsage() error = %v", err)
	}
	if len(applier.applyCalls) != 1 || applier.applyCalls[0] != "rew_auto_1" {
		t.Fatalf("expected apply call [rew_auto_1], got: %v", applier.applyCalls)
	}
	if usage.FiveHour.Utilization != 42.0 {
		t.Fatalf("expected updated utilization 42.0, got: %v", usage.FiveHour.Utilization)
	}
}

func TestAccountUsageService_GetUsage_OpenCodeGoClearsRateLimitWhenUsageRecovers(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC().Truncate(time.Second)
	rateLimitedAt := now.Add(-10 * time.Minute)
	rateLimitResetAt := now.Add(2 * time.Hour)
	reset5h := now.Add(5 * time.Hour)

	repo := &accountUsageCodexProbeRepo{
		stubOpenAIAccountRepo: stubOpenAIAccountRepo{
			accounts: []Account{{
				ID:               86,
				Platform:         PlatformOpenCodeGo,
				Type:             AccountTypeAPIKey,
				RateLimitedAt:    &rateLimitedAt,
				RateLimitResetAt: &rateLimitResetAt,
				Credentials: map[string]any{
					"console_cookie":       "auth=console-cookie",
					"console_workspace_id": "wrk_recovered",
				},
				Extra: map[string]any{
					"opencode_go_usage_source":           "official_console",
					"opencode_go_usage_updated_at":       now.Add(-10 * time.Minute).Format(time.RFC3339),
					"opencode_go_usage_5h_used_percent":  100.0,
					"opencode_go_usage_5h_resets_at":     reset5h.Format(time.RFC3339),
					"opencode_go_usage_7d_used_percent":  10.0,
					"opencode_go_usage_7d_resets_at":     now.Add(7 * 24 * time.Hour).Format(time.RFC3339),
					"opencode_go_usage_30d_used_percent": 10.0,
					"opencode_go_usage_30d_resets_at":    now.Add(30 * 24 * time.Hour).Format(time.RFC3339),
					"opencode_go_console_auth_status":    "ready",
				},
			}},
		},
		updateExtraCh: make(chan map[string]any, 2),
	}

	// 模拟使用奖励后用量恢复至 50% (< 100%)
	recoveredSummary := &OpenCodeGoConsoleSummary{
		WorkspaceID: "wrk_recovered",
		FetchedAt:   now,
		Usage: OpenCodeGoConsoleUsage{
			FiveHour:  OpenCodeGoUsageWindow{Status: "ok", UsagePercent: 50.0, ResetInSec: int(reset5h.Sub(now).Seconds()), ResetsAt: &reset5h},
			SevenDay:  OpenCodeGoUsageWindow{Status: "ok", UsagePercent: 10.0, ResetInSec: int(7 * 24 * 3600), ResetsAt: &reset5h},
			ThirtyDay: OpenCodeGoUsageWindow{Status: "ok", UsagePercent: 10.0, ResetInSec: int(30 * 24 * 3600), ResetsAt: &reset5h},
		},
		Referral: OpenCodeGoReferralSummary{
			AvailableCount: 0,
		},
	}

	fetcher := &sequentialSummaryFetcher{summaries: []*OpenCodeGoConsoleSummary{recoveredSummary}}
	svc := &AccountUsageService{
		accountRepo:                   repo,
		usageLogRepo:                  &openCodeGoUsageLogRepo{},
		cache:                         NewUsageCache(),
		openCodeGoConsoleSummaryFetch: fetcher,
	}

	usage, err := svc.GetUsage(context.Background(), 86, true)
	if err != nil {
		t.Fatalf("GetUsage() error = %v", err)
	}
	if usage.FiveHour.Utilization != 50.0 {
		t.Fatalf("expected utilization 50.0, got: %v", usage.FiveHour.Utilization)
	}
	if repo.accounts[0].RateLimitedAt != nil || repo.accounts[0].RateLimitResetAt != nil {
		t.Fatalf("expected rate limit cleared on account when usage recovered")
	}
}

func TestAccountUsageService_GetUsage_OpenAIClearsRateLimitWhenUsageRecovers(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC().Truncate(time.Second)
	rateLimitedAt := now.Add(-10 * time.Minute)
	rateLimitResetAt := now.Add(2 * time.Hour)

	repo := &accountUsageCodexProbeRepo{
		stubOpenAIAccountRepo: stubOpenAIAccountRepo{
			accounts: []Account{{
				ID:               87,
				Platform:         PlatformOpenAI,
				Type:             AccountTypeOAuth,
				RateLimitedAt:    &rateLimitedAt,
				RateLimitResetAt: &rateLimitResetAt,
				Credentials: map[string]any{
					"chatgpt_account_id": "org-openai-rec",
				},
				Extra: map[string]any{
					"codex_5h_used_percent": 100.0,
				},
			}},
		},
		updateExtraCh: make(chan map[string]any, 2),
	}

	// 模拟 QueryUsage 返回 5h: 0%, 7d: 10%
	mockQuotaSvc := &stubOpenAIQuotaUsageFetcher{
		usage: &OpenAIQuotaUsage{
			AdditionalRateLimits: []OpenAIAdditionalRateLimit{
				{
					MeteredFeature: "codex_bengalfox",
					RateLimit: &OpenAIRateLimit{
						PrimaryWindow: &OpenAIRateLimitWindow{UsedPercent: 0.0, ResetAfterSeconds: 3600, LimitWindowSeconds: 300},
					},
				},
			},
		},
	}

	tokenCache := &stubQuotaTokenCache{tokens: map[string]string{
		OpenAITokenCacheKey(&repo.accounts[0]): "fake-token-rec",
	}}
	tokenProvider := NewOpenAITokenProvider(repo, tokenCache, nil)

	svc := &AccountUsageService{
		accountRepo:        repo,
		usageLogRepo:       &openCodeGoUsageLogRepo{},
		cache:              NewUsageCache(),
		openAIQuotaService: mockQuotaSvc.asService(repo, tokenProvider),
	}

	// 影子账号通过 openAIQuotaService 查询用量
	pid := int64(87)
	shadowAccount := Account{
		ID:               88,
		ParentAccountID:  &pid,
		Platform:         PlatformOpenAI,
		Type:             AccountTypeOAuth,
		QuotaDimension:   QuotaDimensionSpark,
		RateLimitedAt:    &rateLimitedAt,
		RateLimitResetAt: &rateLimitResetAt,
		Credentials:      map[string]any{},
		Extra: map[string]any{
			"codex_5h_used_percent": 100.0,
		},
	}
	repo.accounts = append(repo.accounts, shadowAccount)

	usage, err := svc.GetUsage(context.Background(), 88, true)
	if err != nil {
		t.Fatalf("GetUsage() error = %v", err)
	}
	_ = usage

	if repo.accounts[1].RateLimitedAt != nil || repo.accounts[1].RateLimitResetAt != nil {
		t.Fatalf("expected rate limit cleared on shadow account when codex usage recovered")
	}
}

type stubOpenAIQuotaUsageFetcher struct {
	usage *OpenAIQuotaUsage
}

func (f *stubOpenAIQuotaUsageFetcher) asService(repo AccountRepository, tokenProvider *OpenAITokenProvider) *OpenAIQuotaService {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		_ = json.NewEncoder(w).Encode(f.usage)
	}))
	return NewOpenAIQuotaService(repo, nil, tokenProvider, newQuotaRedirectingFactory(srv))
}

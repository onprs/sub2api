package service

import (
	"context"
	"testing"
	"time"
)

func TestBuildOpenCodeGoConsoleSummaryExtraStoresUsageAndReferralStats(t *testing.T) {
	t.Parallel()

	fetchedAt := time.Date(2026, 6, 22, 4, 50, 0, 0, time.UTC)
	summary, err := ParseOpenCodeGoConsolePage(opencodeGoConsoleHTMLFixture, fetchedAt)
	if err != nil {
		t.Fatalf("ParseOpenCodeGoConsolePage() error = %v", err)
	}

	updates := BuildOpenCodeGoConsoleSummaryExtra(summary, OpenCodeGoConsoleAuthStatusReady)

	if updates["opencode_go_usage_source"] != "official_console" {
		t.Fatalf("usage source = %#v", updates["opencode_go_usage_source"])
	}
	if updates["opencode_go_usage_5h_used_percent"] != 19.0 {
		t.Fatalf("5h percent = %#v", updates["opencode_go_usage_5h_used_percent"])
	}
	if updates["opencode_go_usage_5h_reset_in_sec"] != 5590 {
		t.Fatalf("5h reset seconds = %#v", updates["opencode_go_usage_5h_reset_in_sec"])
	}
	if updates["opencode_go_usage_7d_used_percent"] != 7.0 || updates["opencode_go_usage_30d_used_percent"] != 10.0 {
		t.Fatalf("unexpected usage updates: %#v", updates)
	}
	if updates["opencode_go_referral_code"] != "M5N4TCC0GA" {
		t.Fatalf("referral code = %#v", updates["opencode_go_referral_code"])
	}
	if updates["opencode_go_referral_available_count"] != 1 {
		t.Fatalf("available count = %#v", updates["opencode_go_referral_available_count"])
	}
	if updates["opencode_go_referral_available_amount_cents"] != 500 {
		t.Fatalf("available amount = %#v", updates["opencode_go_referral_available_amount_cents"])
	}
	for k, v := range updates {
		if v == "ljb061121@gmail.com" {
			t.Fatalf("plain reward email leaked in extra key %s", k)
		}
	}
}

func TestAccountUsageServicePersistOpenCodeGoConsoleSummaryWritesExtra(t *testing.T) {
	t.Parallel()

	repo := &accountUsageCodexProbeRepo{updateExtraCh: make(chan map[string]any, 1)}
	svc := &AccountUsageService{accountRepo: repo}
	summary, err := ParseOpenCodeGoConsolePage(opencodeGoConsoleHTMLFixture, time.Date(2026, 6, 22, 4, 50, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("ParseOpenCodeGoConsolePage() error = %v", err)
	}

	if err := svc.PersistOpenCodeGoConsoleSummary(context.Background(), 99, summary, OpenCodeGoConsoleAuthStatusReady); err != nil {
		t.Fatalf("PersistOpenCodeGoConsoleSummary() error = %v", err)
	}

	select {
	case updates := <-repo.updateExtraCh:
		if updates["opencode_go_usage_source"] != "official_console" {
			t.Fatalf("usage source = %#v", updates["opencode_go_usage_source"])
		}
		if updates["opencode_go_console_auth_status"] != "ready" {
			t.Fatalf("auth status = %#v", updates["opencode_go_console_auth_status"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected console summary extra update")
	}
}

func TestAccountUsageServicePersistOpenCodeGoConsoleSummaryMarksExhaustedAccountRateLimited(t *testing.T) {
	t.Parallel()

	repo := &accountUsageCodexProbeRepo{
		updateExtraCh: make(chan map[string]any, 1),
		rateLimitCh:   make(chan time.Time, 1),
	}
	svc := &AccountUsageService{accountRepo: repo}
	fetchedAt := time.Date(2026, 6, 22, 4, 50, 0, 0, time.UTC)
	reset5h := fetchedAt.Add(5 * time.Hour)
	reset7d := fetchedAt.Add(7 * 24 * time.Hour)
	reset30d := fetchedAt.Add(30 * 24 * time.Hour)
	summary := &OpenCodeGoConsoleSummary{
		WorkspaceID: "ws-1",
		FetchedAt:   fetchedAt,
		Usage: OpenCodeGoConsoleUsage{
			FiveHour:  OpenCodeGoUsageWindow{UsagePercent: 12, ResetInSec: int(reset5h.Sub(fetchedAt).Seconds()), ResetsAt: &reset5h},
			SevenDay:  OpenCodeGoUsageWindow{UsagePercent: 100, ResetInSec: int(reset7d.Sub(fetchedAt).Seconds()), ResetsAt: &reset7d},
			ThirtyDay: OpenCodeGoUsageWindow{UsagePercent: 40, ResetInSec: int(reset30d.Sub(fetchedAt).Seconds()), ResetsAt: &reset30d},
		},
	}

	if err := svc.PersistOpenCodeGoConsoleSummary(context.Background(), 99, summary, OpenCodeGoConsoleAuthStatusReady); err != nil {
		t.Fatalf("PersistOpenCodeGoConsoleSummary() error = %v", err)
	}

	select {
	case got := <-repo.rateLimitCh:
		if !got.Equal(reset7d) {
			t.Fatalf("rate limit reset = %s, want %s", got, reset7d)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected exhausted OpenCode Go console snapshot to mark account rate-limited")
	}
}

type stubOpenCodeGoReferralApplier struct {
	applyCalls []string
	applyErr   error
}

func (s *stubOpenCodeGoReferralApplier) Apply(_ context.Context, _, referralID, _ string) error {
	s.applyCalls = append(s.applyCalls, referralID)
	return s.applyErr
}

type sequentialSummaryFetcher struct {
	summaries []*OpenCodeGoConsoleSummary
	callCount int
}

func (f *sequentialSummaryFetcher) FetchSummary(_ context.Context, _, _ string) (*OpenCodeGoConsoleSummary, error) {
	if f.callCount < len(f.summaries) {
		res := f.summaries[f.callCount]
		f.callCount++
		return res, nil
	}
	return f.summaries[len(f.summaries)-1], nil
}

func TestIsOpenCodeGoUsageThresholdReached(t *testing.T) {
	t.Parallel()

	// 全部低于 80%
	usage1 := OpenCodeGoConsoleUsage{
		FiveHour:  OpenCodeGoUsageWindow{UsagePercent: 79.9},
		SevenDay:  OpenCodeGoUsageWindow{UsagePercent: 50.0},
		ThirtyDay: OpenCodeGoUsageWindow{UsagePercent: 30.0},
	}
	if IsOpenCodeGoUsageThresholdReached(usage1, 80.0) {
		t.Fatalf("expected false for usage < 80%%")
	}

	// 5h 达到 80%
	usage2 := OpenCodeGoConsoleUsage{
		FiveHour:  OpenCodeGoUsageWindow{UsagePercent: 80.0},
		SevenDay:  OpenCodeGoUsageWindow{UsagePercent: 10.0},
		ThirtyDay: OpenCodeGoUsageWindow{UsagePercent: 10.0},
	}
	if !IsOpenCodeGoUsageThresholdReached(usage2, 80.0) {
		t.Fatalf("expected true when 5h >= 80%%")
	}

	// 7d 超过 80%
	usage3 := OpenCodeGoConsoleUsage{
		FiveHour:  OpenCodeGoUsageWindow{UsagePercent: 10.0},
		SevenDay:  OpenCodeGoUsageWindow{UsagePercent: 85.5},
		ThirtyDay: OpenCodeGoUsageWindow{UsagePercent: 10.0},
	}
	if !IsOpenCodeGoUsageThresholdReached(usage3, 80.0) {
		t.Fatalf("expected true when 7d >= 80%%")
	}

	// 30d 达到 90%
	usage4 := OpenCodeGoConsoleUsage{
		FiveHour:  OpenCodeGoUsageWindow{UsagePercent: 10.0},
		SevenDay:  OpenCodeGoUsageWindow{UsagePercent: 10.0},
		ThirtyDay: OpenCodeGoUsageWindow{UsagePercent: 90.0},
	}
	if !IsOpenCodeGoUsageThresholdReached(usage4, 80.0) {
		t.Fatalf("expected true when 30d >= 80%%")
	}
}

func TestHasAvailableOpenCodeGoReferralReward(t *testing.T) {
	t.Parallel()

	// AvailableCount > 0
	if !HasAvailableOpenCodeGoReferralReward(OpenCodeGoReferralSummary{AvailableCount: 1}) {
		t.Fatalf("expected true for available_count > 0")
	}

	// Rewards 中含 available
	ref := OpenCodeGoReferralSummary{
		Rewards: []OpenCodeGoReferralReward{
			{ID: "r1", Status: "applied"},
			{ID: "r2", Status: "available"},
		},
	}
	if !HasAvailableOpenCodeGoReferralReward(ref) {
		t.Fatalf("expected true when rewards contain available reward")
	}

	// 全部已使用
	refAllApplied := OpenCodeGoReferralSummary{
		Rewards: []OpenCodeGoReferralReward{
			{ID: "r1", Status: "applied"},
			{ID: "r2", Status: "applied"},
		},
	}
	if HasAvailableOpenCodeGoReferralReward(refAllApplied) {
		t.Fatalf("expected false when all rewards applied")
	}
}

func TestAutoApplyOpenCodeGoReferralRewards_TriggeredAbove80Percent(t *testing.T) {
	t.Parallel()

	applier := &stubOpenCodeGoReferralApplier{}
	now := time.Now().UTC()

	initialSummary := &OpenCodeGoConsoleSummary{
		WorkspaceID: "wrk_test_123",
		FetchedAt:   now,
		Usage: OpenCodeGoConsoleUsage{
			FiveHour:  OpenCodeGoUsageWindow{UsagePercent: 82.0},
			SevenDay:  OpenCodeGoUsageWindow{UsagePercent: 40.0},
			ThirtyDay: OpenCodeGoUsageWindow{UsagePercent: 30.0},
		},
		Referral: OpenCodeGoReferralSummary{
			AvailableCount: 1,
			Rewards: []OpenCodeGoReferralReward{
				{ID: "rew_1", Status: "available", AmountCents: 500},
			},
		},
	}

	afterApplySummary := &OpenCodeGoConsoleSummary{
		WorkspaceID: "wrk_test_123",
		FetchedAt:   now.Add(time.Second),
		Usage: OpenCodeGoConsoleUsage{
			FiveHour:  OpenCodeGoUsageWindow{UsagePercent: 41.0},
			SevenDay:  OpenCodeGoUsageWindow{UsagePercent: 20.0},
			ThirtyDay: OpenCodeGoUsageWindow{UsagePercent: 15.0},
		},
		Referral: OpenCodeGoReferralSummary{
			AvailableCount: 0,
			Rewards: []OpenCodeGoReferralReward{
				{ID: "rew_1", Status: "applied", AmountCents: 500},
			},
		},
	}

	fetcher := &sequentialSummaryFetcher{summaries: []*OpenCodeGoConsoleSummary{afterApplySummary}}
	svc := &AccountUsageService{
		openCodeGoConsoleSummaryFetch:   fetcher,
		openCodeGoReferralActionApplier: applier,
	}

	finalSummary, applied := svc.AutoApplyOpenCodeGoReferralRewardsIfEligible(
		context.Background(),
		42,
		"wrk_test_123",
		"cookie_123",
		initialSummary,
	)

	if !applied {
		t.Fatalf("expected applied to be true")
	}
	if len(applier.applyCalls) != 1 || applier.applyCalls[0] != "rew_1" {
		t.Fatalf("expected apply call with rew_1, got: %v", applier.applyCalls)
	}
	if finalSummary.Usage.FiveHour.UsagePercent != 41.0 {
		t.Fatalf("final 5h percent = %v, want 41.0", finalSummary.Usage.FiveHour.UsagePercent)
	}
}

func TestAutoApplyOpenCodeGoReferralRewards_NotTriggeredBelow80Percent(t *testing.T) {
	t.Parallel()

	applier := &stubOpenCodeGoReferralApplier{}
	now := time.Now().UTC()

	initialSummary := &OpenCodeGoConsoleSummary{
		WorkspaceID: "wrk_test_123",
		FetchedAt:   now,
		Usage: OpenCodeGoConsoleUsage{
			FiveHour:  OpenCodeGoUsageWindow{UsagePercent: 79.5},
			SevenDay:  OpenCodeGoUsageWindow{UsagePercent: 70.0},
			ThirtyDay: OpenCodeGoUsageWindow{UsagePercent: 60.0},
		},
		Referral: OpenCodeGoReferralSummary{
			AvailableCount: 2,
			Rewards: []OpenCodeGoReferralReward{
				{ID: "rew_1", Status: "available", AmountCents: 500},
			},
		},
	}

	svc := &AccountUsageService{
		openCodeGoReferralActionApplier: applier,
	}

	finalSummary, applied := svc.AutoApplyOpenCodeGoReferralRewardsIfEligible(
		context.Background(),
		42,
		"wrk_test_123",
		"cookie_123",
		initialSummary,
	)

	if applied {
		t.Fatalf("expected applied to be false when usage < 80%%")
	}
	if len(applier.applyCalls) != 0 {
		t.Fatalf("expected no apply calls, got: %v", applier.applyCalls)
	}
	if finalSummary != initialSummary {
		t.Fatalf("expected unchanged summary")
	}
}

func TestAutoApplyOpenCodeGoReferralRewards_MultiRewardLoopUntilBelow80(t *testing.T) {
	t.Parallel()

	applier := &stubOpenCodeGoReferralApplier{}
	now := time.Now().UTC()

	initialSummary := &OpenCodeGoConsoleSummary{
		WorkspaceID: "wrk_test_123",
		FetchedAt:   now,
		Usage: OpenCodeGoConsoleUsage{
			FiveHour:  OpenCodeGoUsageWindow{UsagePercent: 95.0},
			SevenDay:  OpenCodeGoUsageWindow{UsagePercent: 90.0},
			ThirtyDay: OpenCodeGoUsageWindow{UsagePercent: 85.0},
		},
		Referral: OpenCodeGoReferralSummary{
			AvailableCount: 2,
			Rewards: []OpenCodeGoReferralReward{
				{ID: "rew_1", Status: "available", AmountCents: 500},
				{ID: "rew_2", Status: "available", AmountCents: 500},
			},
		},
	}

	// 第 1 次应用后仍然 > 80%
	afterFirstApply := &OpenCodeGoConsoleSummary{
		WorkspaceID: "wrk_test_123",
		FetchedAt:   now.Add(time.Second),
		Usage: OpenCodeGoConsoleUsage{
			FiveHour:  OpenCodeGoUsageWindow{UsagePercent: 83.0},
			SevenDay:  OpenCodeGoUsageWindow{UsagePercent: 75.0},
			ThirtyDay: OpenCodeGoUsageWindow{UsagePercent: 70.0},
		},
		Referral: OpenCodeGoReferralSummary{
			AvailableCount: 1,
			Rewards: []OpenCodeGoReferralReward{
				{ID: "rew_1", Status: "applied", AmountCents: 500},
				{ID: "rew_2", Status: "available", AmountCents: 500},
			},
		},
	}

	// 第 2 次应用后降至 45% (< 80%)
	afterSecondApply := &OpenCodeGoConsoleSummary{
		WorkspaceID: "wrk_test_123",
		FetchedAt:   now.Add(2 * time.Second),
		Usage: OpenCodeGoConsoleUsage{
			FiveHour:  OpenCodeGoUsageWindow{UsagePercent: 45.0},
			SevenDay:  OpenCodeGoUsageWindow{UsagePercent: 40.0},
			ThirtyDay: OpenCodeGoUsageWindow{UsagePercent: 35.0},
		},
		Referral: OpenCodeGoReferralSummary{
			AvailableCount: 0,
			Rewards: []OpenCodeGoReferralReward{
				{ID: "rew_1", Status: "applied", AmountCents: 500},
				{ID: "rew_2", Status: "applied", AmountCents: 500},
			},
		},
	}

	fetcher := &sequentialSummaryFetcher{summaries: []*OpenCodeGoConsoleSummary{afterFirstApply, afterSecondApply}}
	svc := &AccountUsageService{
		openCodeGoConsoleSummaryFetch:   fetcher,
		openCodeGoReferralActionApplier: applier,
	}

	finalSummary, applied := svc.AutoApplyOpenCodeGoReferralRewardsIfEligible(
		context.Background(),
		42,
		"wrk_test_123",
		"cookie_123",
		initialSummary,
	)

	if !applied {
		t.Fatalf("expected applied to be true")
	}
	if len(applier.applyCalls) != 2 || applier.applyCalls[0] != "rew_1" || applier.applyCalls[1] != "rew_2" {
		t.Fatalf("expected apply calls [rew_1, rew_2], got: %v", applier.applyCalls)
	}
	if finalSummary.Usage.FiveHour.UsagePercent != 45.0 {
		t.Fatalf("final 5h percent = %v, want 45.0", finalSummary.Usage.FiveHour.UsagePercent)
	}
}

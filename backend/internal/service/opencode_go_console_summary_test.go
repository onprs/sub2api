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

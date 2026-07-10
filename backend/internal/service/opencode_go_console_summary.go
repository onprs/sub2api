package service

import (
	"context"
	"fmt"
	"time"
)

func BuildOpenCodeGoConsoleSummaryExtra(summary *OpenCodeGoConsoleSummary, authStatus string) map[string]any {
	if summary == nil {
		return nil
	}
	if authStatus == "" {
		authStatus = OpenCodeGoConsoleAuthStatusReady
	}
	updates := map[string]any{
		"opencode_go_usage_source":                    openCodeGoUsageSourceOfficialConsole,
		"opencode_go_usage_updated_at":                summary.FetchedAt.UTC().Format(time.RFC3339),
		"opencode_go_console_auth_status":             authStatus,
		"opencode_go_console_auth_checked_at":         summary.FetchedAt.UTC().Format(time.RFC3339),
		"opencode_go_referral_code":                   summary.Referral.ReferralCode,
		"opencode_go_referral_updated_at":             summary.FetchedAt.UTC().Format(time.RFC3339),
		"opencode_go_referral_available_count":        summary.Referral.AvailableCount,
		"opencode_go_referral_available_amount_cents": summary.Referral.AvailableAmountCents,
		"opencode_go_referral_applied_count":          summary.Referral.AppliedCount,
	}
	addOpenCodeGoConsoleUsageExtra(updates, "5h", summary.Usage.FiveHour)
	addOpenCodeGoConsoleUsageExtra(updates, "7d", summary.Usage.SevenDay)
	addOpenCodeGoConsoleUsageExtra(updates, "30d", summary.Usage.ThirtyDay)
	return updates
}

func addOpenCodeGoConsoleUsageExtra(updates map[string]any, prefix string, window OpenCodeGoUsageWindow) {
	updates["opencode_go_usage_"+prefix+"_used_percent"] = window.UsagePercent
	updates["opencode_go_usage_"+prefix+"_reset_in_sec"] = window.ResetInSec
	if window.ResetsAt != nil {
		updates["opencode_go_usage_"+prefix+"_resets_at"] = window.ResetsAt.UTC().Format(time.RFC3339)
	}
}

func (s *AccountUsageService) PersistOpenCodeGoConsoleSummary(ctx context.Context, accountID int64, summary *OpenCodeGoConsoleSummary, authStatus string) error {
	if s == nil || s.accountRepo == nil {
		return fmt.Errorf("account usage service is not configured")
	}
	updates := BuildOpenCodeGoConsoleSummaryExtra(summary, authStatus)
	if len(updates) == 0 {
		return fmt.Errorf("opencode go console summary is empty")
	}
	if err := s.accountRepo.UpdateExtra(ctx, accountID, updates); err != nil {
		return err
	}
	s.syncOpenCodeGoOfficialUsageRateLimitAt(ctx, accountID, nil, updates, summary.FetchedAt)
	return nil
}

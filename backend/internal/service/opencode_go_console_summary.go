package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

// OpenCodeGoAutoApplyRewardThreshold 是自动应用邀请奖励的使用率阈值（80%）
const OpenCodeGoAutoApplyRewardThreshold = 80.0

// OpenCodeGoReferralActionApplier 接口用于应用邀请奖励
type OpenCodeGoReferralActionApplier interface {
	Apply(ctx context.Context, workspaceID, referralID, consoleCookie string) error
}

// IsOpenCodeGoUsageThresholdReached 检查 5h/7d/30d 任一窗口是否达到或超过指定阈值
func IsOpenCodeGoUsageThresholdReached(usage OpenCodeGoConsoleUsage, threshold float64) bool {
	return usage.FiveHour.UsagePercent >= threshold ||
		usage.SevenDay.UsagePercent >= threshold ||
		usage.ThirtyDay.UsagePercent >= threshold
}

// HasAvailableOpenCodeGoReferralReward 检查是否存在可用的邀请奖励
func HasAvailableOpenCodeGoReferralReward(referral OpenCodeGoReferralSummary) bool {
	if referral.AvailableCount > 0 {
		return true
	}
	for _, r := range referral.Rewards {
		if r.Status == "available" {
			return true
		}
	}
	return false
}

// AutoApplyOpenCodeGoReferralRewardsIfEligible 当 5h/7d/30d 任一窗口用量达到或超过 80% 且存在可用邀请奖励时，自动应用奖励并返回最新 summary
func (s *AccountUsageService) AutoApplyOpenCodeGoReferralRewardsIfEligible(
	ctx context.Context,
	accountID int64,
	workspaceID string,
	cookie string,
	summary *OpenCodeGoConsoleSummary,
) (*OpenCodeGoConsoleSummary, bool) {
	if s == nil || summary == nil || workspaceID == "" || cookie == "" {
		return summary, false
	}
	fetcher := s.openCodeGoConsoleSummaryFetch
	if fetcher == nil {
		fetcher = NewOpenCodeGoConsoleClient("", nil)
	}
	actionClient := s.openCodeGoReferralActionApplier
	if actionClient == nil {
		actionClient = NewOpenCodeGoReferralActionClient("", nil)
	}
	return s.autoApplyOpenCodeGoReferralRewards(ctx, accountID, workspaceID, cookie, summary, fetcher, actionClient)
}

func (s *AccountUsageService) autoApplyOpenCodeGoReferralRewards(
	ctx context.Context,
	accountID int64,
	workspaceID string,
	cookie string,
	summary *OpenCodeGoConsoleSummary,
	fetcher OpenCodeGoConsoleSummaryFetcher,
	actionClient OpenCodeGoReferralActionApplier,
) (*OpenCodeGoConsoleSummary, bool) {
	if summary == nil || workspaceID == "" || cookie == "" {
		return summary, false
	}
	if !IsOpenCodeGoUsageThresholdReached(summary.Usage, OpenCodeGoAutoApplyRewardThreshold) {
		return summary, false
	}
	if !HasAvailableOpenCodeGoReferralReward(summary.Referral) {
		return summary, false
	}

	currentSummary := summary
	appliedAny := false
	const maxApplyLoops = 10

	for i := 0; i < maxApplyLoops; i++ {
		if !IsOpenCodeGoUsageThresholdReached(currentSummary.Usage, OpenCodeGoAutoApplyRewardThreshold) {
			break
		}
		var availableReward *OpenCodeGoReferralReward
		for idx := range currentSummary.Referral.Rewards {
			if currentSummary.Referral.Rewards[idx].Status == "available" {
				availableReward = &currentSummary.Referral.Rewards[idx]
				break
			}
		}
		if availableReward == nil {
			break
		}

		slog.Info("opencode_go_auto_applying_referral_reward",
			"account_id", accountID,
			"reward_id", availableReward.ID,
			"amount_cents", availableReward.AmountCents,
			"5h_pct", currentSummary.Usage.FiveHour.UsagePercent,
			"7d_pct", currentSummary.Usage.SevenDay.UsagePercent,
			"30d_pct", currentSummary.Usage.ThirtyDay.UsagePercent,
		)

		err := actionClient.Apply(ctx, workspaceID, availableReward.ID, cookie)
		if err != nil && !errors.Is(err, ErrOpenCodeGoReferralRewardAlreadyApplied) {
			slog.Warn("opencode_go_auto_apply_referral_reward_failed",
				"account_id", accountID,
				"reward_id", availableReward.ID,
				"error", err,
			)
			break
		}

		appliedAny = true

		newSummary, fetchErr := fetcher.FetchSummary(ctx, workspaceID, cookie)
		if fetchErr != nil {
			slog.Warn("opencode_go_auto_apply_refetch_summary_failed",
				"account_id", accountID,
				"error", fetchErr,
			)
			break
		}
		currentSummary = newSummary
	}

	return currentSummary, appliedAny
}

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

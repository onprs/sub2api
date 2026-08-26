package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

const commandCodeUsageCacheTTL = 5 * time.Minute

// commandCodeOfficialUsageQuotaWindows 是官方用量快照覆盖的窗口前缀。
var commandCodeOfficialUsageQuotaWindows = []string{"5h", "7d", "30d"}

// commandCodeBalanceExhaustedEpsilon 判断余额耗尽时允许的浮点误差（美元）。
const commandCodeBalanceExhaustedEpsilon = 0.005

func (s *AccountUsageService) getCommandCodeUsage(ctx context.Context, account *Account, force ...bool) (*UsageInfo, error) {
	forceRefresh := len(force) > 0 && force[0]
	now := time.Now().UTC()
	stored := buildCommandCodeUsageFromExtra(account.Extra, now)
	if !forceRefresh && stored != nil && stored.UpdatedAt != nil && now.Sub(*stored.UpdatedAt) < commandCodeUsageCacheTTL {
		s.syncCommandCodeOfficialUsageRateLimit(ctx, account.ID, account.RateLimitResetAt, account.Extra)
		return stored, nil
	}
	if s == nil || s.commandCodeClient == nil {
		if stored != nil {
			stored.ErrorCode = "configuration_error"
			stored.Error = "Command Code usage client is not configured"
			return stored, nil
		}
		return nil, fmt.Errorf("commandcode usage client is not configured")
	}

	snapshot, err := s.commandCodeClient.FetchUsage(ctx, account)
	if err != nil {
		s.persistCommandCodeUsageError(ctx, account.ID, err, now)
		if stored != nil {
			applyCommandCodeUsageError(stored, err)
			return stored, nil
		}
		return nil, err
	}
	updates := buildCommandCodeUsageExtra(snapshot)
	if err := s.accountRepo.UpdateExtra(ctx, account.ID, updates); err != nil {
		return nil, fmt.Errorf("persist Command Code usage snapshot: %w", err)
	}
	mergeAccountExtra(account, updates)
	s.syncCommandCodeOfficialUsageRateLimit(ctx, account.ID, account.RateLimitResetAt, account.Extra)
	return buildCommandCodeUsageFromExtra(account.Extra, now), nil
}

// syncCommandCodeOfficialUsageRateLimit 在官方快照显示账号完全无法服务时
// 同步账号级限流。Command Code 语义下：窗口限额只约束月度额度，充值
// （purchased/free）余额可绕过窗口；因此仅当充值余额耗尽且
// （任一窗口用满 或 月度额度耗尽）时才主动限流，重置时间取
// 窗口 resetAt 与订阅周期结束时间中最晚的一个。
func (s *AccountUsageService) syncCommandCodeOfficialUsageRateLimit(ctx context.Context, accountID int64, currentResetAt *time.Time, extra map[string]any) {
	if s == nil || s.accountRepo == nil || accountID <= 0 || len(extra) == 0 {
		return
	}
	now := time.Now()
	resetAt := (&Account{
		ID:       accountID,
		Platform: PlatformCommandCode,
		Type:     AccountTypeAPIKey,
		Extra:    extra,
	}).CommandCodeOfficialUsageRateLimitResetAt(now)
	if resetAt == nil {
		return
	}
	if currentResetAt != nil && now.Before(*currentResetAt) && !currentResetAt.Before(*resetAt) {
		return
	}
	if err := s.accountRepo.SetRateLimited(ctx, accountID, *resetAt); err != nil {
		slog.Warn("commandcode_official_usage_rate_limit_sync_failed", "account_id", accountID, "reset_at", *resetAt, "error", err)
	}
}

func buildCommandCodeUsageExtra(snapshot *CommandCodeUsageSnapshot) map[string]any {
	updates := map[string]any{
		"commandcode_usage_source":        commandCodeUsageSourceOfficialAPI,
		"commandcode_usage_auth_status":   "ready",
		"commandcode_usage_last_error_at": nil,
		"commandcode_usage_last_error":    nil,
		"commandcode_usage_plan_id":       nil,
		"commandcode_usage_period_end":    nil,
		"commandcode_usage_balance_usd":   nil,
		"commandcode_usage_monthly_usd":   nil,
		"commandcode_usage_purchased_usd": nil,
		"commandcode_usage_free_usd":      nil,
		"commandcode_usage_5h_used_usd":   nil,
		"commandcode_usage_5h_cap_usd":    nil,
		"commandcode_usage_7d_used_usd":   nil,
		"commandcode_usage_7d_cap_usd":    nil,
		"commandcode_usage_30d_used_usd":  nil,
		"commandcode_usage_30d_cap_usd":   nil,
	}
	for _, prefix := range commandCodeOfficialUsageQuotaWindows {
		updates["commandcode_usage_"+prefix+"_used_percent"] = nil
		updates["commandcode_usage_"+prefix+"_resets_at"] = nil
	}
	if snapshot == nil {
		return updates
	}
	updates["commandcode_usage_updated_at"] = snapshot.FetchedAt.UTC().Format(time.RFC3339Nano)
	updates["commandcode_usage_plan_id"] = snapshot.PlanID
	updates["commandcode_usage_balance_usd"] = snapshot.MonthlyRemaining + snapshot.PurchasedRemaining + snapshot.FreeRemaining
	updates["commandcode_usage_monthly_usd"] = snapshot.MonthlyRemaining
	updates["commandcode_usage_purchased_usd"] = snapshot.PurchasedRemaining
	updates["commandcode_usage_free_usd"] = snapshot.FreeRemaining
	if snapshot.PeriodEnd != nil {
		updates["commandcode_usage_period_end"] = snapshot.PeriodEnd.UTC().Format(time.RFC3339Nano)
	}

	if snapshot.FiveHour != nil {
		writeCommandCodeUsageWindowExtra(updates, "5h", snapshot.FiveHour)
	}
	if snapshot.Weekly != nil {
		writeCommandCodeUsageWindowExtra(updates, "7d", snapshot.Weekly)
	}
	// 30d 窗口 = 订阅月度额度池：cap 来自官方计划表，used = cap - 剩余，
	// 重置时间为当前订阅周期结束。计划未知时保留余额标签但不下发窗口。
	if snapshot.PlanMonthlyCap > 0 {
		used := snapshot.PlanMonthlyCap - snapshot.MonthlyRemaining
		if used < 0 {
			used = 0
		}
		updates["commandcode_usage_30d_used_percent"] = (used / snapshot.PlanMonthlyCap) * 100
		updates["commandcode_usage_30d_used_usd"] = used
		updates["commandcode_usage_30d_cap_usd"] = snapshot.PlanMonthlyCap
		if snapshot.PeriodEnd != nil {
			updates["commandcode_usage_30d_resets_at"] = snapshot.PeriodEnd.UTC().Format(time.RFC3339Nano)
		}
	}
	return updates
}

func writeCommandCodeUsageWindowExtra(updates map[string]any, prefix string, window *CommandCodeUsageWindow) {
	if window == nil {
		return
	}
	updates["commandcode_usage_"+prefix+"_used_usd"] = window.Used
	updates["commandcode_usage_"+prefix+"_cap_usd"] = window.Cap
	if window.Cap > 0 {
		updates["commandcode_usage_"+prefix+"_used_percent"] = (window.Used / window.Cap) * 100
	} else {
		updates["commandcode_usage_"+prefix+"_used_percent"] = 0.0
	}
	if window.ResetAt != nil {
		updates["commandcode_usage_"+prefix+"_resets_at"] = window.ResetAt.UTC().Format(time.RFC3339Nano)
	}
}

func buildCommandCodeUsageFromExtra(extra map[string]any, now time.Time) *UsageInfo {
	if len(extra) == 0 || strings.TrimSpace(fmt.Sprint(extra["commandcode_usage_source"])) != commandCodeUsageSourceOfficialAPI {
		return nil
	}
	updatedAt := observerStoredUsageUpdatedAt(extra, "commandcode_usage_updated_at")
	usage := &UsageInfo{Source: commandCodeUsageSourceOfficialAPI, UpdatedAt: updatedAt}
	usage.FiveHour = buildCommandCodeUsageWindow(extra, "5h", now)
	usage.SevenDay = buildCommandCodeUsageWindow(extra, "7d", now)
	usage.ThirtyDay = buildCommandCodeUsageWindow(extra, "30d", now)
	if observerUsageEmpty(usage) {
		return nil
	}
	return usage
}

func buildCommandCodeUsageWindow(extra map[string]any, prefix string, now time.Time) *UsageProgress {
	key := "commandcode_usage_" + prefix + "_used_percent"
	raw, ok := extra[key]
	if !ok || raw == nil || strings.TrimSpace(fmt.Sprint(raw)) == "<nil>" {
		return nil
	}
	progress := &UsageProgress{Utilization: parseExtraFloat64(raw), Source: commandCodeUsageSourceOfficialAPI}
	progress.SourceLabel = buildCommandCodeUsageWindowLabel(extra, prefix)
	if parsed := observerStoredUsageUpdatedAt(extra, "commandcode_usage_"+prefix+"_resets_at"); parsed != nil {
		progress.ResetsAt = parsed
		progress.RemainingSeconds = int(parsed.Sub(now).Seconds())
		if progress.RemainingSeconds < 0 {
			progress.RemainingSeconds = 0
			progress.Utilization = 0
			progress.ResetsAt = nil
		}
	}
	return progress
}

func buildCommandCodeUsageWindowLabel(extra map[string]any, prefix string) string {
	used := parseExtraFloat64(extra["commandcode_usage_"+prefix+"_used_usd"])
	capValue := parseExtraFloat64(extra["commandcode_usage_"+prefix+"_cap_usd"])
	if capValue > 0 {
		return fmt.Sprintf("$%.2f / $%.2f", used, capValue)
	}
	if used > 0 {
		return fmt.Sprintf("$%.2f", used)
	}
	return ""
}

func (s *AccountUsageService) persistCommandCodeUsageError(ctx context.Context, accountID int64, err error, now time.Time) {
	if s == nil || s.accountRepo == nil || accountID <= 0 || err == nil {
		return
	}
	status := "upstream_error"
	var upstreamErr *CommandCodeError
	if errors.As(err, &upstreamErr) {
		switch upstreamErr.EffectiveStatus() {
		case http.StatusUnauthorized, http.StatusForbidden:
			status = "auth_error"
		case http.StatusTooManyRequests:
			status = "rate_limited"
		case http.StatusNotFound:
			status = "endpoint_unavailable"
		default:
			if upstreamErr.EffectiveStatus() >= http.StatusInternalServerError {
				status = "upstream_error"
			}
		}
	}
	if updateErr := s.accountRepo.UpdateExtra(ctx, accountID, map[string]any{
		"commandcode_usage_source":        commandCodeUsageSourceOfficialAPI,
		"commandcode_usage_auth_status":   status,
		"commandcode_usage_last_error":    sanitizeUpstreamErrorMessage(err.Error()),
		"commandcode_usage_last_error_at": now.UTC().Format(time.RFC3339Nano),
	}); updateErr != nil {
		slog.Warn("commandcode_usage_persist_error_failed", "account_id", accountID, "error", updateErr)
	}
}

func applyCommandCodeUsageError(usage *UsageInfo, err error) {
	if usage == nil || err == nil {
		return
	}
	usage.ErrorCode = "network_error"
	usage.Error = "Command Code usage refresh failed; showing the last successful snapshot"
	var upstreamErr *CommandCodeError
	if !errors.As(err, &upstreamErr) {
		return
	}
	switch upstreamErr.EffectiveStatus() {
	case http.StatusUnauthorized, http.StatusForbidden:
		usage.ErrorCode = "unauthenticated"
		usage.Error = "Command Code API key cannot query usage limits"
	case http.StatusTooManyRequests:
		usage.ErrorCode = "rate_limited"
		usage.Error = "Command Code usage refresh is rate limited; showing the last successful snapshot"
	case http.StatusNotFound:
		usage.ErrorCode = "endpoint_unavailable"
		usage.Error = "Command Code usage endpoints are unavailable on this deployment"
	}
}

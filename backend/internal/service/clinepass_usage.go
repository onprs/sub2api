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

const clinePassUsageCacheTTL = 5 * time.Minute

func (s *AccountUsageService) getClinePassUsage(ctx context.Context, account *Account, force ...bool) (*UsageInfo, error) {
	forceRefresh := len(force) > 0 && force[0]
	now := time.Now().UTC()
	stored := buildClinePassUsageFromExtra(account.Extra, now)
	if !forceRefresh && stored != nil && stored.UpdatedAt != nil && now.Sub(*stored.UpdatedAt) < clinePassUsageCacheTTL {
		s.syncClinePassOfficialUsageRateLimit(ctx, account.ID, account.RateLimitResetAt, account.Extra)
		return stored, nil
	}
	if s == nil || s.clinePassClient == nil {
		if stored != nil {
			stored.ErrorCode = "configuration_error"
			stored.Error = "ClinePass usage client is not configured"
			return stored, nil
		}
		return nil, fmt.Errorf("ClinePass usage client is not configured")
	}

	snapshot, err := s.clinePassClient.FetchUsage(ctx, account)
	if err != nil {
		s.persistClinePassUsageError(ctx, account.ID, err, now)
		if stored != nil {
			applyClinePassUsageError(stored, err)
			return stored, nil
		}
		return nil, err
	}
	updates := buildClinePassUsageExtra(snapshot)
	if err := s.accountRepo.UpdateExtra(ctx, account.ID, updates); err != nil {
		return nil, fmt.Errorf("persist ClinePass usage snapshot: %w", err)
	}
	mergeAccountExtra(account, updates)
	s.syncClinePassOfficialUsageRateLimit(ctx, account.ID, account.RateLimitResetAt, account.Extra)
	return buildClinePassUsageFromExtra(account.Extra, now), nil
}

func (s *AccountUsageService) syncClinePassOfficialUsageRateLimit(ctx context.Context, accountID int64, currentResetAt *time.Time, extra map[string]any) {
	if s == nil || s.accountRepo == nil || accountID <= 0 || len(extra) == 0 {
		return
	}
	now := time.Now().UTC()
	resetAt := (&Account{
		ID:       accountID,
		Platform: PlatformClinePass,
		Type:     AccountTypeAPIKey,
		Extra:    extra,
	}).ClinePassOfficialUsageRateLimitResetAt(now)
	if resetAt == nil {
		return
	}
	if currentResetAt != nil && now.Before(*currentResetAt) && !currentResetAt.Before(*resetAt) {
		return
	}
	if err := s.accountRepo.SetRateLimited(ctx, accountID, *resetAt); err != nil {
		slog.Warn("clinepass_official_usage_rate_limit_sync_failed", "account_id", accountID, "reset_at", *resetAt, "error", err)
	}
}

func buildClinePassUsageExtra(snapshot *ClinePassUsageSnapshot) map[string]any {
	updates := map[string]any{
		"clinepass_usage_source":        clinePassUsageSourceOfficialAPI,
		"clinepass_usage_auth_status":   "ready",
		"clinepass_usage_last_error_at": nil,
	}
	if snapshot == nil {
		return updates
	}
	updates["clinepass_usage_updated_at"] = snapshot.FetchedAt.UTC().Format(time.RFC3339Nano)
	for _, prefix := range clinePassOfficialUsageQuotaWindows {
		updates["clinepass_usage_"+prefix+"_used_percent"] = nil
		updates["clinepass_usage_"+prefix+"_resets_at"] = nil
		window, ok := snapshot.Windows[prefix]
		if !ok {
			continue
		}
		updates["clinepass_usage_"+prefix+"_used_percent"] = window.PercentUsed
		if window.ResetsAt != nil {
			updates["clinepass_usage_"+prefix+"_resets_at"] = window.ResetsAt.UTC().Format(time.RFC3339Nano)
		}
	}
	return updates
}

func buildClinePassUsageFromExtra(extra map[string]any, now time.Time) *UsageInfo {
	if len(extra) == 0 || strings.TrimSpace(fmt.Sprint(extra["clinepass_usage_source"])) != clinePassUsageSourceOfficialAPI {
		return nil
	}
	updatedAt := observerStoredUsageUpdatedAt(extra, "clinepass_usage_updated_at")
	usage := &UsageInfo{Source: clinePassUsageSourceOfficialAPI, UpdatedAt: updatedAt}
	usage.FiveHour = buildClinePassUsageWindow(extra, "5h", now)
	usage.SevenDay = buildClinePassUsageWindow(extra, "7d", now)
	usage.ThirtyDay = buildClinePassUsageWindow(extra, "30d", now)
	if observerUsageEmpty(usage) {
		return nil
	}
	return usage
}

func buildClinePassUsageWindow(extra map[string]any, prefix string, now time.Time) *UsageProgress {
	key := "clinepass_usage_" + prefix + "_used_percent"
	raw, ok := extra[key]
	if !ok || raw == nil || strings.TrimSpace(fmt.Sprint(raw)) == "<nil>" {
		return nil
	}
	progress := &UsageProgress{Utilization: parseExtraFloat64(raw), Source: clinePassUsageSourceOfficialAPI}
	if parsed := observerStoredUsageUpdatedAt(extra, "clinepass_usage_"+prefix+"_resets_at"); parsed != nil {
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

func (s *AccountUsageService) persistClinePassUsageError(ctx context.Context, accountID int64, err error, now time.Time) {
	if s == nil || s.accountRepo == nil || accountID <= 0 {
		return
	}
	status := "error"
	var upstreamErr *ClinePassError
	if errors.As(err, &upstreamErr) {
		switch upstreamErr.EffectiveStatus() {
		case http.StatusUnauthorized, http.StatusForbidden:
			status = "auth_error"
		case http.StatusTooManyRequests:
			status = "rate_limited"
		default:
			if upstreamErr.EffectiveStatus() >= http.StatusInternalServerError {
				status = "upstream_error"
			}
		}
	}
	_ = s.accountRepo.UpdateExtra(ctx, accountID, map[string]any{
		"clinepass_usage_auth_status":   status,
		"clinepass_usage_last_error_at": now.UTC().Format(time.RFC3339Nano),
	})
}

func applyClinePassUsageError(usage *UsageInfo, err error) {
	if usage == nil || err == nil {
		return
	}
	usage.ErrorCode = "network_error"
	usage.Error = "ClinePass usage refresh failed; showing the last successful snapshot"
	var upstreamErr *ClinePassError
	if !errors.As(err, &upstreamErr) {
		return
	}
	switch upstreamErr.EffectiveStatus() {
	case http.StatusUnauthorized, http.StatusForbidden:
		usage.ErrorCode = "unauthenticated"
		usage.Error = "ClinePass API key cannot query usage limits"
	case http.StatusTooManyRequests:
		usage.ErrorCode = "rate_limited"
		usage.Error = "ClinePass usage refresh is rate limited; showing the last successful snapshot"
	}
}

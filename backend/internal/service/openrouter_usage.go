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

const openRouterUsageCacheTTL = 5 * time.Minute

func (s *AccountUsageService) getOpenRouterUsage(ctx context.Context, account *Account, force ...bool) (*UsageInfo, error) {
	forceRefresh := len(force) > 0 && force[0]
	now := time.Now().UTC()
	stored := buildOpenRouterUsageFromExtra(account.Extra, now)
	if !forceRefresh && stored != nil && stored.UpdatedAt != nil && now.Sub(*stored.UpdatedAt) < openRouterUsageCacheTTL {
		return stored, nil
	}
	if s == nil || s.openRouterClient == nil {
		if stored != nil {
			stored.ErrorCode = "configuration_error"
			stored.Error = "OpenRouter usage client is not configured"
			return stored, nil
		}
		return nil, fmt.Errorf("OpenRouter usage client is not configured")
	}

	snapshot, err := s.openRouterClient.FetchUsage(ctx, account)
	if err != nil {
		s.persistOpenRouterUsageError(ctx, account.ID, err, now)
		if stored != nil {
			applyOpenRouterUsageError(stored, err)
			return stored, nil
		}
		return nil, err
	}
	updates := buildOpenRouterUsageExtra(snapshot)
	if err := s.accountRepo.UpdateExtra(ctx, account.ID, updates); err != nil {
		return nil, fmt.Errorf("persist OpenRouter usage snapshot: %w", err)
	}
	mergeAccountExtra(account, updates)
	return buildOpenRouterUsageFromExtra(account.Extra, now), nil
}

func buildOpenRouterUsageExtra(snapshot *OpenRouterUsageSnapshot) map[string]any {
	updates := map[string]any{
		"openrouter_usage_source":        openrouterUsageSourceOfficialAPI,
		"openrouter_usage_auth_status":   "ready",
		"openrouter_usage_last_error_at": nil,
	}
	if snapshot == nil {
		return updates
	}
	updates["openrouter_usage_updated_at"] = snapshot.FetchedAt.UTC().Format(time.RFC3339Nano)
	updates["openrouter_usage_label"] = snapshot.Label
	updates["openrouter_usage_used_usd"] = snapshot.UsageUSD
	updates["openrouter_usage_is_free_tier"] = snapshot.IsFreeTier
	if snapshot.LimitUSD != nil {
		updates["openrouter_usage_limit_usd"] = *snapshot.LimitUSD
	} else {
		updates["openrouter_usage_limit_usd"] = nil
	}
	if snapshot.LimitRemaining != nil {
		updates["openrouter_usage_remaining_usd"] = *snapshot.LimitRemaining
	} else {
		updates["openrouter_usage_remaining_usd"] = nil
	}
	if snapshot.RateLimitReqs != nil {
		updates["openrouter_usage_rate_limit_requests"] = *snapshot.RateLimitReqs
	} else {
		updates["openrouter_usage_rate_limit_requests"] = nil
	}
	updates["openrouter_usage_rate_limit_period"] = snapshot.RateLimitPeriod
	return updates
}

func buildOpenRouterUsageFromExtra(extra map[string]any, now time.Time) *UsageInfo {
	if len(extra) == 0 || strings.TrimSpace(fmt.Sprint(extra["openrouter_usage_source"])) != openrouterUsageSourceOfficialAPI {
		return nil
	}
	updatedAt := observerStoredUsageUpdatedAt(extra, "openrouter_usage_updated_at")
	usage := &UsageInfo{
		Source:    openrouterUsageSourceOfficialAPI,
		UpdatedAt: updatedAt,
	}
	if raw, ok := extra["openrouter_usage_used_usd"]; ok && raw != nil {
		usedUSD := parseExtraFloat64(raw)
		var limitUSD float64
		if lim, ok := extra["openrouter_usage_limit_usd"]; ok && lim != nil {
			limitUSD = parseExtraFloat64(lim)
		}
		var util float64
		var label string
		if limitUSD > 0 {
			util = (usedUSD / limitUSD) * 100
			label = fmt.Sprintf("$%.2f / $%.2f", usedUSD, limitUSD)
		} else {
			label = fmt.Sprintf("$%.2f", usedUSD)
		}
		progress := &UsageProgress{
			Utilization: util,
			Source:      openrouterUsageSourceOfficialAPI,
			SourceLabel: label,
		}
		if reqs, ok := extra["openrouter_usage_rate_limit_requests"]; ok && reqs != nil {
			progress.LimitRequests = int64(parseExtraInt(reqs))
		}
		usage.ThirtyDay = progress
	}
	if observerUsageEmpty(usage) {
		return nil
	}
	return usage
}

func (s *AccountUsageService) persistOpenRouterUsageError(ctx context.Context, accountID int64, err error, now time.Time) {
	if s == nil || s.accountRepo == nil || accountID <= 0 || err == nil {
		return
	}
	code := "upstream_error"
	var openRouterErr *OpenRouterError
	if errors.As(err, &openRouterErr) {
		if openRouterErr.EffectiveStatus() == http.StatusUnauthorized || openRouterErr.EffectiveStatus() == http.StatusForbidden {
			code = "auth_invalid"
		}
	}
	updates := map[string]any{
		"openrouter_usage_source":        openrouterUsageSourceOfficialAPI,
		"openrouter_usage_auth_status":   code,
		"openrouter_usage_last_error":    sanitizeUpstreamErrorMessage(err.Error()),
		"openrouter_usage_last_error_at": now.UTC().Format(time.RFC3339Nano),
	}
	if updateErr := s.accountRepo.UpdateExtra(ctx, accountID, updates); updateErr != nil {
		slog.Warn("openrouter_usage_persist_error_failed", "account_id", accountID, "error", updateErr)
	}
}

func applyOpenRouterUsageError(usage *UsageInfo, err error) {
	if usage == nil || err == nil {
		return
	}
	usage.Error = sanitizeUpstreamErrorMessage(err.Error())
	usage.ErrorCode = "upstream_error"
	var openRouterErr *OpenRouterError
	if errors.As(err, &openRouterErr) {
		if openRouterErr.EffectiveStatus() == http.StatusUnauthorized || openRouterErr.EffectiveStatus() == http.StatusForbidden {
			usage.ErrorCode = "auth_invalid"
		}
	}
}

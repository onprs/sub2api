package service

import (
	"fmt"
	"strings"
	"time"
)

// GetStoredUsageSnapshot returns only quota data already persisted on an account.
// It never calls an upstream provider and never mutates account state.
func (s *AccountUsageService) GetStoredUsageSnapshot(account *Account, now time.Time) (*UsageInfo, error) {
	if account == nil {
		return nil, ErrObserverQuotaUnavailable
	}
	if now.IsZero() {
		now = time.Now()
	}

	switch account.Platform {
	case PlatformAnthropic:
		usage := s.estimateSetupTokenUsageAt(account, now)
		usage.Source = "stored"
		usage.SevenDay = buildPassiveUsageWindowAt(account.Extra, "passive_usage_7d_utilization", "passive_usage_7d_reset", now)
		usage.SevenDayFable = buildPassiveUsageWindowAt(account.Extra, "passive_usage_7d_oi_utilization", "passive_usage_7d_oi_reset", now)
		usage.UpdatedAt = observerStoredUsageUpdatedAt(account.Extra, "passive_usage_sampled_at")
		if observerUsageEmpty(usage) {
			return nil, ErrObserverQuotaUnavailable
		}
		return usage, nil

	case PlatformOpenAI:
		usage := &UsageInfo{Source: "stored", UpdatedAt: observerStoredUsageUpdatedAt(account.Extra, "codex_usage_updated_at")}
		applyExtraToUsage(usage, account.Extra, now)
		if observerUsageEmpty(usage) {
			return nil, ErrObserverQuotaUnavailable
		}
		return usage, nil

	case PlatformOpenCodeGo:
		if usage := buildOpenCodeGoOfficialUsageFromExtra(account.Extra, now); usage != nil {
			return usage, nil
		}
		usage := &UsageInfo{
			Source:    strings.TrimSpace(fmt.Sprint(account.Extra["opencode_go_usage_source"])),
			UpdatedAt: observerStoredUsageUpdatedAt(account.Extra, "opencode_go_usage_updated_at"),
		}
		usage.FiveHour = observerOpenCodeGoStoredWindow(account.Extra, "5h")
		usage.SevenDay = observerOpenCodeGoStoredWindow(account.Extra, "7d")
		usage.ThirtyDay = observerOpenCodeGoStoredWindow(account.Extra, "30d")
		if observerUsageEmpty(usage) {
			return nil, ErrObserverQuotaUnavailable
		}
		return usage, nil

	case PlatformClinePass:
		if usage := buildClinePassUsageFromExtra(account.Extra, now); usage != nil {
			return usage, nil
		}
		return nil, ErrObserverQuotaUnavailable

	default:
		return nil, ErrObserverQuotaUnavailable
	}
}

func observerOpenCodeGoStoredWindow(extra map[string]any, prefix string) *UsageProgress {
	key := "opencode_go_usage_" + prefix + "_used_percent"
	if extra == nil {
		return nil
	}
	if _, ok := extra[key]; !ok {
		return nil
	}
	return &UsageProgress{
		Utilization: parseExtraFloat64(extra[key]),
		Estimated:   strings.EqualFold(strings.TrimSpace(fmt.Sprint(extra["opencode_go_usage_source"])), openCodeGoUsageSourceEstimated),
	}
}

func observerStoredUsageUpdatedAt(extra map[string]any, key string) *time.Time {
	if extra == nil {
		return nil
	}
	raw := strings.TrimSpace(fmt.Sprint(extra[key]))
	if raw == "" || raw == "<nil>" {
		return nil
	}
	parsed, err := parseTime(raw)
	if err != nil {
		return nil
	}
	utc := parsed.UTC()
	return &utc
}

func observerUsageEmpty(usage *UsageInfo) bool {
	if usage == nil {
		return true
	}
	return usage.FiveHour == nil && usage.SevenDay == nil && usage.ThirtyDay == nil &&
		usage.SevenDaySonnet == nil && usage.SevenDayFable == nil &&
		usage.GeminiSharedDaily == nil && usage.GeminiProDaily == nil && usage.GeminiFlashDaily == nil
}

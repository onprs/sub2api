package repository

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestSchedulerMetadataAccountPreservesCommandCodeUsageState(t *testing.T) {
	account := service.Account{
		ID:       8287,
		Platform: service.PlatformCommandCode,
		Type:     service.AccountTypeAPIKey,
		Extra: map[string]any{
			"commandcode_usage_source":           "official_api",
			"commandcode_usage_updated_at":       "2026-09-14T17:58:34Z",
			"commandcode_usage_plan_id":          "individual-goat",
			"commandcode_usage_period_end":       "2026-10-07T11:53:22Z",
			"commandcode_usage_balance_usd":      34.87,
			"commandcode_usage_monthly_usd":      34.87,
			"commandcode_usage_purchased_usd":    0.0,
			"commandcode_usage_free_usd":         0.0,
			"commandcode_usage_5h_used_percent":  0.8,
			"commandcode_usage_5h_resets_at":     "2026-09-14T22:01:45Z",
			"commandcode_usage_7d_used_percent":  0.3,
			"commandcode_usage_7d_resets_at":     "2026-09-21T17:01:45Z",
			"commandcode_usage_30d_used_percent": 50.1,
			"commandcode_usage_30d_resets_at":    "2026-10-07T11:53:22Z",
			"commandcode_usage_last_error":       "drop diagnostic text",
			"unused_large_field":                 "drop-me",
		},
	}

	metadata := buildSchedulerMetadataAccount(account)
	for key, value := range map[string]any{
		"commandcode_usage_source":           "official_api",
		"commandcode_usage_updated_at":       "2026-09-14T17:58:34Z",
		"commandcode_usage_plan_id":          "individual-goat",
		"commandcode_usage_period_end":       "2026-10-07T11:53:22Z",
		"commandcode_usage_balance_usd":      34.87,
		"commandcode_usage_monthly_usd":      34.87,
		"commandcode_usage_purchased_usd":    0.0,
		"commandcode_usage_free_usd":         0.0,
		"commandcode_usage_5h_used_percent":  0.8,
		"commandcode_usage_5h_resets_at":     "2026-09-14T22:01:45Z",
		"commandcode_usage_7d_used_percent":  0.3,
		"commandcode_usage_7d_resets_at":     "2026-09-21T17:01:45Z",
		"commandcode_usage_30d_used_percent": 50.1,
		"commandcode_usage_30d_resets_at":    "2026-10-07T11:53:22Z",
	} {
		require.Equal(t, value, metadata.Extra[key], key)
	}
	require.NotContains(t, metadata.Extra, "commandcode_usage_last_error")
	require.NotContains(t, metadata.Extra, "unused_large_field")
}

func TestCommandCodeUsageExtraIsSchedulerNeutral(t *testing.T) {
	require.True(t, isSchedulerNeutralExtraKey("commandcode_usage_updated_at"))
	require.False(t, shouldEnqueueSchedulerOutboxForExtraUpdates(map[string]any{
		"commandcode_usage_source":          "official_api",
		"commandcode_usage_5h_used_percent": 25.0,
	}))
}

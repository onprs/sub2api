package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestGetStoredUsageSnapshotUsesProvidedNow(t *testing.T) {
	now := time.Date(2026, 7, 17, 10, 0, 0, 0, time.UTC)
	fiveHourReset := now.Add(time.Hour)
	sevenDayReset := now.Add(2 * time.Hour)
	usage, err := (&AccountUsageService{}).GetStoredUsageSnapshot(&Account{
		Platform:         PlatformAnthropic,
		SessionWindowEnd: &fiveHourReset,
		UpdatedAt:        now.Add(-time.Minute),
		Extra: map[string]any{
			"session_window_utilization":   0.42,
			"passive_usage_7d_utilization": 0.73,
			"passive_usage_7d_reset":       sevenDayReset.Unix(),
			"passive_usage_sampled_at":     now.Add(-time.Minute).Format(time.RFC3339),
		},
	}, now)

	require.NoError(t, err)
	require.NotNil(t, usage.FiveHour)
	require.InDelta(t, 42, usage.FiveHour.Utilization, 0.001)
	require.Equal(t, 3600, usage.FiveHour.RemainingSeconds)
	require.NotNil(t, usage.SevenDay)
	require.InDelta(t, 73, usage.SevenDay.Utilization, 0.001)
	require.Equal(t, 7200, usage.SevenDay.RemainingSeconds)
}

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAdminUpdateAccountPreservesProviderUsageSnapshots(t *testing.T) {
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name      string
		id        int64
		platform  string
		extra     map[string]any
		source    string
		fiveHour  float64
		sevenDay  float64
		thirtyDay float64
	}{
		{
			name:     "OpenCode Go",
			id:       601,
			platform: PlatformOpenCodeGo,
			extra: map[string]any{
				"opencode_go_console_auth_status":    OpenCodeGoConsoleAuthStatusReady,
				"opencode_go_usage_source":           openCodeGoUsageSourceOfficialConsole,
				"opencode_go_usage_updated_at":       now.Format(time.RFC3339),
				"opencode_go_usage_5h_used_percent":  19.0,
				"opencode_go_usage_5h_resets_at":     now.Add(5 * time.Hour).Format(time.RFC3339),
				"opencode_go_usage_7d_used_percent":  7.0,
				"opencode_go_usage_7d_resets_at":     now.Add(7 * 24 * time.Hour).Format(time.RFC3339),
				"opencode_go_usage_30d_used_percent": 10.0,
				"opencode_go_usage_30d_resets_at":    now.Add(30 * 24 * time.Hour).Format(time.RFC3339),
			},
			source:    openCodeGoUsageSourceOfficialConsole,
			fiveHour:  19.0,
			sevenDay:  7.0,
			thirtyDay: 10.0,
		},
		{
			name:     "Command Code",
			id:       602,
			platform: PlatformCommandCode,
			extra: map[string]any{
				"commandcode_usage_source":           commandCodeUsageSourceOfficialAPI,
				"commandcode_usage_updated_at":       now.Format(time.RFC3339),
				"commandcode_usage_purchased_usd":    1.0,
				"commandcode_usage_5h_used_percent":  25.0,
				"commandcode_usage_5h_resets_at":     now.Add(5 * time.Hour).Format(time.RFC3339),
				"commandcode_usage_7d_used_percent":  30.0,
				"commandcode_usage_7d_resets_at":     now.Add(7 * 24 * time.Hour).Format(time.RFC3339),
				"commandcode_usage_30d_used_percent": 40.0,
				"commandcode_usage_30d_resets_at":    now.Add(30 * 24 * time.Hour).Format(time.RFC3339),
			},
			source:    commandCodeUsageSourceOfficialAPI,
			fiveHour:  25.0,
			sevenDay:  30.0,
			thirtyDay: 40.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &upstreamBillingProbeAccountRepo{accounts: map[int64]*Account{
				tt.id: {
					ID:       tt.id,
					Platform: tt.platform,
					Type:     AccountTypeAPIKey,
					Status:   StatusActive,
					Credentials: map[string]any{
						"api_key": "test-api-key",
					},
					Extra: tt.extra,
				},
			}}
			svc := &adminServiceImpl{accountRepo: repo}

			updated, err := svc.UpdateAccount(context.Background(), tt.id, &UpdateAccountInput{
				Extra: map[string]any{"custom_setting": "edited"},
			})
			require.NoError(t, err)
			require.NotNil(t, updated)
			require.Equal(t, "edited", updated.Extra["custom_setting"])
			for key, value := range tt.extra {
				require.Equal(t, value, updated.Extra[key], "快照字段 %s 不应在账号编辑后丢失", key)
			}

			usage, err := (&AccountUsageService{}).GetStoredUsageSnapshot(updated, now)
			require.NoError(t, err)
			require.NotNil(t, usage)
			require.Equal(t, tt.source, usage.Source)
			require.NotNil(t, usage.FiveHour)
			require.NotNil(t, usage.SevenDay)
			require.NotNil(t, usage.ThirtyDay)
			require.Equal(t, tt.fiveHour, usage.FiveHour.Utilization)
			require.Equal(t, tt.sevenDay, usage.SevenDay.Utilization)
			require.Equal(t, tt.thirtyDay, usage.ThirtyDay.Utilization)
		})
	}
}

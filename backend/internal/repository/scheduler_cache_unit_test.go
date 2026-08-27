//go:build unit

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestBuildSchedulerMetadataAccount_KeepsOpenAIWSFlags(t *testing.T) {
	account := service.Account{
		ID:       42,
		Platform: service.PlatformOpenAI,
		Type:     service.AccountTypeOAuth,
		Extra: map[string]any{
			"openai_oauth_responses_websockets_v2_enabled": true,
			"openai_oauth_responses_websockets_v2_mode":    service.OpenAIWSIngressModePassthrough,
			"openai_ws_force_http":                         true,
			"openai_responses_mode":                        "force_chat_completions",
			"openai_responses_supported":                   false,
			"mixed_scheduling":                             true,
			"unused_large_field":                           "drop-me",
		},
	}

	got := buildSchedulerMetadataAccount(account)

	require.Equal(t, true, got.Extra["openai_oauth_responses_websockets_v2_enabled"])
	require.Equal(t, service.OpenAIWSIngressModePassthrough, got.Extra["openai_oauth_responses_websockets_v2_mode"])
	require.Equal(t, true, got.Extra["openai_ws_force_http"])
	require.Equal(t, "force_chat_completions", got.Extra["openai_responses_mode"])
	require.Equal(t, false, got.Extra["openai_responses_supported"])
	require.Equal(t, true, got.Extra["mixed_scheduling"])
	require.Nil(t, got.Extra["unused_large_field"])
}

func TestSchedulerCacheSnapshotPreservesOpenAISelectionMetadata(t *testing.T) {
	ctx := context.Background()
	server := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { require.NoError(t, rdb.Close()) })
	cache := NewSchedulerCache(rdb)
	bucket := service.SchedulerBucket{GroupID: 89, Platform: service.PlatformOpenAI, Mode: service.SchedulerModeSingle}
	account := service.Account{
		ID:          89,
		Platform:    service.PlatformOpenAI,
		Type:        service.AccountTypeAPIKey,
		Status:      service.StatusActive,
		Schedulable: true,
		Credentials: map[string]any{
			"openai_capabilities": []any{string(service.OpenAIEndpointCapabilityEmbeddings)},
		},
		Extra: map[string]any{
			"privacy_mode":             service.PrivacyModeTrainingOff,
			"openai_passthrough":       true,
			"openai_oauth_passthrough": true,
			"openai_compact_mode":      service.OpenAICompactModeForceOn,
			"openai_compact_supported": true,
			"unused_large_field":       "drop-me",
		},
	}

	require.NoError(t, cache.SetSnapshot(ctx, bucket, []service.Account{account}))
	snapshot, hit, err := cache.GetSnapshot(ctx, bucket)
	require.NoError(t, err)
	require.True(t, hit)
	require.Len(t, snapshot, 1)

	got := snapshot[0]
	require.True(t, got.IsPrivacySet())
	require.True(t, got.IsOpenAIPassthroughEnabled())
	require.Equal(t, service.OpenAICompactModeForceOn, got.GetOpenAICompactMode())
	compactSupported, compactKnown := got.OpenAICompactSupportKnown()
	require.True(t, compactKnown)
	require.True(t, compactSupported)
	require.True(t, got.SupportsOpenAIEndpointCapability(service.OpenAIEndpointCapabilityEmbeddings))
	require.False(t, got.SupportsOpenAIEndpointCapability(service.OpenAIEndpointCapabilityChatCompletions))
	require.Equal(t, true, got.Extra["openai_oauth_passthrough"])
	require.NotContains(t, got.Extra, "unused_large_field")
}

func TestSchedulerCacheSnapshotPreservesGrokQuotaMetadata(t *testing.T) {
	ctx := context.Background()
	server := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { require.NoError(t, rdb.Close()) })
	cache := NewSchedulerCache(rdb)
	bucket := service.SchedulerBucket{GroupID: 90, Platform: service.PlatformGrok, Mode: service.SchedulerModeSingle}
	account := service.Account{
		ID:          90,
		Platform:    service.PlatformGrok,
		Type:        service.AccountTypeOAuth,
		Status:      service.StatusActive,
		Schedulable: true,
		Extra: map[string]any{
			"grok_usage_snapshot": map[string]any{
				"requests":   map[string]any{"limit": 100, "remaining": 0},
				"updated_at": time.Now().UTC().Format(time.RFC3339),
			},
		},
	}

	require.NoError(t, cache.SetSnapshot(ctx, bucket, []service.Account{account}))
	snapshot, hit, err := cache.GetSnapshot(ctx, bucket)
	require.NoError(t, err)
	require.True(t, hit)
	require.Len(t, snapshot, 1)

	grokUsage := service.NewGrokQuotaFetcher().BuildUsageInfo(snapshot[0])
	require.NotNil(t, grokUsage.GrokRequestQuota)
	require.NotNil(t, grokUsage.GrokRequestQuota.Remaining)
	require.Zero(t, *grokUsage.GrokRequestQuota.Remaining)
}

func TestBuildSchedulerMetadataAccount_KeepsSlimGroupMembership(t *testing.T) {
	account := service.Account{
		ID:       42,
		Platform: service.PlatformAnthropic,
		GroupIDs: []int64{7, 9, 7, 0},
		AccountGroups: []service.AccountGroup{
			{
				AccountID: 42,
				GroupID:   7,
				Priority:  2,
				Account:   &service.Account{ID: 42, Name: "drop-from-metadata"},
				Group:     &service.Group{ID: 7, Name: "drop-from-metadata"},
			},
			{
				AccountID: 42,
				GroupID:   11,
				Priority:  3,
				Group:     &service.Group{ID: 11, Name: "drop-from-metadata"},
			},
			{
				AccountID: 42,
				GroupID:   0,
				Priority:  4,
			},
		},
	}

	got := buildSchedulerMetadataAccount(account)

	require.Equal(t, []int64{7, 9, 11}, got.GroupIDs)
	require.Len(t, got.AccountGroups, 2)
	require.Equal(t, int64(42), got.AccountGroups[0].AccountID)
	require.Equal(t, int64(7), got.AccountGroups[0].GroupID)
	require.Equal(t, 2, got.AccountGroups[0].Priority)
	require.Nil(t, got.AccountGroups[0].Account)
	require.Nil(t, got.AccountGroups[0].Group)
	require.Equal(t, int64(11), got.AccountGroups[1].GroupID)
	require.Nil(t, got.Groups)
}

func TestBuildSchedulerMetadataAccount_KeepsQuotaAutoPauseFields(t *testing.T) {
	account := service.Account{
		ID: 88,
		Extra: map[string]any{
			"codex_5h_used_percent":                12.34,
			"codex_7d_used_percent":                56.78,
			"codex_5h_reset_at":                    "2026-05-29T10:00:00Z",
			"codex_7d_reset_at":                    "2026-06-01T10:00:00Z",
			"codex_5h_reset_after_seconds":         300,
			"codex_7d_reset_after_seconds":         600,
			"codex_5h_window_minutes":              300,
			"codex_7d_window_minutes":              10080,
			"codex_primary_used_percent":           80.0,
			"codex_primary_reset_at":               "2026-06-03T10:00:00Z",
			"codex_primary_reset_after_seconds":    7000,
			"codex_primary_window_minutes":         300,
			"codex_secondary_used_percent":         93.0,
			"codex_secondary_reset_at":             "2026-05-30T10:00:00Z",
			"codex_secondary_reset_after_seconds":  437150,
			"codex_secondary_window_minutes":       10080,
			"codex_primary_over_secondary_percent": 0.0,
			"auto_reset_credit_enabled":            true,
			"auto_reset_credit_5h_threshold":       0.95,
			"auto_reset_credit_7d_threshold":       0.9,
			"codex_auto_reset_credit_state":        map[string]any{"status": "available"},
			"codex_usage_updated_at":               "2026-05-29T09:00:00Z",
			"auto_pause_5h_threshold":              0.95,
			"auto_pause_7d_threshold":              0.96,
			"auto_pause_5h_disabled":               true,
			"auto_pause_7d_disabled":               false,
		},
	}

	got := buildSchedulerMetadataAccount(account)

	require.Equal(t, 12.34, got.Extra["codex_5h_used_percent"])
	require.Equal(t, 56.78, got.Extra["codex_7d_used_percent"])
	require.Equal(t, "2026-05-29T10:00:00Z", got.Extra["codex_5h_reset_at"])
	require.Equal(t, "2026-06-01T10:00:00Z", got.Extra["codex_7d_reset_at"])
	require.Equal(t, 300, got.Extra["codex_5h_reset_after_seconds"])
	require.Equal(t, 600, got.Extra["codex_7d_reset_after_seconds"])
	require.Equal(t, 300, got.Extra["codex_5h_window_minutes"])
	require.Equal(t, 10080, got.Extra["codex_7d_window_minutes"])
	require.Equal(t, 80.0, got.Extra["codex_primary_used_percent"])
	require.Equal(t, "2026-06-03T10:00:00Z", got.Extra["codex_primary_reset_at"])
	require.Equal(t, 7000, got.Extra["codex_primary_reset_after_seconds"])
	require.Equal(t, 300, got.Extra["codex_primary_window_minutes"])
	require.Equal(t, 93.0, got.Extra["codex_secondary_used_percent"])
	require.Equal(t, "2026-05-30T10:00:00Z", got.Extra["codex_secondary_reset_at"])
	require.Equal(t, 437150, got.Extra["codex_secondary_reset_after_seconds"])
	require.Equal(t, 10080, got.Extra["codex_secondary_window_minutes"])
	require.Equal(t, 0.0, got.Extra["codex_primary_over_secondary_percent"])
	require.Equal(t, true, got.Extra["auto_reset_credit_enabled"])
	require.Equal(t, 0.95, got.Extra["auto_reset_credit_5h_threshold"])
	require.Equal(t, 0.9, got.Extra["auto_reset_credit_7d_threshold"])
	require.Equal(t, map[string]any{"status": "available"}, got.Extra["codex_auto_reset_credit_state"])
	require.Equal(t, "2026-05-29T09:00:00Z", got.Extra["codex_usage_updated_at"])
	require.Equal(t, 0.95, got.Extra["auto_pause_5h_threshold"])
	require.Equal(t, 0.96, got.Extra["auto_pause_7d_threshold"])
	require.Equal(t, true, got.Extra["auto_pause_5h_disabled"])
	require.Equal(t, false, got.Extra["auto_pause_7d_disabled"])
}

func TestBuildSchedulerMetadataAccount_KeepsQuotaStateForCachedAccounts(t *testing.T) {
	now := time.Now().UTC()
	activeStart := now.Add(-time.Hour).Format(time.RFC3339)
	expiredDailyStart := now.Add(-25 * time.Hour).Format(time.RFC3339)
	expiredWeeklyStart := now.Add(-8 * 24 * time.Hour).Format(time.RFC3339)
	weeklyResetDay := float64(now.AddDate(0, 0, 1).Weekday())

	cases := []struct {
		name          string
		platform      string
		typ           string
		extra         map[string]any
		quotaExceeded bool
	}{
		{
			name: "anthropic api key total quota exhausted", platform: service.PlatformAnthropic, typ: service.AccountTypeAPIKey,
			extra: map[string]any{"quota_limit": 10.0, "quota_used": 10.0}, quotaExceeded: true,
		},
		{
			name: "gemini api key rolling daily quota exhausted", platform: service.PlatformGemini, typ: service.AccountTypeAPIKey,
			extra: map[string]any{
				"quota_daily_limit": 20.0, "quota_daily_used": 20.0,
				"quota_daily_start": activeStart, "quota_daily_reset_mode": "rolling",
			}, quotaExceeded: true,
		},
		{
			name: "gemini api key expired rolling daily window", platform: service.PlatformGemini, typ: service.AccountTypeAPIKey,
			extra: map[string]any{
				"quota_daily_limit": 20.0, "quota_daily_used": 20.0,
				"quota_daily_start": expiredDailyStart, "quota_daily_reset_mode": "rolling",
			},
		},
		{
			name: "bedrock fixed weekly quota exhausted", platform: service.PlatformAnthropic, typ: service.AccountTypeBedrock,
			extra: map[string]any{
				"quota_weekly_limit": 30.0, "quota_weekly_used": 30.0, "quota_weekly_start": activeStart,
				"quota_weekly_reset_mode": "fixed", "quota_weekly_reset_day": weeklyResetDay,
				"quota_weekly_reset_hour": 0.0, "quota_reset_timezone": "UTC",
			}, quotaExceeded: true,
		},
		{
			name: "bedrock expired fixed weekly window", platform: service.PlatformAnthropic, typ: service.AccountTypeBedrock,
			extra: map[string]any{
				"quota_weekly_limit": 30.0, "quota_weekly_used": 30.0, "quota_weekly_start": expiredWeeklyStart,
				"quota_weekly_reset_mode": "fixed", "quota_weekly_reset_day": weeklyResetDay,
				"quota_weekly_reset_hour": 0.0, "quota_reset_timezone": "UTC",
			},
		},
	}

	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			extra := make(map[string]any, len(tc.extra)+1)
			for key, value := range tc.extra {
				extra[key] = value
			}
			extra["unrelated"] = "drop me"
			account := service.Account{
				ID: int64(46690 + i), Platform: tc.platform, Type: tc.typ, Extra: extra,
				Status: service.StatusActive, Schedulable: true,
			}

			cached := buildSchedulerMetadataAccount(account)

			require.Equal(t, tc.extra, cached.Extra)
			require.NotContains(t, cached.Extra, "unrelated")
			require.Equal(t, tc.quotaExceeded, cached.IsQuotaExceeded())
			require.Equal(t, !tc.quotaExceeded, cached.IsSchedulable())
		})
	}
}

func TestBuildSchedulerMetadataAccount_KeepsModelRateLimits(t *testing.T) {
	account := service.Account{
		ID:       90,
		Platform: service.PlatformAntigravity,
		Extra: map[string]any{
			"model_rate_limits": map[string]any{
				"gemini-3-flash": map[string]any{
					"rate_limit_reset_at": "2026-05-30T10:10:00Z",
				},
				"antigravity:gemini": map[string]any{
					"rate_limit_reset_at": "2026-05-30T10:10:00Z",
				},
			},
			"unused_large_field": "drop-me",
		},
	}

	got := buildSchedulerMetadataAccount(account)

	limits, ok := got.Extra["model_rate_limits"].(map[string]any)
	require.True(t, ok)
	require.Contains(t, limits, "gemini-3-flash")
	require.Contains(t, limits, "antigravity:gemini")
	require.Nil(t, got.Extra["unused_large_field"])
}

func TestBuildSchedulerMetadataAccount_KeepsSparkShadowRoutingIdentity(t *testing.T) {
	parentID := int64(100)
	account := service.Account{
		ID:              200,
		Platform:        service.PlatformOpenAI,
		Type:            service.AccountTypeOAuth,
		ParentAccountID: &parentID,
		QuotaDimension:  service.QuotaDimensionSpark,
		Credentials: map[string]any{
			"model_mapping": map[string]any{
				"gpt-5.3-codex-spark": "gpt-5.3-codex-spark",
			},
			"compact_model_mapping": map[string]any{
				"gpt-5.4": "gpt-5.4-openai-compact",
			},
			"access_token": "drop-me",
		},
	}

	got := buildSchedulerMetadataAccount(account)

	require.NotNil(t, got.ParentAccountID)
	require.Equal(t, parentID, *got.ParentAccountID)
	require.Equal(t, service.QuotaDimensionSpark, got.QuotaDimension)
	require.Equal(t, map[string]any{"gpt-5.3-codex-spark": "gpt-5.3-codex-spark"}, got.Credentials["model_mapping"])
	require.Equal(t, map[string]any{"gpt-5.4": "gpt-5.4-openai-compact"}, got.Credentials["compact_model_mapping"])
	require.Nil(t, got.Credentials["access_token"])
}

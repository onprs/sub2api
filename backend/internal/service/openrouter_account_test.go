package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpenRouterAccountValidation(t *testing.T) {
	credentials := map[string]any{
		"api_key":  "sk-or-v1-testkey",
		"base_url": "https://openrouter.ai/api/v1",
	}

	require.NoError(t, normalizeAndValidateOpenRouterAccount(PlatformOpenRouter, AccountTypeAPIKey, credentials))
	require.Equal(t, "https://openrouter.ai/api/v1", credentials["base_url"])

	// Empty base_url defaults to DefaultOpenRouterBaseURL
	emptyBase := map[string]any{"api_key": "sk-or-v1-testkey"}
	require.NoError(t, normalizeAndValidateOpenRouterAccount(PlatformOpenRouter, AccountTypeAPIKey, emptyBase))
	require.Equal(t, DefaultOpenRouterBaseURL, emptyBase["base_url"])

	// Rejects OAuth type
	require.Error(t, normalizeAndValidateOpenRouterAccount(PlatformOpenRouter, AccountTypeOAuth, credentials))

	// Rejects missing api_key
	require.Error(t, normalizeAndValidateOpenRouterAccount(PlatformOpenRouter, AccountTypeAPIKey, map[string]any{}))

	// Platform quota inclusion
	require.Contains(t, AllowedQuotaPlatforms, PlatformOpenRouter)
	require.True(t, IsAllowedQuotaPlatform(PlatformOpenRouter))
}

func TestOpenRouterAccountHelpers(t *testing.T) {
	acc := &Account{
		ID:          1,
		Platform:    PlatformOpenRouter,
		Type:        AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "sk-or-12345", "base_url": "https://openrouter.ai/api/v1"},
	}

	require.True(t, acc.IsOpenRouter())
	require.True(t, acc.IsOpenRouterAPIKey())
	require.Equal(t, "sk-or-12345", acc.GetOpenRouterAPIKey())
	require.Equal(t, "https://openrouter.ai/api/v1", acc.GetOpenRouterBaseURL())
}

func TestOpenRouterSchedulerSnapshotBuckets(t *testing.T) {
	svc := &SchedulerSnapshotService{}
	buckets, err := svc.defaultBuckets(context.Background())
	require.NoError(t, err)

	seen := make(map[SchedulerBucket]struct{}, len(buckets))
	for _, bucket := range buckets {
		seen[bucket] = struct{}{}
	}

	require.Contains(t, seen, SchedulerBucket{GroupID: 0, Platform: PlatformOpenRouter, Mode: SchedulerModeSingle})
	require.Contains(t, seen, SchedulerBucket{GroupID: 0, Platform: PlatformOpenRouter, Mode: SchedulerModeForced})
	require.NotContains(t, seen, SchedulerBucket{GroupID: 0, Platform: PlatformOpenRouter, Mode: SchedulerModeMixed})
}

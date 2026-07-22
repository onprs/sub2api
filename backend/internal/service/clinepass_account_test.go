package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClinePassAccountContract(t *testing.T) {
	account := &Account{Platform: PlatformClinePass, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "cp-key"}}
	require.True(t, account.IsClinePass())
	require.True(t, account.IsClinePassAPIKey())
	require.Equal(t, "cp-key", account.GetClinePassAPIKey())
	require.Equal(t, DefaultClinePassBaseURL, account.GetClinePassBaseURL())
	require.Contains(t, AllowedQuotaPlatforms, PlatformClinePass)
	require.True(t, IsAllowedQuotaPlatform(PlatformClinePass))

	credentials := map[string]any{"api_key": "cp-key"}
	require.NoError(t, normalizeAndValidateClinePassAccount(PlatformClinePass, AccountTypeAPIKey, credentials))
	require.Equal(t, DefaultClinePassBaseURL, credentials["base_url"])
	require.Error(t, normalizeAndValidateClinePassAccount(PlatformClinePass, AccountTypeOAuth, credentials))
	require.Error(t, normalizeAndValidateClinePassAccount(PlatformClinePass, AccountTypeAPIKey, map[string]any{}))
}

func TestSchedulerSnapshotDefaultBucketsIncludeClinePass(t *testing.T) {
	svc := NewSchedulerSnapshotService(nil, nil, nil, nil, nil)
	buckets, err := svc.defaultBuckets(context.Background())
	require.NoError(t, err)

	seen := make(map[SchedulerBucket]struct{}, len(buckets))
	for _, bucket := range buckets {
		seen[bucket] = struct{}{}
	}
	require.Contains(t, seen, SchedulerBucket{GroupID: 0, Platform: PlatformClinePass, Mode: SchedulerModeSingle})
	require.Contains(t, seen, SchedulerBucket{GroupID: 0, Platform: PlatformClinePass, Mode: SchedulerModeForced})
	require.NotContains(t, seen, SchedulerBucket{GroupID: 0, Platform: PlatformClinePass, Mode: SchedulerModeMixed})
}

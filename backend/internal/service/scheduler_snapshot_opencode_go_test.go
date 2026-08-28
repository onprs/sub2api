//go:build unit

package service

import (
	"context"
	"testing"
	"time"
)

func TestSchedulerSnapshotDefaultBucketsIncludeOpenCodeGoSingleAndForced(t *testing.T) {
	svc := NewSchedulerSnapshotService(nil, nil, nil, nil, nil)

	buckets, err := svc.defaultBuckets(context.Background())
	if err != nil {
		t.Fatalf("defaultBuckets error: %v", err)
	}

	want := map[SchedulerBucket]bool{
		{GroupID: 0, Platform: PlatformOpenCodeGo, Mode: SchedulerModeSingle}: true,
		{GroupID: 0, Platform: PlatformOpenCodeGo, Mode: SchedulerModeForced}: true,
		{GroupID: 0, Platform: PlatformOpenCodeGo, Mode: SchedulerModeMixed}:  false,
	}

	seen := make(map[SchedulerBucket]struct{}, len(buckets))
	for _, bucket := range buckets {
		seen[bucket] = struct{}{}
	}
	for bucket, shouldExist := range want {
		_, exists := seen[bucket]
		if shouldExist && !exists {
			t.Fatalf("expected default bucket %#v", bucket)
		}
		if !shouldExist && exists {
			t.Fatalf("did not expect mixed OpenCode Go bucket %#v", bucket)
		}
	}
}

type schedulerOpenCodeGoBucketCache struct {
	buckets []SchedulerBucket
}

func (c *schedulerOpenCodeGoBucketCache) GetSnapshot(context.Context, SchedulerBucket) ([]*Account, bool, error) {
	return nil, false, nil
}

func (c *schedulerOpenCodeGoBucketCache) SetSnapshot(_ context.Context, bucket SchedulerBucket, _ SchedulerBucketWriteToken, _ []Account) error {
	c.buckets = append(c.buckets, bucket)
	return nil
}

func (c *schedulerOpenCodeGoBucketCache) CaptureBucketWriteToken(_ context.Context, bucket SchedulerBucket) (SchedulerBucketWriteToken, error) {
	return SchedulerBucketWriteToken{Bucket: bucket, Epoch: 1}, nil
}

func (c *schedulerOpenCodeGoBucketCache) RetireBucket(context.Context, SchedulerBucket) error {
	return nil
}

func (c *schedulerOpenCodeGoBucketCache) ReopenBucket(context.Context, SchedulerBucket) (SchedulerBucketWriteToken, error) {
	return SchedulerBucketWriteToken{Epoch: 1}, nil
}

func (c *schedulerOpenCodeGoBucketCache) TryAcquireGroupLifecycleLease(context.Context, int64, time.Duration) (SchedulerGroupLifecycleLease, bool, error) {
	return SchedulerGroupLifecycleLease{}, true, nil
}

func (c *schedulerOpenCodeGoBucketCache) ReleaseGroupLifecycleLease(context.Context, SchedulerGroupLifecycleLease) error {
	return nil
}

func (c *schedulerOpenCodeGoBucketCache) GetAccount(context.Context, int64) (*Account, error) {
	return nil, nil
}

func (c *schedulerOpenCodeGoBucketCache) SetAccount(context.Context, *Account) error {
	return nil
}

func (c *schedulerOpenCodeGoBucketCache) DeleteAccount(context.Context, int64) error {
	return nil
}

func (c *schedulerOpenCodeGoBucketCache) UpdateLastUsed(context.Context, map[int64]time.Time) error {
	return nil
}

func (c *schedulerOpenCodeGoBucketCache) TryLockBucket(context.Context, SchedulerBucket, time.Duration) (bool, error) {
	return true, nil
}

func (c *schedulerOpenCodeGoBucketCache) UnlockBucket(context.Context, SchedulerBucket) error {
	return nil
}

func (c *schedulerOpenCodeGoBucketCache) ListBuckets(context.Context) ([]SchedulerBucket, error) {
	return nil, nil
}

func (c *schedulerOpenCodeGoBucketCache) GetOutboxWatermark(context.Context) (int64, error) {
	return 0, nil
}

func (c *schedulerOpenCodeGoBucketCache) SetOutboxWatermark(context.Context, int64) error {
	return nil
}

type schedulerOpenCodeGoAccountRepo struct {
	accountRepoStub
}

func (r *schedulerOpenCodeGoAccountRepo) ListSchedulableByGroupIDAndPlatform(context.Context, int64, string) ([]Account, error) {
	return []Account{}, nil
}

func (r *schedulerOpenCodeGoAccountRepo) ListSchedulableByGroupIDAndPlatforms(context.Context, int64, []string) ([]Account, error) {
	return []Account{}, nil
}

func TestSchedulerSnapshotGroupRebuildIncludesOpenCodeGoSingleAndForced(t *testing.T) {
	cache := &schedulerOpenCodeGoBucketCache{}
	svc := NewSchedulerSnapshotService(cache, nil, &schedulerOpenCodeGoAccountRepo{}, nil, nil)

	if err := svc.rebuildByGroupIDs(context.Background(), []int64{123}, "test", nil); err != nil {
		t.Fatalf("rebuildByGroupIDs error: %v", err)
	}

	seen := make(map[SchedulerBucket]struct{}, len(cache.buckets))
	for _, bucket := range cache.buckets {
		seen[bucket] = struct{}{}
	}

	for _, bucket := range []SchedulerBucket{
		{GroupID: 123, Platform: PlatformOpenCodeGo, Mode: SchedulerModeSingle},
		{GroupID: 123, Platform: PlatformOpenCodeGo, Mode: SchedulerModeForced},
	} {
		if _, exists := seen[bucket]; !exists {
			t.Fatalf("expected rebuild bucket %#v; got %#v", bucket, cache.buckets)
		}
	}
	if _, exists := seen[SchedulerBucket{GroupID: 123, Platform: PlatformOpenCodeGo, Mode: SchedulerModeMixed}]; exists {
		t.Fatalf("did not expect mixed OpenCode Go rebuild bucket; got %#v", cache.buckets)
	}
}

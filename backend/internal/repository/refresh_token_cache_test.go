//go:build unit

package repository

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func newRefreshTokenTestCache(t *testing.T) (*refreshTokenCache, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	return &refreshTokenCache{rdb: rdb}, mr
}

func sampleRefreshTokenData(familyID string) *service.RefreshTokenData {
	return &service.RefreshTokenData{
		UserID:       7,
		TokenVersion: 1,
		FamilyID:     familyID,
		CreatedAt:    time.Now(),
		ExpiresAt:    time.Now().Add(time.Hour),
	}
}

// ConsumeRefreshToken must atomically observe a stored token exactly once: the
// first consumer wins, any follow-up consumer (incl. a concurrent one) misses.
func TestRefreshTokenCache_ConsumeRefreshToken_AtomicallyConsumesOnce(t *testing.T) {
	cache, _ := newRefreshTokenTestCache(t)
	ctx := context.Background()
	const tokenHash = "hash-abc"

	require.NoError(t, cache.StoreRefreshToken(ctx, tokenHash, sampleRefreshTokenData("fam-1"), time.Hour))

	data, consumed, err := cache.ConsumeRefreshToken(ctx, tokenHash)
	require.NoError(t, err)
	require.True(t, consumed)
	require.Equal(t, int64(7), data.UserID)
	require.Equal(t, "fam-1", data.FamilyID)

	// Subsequent consume reports a miss (token already consumed).
	data2, consumed2, err2 := cache.ConsumeRefreshToken(ctx, tokenHash)
	require.NoError(t, err2)
	require.False(t, consumed2)
	require.Nil(t, data2)
}

// Many goroutines racing to consume the SAME token must allow exactly one
// success — the core atomicity guarantee that blocks refresh-token replay.
func TestRefreshTokenCache_ConsumeRefreshToken_ConcurrentOnlyOneSucceeds(t *testing.T) {
	cache, _ := newRefreshTokenTestCache(t)
	ctx := context.Background()
	const tokenHash = "hash-race"

	require.NoError(t, cache.StoreRefreshToken(ctx, tokenHash, sampleRefreshTokenData("fam-race"), time.Hour))

	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)
	var succeeded int32
	var failed int32
	var errored int32
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			_, consumed, err := cache.ConsumeRefreshToken(ctx, tokenHash)
			switch {
			case err != nil:
				atomic.AddInt32(&errored, 1)
			case consumed:
				atomic.AddInt32(&succeeded, 1)
			default:
				atomic.AddInt32(&failed, 1)
			}
		}()
	}
	wg.Wait()

	require.Equal(t, int32(1), succeeded, "exactly one concurrent consumer should win")
	require.Equal(t, int32(n-1), failed, "all other consumers should observe a miss")
	require.Equal(t, int32(0), errored, "no consumer should error")
}

func TestRefreshTokenCache_ConsumeRefreshToken_NotFound(t *testing.T) {
	cache, _ := newRefreshTokenTestCache(t)
	ctx := context.Background()

	data, consumed, err := cache.ConsumeRefreshToken(ctx, "absent")
	require.NoError(t, err)
	require.False(t, consumed)
	require.Nil(t, data)
}

// StoreRefreshToken must also persist the family reverse-index entry that the
// replay-revocation path relies on once a token has already been consumed.
func TestRefreshTokenCache_StoreWritesFamilyReverseIndex(t *testing.T) {
	cache, _ := newRefreshTokenTestCache(t)
	ctx := context.Background()
	const tokenHash = "hash-fam-idx"

	require.NoError(t, cache.StoreRefreshToken(ctx, tokenHash, sampleRefreshTokenData("fam-9"), time.Hour))

	familyID, err := cache.GetRefreshTokenFamilyID(ctx, tokenHash)
	require.NoError(t, err)
	require.Equal(t, "fam-9", familyID)
}

func TestRefreshTokenCache_GetRefreshTokenFamilyID_NotFoundReturnsEmpty(t *testing.T) {
	cache, _ := newRefreshTokenTestCache(t)
	ctx := context.Background()

	familyID, err := cache.GetRefreshTokenFamilyID(ctx, "absent")
	require.NoError(t, err)
	require.Equal(t, "", familyID)
}

// DeleteTokenFamily must remove both the token payloads and their
// reverse-index entries, so a replay after revocation no longer resolves a
// family (the family is gone) and cannot be consumed again.
func TestRefreshTokenCache_DeleteTokenFamily_RemovesPayloadAndReverseIndex(t *testing.T) {
	cache, _ := newRefreshTokenTestCache(t)
	ctx := context.Background()
	const (
		tokenHash = "hash-fam-del"
		familyID  = "fam-del"
	)

	require.NoError(t, cache.StoreRefreshToken(ctx, tokenHash, sampleRefreshTokenData(familyID), time.Hour))
	require.NoError(t, cache.AddToFamilyTokenSet(ctx, familyID, tokenHash, time.Hour))

	require.NoError(t, cache.DeleteTokenFamily(ctx, familyID))

	_, consumed, err := cache.ConsumeRefreshToken(ctx, tokenHash)
	require.NoError(t, err)
	require.False(t, consumed, "token payload must be gone after family revocation")

	familyID2, err := cache.GetRefreshTokenFamilyID(ctx, tokenHash)
	require.NoError(t, err)
	require.Equal(t, "", familyID2, "reverse-index entry must be removed")
}

// ConsumeRefreshToken must surface operational failures (not mask them as a
// miss), so the auth service can fail closed with ErrServiceUnavailable.
func TestRefreshTokenCache_ConsumeRefreshToken_LuaResultShapeIsStable(t *testing.T) {
	cache, mr := newRefreshTokenTestCache(t)
	ctx := context.Background()
	const tokenHash = "hash-shape"

	require.NoError(t, cache.StoreRefreshToken(ctx, tokenHash, sampleRefreshTokenData("fam-shape"), time.Hour))

	// Closing the underlying miniredis forces the script run to error.
	mr.Close()

	_, _, err := cache.ConsumeRefreshToken(ctx, tokenHash)
	require.Error(t, err)
}

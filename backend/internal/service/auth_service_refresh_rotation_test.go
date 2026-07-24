//go:build unit

package service_test

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/enttest"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "modernc.org/sqlite"
)

// refreshRotationCacheStub embeds the shared in-memory refresh-token stub and
// instruments DeleteTokenFamily so tests can assert that replay revocation ran.
type refreshRotationCacheStub struct {
	*emailBindRefreshTokenCacheStub
	deleteFamilyCalls int64
}

func newRefreshRotationCacheStub() *refreshRotationCacheStub {
	return &refreshRotationCacheStub{emailBindRefreshTokenCacheStub: newEmailBindRefreshTokenCacheStub()}
}

// DeleteTokenFamily shadows the promoted method to count revocations
// transparently for every other call (Store/Consume/AddToFamily still go to the
// embedded stubber) so the test asserts the family got pulled down on replay.
func (s *refreshRotationCacheStub) DeleteTokenFamily(ctx context.Context, familyID string) error {
	atomic.AddInt64(&s.deleteFamilyCalls, 1)
	return s.emailBindRefreshTokenCacheStub.DeleteTokenFamily(ctx, familyID)
}

func newAuthServiceForRefreshRotationTest(t *testing.T, cache service.RefreshTokenCache) (*service.AuthService, service.UserRepository, *dbent.Client) {
	t.Helper()

	db, err := sql.Open("sqlite", "file:refresh_rotation?mode=memory&cache=shared")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	_, err = db.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)
	_, err = db.Exec(`
CREATE TABLE IF NOT EXISTS user_provider_default_grants (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id INTEGER NOT NULL,
	provider_type TEXT NOT NULL,
	grant_reason TEXT NOT NULL DEFAULT 'first_bind',
	created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	UNIQUE(user_id, provider_type, grant_reason)
)`)
	require.NoError(t, err)

	drv := entsql.OpenDB(dialect.SQLite, db)
	client := enttest.NewClient(t, enttest.WithOptions(dbent.Driver(drv)))
	t.Cleanup(func() { _ = client.Close() })

	repo := repository.NewUserRepository(client, db)
	cfg := &config.Config{
		JWT: config.JWTConfig{
			Secret:                 "refresh-rotation-test-secret",
			ExpireHour:             1,
			RefreshTokenExpireDays: 30,
		},
	}

	svc := service.NewAuthService(client, repo, nil, cache, cfg, nil, nil, nil, nil, nil, nil, nil, nil)
	return svc, repo, client
}

func refreshRotationCreateUser(t *testing.T, ctx context.Context, client *dbent.Client, email string) *dbent.User {
	t.Helper()
	user, err := client.User.Create().
		SetEmail(email).
		SetPasswordHash("rotation-hash").
		SetRole(service.RoleUser).
		SetStatus(service.StatusActive).
		Save(ctx)
	require.NoError(t, err)
	return user
}

// A single refresh rotates the token and the old refresh token can no longer be
// reused. Replaying the consumed token pulls down the whole family, so the
// freshly issued descendant token is also rejected on a second replay.
func TestAuthService_RefreshTokenPair_RotatesTokenAndRevokesFamilyOnReplay(t *testing.T) {
	cache := newRefreshRotationCacheStub()
	svc, repo, client := newAuthServiceForRefreshRotationTest(t, cache)
	ctx := context.Background()

	dbUser := refreshRotationCreateUser(t, ctx, client, "rotation@example.com")
	user, err := repo.GetByID(ctx, dbUser.ID)
	require.NoError(t, err)

	pair, err := svc.GenerateTokenPair(ctx, user, "")
	require.NoError(t, err)
	t0 := pair.RefreshToken
	require.NotEmpty(t, t0)

	// First refresh: succeeds and issues a NEW, distinct refresh token.
	refreshed, err := svc.RefreshTokenPair(ctx, t0)
	require.NoError(t, err)
	require.NotEmpty(t, refreshed.RefreshToken)
	require.NotEqual(t, t0, refreshed.RefreshToken)
	require.Equal(t, int64(0), atomic.LoadInt64(&cache.deleteFamilyCalls),
		"happy rotation must not revoke the family")

	t1 := refreshed.RefreshToken

	// Replay of the original (consumed) token: invalid, and triggers family revoke.
	_, replayErr := svc.RefreshTokenPair(ctx, t0)
	require.ErrorIs(t, replayErr, service.ErrRefreshTokenInvalid)
	require.GreaterOrEqual(t, atomic.LoadInt64(&cache.deleteFamilyCalls), int64(1),
		"replay must revoke the token family")

	// The descendant issued moments ago belongs to the revoked family, so it is
	// also rejected — demonstrating the family revocation kills the session chain.
	_, replayErr2 := svc.RefreshTokenPair(ctx, t1)
	require.ErrorIs(t, replayErr2, service.ErrRefreshTokenInvalid)
}

// Two concurrent refreshes of the SAME token: atomic consume allows exactly one
// to succeed; the other misses, is rejected as invalid and revokes the family.
func TestAuthService_RefreshTokenPair_ConcurrentReplayRevokesFamily(t *testing.T) {
	cache := newRefreshRotationCacheStub()
	svc, repo, client := newAuthServiceForRefreshRotationTest(t, cache)
	ctx := context.Background()

	dbUser := refreshRotationCreateUser(t, ctx, client, "concurrent-replay@example.com")
	user, err := repo.GetByID(ctx, dbUser.ID)
	require.NoError(t, err)

	pair, err := svc.GenerateTokenPair(ctx, user, "")
	require.NoError(t, err)
	t0 := pair.RefreshToken

	var wg sync.WaitGroup
	const workers = 2
	wg.Add(workers)
	var succeeded, failed int64
	var firstMu sync.Mutex
	var firstRefreshToken string
	start := make(chan struct{})
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			<-start
			out, err := svc.RefreshTokenPair(ctx, t0)
			if err == nil {
				switch atomic.AddInt64(&succeeded, 1) {
				case 1:
					firstMu.Lock()
					firstRefreshToken = out.RefreshToken
					firstMu.Unlock()
				}
			} else if errors.Is(err, service.ErrRefreshTokenInvalid) {
				atomic.AddInt64(&failed, 1)
			}
		}()
	}
	close(start)
	wg.Wait()

	require.Equal(t, int64(1), succeeded, "exactly one concurrent refresh should win")
	require.Equal(t, int64(1), failed, "the other concurrent refresh must be rejected as a replay")
	require.NotEmpty(t, firstRefreshToken, "the winning refresh should issue a new token")
	require.GreaterOrEqual(t, atomic.LoadInt64(&cache.deleteFamilyCalls), int64(1),
		"the replayed (losing) refresh must revoke the family")
}

package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

const (
	refreshTokenKeyPrefix       = "refresh_token:"
	userRefreshTokensPrefix     = "user_refresh_tokens:"
	tokenFamilyPrefix           = "token_family:"
	rRefreshTokenFamilyOfPrefix = "refresh_token_family_of:"
)

// consumeRefreshTokenScript atomically GET+DEL a refresh token key in a single
// Redis EVAL. Lua semantics: returning `false` maps to a nil reply, `true` to
// the integer 1, so callers receive {nil, ""} on miss and {int64(1), value} on
// hit. This guarantees only one concurrent caller can observe a consumed token.
var consumeRefreshTokenScript = redis.NewScript(`
local v = redis.call('GET', KEYS[1])
if v == false then return {false, ''} end
redis.call('DEL', KEYS[1])
return {true, v}
`)

// refreshTokenKey generates the Redis key for a refresh token.
func refreshTokenKey(tokenHash string) string {
	return refreshTokenKeyPrefix + tokenHash
}

// refreshTokenFamilyOfKey generates the reverse-index key mapping a token hash
// to its family id. It is written at store time and re-read on a consume miss
// so that a replay attempt can revoke the right family even though the token
// payload has already been consumed.
func refreshTokenFamilyOfKey(tokenHash string) string {
	return rRefreshTokenFamilyOfPrefix + tokenHash
}

// userRefreshTokensKey generates the Redis key for user's token set.
func userRefreshTokensKey(userID int64) string {
	return fmt.Sprintf("%s%d", userRefreshTokensPrefix, userID)
}

// tokenFamilyKey generates the Redis key for token family set.
func tokenFamilyKey(familyID string) string {
	return tokenFamilyPrefix + familyID
}

type refreshTokenCache struct {
	rdb *redis.Client
}

// NewRefreshTokenCache creates a new RefreshTokenCache implementation.
func NewRefreshTokenCache(rdb *redis.Client) service.RefreshTokenCache {
	return &refreshTokenCache{rdb: rdb}
}

func (c *refreshTokenCache) StoreRefreshToken(ctx context.Context, tokenHash string, data *service.RefreshTokenData, ttl time.Duration) error {
	key := refreshTokenKey(tokenHash)
	val, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshal refresh token data: %w", err)
	}
	// Persist the payload together with the family reverse-index entry so a
	// later replay (after the token is consumed) can still resolve the family.
	familyOfKey := refreshTokenFamilyOfKey(tokenHash)
	pipe := c.rdb.Pipeline()
	pipe.Set(ctx, key, val, ttl)
	pipe.Set(ctx, familyOfKey, data.FamilyID, ttl)
	_, err = pipe.Exec(ctx)
	return err
}

func (c *refreshTokenCache) GetRefreshToken(ctx context.Context, tokenHash string) (*service.RefreshTokenData, error) {
	key := refreshTokenKey(tokenHash)
	val, err := c.rdb.Get(ctx, key).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, service.ErrRefreshTokenNotFound
		}
		return nil, err
	}
	var data service.RefreshTokenData
	if err := json.Unmarshal([]byte(val), &data); err != nil {
		return nil, fmt.Errorf("unmarshal refresh token data: %w", err)
	}
	return &data, nil
}

// ConsumeRefreshToken atomically reads and deletes a refresh token. It returns
// (data, true, nil) on hit and (nil, false, nil) on miss. Operational failures
// are surfaced as errors so the caller can fail closed with ErrServiceUnavailable.
func (c *refreshTokenCache) ConsumeRefreshToken(ctx context.Context, tokenHash string) (*service.RefreshTokenData, bool, error) {
	key := refreshTokenKey(tokenHash)
	res, err := consumeRefreshTokenScript.Run(ctx, c.rdb, []string{key}).Result()
	if err != nil {
		return nil, false, err
	}
	arr, ok := res.([]any)
	if !ok || len(arr) < 2 {
		return nil, false, fmt.Errorf("unexpected refresh token consume script result: %v", res)
	}
	// Lua `false` -> nil element; `true` -> int64(1). Treat nil and 0 as miss.
	switch first := arr[0].(type) {
	case nil:
		return nil, false, nil
	case int64:
		if first == 0 {
			return nil, false, nil
		}
	}
	val, ok := arr[1].(string)
	if !ok {
		return nil, false, fmt.Errorf("unexpected refresh token consume script value type: %T", arr[1])
	}
	var data service.RefreshTokenData
	if err := json.Unmarshal([]byte(val), &data); err != nil {
		return nil, false, fmt.Errorf("unmarshal refresh token data: %w", err)
	}
	return &data, true, nil
}

func (c *refreshTokenCache) GetRefreshTokenFamilyID(ctx context.Context, tokenHash string) (string, error) {
	key := refreshTokenFamilyOfKey(tokenHash)
	val, err := c.rdb.Get(ctx, key).Result()
	if err != nil {
		if err == redis.Nil {
			return "", nil
		}
		return "", err
	}
	return val, nil
}

func (c *refreshTokenCache) DeleteRefreshToken(ctx context.Context, tokenHash string) error {
	key := refreshTokenKey(tokenHash)
	familyOfKey := refreshTokenFamilyOfKey(tokenHash)
	pipe := c.rdb.Pipeline()
	pipe.Del(ctx, key)
	pipe.Del(ctx, familyOfKey)
	_, err := pipe.Exec(ctx)
	return err
}

func (c *refreshTokenCache) DeleteUserRefreshTokens(ctx context.Context, userID int64) error {
	// Get all token hashes for this user
	tokenHashes, err := c.GetUserTokenHashes(ctx, userID)
	if err != nil && err != redis.Nil {
		return fmt.Errorf("get user token hashes: %w", err)
	}

	if len(tokenHashes) == 0 {
		return nil
	}

	// Build keys to delete
	keys := make([]string, 0, 2*len(tokenHashes)+1)
	for _, hash := range tokenHashes {
		keys = append(keys, refreshTokenKey(hash), refreshTokenFamilyOfKey(hash))
	}
	keys = append(keys, userRefreshTokensKey(userID))

	// Delete all keys in a pipeline
	pipe := c.rdb.Pipeline()
	for _, key := range keys {
		pipe.Del(ctx, key)
	}
	_, err = pipe.Exec(ctx)
	return err
}

func (c *refreshTokenCache) DeleteTokenFamily(ctx context.Context, familyID string) error {
	// Get all token hashes in this family
	tokenHashes, err := c.GetFamilyTokenHashes(ctx, familyID)
	if err != nil && err != redis.Nil {
		return fmt.Errorf("get family token hashes: %w", err)
	}

	if len(tokenHashes) == 0 {
		return nil
	}

	// Build keys to delete
	keys := make([]string, 0, 2*len(tokenHashes)+1)
	for _, hash := range tokenHashes {
		keys = append(keys, refreshTokenKey(hash), refreshTokenFamilyOfKey(hash))
	}
	keys = append(keys, tokenFamilyKey(familyID))

	// Delete all keys in a pipeline
	pipe := c.rdb.Pipeline()
	for _, key := range keys {
		pipe.Del(ctx, key)
	}
	_, err = pipe.Exec(ctx)
	return err
}

func (c *refreshTokenCache) AddToUserTokenSet(ctx context.Context, userID int64, tokenHash string, ttl time.Duration) error {
	key := userRefreshTokensKey(userID)
	pipe := c.rdb.Pipeline()
	pipe.SAdd(ctx, key, tokenHash)
	pipe.Expire(ctx, key, ttl)
	_, err := pipe.Exec(ctx)
	return err
}

func (c *refreshTokenCache) AddToFamilyTokenSet(ctx context.Context, familyID string, tokenHash string, ttl time.Duration) error {
	key := tokenFamilyKey(familyID)
	pipe := c.rdb.Pipeline()
	pipe.SAdd(ctx, key, tokenHash)
	pipe.Expire(ctx, key, ttl)
	_, err := pipe.Exec(ctx)
	return err
}

func (c *refreshTokenCache) GetUserTokenHashes(ctx context.Context, userID int64) ([]string, error) {
	key := userRefreshTokensKey(userID)
	return c.rdb.SMembers(ctx, key).Result()
}

func (c *refreshTokenCache) GetFamilyTokenHashes(ctx context.Context, familyID string) ([]string, error) {
	key := tokenFamilyKey(familyID)
	return c.rdb.SMembers(ctx, key).Result()
}

func (c *refreshTokenCache) IsTokenInFamily(ctx context.Context, familyID string, tokenHash string) (bool, error) {
	key := tokenFamilyKey(familyID)
	return c.rdb.SIsMember(ctx, key, tokenHash).Result()
}

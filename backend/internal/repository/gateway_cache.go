package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

const stickySessionPrefix = "sticky_session:"

const (
	apiKeyRoutingStickyPrefix = "api_key_routing_sticky:"
	apiKeyRoutingHealthPrefix = "api_key_routing_health:"
	apiKeyRoutingHealthBucket = 5 * time.Minute
	apiKeyRoutingHealthTTL    = 40 * time.Minute
)

type gatewayCache struct {
	rdb *redis.Client
}

func NewGatewayCache(rdb *redis.Client) service.GatewayCache {
	return &gatewayCache{rdb: rdb}
}

// buildSessionKey 构建 session key，包含 groupID 实现分组隔离
// 格式: sticky_session:{groupID}:{sessionHash}
func buildSessionKey(groupID int64, sessionHash string) string {
	return fmt.Sprintf("%s%d:%s", stickySessionPrefix, groupID, sessionHash)
}

func (c *gatewayCache) GetSessionAccountID(ctx context.Context, groupID int64, sessionHash string) (int64, error) {
	key := buildSessionKey(groupID, sessionHash)
	return c.rdb.Get(ctx, key).Int64()
}

func (c *gatewayCache) SetSessionAccountID(ctx context.Context, groupID int64, sessionHash string, accountID int64, ttl time.Duration) error {
	key := buildSessionKey(groupID, sessionHash)
	return c.rdb.Set(ctx, key, accountID, ttl).Err()
}

func (c *gatewayCache) RefreshSessionTTL(ctx context.Context, groupID int64, sessionHash string, ttl time.Duration) error {
	key := buildSessionKey(groupID, sessionHash)
	return c.rdb.Expire(ctx, key, ttl).Err()
}

// DeleteSessionAccountID 删除粘性会话与账号的绑定关系。
// 当检测到绑定的账号不可用（如状态错误、禁用、不可调度等）时调用，
// 以便下次请求能够重新选择可用账号。
//
// DeleteSessionAccountID removes the sticky session binding for the given session.
// Called when the bound account becomes unavailable (e.g., error status, disabled,
// or unschedulable), allowing subsequent requests to select a new available account.
func (c *gatewayCache) DeleteSessionAccountID(ctx context.Context, groupID int64, sessionHash string) error {
	key := buildSessionKey(groupID, sessionHash)
	return c.rdb.Del(ctx, key).Err()
}

func apiKeyRoutingStickyKey(apiKeyID int64, sessionKey string) string {
	hash := sha256.Sum256([]byte(sessionKey))
	return fmt.Sprintf("%s%d:%s", apiKeyRoutingStickyPrefix, apiKeyID, hex.EncodeToString(hash[:]))
}

func (c *gatewayCache) GetAPIKeyRoutingGroupID(ctx context.Context, apiKeyID int64, sessionKey string) (int64, error) {
	return c.rdb.Get(ctx, apiKeyRoutingStickyKey(apiKeyID, sessionKey)).Int64()
}

func (c *gatewayCache) SetAPIKeyRoutingGroupID(ctx context.Context, apiKeyID int64, sessionKey string, groupID int64, ttl time.Duration) error {
	return c.rdb.Set(ctx, apiKeyRoutingStickyKey(apiKeyID, sessionKey), groupID, ttl).Err()
}

func apiKeyRoutingHealthKey(groupID int64, at time.Time) string {
	bucket := at.UTC().Truncate(apiKeyRoutingHealthBucket).Unix()
	return fmt.Sprintf("%s%d:%d", apiKeyRoutingHealthPrefix, groupID, bucket)
}

func (c *gatewayCache) GetAPIKeyRoutingHealth(ctx context.Context, groupID int64, since time.Time) (service.APIKeyRoutingHealth, error) {
	health := service.APIKeyRoutingHealth{}
	if c == nil || c.rdb == nil || groupID <= 0 {
		return health, nil
	}
	start := since.UTC().Truncate(apiKeyRoutingHealthBucket)
	end := time.Now().UTC().Truncate(apiKeyRoutingHealthBucket)
	pipe := c.rdb.Pipeline()
	commands := make([]*redis.SliceCmd, 0, 8)
	for bucket := start; !bucket.After(end); bucket = bucket.Add(apiKeyRoutingHealthBucket) {
		commands = append(commands, pipe.HMGet(ctx, apiKeyRoutingHealthKey(groupID, bucket), "success", "failure"))
	}
	if _, err := pipe.Exec(ctx); err != nil && err != redis.Nil {
		return health, err
	}
	for _, command := range commands {
		values, err := command.Result()
		if err != nil && err != redis.Nil {
			return health, err
		}
		if len(values) > 0 && values[0] != nil {
			value, _ := strconv.ParseInt(fmt.Sprint(values[0]), 10, 64)
			health.Success += value
		}
		if len(values) > 1 && values[1] != nil {
			value, _ := strconv.ParseInt(fmt.Sprint(values[1]), 10, 64)
			health.Failure += value
		}
	}
	return health, nil
}

func (c *gatewayCache) RecordAPIKeyRoutingOutcome(ctx context.Context, groupID int64, success bool, at time.Time) error {
	if c == nil || c.rdb == nil || groupID <= 0 {
		return nil
	}
	field := "failure"
	if success {
		field = "success"
	}
	key := apiKeyRoutingHealthKey(groupID, at)
	pipe := c.rdb.TxPipeline()
	pipe.HIncrBy(ctx, key, field, 1)
	pipe.Expire(ctx, key, apiKeyRoutingHealthTTL)
	_, err := pipe.Exec(ctx)
	return err
}

// Compile-time assertion: gatewayCache must implement CyberSessionBlockStore.
var _ service.CyberSessionBlockStore = (*gatewayCache)(nil)

const cyberSessionBlockPrefix = "cyber_session_block:"

// SetCyberSessionBlocked 把被 cyber_policy 命中的会话写入屏蔽表（TTL 自动过期）。
// 存储值 "1" 作为存在标记（IsCyberSessionBlocked 只检查 key 是否存在，不读值）。
func (c *gatewayCache) SetCyberSessionBlocked(ctx context.Context, key string, ttl time.Duration) error {
	return c.rdb.Set(ctx, cyberSessionBlockPrefix+key, "1", ttl).Err()
}

// IsCyberSessionBlocked 查询会话是否在屏蔽表中。
func (c *gatewayCache) IsCyberSessionBlocked(ctx context.Context, key string) (bool, error) {
	n, err := c.rdb.Exists(ctx, cyberSessionBlockPrefix+key).Result()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

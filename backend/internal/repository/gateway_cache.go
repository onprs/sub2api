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

// apiKeyRoutingHealthRecordScript 在一次原子更新中维护计数与最近结果。
// 时间戳判断可避免较慢的 Redis 写入覆盖更新请求的结果。
var apiKeyRoutingHealthRecordScript = redis.NewScript(`
	local key = KEYS[1]
	local success = tonumber(ARGV[1])
	local latency_ms = tonumber(ARGV[2])
	local observed_at_ms = tonumber(ARGV[3])
	local ttl_seconds = tonumber(ARGV[4])

	if success == 1 then
		redis.call('HINCRBY', key, 'success', 1)
	else
		redis.call('HINCRBY', key, 'failure', 1)
	end
	if latency_ms >= 0 then
		redis.call('HINCRBY', key, 'latency_total_ms', latency_ms)
		redis.call('HINCRBY', key, 'latency_samples', 1)
	end

	local previous_observed_at_ms = tonumber(redis.call('HGET', key, 'last_observed_at_ms'))
	if previous_observed_at_ms == nil or observed_at_ms >= previous_observed_at_ms then
		redis.call('HSET', key,
			'last_success', success,
			'last_observed_at_ms', observed_at_ms)
	end
	redis.call('EXPIRE', key, ttl_seconds)
	return 1
`)

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
	healthByID, err := c.GetAPIKeyRoutingHealthBatch(ctx, []int64{groupID}, since)
	if err != nil {
		return service.APIKeyRoutingHealth{}, err
	}
	return healthByID[groupID], nil
}

type apiKeyRoutingHealthCommand struct {
	groupID int64
	command *redis.SliceCmd
}

func (c *gatewayCache) GetAPIKeyRoutingHealthBatch(ctx context.Context, groupIDs []int64, since time.Time) (map[int64]service.APIKeyRoutingHealth, error) {
	healthByID := make(map[int64]service.APIKeyRoutingHealth, len(groupIDs))
	if c == nil || c.rdb == nil {
		return healthByID, nil
	}
	start := since.UTC().Truncate(apiKeyRoutingHealthBucket)
	end := time.Now().UTC().Truncate(apiKeyRoutingHealthBucket)
	pipe := c.rdb.Pipeline()
	commands := make([]apiKeyRoutingHealthCommand, 0, len(groupIDs)*8)
	seen := make(map[int64]struct{}, len(groupIDs))
	for _, groupID := range groupIDs {
		if groupID <= 0 {
			continue
		}
		if _, exists := seen[groupID]; exists {
			continue
		}
		seen[groupID] = struct{}{}
		healthByID[groupID] = service.APIKeyRoutingHealth{}
		for bucket := start; !bucket.After(end); bucket = bucket.Add(apiKeyRoutingHealthBucket) {
			commands = append(commands, apiKeyRoutingHealthCommand{
				groupID: groupID,
				command: pipe.HMGet(
					ctx,
					apiKeyRoutingHealthKey(groupID, bucket),
					"success",
					"failure",
					"latency_total_ms",
					"latency_samples",
					"last_success",
					"last_observed_at_ms",
				),
			})
		}
	}
	if len(commands) == 0 {
		return healthByID, nil
	}
	if _, err := pipe.Exec(ctx); err != nil && err != redis.Nil {
		return healthByID, err
	}
	for _, item := range commands {
		values, err := item.command.Result()
		if err != nil && err != redis.Nil {
			return healthByID, err
		}
		health := healthByID[item.groupID]
		mergeAPIKeyRoutingHealthValues(&health, values)
		healthByID[item.groupID] = health
	}
	return healthByID, nil
}

func mergeAPIKeyRoutingHealthValues(health *service.APIKeyRoutingHealth, values []any) {
	if health == nil {
		return
	}
	if value, ok := apiKeyRoutingHealthInt(values, 0); ok {
		health.Success += value
	}
	if value, ok := apiKeyRoutingHealthInt(values, 1); ok {
		health.Failure += value
	}
	if value, ok := apiKeyRoutingHealthInt(values, 2); ok {
		health.LatencyTotalMs += value
	}
	if value, ok := apiKeyRoutingHealthInt(values, 3); ok {
		health.LatencySamples += value
	}
	observedAtMs, hasObservedAt := apiKeyRoutingHealthInt(values, 5)
	lastSuccessValue, hasLastSuccess := apiKeyRoutingHealthInt(values, 4)
	if !hasObservedAt || !hasLastSuccess {
		return
	}
	observedAt := time.UnixMilli(observedAtMs).UTC()
	if health.LastObservedAt != nil && !observedAt.After(*health.LastObservedAt) {
		return
	}
	lastSuccess := lastSuccessValue == 1
	health.LastSuccess = &lastSuccess
	health.LastObservedAt = &observedAt
}

func apiKeyRoutingHealthInt(values []any, index int) (int64, bool) {
	if index < 0 || index >= len(values) || values[index] == nil {
		return 0, false
	}
	value, err := strconv.ParseInt(fmt.Sprint(values[index]), 10, 64)
	return value, err == nil
}

func (c *gatewayCache) RecordAPIKeyRoutingOutcome(ctx context.Context, groupID int64, success bool, latencyMs *int64, at time.Time) error {
	if c == nil || c.rdb == nil || groupID <= 0 {
		return nil
	}
	successValue := int64(0)
	if success {
		successValue = 1
	}
	latencyValue := int64(-1)
	if latencyMs != nil && *latencyMs >= 0 {
		latencyValue = *latencyMs
	}
	return apiKeyRoutingHealthRecordScript.Run(
		ctx,
		c.rdb,
		[]string{apiKeyRoutingHealthKey(groupID, at)},
		successValue,
		latencyValue,
		at.UTC().UnixMilli(),
		int64(apiKeyRoutingHealthTTL/time.Second),
	).Err()
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

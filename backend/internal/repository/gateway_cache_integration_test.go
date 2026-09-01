//go:build integration

package repository

import (
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

type GatewayCacheSuite struct {
	IntegrationRedisSuite
	cache service.GatewayCache
}

func (s *GatewayCacheSuite) SetupTest() {
	s.IntegrationRedisSuite.SetupTest()
	s.cache = NewGatewayCache(s.rdb)
}

func (s *GatewayCacheSuite) TestGetSessionAccountID_Missing() {
	_, err := s.cache.GetSessionAccountID(s.ctx, 1, "nonexistent")
	require.True(s.T(), errors.Is(err, service.ErrStickySessionNotFound), "expected ErrStickySessionNotFound for missing session")
}

func (s *GatewayCacheSuite) TestSetAndGetSessionAccountID() {
	sessionID := "s1"
	accountID := int64(99)
	groupID := int64(1)
	sessionTTL := 1 * time.Minute

	require.NoError(s.T(), s.cache.SetSessionAccountID(s.ctx, groupID, sessionID, accountID, sessionTTL), "SetSessionAccountID")

	sid, err := s.cache.GetSessionAccountID(s.ctx, groupID, sessionID)
	require.NoError(s.T(), err, "GetSessionAccountID")
	require.Equal(s.T(), accountID, sid, "session id mismatch")
}

func (s *GatewayCacheSuite) TestSessionAccountID_TTL() {
	sessionID := "s2"
	accountID := int64(100)
	groupID := int64(1)
	sessionTTL := 1 * time.Minute

	require.NoError(s.T(), s.cache.SetSessionAccountID(s.ctx, groupID, sessionID, accountID, sessionTTL), "SetSessionAccountID")

	sessionKey := buildSessionKey(groupID, sessionID)
	ttl, err := s.rdb.TTL(s.ctx, sessionKey).Result()
	require.NoError(s.T(), err, "TTL sessionKey after Set")
	s.AssertTTLWithin(ttl, 1*time.Second, sessionTTL)
}

func (s *GatewayCacheSuite) TestRefreshSessionTTL() {
	sessionID := "s3"
	accountID := int64(101)
	groupID := int64(1)
	initialTTL := 1 * time.Minute
	refreshTTL := 3 * time.Minute

	require.NoError(s.T(), s.cache.SetSessionAccountID(s.ctx, groupID, sessionID, accountID, initialTTL), "SetSessionAccountID")

	require.NoError(s.T(), s.cache.RefreshSessionTTL(s.ctx, groupID, sessionID, refreshTTL), "RefreshSessionTTL")

	sessionKey := buildSessionKey(groupID, sessionID)
	ttl, err := s.rdb.TTL(s.ctx, sessionKey).Result()
	require.NoError(s.T(), err, "TTL after Refresh")
	s.AssertTTLWithin(ttl, 1*time.Second, refreshTTL)
}

func (s *GatewayCacheSuite) TestRefreshSessionTTL_MissingKey() {
	// RefreshSessionTTL on a missing key should not error (no-op)
	err := s.cache.RefreshSessionTTL(s.ctx, 1, "missing-session", 1*time.Minute)
	require.NoError(s.T(), err, "RefreshSessionTTL on missing key should not error")
}

func (s *GatewayCacheSuite) TestDeleteSessionAccountID() {
	sessionID := "openai:s4"
	accountID := int64(102)
	groupID := int64(1)
	sessionTTL := 1 * time.Minute

	require.NoError(s.T(), s.cache.SetSessionAccountID(s.ctx, groupID, sessionID, accountID, sessionTTL), "SetSessionAccountID")
	require.NoError(s.T(), s.cache.DeleteSessionAccountID(s.ctx, groupID, sessionID), "DeleteSessionAccountID")

	_, err := s.cache.GetSessionAccountID(s.ctx, groupID, sessionID)
	require.True(s.T(), errors.Is(err, service.ErrStickySessionNotFound), "expected ErrStickySessionNotFound after delete")
}

func (s *GatewayCacheSuite) TestGetSessionAccountID_CorruptedValue() {
	sessionID := "corrupted"
	groupID := int64(1)
	sessionKey := buildSessionKey(groupID, sessionID)

	// Set a non-integer value
	require.NoError(s.T(), s.rdb.Set(s.ctx, sessionKey, "not-a-number", 1*time.Minute).Err(), "Set invalid value")

	_, err := s.cache.GetSessionAccountID(s.ctx, groupID, sessionID)
	require.Error(s.T(), err, "expected error for corrupted value")
	require.False(s.T(), errors.Is(err, service.ErrStickySessionNotFound), "expected parsing error, not a miss")
}

func (s *GatewayCacheSuite) TestAPIKeyRoutingStickyBindingIsHashedAndExpires() {
	cache := s.cache.(*gatewayCache)
	const sessionKey = "private-conversation-identifier"
	const ttl = time.Hour

	require.NoError(s.T(), cache.SetAPIKeyRoutingGroupID(s.ctx, 71, sessionKey, 81, ttl))
	groupID, err := cache.GetAPIKeyRoutingGroupID(s.ctx, 71, sessionKey)
	require.NoError(s.T(), err)
	require.Equal(s.T(), int64(81), groupID)

	redisKey := apiKeyRoutingStickyKey(71, sessionKey)
	require.NotContains(s.T(), redisKey, sessionKey)
	gotTTL, err := s.rdb.TTL(s.ctx, redisKey).Result()
	require.NoError(s.T(), err)
	s.AssertTTLWithin(gotTTL, time.Second, ttl)
}

func (s *GatewayCacheSuite) TestAPIKeyRoutingHealthAggregatesFiveMinuteBucketsWithinWindow() {
	cache := s.cache.(*gatewayCache)
	now := time.Now().UTC()
	groupID := int64(91)
	latestLatency := int64(120)
	olderLatency := int64(130)
	failedLatency := int64(200)
	outsideLatency := int64(900)

	require.NoError(s.T(), cache.RecordAPIKeyRoutingOutcome(s.ctx, groupID, true, &latestLatency, now))
	require.NoError(s.T(), cache.RecordAPIKeyRoutingOutcome(s.ctx, groupID, true, &olderLatency, now.Add(-5*time.Minute)))
	require.NoError(s.T(), cache.RecordAPIKeyRoutingOutcome(s.ctx, groupID, false, &failedLatency, now.Add(-10*time.Minute)))
	// 此桶仍存在于 Redis，但不应进入 30 分钟窗口查询。
	require.NoError(s.T(), cache.RecordAPIKeyRoutingOutcome(s.ctx, groupID, false, &outsideLatency, now.Add(-45*time.Minute)))

	health, err := cache.GetAPIKeyRoutingHealth(s.ctx, groupID, now.Add(-30*time.Minute))
	require.NoError(s.T(), err)
	require.Equal(s.T(), int64(2), health.Success)
	require.Equal(s.T(), int64(1), health.Failure)
	require.Equal(s.T(), int64(450), health.LatencyTotalMs)
	require.Equal(s.T(), int64(3), health.LatencySamples)
	require.NotNil(s.T(), health.LastSuccess)
	require.True(s.T(), *health.LastSuccess)
	require.NotNil(s.T(), health.LastObservedAt)
	require.Equal(s.T(), now.UnixMilli(), health.LastObservedAt.UnixMilli())

	batch, err := cache.GetAPIKeyRoutingHealthBatch(s.ctx, []int64{groupID, groupID, 92}, now.Add(-30*time.Minute))
	require.NoError(s.T(), err)
	require.Equal(s.T(), health.Success, batch[groupID].Success)
	require.Equal(s.T(), service.APIKeyRoutingHealth{}, batch[92])

	// 同一桶内较旧的写入后到达时，不得覆盖时间更新的最近结果。
	outOfOrderGroupID := int64(93)
	bucketStart := now.Truncate(apiKeyRoutingHealthBucket).Add(-apiKeyRoutingHealthBucket)
	olderAt := bucketStart.Add(time.Minute)
	newerAt := bucketStart.Add(2 * time.Minute)
	require.NoError(s.T(), cache.RecordAPIKeyRoutingOutcome(s.ctx, outOfOrderGroupID, false, nil, newerAt))
	require.NoError(s.T(), cache.RecordAPIKeyRoutingOutcome(s.ctx, outOfOrderGroupID, true, nil, olderAt))
	outOfOrderHealth, err := cache.GetAPIKeyRoutingHealth(s.ctx, outOfOrderGroupID, now.Add(-30*time.Minute))
	require.NoError(s.T(), err)
	require.Equal(s.T(), int64(1), outOfOrderHealth.Success)
	require.Equal(s.T(), int64(1), outOfOrderHealth.Failure)
	require.NotNil(s.T(), outOfOrderHealth.LastSuccess)
	require.False(s.T(), *outOfOrderHealth.LastSuccess)
	require.NotNil(s.T(), outOfOrderHealth.LastObservedAt)
	require.Equal(s.T(), newerAt.UnixMilli(), outOfOrderHealth.LastObservedAt.UnixMilli())

	bucketTTL, err := s.rdb.TTL(s.ctx, apiKeyRoutingHealthKey(groupID, now)).Result()
	require.NoError(s.T(), err)
	s.AssertTTLWithin(bucketTTL, time.Second, apiKeyRoutingHealthTTL)
}

func TestGatewayCacheSuite(t *testing.T) {
	suite.Run(t, new(GatewayCacheSuite))
}

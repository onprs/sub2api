package repository

import (
	"strconv"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestBillingCacheParseSubscriptionCache_RollingQuotaFields(t *testing.T) {
	fiveHour := 2.5
	thirtyDay := 20.0
	fiveHourStart := time.Date(2026, 5, 27, 1, 2, 3, 0, time.UTC)
	sevenDayStart := time.Date(2026, 5, 20, 1, 2, 3, 0, time.UTC)

	got, err := (&billingCache{}).parseSubscriptionCache(map[string]string{
		subFieldStatus:               "active",
		subFieldExpiresAt:            "1800000000",
		subFieldFiveHourLimitUSD:     "2.5",
		subFieldSevenDayLimitUSD:     "",
		subFieldThirtyDayLimitUSD:    "20",
		subFieldFiveHourUsage:        "2.4",
		subFieldSevenDayUsage:        "7.8",
		subFieldThirtyDayUsage:       "12.3",
		subFieldFiveHourWindowStart:  strconv.FormatInt(fiveHourStart.Unix(), 10),
		subFieldSevenDayWindowStart:  strconv.FormatInt(sevenDayStart.Unix(), 10),
		subFieldThirtyDayWindowStart: "",
		subFieldVersion:              "42",
	})

	require.NoError(t, err)
	require.NotNil(t, got.FiveHourLimitUSD)
	require.Equal(t, fiveHour, *got.FiveHourLimitUSD)
	require.Nil(t, got.SevenDayLimitUSD)
	require.NotNil(t, got.ThirtyDayLimitUSD)
	require.Equal(t, thirtyDay, *got.ThirtyDayLimitUSD)
	require.Equal(t, 2.4, got.FiveHourUsage)
	require.Equal(t, 7.8, got.SevenDayUsage)
	require.Equal(t, 12.3, got.ThirtyDayUsage)
	require.NotNil(t, got.FiveHourWindowStart)
	require.Equal(t, fiveHourStart, got.FiveHourWindowStart.UTC())
	require.NotNil(t, got.SevenDayWindowStart)
	require.Equal(t, sevenDayStart, got.SevenDayWindowStart.UTC())
	require.Nil(t, got.ThirtyDayWindowStart)
}

func TestBillingCacheGetSubscriptionCacheRejectsLegacyCacheWithoutRollingFields(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	cache := &billingCache{rdb: rdb}
	ctx := t.Context()
	key := billingSubKey(10, 20)

	require.NoError(t, rdb.HSet(ctx, key, map[string]any{
		subFieldStatus:       service.SubscriptionStatusActive,
		subFieldExpiresAt:    time.Now().Add(time.Hour).Unix(),
		subFieldDailyUsage:   "1",
		subFieldWeeklyUsage:  "2",
		subFieldMonthlyUsage: "3",
		subFieldVersion:      "7",
	}).Err())

	got, err := cache.GetSubscriptionCache(ctx, 10, 20)

	require.Error(t, err)
	require.Nil(t, got)
	require.Contains(t, err.Error(), "rolling quota")
}

func TestBillingCacheSetAndUpdateSubscriptionCache_RollingQuotaFields(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	cache := &billingCache{rdb: rdb}
	ctx := t.Context()

	fiveHourLimit := 5.0
	thirtyDayLimit := 30.0
	fiveHourStart := time.Now().Add(-1 * time.Hour).UTC()
	expiredSevenDayStart := time.Now().Add(-service.SubscriptionWindowSevenDay - time.Minute).UTC()

	require.NoError(t, cache.SetSubscriptionCache(ctx, 10, 20, &service.SubscriptionCacheData{
		Status:               service.SubscriptionStatusActive,
		ExpiresAt:            time.Now().Add(24 * time.Hour),
		DailyUsage:           1,
		WeeklyUsage:          2,
		MonthlyUsage:         3,
		FiveHourLimitUSD:     &fiveHourLimit,
		SevenDayLimitUSD:     nil,
		ThirtyDayLimitUSD:    &thirtyDayLimit,
		FiveHourUsage:        2,
		SevenDayUsage:        9,
		ThirtyDayUsage:       10,
		FiveHourWindowStart:  &fiveHourStart,
		SevenDayWindowStart:  &expiredSevenDayStart,
		ThirtyDayWindowStart: nil,
		Version:              7,
	}))

	beforeUpdate := time.Now().Unix()
	require.NoError(t, cache.UpdateSubscriptionUsage(ctx, 10, 20, 0.5))
	afterUpdate := time.Now().Unix()

	got, err := cache.GetSubscriptionCache(ctx, 10, 20)
	require.NoError(t, err)
	require.Equal(t, 1.5, got.DailyUsage)
	require.Equal(t, 2.5, got.WeeklyUsage)
	require.Equal(t, 3.5, got.MonthlyUsage)
	require.NotNil(t, got.FiveHourLimitUSD)
	require.Equal(t, fiveHourLimit, *got.FiveHourLimitUSD)
	require.Nil(t, got.SevenDayLimitUSD)
	require.NotNil(t, got.ThirtyDayLimitUSD)
	require.Equal(t, thirtyDayLimit, *got.ThirtyDayLimitUSD)
	require.Equal(t, 2.5, got.FiveHourUsage)
	require.Equal(t, 0.5, got.SevenDayUsage)
	require.Equal(t, 0.5, got.ThirtyDayUsage)
	require.NotNil(t, got.FiveHourWindowStart)
	require.Equal(t, fiveHourStart.Unix(), got.FiveHourWindowStart.Unix())
	require.NotNil(t, got.SevenDayWindowStart)
	require.GreaterOrEqual(t, got.SevenDayWindowStart.Unix(), beforeUpdate)
	require.LessOrEqual(t, got.SevenDayWindowStart.Unix(), afterUpdate)
	require.NotNil(t, got.ThirtyDayWindowStart)
	require.GreaterOrEqual(t, got.ThirtyDayWindowStart.Unix(), beforeUpdate)
	require.LessOrEqual(t, got.ThirtyDayWindowStart.Unix(), afterUpdate)
}

func TestBillingCacheUpdateSubscriptionUsageRecordsRollingUsageBeyondLimits(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	cache := &billingCache{rdb: rdb}
	ctx := t.Context()

	fiveHourLimit := 5.0
	sevenDayLimit := 0.0
	start := time.Now().Add(-time.Hour).UTC()

	require.NoError(t, cache.SetSubscriptionCache(ctx, 10, 20, &service.SubscriptionCacheData{
		Status:               service.SubscriptionStatusActive,
		ExpiresAt:            time.Now().Add(24 * time.Hour),
		FiveHourLimitUSD:     &fiveHourLimit,
		SevenDayLimitUSD:     &sevenDayLimit,
		ThirtyDayLimitUSD:    nil,
		FiveHourUsage:        4.5,
		SevenDayUsage:        3,
		ThirtyDayUsage:       4.5,
		FiveHourWindowStart:  &start,
		SevenDayWindowStart:  &start,
		ThirtyDayWindowStart: &start,
	}))

	require.NoError(t, cache.UpdateSubscriptionUsage(ctx, 10, 20, 2))

	got, err := cache.GetSubscriptionCache(ctx, 10, 20)
	require.NoError(t, err)
	require.Equal(t, 6.5, got.FiveHourUsage)
	require.Equal(t, 5.0, got.SevenDayUsage)
	require.Equal(t, 6.5, got.ThirtyDayUsage)
}

func TestBillingCacheUpdateSubscriptionUsageReturnsMissWhenKeyAbsent(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	cache := &billingCache{rdb: rdb}
	ctx := t.Context()

	err := cache.UpdateSubscriptionUsage(ctx, 10, 20, 1)

	require.ErrorIs(t, err, redis.Nil)
}

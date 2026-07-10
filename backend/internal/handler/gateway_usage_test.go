package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestUsageSubscriptionPayloadUsesRollingQuotaSnapshot(t *testing.T) {
	gin.SetMode(gin.TestMode)

	legacyDaily := 999.0
	legacyWeekly := 888.0
	legacyMonthly := 777.0
	fiveHourLimit := 10.0
	thirtyDayLimit := 100.0
	windowStart := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	expiresAt := windowStart.Add(2 * time.Hour)

	apiKey := &service.APIKey{
		ID:     101,
		UserID: 202,
		Status: service.StatusAPIKeyActive,
		Group: &service.Group{
			ID:               303,
			Name:             "Snapshot Plan",
			SubscriptionType: service.SubscriptionTypeSubscription,
			DailyLimitUSD:    &legacyDaily,
			WeeklyLimitUSD:   &legacyWeekly,
			MonthlyLimitUSD:  &legacyMonthly,
		},
	}
	subscription := &service.UserSubscription{
		ID:                   404,
		UserID:               202,
		GroupID:              303,
		Status:               service.SubscriptionStatusActive,
		StartsAt:             windowStart.Add(-time.Hour),
		ExpiresAt:            expiresAt,
		FiveHourLimitUSD:     &fiveHourLimit,
		SevenDayLimitUSD:     nil,
		ThirtyDayLimitUSD:    &thirtyDayLimit,
		FiveHourUsageUSD:     4,
		SevenDayUsageUSD:     40,
		ThirtyDayUsageUSD:    20,
		FiveHourWindowStart:  &windowStart,
		SevenDayWindowStart:  &windowStart,
		ThirtyDayWindowStart: &windowStart,
		DailyUsageUSD:        900,
		WeeklyUsageUSD:       800,
		MonthlyUsageUSD:      700,
		DailyWindowStart:     &windowStart,
		WeeklyWindowStart:    &windowStart,
		MonthlyWindowStart:   &windowStart,
	}

	router := gin.New()
	handler := &GatewayHandler{}
	router.GET("/v1/usage", func(c *gin.Context) {
		c.Set(string(middleware2.ContextKeyAPIKey), apiKey)
		c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: apiKey.UserID})
		c.Set(string(middleware2.ContextKeySubscription), subscription)
		handler.Usage(c)
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/usage", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, float64(6), body["remaining"])

	sub, ok := body["subscription"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, fiveHourLimit, sub["five_hour_limit_usd"])
	require.Equal(t, float64(4), sub["five_hour_usage_usd"])
	require.Equal(t, windowStart.Format(time.RFC3339), sub["five_hour_window_start"])
	require.Equal(t, expiresAt.Format(time.RFC3339), sub["five_hour_window_end"])
	require.Equal(t, expiresAt.Format(time.RFC3339), sub["five_hour_window_resets_at"])

	require.Nil(t, sub["seven_day_limit_usd"], "nil rolling snapshot limit means unlimited")
	require.Equal(t, float64(40), sub["seven_day_usage_usd"])
	require.Equal(t, thirtyDayLimit, sub["thirty_day_limit_usd"])

	require.NotEqual(t, legacyDaily, sub["five_hour_limit_usd"])
	require.NotEqual(t, legacyWeekly, sub["seven_day_limit_usd"])
	require.NotEqual(t, legacyMonthly, sub["thirty_day_limit_usd"])
}

func TestUsageSubscriptionPayloadNormalizesExpiredRollingWindows(t *testing.T) {
	gin.SetMode(gin.TestMode)

	fiveHourLimit := 10.0
	windowStart := time.Now().Add(-6 * time.Hour).UTC()
	expiresAt := time.Now().Add(24 * time.Hour).UTC()

	apiKey := &service.APIKey{
		ID:     101,
		UserID: 202,
		Status: service.StatusAPIKeyActive,
		Group: &service.Group{
			ID:               303,
			Name:             "Snapshot Plan",
			SubscriptionType: service.SubscriptionTypeSubscription,
		},
	}
	subscription := &service.UserSubscription{
		ID:                  404,
		UserID:              202,
		GroupID:             303,
		Status:              service.SubscriptionStatusActive,
		StartsAt:            windowStart.Add(-time.Hour),
		ExpiresAt:           expiresAt,
		FiveHourLimitUSD:    &fiveHourLimit,
		FiveHourUsageUSD:    10,
		FiveHourWindowStart: &windowStart,
	}

	router := gin.New()
	handler := &GatewayHandler{}
	router.GET("/v1/usage", func(c *gin.Context) {
		c.Set(string(middleware2.ContextKeyAPIKey), apiKey)
		c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: apiKey.UserID})
		c.Set(string(middleware2.ContextKeySubscription), subscription)
		handler.Usage(c)
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/usage", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, fiveHourLimit, body["remaining"])

	sub, ok := body["subscription"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, float64(0), sub["five_hour_usage_usd"])
	require.Nil(t, sub["five_hour_window_start"])
	require.Nil(t, sub["five_hour_window_end"])
	require.Nil(t, sub["five_hour_window_resets_at"])
}

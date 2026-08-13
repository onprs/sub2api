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

func TestGatewayUsageResponsesExposeSub2APIUsageSchema(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	t.Run("quota limited", func(t *testing.T) {
		t.Parallel()
		h := &GatewayHandler{}
		apiKey := &service.APIKey{
			Status:    service.StatusAPIKeyActive,
			Quota:     20,
			QuotaUsed: 7.5,
		}
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodGet, "/v1/usage", nil)

		h.usageQuotaLimited(c, c.Request.Context(), apiKey, nil, nil, nil)

		var payload map[string]any
		require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &payload))
		require.Equal(t, "sub2api.usage", payload["object"])
		require.Equal(t, float64(1), payload["schema_version"])
		require.Equal(t, "quota_limited", payload["mode"])
	})

	t.Run("summary only", func(t *testing.T) {
		t.Parallel()
		h := &GatewayHandler{}
		apiKey := &service.APIKey{
			Status:    service.StatusAPIKeyActive,
			Quota:     10,
			QuotaUsed: 4,
		}
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodGet, "/v1/usage?summary_only=true", nil)
		c.Set(string(middleware2.ContextKeyAPIKey), apiKey)
		c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 1})

		h.Usage(c)

		var payload map[string]any
		require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &payload))
		require.Equal(t, "sub2api.usage", payload["object"])
		require.Equal(t, float64(1), payload["schema_version"])
		require.Equal(t, "quota_limited", payload["mode"])
		require.NotContains(t, payload, "usage")
		require.NotContains(t, payload, "daily_usage")
		require.NotContains(t, payload, "model_stats")
	})

	t.Run("subscription", func(t *testing.T) {
		t.Parallel()
		limit := 30.0
		groupID := int64(8)
		group := &service.Group{
			ID:               groupID,
			Name:             "订阅计划",
			SubscriptionType: service.SubscriptionTypeSubscription,
		}
		apiKey := &service.APIKey{GroupID: &groupID, Group: group}
		subscription := &service.UserSubscription{
			GroupID:           groupID,
			ExpiresAt:         time.Now().Add(24 * time.Hour),
			Status:            service.SubscriptionStatusActive,
			SevenDayLimitUSD:  &limit,
			SevenDayUsageUSD:  12,
			FiveHourUsageUSD:  0,
			ThirtyDayUsageUSD: 0,
		}
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodGet, "/v1/usage", nil)
		c.Set(string(middleware2.ContextKeySubscription), subscription)

		h := &GatewayHandler{}
		h.usageUnrestricted(c, c.Request.Context(), apiKey, middleware2.AuthSubject{UserID: 1}, nil, nil, nil)

		var payload map[string]any
		require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &payload))
		require.Equal(t, "sub2api.usage", payload["object"])
		require.Equal(t, float64(1), payload["schema_version"])
		require.Equal(t, "unrestricted", payload["mode"])
		require.InDelta(t, 18, payload["remaining"].(float64), 1e-9)
	})
}

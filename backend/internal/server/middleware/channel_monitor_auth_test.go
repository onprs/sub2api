package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func internalChannelMonitorTestKey() *service.APIKey {
	groupID := int64(20)
	group := &service.Group{
		ID:       groupID,
		Name:     "OpenAI Primary",
		Platform: service.PlatformOpenAI,
		Status:   service.StatusActive,
	}
	return &service.APIKey{
		ID:                     -34,
		UserID:                 -34,
		GroupID:                &groupID,
		Group:                  group,
		Status:                 service.StatusAPIKeyActive,
		InternalChannelMonitor: true,
		User: &service.User{
			ID:     -34,
			Role:   service.RoleAdmin,
			Status: service.StatusActive,
		},
	}
}

func TestAPIKeyAuthAcceptsOnlyContextBoundInternalChannelMonitor(t *testing.T) {
	gin.SetMode(gin.TestMode)
	apiKey := internalChannelMonitorTestKey()
	router := gin.New()
	router.Use(apiKeyAuthWithSubscription(nil, nil, nil))
	router.POST("/v1/chat/completions", func(c *gin.Context) {
		got, ok := GetAPIKeyFromContext(c)
		require.True(t, ok)
		require.Same(t, apiKey, got)
		c.Status(http.StatusNoContent)
	})

	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	request = request.WithContext(service.WithInternalChannelMonitorRequest(request.Context(), apiKey))
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	require.Equal(t, http.StatusNoContent, recorder.Code)

	forgedRequest := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	forgedRequest.Header.Set("X-Sub2API-Internal-Monitor", "true")
	forgedRecorder := httptest.NewRecorder()
	router.ServeHTTP(forgedRecorder, forgedRequest)
	require.Equal(t, http.StatusUnauthorized, forgedRecorder.Code)
}

func TestGoogleAPIKeyAuthAcceptsContextBoundInternalChannelMonitor(t *testing.T) {
	gin.SetMode(gin.TestMode)
	apiKey := internalChannelMonitorTestKey()
	router := gin.New()
	router.Use(APIKeyAuthWithSubscriptionGoogle(nil, nil, nil))
	router.POST("/v1beta/models/:model", func(c *gin.Context) {
		got, ok := GetAPIKeyFromContext(c)
		require.True(t, ok)
		require.Same(t, apiKey, got)
		c.Status(http.StatusNoContent)
	})

	request := httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini:generateContent", nil)
	request = request.WithContext(service.WithInternalChannelMonitorRequest(request.Context(), apiKey))
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	require.Equal(t, http.StatusNoContent, recorder.Code)
}

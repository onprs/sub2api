package handler

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type apiKeyRoutingMiddlewareAccountRepo struct {
	service.AccountRepository
	byGroup map[int64][]service.Account
}

func (r *apiKeyRoutingMiddlewareAccountRepo) ListSchedulableByGroupIDAndPlatform(_ context.Context, groupID int64, platform string) ([]service.Account, error) {
	accounts := r.byGroup[groupID]
	result := make([]service.Account, 0, len(accounts))
	for _, account := range accounts {
		if account.Platform == platform {
			result = append(result, account)
		}
	}
	return result, nil
}

type apiKeyRoutingMiddlewareCache struct {
	service.GatewayCache
	mu        sync.Mutex
	sticky    map[string]int64
	health    map[int64]service.APIKeyRoutingHealth
	outcomes  map[int64][]bool
	latencies map[int64][]*int64
}

func (c *apiKeyRoutingMiddlewareCache) GetAPIKeyRoutingGroupID(_ context.Context, _ int64, sessionKey string) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	groupID, ok := c.sticky[sessionKey]
	if !ok {
		return 0, errors.New("routing binding not found")
	}
	return groupID, nil
}

func (c *apiKeyRoutingMiddlewareCache) SetAPIKeyRoutingGroupID(_ context.Context, _ int64, sessionKey string, groupID int64, _ time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.sticky == nil {
		c.sticky = make(map[string]int64)
	}
	c.sticky[sessionKey] = groupID
	return nil
}

func (c *apiKeyRoutingMiddlewareCache) GetAPIKeyRoutingHealth(_ context.Context, groupID int64, _ time.Time) (service.APIKeyRoutingHealth, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.health[groupID], nil
}

func (c *apiKeyRoutingMiddlewareCache) RecordAPIKeyRoutingOutcome(_ context.Context, groupID int64, success bool, latencyMs *int64, _ time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.outcomes == nil {
		c.outcomes = make(map[int64][]bool)
	}
	if c.latencies == nil {
		c.latencies = make(map[int64][]*int64)
	}
	var latencyCopy *int64
	if latencyMs != nil {
		value := *latencyMs
		latencyCopy = &value
	}
	c.outcomes[groupID] = append(c.outcomes[groupID], success)
	c.latencies[groupID] = append(c.latencies[groupID], latencyCopy)
	return nil
}

func (c *apiKeyRoutingMiddlewareCache) routingOutcomes(groupID int64) []bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]bool(nil), c.outcomes[groupID]...)
}

func (c *apiKeyRoutingMiddlewareCache) routingLatencies(groupID int64) []*int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]*int64(nil), c.latencies[groupID]...)
}

func newAPIKeyRoutingMiddlewareHandler(accounts map[int64][]service.Account, cache *apiKeyRoutingMiddlewareCache) *GatewayHandler {
	if cache == nil {
		cache = &apiKeyRoutingMiddlewareCache{}
	}
	return &GatewayHandler{gatewayService: service.NewGatewayService(
		&apiKeyRoutingMiddlewareAccountRepo{byGroup: accounts},
		nil, nil, nil, nil, nil, nil, cache, nil, nil, nil, nil, nil, nil,
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
	)}
}

func apiKeyRoutingMiddlewareGroup(id int64, allowImage bool) *service.Group {
	return &service.Group{
		ID:                        id,
		Platform:                  service.PlatformOpenAI,
		Status:                    service.StatusActive,
		SubscriptionType:          service.SubscriptionTypeStandard,
		RateMultiplier:            1,
		AllowImageGeneration:      allowImage,
		AllowBatchImageGeneration: allowImage,
	}
}

func apiKeyRoutingMiddlewareAccount(id int64) service.Account {
	return service.Account{
		ID:          id,
		Platform:    service.PlatformOpenAI,
		Type:        service.AccountTypeAPIKey,
		Status:      service.StatusActive,
		Schedulable: true,
		Concurrency: 1,
	}
}

func TestAPIKeyRoutingMiddleware_SelectsMediaCapableGroupAndRestoresBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mediaDisabled := apiKeyRoutingMiddlewareGroup(801, false)
	mediaEnabled := apiKeyRoutingMiddlewareGroup(802, true)
	primaryID := mediaDisabled.ID
	apiKey := &service.APIKey{
		ID:              701,
		UserID:          702,
		GroupID:         &primaryID,
		Group:           mediaDisabled,
		RoutingPlatform: service.PlatformOpenAI,
		RoutingStrategy: service.APIKeyRoutingStrategyManual,
		RoutingGroups: []service.APIKeyGroupBinding{
			{GroupID: mediaDisabled.ID, Priority: 0, Group: mediaDisabled},
			{GroupID: mediaEnabled.ID, Priority: 1, Group: mediaEnabled},
		},
		Status: service.StatusActive,
		User:   &service.User{ID: 702, Status: service.StatusActive, Balance: 10},
	}
	cache := &apiKeyRoutingMiddlewareCache{}
	h := newAPIKeyRoutingMiddlewareHandler(map[int64][]service.Account{
		mediaDisabled.ID: {apiKeyRoutingMiddlewareAccount(901)},
		mediaEnabled.ID:  {apiKeyRoutingMiddlewareAccount(902)},
	}, cache)
	body := []byte(`{"model":"gpt-image","tools":[{"type":"image_generation"}]}`)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware2.ContextKeyAPIKey), apiKey)
		c.Next()
	})
	router.Use(h.APIKeyRoutingMiddleware(false))
	router.POST("/v1/responses", func(c *gin.Context) {
		effectiveKey, ok := middleware2.GetAPIKeyFromContext(c)
		require.True(t, ok)
		require.Equal(t, mediaEnabled.ID, *effectiveKey.GroupID)
		group, ok := c.Request.Context().Value(ctxkey.Group).(*service.Group)
		require.True(t, ok)
		require.Equal(t, mediaEnabled.ID, group.ID)
		restored, err := io.ReadAll(c.Request.Body)
		require.NoError(t, err)
		require.Equal(t, body, restored)

		// 组内失败后成功不应降低分组成功率。
		c.Set(service.OpsUpstreamStatusCodeKey, http.StatusBadGateway)
		c.Set(service.OpsTimeToFirstTokenMsKey, int64(137))
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, []bool{true}, cache.routingOutcomes(mediaEnabled.ID))
	require.Len(t, cache.routingLatencies(mediaEnabled.ID), 1)
	require.Equal(t, int64(137), *cache.routingLatencies(mediaEnabled.ID)[0])
	require.Equal(t, mediaDisabled.ID, *apiKey.GroupID, "中间件不能修改缓存中的原始 Key")
}

func TestAPIKeyRoutingMiddleware_NoEligibleGroupUsesProtocolErrorShape(t *testing.T) {
	gin.SetMode(gin.TestMode)
	first := apiKeyRoutingMiddlewareGroup(811, false)
	second := apiKeyRoutingMiddlewareGroup(812, false)
	primaryID := first.ID
	apiKey := &service.APIKey{
		ID:              711,
		UserID:          712,
		GroupID:         &primaryID,
		Group:           first,
		RoutingPlatform: service.PlatformOpenAI,
		RoutingStrategy: service.APIKeyRoutingStrategyManual,
		RoutingGroups: []service.APIKeyGroupBinding{
			{GroupID: first.ID, Priority: 0, Group: first},
			{GroupID: second.ID, Priority: 1, Group: second},
		},
		Status: service.StatusActive,
		User:   &service.User{ID: 712, Status: service.StatusActive, Balance: 10},
	}
	h := newAPIKeyRoutingMiddlewareHandler(map[int64][]service.Account{
		first.ID:  {apiKeyRoutingMiddlewareAccount(911)},
		second.ID: {apiKeyRoutingMiddlewareAccount(912)},
	}, nil)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware2.ContextKeyAPIKey), apiKey)
		c.Next()
	})
	router.Use(h.APIKeyRoutingMiddleware(false))
	router.POST("/v1/images/generations", func(c *gin.Context) {
		t.Fatal("无合格候选时不应进入 handler")
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewBufferString(`{"model":"gpt-image"}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	require.Equal(t, "NO_AVAILABLE_ROUTING_GROUP", gjson.Get(rec.Body.String(), "code").String())
}

func TestAPIKeyRoutingInputFromRequest_ProtocolCoverage(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("Responses WebSocket waits for first payload", func(t *testing.T) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
		_, shouldResolve, err := apiKeyRoutingInputFromRequest(c)
		require.NoError(t, err)
		require.False(t, shouldResolve)
	})

	t.Run("video status restores generation session", func(t *testing.T) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodGet, "/v1/videos/video-request-1", nil)
		c.Params = gin.Params{{Key: "request_id", Value: "video-request-1"}}
		const userID int64 = 17
		const apiKeyID int64 = 29
		c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{ID: apiKeyID})
		c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: userID})
		input, shouldResolve, err := apiKeyRoutingInputFromRequest(c)
		require.NoError(t, err)
		require.True(t, shouldResolve)
		require.Equal(t, service.APIKeyRoutingCapabilityVideo, input.Capability)
		require.Equal(t, service.GrokMediaVideoRequestSessionHash("video-request-1", userID, apiKeyID), input.SessionKey)
	})

	t.Run("Codex models manifest selects one actual group", func(t *testing.T) {
		for _, target := range []string{
			"/v1/models?client_version=1.2.3",
			"/backend-api/codex/models",
		} {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodGet, target, nil)
			_, shouldResolve, err := apiKeyRoutingInputFromRequest(c)
			require.NoError(t, err)
			require.True(t, shouldResolve, target)
		}
	})

	t.Run("ordinary model list is candidate union", func(t *testing.T) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
		_, shouldResolve, err := apiKeyRoutingInputFromRequest(c)
		require.NoError(t, err)
		require.False(t, shouldResolve)
	})

	t.Run("batch resource management keeps job ownership routing", func(t *testing.T) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/batches/batch_1/cancel", nil)
		_, shouldResolve, err := apiKeyRoutingInputFromRequest(c)
		require.NoError(t, err)
		require.False(t, shouldResolve)
	})

	t.Run("Gemini model action extracts model", func(t *testing.T) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-2.5-pro:generateContent", bytes.NewBufferString(`{"contents":[],"generationConfig":{"imageConfig":{"imageSize":"4K"}}}`))
		c.Request.Header.Set("Content-Type", "application/json")
		c.Params = gin.Params{{Key: "modelAction", Value: "/gemini-2.5-pro:generateContent"}}
		input, shouldResolve, err := apiKeyRoutingInputFromRequest(c)
		require.NoError(t, err)
		require.True(t, shouldResolve)
		require.Equal(t, "gemini-2.5-pro", input.Model)
		require.Equal(t, "4K", input.MediaSize)
	})

	t.Run("OpenAI endpoint requirements follow the requested route", func(t *testing.T) {
		for _, testCase := range []struct {
			name       string
			path       string
			body       string
			capability service.OpenAIEndpointCapability
		}{
			{name: "embeddings", path: "/v1/embeddings", body: `{"model":"text-embedding-3-small","input":"hello"}`, capability: service.OpenAIEndpointCapabilityEmbeddings},
			{name: "chat", path: "/v1/chat/completions", body: `{"model":"gpt-5.4","messages":[]}`, capability: service.OpenAIEndpointCapabilityChatCompletions},
			{name: "legacy compact responses", path: "/v1/responses", body: `{"model":"gpt-5.4","stream":false,"input":[{"type":"compaction_trigger"}]}`, capability: service.OpenAIEndpointCapabilityResponses},
			{name: "native responses", path: "/v1/responses", body: `{"model":"gpt-5.4","stream":true,"store":true,"prompt_cache_key":"routing-native","input":[{"type":"compaction_trigger"}]}`, capability: service.OpenAIEndpointCapabilityResponses},
		} {
			t.Run(testCase.name, func(t *testing.T) {
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest(http.MethodPost, testCase.path, bytes.NewBufferString(testCase.body))
				c.Request.Header.Set("Content-Type", "application/json")
				c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{RoutingPlatform: service.PlatformOpenAI})
				input, shouldResolve, err := apiKeyRoutingInputFromRequest(c)
				require.NoError(t, err)
				require.True(t, shouldResolve)
				require.Equal(t, testCase.capability, input.RequiredEndpointCapability)
			})
		}
	})

	t.Run("Responses image tool extracts media size", func(t *testing.T) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewBufferString(`{"model":"gpt-5.4","tools":[{"type":"image_generation","size":"1536x1024"}]}`))
		c.Request.Header.Set("Content-Type", "application/json")
		input, shouldResolve, err := apiKeyRoutingInputFromRequest(c)
		require.NoError(t, err)
		require.True(t, shouldResolve)
		require.Equal(t, service.APIKeyRoutingCapabilityImage, input.Capability)
		require.Equal(t, "1536x1024", input.MediaSize)
	})
}

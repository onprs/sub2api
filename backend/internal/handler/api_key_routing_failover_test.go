package handler

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func routingFailoverFixture(t *testing.T, final gin.HandlerFunc, prepare ...gin.HandlerFunc) (*gin.Engine, *apiKeyRoutingMiddlewareCache, *service.APIKey) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	first, second := apiKeyRoutingMiddlewareGroup(801, true), apiKeyRoutingMiddlewareGroup(802, true)
	key := &service.APIKey{ID: 701, UserID: 702, Group: first, GroupID: &first.ID, RoutingPlatform: service.PlatformOpenAI,
		RoutingStrategy: service.APIKeyRoutingStrategyManual, User: &service.User{ID: 702, Status: service.StatusActive, Balance: 10},
		RoutingGroups: []service.APIKeyGroupBinding{{Group: first, GroupID: first.ID}, {Group: second, GroupID: second.ID, Priority: 1}},
	}
	cache := &apiKeyRoutingMiddlewareCache{}
	h := newAPIKeyRoutingMiddlewareHandler(map[int64][]service.Account{first.ID: {apiKeyRoutingMiddlewareAccount(901)}, second.ID: {apiKeyRoutingMiddlewareAccount(902)}}, cache)
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set(string(middleware2.ContextKeyAPIKey), key); c.Next() })
	router.Use(h.APIKeyRoutingMiddleware(false, prepare...))
	for _, preflight := range prepare {
		router.Use(preflight)
	}
	for _, path := range []string{"/v1/responses", "/v1/messages", "/v1/chat/completions", "/v1/embeddings", "/v1/images/generations", "/v1/responses/compact", "/v1/models/*modelAction"} {
		router.POST(path, final)
	}
	return router, cache, key
}

func TestAPIKeyRoutingFailover_RetriesHTTPErrorPagesWithCleanContext(t *testing.T) {
	for _, status := range []int{429, 500, 502, 503, 504, 529} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			for _, path := range []string{"/v1/responses", "/v1/messages", "/v1/chat/completions", "/v1/embeddings"} {
				var visited []int64
				preflights := 0
				body := `{"model":"gpt-route","stream":false,"input":"test"}`
				router, cache, key := routingFailoverFixture(t, func(c *gin.Context) {
					apiKey, _ := middleware2.GetAPIKeyFromContext(c)
					visited = append(visited, apiKey.Group.ID)
					require.Equal(t, path, c.FullPath())
					group, ok := c.Request.Context().Value(ctxkey.Group).(*service.Group)
					require.True(t, ok)
					require.Equal(t, apiKey.Group.ID, group.ID)
					data, err := io.ReadAll(c.Request.Body)
					require.NoError(t, err)
					require.Equal(t, body, string(data))
					if len(visited) == 1 {
						c.Set("attempt_failure", true)
						c.Request.Header.Set("X-Attempt", "failed")
						c.Request = c.Request.WithContext(service.WithResolvedTargetPlatform(c.Request.Context(), service.PlatformGrok))
						c.Header("Retry-After", "999")
						c.Header("Content-Type", "text/html")
						c.AbortWithStatus(status)
						_, _ = c.Writer.WriteString("<html>upstream unavailable</html>")
						c.Writer.Flush()
						return
					}
					_, stale := c.Get("attempt_failure")
					require.False(t, stale)
					require.Empty(t, c.GetHeader("X-Attempt"))
					_, resolved := service.ResolvedTargetPlatformFromContext(c.Request.Context())
					require.False(t, resolved)
					c.JSON(http.StatusOK, gin.H{"ok": true})
				}, func(c *gin.Context) { preflights++; c.Next() })
				rec := httptest.NewRecorder()
				req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				router.ServeHTTP(rec, req)
				require.Equal(t, []int64{801, 802}, visited)
				require.Equal(t, 2, preflights)
				require.Equal(t, http.StatusOK, rec.Code)
				require.JSONEq(t, `{"ok":true}`, rec.Body.String())
				require.Empty(t, rec.Header().Get("Retry-After"))
				require.Equal(t, []bool{false}, cache.routingOutcomes(801))
				require.Equal(t, []bool{true}, cache.routingOutcomes(802))
				require.Equal(t, int64(801), *key.GroupID)
			}
		})
	}
}

func TestAPIKeyRoutingFailover_ExhaustionIsBounded(t *testing.T) {
	var visited []int64
	router, cache, _ := routingFailoverFixture(t, func(c *gin.Context) {
		key, _ := middleware2.GetAPIKeyFromContext(c)
		visited = append(visited, key.Group.ID)
		c.JSON(http.StatusServiceUnavailable, gin.H{"group": key.Group.ID})
	})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-route"}`)))
	require.Equal(t, []int64{801, 802}, visited)
	require.Equal(t, 503, rec.Code)
	require.JSONEq(t, `{"group":802}`, rec.Body.String())
	require.Equal(t, []bool{false}, cache.routingOutcomes(801))
	require.Equal(t, []bool{false}, cache.routingOutcomes(802))
}

func TestAPIKeyRoutingFailover_DoesNotReplayUnsafeOrCommittedRequests(t *testing.T) {
	for _, tc := range []struct {
		name, path, body          string
		status                    int
		stream, cancel, oversized bool
	}{
		{name: "client error", status: 400},
		{name: "permission denied", status: 403},
		{name: "model not found", status: 404},
		{name: "stream already started", status: 200, stream: true},
		{name: "cancelled", status: 503, cancel: true},
		{name: "oversized error", status: 502, oversized: true},
		{name: "background task", status: 503, body: `{"model":"gpt-route","background":true}`},
		{name: "response continuation", status: 503, body: `{"model":"gpt-route","previous_response_id":"resp_test"}`},
		{name: "conversation continuation", status: 503, body: `{"model":"gpt-route","conversation":"conv_test"}`},
		{name: "image generation", status: 503, path: "/v1/images/generations"},
		{name: "responses subpath", status: 503, path: "/v1/responses/compact"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			router, _, _ := routingFailoverFixture(t, func(c *gin.Context) {
				calls++
				c.Status(tc.status)
				if tc.stream {
					c.Header("Content-Type", "text/event-stream")
					_, _ = c.Writer.WriteString("data: partial\n\n")
					c.Writer.Flush()
					service.MarkOpsStreamFailure(c, "upstream_error", "", "upstream unavailable", 503)
				} else if tc.oversized {
					_, _ = c.Writer.Write(bytes.Repeat([]byte("x"), apiKeyRoutingErrorBufferLimit+1))
				} else {
					c.Writer.WriteHeaderNow()
				}
				if tc.cancel {
					cancel()
				}
			})
			path, body := tc.path, tc.body
			if path == "" {
				path = "/v1/responses"
			}
			if body == "" {
				body = `{"model":"gpt-route"}`
			}
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)).WithContext(ctx)
			router.ServeHTTP(rec, req)
			require.Equal(t, 1, calls)
			require.Equal(t, tc.status, rec.Code)
		})
	}
}

func TestAPIKeyRoutingFailover_PreflightErrorStopsDispatch(t *testing.T) {
	calls := 0
	preflight := func(c *gin.Context) {
		key, _ := middleware2.GetAPIKeyFromContext(c)
		if key.Group.ID == 802 {
			c.AbortWithStatusJSON(403, gin.H{"error": "denied"})
			return
		}
		c.Next()
	}
	router, _, _ := routingFailoverFixture(t, func(c *gin.Context) { calls++; c.AbortWithStatus(502) }, preflight)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-route"}`)))
	require.Equal(t, 1, calls)
	require.Equal(t, 403, rec.Code)
}

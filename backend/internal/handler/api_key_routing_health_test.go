package handler

import (
	"context"
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

type apiKeyRoutingHealthUserRepo struct {
	service.UserRepository
	user *service.User
}

func (r *apiKeyRoutingHealthUserRepo) GetByID(_ context.Context, _ int64) (*service.User, error) {
	return r.user, nil
}

type apiKeyRoutingHealthGroupRepo struct {
	service.GroupRepository
	groups []service.Group
}

func (r *apiKeyRoutingHealthGroupRepo) ListActive(_ context.Context) ([]service.Group, error) {
	return append([]service.Group(nil), r.groups...), nil
}

type apiKeyRoutingHealthSubscriptionRepo struct {
	service.UserSubscriptionRepository
}

func (r *apiKeyRoutingHealthSubscriptionRepo) ListActiveByUserID(_ context.Context, _ int64) ([]service.UserSubscription, error) {
	return []service.UserSubscription{}, nil
}

type apiKeyRoutingHealthAPIKeyRepo struct {
	service.APIKeyRepository
	ownedGroupIDs []int64
}

func (r *apiKeyRoutingHealthAPIKeyRepo) ListOwnedRoutingGroupIDs(_ context.Context, _ int64, requested []int64) ([]int64, error) {
	owned := make(map[int64]struct{}, len(r.ownedGroupIDs))
	for _, groupID := range r.ownedGroupIDs {
		owned[groupID] = struct{}{}
	}
	result := make([]int64, 0, len(requested))
	for _, groupID := range requested {
		if _, exists := owned[groupID]; exists {
			result = append(result, groupID)
		}
	}
	return result, nil
}

func newAPIKeyRoutingHealthAPIKeyService(user *service.User, groups []service.Group, ownedGroupIDs []int64) *service.APIKeyService {
	return service.NewAPIKeyService(
		&apiKeyRoutingHealthAPIKeyRepo{ownedGroupIDs: ownedGroupIDs},
		&apiKeyRoutingHealthUserRepo{user: user},
		&apiKeyRoutingHealthGroupRepo{groups: groups},
		&apiKeyRoutingHealthSubscriptionRepo{},
		nil,
		nil,
		nil,
	)
}

func serveAPIKeyRoutingHealth(t *testing.T, handler *GatewayHandler, role, query string) (int, apiKeyRoutingHealthResponse) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 701})
		c.Set(string(middleware2.ContextKeyUserRole), role)
		c.Next()
	})
	router.GET("/api/v1/groups/routing-health", handler.GetAPIKeyRoutingHealth)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/groups/routing-health?group_ids="+query, nil)
	router.ServeHTTP(recorder, request)

	var envelope struct {
		Data apiKeyRoutingHealthResponse `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
	return recorder.Code, envelope.Data
}

func TestGetAPIKeyRoutingHealth_FiltersGroupsForRegularUsers(t *testing.T) {
	lastSuccess := true
	observedAt := time.Now().UTC()
	cache := &apiKeyRoutingMiddlewareCache{health: map[int64]service.APIKeyRoutingHealth{
		11: {
			Success:        9,
			Failure:        1,
			LatencyTotalMs: 600,
			LatencySamples: 3,
			LastSuccess:    &lastSuccess,
			LastObservedAt: &observedAt,
		},
		12: {Success: 10},
		13: {Success: 7, Failure: 3},
	}}
	handler := newAPIKeyRoutingMiddlewareHandler(nil, cache)
	handler.apiKeyService = newAPIKeyRoutingHealthAPIKeyService(
		&service.User{ID: 701, Status: service.StatusActive},
		[]service.Group{
			{ID: 11, Status: service.StatusActive, SubscriptionType: service.SubscriptionTypeStandard},
			{ID: 12, Status: service.StatusActive, SubscriptionType: service.SubscriptionTypeStandard, IsExclusive: true},
		},
		[]int64{13},
	)

	status, body := serveAPIKeyRoutingHealth(t, handler, service.RoleUser, "11,12,13,11")
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, service.APIKeyRoutingHealthWindowMinutes, body.WindowMinutes)
	require.Len(t, body.Items, 2)
	require.Equal(t, int64(11), body.Items[0].GroupID)
	require.Equal(t, service.APIKeyRoutingHealthStatusDegraded, body.Items[0].Status)
	require.Equal(t, 90.0, *body.Items[0].SuccessRate)
	require.Equal(t, int64(200), *body.Items[0].AverageLatencyMs)
	require.Equal(t, int64(13), body.Items[1].GroupID)
	require.Equal(t, 70.0, *body.Items[1].SuccessRate)
}

func TestGetAPIKeyRoutingHealth_AllowsAdminCatalogAndRejectsInvalidIDs(t *testing.T) {
	cache := &apiKeyRoutingMiddlewareCache{health: map[int64]service.APIKeyRoutingHealth{
		11: {Success: 10},
		12: {Failure: 10},
	}}
	handler := newAPIKeyRoutingMiddlewareHandler(nil, cache)
	handler.apiKeyService = service.NewAPIKeyService(nil, nil, nil, nil, nil, nil, nil)

	status, body := serveAPIKeyRoutingHealth(t, handler, service.RoleAdmin, "11,12")
	require.Equal(t, http.StatusOK, status)
	require.Len(t, body.Items, 2)
	require.Equal(t, service.APIKeyRoutingHealthStatusOperational, body.Items[0].Status)
	require.Equal(t, service.APIKeyRoutingHealthStatusFailed, body.Items[1].Status)

	invalidStatus, _ := serveAPIKeyRoutingHealth(t, handler, service.RoleAdmin, "11,invalid")
	require.Equal(t, http.StatusBadRequest, invalidStatus)
}

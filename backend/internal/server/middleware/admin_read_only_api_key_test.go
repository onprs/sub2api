package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type readOnlyAuthSettings struct {
	service.SettingRepository
	values map[string]string
	getErr error
}

func (r *readOnlyAuthSettings) GetValue(_ context.Context, key string) (string, error) {
	if r.getErr != nil {
		return "", r.getErr
	}
	value, ok := r.values[key]
	if !ok {
		return "", service.ErrSettingNotFound
	}
	return value, nil
}
func (r *readOnlyAuthSettings) Set(_ context.Context, key, value string) error {
	r.values[key] = value
	return nil
}
func (r *readOnlyAuthSettings) Delete(_ context.Context, key string) error {
	delete(r.values, key)
	return nil
}

type readOnlyAuthUsers struct {
	service.UserRepository
	admin *service.User
}

func (r *readOnlyAuthUsers) GetFirstAdmin(context.Context) (*service.User, error) {
	return r.admin, nil
}

func TestAdminReadOnlyAPIKeyAccessBoundary(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &readOnlyAuthSettings{values: map[string]string{service.SettingKeyAdminAPIKey: "完整权限测试密钥"}}
	settings := service.NewSettingService(repo, nil)
	key, err := settings.GenerateAdminReadOnlyAPIKey(context.Background())
	require.NoError(t, err)
	users := service.NewUserService(&readOnlyAuthUsers{admin: &service.User{ID: 1, Role: service.RoleAdmin, Status: service.StatusActive}}, nil, nil, nil)
	router := gin.New()
	router.Use(gin.HandlerFunc(NewAdminAuthMiddleware(nil, users, settings, nil)))
	called := false
	handler := func(c *gin.Context) {
		called = true
		c.JSON(http.StatusOK, gin.H{"auth_method": c.GetString("auth_method")})
	}
	allowed := []struct{ method, path, requestPath string }{
		{"GET", "/api/v1/admin/groups", "/api/v1/admin/groups?platform=anthropic"},
		{"GET", "/api/v1/admin/groups/all", "/api/v1/admin/groups/all"},
		{"GET", "/api/v1/admin/groups/:id", "/api/v1/admin/groups/3"},
		{"GET", "/api/v1/admin/accounts", "/api/v1/admin/accounts?group=3&lite=false"},
		{"GET", "/api/v1/admin/accounts/:id", "/api/v1/admin/accounts/1"},
		{"GET", "/api/v1/admin/accounts/:id/usage", "/api/v1/admin/accounts/1/usage?source=active&force=true"},
		{"GET", "/api/v1/admin/accounts/:id/claude/reset-credits", "/api/v1/admin/accounts/1/claude/reset-credits"},
		{"POST", "/api/v1/admin/accounts/usage/batch", "/api/v1/admin/accounts/usage/batch"},
	}
	blocked := []struct{ method, path string }{
		{"POST", "/api/v1/admin/accounts"}, {"PUT", "/api/v1/admin/accounts/:id"}, {"DELETE", "/api/v1/admin/accounts/:id"},
		{"POST", "/api/v1/admin/accounts/:id/claude/reset-credits/redeem"}, {"POST", "/api/v1/admin/accounts/:id/reset-quota"},
		{"POST", "/api/v1/admin/accounts/:id/clear-error"}, {"POST", "/api/v1/admin/accounts/:id/refresh"},
		{"POST", "/api/v1/admin/openai/accounts/:id/reset-quota"}, {"POST", "/api/v1/admin/settings/admin-read-only-api-key/regenerate"},
		{"DELETE", "/api/v1/admin/settings/admin-read-only-api-key"}, {"GET", "/api/v1/admin/settings"},
		{"GET", "/api/v1/admin/settings/admin-api-key"}, {"GET", "/api/v1/admin/accounts/data"},
		{"GET", "/api/v1/admin/proxies"}, {"GET", "/api/v1/admin/users"}, {"GET", "/api/v1/admin/groups/:id/api-keys"},
		{"GET", "/api/v1/admin/integration-tokens/account-observer"}, {"GET", "/api/v1/admin/future-read-endpoint"},
	}
	for _, route := range allowed {
		router.Handle(route.method, route.path, handler)
	}
	for _, route := range blocked {
		router.Handle(route.method, route.path, handler)
	}
	request := func(method, path, credential string) *httptest.ResponseRecorder {
		called = false
		w := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, strings.NewReader(`{"account_ids":[1]}`))
		req.Header.Set("x-api-key", credential)
		router.ServeHTTP(w, req)
		return w
	}
	for _, route := range allowed {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			w := request(route.method, route.requestPath, key)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			require.True(t, called)
			require.Contains(t, w.Body.String(), service.AdminReadOnlyAPIKeyAuthMethod)
		})
	}
	for _, route := range blocked {
		t.Run("拒绝 "+route.method+" "+route.path, func(t *testing.T) {
			path := strings.ReplaceAll(route.path, ":id", "1")
			w := request(route.method, path, key)
			require.Equal(t, http.StatusForbidden, w.Code)
			require.False(t, called)
			require.Contains(t, w.Body.String(), "ADMIN_READ_ONLY_FORBIDDEN")
		})
	}
	w := request(http.MethodGet, "/api/v1/admin/settings", "完整权限测试密钥")
	require.Equal(t, http.StatusOK, w.Code)
	require.True(t, called)
	require.Contains(t, w.Body.String(), "admin_api_key")

	w = request(http.MethodGet, "/api/v1/admin/accounts", service.AdminReadOnlyAPIKeyPrefix+strings.Repeat("0", 64))
	require.Equal(t, http.StatusUnauthorized, w.Code)
	require.False(t, called)
	repo.getErr = errors.New("数据库读取失败")
	w = request(http.MethodGet, "/api/v1/admin/accounts", key)
	require.Equal(t, http.StatusInternalServerError, w.Code)
	require.False(t, called)
	repo.getErr = nil

	replacement, err := settings.GenerateAdminReadOnlyAPIKey(context.Background())
	require.NoError(t, err)
	require.Equal(t, http.StatusUnauthorized, request(http.MethodGet, "/api/v1/admin/accounts", key).Code)
	require.Equal(t, http.StatusOK, request(http.MethodGet, "/api/v1/admin/accounts", replacement).Code)
	require.NoError(t, settings.DeleteAdminReadOnlyAPIKey(context.Background()))
	require.Equal(t, http.StatusUnauthorized, request(http.MethodGet, "/api/v1/admin/accounts", replacement).Code)
}

func TestAdminReadOnlyAPIKeyRejectsWebSocket(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/admin/accounts", nil)
	c.Request.Header.Set("Connection", "Upgrade")
	c.Request.Header.Set("Upgrade", "websocket")
	require.False(t, adminReadOnlyAPIKeyAllows(c))
}

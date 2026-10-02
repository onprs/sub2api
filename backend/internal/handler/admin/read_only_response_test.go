package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAdminReadOnlyAccountResponsesHidePrivateFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	admin := newStubAdminService()
	secretNote := "private-note-fixture"
	account := service.Account{
		ID: 1, Name: "额度监控账号", Platform: service.PlatformAnthropic, Type: service.AccountTypeOAuth,
		Status: service.StatusActive, Schedulable: true, GroupIDs: []int64{3}, Notes: &secretNote,
		Credentials: map[string]any{"access_token": "private-access-fixture", "base_url": "https://private-host.invalid", "custom_header": "private-header-fixture"},
		Extra:       map[string]any{"custom_secret": "private-extra-fixture"}, ErrorMessage: "private-error-fixture",
	}
	admin.accounts = []service.Account{account}
	admin.getAccountResult = &account
	h := &AccountHandler{adminService: admin}
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set("auth_method", service.AdminReadOnlyAPIKeyAuthMethod) })
	router.GET("/api/v1/admin/accounts", h.List)
	router.GET("/api/v1/admin/accounts/:id", h.GetByID)
	for _, path := range []string{"/api/v1/admin/accounts?platform=anthropic&group=3&lite=false", "/api/v1/admin/accounts?platform=anthropic&group=3&lite=true", "/api/v1/admin/accounts/1"} {
		t.Run(path, func(t *testing.T) {
			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			require.Contains(t, w.Body.String(), "额度监控账号")
			require.Contains(t, w.Body.String(), `"group_ids":[3]`)
			for _, field := range []string{`"credentials"`, `"extra"`, `"notes"`, `"proxy"`, `"error_message"`, "private-"} {
				require.NotContains(t, w.Body.String(), field)
			}
		})
	}
	require.Equal(t, int64(3), admin.lastListAccounts.groupID)
	require.Equal(t, service.PlatformAnthropic, admin.lastListAccounts.platform)
	require.Equal(t, "private-header-fixture", account.Credentials["custom_header"])
	require.Equal(t, "private-extra-fixture", account.Extra["custom_secret"])
}

type readOnlyGroupAdminService struct{ *stubAdminService }

func (s *readOnlyGroupAdminService) GetGroup(_ context.Context, id int64) (*service.Group, error) {
	for i := range s.groups {
		if s.groups[i].ID == id {
			return &s.groups[i], nil
		}
	}
	return nil, service.ErrGroupNotFound
}

func TestAdminReadOnlyGroupResponsesUseExplicitFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	admin := &readOnlyGroupAdminService{newStubAdminService()}
	admin.groups = []service.Group{{ID: 3, Name: "额度监控分组", Platform: service.PlatformAnthropic, Status: service.StatusActive, Description: "private-group-detail", AccountCount: 2}}
	h := &GroupHandler{adminService: admin}
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set("auth_method", service.AdminReadOnlyAPIKeyAuthMethod) })
	router.GET("/api/v1/admin/groups", h.List)
	router.GET("/api/v1/admin/groups/all", h.GetAll)
	router.GET("/api/v1/admin/groups/:id", h.GetByID)
	for _, path := range []string{"/api/v1/admin/groups", "/api/v1/admin/groups/all", "/api/v1/admin/groups/3"} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		require.Contains(t, w.Body.String(), "额度监控分组")
		require.Contains(t, w.Body.String(), `"account_count":2`)
		require.NotContains(t, w.Body.String(), "private-group-detail")
		require.NotContains(t, w.Body.String(), `"account_groups"`)
	}
}

type readOnlySettingHandlerRepo struct {
	service.SettingRepository
	values map[string]string
}

func (r *readOnlySettingHandlerRepo) GetValue(_ context.Context, key string) (string, error) {
	value, ok := r.values[key]
	if !ok {
		return "", service.ErrSettingNotFound
	}
	return value, nil
}
func (r *readOnlySettingHandlerRepo) Set(_ context.Context, key, value string) error {
	r.values[key] = value
	return nil
}
func (r *readOnlySettingHandlerRepo) Delete(_ context.Context, key string) error {
	delete(r.values, key)
	return nil
}

func TestAdminReadOnlyAPIKeyManagementReturnsFullKeyOnlyOnce(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &readOnlySettingHandlerRepo{values: make(map[string]string)}
	h := &SettingHandler{settingService: service.NewSettingService(repo, nil)}
	router := gin.New()
	router.GET("/status", h.GetAdminReadOnlyAPIKey)
	router.POST("/regenerate", h.RegenerateAdminReadOnlyAPIKey)
	router.DELETE("/key", h.DeleteAdminReadOnlyAPIKey)
	request := func(method, path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(method, path, nil))
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		return w
	}
	require.Contains(t, request("GET", "/status").Body.String(), `"exists":false`)
	created := request("POST", "/regenerate")
	var result struct {
		Data struct {
			Key string `json:"key"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(created.Body.Bytes(), &result))
	require.NotEmpty(t, result.Data.Key)
	status := request("GET", "/status")
	require.Contains(t, status.Body.String(), `"exists":true`)
	require.NotContains(t, status.Body.String(), result.Data.Key)
	require.NotContains(t, status.Body.String(), `"hash"`)
	require.NotContains(t, repo.values[service.SettingKeyAdminReadOnlyAPIKey], result.Data.Key)
	request("DELETE", "/key")
	require.Contains(t, request("GET", "/status").Body.String(), `"exists":false`)
}

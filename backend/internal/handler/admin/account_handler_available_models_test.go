package admin

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type availableModelsAdminService struct {
	*stubAdminService
	account service.Account
}

func (s *availableModelsAdminService) GetAccount(_ context.Context, id int64) (*service.Account, error) {
	if s.account.ID == id {
		acc := s.account
		return &acc, nil
	}
	return s.stubAdminService.GetAccount(context.Background(), id)
}

func setupAvailableModelsRouter(adminSvc service.AdminService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := NewAccountHandler(adminSvc, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	router.GET("/api/v1/admin/accounts/:id/models", handler.GetAvailableModels)
	return router
}

func setupAvailableModelsRouterWithAccountTest(adminSvc service.AdminService, accountTestSvc *service.AccountTestService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := NewAccountHandler(adminSvc, nil, nil, nil, nil, nil, nil, nil, accountTestSvc, nil, nil, nil, nil, nil)
	router.GET("/api/v1/admin/accounts/:id/models", handler.GetAvailableModels)
	return router
}

type syncUpstreamHTTPUpstream struct {
	resp *http.Response
	err  error
}

func (u *syncUpstreamHTTPUpstream) Do(req *http.Request, proxyURL string, accountID int64, accountConcurrency int) (*http.Response, error) {
	if u.err != nil {
		return nil, u.err
	}
	return u.resp, nil
}

func (u *syncUpstreamHTTPUpstream) DoWithTLS(req *http.Request, proxyURL string, accountID int64, accountConcurrency int, profile *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxyURL, accountID, accountConcurrency)
}

func setupSyncUpstreamModelsRouter(adminSvc service.AdminService, upstream service.HTTPUpstream) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	accountTestSvc := service.NewAccountTestService(
		nil,
		nil,
		nil,
		nil,
		nil,
		upstream,
		&config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{Enabled: false}}},
		nil,
		nil,
		nil,
		nil,
	)
	handler := NewAccountHandler(adminSvc, nil, nil, nil, nil, nil, nil, nil, accountTestSvc, nil, nil, nil, nil, nil)
	router.POST("/api/v1/admin/accounts/:id/models/sync-upstream", handler.SyncUpstreamModels)
	return router
}

func TestAccountHandlerGetAvailableModels_GrokUsesXAIModels(t *testing.T) {
	svc := &availableModelsAdminService{
		stubAdminService: newStubAdminService(),
		account: service.Account{
			ID:       44,
			Name:     "grok-oauth",
			Platform: service.PlatformGrok,
			Type:     service.AccountTypeOAuth,
			Status:   service.StatusActive,
			Credentials: map[string]any{
				"model_mapping": map[string]any{
					"grok-4.3": "grok-4.3",
				},
			},
		},
	}
	router := setupAvailableModelsRouter(svc)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/accounts/44/models", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Data, 1)
	require.Equal(t, "grok-4.3", resp.Data[0].ID)
}

func TestAccountHandlerGetAvailableModels_GrokDefaultsToXAIModelsWithoutMapping(t *testing.T) {
	svc := &availableModelsAdminService{
		stubAdminService: newStubAdminService(),
		account: service.Account{
			ID:       45,
			Name:     "grok-oauth-defaults",
			Platform: service.PlatformGrok,
			Type:     service.AccountTypeOAuth,
			Status:   service.StatusActive,
		},
	}
	router := setupAvailableModelsRouter(svc)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/accounts/45/models", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.NotEmpty(t, resp.Data)

	var ids []string
	for _, model := range resp.Data {
		id := model.ID
		ids = append(ids, id)
		require.NotContains(t, strings.ToLower(id), "claude")
	}
	require.Contains(t, ids, "grok-4.3")
	require.Contains(t, ids, "grok-build-0.1")
}

func TestAccountHandlerGetAvailableModels_OpenAIOAuthUsesExplicitModelMapping(t *testing.T) {
	svc := &availableModelsAdminService{
		stubAdminService: newStubAdminService(),
		account: service.Account{
			ID:       42,
			Name:     "openai-oauth",
			Platform: service.PlatformOpenAI,
			Type:     service.AccountTypeOAuth,
			Status:   service.StatusActive,
			Credentials: map[string]any{
				"model_mapping": map[string]any{
					"gpt-5": "gpt-5.1",
				},
			},
		},
	}
	router := setupAvailableModelsRouter(svc)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/accounts/42/models", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Data, 1)
	require.Equal(t, "gpt-5", resp.Data[0].ID)
}

func TestAccountHandlerGetAvailableModels_OpenAIOAuthPassthroughFallsBackToDefaults(t *testing.T) {
	svc := &availableModelsAdminService{
		stubAdminService: newStubAdminService(),
		account: service.Account{
			ID:       43,
			Name:     "openai-oauth-passthrough",
			Platform: service.PlatformOpenAI,
			Type:     service.AccountTypeOAuth,
			Status:   service.StatusActive,
			Credentials: map[string]any{
				"model_mapping": map[string]any{
					"gpt-5": "gpt-5.1",
				},
			},
			Extra: map[string]any{
				"openai_passthrough": true,
			},
		},
	}
	router := setupAvailableModelsRouter(svc)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/accounts/43/models", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.NotEmpty(t, resp.Data)
	require.NotEqual(t, "gpt-5", resp.Data[0].ID)
}

func TestAccountHandlerGetAvailableModels_OpenAISparkShadowReturnsMappingModels(t *testing.T) {
	parentID := int64(100)
	svc := &availableModelsAdminService{
		stubAdminService: newStubAdminService(),
		account: service.Account{
			ID:              44,
			Name:            "openai-spark-shadow",
			Platform:        service.PlatformOpenAI,
			Type:            service.AccountTypeOAuth,
			Status:          service.StatusActive,
			ParentAccountID: &parentID,
			QuotaDimension:  service.QuotaDimensionSpark,
			Credentials: map[string]any{
				"model_mapping": map[string]any{
					"gpt-5.3-codex-spark": "gpt-5.3-codex-spark",
				},
			},
		},
	}
	router := setupAvailableModelsRouter(svc)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/accounts/44/models", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	ids := make([]string, 0, len(resp.Data))
	for _, m := range resp.Data {
		ids = append(ids, m.ID)
	}
	require.ElementsMatch(t, []string{
		"gpt-5.3-codex-spark",
	}, ids, "影子可用模型由 model_mapping 派生（非写死）")
}

func TestAccountHandlerGetAvailableModels_GeminiAPIKeyFreeUsesAIStudioCatalog(t *testing.T) {
	svc := &availableModelsAdminService{
		stubAdminService: newStubAdminService(),
		account: service.Account{
			ID:       45,
			Name:     "gemini-aistudio-free",
			Platform: service.PlatformGemini,
			Type:     service.AccountTypeAPIKey,
			Status:   service.StatusActive,
			Credentials: map[string]any{
				"tier_id": "aistudio_free",
			},
		},
	}
	router := setupAvailableModelsRouter(svc)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/accounts/45/models", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp struct {
		Data []struct {
			ID          string `json:"id"`
			DisplayName string `json:"display_name"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))

	ids := make([]string, 0, len(resp.Data))
	for _, model := range resp.Data {
		ids = append(ids, model.ID)
		require.NotEmpty(t, model.DisplayName)
	}
	require.Equal(t, []string{
		"gemini-3-flash-preview",
		"gemini-2.5-flash",
		"gemini-2.5-flash-lite",
		"gemini-3.1-flash-lite",
		"gemini-3.5-flash",
		"gemini-3.5-flash-lite",
		"gemini-3.6-flash",
		"gemini-3.7-flash",
		"gemma-4-26b-a4b-it",
		"gemma-4-31b-it",
	}, ids)
	require.NotContains(t, ids, "gemma-4-26b-it")
	require.NotContains(t, ids, "gemini-2.5-pro")
}

func TestAccountHandlerGetAvailableModels_GeminiAPIKeyPaidRetainsExtendedCatalog(t *testing.T) {
	svc := &availableModelsAdminService{
		stubAdminService: newStubAdminService(),
		account: service.Account{
			ID:       46,
			Name:     "gemini-aistudio-paid",
			Platform: service.PlatformGemini,
			Type:     service.AccountTypeAPIKey,
			Status:   service.StatusActive,
			Credentials: map[string]any{
				"tier_id": "aistudio_paid",
			},
		},
	}
	router := setupAvailableModelsRouter(svc)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/accounts/46/models", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	ids := make([]string, 0, len(resp.Data))
	for _, model := range resp.Data {
		ids = append(ids, model.ID)
	}
	require.Contains(t, ids, "gemini-2.5-pro")
	require.Contains(t, ids, "gemini-3.1-flash-image")
}

func TestAccountHandlerGetAvailableModels_GeminiAPIKeyFreeKeepsExplicitMapping(t *testing.T) {
	svc := &availableModelsAdminService{
		stubAdminService: newStubAdminService(),
		account: service.Account{
			ID:       47,
			Name:     "gemini-aistudio-free-mapped",
			Platform: service.PlatformGemini,
			Type:     service.AccountTypeAPIKey,
			Status:   service.StatusActive,
			Credentials: map[string]any{
				"tier_id": "aistudio_free",
				"model_mapping": map[string]any{
					"free-flash": "gemini-3.7-flash",
				},
			},
		},
	}
	router := setupAvailableModelsRouter(svc)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/accounts/47/models", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Data, 1)
	require.Equal(t, []string{"free-flash"}, []string{resp.Data[0].ID})
}

func TestAccountHandlerGetAvailableModels_ClinePassUsesCatalogWithoutMapping(t *testing.T) {
	expectedModels := service.ClinePassDefaultModelIDs()
	svc := &availableModelsAdminService{
		stubAdminService: newStubAdminService(),
		account: service.Account{
			ID:       46,
			Name:     "clinepass-defaults",
			Platform: service.PlatformClinePass,
			Type:     service.AccountTypeAPIKey,
			Status:   service.StatusActive,
		},
	}
	router := setupAvailableModelsRouter(svc)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/accounts/46/models", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.NotEmpty(t, resp.Data)
	ids := make([]string, 0, len(resp.Data))
	for _, model := range resp.Data {
		ids = append(ids, model.ID)
		require.True(t, strings.HasPrefix(model.ID, "cline-pass/"), model.ID)
		require.NotContains(t, strings.ToLower(model.ID), "claude")
	}
	require.ElementsMatch(t, expectedModels, ids)
}

func TestAccountHandlerGetAvailableModels_ClinePassUsesRequestedMappingModels(t *testing.T) {
	svc := &availableModelsAdminService{
		stubAdminService: newStubAdminService(),
		account: service.Account{
			ID:       47,
			Name:     "clinepass-mapped",
			Platform: service.PlatformClinePass,
			Type:     service.AccountTypeAPIKey,
			Status:   service.StatusActive,
			Credentials: map[string]any{
				"model_mapping": map[string]any{
					"friendly-cline": "cline-pass/glm-5.2",
				},
			},
		},
	}
	router := setupAvailableModelsRouter(svc)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/accounts/47/models", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Data, 1)
	require.Equal(t, "friendly-cline", resp.Data[0].ID)
}

func TestAccountHandlerGetAvailableModels_AntigravityOAuthFallsBackToAgyCatalog(t *testing.T) {
	svc := &availableModelsAdminService{
		stubAdminService: newStubAdminService(),
		account: service.Account{
			ID:       48,
			Name:     "antigravity-oauth-fallback",
			Platform: service.PlatformAntigravity,
			Type:     service.AccountTypeOAuth,
			Status:   service.StatusActive,
		},
	}
	router := setupAvailableModelsRouter(svc)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/accounts/48/models", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp struct {
		Data []antigravity.CatalogModel `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Data, 14)

	byID := make(map[string]antigravity.CatalogModel, len(resp.Data))
	for _, model := range resp.Data {
		byID[model.ID] = model
		require.Equal(t, antigravity.CatalogSourceFallback, model.Source)
		require.NotEmpty(t, model.InternalModel)
	}
	require.Equal(t, "gemini-3.7-flash-tiered", byID["gemini-3.7-flash-high"].CatalogID)
	require.Equal(t, "gemini-3.7-flash-high", byID["gemini-3.7-flash-high"].WireModel)
	require.Equal(t, "MODEL_PLACEHOLDER_M298", byID["gemini-3.7-flash-high"].InternalModel)
	require.Equal(t, "gemini-pro-agent", byID["gemini-3.1-pro-high"].WireModel)
	require.Equal(t, "gemini-3.5-flash-extra-low", byID["gemini-3.5-flash-low"].WireModel)
	require.NotContains(t, byID, "gemini-3-flash-agent")
	require.NotContains(t, byID, "chat_20706")
}

func TestAccountHandlerGetAvailableModels_AntigravitySetupTokenUsesResolvedLiveCatalog(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1internal:fetchAvailableModels", r.URL.Path)
		require.Equal(t, "Bearer setup-token", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"models": {
				"future_opaque_42":{"displayName":"Future Opaque","model":"MODEL_PLACEHOLDER_M999"},
				"gemini-3.7-flash-tiered":{"displayName":"Gemini 3.7 Flash","model":"MODEL_PLACEHOLDER_M301"},
				"gemini-pro-agent":{"displayName":"Gemini 3.1 Pro (High)","model":"MODEL_PLACEHOLDER_M16"}
			},
			"tieredModelIds":{"flash":["gemini-3.7-flash-tiered"]}
		}`))
	}))
	defer server.Close()

	oldBaseURLs := append([]string(nil), antigravity.BaseURLs...)
	oldBaseURL := antigravity.BaseURL
	antigravity.BaseURLs = []string{server.URL}
	antigravity.BaseURL = server.URL
	t.Cleanup(func() {
		antigravity.BaseURLs = oldBaseURLs
		antigravity.BaseURL = oldBaseURL
	})

	account := service.Account{
		ID:       49,
		Name:     "antigravity-setup-live",
		Platform: service.PlatformAntigravity,
		Type:     service.AccountTypeSetupToken,
		Status:   service.StatusActive,
		Credentials: map[string]any{
			"access_token": "setup-token",
			"project_id":   "project-live",
		},
	}
	adminSvc := &availableModelsAdminService{stubAdminService: newStubAdminService(), account: account}
	tokenProvider := service.NewAntigravityTokenProvider(nil, nil, nil)
	gatewaySvc := service.NewAntigravityGatewayService(nil, nil, nil, tokenProvider, nil, nil, nil, nil)
	accountTestSvc := service.NewAccountTestService(nil, nil, nil, nil, gatewaySvc, nil, &config.Config{}, nil, nil, nil, nil)
	router := setupAvailableModelsRouterWithAccountTest(adminSvc, accountTestSvc)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/accounts/49/models", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp struct {
		Data []antigravity.CatalogModel `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Data, 4)
	require.Equal(t, []string{
		"gemini-3.7-flash-high",
		"gemini-3.7-flash-medium",
		"gemini-3.7-flash-low",
		"gemini-3.1-pro-high",
	}, []string{resp.Data[0].ID, resp.Data[1].ID, resp.Data[2].ID, resp.Data[3].ID})
	require.Equal(t, "gemini-3.7-flash-tiered", resp.Data[0].CatalogID)
	require.Equal(t, "MODEL_PLACEHOLDER_M298", resp.Data[0].InternalModel)
	require.Equal(t, "gemini-3.7-flash-high", resp.Data[0].WireModel)
	require.Equal(t, "gemini-pro-agent", resp.Data[3].WireModel)
	require.Equal(t, antigravity.CatalogSourceLive, resp.Data[3].Source)
	for _, model := range resp.Data {
		require.NotEqual(t, "future_opaque_42", model.ID)
	}
}

func TestAccountHandlerGetAvailableModels_AntigravityAPIKeyUsesOwnUpstreamCatalog(t *testing.T) {
	upstream := &syncUpstreamHTTPUpstream{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"data":[{"id":"relay-model"},{"id":"gemini-3.1-pro-high"}]}`)),
	}}
	account := service.Account{
		ID:       50,
		Name:     "antigravity-apikey-relay",
		Platform: service.PlatformAntigravity,
		Type:     service.AccountTypeAPIKey,
		Status:   service.StatusActive,
		Credentials: map[string]any{
			"api_key":  "relay-key",
			"base_url": "https://gateway.example.com/antigravity",
		},
	}
	adminSvc := &availableModelsAdminService{stubAdminService: newStubAdminService(), account: account}
	accountTestSvc := service.NewAccountTestService(nil, nil, nil, nil, nil, upstream, &config.Config{
		Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{Enabled: false}},
	}, nil, nil, nil, nil)
	router := setupAvailableModelsRouterWithAccountTest(adminSvc, accountTestSvc)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/accounts/50/models", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp struct {
		Data []antigravity.CatalogModel `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Data, 2)
	for _, model := range resp.Data {
		require.Equal(t, antigravity.CatalogSourceUpstream, model.Source)
		require.Equal(t, model.ID, model.WireModel)
	}
	require.Equal(t, "gemini-3.1-pro-high", resp.Data[0].ID)
	require.Equal(t, "gemini-3.1-pro-high", resp.Data[0].WireModel, "API-key relay must not inherit the official Pro Agent route")
}

func TestAccountHandlerSyncUpstreamModels_ConfigErrorReturnsBadRequest(t *testing.T) {
	svc := &availableModelsAdminService{
		stubAdminService: newStubAdminService(),
		account: service.Account{
			ID:       44,
			Name:     "openai-apikey-missing-key",
			Platform: service.PlatformOpenAI,
			Type:     service.AccountTypeAPIKey,
			Status:   service.StatusActive,
			Credentials: map[string]any{
				"base_url": "https://openai.example.com/v1",
			},
		},
	}
	router := setupSyncUpstreamModelsRouter(svc, &syncUpstreamHTTPUpstream{})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/44/models/sync-upstream", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "No OpenAI API key is available")
}

func TestAccountHandlerSyncUpstreamModels_UpstreamErrorDoesNotExposeBody(t *testing.T) {
	svc := &availableModelsAdminService{
		stubAdminService: newStubAdminService(),
		account: service.Account{
			ID:       45,
			Name:     "openai-apikey-upstream-error",
			Platform: service.PlatformOpenAI,
			Type:     service.AccountTypeAPIKey,
			Status:   service.StatusActive,
			Credentials: map[string]any{
				"api_key":  "openai-key",
				"base_url": "https://openai.example.com/v1",
			},
		},
	}
	upstream := &syncUpstreamHTTPUpstream{resp: &http.Response{
		StatusCode: http.StatusBadGateway,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"error":"SECRET_TOKEN should not be exposed"}`)),
	}}
	router := setupSyncUpstreamModelsRouter(svc, upstream)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/45/models/sync-upstream", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadGateway, rec.Code)
	require.Contains(t, rec.Body.String(), "Upstream model list request failed with HTTP 502")
	require.NotContains(t, rec.Body.String(), "SECRET_TOKEN")
}

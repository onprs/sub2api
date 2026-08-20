package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type gatewayModelsAccountRepoStub struct {
	service.AccountRepository

	byGroup map[int64][]service.Account
}

type gatewayModelsResponseForTest struct {
	Object  string                    `json:"object"`
	Data    []gatewayModelItemForTest `json:"data"`
	Message string                    `json:"message"`
}

type gatewayModelItemForTest struct {
	ID        string `json:"id"`
	Object    string `json:"object"`
	Created   int64  `json:"created"`
	OwnedBy   string `json:"owned_by"`
	CreatedAt string `json:"created_at"`
}

type gatewayGeminiModelsResponseForTest struct {
	Models  []gatewayGeminiModelItemForTest `json:"models"`
	Message string                          `json:"message"`
}

type gatewayGeminiModelItemForTest struct {
	Name                       string   `json:"name"`
	DisplayName                string   `json:"displayName"`
	SupportedGenerationMethods []string `json:"supportedGenerationMethods"`
}

func (s *gatewayModelsAccountRepoStub) ListSchedulableByGroupID(ctx context.Context, groupID int64) ([]service.Account, error) {
	accounts, ok := s.byGroup[groupID]
	if !ok {
		return nil, nil
	}
	out := make([]service.Account, len(accounts))
	copy(out, accounts)
	return out, nil
}

func newGatewayModelsHandlerForTest(repo service.AccountRepository) *GatewayHandler {
	return &GatewayHandler{
		gatewayService: service.NewGatewayService(
			repo,
			nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
			nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
		),
	}
}

func TestGeminiV1BetaListModels_OpenAIGroupListsGenerationRoutableAliases(t *testing.T) {
	gin.SetMode(gin.TestMode)
	groupID := int64(19)
	h := newGatewayModelsHandlerForTest(&gatewayModelsAccountRepoStub{byGroup: map[int64][]service.Account{
		groupID: {{
			ID: 1, Platform: service.PlatformOpenAI,
			Credentials: map[string]any{"model_mapping": map[string]any{
				"google-client-model": "gpt-5.4",
				"wildcard-*":          "gpt-5.4",
			}},
		}},
	}})
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1beta/models", nil)
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{GroupID: &groupID, Group: &service.Group{ID: groupID, Platform: service.PlatformOpenAI}})

	h.GeminiV1BetaListModels(c)

	require.Equal(t, http.StatusOK, rec.Code)
	var got gatewayGeminiModelsResponseForTest
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Len(t, got.Models, 1)
	require.Equal(t, "models/google-client-model", got.Models[0].Name)
	require.Equal(t, []string{"generateContent", "streamGenerateContent"}, got.Models[0].SupportedGenerationMethods)
	require.NotContains(t, rec.Body.String(), "gpt-5.4")
}

func TestGeminiV1BetaListModels_DynamicGeminiKeyReturnsCandidateUnion(t *testing.T) {
	gin.SetMode(gin.TestMode)
	firstGroupID := int64(25)
	secondGroupID := int64(26)
	h := newGatewayModelsHandlerForTest(&gatewayModelsAccountRepoStub{byGroup: map[int64][]service.Account{
		firstGroupID: {{
			ID: 11, Platform: service.PlatformGemini,
			Credentials: map[string]any{"model_mapping": map[string]any{
				"gemini-first": "gemini-2.5-pro",
			}},
		}},
		secondGroupID: {{
			ID: 12, Platform: service.PlatformGemini,
			Credentials: map[string]any{"model_mapping": map[string]any{
				"gemini-second": "gemini-2.5-flash",
			}},
		}},
	}})
	firstGroup := &service.Group{
		ID: firstGroupID, Platform: service.PlatformGemini, Status: service.StatusActive,
		SubscriptionType: service.SubscriptionTypeStandard,
	}
	secondGroup := &service.Group{
		ID: secondGroupID, Platform: service.PlatformGemini, Status: service.StatusActive,
		SubscriptionType: service.SubscriptionTypeStandard,
	}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1beta/models", nil)
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
		ID: 100, UserID: 200, GroupID: &firstGroupID, Group: nil,
		RoutingPlatform: service.PlatformGemini,
		RoutingStrategy: service.APIKeyRoutingStrategyBalanced,
		RoutingGroups: []service.APIKeyGroupBinding{
			{GroupID: firstGroupID, Priority: 0, Group: firstGroup},
			{GroupID: secondGroupID, Priority: 1, Group: secondGroup},
		},
	})

	h.GeminiV1BetaListModels(c)

	require.Equal(t, http.StatusOK, rec.Code)
	var got gatewayGeminiModelsResponseForTest
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	modelNames := make([]string, 0, len(got.Models))
	for _, model := range got.Models {
		modelNames = append(modelNames, model.Name)
	}
	require.Contains(t, modelNames, "models/gemini-first")
	require.Contains(t, modelNames, "models/gemini-second")
}

func TestGeminiV1BetaListModels_UsesPlatformFallbackWhenNoSchedulableAccount(t *testing.T) {
	gin.SetMode(gin.TestMode)
	groupID := int64(35)
	h := newGatewayModelsHandlerForTest(&gatewayModelsAccountRepoStub{byGroup: map[int64][]service.Account{}})
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1beta/models", nil)
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{GroupID: &groupID, Group: &service.Group{ID: groupID, Platform: service.PlatformOpenAI}})

	h.GeminiV1BetaListModels(c)

	require.Equal(t, http.StatusOK, rec.Code)
	var got gatewayGeminiModelsResponseForTest
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Contains(t, geminiModelNamesForTest(got.Models), "models/gpt-5.4")
}

func TestGeminiV1BetaListModels_ExpandsWildcardMappingToConcreteDefaults(t *testing.T) {
	gin.SetMode(gin.TestMode)
	groupID := int64(36)
	h := newGatewayModelsHandlerForTest(&gatewayModelsAccountRepoStub{byGroup: map[int64][]service.Account{
		groupID: {{
			ID: 1, Platform: service.PlatformOpenAI,
			Credentials: map[string]any{"model_mapping": map[string]any{
				"gpt-*":        "gpt-5.4",
				"legacy-model": "gpt-5.4",
			}},
		}},
	}})
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1beta/models", nil)
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{GroupID: &groupID, Group: &service.Group{ID: groupID, Platform: service.PlatformOpenAI}})

	h.GeminiV1BetaListModels(c)

	require.Equal(t, http.StatusOK, rec.Code)
	var got gatewayGeminiModelsResponseForTest
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	names := geminiModelNamesForTest(got.Models)
	require.Contains(t, names, "models/gpt-5.4")
	require.Contains(t, names, "models/legacy-model")
	require.NotContains(t, rec.Body.String(), "*")
}

func TestGeminiV1BetaGetModel_MatchesWildcardMapping(t *testing.T) {
	gin.SetMode(gin.TestMode)
	groupID := int64(38)
	h := newGatewayModelsHandlerForTest(&gatewayModelsAccountRepoStub{byGroup: map[int64][]service.Account{
		groupID: {{
			ID: 1, Platform: service.PlatformOpenAI,
			Credentials: map[string]any{"model_mapping": map[string]any{
				"custom-*": "gpt-5.4",
			}},
		}},
	}})
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1beta/models/custom-model", nil)
	c.Params = gin.Params{{Key: "model", Value: "custom-model"}}
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{GroupID: &groupID, Group: &service.Group{ID: groupID, Platform: service.PlatformOpenAI}})

	h.GeminiV1BetaGetModel(c)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "models/custom-model", gjson.GetBytes(rec.Body.Bytes(), "name").String())
}

func TestGeminiV1BetaGetModel_UsesSameFallbackSurface(t *testing.T) {
	gin.SetMode(gin.TestMode)
	groupID := int64(37)
	h := newGatewayModelsHandlerForTest(&gatewayModelsAccountRepoStub{byGroup: map[int64][]service.Account{}})
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1beta/models/gpt-5.4", nil)
	c.Params = gin.Params{{Key: "model", Value: "gpt-5.4"}}
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{GroupID: &groupID, Group: &service.Group{ID: groupID, Platform: service.PlatformOpenAI}})

	h.GeminiV1BetaGetModel(c)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "models/gpt-5.4", gjson.GetBytes(rec.Body.Bytes(), "name").String())
}

func TestGeminiV1BetaListModels_AntigravityAndOpenCodeGroupsAreAdmitted(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name     string
		platform string
		model    string
	}{
		{name: "antigravity", platform: service.PlatformAntigravity, model: "gemini-3-flash"},
		{name: "opencode", platform: service.PlatformOpenCodeGo, model: "qwen3.7-plus"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			groupID := int64(20)
			h := newGatewayModelsHandlerForTest(&gatewayModelsAccountRepoStub{byGroup: map[int64][]service.Account{
				groupID: {{ID: 1, Platform: tc.platform, Credentials: map[string]any{"model_mapping": map[string]any{tc.model: tc.model}}}},
			}})
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodGet, "/v1beta/models", nil)
			c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{GroupID: &groupID, Group: &service.Group{ID: groupID, Platform: tc.platform}})

			h.GeminiV1BetaListModels(c)

			require.Equal(t, http.StatusOK, rec.Code)
			require.Contains(t, rec.Body.String(), "models/"+tc.model)
			require.NotContains(t, rec.Body.String(), "does not support Gemini generation")
		})
	}
}

func TestGeminiV1BetaGetModel_AntigravityAndOpenCodeGroupsUseAliasSurface(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name          string
		platform      string
		clientModel   string
		upstreamModel string
	}{
		{name: "antigravity", platform: service.PlatformAntigravity, clientModel: "gemini-alias", upstreamModel: "gemini-3-flash"},
		{name: "opencode", platform: service.PlatformOpenCodeGo, clientModel: "qwen-alias", upstreamModel: "qwen3.7-plus"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			groupID := int64(24)
			h := newGatewayModelsHandlerForTest(&gatewayModelsAccountRepoStub{byGroup: map[int64][]service.Account{
				groupID: {{ID: 5, Platform: tc.platform, Credentials: map[string]any{"model_mapping": map[string]any{tc.clientModel: tc.upstreamModel}}}},
			}})
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodGet, "/v1beta/models/"+tc.clientModel, nil)
			c.Params = gin.Params{{Key: "model", Value: tc.clientModel}}
			c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{GroupID: &groupID, Group: &service.Group{ID: groupID, Platform: tc.platform}})

			h.GeminiV1BetaGetModel(c)

			require.Equal(t, http.StatusOK, rec.Code)
			require.Equal(t, "models/"+tc.clientModel, gjson.GetBytes(rec.Body.Bytes(), "name").String())
		})
	}
}

func TestGeminiV1BetaListModels_AnthropicGroupListsGenerationRoutableAliases(t *testing.T) {
	gin.SetMode(gin.TestMode)
	groupID := int64(21)
	h := newGatewayModelsHandlerForTest(&gatewayModelsAccountRepoStub{byGroup: map[int64][]service.Account{
		groupID: {{
			ID: 2, Platform: service.PlatformAnthropic,
			Credentials: map[string]any{"model_mapping": map[string]any{
				"google-claude-model": "claude-sonnet-4-6",
				"wildcard-*":          "claude-sonnet-4-6",
			}},
		}},
	}})
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1beta/models", nil)
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{GroupID: &groupID, Group: &service.Group{ID: groupID, Platform: service.PlatformAnthropic}})

	h.GeminiV1BetaListModels(c)

	require.Equal(t, http.StatusOK, rec.Code)
	var got gatewayGeminiModelsResponseForTest
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Len(t, got.Models, 1)
	require.Equal(t, "models/google-claude-model", got.Models[0].Name)
	require.Equal(t, []string{"generateContent", "streamGenerateContent"}, got.Models[0].SupportedGenerationMethods)
	require.NotContains(t, rec.Body.String(), "claude-sonnet-4-6")
}

func TestGeminiV1BetaListModels_AnthropicEmptyMappingUsesClaudeDefaultsOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	groupID := int64(22)
	h := newGatewayModelsHandlerForTest(&gatewayModelsAccountRepoStub{byGroup: map[int64][]service.Account{
		groupID: {{ID: 3, Platform: service.PlatformAnthropic}},
	}})
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1beta/models", nil)
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{GroupID: &groupID, Group: &service.Group{ID: groupID, Platform: service.PlatformAnthropic}})

	h.GeminiV1BetaListModels(c)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "claude-sonnet")
	require.NotContains(t, rec.Body.String(), "gemini-")
}

func TestGeminiV1BetaGetModel_AnthropicGroupUsesAliasSurface(t *testing.T) {
	gin.SetMode(gin.TestMode)
	groupID := int64(23)
	h := newGatewayModelsHandlerForTest(&gatewayModelsAccountRepoStub{byGroup: map[int64][]service.Account{
		groupID: {{
			ID: 4, Platform: service.PlatformAnthropic,
			Credentials: map[string]any{"model_mapping": map[string]any{"google-claude-model": "claude-sonnet-4-6"}},
		}},
	}})
	newContext := func(model string) (*httptest.ResponseRecorder, *gin.Context) {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest(http.MethodGet, "/v1beta/models/"+model, nil)
		c.Params = gin.Params{{Key: "model", Value: model}}
		c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{GroupID: &groupID, Group: &service.Group{ID: groupID, Platform: service.PlatformAnthropic}})
		return rec, c
	}

	aliasRecorder, aliasContext := newContext("google-claude-model")
	h.GeminiV1BetaGetModel(aliasContext)
	require.Equal(t, http.StatusOK, aliasRecorder.Code)
	require.Equal(t, "models/google-claude-model", gjson.GetBytes(aliasRecorder.Body.Bytes(), "name").String())

	targetRecorder, targetContext := newContext("claude-sonnet-4-6")
	h.GeminiV1BetaGetModel(targetContext)
	require.Equal(t, http.StatusNotFound, targetRecorder.Code)
}

func TestGeminiV1BetaGetModel_OpenAIGroupRejectsUnroutableModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	groupID := int64(18)
	h := newGatewayModelsHandlerForTest(&gatewayModelsAccountRepoStub{byGroup: map[int64][]service.Account{
		groupID: {{
			ID: 1, Platform: service.PlatformOpenAI,
			Credentials: map[string]any{"model_mapping": map[string]any{"google-client-model": "gpt-5.4"}},
		}},
	}})
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1beta/models/not-routable", nil)
	c.Params = gin.Params{{Key: "model", Value: "not-routable"}}
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{GroupID: &groupID, Group: &service.Group{ID: groupID, Platform: service.PlatformOpenAI}})

	h.GeminiV1BetaGetModel(c)

	require.Equal(t, http.StatusNotFound, rec.Code)
	require.Equal(t, "NOT_FOUND", gjson.GetBytes(rec.Body.Bytes(), "error.status").String())
}

func TestGatewayModels_GeminiGroupFallsBackToGeminiModels(t *testing.T) {
	gin.SetMode(gin.TestMode)

	groupID := int64(20)
	h := newGatewayModelsHandlerForTest(
		&gatewayModelsAccountRepoStub{
			byGroup: map[int64][]service.Account{
				groupID: {
					{ID: 1, Platform: service.PlatformGemini},
				},
			},
		},
	)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
		Group: &service.Group{ID: groupID, Platform: service.PlatformGemini},
	})

	h.Models(c)

	require.Equal(t, http.StatusOK, rec.Code)

	var got gatewayModelsResponseForTest
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Equal(t, "list", got.Object)
	require.Contains(t, modelIDsForTest(got.Data), "gemini-2.5-flash")
	require.NotContains(t, modelIDsForTest(got.Data), "claude-sonnet-4-6")
}

func TestGatewayModels_OpenCodeGoGroupFallsBackToOpenCodeGoModels(t *testing.T) {
	gin.SetMode(gin.TestMode)

	groupID := int64(34)
	h := newGatewayModelsHandlerForTest(
		&gatewayModelsAccountRepoStub{
			byGroup: map[int64][]service.Account{
				groupID: {
					{ID: 1, Platform: service.PlatformOpenCodeGo},
				},
			},
		},
	)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
		Group: &service.Group{ID: groupID, Platform: service.PlatformOpenCodeGo},
	})

	h.Models(c)

	require.Equal(t, http.StatusOK, rec.Code)

	var got gatewayModelsResponseForTest
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	modelIDs := modelIDsForTest(got.Data)
	require.Contains(t, modelIDs, "glm-5.2")
	require.Contains(t, modelIDs, "qwen3.5-plus")
	require.Contains(t, modelIDs, "kimi-k2.7-code")
	require.Contains(t, modelIDs, "qwen3.7-plus")
	require.NotContains(t, modelIDs, "kimi-k2.7")
	require.NotContains(t, modelIDs, "claude-sonnet-4-6")
}

func TestGatewayModels_GeminiGroupFiltersMappedModelsByPlatform(t *testing.T) {
	gin.SetMode(gin.TestMode)

	groupID := int64(21)
	h := newGatewayModelsHandlerForTest(
		&gatewayModelsAccountRepoStub{
			byGroup: map[int64][]service.Account{
				groupID: {
					{
						ID:       1,
						Platform: service.PlatformAnthropic,
						Credentials: map[string]any{
							"model_mapping": map[string]any{
								"claude-sonnet-4-6": "claude-sonnet-4-6",
							},
						},
					},
					{
						ID:       2,
						Platform: service.PlatformGemini,
						Credentials: map[string]any{
							"model_mapping": map[string]any{
								"gemini-2.5-flash": "gemini-2.5-flash",
							},
						},
					},
				},
			},
		},
	)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
		Group: &service.Group{ID: groupID, Platform: service.PlatformGemini},
	})

	h.Models(c)

	require.Equal(t, http.StatusOK, rec.Code)

	var got gatewayModelsResponseForTest
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Equal(t, []string{"gemini-2.5-flash"}, modelIDsForTest(got.Data))
}

func TestGatewayModels_AntigravityClaudeModelsUseExplicitMappingOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)

	groupID := int64(30)
	h := newGatewayModelsHandlerForTest(
		&gatewayModelsAccountRepoStub{
			byGroup: map[int64][]service.Account{
				groupID: {
					{
						ID:       1,
						Platform: service.PlatformAntigravity,
						Credentials: map[string]any{
							"model_mapping": map[string]any{
								"claude-sonnet-4-6": "claude-sonnet-4-6",
								"claude-opus-4-6":   "claude-opus-4-6-thinking",
								"claude-*":          "claude-sonnet-4-6",
								"gemini-3-flash":    "gemini-3-flash",
							},
						},
					},
					{
						ID:       2,
						Platform: service.PlatformAnthropic,
						Credentials: map[string]any{
							"model_mapping": map[string]any{
								"claude-ignored": "claude-ignored",
							},
						},
					},
				},
			},
		},
	)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/antigravity/v1/models", nil)
	c.Set(string(middleware2.ContextKeyAPIKey), apiKeyWithGroupForModelsTest(groupID, service.PlatformAnthropic))

	h.AntigravityModels(c)

	require.Equal(t, http.StatusOK, rec.Code)

	var got gatewayModelsResponseForTest
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Equal(t, "list", got.Object)
	require.Empty(t, got.Message)
	require.Equal(t, []string{"claude-opus-4-6", "claude-sonnet-4-6"}, modelIDsForTest(got.Data))
}

func TestGatewayModels_AntigravityClaudeModelsEmptyMappingReturnsMessage(t *testing.T) {
	gin.SetMode(gin.TestMode)

	groupID := int64(31)
	h := newGatewayModelsHandlerForTest(
		&gatewayModelsAccountRepoStub{
			byGroup: map[int64][]service.Account{
				groupID: {
					{ID: 1, Platform: service.PlatformAntigravity},
				},
			},
		},
	)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/antigravity/models", nil)
	c.Set(string(middleware2.ContextKeyAPIKey), apiKeyWithGroupForModelsTest(groupID, service.PlatformAnthropic))

	h.AntigravityModels(c)

	require.Equal(t, http.StatusOK, rec.Code)

	var got gatewayModelsResponseForTest
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Equal(t, "list", got.Object)
	require.Empty(t, got.Data)
	require.Equal(t, "No Claude-compatible Antigravity model_mapping configured. Please ask the administrator to configure model mappings.", got.Message)
}

func TestGatewayModels_AntigravityGeminiModelsUseExplicitMappingOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)

	groupID := int64(32)
	h := newGatewayModelsHandlerForTest(
		&gatewayModelsAccountRepoStub{
			byGroup: map[int64][]service.Account{
				groupID: {
					{
						ID:       1,
						Platform: service.PlatformAntigravity,
						Credentials: map[string]any{
							"model_mapping": map[string]any{
								"claude-sonnet-4-6": "claude-sonnet-4-6",
								"gemini-3-flash":    "gemini-3-flash",
								"gemini-3-pro-high": "gemini-3-pro-high",
								"gemini-*":          "gemini-3-flash",
							},
						},
					},
				},
			},
		},
	)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/antigravity/v1beta/models", nil)
	c.Set(string(middleware2.ContextKeyAPIKey), apiKeyWithGroupForModelsTest(groupID, service.PlatformGemini))
	c.Set(string(middleware2.ContextKeyForcePlatform), service.PlatformAntigravity)

	h.GeminiV1BetaListModels(c)

	require.Equal(t, http.StatusOK, rec.Code)

	var got gatewayGeminiModelsResponseForTest
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Empty(t, got.Message)
	require.Equal(t, []string{"models/gemini-3-flash", "models/gemini-3-pro-high"}, geminiModelNamesForTest(got.Models))
	require.Equal(t, "gemini-3-flash", got.Models[0].DisplayName)
	require.Equal(t, []string{"generateContent", "streamGenerateContent"}, got.Models[0].SupportedGenerationMethods)
}

func TestGatewayModels_AntigravityGeminiModelsEmptyMappingReturnsMessage(t *testing.T) {
	gin.SetMode(gin.TestMode)

	groupID := int64(33)
	h := newGatewayModelsHandlerForTest(
		&gatewayModelsAccountRepoStub{
			byGroup: map[int64][]service.Account{
				groupID: {
					{
						ID:       1,
						Platform: service.PlatformAntigravity,
						Credentials: map[string]any{
							"model_mapping": map[string]any{
								"gemini-*": "gemini-3-flash",
							},
						},
					},
				},
			},
		},
	)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/antigravity/v1beta/models", nil)
	c.Set(string(middleware2.ContextKeyAPIKey), apiKeyWithGroupForModelsTest(groupID, service.PlatformGemini))
	c.Set(string(middleware2.ContextKeyForcePlatform), service.PlatformAntigravity)

	h.GeminiV1BetaListModels(c)

	require.Equal(t, http.StatusOK, rec.Code)

	var got gatewayGeminiModelsResponseForTest
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Empty(t, got.Models)
	require.Equal(t, "No Gemini-compatible Antigravity model_mapping configured. Please ask the administrator to configure model mappings.", got.Message)
}

func TestGatewayModels_CustomModelsListDisabledKeepsOriginalModels(t *testing.T) {
	gin.SetMode(gin.TestMode)

	groupID := int64(22)
	h := newGatewayModelsHandlerForTest(
		&gatewayModelsAccountRepoStub{
			byGroup: map[int64][]service.Account{
				groupID: {
					{
						ID:       1,
						Platform: service.PlatformOpenAI,
						Credentials: map[string]any{
							"model_mapping": map[string]any{
								"gpt-5.5": "gpt-5.5",
								"gpt-5.4": "gpt-5.4",
							},
						},
					},
				},
			},
		},
	)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
		Group: &service.Group{
			ID:       groupID,
			Platform: service.PlatformOpenAI,
			ModelsListConfig: service.GroupModelsListConfig{
				Enabled: false,
				Models:  []string{"gpt-5.5"},
			},
		},
	})

	h.Models(c)

	require.Equal(t, http.StatusOK, rec.Code)

	var got gatewayModelsResponseForTest
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Equal(t, []string{"gpt-5.4", "gpt-5.5"}, modelIDsForTest(got.Data))
}

func TestGatewayModels_CustomModelsListFiltersAndOrdersMappedModels(t *testing.T) {
	gin.SetMode(gin.TestMode)

	groupID := int64(23)
	h := newGatewayModelsHandlerForTest(
		&gatewayModelsAccountRepoStub{
			byGroup: map[int64][]service.Account{
				groupID: {
					{
						ID:       1,
						Platform: service.PlatformOpenAI,
						Credentials: map[string]any{
							"model_mapping": map[string]any{
								"gpt-5.4":         "gpt-5.4",
								"gpt-5.5":         "gpt-5.5",
								"legacy-gpt-2024": "legacy-gpt-2024",
							},
						},
					},
				},
			},
		},
	)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
		Group: &service.Group{
			ID:       groupID,
			Platform: service.PlatformOpenAI,
			ModelsListConfig: service.GroupModelsListConfig{
				Enabled: true,
				Models:  []string{"gpt-5.5", "missing-model", "gpt-5.4"},
			},
		},
	})

	h.Models(c)

	require.Equal(t, http.StatusOK, rec.Code)

	var got gatewayModelsResponseForTest
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Equal(t, []string{"gpt-5.5", "gpt-5.4"}, modelIDsForTest(got.Data))
}

func TestGatewayModels_CustomModelsListKeepsConcreteModelAllowedByWildcardMapping(t *testing.T) {
	gin.SetMode(gin.TestMode)

	groupID := int64(26)
	h := newGatewayModelsHandlerForTest(
		&gatewayModelsAccountRepoStub{
			byGroup: map[int64][]service.Account{
				groupID: {
					{
						ID:       1,
						Platform: service.PlatformAnthropic,
						Credentials: map[string]any{
							"model_mapping": map[string]any{
								"claude-*": "claude-sonnet-4-6",
							},
						},
					},
				},
			},
		},
	)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
		Group: &service.Group{
			ID:       groupID,
			Platform: service.PlatformAnthropic,
			ModelsListConfig: service.GroupModelsListConfig{
				Enabled: true,
				Models:  []string{"claude-sonnet-4-6"},
			},
		},
	})

	h.Models(c)

	require.Equal(t, http.StatusOK, rec.Code)

	var got gatewayModelsResponseForTest
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Equal(t, []string{"claude-sonnet-4-6"}, modelIDsForTest(got.Data))
}

func TestGatewayModels_AnthropicCustomModelsListIncludesOAuthClaudeAndMappedDeepSeek(t *testing.T) {
	gin.SetMode(gin.TestMode)

	groupID := int64(28)
	h := newGatewayModelsHandlerForTest(
		&gatewayModelsAccountRepoStub{
			byGroup: map[int64][]service.Account{
				groupID: {
					{
						ID:       1,
						Platform: service.PlatformAnthropic,
						Type:     service.AccountTypeOAuth,
					},
					{
						ID:       2,
						Platform: service.PlatformAnthropic,
						Type:     service.AccountTypeAPIKey,
						Credentials: map[string]any{
							"model_mapping": map[string]any{
								"deepseek-v4-pro": "deepseek-v4-pro",
							},
						},
					},
				},
			},
		},
	)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
		Group: &service.Group{
			ID:       groupID,
			Platform: service.PlatformAnthropic,
			ModelsListConfig: service.GroupModelsListConfig{
				Enabled: true,
				Models:  []string{"claude-fable-5", "claude-opus-4-8", "deepseek-v4-pro"},
			},
		},
	})

	h.Models(c)

	require.Equal(t, http.StatusOK, rec.Code)

	var got gatewayModelsResponseForTest
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Equal(t, []string{"claude-fable-5", "claude-opus-4-8", "deepseek-v4-pro"}, modelIDsForTest(got.Data))
}

func TestGatewayModels_AnthropicCustomModelsListDisabledKeepsMappedModelList(t *testing.T) {
	gin.SetMode(gin.TestMode)

	groupID := int64(29)
	h := newGatewayModelsHandlerForTest(
		&gatewayModelsAccountRepoStub{
			byGroup: map[int64][]service.Account{
				groupID: {
					{
						ID:       1,
						Platform: service.PlatformAnthropic,
						Type:     service.AccountTypeOAuth,
					},
					{
						ID:       2,
						Platform: service.PlatformAnthropic,
						Type:     service.AccountTypeAPIKey,
						Credentials: map[string]any{
							"model_mapping": map[string]any{
								"deepseek-v4-pro": "deepseek-v4-pro",
							},
						},
					},
				},
			},
		},
	)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
		Group: &service.Group{
			ID:       groupID,
			Platform: service.PlatformAnthropic,
			ModelsListConfig: service.GroupModelsListConfig{
				Enabled: false,
				Models:  []string{"claude-fable-5", "deepseek-v4-pro"},
			},
		},
	})

	h.Models(c)

	require.Equal(t, http.StatusOK, rec.Code)

	var got gatewayModelsResponseForTest
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Equal(t, []string{"deepseek-v4-pro"}, modelIDsForTest(got.Data))
}

func TestGatewayModels_AnthropicCustomModelsListIncludesOAuthClaudeWithoutMappings(t *testing.T) {
	gin.SetMode(gin.TestMode)

	groupID := int64(30)
	h := newGatewayModelsHandlerForTest(
		&gatewayModelsAccountRepoStub{
			byGroup: map[int64][]service.Account{
				groupID: {
					{
						ID:       1,
						Platform: service.PlatformAnthropic,
						Type:     service.AccountTypeOAuth,
					},
				},
			},
		},
	)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
		Group: &service.Group{
			ID:       groupID,
			Platform: service.PlatformAnthropic,
			ModelsListConfig: service.GroupModelsListConfig{
				Enabled: true,
				Models:  []string{"claude-opus-4-6-thinking", "claude-sonnet-4-6", "claude-sonnet-4-5"},
			},
		},
	})

	h.Models(c)

	require.Equal(t, http.StatusOK, rec.Code)

	var got gatewayModelsResponseForTest
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Equal(t, []string{"claude-opus-4-6-thinking", "claude-sonnet-4-6"}, modelIDsForTest(got.Data))
}

func TestGatewayModels_CustomModelsListCanReturnEmptyWhenSelectionsUnavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)

	groupID := int64(24)
	h := newGatewayModelsHandlerForTest(
		&gatewayModelsAccountRepoStub{
			byGroup: map[int64][]service.Account{
				groupID: {
					{
						ID:       1,
						Platform: service.PlatformOpenAI,
						Credentials: map[string]any{
							"model_mapping": map[string]any{
								"gpt-5.4": "gpt-5.4",
							},
						},
					},
				},
			},
		},
	)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
		Group: &service.Group{
			ID:       groupID,
			Platform: service.PlatformOpenAI,
			ModelsListConfig: service.GroupModelsListConfig{
				Enabled: true,
				Models:  []string{"gpt-5.5"},
			},
		},
	})

	h.Models(c)

	require.Equal(t, http.StatusOK, rec.Code)

	var got gatewayModelsResponseForTest
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Empty(t, modelIDsForTest(got.Data))
}

func TestGatewayModels_CustomModelsListFiltersDefaultFallbackModels(t *testing.T) {
	gin.SetMode(gin.TestMode)

	groupID := int64(25)
	h := newGatewayModelsHandlerForTest(
		&gatewayModelsAccountRepoStub{
			byGroup: map[int64][]service.Account{
				groupID: {
					{ID: 1, Platform: service.PlatformOpenAI},
				},
			},
		},
	)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
		Group: &service.Group{
			ID:       groupID,
			Platform: service.PlatformOpenAI,
			ModelsListConfig: service.GroupModelsListConfig{
				Enabled: true,
				Models:  []string{"gpt-5.5", "legacy-gpt-2024", "gpt-5.4"},
			},
		},
	})

	h.Models(c)

	require.Equal(t, http.StatusOK, rec.Code)

	var got gatewayModelsResponseForTest
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Equal(t, []string{"gpt-5.5", "gpt-5.4"}, modelIDsForTest(got.Data))
}

func TestGatewayModels_OpenAICustomModelsListKeepsOpenAIResponseShapeForDefaultFallback(t *testing.T) {
	gin.SetMode(gin.TestMode)

	groupID := int64(27)
	h := newGatewayModelsHandlerForTest(
		&gatewayModelsAccountRepoStub{
			byGroup: map[int64][]service.Account{
				groupID: {
					{ID: 1, Platform: service.PlatformOpenAI},
				},
			},
		},
	)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
		Group: &service.Group{
			ID:       groupID,
			Platform: service.PlatformOpenAI,
			ModelsListConfig: service.GroupModelsListConfig{
				Enabled: true,
				Models:  []string{"gpt-5.5", "gpt-5.4"},
			},
		},
	})

	h.Models(c)

	require.Equal(t, http.StatusOK, rec.Code)

	var got gatewayModelsResponseForTest
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Equal(t, []string{"gpt-5.5", "gpt-5.4"}, modelIDsForTest(got.Data))
	require.Equal(t, "model", got.Data[0].Object)
	require.NotZero(t, got.Data[0].Created)
	require.Equal(t, "openai", got.Data[0].OwnedBy)
	require.Empty(t, got.Data[0].CreatedAt)
}

func modelIDsForTest(models []gatewayModelItemForTest) []string {
	ids := make([]string, 0, len(models))
	for _, model := range models {
		ids = append(ids, model.ID)
	}
	return ids
}

func geminiModelNamesForTest(models []gatewayGeminiModelItemForTest) []string {
	names := make([]string, 0, len(models))
	for _, model := range models {
		names = append(names, model.Name)
	}
	return names
}

func apiKeyWithGroupForModelsTest(groupID int64, platform string) *service.APIKey {
	return &service.APIKey{
		GroupID: &groupID,
		Group:   &service.Group{ID: groupID, Platform: platform},
	}
}

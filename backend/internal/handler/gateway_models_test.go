package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
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

type codexModelsResponseForTest struct {
	Models []struct {
		Slug                     string                       `json:"slug"`
		SupportedReasoningLevels []codexReasoningLevelForTest `json:"supported_reasoning_levels"`
		InputModalities          []string                     `json:"input_modalities"`
		ModelMessages            map[string]json.RawMessage   `json:"model_messages"`
		TruncationPolicy         map[string]json.RawMessage   `json:"truncation_policy"`
		AvailabilityNUX          json.RawMessage              `json:"availability_nux"`
		Upgrade                  json.RawMessage              `json:"upgrade"`
	} `json:"models"`
}

type codexReasoningLevelForTest struct {
	Effort string `json:"effort"`
}

type gatewayModelItemForTest struct {
	ID                      string                                `json:"id"`
	Object                  string                                `json:"object"`
	Created                 int64                                 `json:"created"`
	OwnedBy                 string                                `json:"owned_by"`
	CreatedAt               string                                `json:"created_at"`
	SupportsReasoningEffort bool                                  `json:"supportsReasoningEffort"`
	ReasoningEffort         string                                `json:"reasoningEffort"`
	ReasoningEfforts        []gatewayReasoningEffortOptionForTest `json:"reasoningEfforts"`
}

type gatewayReasoningEffortOptionForTest struct {
	Value   string `json:"value"`
	Label   string `json:"label"`
	Default bool   `json:"default"`
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

func (s *gatewayModelsAccountRepoStub) ListByGroup(ctx context.Context, groupID int64) ([]service.Account, error) {
	return s.ListSchedulableByGroupID(ctx, groupID)
}

func (s *gatewayModelsAccountRepoStub) ListModelAvailabilityCandidates(ctx context.Context, groupID *int64, _ []string, _ bool) ([]service.Account, error) {
	if groupID == nil {
		return nil, nil
	}
	return s.ListSchedulableByGroupID(ctx, *groupID)
}

func newGatewayModelsHandlerForTest(repo service.AccountRepository) *GatewayHandler {
	return &GatewayHandler{
		gatewayService: service.NewGatewayService(
			repo,
			nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
			nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
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

func TestDefaultModelIDsForCompositeIncludesAntigravityDefaults(t *testing.T) {
	antigravityIDs := defaultModelIDsForPlatform(service.PlatformAntigravity)
	require.NotEmpty(t, antigravityIDs)

	compositeIDs := defaultModelIDsForPlatform(service.PlatformComposite)
	require.Contains(t, compositeIDs, antigravityIDs[0])
}

// Scenario: Anthropic defaults contain only Claude while Antigravity keeps its own Gemini models.
func TestDefaultModelIDsForAnthropicExcludeAntigravityGemini(t *testing.T) {
	const antigravityGeminiModel = "gemini-3.7-flash-high"

	anthropicIDs := defaultModelIDsForPlatform(service.PlatformAnthropic)
	require.Contains(t, anthropicIDs, "claude-opus-4-6")
	require.NotContains(t, anthropicIDs, antigravityGeminiModel)

	antigravityIDs := defaultModelIDsForPlatform(service.PlatformAntigravity)
	require.Contains(t, antigravityIDs, antigravityGeminiModel)
}

// Scenario: non-OpenAI groups return a Codex manifest instead of a standard model list.
func TestGatewayCodexModels_NonOpenAIGroupsUseMappedModels(t *testing.T) {
	tests := []struct {
		name       string
		platform   string
		model      string
		efforts    []string
		modalities []string
	}{
		{
			name:       "Grok",
			platform:   service.PlatformGrok,
			model:      "grok-4.6",
			efforts:    []string{"low", "medium", "high", "xhigh"},
			modalities: []string{"text", "image"},
		},
		{
			name:       "DeepSeek",
			platform:   service.PlatformDeepseek,
			model:      "deepseek-v4-pro",
			efforts:    []string{"low", "high", "max"},
			modalities: []string{"text"},
		},
		{
			name:       "provider-qualified Claude",
			platform:   service.PlatformAnthropic,
			model:      "anthropic/claude-sonnet-4-6",
			efforts:    []string{"low", "medium", "high", "max"},
			modalities: []string{"text"},
		},
	}

	for index, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			groupID := int64(100 + index)
			h := newGatewayModelsHandlerForTest(&gatewayModelsAccountRepoStub{
				byGroup: map[int64][]service.Account{
					groupID: {
						{
							ID:       1,
							Platform: tt.platform,
							Credentials: map[string]any{
								"model_mapping": map[string]any{tt.model: tt.model},
							},
						},
					},
				},
			})

			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodGet, "/models?client_version=0.147.0", nil)
			c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
				Group: &service.Group{ID: groupID, Platform: tt.platform},
			})

			h.CodexModels(c)

			require.Equal(t, http.StatusOK, rec.Code)
			var got codexModelsResponseForTest
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
			require.Len(t, got.Models, 1)
			require.Equal(t, tt.model, got.Models[0].Slug)
			require.NotEmpty(t, got.Models[0].ModelMessages)
			require.NotEmpty(t, got.Models[0].TruncationPolicy)
			require.NotNil(t, got.Models[0].AvailabilityNUX)
			require.NotNil(t, got.Models[0].Upgrade)
			require.Equal(t, tt.efforts, codexReasoningEffortsForTest(got.Models[0].SupportedReasoningLevels))
			require.Equal(t, tt.modalities, got.Models[0].InputModalities)
		})
	}
}

// Scenario: Composite manifests aggregate only administrator-configured models.
func TestGatewayCodexModels_CompositeUsesCompleteEffectiveModelList(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const groupID int64 = 120
	h := newGatewayModelsHandlerForTest(&gatewayModelsAccountRepoStub{
		byGroup: map[int64][]service.Account{
			groupID: {
				{
					ID:          3,
					Platform:    service.PlatformOpenAI,
					Status:      service.StatusActive,
					Schedulable: true,
					Credentials: map[string]any{},
				},
				{
					ID:       1,
					Platform: service.PlatformOpenAI,
					Credentials: map[string]any{
						"model_mapping": map[string]any{"gpt-5.5": "gpt-5.5"},
					},
				},
				{
					ID:       2,
					Platform: service.PlatformGrok,
					Credentials: map[string]any{
						"model_mapping": map[string]any{"grok-4.6": "grok-4.6"},
					},
				},
			},
		},
	})

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/models?client_version=0.147.0", nil)
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
		Group: &service.Group{ID: groupID, Platform: service.PlatformComposite},
	})

	h.CodexModels(c)

	require.Equal(t, http.StatusOK, rec.Code)
	var got codexModelsResponseForTest
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Equal(t, []string{"gpt-5.5", "grok-4.6"}, codexModelSlugsForTest(got.Models))
}

func TestGatewayCodexModels_DynamicKeyUsesCandidateGroupUnion(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const firstGroupID int64 = 130
	const secondGroupID int64 = 131
	firstGroup := &service.Group{
		ID:       firstGroupID,
		Platform: service.PlatformOpenAI,
		Status:   service.StatusActive,
		ModelsListConfig: service.GroupModelsListConfig{
			Enabled: true,
			Models:  []string{"first-public-model"},
		},
	}
	secondGroup := &service.Group{
		ID:       secondGroupID,
		Platform: service.PlatformOpenAI,
		Status:   service.StatusActive,
		ModelsListConfig: service.GroupModelsListConfig{
			Enabled: true,
			Models:  []string{"second-public-model"},
		},
	}
	h := newGatewayModelsHandlerForTest(&gatewayModelsAccountRepoStub{byGroup: map[int64][]service.Account{
		firstGroupID: {{
			ID:       1,
			Platform: service.PlatformOpenAI,
			Credentials: map[string]any{
				"model_mapping": map[string]any{"first-public-model": "gpt-5.5"},
			},
		}},
		secondGroupID: {{
			ID:       2,
			Platform: service.PlatformOpenAI,
			Credentials: map[string]any{
				"model_mapping": map[string]any{"second-public-model": "gpt-5.6"},
			},
		}},
	}})

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/models?client_version=0.147.0", nil)
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
		RoutingPlatform: service.PlatformOpenAI,
		RoutingStrategy: service.APIKeyRoutingStrategyBalanced,
		RoutingGroups: []service.APIKeyGroupBinding{
			{GroupID: firstGroupID, Priority: 0, Group: firstGroup},
			{GroupID: secondGroupID, Priority: 1, Group: secondGroup},
		},
	})

	h.CodexModels(c)

	require.Equal(t, http.StatusOK, rec.Code)
	var got codexModelsResponseForTest
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Equal(t, []string{"first-public-model", "second-public-model"}, codexModelSlugsForTest(got.Models))
}

func TestGatewayCodexModels_GeneratedManifestUsesFinalBodyETag(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const groupID int64 = 122
	h := newGatewayModelsHandlerForTest(&gatewayModelsAccountRepoStub{
		byGroup: map[int64][]service.Account{
			groupID: {{
				ID:       1,
				Platform: service.PlatformDeepseek,
				Credentials: map[string]any{
					"model_mapping": map[string]any{"deepseek-v4-pro": "deepseek-v4-pro"},
				},
			}},
		},
	})
	group := &service.Group{ID: groupID, Platform: service.PlatformDeepseek}

	first := httptest.NewRecorder()
	firstContext, _ := gin.CreateTestContext(first)
	firstContext.Request = httptest.NewRequest(http.MethodGet, "/models?client_version=0.147.0", nil)
	firstContext.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{Group: group})
	h.CodexModels(firstContext)

	require.Equal(t, http.StatusOK, first.Code)
	etag := first.Header().Get("ETag")
	require.NotEmpty(t, etag)
	require.Equal(t, service.CodexModelsManifestETag(first.Body.Bytes()), etag)

	second := httptest.NewRecorder()
	secondContext, _ := gin.CreateTestContext(second)
	secondContext.Request = httptest.NewRequest(http.MethodGet, "/models?client_version=0.147.0", nil)
	secondContext.Request.Header.Set("If-None-Match", "W/"+etag)
	secondContext.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{Group: group})
	h.CodexModels(secondContext)

	require.Equal(t, http.StatusNotModified, second.Code)
	require.Empty(t, second.Body.Bytes())
	require.Equal(t, etag, second.Header().Get("ETag"))
}

// Scenario: group models_list_config limits the generated Codex manifest.
func TestGatewayCodexModels_CustomModelsListFiltersCompositeManifest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const groupID int64 = 121
	h := newGatewayModelsHandlerForTest(&gatewayModelsAccountRepoStub{
		byGroup: map[int64][]service.Account{
			groupID: {
				{
					ID:       1,
					Platform: service.PlatformOpenAI,
					Credentials: map[string]any{
						"model_mapping": map[string]any{"gpt-5.5": "gpt-5.5"},
					},
				},
				{
					ID:       2,
					Platform: service.PlatformGrok,
					Credentials: map[string]any{
						"model_mapping": map[string]any{"grok-4.6": "grok-4.6"},
					},
				},
			},
		},
	})

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/models?client_version=0.147.0", nil)
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
		Group: &service.Group{
			ID:       groupID,
			Platform: service.PlatformComposite,
			ModelsListConfig: service.GroupModelsListConfig{
				Enabled: true,
				Models:  []string{"grok-4.6"},
			},
		},
	})

	h.CodexModels(c)

	require.Equal(t, http.StatusOK, rec.Code)
	var got codexModelsResponseForTest
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Equal(t, []string{"grok-4.6"}, codexModelSlugsForTest(got.Models))
}

func codexModelSlugsForTest(models []struct {
	Slug                     string                       `json:"slug"`
	SupportedReasoningLevels []codexReasoningLevelForTest `json:"supported_reasoning_levels"`
	InputModalities          []string                     `json:"input_modalities"`
	ModelMessages            map[string]json.RawMessage   `json:"model_messages"`
	TruncationPolicy         map[string]json.RawMessage   `json:"truncation_policy"`
	AvailabilityNUX          json.RawMessage              `json:"availability_nux"`
	Upgrade                  json.RawMessage              `json:"upgrade"`
}) []string {
	slugs := make([]string, 0, len(models))
	for _, model := range models {
		slugs = append(slugs, model.Slug)
	}
	return slugs
}

func codexReasoningEffortsForTest(levels []codexReasoningLevelForTest) []string {
	efforts := make([]string, 0, len(levels))
	for _, level := range levels {
		efforts = append(efforts, level.Effort)
	}
	return efforts
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

func TestGatewayModels_Grok45AdvertisesReasoningEffortForGrokBuild(t *testing.T) {
	assertGrokGatewayReasoningEfforts(t, 4409, "grok-4.5", []gatewayReasoningEffortOptionForTest{
		{Value: "low", Label: "Low"},
		{Value: "medium", Label: "Medium"},
		{Value: "high", Label: "High", Default: true},
	})
}

func TestGatewayModels_Grok46AdvertisesXHighReasoningEffortForGrokBuild(t *testing.T) {
	xhighEfforts := []gatewayReasoningEffortOptionForTest{
		{Value: "low", Label: "Low"},
		{Value: "medium", Label: "Medium"},
		{Value: "high", Label: "High", Default: true},
		{Value: "xhigh", Label: "xHigh"},
	}
	tests := []struct {
		groupID int64
		model   string
	}{
		{groupID: 4410, model: "grok-4.6"},
		{groupID: 4411, model: "grok-4.6-latest"},
	}
	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			assertGrokGatewayReasoningEfforts(t, tt.groupID, tt.model, xhighEfforts)
		})
	}
}

func assertGrokGatewayReasoningEfforts(t *testing.T, groupID int64, modelID string, want []gatewayReasoningEffortOptionForTest) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	h := newGatewayModelsHandlerForTest(
		&gatewayModelsAccountRepoStub{
			byGroup: map[int64][]service.Account{
				groupID: {
					{
						ID:       1,
						Platform: service.PlatformGrok,
						Credentials: map[string]any{
							"model_mapping": map[string]any{modelID: modelID},
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
		Group: &service.Group{ID: groupID, Platform: service.PlatformGrok},
	})

	h.Models(c)

	require.Equal(t, http.StatusOK, rec.Code)
	var got gatewayModelsResponseForTest
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Len(t, got.Data, 1)
	model := got.Data[0]
	require.Equal(t, modelID, model.ID)
	require.True(t, model.SupportsReasoningEffort)
	require.Equal(t, "high", model.ReasoningEffort)
	require.Equal(t, want, model.ReasoningEfforts)
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

// Scenario: a Composite group with only Anthropic accounts must not inherit Antigravity Gemini defaults.
func TestGatewayCodexModels_CompositeAnthropicDoesNotAdvertiseAntigravityDefaults(t *testing.T) {
	gin.SetMode(gin.TestMode)

	groupID := int64(64)
	h := newGatewayModelsHandlerForTest(&gatewayModelsAccountRepoStub{
		byGroup: map[int64][]service.Account{
			groupID: {{ID: 1, Platform: service.PlatformAnthropic}},
		},
	})

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/models?client_version=0.147.0", nil)
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
		Group: &service.Group{ID: groupID, Platform: service.PlatformComposite},
	})

	h.CodexModels(c)

	require.Equal(t, http.StatusOK, rec.Code)
	var got codexModelsResponseForTest
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	slugs := codexModelSlugsForTest(got.Models)
	require.Contains(t, slugs, "claude-opus-4-6")
	require.NotContains(t, slugs, "gemini-2.5-flash")
}

// Scenario: Antigravity retains its own Claude and Gemini defaults inside Composite groups.
func TestGatewayModels_CompositeAntigravityAdvertisesAntigravityDefaults(t *testing.T) {
	gin.SetMode(gin.TestMode)

	groupID := int64(65)
	h := newGatewayModelsHandlerForTest(&gatewayModelsAccountRepoStub{
		byGroup: map[int64][]service.Account{
			groupID: {{ID: 1, Platform: service.PlatformAntigravity}},
		},
	})

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
		Group: &service.Group{ID: groupID, Platform: service.PlatformComposite},
	})

	h.Models(c)

	require.Equal(t, http.StatusOK, rec.Code)
	var got gatewayModelsResponseForTest
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	ids := modelIDsForTest(got.Data)
	require.Contains(t, ids, "claude-opus-4-6")
	require.Contains(t, ids, "gemini-2.5-flash")
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

func TestGatewayModels_CompositeCustomModelsListFiltersAcrossConcretePlatforms(t *testing.T) {
	gin.SetMode(gin.TestMode)

	groupID := int64(33)
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
								"gpt-5.5": "gpt-5.5",
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
					{
						ID:       3,
						Platform: service.PlatformAntigravity,
						Credentials: map[string]any{
							"model_mapping": map[string]any{
								"ag-custom-model": "ag-custom-model",
							},
						},
					},
					{
						ID:       4,
						Platform: service.PlatformKimi,
						Credentials: map[string]any{
							"model_mapping": map[string]any{"kimi-custom": "kimi-upstream"},
						},
					},
					{
						ID:       5,
						Platform: service.PlatformZhipu,
						Credentials: map[string]any{
							"model_mapping": map[string]any{"glm-custom": "glm-upstream"},
						},
					},
					{
						ID:       6,
						Platform: service.PlatformDeepseek,
						Credentials: map[string]any{
							"model_mapping": map[string]any{"deepseek-custom": "deepseek-upstream"},
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
			Platform: service.PlatformComposite,
			ModelsListConfig: service.GroupModelsListConfig{
				Enabled: true,
				Models:  []string{"gemini-2.5-flash", "missing-model", "ag-custom-model", "gpt-5.5", "kimi-custom", "glm-custom", "deepseek-custom"},
			},
		},
	})

	h.Models(c)

	require.Equal(t, http.StatusOK, rec.Code)

	var got gatewayModelsResponseForTest
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Equal(t, []string{"gemini-2.5-flash", "ag-custom-model", "gpt-5.5", "kimi-custom", "glm-custom", "deepseek-custom"}, modelIDsForTest(got.Data))
}

func TestGatewayModels_CompositeUnmappedAccountsFallbackToLinkedPlatformsOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)

	groupID := int64(34)
	h := newGatewayModelsHandlerForTest(
		&gatewayModelsAccountRepoStub{
			byGroup: map[int64][]service.Account{
				groupID: {
					{ID: 1, Platform: service.PlatformOpenAI},
					{ID: 2, Platform: service.PlatformGrok},
				},
			},
		},
	)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
		Group: &service.Group{ID: groupID, Platform: service.PlatformComposite},
	})

	h.Models(c)

	require.Equal(t, http.StatusOK, rec.Code)

	var got gatewayModelsResponseForTest
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))

	ids := modelIDsForTest(got.Data)
	require.Contains(t, ids, "gpt-5.5")
	require.Contains(t, ids, "grok-4.3")
	require.NotContains(t, ids, "claude-sonnet-4-6")
	require.NotContains(t, ids, "gemini-2.5-flash")
}

// CN 供应商没有静态默认模型列表：composite 下无映射的可调度 CN 账号不得把
// defaultModelIDsForPlatform default 分支的 Claude 列表挂到 CN 平台名下。
func TestGatewayModels_CompositeUnmappedCNAccountsContributeNoDefaults(t *testing.T) {
	gin.SetMode(gin.TestMode)

	groupID := int64(35)
	h := newGatewayModelsHandlerForTest(
		&gatewayModelsAccountRepoStub{
			byGroup: map[int64][]service.Account{
				groupID: {
					{ID: 1, Platform: service.PlatformOpenAI},
					{ID: 2, Platform: service.PlatformKimi},
					{ID: 3, Platform: service.PlatformZhipu},
					{ID: 4, Platform: service.PlatformDeepseek},
				},
			},
		},
	)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
		Group: &service.Group{ID: groupID, Platform: service.PlatformComposite},
	})

	h.Models(c)

	require.Equal(t, http.StatusOK, rec.Code)

	var got gatewayModelsResponseForTest
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))

	ids := modelIDsForTest(got.Data)
	require.Contains(t, ids, "gpt-5.5")
	require.NotContains(t, ids, "claude-sonnet-4-6")
}

// 独立 CN 分组沿用 default 分支的 Claude 默认列表（Claude Code 客户端请求的
// 就是这些模型名并经账号 model_mapping 转换），composite 支持不得改变该回退。
func TestDefaultModelIDsForPlatform_CNProvidersKeepClaudeDefaults(t *testing.T) {
	want := make([]string, 0, len(claude.DefaultModels))
	for _, model := range claude.DefaultModels {
		want = append(want, model.ID)
	}
	for _, platform := range []string{service.PlatformKimi, service.PlatformZhipu, service.PlatformDeepseek} {
		require.Equal(t, want, defaultModelIDsForPlatform(platform), "platform=%s", platform)
	}
}

func TestDefaultCodexModelIDsForPlatform_DeepSeekUsesDeepSeekModels(t *testing.T) {
	require.Equal(t, []string{"deepseek-v4-pro", "deepseek-v4-flash"}, defaultCodexModelIDsForPlatform(service.PlatformDeepseek))
	require.Equal(t, defaultModelIDsForPlatform(service.PlatformAnthropic), defaultCodexModelIDsForPlatform(service.PlatformAnthropic))
}

func TestGatewayCodexModels_DeepSeekWithoutMappingUsesDeepSeekDefaults(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const groupID int64 = 130
	h := newGatewayModelsHandlerForTest(&gatewayModelsAccountRepoStub{
		byGroup: map[int64][]service.Account{
			groupID: {
				{
					ID:          1,
					Platform:    service.PlatformDeepseek,
					Status:      service.StatusActive,
					Schedulable: true,
					Credentials: map[string]any{},
				},
			},
		},
	})

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/models?client_version=0.150.0", nil)
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
		Group: &service.Group{ID: groupID, Platform: service.PlatformDeepseek},
	})

	h.CodexModels(c)

	require.Equal(t, http.StatusOK, rec.Code)
	var got codexModelsResponseForTest
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	slugs := make([]string, 0, len(got.Models))
	for _, model := range got.Models {
		slugs = append(slugs, model.Slug)
	}
	require.Contains(t, slugs, "deepseek-v4-pro")
	require.Contains(t, slugs, "deepseek-v4-flash")
	require.NotContains(t, slugs, "claude-sonnet-4-6")
	require.NotContains(t, slugs, "claude-opus-4-6")
}

func TestGatewayCodexModels_OmitsWildcardMappingKeys(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const groupID int64 = 131
	h := newGatewayModelsHandlerForTest(&gatewayModelsAccountRepoStub{
		byGroup: map[int64][]service.Account{
			groupID: {
				{
					ID:       1,
					Platform: service.PlatformDeepseek,
					Credentials: map[string]any{
						"model_mapping": map[string]any{
							"foo-*":           "deepseek-v4-pro",
							"deepseek-v4-pro": "deepseek-v4-pro",
						},
					},
				},
			},
		},
	})

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/models?client_version=0.150.0", nil)
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
		Group: &service.Group{ID: groupID, Platform: service.PlatformDeepseek},
	})

	h.CodexModels(c)

	require.Equal(t, http.StatusOK, rec.Code)
	var got codexModelsResponseForTest
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	slugs := make([]string, 0, len(got.Models))
	for _, model := range got.Models {
		slugs = append(slugs, model.Slug)
	}
	require.Equal(t, []string{"deepseek-v4-pro"}, slugs)
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

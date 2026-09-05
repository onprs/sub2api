package antigravity

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFallbackCatalogModels_MatchesAgyUserCatalog(t *testing.T) {
	wantIDs := []string{
		"gemini-3.8-flash", "gemini-3.7-flash", "gemini-3.6-flash", "gemini-3.5-flash", "gemini-3.1-pro",
		"claude-fable-5-1", "claude-sonnet-4-6", "claude-opus-4-6-thinking", "gpt-oss-120b-medium",
	}
	models := FallbackCatalogModels()
	var gotIDs []string
	for _, model := range models {
		gotIDs = append(gotIDs, model.ID)
		require.Equal(t, "model", model.Type)
		require.Equal(t, CatalogSourceFallback, model.Source)
		if len(model.ReasoningEfforts) > 0 {
			require.Empty(t, model.WireModel)
			require.Empty(t, model.InternalModel)
			require.Nil(t, model.ThinkingBudget)
			require.Equal(t, model.ID, model.ResponseModel)
		}
	}
	require.Equal(t, wantIDs, gotIDs)
	require.Equal(t, []string{"high", "medium", "low"}, models[0].ReasoningEfforts)
	require.Equal(t, []string{"high", "low"}, models[4].ReasoningEfforts)
}

func TestFallbackCatalogModels_ReturnsIndependentSlice(t *testing.T) {
	first := FallbackCatalogModels()
	first[0].ID = "changed"
	first[0].ReasoningEfforts[0] = "changed"

	second := FallbackCatalogModels()
	require.Equal(t, "gemini-3.8-flash", second[0].ID)
	require.Equal(t, "high", second[0].ReasoningEfforts[0])
}

func TestCatalogModelsFromResponse_ExpandsTierAndNormalizesRawAliases(t *testing.T) {
	body := []byte(`{
		"models": {
			"gemini-3.7-flash-tiered": {"displayName":"Gemini 3.7 Flash","model":"MODEL_PLACEHOLDER_M301"},
			"gemini-3.6-flash-high": {"model":"MODEL_PLACEHOLDER_M71"},
			"gemini-3.6-flash-medium": {"model":"MODEL_PLACEHOLDER_M72"},
			"gemini-3.6-flash-low": {"model":"MODEL_PLACEHOLDER_M73"},
			"gemini-3-flash-agent": {"model":"MODEL_PLACEHOLDER_M84"},
			"gemini-3.5-flash-low": {"model":"MODEL_PLACEHOLDER_M20"},
			"gemini-3.5-flash-extra-low": {"model":"MODEL_PLACEHOLDER_M187"},
			"gemini-pro-agent": {"model":"MODEL_PLACEHOLDER_M16"},
			"gemini-3.1-pro-low": {"model":"MODEL_PLACEHOLDER_M36"},
			"claude-sonnet-4-6": {"model":"MODEL_PLACEHOLDER_M35","vertexModelId":"claude-sonnet-4-6@default"},
			"claude-opus-4-6-thinking": {"model":"MODEL_PLACEHOLDER_M26","vertexModelId":"claude-opus-4-6@default"},
			"gpt-oss-120b-medium": {"model":"MODEL_OPENAI_GPT_OSS_120B_MEDIUM","vertexModelId":"openai/gpt-oss-120b-maas"},
			"future_opaque_42": {"model":"MODEL_PLACEHOLDER_M999"},
			"chat_20706": {"model":"MODEL_CHAT_20706"}
		},
		"tieredModelIds": {"flash":["gemini-3.7-flash-tiered"]},
		"tabModelIds": ["chat_20706"],
		"commandModelIds": ["gemini-3-flash"],
		"agentModelSorts": [{"displayName":"Recommended","groups":[{"modelIds":["gemini-3.6-flash-high"]}]}]
	}`)

	var response FetchAvailableModelsResponse
	require.NoError(t, json.Unmarshal(body, &response))
	var raw map[string]any
	require.NoError(t, json.Unmarshal(body, &raw))

	models := CatalogModelsFromResponse(&response, raw)
	require.Len(t, models, 7)
	require.Equal(t, []string{"gemini-3.7-flash", "gemini-3.6-flash", "gemini-3.5-flash"}, []string{models[0].ID, models[1].ID, models[2].ID})
	require.Equal(t, []string{"high", "medium", "low"}, models[0].ReasoningEfforts)
	require.Empty(t, models[0].WireModel)
	require.Empty(t, models[0].InternalModel)
	require.NotNil(t, models[0].Metadata)

	require.Equal(t, []string{"gemini-3.7-flash-tiered"}, response.TieredModelIDs["flash"])
	require.Equal(t, []string{"chat_20706"}, response.TabModelIDs)
	require.Equal(t, []string{"gemini-3-flash"}, response.CommandModelIDs)
	require.Equal(t, []string{"gemini-3.6-flash-high"}, response.AgentModelSorts[0].Groups[0].ModelIDs)

	rawModels := RawCatalogModelsFromResponse(&response, raw)
	require.Len(t, rawModels, 14)
	byID := make(map[string]CatalogModel, len(rawModels))
	for _, model := range rawModels {
		byID[model.ID] = model
	}
	require.Equal(t, "MODEL_PLACEHOLDER_M999", byID["future_opaque_42"].InternalModel)
	require.Equal(t, "MODEL_CHAT_20706", byID["chat_20706"].InternalModel)
}

func TestCatalogModelsFromResponse_RequiresAdvertisedTierGroup(t *testing.T) {
	response := &FetchAvailableModelsResponse{
		Models: map[string]ModelInfo{
			"gemini-3.7-flash-tiered": {Model: "MODEL_PLACEHOLDER_M301"},
		},
		TieredModelIDs: map[string][]string{"flash": {"other-tiered-model"}},
	}

	require.Empty(t, CatalogModelsFromResponse(response, nil))
}

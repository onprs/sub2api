package antigravity

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFallbackCatalogModels_MatchesAgyUserCatalog(t *testing.T) {
	wantIDs := []string{
		"gemini-3.7-flash-high",
		"gemini-3.7-flash-medium",
		"gemini-3.7-flash-low",
		"gemini-3.6-flash-high",
		"gemini-3.6-flash-medium",
		"gemini-3.6-flash-low",
		"gemini-3.5-flash-high",
		"gemini-3.5-flash-medium",
		"gemini-3.5-flash-low",
		"gemini-3.1-pro-high",
		"gemini-3.1-pro-low",
		"claude-fable-5-1",
		"claude-sonnet-4-6",
		"claude-opus-4-6-thinking",
		"gpt-oss-120b-medium",
	}

	models := FallbackCatalogModels()
	require.Len(t, models, len(wantIDs))

	gotIDs := make([]string, 0, len(models))
	for _, model := range models {
		gotIDs = append(gotIDs, model.ID)
		require.Equal(t, "model", model.Type)
		require.Equal(t, CatalogSourceFallback, model.Source)
		require.NotEmpty(t, model.CatalogID)
		require.NotEmpty(t, model.WireModel)
		if model.ID != "claude-fable-5-1" {
			require.NotEmpty(t, model.InternalModel)
			require.NotNil(t, model.ThinkingBudget)
		}
	}
	require.Equal(t, wantIDs, gotIDs)

	require.Equal(t, "gemini-3.7-flash-tiered", models[0].CatalogID)
	require.Equal(t, "gemini-3.7-flash-high", models[0].WireModel)
	require.Equal(t, "MODEL_PLACEHOLDER_M298", models[0].InternalModel)
	require.Equal(t, "gemini-3.7-flash", models[0].ResponseModel)
	require.Equal(t, -1, *models[0].ThinkingBudget)

	require.Equal(t, "gemini-3-flash-agent", models[6].WireModel)
	require.Equal(t, "gemini-3.5-flash-low", models[7].WireModel)
	require.Equal(t, "gemini-3.5-flash-extra-low", models[8].WireModel)
	require.Equal(t, "gemini-pro-agent", models[9].WireModel)
	require.Equal(t, "openai/gpt-oss-120b-maas", models[14].BackendModel)
}

func TestFallbackCatalogModels_ReturnsIndependentSlice(t *testing.T) {
	first := FallbackCatalogModels()
	first[0].ID = "changed"
	first[0].ThinkingBudget = nil

	second := FallbackCatalogModels()
	require.Equal(t, "gemini-3.7-flash-high", second[0].ID)
	require.NotNil(t, second[0].ThinkingBudget)
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
	require.Len(t, models, 14)
	require.Equal(t, []string{"gemini-3.7-flash-high", "gemini-3.7-flash-medium", "gemini-3.7-flash-low"}, []string{models[0].ID, models[1].ID, models[2].ID})
	require.Equal(t, []string{"MODEL_PLACEHOLDER_M298", "MODEL_PLACEHOLDER_M299", "MODEL_PLACEHOLDER_M300"}, []string{models[0].InternalModel, models[1].InternalModel, models[2].InternalModel})
	require.Equal(t, "MODEL_PLACEHOLDER_M20", models[7].InternalModel)
	require.Equal(t, "gemini-3.5-flash-low", models[7].WireModel)
	require.Equal(t, "gemini-3.5-flash-extra-low", models[8].WireModel)
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

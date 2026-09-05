package openai

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDefaultModelsIncludeBareGPT56Alias(t *testing.T) {
	require.Contains(t, DefaultModelIDs(), "gpt-5.6")
}

func TestDefaultModelsIncludeCodexAstraWithoutAPIOnlyAdditions(t *testing.T) {
	ids := DefaultModelIDs()
	require.Contains(t, ids, "gpt-6-astra")
	for _, id := range []string{"gpt-5.6-cyber", "gpt-5.5-pro", "gpt-5.4-pro", "gpt-5.4-nano"} {
		require.NotContains(t, ids, id)
	}
	seen := make(map[string]bool)
	for _, id := range ids {
		require.False(t, seen[id], id)
		seen[id] = true
	}
}

func TestDefaultModelsIncludeGPT6Astra(t *testing.T) {
	require.Contains(t, DefaultModelIDs(), "gpt-6-astra")
	require.Contains(t, DefaultModelIDs(), "gpt-6")
	var displayName string
	for _, model := range DefaultModels {
		if model.ID == "gpt-6-astra" {
			displayName = model.DisplayName
			break
		}
	}
	require.Equal(t, "GPT-6 Astra", displayName)
}

func TestDefaultModelsPreferConcreteGPT56SolForAccountTests(t *testing.T) {
	require.NotEmpty(t, DefaultModels)
	require.Equal(t, "gpt-5.6-sol", DefaultModels[0].ID)
}

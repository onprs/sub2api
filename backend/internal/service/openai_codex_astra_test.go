package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCodexAstraCatalogAndForwardingMetadata(t *testing.T) {
	for _, model := range []string{"gpt-6-astra", "openai/gpt-6-astra", "GPT-6-ASTRA"} {
		t.Run(model, func(t *testing.T) {
			require.Equal(t, "gpt-6-astra", normalizeKnownOpenAICodexModel(model))
			require.True(t, isOpenAICodexReasoningGPTModel(model))
			require.True(t, isOpenAICodexImageInputModel(model))
			require.Equal(t, "max", normalizeOpenAIReasoningEffortForModel("max", model))
			descriptor := newConfiguredCodexModelDescriptor(model)
			require.Equal(t, "GPT-6 Astra", descriptor.DisplayName)
			require.Equal(t, []string{"low", "medium", "high", "xhigh", "max"}, effortsFromConfiguredCodexLevels(descriptor.SupportedReasoningLevels))
			require.Equal(t, int64(1_050_000), descriptor.MaxContextWindow)
			require.Equal(t, model, descriptor.Slug)
		})
	}

	body, err := buildCodexModelsManifestForAccounts(PlatformOpenAI, []string{"gpt-6-astra"}, []Account{{
		ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
	}}, nil, true)
	require.NoError(t, err)
	models := decodeCodexManifestModels(t, body)
	require.Len(t, models, 1)
	require.Equal(t, []any{"text", "image"}, models[0]["input_modalities"])
	require.Equal(t, "xhigh", normalizeOpenAIReasoningEffortForModel("max", "gpt-5.4"))
	require.False(t, isOpenAIGPT6AstraModel("gpt-6-unknown"))
}

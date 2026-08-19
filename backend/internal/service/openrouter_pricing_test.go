package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpenRouterPricing(t *testing.T) {
	// Builtin 17 free models
	models := OpenRouterFallbackModelIDs()
	for _, m := range models {
		pricing, ok := openRouterReferencePricing(m)
		require.True(t, ok, "model %s should have reference pricing", m)
		require.NotNil(t, pricing)
		require.Equal(t, float64(0), pricing.InputPricePerToken)
		require.Equal(t, float64(0), pricing.OutputPricePerToken)
	}

	// Arbitrary :free model
	freePricing, ok := openRouterReferencePricing("some-org/some-model:free")
	require.True(t, ok)
	require.Equal(t, float64(0), freePricing.InputPricePerToken)

	// openrouter/free
	orFree, ok := openRouterReferencePricing("openrouter/free")
	require.True(t, ok)
	require.Equal(t, float64(0), orFree.InputPricePerToken)

	// Unknown non-free model returns false
	_, ok = openRouterReferencePricing("openai/gpt-4o")
	require.False(t, ok)
}

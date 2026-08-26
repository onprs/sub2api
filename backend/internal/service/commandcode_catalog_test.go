package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func nowForTest() time.Time {
	return time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
}

func TestParseCommandCodeCatalogCollectsModelIDs(t *testing.T) {
	models, err := parseCommandCodeCatalog([]byte(`{
		"object":"list",
		"data":[
			{"id":"claude-sonnet-5","object":"model","owned_by":"command-code","name":"Claude Sonnet 5","context_length":1000000},
			{"id":"deepseek/deepseek-v4-flash","object":"model","owned_by":"command-code","name":"DeepSeek V4 Flash","context_length":1000000},
			{"id":"deepseek/deepseek-v4-flash","object":"model"},
			{"id":"  ","object":"model"}
		]
	}`))
	require.NoError(t, err)
	require.Equal(t, []string{"claude-sonnet-5", "deepseek/deepseek-v4-flash"}, models)
}

func TestParseCommandCodeCatalogRejectsInvalidPayloads(t *testing.T) {
	_, err := parseCommandCodeCatalog([]byte(`{"object":"list","data":[]}`))
	require.Error(t, err)

	_, err = parseCommandCodeCatalog([]byte(`not-json`))
	require.Error(t, err)
}

func TestCommandCodeFallbackCatalogHasAllPricedModels(t *testing.T) {
	for _, model := range CommandCodeFallbackModelIDs() {
		_, ok := commandCodeReferencePricingAt(model, nowForTest())
		require.True(t, ok, "fallback catalog model %q has no pricing entry", model)
	}
}

func TestCommandCodeCatalogExposesFallbackWithoutNetwork(t *testing.T) {
	// nil client 时仍返回内置回退目录。
	catalog := NewCommandCodeCatalog(nil)
	models := catalog.ModelIDs(context.TODO())
	require.NotEmpty(t, models)
	require.Contains(t, models, "claude-sonnet-5")
	require.Contains(t, models, "deepseek/deepseek-v4-flash")
}

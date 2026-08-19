package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpenRouterCatalogBuiltins(t *testing.T) {
	models := OpenRouterFallbackModelIDs()
	expected := []string{
		"dots-studio/dots-3-note-preview:free",
		"google/gemma-4-26b-a4b-it:free",
		"google/gemma-4-31b-it:free",
		"liquid/lfm-2.5-2.6b:free",
		"nvidia/nemotron-3.5-lightning:free",
		"nvidia/nemotron-3.5-content-safety:free",
		"nvidia/nemotron-3-ultra-550b-a55b:free",
		"nvidia/nemotron-3-nano-omni-30b-a3b-reasoning:free",
		"nvidia/nemotron-3-super-120b-a12b:free",
		"nvidia/nemotron-3-nano-30b-a3b:free",
		"nvidia/nemotron-nano-12b-v2-vl:free",
		"nvidia/nemotron-nano-9b-v2:free",
		"openai/gpt-oss-20b:free",
		"openrouter/free",
		"poolside/laguna-s-2.1:free",
		"poolside/laguna-xs-2.1:free",
		"z-ai/glm-5.2:free",
	}

	require.Len(t, models, len(expected))
	for _, exp := range expected {
		require.Contains(t, models, exp)
	}
}

func TestOpenRouterCatalogFetch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"data": [
				{"id": "meta-llama/llama-3-8b-instruct:free"},
				{"id": "google/gemma-2-9b-it:free"}
			]
		}`))
	}))
	defer server.Close()

	catalog := NewOpenRouterCatalog(server.Client())
	catalog.endpoint = server.URL

	models, err := catalog.ForceRefresh(context.Background())
	require.NoError(t, err)
	require.Equal(t, []string{"google/gemma-2-9b-it:free", "meta-llama/llama-3-8b-instruct:free"}, models)
}

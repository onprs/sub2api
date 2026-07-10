package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type pricingTestRemoteClient struct {
	pricingBodies map[string][]byte
	hashText      string
	fetchedURLs   []string
}

type openCodeGoCatalogRoundTripper map[string]string

func (rt openCodeGoCatalogRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	body, ok := rt[req.URL.String()]
	if !ok {
		return &http.Response{
			StatusCode: http.StatusNotFound,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`not found`)),
			Request:    req,
		}, nil
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}, nil
}

func (c *pricingTestRemoteClient) FetchPricingJSON(_ context.Context, url string) ([]byte, error) {
	c.fetchedURLs = append(c.fetchedURLs, url)
	if body, ok := c.pricingBodies[url]; ok {
		return body, nil
	}
	if body, ok := c.pricingBodies[strings.TrimRight(url, "/")]; ok {
		return body, nil
	}
	return c.pricingBodies[strings.TrimRight(url, "/")+"/"], nil
}

func (c *pricingTestRemoteClient) FetchHashText(_ context.Context, _ string) (string, error) {
	return c.hashText, nil
}

func TestParsePricingData_ParsesPriorityAndServiceTierFields(t *testing.T) {
	svc := &PricingService{}
	body := []byte(`{
		"gpt-5.4": {
			"input_cost_per_token": 0.0000025,
			"input_cost_per_token_priority": 0.000005,
			"output_cost_per_token": 0.000015,
			"output_cost_per_token_priority": 0.00003,
			"cache_creation_input_token_cost": 0.0000025,
			"cache_read_input_token_cost": 0.00000025,
			"cache_read_input_token_cost_priority": 0.0000005,
			"supports_service_tier": true,
			"supports_prompt_caching": true,
			"litellm_provider": "openai",
			"mode": "chat"
		}
	}`)

	data, err := svc.parsePricingData(body)
	require.NoError(t, err)
	pricing := data["gpt-5.4"]
	require.NotNil(t, pricing)
	require.InDelta(t, 5e-6, pricing.InputCostPerTokenPriority, 1e-12)
	require.InDelta(t, 3e-5, pricing.OutputCostPerTokenPriority, 1e-12)
	require.InDelta(t, 5e-7, pricing.CacheReadInputTokenCostPriority, 1e-12)
	require.True(t, pricing.SupportsServiceTier)
}

func TestGetModelPricing_Gpt53CodexSparkUsesGpt51CodexPricing(t *testing.T) {
	sparkPricing := &LiteLLMModelPricing{InputCostPerToken: 1}
	gpt53Pricing := &LiteLLMModelPricing{InputCostPerToken: 9}

	svc := &PricingService{
		pricingData: map[string]*LiteLLMModelPricing{
			"gpt-5.1-codex": sparkPricing,
			"gpt-5.3":       gpt53Pricing,
		},
	}

	got := svc.GetModelPricing("gpt-5.3-codex-spark")
	require.Same(t, sparkPricing, got)
}

func TestGetModelPricing_Gpt53CodexFallbackStillUsesGpt52Codex(t *testing.T) {
	gpt52CodexPricing := &LiteLLMModelPricing{InputCostPerToken: 2}

	svc := &PricingService{
		pricingData: map[string]*LiteLLMModelPricing{
			"gpt-5.2-codex": gpt52CodexPricing,
		},
	}

	got := svc.GetModelPricing("gpt-5.3-codex")
	require.Same(t, gpt52CodexPricing, got)
}

func TestGetModelPricing_OpenAIFallbackMatchedLoggedAsInfo(t *testing.T) {
	logSink, restore := captureStructuredLog(t)
	defer restore()

	gpt52CodexPricing := &LiteLLMModelPricing{InputCostPerToken: 2}
	svc := &PricingService{
		pricingData: map[string]*LiteLLMModelPricing{
			"gpt-5.2-codex": gpt52CodexPricing,
		},
	}

	got := svc.GetModelPricing("gpt-5.3-codex")
	require.Same(t, gpt52CodexPricing, got)

	require.True(t, logSink.ContainsMessageAtLevel("[Pricing] OpenAI fallback matched gpt-5.3-codex -> gpt-5.2-codex", "info"))
	require.False(t, logSink.ContainsMessageAtLevel("[Pricing] OpenAI fallback matched gpt-5.3-codex -> gpt-5.2-codex", "warn"))
}

func TestGetModelPricing_Gpt54UsesStaticFallbackWhenRemoteMissing(t *testing.T) {
	svc := &PricingService{
		pricingData: map[string]*LiteLLMModelPricing{
			"gpt-5.1-codex": &LiteLLMModelPricing{InputCostPerToken: 1.25e-6},
		},
	}

	got := svc.GetModelPricing("gpt-5.4")
	require.NotNil(t, got)
	require.InDelta(t, 2.5e-6, got.InputCostPerToken, 1e-12)
	require.InDelta(t, 1.5e-5, got.OutputCostPerToken, 1e-12)
	require.InDelta(t, 2.5e-7, got.CacheReadInputTokenCost, 1e-12)
	require.Equal(t, 272000, got.LongContextInputTokenThreshold)
	require.InDelta(t, 2.0, got.LongContextInputCostMultiplier, 1e-12)
	require.InDelta(t, 1.5, got.LongContextOutputCostMultiplier, 1e-12)
}

func TestGetModelPricing_OpenAICompactAliasUsesStaticFallback(t *testing.T) {
	svc := &PricingService{
		pricingData: map[string]*LiteLLMModelPricing{
			"gpt-5.1-codex": {InputCostPerToken: 1.25e-6},
		},
	}

	got := svc.GetModelPricing("openai/gpt5.5")
	require.NotNil(t, got)
	require.InDelta(t, 2.5e-6, got.InputCostPerToken, 1e-12)
	require.InDelta(t, 1.5e-5, got.OutputCostPerToken, 1e-12)
}

func TestDefaultPricingIncludesCodexAutoReview(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "resources", "model-pricing", "model_prices_and_context_window.json"))
	require.NoError(t, err)

	svc := &PricingService{}
	pricingData, err := svc.parsePricingData(data)
	require.NoError(t, err)
	svc.pricingData = pricingData

	got := svc.GetModelPricing("codex-auto-review")
	require.NotNil(t, got)
	require.InDelta(t, 5e-6, got.InputCostPerToken, 1e-12)
	require.InDelta(t, 3e-5, got.OutputCostPerToken, 1e-12)
	require.InDelta(t, 5e-7, got.CacheReadInputTokenCost, 1e-12)
}

func TestGetModelPricing_Gpt54MiniUsesDedicatedStaticFallbackWhenRemoteMissing(t *testing.T) {
	svc := &PricingService{
		pricingData: map[string]*LiteLLMModelPricing{
			"gpt-5.1-codex": {InputCostPerToken: 1.25e-6},
		},
	}

	got := svc.GetModelPricing("gpt-5.4-mini")
	require.NotNil(t, got)
	require.InDelta(t, 7.5e-7, got.InputCostPerToken, 1e-12)
	require.InDelta(t, 4.5e-6, got.OutputCostPerToken, 1e-12)
	require.InDelta(t, 7.5e-8, got.CacheReadInputTokenCost, 1e-12)
	require.Zero(t, got.LongContextInputTokenThreshold)
}

func TestParseOpenCodeGoPricingDocument_MapsOfficialModelIDsAndPrices(t *testing.T) {
	body := []byte(`
<p>The estimates are also based on the following prices per 1M tokens:</p>
<table><thead><tr><th>Model</th><th>Input</th><th>Output</th><th>Cached Read</th><th>Cached Write</th></tr></thead><tbody>
<tr><td>GLM-5.2</td><td>$1.40</td><td>$4.40</td><td>$0.26</td><td>-</td></tr>
<tr><td>Kimi K2.7 Code</td><td>$0.95</td><td>$4.00</td><td>$0.19</td><td>-</td></tr>
<tr><td>Qwen3.7 Plus (&lt;= 256K tokens)</td><td>$0.40</td><td>$1.60</td><td>$0.04</td><td>$0.50</td></tr>
<tr><td>Qwen3.7 Plus (&gt; 256K tokens)</td><td>$1.20</td><td>$4.80</td><td>$0.12</td><td>$1.50</td></tr>
</tbody></table>
<table><thead><tr><th>Model</th><th>Model ID</th><th>Endpoint</th></tr></thead><tbody>
<tr><td>GLM-5.2</td><td>glm-5.2</td><td><code>https://opencode.ai/zen/go/v1/chat/completions</code></td></tr>
<tr><td>Kimi K2.7</td><td>kimi-k2.7</td><td><code>https://opencode.ai/zen/go/v1/chat/completions</code></td></tr>
<tr><td>Qwen3.7 Plus</td><td>qwen3.7-plus</td><td><code>https://opencode.ai/zen/go/v1/messages</code></td></tr>
</tbody></table>`)

	pricing, err := parseOpenCodeGoPricingDocument(body)
	require.NoError(t, err)

	glm := pricing["glm-5.2"]
	require.NotNil(t, glm)
	require.InDelta(t, 1.40e-6, glm.InputCostPerToken, 1e-12)
	require.InDelta(t, 4.40e-6, glm.OutputCostPerToken, 1e-12)
	require.InDelta(t, 0.26e-6, glm.CacheReadInputTokenCost, 1e-12)
	require.Equal(t, PlatformOpenCodeGo, glm.LiteLLMProvider)

	kimi := pricing["kimi-k2.7"]
	require.NotNil(t, kimi)
	require.InDelta(t, 0.95e-6, kimi.InputCostPerToken, 1e-12)
	require.InDelta(t, 4.00e-6, kimi.OutputCostPerToken, 1e-12)
	require.InDelta(t, 0.19e-6, kimi.CacheReadInputTokenCost, 1e-12)
	require.Zero(t, kimi.CacheCreationInputTokenCost)
	require.Equal(t, PlatformOpenCodeGo, kimi.LiteLLMProvider)

	qwen := pricing["qwen3.7-plus"]
	require.NotNil(t, qwen)
	require.InDelta(t, 0.40e-6, qwen.InputCostPerToken, 1e-12)
	require.InDelta(t, 1.60e-6, qwen.OutputCostPerToken, 1e-12)
	require.InDelta(t, 0.04e-6, qwen.CacheReadInputTokenCost, 1e-12)
	require.InDelta(t, 0.50e-6, qwen.CacheCreationInputTokenCost, 1e-12)
	require.Equal(t, 256000, qwen.LongContextInputTokenThreshold)
	require.InDelta(t, 3.0, qwen.LongContextInputCostMultiplier, 1e-12)
	require.InDelta(t, 3.0, qwen.LongContextOutputCostMultiplier, 1e-12)
}

func TestParseOpenCodeGoCatalogDocument_MapsOfficialEndpointProtocols(t *testing.T) {
	body := []byte(`
<table><thead><tr><th>Model</th><th>Model ID</th><th>Endpoint</th></tr></thead><tbody>
<tr><td>GLM-5.2</td><td>glm-5.2</td><td><code>https://opencode.ai/zen/go/v1/chat/completions</code></td></tr>
<tr><td>Qwen3.7 Plus</td><td>qwen3.7-plus</td><td><code>https://opencode.ai/zen/go/v1/messages</code></td></tr>
</tbody></table>`)

	catalog := parseOpenCodeGoCatalogDocument(body)

	require.Equal(t, OpenCodeGoProtocolChatCompletions, catalog["glm-5.2"].Protocol)
	require.Equal(t, OpenCodeGoProtocolMessages, catalog["qwen3.7-plus"].Protocol)
}

func TestParseOpenCodeGoModelsEndpoint_CollectsOfficialModelIDs(t *testing.T) {
	body := []byte(`{"object":"list","data":[{"id":"glm-5.2"},{"id":"qwen3.5-plus"},{"id":"hy3-preview"}]}`)

	ids, err := parseOpenCodeGoModelsEndpoint(body)

	require.NoError(t, err)
	require.Equal(t, []string{"glm-5.2", "hy3-preview", "qwen3.5-plus"}, ids)
}

func TestOpenCodeGoCatalogFetchUsesOfficialModelsAsModelIDAuthority(t *testing.T) {
	catalog := newOpenCodeGoCatalog(map[string]OpenCodeGoCatalogEntry{
		"legacy-only": {ID: "legacy-only", Protocol: OpenCodeGoProtocolChatCompletions},
		"glm-5.2":     {ID: "glm-5.2"},
	})
	catalog.client = &http.Client{
		Transport: openCodeGoCatalogRoundTripper{
			openCodeGoOfficialDocsURL: `
<table><thead><tr><th>Model</th><th>Model ID</th><th>Endpoint</th></tr></thead><tbody>
<tr><td>GLM-5.2</td><td>glm-5.2</td><td><code>https://opencode.ai/zen/go/v1/chat/completions</code></td></tr>
<tr><td>Kimi K2.7</td><td>kimi-k2.7</td><td><code>https://opencode.ai/zen/go/v1/chat/completions</code></td></tr>
</tbody></table>`,
			openCodeGoOfficialModelsURL: `{"object":"list","data":[{"id":"glm-5.2"},{"id":"kimi-k2.8-code"},{"id":"qwen3.8-plus"},{"id":"hy3-preview"}]}`,
		},
	}

	refreshed, err := catalog.fetch(context.Background(), catalog.entries)

	require.NoError(t, err)
	require.Contains(t, refreshed, "glm-5.2")
	require.Contains(t, refreshed, "kimi-k2.8-code")
	require.Contains(t, refreshed, "qwen3.8-plus")
	require.Contains(t, refreshed, "hy3-preview")
	require.NotContains(t, refreshed, "kimi-k2.7")
	require.NotContains(t, refreshed, "legacy-only")
	require.Equal(t, OpenCodeGoProtocolChatCompletions, refreshed["glm-5.2"].Protocol)
	require.Equal(t, OpenCodeGoProtocolChatCompletions, refreshed["kimi-k2.8-code"].Protocol)
	require.Equal(t, OpenCodeGoProtocolMessages, refreshed["qwen3.8-plus"].Protocol)
	require.Empty(t, refreshed["hy3-preview"].Protocol)
}

func TestSyncWithRemoteRefreshesOpenCodeGoPricingWhenBaseHashUnchanged(t *testing.T) {
	const docsURL = "https://opencode.ai/docs/go/"
	remote := &pricingTestRemoteClient{
		hashText: "same-hash",
		pricingBodies: map[string][]byte{
			docsURL: []byte(`
<table><tbody>
<tr><td>Kimi K2.7 Code</td><td>$0.95</td><td>$4.00</td><td>$0.19</td><td>-</td></tr>
</tbody></table>
<table><tbody>
<tr><td>Kimi K2.7</td><td>kimi-k2.7</td><td><code>https://opencode.ai/zen/go/v1/chat/completions</code></td></tr>
</tbody></table>`),
		},
	}
	svc := &PricingService{
		cfg: &config.Config{
			Pricing: config.PricingConfig{
				RemoteURL:           "https://example.com/model_prices.json",
				HashURL:             "https://example.com/model_prices.sha256",
				OpenCodeGoDocsURL:   docsURL,
				DataDir:             t.TempDir(),
				UpdateIntervalHours: 24,
			},
			Security: config.SecurityConfig{
				URLAllowlist: config.URLAllowlistConfig{
					Enabled: false,
				},
			},
		},
		remoteClient: remote,
		pricingData: map[string]*LiteLLMModelPricing{
			"kimi-k2.7": {
				InputCostPerToken: 99e-6,
			},
		},
		localHash: "same-hash",
	}

	require.NoError(t, svc.syncWithRemote())

	pricing := svc.GetModelPricing("kimi-k2.7")
	require.NotNil(t, pricing)
	require.InDelta(t, 0.95e-6, pricing.InputCostPerToken, 1e-12)
	require.Contains(t, remote.fetchedURLs, strings.TrimRight(docsURL, "/"))
}

func TestMergeOpenCodeGoPricingBestEffort_MergesModelsDevSupplementalPricing(t *testing.T) {
	const docsURL = "https://opencode.ai/docs/go/"
	remote := &pricingTestRemoteClient{
		pricingBodies: map[string][]byte{
			docsURL: []byte(`
<table><tbody>
<tr><td>Kimi K2.7 Code</td><td>$0.95</td><td>$4.00</td><td>$0.19</td><td>-</td></tr>
</tbody></table>
<table><tbody>
<tr><td>Kimi K2.7</td><td>kimi-k2.7</td><td><code>https://opencode.ai/zen/go/v1/chat/completions</code></td></tr>
</tbody></table>`),
			cliImportModelsDevAPIURL: []byte(`{
				"opencode-go": {
					"models": {
						"kimi-k2.5": {
							"id": "kimi-k2.5",
							"name": "Kimi K2.5",
							"reasoning": true,
							"attachment": true,
							"tool_call": true,
							"modalities": {"input": ["text", "image", "video"], "output": ["text"]},
							"limit": {"context": 262144, "output": 65536},
							"cost": {"input": 0.6, "output": 3.0, "cache_read": 0.1},
							"status": "deprecated"
						}
					}
				}
			}`),
		},
	}
	svc := &PricingService{
		cfg: &config.Config{
			Pricing: config.PricingConfig{
				OpenCodeGoDocsURL: docsURL,
			},
			Security: config.SecurityConfig{
				URLAllowlist: config.URLAllowlistConfig{
					Enabled:      true,
					PricingHosts: []string{"opencode.ai"},
				},
			},
		},
		remoteClient: remote,
	}
	pricingData := map[string]*LiteLLMModelPricing{}

	merged := svc.mergeOpenCodeGoPricingBestEffort(context.Background(), pricingData)

	require.Equal(t, 2, merged)
	require.Contains(t, remote.fetchedURLs, strings.TrimRight(docsURL, "/"))
	require.Contains(t, remote.fetchedURLs, cliImportModelsDevAPIURL)

	kimi25 := pricingData["kimi-k2.5"]
	require.NotNil(t, kimi25)
	require.Equal(t, PlatformOpenCodeGo, kimi25.LiteLLMProvider)
	require.Equal(t, "chat", kimi25.Mode)
	require.InDelta(t, 0.60e-6, kimi25.InputCostPerToken, 1e-12)
	require.InDelta(t, 3.00e-6, kimi25.OutputCostPerToken, 1e-12)
	require.InDelta(t, 0.10e-6, kimi25.CacheReadInputTokenCost, 1e-12)
	require.Equal(t, 262144, kimi25.MaxInputTokens)
	require.Equal(t, 65536, kimi25.MaxOutputTokens)
	require.True(t, kimi25.SupportsReasoning)
	require.True(t, kimi25.SupportsVision)
	require.True(t, kimi25.SupportsFunctionCalling)
}

func TestGetModelPricing_Gpt54NanoUsesDedicatedStaticFallbackWhenRemoteMissing(t *testing.T) {
	svc := &PricingService{
		pricingData: map[string]*LiteLLMModelPricing{
			"gpt-5.1-codex": {InputCostPerToken: 1.25e-6},
		},
	}

	got := svc.GetModelPricing("gpt-5.4-nano")
	require.NotNil(t, got)
	require.InDelta(t, 2e-7, got.InputCostPerToken, 1e-12)
	require.InDelta(t, 1.25e-6, got.OutputCostPerToken, 1e-12)
	require.InDelta(t, 2e-8, got.CacheReadInputTokenCost, 1e-12)
	require.Zero(t, got.LongContextInputTokenThreshold)
}

func TestGetModelPricing_ImageModelDoesNotFallbackToTextModel(t *testing.T) {
	imagePricing := &LiteLLMModelPricing{InputCostPerToken: 3}
	textPricing := &LiteLLMModelPricing{InputCostPerToken: 9}

	svc := &PricingService{
		pricingData: map[string]*LiteLLMModelPricing{
			"gpt-image-2": imagePricing,
			"gpt-5.4":     textPricing,
		},
	}

	got := svc.GetModelPricing("gpt-image-3")
	require.Same(t, imagePricing, got)
}

func TestParsePricingData_PreservesPriorityAndServiceTierFields(t *testing.T) {
	raw := map[string]any{
		"gpt-5.4": map[string]any{
			"input_cost_per_token":                 2.5e-6,
			"input_cost_per_token_priority":        5e-6,
			"output_cost_per_token":                15e-6,
			"output_cost_per_token_priority":       30e-6,
			"cache_read_input_token_cost":          0.25e-6,
			"cache_read_input_token_cost_priority": 0.5e-6,
			"supports_service_tier":                true,
			"supports_prompt_caching":              true,
			"litellm_provider":                     "openai",
			"mode":                                 "chat",
		},
	}
	body, err := json.Marshal(raw)
	require.NoError(t, err)

	svc := &PricingService{}
	pricingMap, err := svc.parsePricingData(body)
	require.NoError(t, err)

	pricing := pricingMap["gpt-5.4"]
	require.NotNil(t, pricing)
	require.InDelta(t, 2.5e-6, pricing.InputCostPerToken, 1e-12)
	require.InDelta(t, 5e-6, pricing.InputCostPerTokenPriority, 1e-12)
	require.InDelta(t, 15e-6, pricing.OutputCostPerToken, 1e-12)
	require.InDelta(t, 30e-6, pricing.OutputCostPerTokenPriority, 1e-12)
	require.InDelta(t, 0.25e-6, pricing.CacheReadInputTokenCost, 1e-12)
	require.InDelta(t, 0.5e-6, pricing.CacheReadInputTokenCostPriority, 1e-12)
	require.True(t, pricing.SupportsServiceTier)
}

func TestParsePricingData_PreservesServiceTierPriorityFields(t *testing.T) {
	svc := &PricingService{}
	pricingData, err := svc.parsePricingData([]byte(`{
		"gpt-5.4": {
			"input_cost_per_token": 0.0000025,
			"input_cost_per_token_priority": 0.000005,
			"output_cost_per_token": 0.000015,
			"output_cost_per_token_priority": 0.00003,
			"cache_read_input_token_cost": 0.00000025,
			"cache_read_input_token_cost_priority": 0.0000005,
			"supports_service_tier": true,
			"litellm_provider": "openai",
			"mode": "chat"
		}
	}`))
	require.NoError(t, err)

	pricing := pricingData["gpt-5.4"]
	require.NotNil(t, pricing)
	require.InDelta(t, 0.0000025, pricing.InputCostPerToken, 1e-12)
	require.InDelta(t, 0.000005, pricing.InputCostPerTokenPriority, 1e-12)
	require.InDelta(t, 0.000015, pricing.OutputCostPerToken, 1e-12)
	require.InDelta(t, 0.00003, pricing.OutputCostPerTokenPriority, 1e-12)
	require.InDelta(t, 0.00000025, pricing.CacheReadInputTokenCost, 1e-12)
	require.InDelta(t, 0.0000005, pricing.CacheReadInputTokenCostPriority, 1e-12)
	require.True(t, pricing.SupportsServiceTier)
}

// ---------------------------------------------------------------------------
// ListModelNamesByProvider
// ---------------------------------------------------------------------------

func TestListModelNamesByProvider_ReturnsMatchingModels(t *testing.T) {
	svc := &PricingService{
		pricingData: map[string]*LiteLLMModelPricing{
			"claude-opus-4-5-20251101": {LiteLLMProvider: "anthropic", InputCostPerToken: 1.5e-5},
			"claude-sonnet-4-5":        {LiteLLMProvider: "anthropic", InputCostPerToken: 3e-6},
			"gpt-4o":                   {LiteLLMProvider: "openai", InputCostPerToken: 5e-6},
			"gemini-2.5-pro":           {LiteLLMProvider: "google", InputCostPerToken: 1.25e-6},
		},
	}

	got := svc.ListModelNamesByProvider("anthropic")
	require.ElementsMatch(t, []string{"claude-opus-4-5-20251101", "claude-sonnet-4-5"}, got)
	// Must be sorted
	require.Equal(t, "claude-opus-4-5-20251101", got[0])
	require.Equal(t, "claude-sonnet-4-5", got[1])
}

func TestListModelNamesByProvider_CaseInsensitive(t *testing.T) {
	svc := &PricingService{
		pricingData: map[string]*LiteLLMModelPricing{
			"gpt-4o": {LiteLLMProvider: "OpenAI", InputCostPerToken: 5e-6},
		},
	}

	got := svc.ListModelNamesByProvider("openai")
	require.Equal(t, []string{"gpt-4o"}, got)

	got2 := svc.ListModelNamesByProvider("OPENAI")
	require.Equal(t, []string{"gpt-4o"}, got2)
}

func TestListModelNamesByProvider_NoMatch(t *testing.T) {
	svc := &PricingService{
		pricingData: map[string]*LiteLLMModelPricing{
			"gpt-4o": {LiteLLMProvider: "openai", InputCostPerToken: 5e-6},
		},
	}

	got := svc.ListModelNamesByProvider("anthropic")
	require.NotNil(t, got)
	require.Empty(t, got)
}

func TestListModelNamesByProvider_EmptyCatalog(t *testing.T) {
	svc := &PricingService{
		pricingData: map[string]*LiteLLMModelPricing{},
	}

	got := svc.ListModelNamesByProvider("openai")
	require.NotNil(t, got)
	require.Empty(t, got)
}

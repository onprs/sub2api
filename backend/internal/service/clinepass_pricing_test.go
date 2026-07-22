package service

import (
	"context"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestClinePassBillingModelPricingCandidatesUseBoundedPrefixes(t *testing.T) {
	require.Equal(t, []string{
		"cline-pass/kimi-k2.7-code",
		"clinepass/kimi-k2.7-code",
		"kimi-k2.7-code",
		"kimi-k2.7",
	}, billingModelPricingCandidates("cline-pass/kimi-k2.7-code"))
}

func TestGatewayTokenPricingClinePassFailsClosedAndUsesAccountMapping(t *testing.T) {
	pricingSvc := &PricingService{pricingData: map[string]*LiteLLMModelPricing{
		"minimax-m3": {
			Mode:               "chat",
			InputCostPerToken:  1e-6,
			OutputCostPerToken: 2e-6,
			LiteLLMProvider:    PlatformClinePass,
		},
	}}
	cfg := &config.Config{}
	svc := &GatewayService{cfg: cfg, billingService: NewBillingService(cfg, pricingSvc)}
	account := &Account{ID: 72, Platform: PlatformClinePass, Credentials: map[string]any{
		"model_mapping": map[string]any{"client-alias": "cline-pass/minimax-m3"},
	}}

	err := svc.ValidateGatewayTokenPricingAvailable(context.Background(), &APIKey{ID: 4}, account, "client-alias", ChannelMappingResult{
		MappedModel:        "client-alias",
		BillingModelSource: BillingModelSourceRequested,
	})
	require.NoError(t, err)

	err = svc.ValidateGatewayTokenPricingAvailable(context.Background(), &APIKey{ID: 4}, account, "new-unpriced-model", ChannelMappingResult{
		MappedModel:        "cline-pass/new-unpriced-model",
		BillingModelSource: BillingModelSourceChannelMapped,
	})
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
}

func TestClinePassReferencePricingCoversFallbackCatalog(t *testing.T) {
	type expectedPricing struct {
		input      float64
		output     float64
		cacheWrite float64
		cacheRead  float64
	}
	expected := map[string]expectedPricing{
		"glm-5.2":           {input: 1.4e-6, output: 4.4e-6, cacheRead: 0.26e-6},
		"kimi-k3":           {input: 3e-6, output: 15e-6, cacheRead: 0.3e-6},
		"deepseek-v4-pro":   {input: 1.74e-6, output: 3.48e-6, cacheRead: 0.0145e-6},
		"deepseek-v4-flash": {input: 0.14e-6, output: 0.28e-6, cacheRead: 0.0028e-6},
		"kimi-k2.7-code":    {input: 0.95e-6, output: 4e-6, cacheRead: 0.19e-6},
		"kimi-k2.6":         {input: 0.95e-6, output: 4e-6, cacheRead: 0.16e-6},
		"mimo-v2.5-pro":     {input: 1.74e-6, output: 3.48e-6, cacheRead: 0.0145e-6},
		"mimo-v2.5":         {input: 0.14e-6, output: 0.28e-6, cacheRead: 0.0028e-6},
		"minimax-m3":        {input: 0.3e-6, output: 1.2e-6, cacheRead: 0.06e-6},
		"qwen3.7-max":       {input: 2.5e-6, output: 7.5e-6, cacheWrite: 3.125e-6, cacheRead: 0.5e-6},
		"qwen3.7-plus":      {input: 0.4e-6, output: 1.6e-6, cacheWrite: 0.5e-6, cacheRead: 0.04e-6},
	}

	// Conflicting generic data must not override ClinePass's published quota rates.
	pricingSvc := &PricingService{pricingData: map[string]*LiteLLMModelPricing{
		"deepseek-v4-pro": {
			InputCostPerToken:  99e-6,
			OutputCostPerToken: 99e-6,
		},
	}}
	svc := NewBillingService(&config.Config{}, pricingSvc)
	models := ClinePassFallbackModelIDs()
	require.Len(t, models, len(expected))
	for _, model := range models {
		model := model
		t.Run(model, func(t *testing.T) {
			modelID := strings.TrimPrefix(model, "cline-pass/")
			want, ok := expected[modelID]
			require.True(t, ok)
			pricing, err := svc.GetModelPricing(model)
			require.NoError(t, err)
			require.InDelta(t, want.input, pricing.InputPricePerToken, 1e-15)
			require.InDelta(t, want.output, pricing.OutputPricePerToken, 1e-15)
			require.InDelta(t, want.cacheWrite, pricing.CacheCreationPricePerToken, 1e-15)
			require.InDelta(t, want.cacheRead, pricing.CacheReadPricePerToken, 1e-15)
		})
	}

	aliasPricing, err := svc.GetModelPricing("clinepass/kimi-k3")
	require.NoError(t, err)
	require.InDelta(t, 3e-6, aliasPricing.InputPricePerToken, 1e-15)
}

func TestClinePassReferencePricingRejectsUnknownPrefixedModel(t *testing.T) {
	pricingSvc := &PricingService{pricingData: map[string]*LiteLLMModelPricing{
		"glm-5.999": {InputCostPerToken: 1e-6, OutputCostPerToken: 2e-6},
	}}
	svc := NewBillingService(&config.Config{}, pricingSvc)

	_, err := svc.GetModelPricing("cline-pass/glm-5.999")
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
}

func TestClinePassQwen37PlusAppliesPublishedLongContextTier(t *testing.T) {
	svc := NewBillingService(&config.Config{}, nil)
	baseTokens := UsageTokens{
		InputTokens:         100000,
		OutputTokens:        100,
		CacheCreationTokens: 100000,
		CacheReadTokens:     56000,
	}

	base, err := svc.CalculateCost("cline-pass/qwen3.7-plus", baseTokens, 1)
	require.NoError(t, err)
	require.InDelta(t, 100000*0.4e-6, base.InputCost, 1e-12)
	require.InDelta(t, 100*1.6e-6, base.OutputCost, 1e-12)
	require.InDelta(t, 100000*0.5e-6, base.CacheCreationCost, 1e-12)
	require.InDelta(t, 56000*0.04e-6, base.CacheReadCost, 1e-12)

	longTokens := baseTokens
	longTokens.CacheReadTokens++
	longContext, err := svc.CalculateCost("cline-pass/qwen3.7-plus", longTokens, 1)
	require.NoError(t, err)
	require.InDelta(t, 100000*1.2e-6, longContext.InputCost, 1e-12)
	require.InDelta(t, 100*4.8e-6, longContext.OutputCost, 1e-12)
	require.InDelta(t, 100000*1.5e-6, longContext.CacheCreationCost, 1e-12)
	require.InDelta(t, 56001*0.12e-6, longContext.CacheReadCost, 1e-12)
}

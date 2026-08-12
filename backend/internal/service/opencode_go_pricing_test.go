//go:build unit

package service

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestOpenCodeGoReferencePricingCoversSeedCatalog(t *testing.T) {
	catalog := openCodeGoSeedCatalog()
	require.Len(t, catalog, 25)
	require.Len(t, openCodeGoReferencePrices, len(catalog))

	for modelID := range catalog {
		pricing, ok := openCodeGoReferencePricing(modelID)
		require.Truef(t, ok, "OpenCode Go 模型 %s 缺少平台参考价", modelID)
		require.NotNil(t, pricing, modelID)
		require.Greater(t, pricing.InputPricePerToken, 0.0, modelID)
		require.Greater(t, pricing.OutputPricePerToken, 0.0, modelID)
	}
}

func TestOpenCodeGoModelsDevSupplementalModelsAreExplicit(t *testing.T) {
	require.Equal(t, map[string]struct{}{
		"hy3-preview":  {},
		"kimi-k2.5":    {},
		"mimo-v2-omni": {},
		"mimo-v2-pro":  {},
		"qwen3.5-plus": {},
	}, openCodeGoModelsDevSupplementalModels)

	for _, modelID := range []string{
		"deepseek-v4-flash",
		"deepseek-v4-pro",
		"glm-5.2",
		"gpt-5.6-luna",
		"qwen3.8-max",
	} {
		require.False(t, isOpenCodeGoModelsDevSupplementalModel(modelID), modelID)
	}
}

func TestOpenCodeGoReferencePricingLocksSupplementalRates(t *testing.T) {
	tests := []struct {
		model      string
		input      float64
		output     float64
		cacheWrite float64
		cacheRead  float64
	}{
		{model: "hy3-preview", input: 0.285714e-6, output: 1.142857e-6, cacheRead: 0.114286e-6},
		{model: "kimi-k2.5", input: 0.60e-6, output: 3.00e-6, cacheRead: 0.10e-6},
		{model: "mimo-v2-omni", input: 0.40e-6, output: 2.00e-6, cacheRead: 0.08e-6},
		{model: "mimo-v2-pro", input: 1.00e-6, output: 3.00e-6, cacheRead: 0.20e-6},
		{model: "qwen3.5-plus", input: 0.20e-6, output: 1.20e-6, cacheWrite: 0.25e-6, cacheRead: 0.02e-6},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			pricing, ok := openCodeGoReferencePricing(tt.model)
			require.True(t, ok)
			require.InDelta(t, tt.input, pricing.InputPricePerToken, 1e-15)
			require.InDelta(t, tt.output, pricing.OutputPricePerToken, 1e-15)
			require.InDelta(t, tt.cacheWrite, pricing.CacheCreationPricePerToken, 1e-15)
			require.InDelta(t, tt.cacheRead, pricing.CacheReadPricePerToken, 1e-15)
		})
	}
}

func TestOpenCodeGoReferencePricingLocksLongContextTiers(t *testing.T) {
	tests := []struct {
		model            string
		threshold        int
		inputMultiplier  float64
		outputMultiplier float64
	}{
		{model: "gpt-5.6-luna", threshold: 272000, inputMultiplier: 2, outputMultiplier: 1.5},
		{model: "mimo-v2-pro", threshold: 256000, inputMultiplier: 2, outputMultiplier: 2},
		{model: "qwen3.6-plus", threshold: 256000, inputMultiplier: 4, outputMultiplier: 2},
		{model: "qwen3.7-plus", threshold: 256000, inputMultiplier: 3, outputMultiplier: 3},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			pricing, ok := openCodeGoReferencePricing(tt.model)
			require.True(t, ok)
			require.Equal(t, tt.threshold, pricing.LongContextInputThreshold)
			require.InDelta(t, tt.inputMultiplier, pricing.LongContextInputMultiplier, 1e-12)
			require.InDelta(t, tt.outputMultiplier, pricing.LongContextOutputMultiplier, 1e-12)
		})
	}
}

func TestOpenCodeGoReferencePricingSupportsExactKimiAlias(t *testing.T) {
	canonical, ok := openCodeGoReferencePricing("kimi-k2.7-code")
	require.True(t, ok)
	legacy, ok := openCodeGoReferencePricing("opencode-go/kimi-k2.7")
	require.True(t, ok)
	require.Equal(t, canonical, legacy)
}

func TestBillingServiceOpenCodeGoPlatformPricingUsesOnlyOfficialDynamicSnapshot(t *testing.T) {
	pricingSvc := &PricingService{
		pricingData: map[string]*LiteLLMModelPricing{
			"hy3-preview": {
				InputCostPerToken:  77e-6,
				OutputCostPerToken: 77e-6,
				LiteLLMProvider:    PlatformOpenCodeGo,
			},
		},
		openCodeGoPricing: map[string]*LiteLLMModelPricing{
			"glm-5.2": {
				InputCostPerToken:          8e-6,
				OutputCostPerToken:         9e-6,
				LiteLLMProvider:            PlatformOpenCodeGo,
				OpenCodeGoPricingAuthority: openCodeGoPricingAuthorityOfficial,
			},
			"deepseek-v4-flash": {
				InputCostPerToken:          0.07e-6,
				OutputCostPerToken:         0.14e-6,
				LiteLLMProvider:            PlatformOpenCodeGo,
				OpenCodeGoPricingAuthority: openCodeGoPricingAuthorityModelsDev,
			},
		},
	}
	billingSvc := NewBillingService(&config.Config{}, pricingSvc)

	official, err := billingSvc.GetModelPricingForPlatform(PlatformOpenCodeGo, "glm-5.2")
	require.NoError(t, err)
	require.InDelta(t, 8e-6, official.InputPricePerToken, 1e-15)
	require.InDelta(t, 9e-6, official.OutputPricePerToken, 1e-15)

	formalReference, err := billingSvc.GetModelPricingForPlatform(PlatformOpenCodeGo, "deepseek-v4-flash")
	require.NoError(t, err)
	require.InDelta(t, 0.14e-6, formalReference.InputPricePerToken, 1e-15)
	require.InDelta(t, 0.28e-6, formalReference.OutputPricePerToken, 1e-15)

	isolatedReference, err := billingSvc.GetModelPricingForPlatform(PlatformOpenCodeGo, "hy3-preview")
	require.NoError(t, err)
	require.InDelta(t, 0.285714e-6, isolatedReference.InputPricePerToken, 1e-15)

	generic, err := billingSvc.GetModelPricing("hy3-preview")
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
	require.Nil(t, generic)
}

func TestCalculateCostForPlatformAppliesOpenCodeGoLongContextAndGroupMultiplier(t *testing.T) {
	billingSvc := NewBillingService(&config.Config{}, nil)
	cost, err := billingSvc.CalculateCostForPlatform(
		PlatformOpenCodeGo,
		"qwen3.7-plus",
		UsageTokens{InputTokens: 256001, OutputTokens: 1000},
		1.25,
	)
	require.NoError(t, err)

	standard := float64(256001)*0.4e-6*3 + float64(1000)*1.6e-6*3
	require.InDelta(t, standard, cost.TotalCost, 1e-12)
	require.InDelta(t, standard*1.25, cost.ActualCost, 1e-12)
}

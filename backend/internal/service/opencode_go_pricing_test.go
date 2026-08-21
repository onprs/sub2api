//go:build unit

package service

import (
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestOpenCodeGoReferencePricingCoversSeedCatalog(t *testing.T) {
	catalog := openCodeGoSeedCatalog()
	require.Len(t, catalog, 28)
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
		"glm-5.3",
		"gpt-5.6-luna",
		"qwen3.8-max",
	} {
		require.False(t, isOpenCodeGoModelsDevSupplementalModel(modelID), modelID)
	}
}

func TestOpenCodeGoReferencePricingLocksGLM53Rate(t *testing.T) {
	pricing, ok := openCodeGoReferencePricing("glm-5.3")
	require.True(t, ok)
	require.InDelta(t, 1.4e-6, pricing.InputPricePerToken, 1e-15)
	require.InDelta(t, 4.4e-6, pricing.OutputPricePerToken, 1e-15)
	require.InDelta(t, 0.26e-6, pricing.CacheReadPricePerToken, 1e-15)
	require.Zero(t, pricing.CacheCreationPricePerToken)
}

func TestOpenCodeGoReferencePricingLocksOfficialCurrentRates(t *testing.T) {
	tests := []struct {
		model      string
		input      float64
		output     float64
		cacheRead  float64
		cacheWrite float64
	}{
		{model: "grok-4.5", input: 2, output: 6, cacheRead: 0.3},
		{model: "gpt-5.6-luna", input: 0.2, output: 1.2, cacheRead: 0.02, cacheWrite: 0.25},
		{model: "glm-5.3", input: 1.4, output: 4.4, cacheRead: 0.26},
		{model: "glm-5.2", input: 1.4, output: 4.4, cacheRead: 0.26},
		{model: "glm-5.1", input: 1.4, output: 4.4, cacheRead: 0.26},
		{model: "kimi-k3", input: 3, output: 15, cacheRead: 0.3},
		{model: "kimi-k2.7-code", input: 0.95, output: 4, cacheRead: 0.19},
		{model: "kimi-k2.6", input: 0.95, output: 4, cacheRead: 0.16},
		{model: "mimo-v2.5", input: 0.14, output: 0.28, cacheRead: 0.0028},
		{model: "mimo-v2.5-pro", input: 0.435, output: 0.87, cacheRead: 0.003625},
		{model: "minimax-m3", input: 0.3, output: 1.2, cacheRead: 0.06},
		{model: "minimax-m2.7", input: 0.3, output: 1.2, cacheRead: 0.06, cacheWrite: 0.375},
		{model: "minimax-m2.5", input: 0.3, output: 1.2, cacheRead: 0.06, cacheWrite: 0.375},
		{model: "muse-spark-1.2-contributor", input: 0.1, output: 0.2, cacheRead: 0.002},
		{model: "qwen3.8-max", input: 2, output: 6, cacheRead: 0.25, cacheWrite: 2.5},
		{model: "qwen3.7-max", input: 2.5, output: 7.5, cacheRead: 0.5, cacheWrite: 3.125},
		{model: "qwen3.7-plus", input: 0.4, output: 1.6, cacheRead: 0.04, cacheWrite: 0.5},
		{model: "qwen3.6-plus", input: 0.5, output: 3, cacheRead: 0.05, cacheWrite: 0.625},
		{model: "deepseek-v4-pro", input: 0.66, output: 1.98, cacheRead: 0.022},
		{model: "deepseek-v4-flash", input: 0.22, output: 0.66, cacheRead: 0.007},
		{model: "deepseek-v4-flash-vision-exp", input: 0.22, output: 0.66, cacheRead: 0.007},
		{model: "hy3", input: 0.14, output: 0.58, cacheRead: 0.035},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			pricing, ok := openCodeGoReferencePricing(tt.model)
			require.True(t, ok)
			require.InDelta(t, tt.input*1e-6, pricing.InputPricePerToken, 1e-15)
			require.InDelta(t, tt.output*1e-6, pricing.OutputPricePerToken, 1e-15)
			require.InDelta(t, tt.cacheRead*1e-6, pricing.CacheReadPricePerToken, 1e-15)
			require.InDelta(t, tt.cacheWrite*1e-6, pricing.CacheCreationPricePerToken, 1e-15)
		})
	}
}

func TestOpenCodeGoReferencePricingUsesOfficialDeepSeekTimeBands(t *testing.T) {
	tests := []struct {
		name      string
		model     string
		now       time.Time
		input     float64
		output    float64
		cacheRead float64
	}{
		{name: "off peak before first window", model: "deepseek-v4-pro", now: time.Date(2026, time.August, 21, 0, 59, 59, 0, time.UTC), input: 0.66e-6, output: 1.98e-6, cacheRead: 0.022e-6},
		{name: "first peak starts", model: "deepseek-v4-pro", now: time.Date(2026, time.August, 21, 1, 0, 0, 0, time.UTC), input: 1.32e-6, output: 3.96e-6, cacheRead: 0.044e-6},
		{name: "first peak ends", model: "deepseek-v4-flash", now: time.Date(2026, time.August, 21, 4, 0, 0, 0, time.UTC), input: 0.22e-6, output: 0.66e-6, cacheRead: 0.007e-6},
		{name: "second peak starts", model: "deepseek-v4-flash", now: time.Date(2026, time.August, 21, 6, 0, 0, 0, time.UTC), input: 0.44e-6, output: 1.32e-6, cacheRead: 0.014e-6},
		{name: "second peak ends", model: "deepseek-v4-flash-vision-exp", now: time.Date(2026, time.August, 21, 10, 0, 0, 0, time.UTC), input: 0.22e-6, output: 0.66e-6, cacheRead: 0.007e-6},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pricing, ok := openCodeGoReferencePricingAt(tt.model, tt.now)
			require.True(t, ok)
			require.InDelta(t, tt.input, pricing.InputPricePerToken, 1e-15)
			require.InDelta(t, tt.output, pricing.OutputPricePerToken, 1e-15)
			require.InDelta(t, tt.cacheRead, pricing.CacheReadPricePerToken, 1e-15)
		})
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
		{model: "muse-spark-1.2-contributor", input: 0.10e-6, output: 0.20e-6, cacheRead: 0.002e-6},
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

	formalReference, err := billingSvc.getModelPricingForPlatformAt(
		PlatformOpenCodeGo,
		"deepseek-v4-flash",
		time.Date(2026, time.August, 21, 0, 0, 0, 0, time.UTC),
	)
	require.NoError(t, err)
	require.InDelta(t, 0.22e-6, formalReference.InputPricePerToken, 1e-15)
	require.InDelta(t, 0.66e-6, formalReference.OutputPricePerToken, 1e-15)

	isolatedReference, err := billingSvc.GetModelPricingForPlatform(PlatformOpenCodeGo, "hy3-preview")
	require.NoError(t, err)
	require.InDelta(t, 0.285714e-6, isolatedReference.InputPricePerToken, 1e-15)

	generic, err := billingSvc.GetModelPricing("hy3-preview")
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
	require.Nil(t, generic)
}

func TestBillingServiceOpenCodeGoRejectsLegacySingleBandDeepSeekCache(t *testing.T) {
	pricingSvc := &PricingService{
		openCodeGoPricing: map[string]*LiteLLMModelPricing{
			"deepseek-v4-flash": {
				InputCostPerToken:          0.44e-6,
				OutputCostPerToken:         1.32e-6,
				CacheReadInputTokenCost:    0.014e-6,
				LiteLLMProvider:            PlatformOpenCodeGo,
				OpenCodeGoPricingAuthority: openCodeGoPricingAuthorityOfficial,
			},
		},
	}
	billingSvc := NewBillingService(&config.Config{}, pricingSvc)

	require.Nil(t, pricingSvc.GetOpenCodeGoModelPricingExact("deepseek-v4-flash"))

	offPeak, err := billingSvc.getModelPricingForPlatformAt(
		PlatformOpenCodeGo,
		"deepseek-v4-flash",
		time.Date(2026, time.August, 21, 0, 0, 0, 0, time.UTC),
	)
	require.NoError(t, err)
	require.InDelta(t, 0.22e-6, offPeak.InputPricePerToken, 1e-15)
	require.InDelta(t, 0.66e-6, offPeak.OutputPricePerToken, 1e-15)

	peak, err := billingSvc.getModelPricingForPlatformAt(
		PlatformOpenCodeGo,
		"deepseek-v4-flash",
		time.Date(2026, time.August, 21, 1, 0, 0, 0, time.UTC),
	)
	require.NoError(t, err)
	require.InDelta(t, 0.44e-6, peak.InputPricePerToken, 1e-15)
	require.InDelta(t, 1.32e-6, peak.OutputPricePerToken, 1e-15)
}

func TestBillingServiceOpenCodeGoOfficialZeroRatePricingIsTemporaryAndPlatformScoped(t *testing.T) {
	pricingSvc := &PricingService{
		openCodeGoPricing: map[string]*LiteLLMModelPricing{
			"ox-alpha-free": {
				LiteLLMProvider:            PlatformOpenCodeGo,
				Mode:                       "chat",
				OpenCodeGoPricingAuthority: openCodeGoPricingAuthorityOfficial,
				OpenCodeGoExplicitZeroRate: true,
				InputCostPerTokenKnown:     true,
				OutputCostPerTokenKnown:    true,
			},
		},
		openCodeGoPricingConfirmedAt: time.Now(),
	}
	billingSvc := NewBillingService(&config.Config{}, pricingSvc)

	pricing, err := billingSvc.GetModelPricingForPlatform(PlatformOpenCodeGo, "ox-alpha-free")
	require.NoError(t, err)
	require.NotNil(t, pricing)
	require.True(t, pricing.AllowZeroRate)

	cost, err := billingSvc.CalculateCostForPlatform(
		PlatformOpenCodeGo,
		"ox-alpha-free",
		UsageTokens{InputTokens: 1000, OutputTokens: 500, CacheReadTokens: 250},
		1.25,
	)
	require.NoError(t, err)
	require.Zero(t, cost.TotalCost)
	require.Zero(t, cost.ActualCost)

	generic, err := billingSvc.GetModelPricing("ox-alpha-free")
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
	require.Nil(t, generic)

	pricingSvc.mu.Lock()
	pricingSvc.openCodeGoPricingConfirmedAt = time.Now().Add(-openCodeGoZeroRateEvidenceTTL - time.Minute)
	pricingSvc.mu.Unlock()
	_, err = billingSvc.GetModelPricingForPlatform(PlatformOpenCodeGo, "ox-alpha-free")
	require.ErrorIs(t, err, ErrModelPricingUnavailable)

	pricingSvc.mu.Lock()
	pricingSvc.openCodeGoPricingConfirmedAt = time.Now().Add(time.Minute)
	pricingSvc.mu.Unlock()
	_, err = billingSvc.GetModelPricingForPlatform(PlatformOpenCodeGo, "ox-alpha-free")
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
}

func TestBillingServiceOpenCodeGoUntrustedZeroRateStillFailsClosed(t *testing.T) {
	pricingSvc := &PricingService{
		openCodeGoPricing: map[string]*LiteLLMModelPricing{
			"unpriced-preview": {
				LiteLLMProvider:            PlatformOpenCodeGo,
				OpenCodeGoPricingAuthority: openCodeGoPricingAuthorityModelsDev,
				OpenCodeGoExplicitZeroRate: true,
			},
		},
		openCodeGoPricingConfirmedAt: time.Now(),
	}
	billingSvc := NewBillingService(&config.Config{}, pricingSvc)

	_, err := billingSvc.CalculateCostForPlatform(
		PlatformOpenCodeGo,
		"unpriced-preview",
		UsageTokens{InputTokens: 10, OutputTokens: 5},
		1,
	)
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
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

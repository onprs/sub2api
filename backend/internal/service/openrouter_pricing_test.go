package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestOpenRouterPricing(t *testing.T) {
	// Builtin 17 free models
	models := OpenRouterFallbackModelIDs()
	for _, m := range models {
		pricing, ok := openRouterReferencePricing(m)
		require.True(t, ok, "model %s should have reference pricing", m)
		require.NotNil(t, pricing)
		require.True(t, pricing.AllowZeroRate)
		require.Equal(t, float64(0), pricing.InputPricePerToken)
		require.Equal(t, float64(0), pricing.OutputPricePerToken)
	}

	// Arbitrary :free model
	freePricing, ok := openRouterReferencePricing("some-org/some-model:free")
	require.True(t, ok)
	require.True(t, freePricing.AllowZeroRate)
	require.Equal(t, float64(0), freePricing.InputPricePerToken)

	// openrouter/free
	orFree, ok := openRouterReferencePricing("openrouter/free")
	require.True(t, ok)
	require.True(t, orFree.AllowZeroRate)
	require.Equal(t, float64(0), orFree.InputPricePerToken)

	// prefixed openrouter/some-org/some-model:free
	prefixedFree, ok := openRouterReferencePricing("openrouter/some-org/some-model:free")
	require.True(t, ok)
	require.True(t, prefixedFree.AllowZeroRate)

	// Unknown non-free model returns false
	_, ok = openRouterReferencePricing("openai/gpt-4o")
	require.False(t, ok)
}

func TestOpenRouterBillingServicePricingAndCostCalculation(t *testing.T) {
	svc := NewBillingService(&config.Config{}, nil)

	// 1. GetModelPricing and GetModelPricingForPlatform
	pricing, err := svc.GetModelPricing("openrouter/free")
	require.NoError(t, err)
	require.NotNil(t, pricing)
	require.True(t, pricing.AllowZeroRate)
	require.True(t, hasBillableTokenPricing(pricing))

	pricingPlat, err := svc.GetModelPricingForPlatform(PlatformOpenRouter, "google/gemma-4-26b-a4b-it:free")
	require.NoError(t, err)
	require.NotNil(t, pricingPlat)
	require.True(t, pricingPlat.AllowZeroRate)
	require.True(t, hasBillableTokenPricing(pricingPlat))

	// 2. CalculateCost and CalculateCostForPlatform with tokens > 0
	tokens := UsageTokens{InputTokens: 1000, OutputTokens: 500, CacheReadTokens: 200}
	cost, err := svc.CalculateCost("openrouter/free", tokens, 1.0)
	require.NoError(t, err)
	require.NotNil(t, cost)
	require.Equal(t, float64(0), cost.TotalCost)
	require.Equal(t, float64(0), cost.ActualCost)

	costPlat, err := svc.CalculateCostForPlatform(PlatformOpenRouter, "custom-org/my-model:free", tokens, 1.2)
	require.NoError(t, err)
	require.NotNil(t, costPlat)
	require.Equal(t, float64(0), costPlat.TotalCost)
	require.Equal(t, float64(0), costPlat.ActualCost)

	// 3. Unknown non-free model without LiteLLM pricing fails closed
	_, err = svc.GetModelPricingForPlatform(PlatformOpenRouter, "unknown-org/unpriced-model")
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
}

func TestGatewayTokenPricingOpenRouterAllowsFreeModels(t *testing.T) {
	cfg := &config.Config{}
	svc := &GatewayService{cfg: cfg, billingService: NewBillingService(cfg, nil)}
	account := &Account{ID: 88, Platform: PlatformOpenRouter}

	// 1. openrouter/free passes preflight
	err := svc.ValidateGatewayTokenPricingAvailable(context.Background(), &APIKey{ID: 1}, account, "openrouter/free", ChannelMappingResult{
		MappedModel:        "openrouter/free",
		BillingModelSource: BillingModelSourceRequested,
	})
	require.NoError(t, err)

	// 2. :free suffix passes preflight
	err = svc.ValidateGatewayTokenPricingAvailable(context.Background(), &APIKey{ID: 1}, account, "meta-llama/llama-3.3-70b-instruct:free", ChannelMappingResult{
		MappedModel:        "meta-llama/llama-3.3-70b-instruct:free",
		BillingModelSource: BillingModelSourceRequested,
	})
	require.NoError(t, err)

	// 3. Unpriced non-free model fails closed
	err = svc.ValidateGatewayTokenPricingAvailable(context.Background(), &APIKey{ID: 1}, account, "meta-llama/llama-3.3-70b-instruct", ChannelMappingResult{
		MappedModel:        "meta-llama/llama-3.3-70b-instruct",
		BillingModelSource: BillingModelSourceRequested,
	})
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
}

func TestChannelServiceBuildCatalogSupportedModelOpenRouterFreeModel(t *testing.T) {
	billingSvc := NewBillingService(&config.Config{}, nil)
	svc := &ChannelService{billingService: billingSvc}

	model := svc.BuildCatalogSupportedModel("openrouter/free", PlatformOpenRouter, nil)
	require.NotNil(t, model.Pricing)
	require.NotNil(t, model.Pricing.InputPrice)
	require.NotNil(t, model.Pricing.OutputPrice)
	require.Equal(t, float64(0), *model.Pricing.InputPrice)
	require.Equal(t, float64(0), *model.Pricing.OutputPrice)
	require.Equal(t, PricingSourceCatalog, model.PricingSource)
}

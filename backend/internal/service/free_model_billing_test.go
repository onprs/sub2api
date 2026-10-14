//go:build unit

package service

import (
	"context"
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// applyFreeModelMigrationForTest 把历史迁移种子升级到 248 的计费配置。
func applyFreeModelMigrationForTest(t *testing.T, entries []ChannelModelPricing) []ChannelModelPricing {
	t.Helper()
	migration, err := os.ReadFile("../../migrations/248_free_model_billing_price.sql")
	require.NoError(t, err)
	parts := strings.Split(string(migration), "$free_models$")
	require.Len(t, parts, 3)
	var models map[string][]string
	require.NoError(t, json.Unmarshal([]byte(parts[1]), &models))
	parts = strings.Split(string(migration), "$free_price$")
	require.Len(t, parts, 3)
	var price float64
	require.NoError(t, json.Unmarshal([]byte(parts[1]), &price))
	require.Equal(t, 1e-9, price)
	result := make([]ChannelModelPricing, len(entries))
	for index, entry := range entries {
		result[index] = entry.Clone()
		free := len(entry.Models) > 0
		for _, model := range entry.Models {
			free = free && slices.Contains(models[entry.Platform], model)
		}
		if !free {
			continue
		}
		target := &result[index]
		for _, field := range []**float64{&target.InputPrice, &target.OutputPrice, &target.CacheReadPrice, &target.CacheWritePrice, &target.CacheWrite1hPrice, &target.ImageInputPrice, &target.ImageOutputPrice} {
			value := price
			*field = &value
		}
		for _, field := range []**float64{&target.FastMultiplier, &target.FlexMultiplier, &target.MaxReasoningEffortMultiplier} {
			one := 1.0
			*field = &one
		}
		target.ReasoningEffortMultipliers = map[string]float64{}
		target.MonthlyQuota = nil
		target.TimePricing = nil
		target.Intervals = []PricingInterval{}
	}
	return result
}

func TestFreeModelBillingUsesFixedTokenAndCachePrices(t *testing.T) {
	billing := &BillingService{}
	at := time.Date(2026, 10, 14, 5, 0, 0, 0, time.UTC)
	for _, platform := range []string{PlatformOpenCodeGo, PlatformCommandCode} {
		models := openCodeFreeModels
		if platform == PlatformCommandCode {
			models = []string{"inclusionai/ling-3.0-flash-sante:free", "inclusionai/ling-3.1-flash:free", "poolside/laguna-s-2.1-free", "stealth/glyph-cluster:free"}
		}
		for _, model := range models {
			t.Run(platform+"/"+model, func(t *testing.T) {
				pricing, err := billing.getModelPricingForPlatformAt(platform, model, at)
				require.NoError(t, err)
				require.False(t, pricing.AllowZeroRate)
				display := (&ChannelService{billingService: billing}).BuildCatalogSupportedModel(model, platform, nil)
				require.Equal(t, PricingSourceCatalog, display.PricingSource)
				require.NotNil(t, display.Pricing)
				require.InDelta(t, 1e-9, *display.Pricing.InputPrice, 1e-18)
				require.InDelta(t, 1e-9, *display.Pricing.CacheWrite1hPrice, 1e-18)
				require.Nil(t, display.MonthlyQuota)
				require.Nil(t, display.QuotaCost)
				for _, rate := range []float64{pricing.InputPricePerToken, pricing.OutputPricePerToken, pricing.CacheReadPricePerToken, pricing.CacheCreationPricePerToken, pricing.CacheCreation5mPrice, pricing.CacheCreation1hPrice, pricing.ImageInputPricePerToken, pricing.ImageOutputPricePerToken, pricing.ImageCacheReadPricePerToken} {
					require.Equal(t, 1e-9, rate)
				}
				for _, tier := range []string{"", "fast", "flex"} {
					tokens := UsageTokens{InputTokens: 1_000_000, ImageInputTokens: 300_000, OutputTokens: 1_000_000, ImageOutputTokens: 400_000, CacheReadTokens: 1_000_000, ImageCacheReadTokens: 200_000, CacheCreationTokens: 2_000_000, CacheCreation5mTokens: 1_000_000, CacheCreation1hTokens: 1_000_000}
					cost, err := billing.CalculateCostUnified(CostInput{Ctx: context.Background(), Model: model, PricingPlatform: platform, Tokens: tokens, RateMultiplier: 2, PricingAt: at, ServiceTier: tier, ReasoningEffort: "max", Resolver: NewModelPricingResolver(nil, billing)})
					require.NoError(t, err)
					require.NoError(t, billing.applyPlatformQuotaCostAt(cost, platform, model, at))
					require.InDelta(t, 0.005, cost.TotalCost, 1e-12, tier)
					require.InDelta(t, 0.01, cost.ActualCost, 1e-12, tier)
					require.InDelta(t, 1, cost.ModelSpecificMultiplier, 1e-12, tier)
					require.False(t, cost.AllowZeroRate)
					require.NoError(t, billing.applyPlatformQuotaCostAt(cost, platform, model, at))
					require.InDelta(t, 0.01, cost.ActualCost, 1e-12)
				}
				for _, usage := range []UsageTokens{{InputTokens: 1_000_000}, {OutputTokens: 1_000_000}, {CacheReadTokens: 1_000_000}, {CacheCreationTokens: 1_000_000}, {CacheCreation5mTokens: 1_000_000}, {CacheCreation1hTokens: 1_000_000}} {
					cost, err := billing.CalculateCostUnified(CostInput{Model: model, PricingPlatform: platform, Tokens: usage, RateMultiplier: 1, PricingAt: at})
					require.NoError(t, err)
					require.InDelta(t, 0.001, cost.TotalCost, 1e-12)
				}
			})
		}
	}
}

func TestFreeModelBillingKeepsWhitelistAndPaidRates(t *testing.T) {
	require.Len(t, openCodeFreeModels, 9)
	require.Len(t, DefaultOpenCodeGoModelIDs(), 31)
	require.Len(t, openCodeZenPricedModels, 72)
	migration, err := os.ReadFile("../../migrations/248_free_model_billing_price.sql")
	require.NoError(t, err)
	parts := strings.Split(string(migration), "$free_models$")
	require.Len(t, parts, 3)
	var migrationModels map[string][]string
	require.NoError(t, json.Unmarshal([]byte(parts[1]), &migrationModels))
	require.ElementsMatch(t, openCodeFreeModels, migrationModels[PlatformOpenCodeGo])
	var commandCodeFreeModels []string
	for _, entry := range commandCodeFallbackCatalogEntries() {
		pricing, available := commandCodeCatalogPricingAt(entry, time.Date(2026, 10, 14, 5, 0, 0, 0, time.UTC))
		if available && isFreeModelReferencePricing(pricing) {
			commandCodeFreeModels = append(commandCodeFreeModels, entry.ID)
		}
	}
	require.Len(t, commandCodeFreeModels, 4)
	require.ElementsMatch(t, commandCodeFreeModels, migrationModels[PlatformCommandCode])
	for _, model := range openCodeFreeModels {
		require.Contains(t, openCodeZenPricedModels, model)
	}
	billing := &BillingService{}
	for _, test := range []struct{ platform, model string }{{PlatformOpenCodeGo, "omen-alpha"}, {PlatformOpenCodeGo, "unpublished-free"}, {PlatformCommandCode, "stealth/nonexistent:free"}, {PlatformCommandCode, "gpt-6.1-sol"}} {
		_, err := billing.GetModelPricingForPlatform(test.platform, test.model)
		require.Error(t, err)
	}
	pricing, err := billing.GetModelPricingForPlatform(PlatformOpenCodeGo, "glm-5.3")
	require.NoError(t, err)
	require.InDelta(t, 1.4e-6, pricing.InputPricePerToken, 1e-15)
	require.InDelta(t, 4.4e-6, pricing.OutputPricePerToken, 1e-15)
	// 免费标记与正价并存时，不覆盖已经恢复收费的官方价格。
	require.False(t, isFreeModelReferencePricing(&ModelPricing{AllowZeroRate: true, InputPricePerToken: 1e-6}))
	require.False(t, isFreeModelReferencePricing(&ModelPricing{AllowZeroRate: true, InputPricePerToken: -1e-6}))
}

func TestFreeModelChannelBillingAndAccountStatsUseSamePrice(t *testing.T) {
	ctx := context.Background()
	at := time.Date(2026, 10, 14, 5, 0, 0, 0, time.UTC)
	groupID := int64(1)
	for _, platform := range []string{PlatformOpenCodeGo, PlatformCommandCode} {
		pricing := openCodeDefaultPricing
		if platform == PlatformCommandCode {
			pricing = commandCodeBillingSeedPricing(t)
		}
		billing := &BillingService{}
		rule := AccountStatsPricingRule{Platform: platform, PricingSource: AccountStatsPricingSourceChannel}
		channel := &Channel{ID: 1, Status: StatusActive, GroupIDs: []int64{groupID}, ModelPricing: pricing, AccountStatsPricingRules: []AccountStatsPricingRule{rule}}
		channels := &ChannelService{billingService: billing}
		channels.cache.Store(populateChannelCache([]Channel{*channel}, map[int64]string{groupID: platform}))
		for _, entry := range pricing {
			model := entry.Models[0]
			free := isOpenCodeFreeModel(model)
			if platform == PlatformCommandCode {
				free = isCommandCodeFreeModelAt(model, at)
			}
			if !free {
				continue
			}
			t.Run(platform+"/"+model, func(t *testing.T) {
				tokens := UsageTokens{InputTokens: 1_000_000, OutputTokens: 1_000_000, CacheReadTokens: 1_000_000, CacheCreationTokens: 2_000_000, CacheCreation5mTokens: 1_000_000, CacheCreation1hTokens: 1_000_000}
				cost, err := billing.CalculateCostUnified(CostInput{Ctx: ctx, Model: model, PricingPlatform: platform, GroupID: &groupID, Tokens: tokens, RateMultiplier: 2, PricingAt: at, Resolver: NewModelPricingResolver(channels, billing)})
				require.NoError(t, err)
				require.InDelta(t, 0.005, cost.TotalCost, 1e-12)
				require.InDelta(t, 0.01, cost.ActualCost, 1e-12)
				require.InDelta(t, 1, cost.ModelSpecificMultiplier, 1e-12)
				stats := tryChannelAccountStatsRule(ctx, billing, channel, rule, 7, groupID, platform, model, tokens, 1, "", "", at)
				require.NotNil(t, stats)
				require.InDelta(t, 0.005, *stats, 1e-12)
				display := channels.BuildSupportedModelForPricingGroup(ctx, groupID, model, platform, nil)
				require.NotNil(t, display.Pricing)
				require.InDelta(t, 1e-9, *display.Pricing.CacheWrite1hPrice, 1e-18)
				require.Nil(t, display.MonthlyQuota)
				require.Nil(t, display.QuotaCost)
			})
		}
	}
}

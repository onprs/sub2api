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

	gocache "github.com/patrickmn/go-cache"
	"github.com/stretchr/testify/require"
)

type commandCodeBillingSeedRates struct {
	Input      float64  `json:"input"`
	Output     float64  `json:"output"`
	CacheRead  float64  `json:"cache_read"`
	CacheWrite *float64 `json:"cache_write"`
}

type commandCodeBillingSeed struct {
	commandCodeBillingSeedRates
	Model        string              `json:"model"`
	MonthlyQuota MonthlyQuotaPolicy  `json:"monthly_quota"`
	TimePricing  *ChannelTimePricing `json:"time_pricing"`
	Intervals    []struct {
		commandCodeBillingSeedRates
		MinTokens int  `json:"min_tokens"`
		MaxTokens *int `json:"max_tokens"`
	} `json:"intervals"`
}

func (rates commandCodeBillingSeedRates) pricing() ChannelModelPricing {
	input, output, cacheRead := rates.Input*1e-6, rates.Output*1e-6, rates.CacheRead*1e-6
	pricing := ChannelModelPricing{Platform: PlatformCommandCode, BillingMode: BillingModeToken,
		InputPrice: &input, OutputPrice: &output, CacheReadPrice: &cacheRead}
	if rates.CacheWrite != nil {
		value := *rates.CacheWrite * 1e-6
		pricing.CacheWritePrice = &value
	}
	return pricing
}

func commandCodeBillingSeedPricing(t *testing.T) []ChannelModelPricing {
	t.Helper()
	migration, err := os.ReadFile("../../migrations/245_command_code_channel_billing.sql")
	require.NoError(t, err)
	parts := strings.Split(string(migration), "$seed$")
	require.Len(t, parts, 3)
	var seeds []commandCodeBillingSeed
	require.NoError(t, json.Unmarshal([]byte(parts[1]), &seeds))
	update, err := os.ReadFile("../../migrations/247_sync_official_channel_catalogs.sql")
	require.NoError(t, err)
	parts = strings.Split(string(update), "$commandcode_quotas$")
	require.Len(t, parts, 3)
	var quotas map[string]MonthlyQuotaPolicy
	require.NoError(t, json.Unmarshal([]byte(parts[1]), &quotas))
	for index := range seeds {
		if quota, ok := quotas[seeds[index].Model]; ok {
			seeds[index].MonthlyQuota = quota
		}
	}
	// 251 同步 2026-10-27 官方目录：新增、下架与额度调整。
	sync, err := os.ReadFile("../../migrations/251_sync_official_channel_catalogs_20261027.sql")
	require.NoError(t, err)
	parts = strings.Split(string(sync), "$commandcode_retired$")
	require.Len(t, parts, 3)
	var retired []string
	require.NoError(t, json.Unmarshal([]byte(parts[1]), &retired))
	parts = strings.Split(string(sync), "$commandcode_additions$")
	require.Len(t, parts, 3)
	var additions []commandCodeBillingSeed
	require.NoError(t, json.Unmarshal([]byte(parts[1]), &additions))
	parts = strings.Split(string(sync), "$commandcode_quotas$")
	require.Len(t, parts, 3)
	require.NoError(t, json.Unmarshal([]byte(parts[1]), &quotas))
	retained := make([]commandCodeBillingSeed, 0, len(seeds)+len(additions))
	for _, seed := range seeds {
		if slices.Contains(retired, seed.Model) {
			continue
		}
		if quota, ok := quotas[seed.Model]; ok {
			seed.MonthlyQuota = quota
		}
		retained = append(retained, seed)
	}
	for _, seed := range additions {
		if quota, ok := quotas[seed.Model]; ok {
			seed.MonthlyQuota = quota
		}
		retained = append(retained, seed)
	}
	seeds = retained
	pricing := make([]ChannelModelPricing, 0, len(seeds))
	for _, seed := range seeds {
		entry := seed.pricing()
		entry.Models = []string{seed.Model}
		if seed.MonthlyQuota.BaseCreditsUSD != 0 || len(seed.MonthlyQuota.Periods) != 0 {
			quota := seed.MonthlyQuota.Clone()
			entry.MonthlyQuota = &quota
		}
		entry.TimePricing = seed.TimePricing
		for _, tier := range seed.Intervals {
			rates := tier.pricing()
			entry.Intervals = append(entry.Intervals, PricingInterval{
				MinTokens: tier.MinTokens, MaxTokens: tier.MaxTokens,
				InputPrice: rates.InputPrice, OutputPrice: rates.OutputPrice,
				CacheReadPrice: rates.CacheReadPrice, CacheWritePrice: rates.CacheWritePrice,
			})
		}
		pricing = append(pricing, entry)
	}
	return applyFreeModelMigrationForTest(t, pricing)
}

func commandCodeBillingTestService(pricing []ChannelModelPricing) (*ChannelService, *BillingService, *ModelPricingResolver) {
	billing := &BillingService{}
	channels := &ChannelService{billingService: billing}
	channel := Channel{ID: 1, Name: "Command Code", Status: StatusActive, GroupIDs: []int64{1},
		BillingModelSource: BillingModelSourceRequested, ModelPricing: pricing}
	channels.cache.Store(populateChannelCache([]Channel{channel}, map[int64]string{1: PlatformCommandCode}))
	return channels, billing, NewModelPricingResolver(channels, billing)
}

func TestCommandCodeChannelBillingSeedPreservesPricesAndMonthlyRules(t *testing.T) {
	ctx := context.Background()
	groupID := int64(1)
	seed := commandCodeBillingSeedPricing(t)
	entries := commandCodeFallbackCatalogEntries()
	require.Len(t, seed, len(entries))
	require.NoError(t, validatePricingEntries(seed))
	_, billing, resolver := commandCodeBillingTestService(seed)
	for _, configured := range seed {
		model := configured.Models[0]
		entry, ok := entries[strings.ToLower(model)]
		require.True(t, ok, model)
		for _, at := range []time.Time{
			time.Date(2026, 10, 12, 2, 0, 0, 0, time.UTC),
			time.Date(2026, 10, 12, 5, 0, 0, 0, time.UTC),
			time.Date(2026, 10, 11, 2, 0, 0, 0, time.UTC),
			time.Date(2026, 10, 21, 2, 0, 0, 0, time.UTC),
			time.Date(2026, 10, 14, 12, 29, 59, 0, time.UTC),
			time.Date(2026, 10, 14, 12, 30, 0, 0, time.UTC),
			time.Date(2026, 10, 16, 13, 59, 58, 0, time.UTC),
			time.Date(2026, 10, 16, 13, 59, 59, 0, time.UTC),
			time.Date(2026, 10, 17, 12, 29, 58, 0, time.UTC),
			time.Date(2026, 10, 17, 12, 29, 59, 0, time.UTC),
		} {
			t.Run(model+"/"+at.Format(time.RFC3339), func(t *testing.T) {
				official, err := billing.getModelPricingForPlatformAt(PlatformCommandCode, model, at)
				require.NoError(t, err)
				resolved := resolver.Resolve(ctx, PricingInput{Model: model, Platform: PlatformCommandCode, GroupID: &groupID, PricingAt: at})
				require.Equal(t, PricingSourceChannel, resolved.Source)
				contexts := []int{1000}
				for _, tier := range entry.Tiers {
					if tier.MaxTokens != nil {
						contexts = append(contexts, *tier.MaxTokens, *tier.MaxTokens+1)
					}
				}
				for _, contextTokens := range contexts {
					expected := official
					if interval := FindMatchingInterval(official.Intervals, contextTokens); interval != nil {
						expected = intervalToModelPricing(interval, official, nil)
					}
					usage := UsageTokens{InputTokens: contextTokens, OutputTokens: 200}
					if contextTokens == 1000 {
						if configured.CacheReadPrice != nil && *configured.CacheReadPrice > 0 {
							usage.CacheReadTokens = 17
						}
						if configured.CacheWritePrice != nil && *configured.CacheWritePrice > 0 {
							usage.CacheCreationTokens = 23
						}
					}
					cost, err := billing.CalculateCostUnified(CostInput{
						Ctx: ctx, Model: model, PricingPlatform: PlatformCommandCode, GroupID: &groupID,
						Tokens: usage, RateMultiplier: 1, PricingAt: at, Resolver: resolver,
					})
					require.NoError(t, err)
					original := billing.computeTokenBreakdown(expected, usage, 1, "", false)
					require.InDelta(t, original.TotalCost, cost.TotalCost, 1e-10, "context=%d", contextTokens)
					require.NoError(t, billing.applyPlatformQuotaCostAt(cost, PlatformCommandCode, model, at))
					quota, available := entry.MonthlyQuota.ResolveAt(at, commandCodeSharedMonthlyQuotaUSD)
					if available {
						require.InDelta(t, original.TotalCost*quota.CostMultiplier, cost.ActualCost, 1e-10)
					} else {
						require.False(t, cost.AllowZeroRate)
						require.Positive(t, cost.ActualCost)
						require.InDelta(t, original.TotalCost, cost.ActualCost, 1e-12)
					}
				}
			})
		}
	}
}

func TestCommandCodeChannelChangesImmediatelyUpdateModelsDisplayAndBilling(t *testing.T) {
	ctx := context.Background()
	groupID := int64(1)
	current := Channel{ID: 1, Name: "Command Code", Status: StatusActive, GroupIDs: []int64{groupID},
		BillingModelSource: BillingModelSourceRequested, ModelPricing: commandCodeBillingSeedPricing(t)}
	repo := &mockChannelRepository{
		listAllFn: func(context.Context) ([]Channel, error) { return []Channel{*current.Clone()}, nil },
		getGroupPlatformsFn: func(context.Context, []int64) (map[int64]string, error) {
			return map[int64]string{groupID: PlatformCommandCode}, nil
		},
	}
	billing := &BillingService{}
	channels := NewChannelService(repo, nil, nil, nil, billing)
	resolver := NewModelPricingResolver(channels, billing)
	gateway := &GatewayService{channelService: channels, resolver: resolver, billingService: billing,
		modelsListCache: gocache.New(time.Hour, 0)}
	gateway.modelsListCache.Set(modelsListCacheKey(&groupID, PlatformCommandCode), []string{"outdated-model"}, time.Hour)
	require.NotContains(t, gateway.GetAvailableModels(ctx, &groupID, PlatformCommandCode), "outdated-model")

	price := 7e-6
	current.ModelPricing = []ChannelModelPricing{{Platform: PlatformCommandCode, Models: []string{"command-code-custom-model"},
		BillingMode: BillingModeToken, InputPrice: &price, OutputPrice: &price,
		MonthlyQuota: &MonthlyQuotaPolicy{BaseCreditsUSD: 35}}}
	channels.InvalidateCache()
	require.Equal(t, []string{"command-code-custom-model"}, gateway.GetAvailableModels(ctx, &groupID, PlatformCommandCode))
	display := channels.BuildSupportedModelForPricingGroup(ctx, groupID, "command-code-custom-model", PlatformCommandCode, nil)
	require.Equal(t, PricingSourceChannel, display.PricingSource)
	require.InDelta(t, price, *display.Pricing.InputPrice, 1e-15)
	require.Equal(t, 2.0, display.QuotaCost.CostMultiplier)
	require.Empty(t, display.Pricing.Intervals)
	require.Empty(t, display.PricingTimeBands)

	apiKey := &APIKey{GroupID: &groupID, Group: &Group{ID: groupID, Platform: PlatformCommandCode, LongContextPricingEnabled: true}}
	result := &ForwardResult{Model: "command-code-custom-model", Usage: ClaudeUsage{InputTokens: 1000, OutputTokens: 200}}
	cost, _, err := gateway.calculateRecordUsageCostFromCandidates(ctx, result, apiKey,
		[]string{"command-code-custom-model"}, 3, 1, 1, time.Now(), &recordUsageOpts{PricingPlatform: PlatformCommandCode})
	require.NoError(t, err)
	require.InDelta(t, 1200*price*3*2, cost.ActualCost, 1e-12)
	result.SearchCount = 1
	withSearch, _, err := gateway.calculateRecordUsageCostFromCandidates(ctx, result, apiKey,
		[]string{"command-code-custom-model"}, 3, 1, 1, time.Now(), &recordUsageOpts{PricingPlatform: PlatformCommandCode})
	require.NoError(t, err)
	require.InDelta(t, cost.ActualCost+defaultSearchPricePer1k/1000*2, withSearch.ActualCost, 1e-12)

	current.ModelPricing = nil
	channels.InvalidateCache()
	models := gateway.GetAvailableModels(ctx, &groupID, PlatformCommandCode)
	require.NotNil(t, models)
	require.Empty(t, models)
	resolved := resolver.Resolve(ctx, PricingInput{Model: "gpt-5.6-sol", Platform: PlatformCommandCode, GroupID: &groupID})
	require.Equal(t, PricingSourceMissing, resolved.Source)
	require.Nil(t, resolved.BasePricing)
}

func TestCommandCodeChannelDisplaysEffectiveTierAndTimePrice(t *testing.T) {
	input, output, tierMultiplier := 1e-6, 2e-6, 3.0
	pricing := ChannelModelPricing{Platform: PlatformCommandCode, Models: []string{"custom-tier-model"},
		BillingMode: BillingModeToken, InputPrice: &input, OutputPrice: &output,
		Intervals: []PricingInterval{{MinTokens: 0, InputMultiplier: &tierMultiplier}},
		TimePricing: &ChannelTimePricing{Timezone: "UTC", Periods: []ChannelTimePricingPeriod{
			{StartTime: "01:00:00", EndTime: "04:00:00", Multiplier: 2},
		}},
	}
	at := time.Date(2026, 10, 12, 2, 0, 0, 0, time.UTC)
	display := commandCodeChannelDisplayPricingAt(&pricing, at)
	require.InDelta(t, 6e-6, *display.Intervals[0].InputPrice, 1e-15)
	require.InDelta(t, 4e-6, *display.Intervals[0].OutputPrice, 1e-15)
	require.Nil(t, pricing.Intervals[0].InputPrice)
	require.Empty(t, commandCodeChannelPricingTimeBandsAt(&pricing, at))
	_, billing, resolver := commandCodeBillingTestService([]ChannelModelPricing{pricing})
	groupID := int64(1)
	cost, err := billing.CalculateCostUnified(CostInput{Ctx: context.Background(), Model: pricing.Models[0],
		PricingPlatform: PlatformCommandCode, GroupID: &groupID, Resolver: resolver,
		PricingAt: at, RateMultiplier: 1, Tokens: UsageTokens{InputTokens: 1000, OutputTokens: 200}})
	require.NoError(t, err)
	require.InDelta(t, 1000**display.Intervals[0].InputPrice+200**display.Intervals[0].OutputPrice, cost.TotalCost, 1e-12)
}

func TestCommandCodeChannelFlatPriceDoesNotRestoreOfficialTiersOrTimeBands(t *testing.T) {
	price := 9e-6
	pricing := []ChannelModelPricing{{Platform: PlatformCommandCode, Models: []string{"Qwen/Qwen3.7-Plus", "deepseek/deepseek-v4.1-flash"},
		BillingMode: BillingModeToken, InputPrice: &price, OutputPrice: &price}}
	channels, billing, resolver := commandCodeBillingTestService(pricing)
	groupID := int64(1)
	ctx := context.Background()
	for _, model := range pricing[0].Models {
		resolved := resolver.Resolve(ctx, PricingInput{Model: model, Platform: PlatformCommandCode, GroupID: &groupID})
		require.Empty(t, resolved.Intervals)
		display := channels.BuildSupportedModelForPricingGroup(ctx, groupID, model, PlatformCommandCode, nil)
		require.Empty(t, display.PricingTimeBands)
		require.Nil(t, display.QuotaCost)
		cost, err := billing.CalculateCostUnified(CostInput{Ctx: ctx, Model: model, PricingPlatform: PlatformCommandCode,
			GroupID: &groupID, Tokens: UsageTokens{InputTokens: 300_000}, RateMultiplier: 1, Resolver: resolver})
		require.NoError(t, err)
		require.InDelta(t, 300_000*price, cost.ActualCost, 1e-12)
	}
}

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCommandCodeReferencePricingCoversOfficialGoatRates(t *testing.T) {
	offPeak := nowForTest()

	pricing, ok := commandCodeReferencePricingAt("gpt-5.6-luna", offPeak)
	require.True(t, ok)
	require.InDelta(t, 0.2e-6, pricing.InputPricePerToken, 1e-12)
	require.InDelta(t, 1.2e-6, pricing.OutputPricePerToken, 1e-12)
	require.InDelta(t, 0.02e-6, pricing.CacheReadPricePerToken, 1e-12)
	require.InDelta(t, 0.25e-6, pricing.CacheCreationPricePerToken, 1e-12)
	require.Len(t, pricing.Intervals, 2)

	pricing, ok = commandCodeReferencePricingAt("google/gemini-3.8-flash", offPeak)
	require.True(t, ok)
	require.InDelta(t, 1.5e-6, pricing.InputPricePerToken, 1e-12)
	require.InDelta(t, 7.5e-6, pricing.OutputPricePerToken, 1e-12)
	require.InDelta(t, 0.15e-6, pricing.CacheReadPricePerToken, 1e-12)

	pricing, ok = commandCodeReferencePricingAt("deepseek/deepseek-v4-flash-fast", offPeak)
	require.True(t, ok)
	require.InDelta(t, 0.28e-6, pricing.InputPricePerToken, 1e-12)
	require.InDelta(t, 0.56e-6, pricing.OutputPricePerToken, 1e-12)
	require.InDelta(t, 0.07e-6, pricing.CacheReadPricePerToken, 1e-12)

	pricing, ok = commandCodeReferencePricingAt("Qwen/Qwen3.8-Max-0902", offPeak)
	require.True(t, ok)
	require.InDelta(t, 2e-6, pricing.InputPricePerToken, 1e-12)
	require.InDelta(t, 6e-6, pricing.OutputPricePerToken, 1e-12)
	require.InDelta(t, 0.25e-6, pricing.CacheReadPricePerToken, 1e-12)
	require.Zero(t, pricing.CacheCreationPricePerToken)

	pricing, ok = commandCodeReferencePricingAt("tencent/hy4-preview", offPeak)
	require.True(t, ok)
	require.InDelta(t, 0.834e-6, pricing.InputPricePerToken, 1e-12)
	require.InDelta(t, 2.501e-6, pricing.OutputPricePerToken, 1e-12)
	require.InDelta(t, 0.042e-6, pricing.CacheReadPricePerToken, 1e-12)

	pricing, ok = commandCodeReferencePricingAt("deepseek/deepseek-v4-flash", offPeak)
	require.True(t, ok)
	require.InDelta(t, 0.22e-6, pricing.InputPricePerToken, 1e-12)
	require.InDelta(t, 0.66e-6, pricing.OutputPricePerToken, 1e-12)
	require.InDelta(t, 0.007e-6, pricing.CacheReadPricePerToken, 1e-12)

	pricing, ok = commandCodeReferencePricingAt("Qwen/Qwen3.7-Max", offPeak)
	require.True(t, ok)
	require.InDelta(t, 2.5e-6, pricing.InputPricePerToken, 1e-12)
	require.InDelta(t, 7.5e-6, pricing.OutputPricePerToken, 1e-12)
	require.InDelta(t, 0.5e-6, pricing.CacheReadPricePerToken, 1e-12)
	require.InDelta(t, 3.13e-6, pricing.CacheCreationPricePerToken, 1e-12)

	pricing, ok = commandCodeReferencePricingAt("Qwen/Qwen3.7-Flash", offPeak)
	require.True(t, ok)
	require.Len(t, pricing.Intervals, 3)
	require.InDelta(t, 0.03e-6, *pricing.Intervals[0].InputPrice, 1e-12)
	require.InDelta(t, 0.10e-6, *pricing.Intervals[1].InputPrice, 1e-12)
	require.InDelta(t, 0.20e-6, *pricing.Intervals[2].InputPrice, 1e-12)
}

func TestCommandCodeReferencePricingResolvesAliasesAndCase(t *testing.T) {
	for _, model := range []string{
		"deepseek-v4-flash",
		"DeepSeek/DeepSeek-V4-Flash",
		"kimi-k3",
		"moonshotai/Kimi-K3",
		"qwen-3.7-plus",
		"qwen3.7-plus",
		"grok-4.6",
		"glm-5.3-flash",
		"gemini-3.8-flash",
		"qwen-3.8-max-0902",
		"deepseek-v4-flash-fast",
		"hy4-preview",
		"longcat-2.0",
		"muse-spark-1.3-contributor",
		"laguna-s-2.1",
	} {
		_, ok := commandCodeReferencePricingAt(model, nowForTest())
		require.True(t, ok, "expected pricing for %q", model)
	}

	_, ok := commandCodeReferencePricingAt("totally-unknown-model", nowForTest())
	require.False(t, ok)
}

func TestCommandCodeReferencePricingAppliesOfficialDeepSeekPeakWindows(t *testing.T) {
	base := time.Date(2026, 8, 26, 0, 30, 0, 0, time.UTC)

	offPeak, ok := commandCodeReferencePricingAt("deepseek/deepseek-v4-pro", base)
	require.True(t, ok)
	require.InDelta(t, 0.66e-6, offPeak.InputPricePerToken, 1e-12)
	require.InDelta(t, 1.98e-6, offPeak.OutputPricePerToken, 1e-12)

	peak, ok := commandCodeReferencePricingAt("deepseek/deepseek-v4-pro", base.Add(45*time.Minute))
	require.True(t, ok)
	require.InDelta(t, 1.32e-6, peak.InputPricePerToken, 1e-12)
	require.InDelta(t, 3.96e-6, peak.OutputPricePerToken, 1e-12)

	peak, ok = commandCodeReferencePricingAt("deepseek/deepseek-v4-flash-vision-exp", base.Add(6*time.Hour))
	require.True(t, ok)
	require.InDelta(t, 0.44e-6, peak.InputPricePerToken, 1e-12)

	// Flash Fast 没有官方峰谷元数据，峰时仍使用自身固定价格。
	fixed, ok := commandCodeReferencePricingAt("deepseek/deepseek-v4-flash-fast", base.Add(6*time.Hour))
	require.True(t, ok)
	require.InDelta(t, 0.28e-6, fixed.InputPricePerToken, 1e-12)
	require.InDelta(t, 0.56e-6, fixed.OutputPricePerToken, 1e-12)

	beforeEffective := time.Date(2026, 8, 1, 1, 30, 0, 0, time.UTC)
	pricing, ok := commandCodeReferencePricingAt("deepseek/deepseek-v4-flash", beforeEffective)
	require.True(t, ok)
	require.InDelta(t, 0.22e-6, pricing.InputPricePerToken, 1e-12)
}

func TestCommandCodeScheduledPriceChangeActivatesAtOfficialInstant(t *testing.T) {
	entry := commandCodeCatalogEntry{
		ID:            "test/scheduled-model",
		ContextWindow: 100_000,
		Tiers: []commandCodeCatalogTier{{
			Rates: commandCodeRates(1, 2, 0.1),
		}},
		ScheduledChange: &commandCodeCatalogScheduledChange{
			Effective: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
			Rates:     commandCodeRates(3, 4, 0.3),
		},
	}

	before, ok := commandCodeCatalogPricingAt(entry, time.Date(2026, 8, 31, 23, 59, 59, 0, time.UTC))
	require.True(t, ok)
	require.InDelta(t, 1e-6, before.InputPricePerToken, 1e-12)

	after, ok := commandCodeCatalogPricingAt(entry, entry.ScheduledChange.Effective)
	require.True(t, ok)
	require.InDelta(t, 3e-6, after.InputPricePerToken, 1e-12)
	require.InDelta(t, 4e-6, after.OutputPricePerToken, 1e-12)
}

func TestCommandCodeCatalogPricingUsesListRatesAfterPromotionExpiry(t *testing.T) {
	expiresAt := time.Date(2026, 12, 31, 23, 59, 59, 0, time.UTC)
	listRates := commandCodeRates(1.5, 7.5, 0.15, 0.08334)
	entry := commandCodeCatalogEntry{
		ID:            "test/discounted-model",
		ContextWindow: 1_000_000,
		Tiers: []commandCodeCatalogTier{{
			Rates:     commandCodeRates(0.75, 3.75, 0.075, 0.04167),
			ListRates: &listRates,
		}},
		Deal: &commandCodeCatalogDeal{
			Code: "limited-discount", Label: "50% off", DiscountPercent: 50, ExpiresAt: expiresAt,
		},
	}

	before, ok := commandCodeCatalogPricingAt(entry, expiresAt.Add(-time.Second))
	require.True(t, ok)
	require.InDelta(t, 0.75e-6, before.InputPricePerToken, 1e-12)
	require.InDelta(t, 3.75e-6, before.OutputPricePerToken, 1e-12)

	after, ok := commandCodeCatalogPricingAt(entry, expiresAt.Add(time.Second))
	require.True(t, ok)
	require.InDelta(t, 1.5e-6, after.InputPricePerToken, 1e-12)
	require.InDelta(t, 7.5e-6, after.OutputPricePerToken, 1e-12)
	require.InDelta(t, 0.08334e-6, after.CacheCreationPricePerToken, 1e-12)
}

func TestCommandCodeCurrentFreeModelsAndRemovedDeals(t *testing.T) {
	now := nowForTest()
	for _, model := range []string{
		"meituan/LongCat-2.0:free",
		"poolside/laguna-s-2.1-free",
	} {
		free, ok := commandCodeReferencePricingAt(model, now)
		require.True(t, ok, model)
		require.True(t, free.AllowZeroRate, model)
	}

	for _, removed := range []string{
		"minimax/minimax-m3-free",
		"minimax/minimax-m2.7-free",
	} {
		_, ok := commandCodeReferencePricingAt(removed, now)
		require.False(t, ok, removed)
	}

	entry := commandCodeCatalogEntry{
		ID:            "test/expired-free",
		ContextWindow: 100_000,
		Tiers:         []commandCodeCatalogTier{{Rates: commandCodeRates(0, 0, 0)}},
		Deal: &commandCodeCatalogDeal{
			Code: "free", Label: "Free", DiscountPercent: 100, Free: true,
			ExpiresAt: now.Add(-time.Second),
		},
	}
	_, ok := commandCodeCatalogPricingAt(entry, now)
	require.False(t, ok)
}

func TestCommandCodeChannelFlatOverrideMergesIntoEveryOfficialTier(t *testing.T) {
	base, ok := commandCodeReferencePricingAt("Qwen/Qwen3.7-Plus", nowForTest())
	require.True(t, ok)
	customInput := 9e-6
	resolved := &ResolvedPricing{
		Mode:                  BillingModeToken,
		BasePricing:           base,
		Intervals:             append([]PricingInterval(nil), base.Intervals...),
		IntervalsRequireMatch: true,
	}
	resolver := &ModelPricingResolver{}
	resolver.applyTokenOverrides(&ChannelModelPricing{InputPrice: &customInput}, resolved)

	require.Len(t, resolved.Intervals, 2)
	require.InDelta(t, customInput, *resolved.Intervals[0].InputPrice, 1e-15)
	require.InDelta(t, customInput, *resolved.Intervals[1].InputPrice, 1e-15)
	require.InDelta(t, 1.6e-6, *resolved.Intervals[0].OutputPrice, 1e-15)
	require.InDelta(t, 4.8e-6, *resolved.Intervals[1].OutputPrice, 1e-15)

	longContext := resolver.GetIntervalPricing(resolved, 256_001)
	require.InDelta(t, customInput, longContext.InputPricePerToken, 1e-15)
	require.InDelta(t, 4.8e-6, longContext.OutputPricePerToken, 1e-15)
	upperBound := 100
	bounded := &ResolvedPricing{
		BasePricing:           base,
		Intervals:             []PricingInterval{{MinTokens: 0, MaxTokens: &upperBound, InputPrice: &customInput}},
		IntervalsRequireMatch: true,
	}
	require.Nil(t, resolver.GetIntervalPricing(bounded, 101))
}

func TestCommandCodeQuotaCostAppliesOfficialMonthlyCreditsMultiplier(t *testing.T) {
	svc := &BillingService{}

	// GPT-5.6 Luna：官方 credits $20 → 倍率 70/20 = 3.5x
	quotaCost, ok := svc.GetCommandCodeQuotaCost("gpt-5.6-luna")
	require.True(t, ok)
	require.InDelta(t, 20, quotaCost.IncludedMonthlyUsageUSD, 1e-9)
	require.InDelta(t, 3.5, quotaCost.Multiplier, 1e-9)

	// DeepSeek V4 Flash：官方 credits $60 → 倍率 70/60 ≈ 1.1667x
	quotaCost, ok = svc.GetCommandCodeQuotaCost("deepseek/deepseek-v4-flash")
	require.True(t, ok)
	require.InDelta(t, 60, quotaCost.IncludedMonthlyUsageUSD, 1e-9)
	require.InDelta(t, 70.0/60.0, quotaCost.Multiplier, 1e-9)

	// GLM-5.2：官方 credits $70 → 倍率 1x
	quotaCost, ok = svc.GetCommandCodeQuotaCost("zai-org/glm-5.2")
	require.True(t, ok)
	require.InDelta(t, 70, quotaCost.IncludedMonthlyUsageUSD, 1e-9)
	require.InDelta(t, 1.0, quotaCost.Multiplier, 1e-9)

	// MiniMax M3：官方 credits $47 → 倍率 70/47
	quotaCost, ok = svc.GetCommandCodeQuotaCost("MiniMaxAI/MiniMax-M3")
	require.True(t, ok)
	require.InDelta(t, 47, quotaCost.IncludedMonthlyUsageUSD, 1e-9)
	require.InDelta(t, 70.0/47.0, quotaCost.Multiplier, 1e-9)

	// Gemini 3.8 Flash：官方 credits $40 → 倍率 1.75x
	quotaCost, ok = svc.GetCommandCodeQuotaCost("google/gemini-3.8-flash")
	require.True(t, ok)
	require.InDelta(t, 40, quotaCost.IncludedMonthlyUsageUSD, 1e-9)
	require.InDelta(t, 1.75, quotaCost.Multiplier, 1e-9)

	// 免费模型无额度倍率，由 AllowZeroRate 路径按 $0 计费。
	_, ok = svc.GetCommandCodeQuotaCost("meituan/LongCat-2.0:free")
	require.False(t, ok)

	// 未知模型闭合失败
	_, ok = svc.GetCommandCodeQuotaCost("totally-unknown-model")
	require.False(t, ok)
}

func TestCommandCodePricingMetadataMatchesCurrentOfficialPromotions(t *testing.T) {
	contextWindow, promotion, ok := commandCodeReferenceMetadataAt("xiaomi/mimo-v2.5", nowForTest())
	require.True(t, ok)
	require.Equal(t, 1_000_000, contextWindow)
	require.NotNil(t, promotion)
	require.Equal(t, "98% off", promotion.Label)
	require.Equal(t, float64(98), promotion.DiscountPercent)
	require.Nil(t, promotion.ExpiresAt)

	contextWindow, promotion, ok = commandCodeReferenceMetadataAt("google/gemini-3.7-flash", nowForTest())
	require.True(t, ok)
	require.Equal(t, 1_048_576, contextWindow)
	require.Nil(t, promotion)
}

func TestCommandCodePricingTimeBandsExposeOfficialUTCRanges(t *testing.T) {
	bands := commandCodeReferencePricingTimeBandsAt("deepseek/deepseek-v4-flash", nowForTest())
	require.Len(t, bands, 2)
	require.Equal(t, "off_peak", bands[0].Code)
	require.Equal(t, []string{"00:00-01:00", "04:00-06:00", "10:00-24:00"}, bands[0].TimeRanges)
	require.InDelta(t, 0.22e-6, *bands[0].Pricing.InputPrice, 1e-12)
	require.Equal(t, "peak", bands[1].Code)
	require.Equal(t, []string{"01:00-04:00", "06:00-10:00"}, bands[1].TimeRanges)
	require.InDelta(t, 0.44e-6, *bands[1].Pricing.InputPrice, 1e-12)
}

func TestCommandCodeRecordUsageAppliesOfficialMonthlyCreditsMultiplier(t *testing.T) {
	svc := &BillingService{}
	gateway := &GatewayService{billingService: svc}
	apiKey := &APIKey{ID: 1}
	result := &ForwardResult{
		Model: "gpt-5.6-luna",
		Usage: ClaudeUsage{
			InputTokens:  1000,
			OutputTokens: 500,
		},
	}
	opts := &recordUsageOpts{PricingPlatform: PlatformCommandCode}

	cost, model, err := gateway.calculateRecordUsageCostFromCandidates(
		context.Background(), result, apiKey, []string{"gpt-5.6-luna"}, 1, 1, time.Time{}, opts,
	)
	require.NoError(t, err)
	require.Equal(t, "gpt-5.6-luna", model)

	// GPT-5.6 Luna：credits $20 → 倍率 70/20 = 3.5x。
	// TotalCost 保持目录原价，ActualCost 乘以模型倍率。
	require.InDelta(t, 1000*0.2e-6+500*1.2e-6, cost.TotalCost, 1e-12)
	require.InDelta(t, 3.5, cost.ModelSpecificMultiplier, 1e-12)
	require.InDelta(t, (1000*0.2e-6+500*1.2e-6)*3.5, cost.ActualCost, 1e-12)
}

func TestCommandCodeRecordUsageFailsClosedWhenCreditsMissing(t *testing.T) {
	svc := &BillingService{}
	gateway := &GatewayService{billingService: svc}
	apiKey := &APIKey{ID: 1}
	result := &ForwardResult{
		Model: "totally-unknown-model",
		Usage: ClaudeUsage{InputTokens: 10, OutputTokens: 10},
	}
	opts := &recordUsageOpts{PricingPlatform: PlatformCommandCode}

	_, _, err := gateway.calculateRecordUsageCostFromCandidates(
		context.Background(), result, apiKey, []string{"totally-unknown-model"}, 1, 1, time.Time{}, opts,
	)
	require.Error(t, err)
}

func TestBillingServiceCommandCodeContextTiersDriveActualCost(t *testing.T) {
	svc := &BillingService{}

	standard, err := svc.CalculateCostForPlatform(
		PlatformCommandCode,
		"gpt-5.6-luna",
		UsageTokens{InputTokens: 272_000, OutputTokens: 1},
		1,
	)
	require.NoError(t, err)
	require.InDelta(t, float64(272_000)*0.2e-6+1.2e-6, standard.TotalCost, 1e-12)

	longContext, err := svc.CalculateCostForPlatform(
		PlatformCommandCode,
		"gpt-5.6-luna",
		UsageTokens{InputTokens: 272_001, OutputTokens: 1},
		1,
	)
	require.NoError(t, err)
	require.InDelta(t, float64(272_001)*0.4e-6+1.8e-6, longContext.TotalCost, 1e-12)

	middle, err := svc.CalculateCostForPlatform(
		PlatformCommandCode,
		"Qwen/Qwen3.7-Flash",
		UsageTokens{InputTokens: 32_001, OutputTokens: 1},
		1,
	)
	require.NoError(t, err)
	require.InDelta(t, float64(32_001)*0.1e-6+0.4e-6, middle.TotalCost, 1e-12)
}

func TestBillingServiceCommandCodeContextIncludesCacheTokens(t *testing.T) {
	svc := &BillingService{}
	cost, err := svc.CalculateCostForPlatform(
		PlatformCommandCode,
		"xai/grok-4.6",
		UsageTokens{InputTokens: 1, CacheReadTokens: 200_000, OutputTokens: 1},
		1,
	)
	require.NoError(t, err)
	require.InDelta(t, 1*4e-6+200_000*1e-6+1*12e-6, cost.TotalCost, 1e-12)
}

func TestBillingServiceCommandCodePlatformPricingFailsClosed(t *testing.T) {
	svc := &BillingService{}
	pricing, err := svc.GetModelPricingForPlatform(PlatformCommandCode, "gpt-5.6-luna")
	require.NoError(t, err)
	require.NotNil(t, pricing)

	_, err = svc.GetModelPricingForPlatform(PlatformCommandCode, "unknown-model")
	require.Error(t, err)
	_, err = svc.GetModelPricing("unknown-model")
	require.Error(t, err)
}

func TestGatewayServiceValidateGatewayTokenPricingAvailableCommandCode(t *testing.T) {
	gateway := &GatewayService{billingService: &BillingService{}}
	account := &Account{ID: 201, Platform: PlatformCommandCode, Type: AccountTypeAPIKey}

	err := gateway.ValidateGatewayTokenPricingAvailable(context.Background(), &APIKey{ID: 1}, account, "gpt-5.6-luna", ChannelMappingResult{})
	require.NoError(t, err)

	err = gateway.ValidateGatewayTokenPricingAvailable(context.Background(), &APIKey{ID: 1}, account, "totally-unknown-model", ChannelMappingResult{})
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrModelPricingUnavailable))
}

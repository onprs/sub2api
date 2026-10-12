package service

import (
	"context"
	"slices"
	"time"
)

const AccountStatsPricingSourceChannel = "channel_model_multiplier"

// AccountStatsRulePricingAt 从渠道生成当前账号规则的模型和价格，避免维护第二份目录。
func (c *Channel) AccountStatsRulePricingAt(rule AccountStatsPricingRule, at time.Time) []ChannelModelPricing {
	if rule.PricingSource != AccountStatsPricingSourceChannel {
		return rule.Pricing
	}
	pricing := make([]ChannelModelPricing, 0)
	for _, entry := range c.ModelPricing {
		if entry.Platform != rule.Platform {
			continue
		}
		cloned := entry.Clone()
		multiplier := entry.TimePricing.MultiplierAt(at)
		if entry.MonthlyQuota != nil {
			sharedCredits := openCodeSharedMonthlyQuotaUSD
			if rule.Platform == PlatformCommandCode {
				sharedCredits = commandCodeSharedMonthlyQuotaUSD
			}
			if quota, ok := entry.MonthlyQuota.ResolveAt(at, sharedCredits); ok {
				multiplier *= quota.CostMultiplier
			}
		}
		scale := func(price **float64) {
			if *price != nil {
				value := **price * multiplier
				*price = &value
			}
		}
		for _, price := range []**float64{&cloned.InputPrice, &cloned.OutputPrice, &cloned.CacheWritePrice, &cloned.CacheWrite1hPrice, &cloned.CacheReadPrice, &cloned.ImageInputPrice, &cloned.ImageOutputPrice, &cloned.PerRequestPrice} {
			scale(price)
		}
		for i := range cloned.Intervals {
			interval := &cloned.Intervals[i]
			for _, price := range []**float64{&interval.InputPrice, &interval.OutputPrice, &interval.CacheWritePrice, &interval.CacheWrite1hPrice, &interval.CacheReadPrice, &interval.PerRequestPrice} {
				scale(price)
			}
		}
		cloned.TimePricing = nil
		cloned.MonthlyQuota = nil
		pricing = append(pricing, cloned)
	}
	return pricing
}

func tryChannelAccountStatsRule(ctx context.Context, billing *BillingService, channel *Channel, rule AccountStatsPricingRule, accountID, groupID int64, platform, model string, tokens UsageTokens, requestCount int, serviceTier, reasoningEffort string, at time.Time) *float64 {
	if rule.Platform != platform {
		return nil
	}
	matches := matchAccountStatsRule(&rule, accountID, groupID)
	// 未指定账号或分组时覆盖该渠道的全部分组，包括后续新增的关联分组。
	if len(rule.AccountIDs) == 0 && len(rule.GroupIDs) == 0 {
		matches = slices.Contains(channel.GroupIDs, groupID)
	}
	if !matches {
		return nil
	}
	pricing := findPricingForModelCandidates(channel.ModelPricing, platform, model)
	if pricing == nil {
		return nil
	}
	if billing == nil {
		billing = &BillingService{}
	}
	resolver := NewModelPricingResolver(nil, billing)
	mode := pricing.BillingMode
	if mode == "" {
		mode = BillingModeToken
	}
	resolved := &ResolvedPricing{Mode: mode, Source: PricingSourceChannel, channelPricing: pricing, longContextPricingEnabled: true}
	if mode == BillingModeToken {
		resolver.applyTokenOverrides(pricing, resolved)
		resolved.BasePricing.AllowZeroRate = commandCodeChannelPricingIsFree(pricing)
	} else {
		resolver.applyRequestTierOverrides(pricing, resolved)
	}
	cost, err := billing.CalculateCostUnified(CostInput{
		Ctx: ctx, Model: model, PricingPlatform: platform, Tokens: tokens,
		RequestCount: requestCount, RateMultiplier: 1, PricingAt: at,
		ServiceTier: serviceTier, ReasoningEffort: reasoningEffort,
		Resolver: resolver, Resolved: resolved,
	})
	if err != nil || cost == nil {
		return nil
	}
	value := costBeforeGroupMultiplier(cost)
	return &value
}

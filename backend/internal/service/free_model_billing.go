package service

import "time"

// freeModelBillingPricePerToken 将免费模型的统一售价从每百万 token 换算为每 token。
const freeModelBillingPricePerToken = 0.001 / 1_000_000

func newFreeModelBillingPricing() *ModelPricing {
	fast, flex, maxReasoning := 1.0, 1.0, 1.0
	return &ModelPricing{
		InputPricePerToken:          freeModelBillingPricePerToken,
		OutputPricePerToken:         freeModelBillingPricePerToken,
		CacheCreationPricePerToken:  freeModelBillingPricePerToken,
		CacheReadPricePerToken:      freeModelBillingPricePerToken,
		CacheCreation5mPrice:        freeModelBillingPricePerToken,
		CacheCreation1hPrice:        freeModelBillingPricePerToken,
		CacheCreationPriceExplicit:  true,
		SupportsCacheBreakdown:      true,
		ImageInputPricePerToken:     freeModelBillingPricePerToken,
		ImageCacheReadPricePerToken: freeModelBillingPricePerToken,
		ImageOutputPricePerToken:    freeModelBillingPricePerToken,
		ImageOutputPriceExplicit:    true,
		FastMultiplier:              &fast, FlexMultiplier: &flex, MaxReasoningEffortMultiplier: &maxReasoning,
	}
}

// 官方免费标记与零单价共同确认，避免把缺价或已结束的免费活动当作免费模型。
func isFreeModelReferencePricing(pricing *ModelPricing) bool {
	if pricing == nil || !pricing.AllowZeroRate {
		return false
	}
	for _, price := range []float64{
		pricing.InputPricePerToken, pricing.InputPricePerTokenPriority, pricing.OutputPricePerToken, pricing.OutputPricePerTokenPriority,
		pricing.CacheCreationPricePerToken, pricing.CacheCreationPricePerTokenPriority, pricing.CacheReadPricePerToken, pricing.CacheReadPricePerTokenPriority,
		pricing.CacheCreation5mPrice, pricing.CacheCreation1hPrice, pricing.ImageInputPricePerToken, pricing.ImageOutputPricePerToken, pricing.ImageCacheReadPricePerToken,
	} {
		if price != 0 {
			return false
		}
	}
	for _, interval := range pricing.Intervals {
		for _, price := range []*float64{interval.InputPrice, interval.OutputPrice, interval.CacheWritePrice, interval.CacheWrite1hPrice, interval.CacheReadPrice} {
			if price != nil && *price != 0 {
				return false
			}
		}
	}
	return true
}

func isCommandCodeFreeModelAt(model string, at time.Time) bool {
	if at.IsZero() {
		at = time.Now()
	}
	for _, candidate := range billingModelPricingCandidates(model) {
		if pricing, ok := commandCodeReferencePricingAt(candidate, at); ok && isFreeModelReferencePricing(pricing) {
			return true
		}
	}
	return false
}

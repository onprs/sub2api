package service

import "time"

// applyPlatformQuotaCostAt 让不同协议入口共用平台月额度折算规则。
func (s *BillingService) applyPlatformQuotaCostAt(cost *CostBreakdown, platform, model string, at time.Time) error {
	if (platform != PlatformCommandCode && platform != PlatformOpenCodeGo) || cost == nil || cost.platformQuotaApplied {
		return nil
	}
	if platform == PlatformOpenCodeGo {
		pricing, ok := openCodeReferencePricing(model)
		if !ok {
			return quotaCostUnavailableError(model)
		}
		return applyOpenCodeChannelQuotaCostAt(cost, pricing, at)
	}
	if cost.AllowZeroRate {
		applyModelSpecificMultiplierToCost(cost, 1)
		return nil
	}
	quota, ok := s.GetCommandCodeQuotaCostAt(model, at)
	if !ok {
		return quotaCostUnavailableError(model)
	}
	applyModelSpecificMultiplierToCost(cost, quota.Multiplier)
	return nil
}

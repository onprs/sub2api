package service

import "time"

// applyPlatformQuotaCostAt 让不同协议入口共用平台月额度折算规则。
func (s *BillingService) applyPlatformQuotaCostAt(cost *CostBreakdown, platform, model string, at time.Time) error {
	if platform != PlatformCommandCode || cost == nil {
		return nil
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

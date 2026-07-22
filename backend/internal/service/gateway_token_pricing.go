package service

import (
	"context"
	"strings"
)

// ValidateGatewayTokenPricingAvailable performs a fast preflight for gateway
// paths that cannot safely proceed when token pricing is missing. This is
// especially important for providers with open-ended model catalogs, which
// must not silently fall back to zero-cost billing for unknown models.
func (s *GatewayService) ValidateGatewayTokenPricingAvailable(ctx context.Context, apiKey *APIKey, account *Account, requestedModel string, mapping ChannelMappingResult) error {
	if s == nil || account == nil || (!account.IsOpenCodeGo() && !account.IsClinePass()) {
		return nil
	}

	billingModel := strings.TrimSpace(requestedModel)
	if mapping.BillingModelSource == BillingModelSourceChannelMapped && strings.TrimSpace(mapping.MappedModel) != "" {
		billingModel = strings.TrimSpace(mapping.MappedModel)
	}
	if mapping.BillingModelSource == BillingModelSourceRequested && strings.TrimSpace(requestedModel) != "" {
		billingModel = strings.TrimSpace(requestedModel)
	}
	if billingModel == "" {
		return tokenPricingUnavailableError(requestedModel)
	}

	accountMappedModel := strings.TrimSpace(account.GetMappedModel(requestedModel))
	candidates := billableUsageModelCandidates(billingModel, mapping.MappedModel, accountMappedModel, requestedModel)
	if s.resolver != nil && apiKey != nil && apiKey.GroupID != nil {
		for _, candidate := range candidates {
			candidate = strings.TrimSpace(candidate)
			if candidate == "" {
				continue
			}
			resolved := s.resolver.Resolve(ctx, PricingInput{Model: candidate, GroupID: apiKey.GroupID})
			if resolved == nil {
				continue
			}
			if resolved.Mode == BillingModePerRequest || resolved.Mode == BillingModeImage {
				return nil
			}
			pricing := s.resolver.GetIntervalPricing(resolved, 0)
			if hasBillableTokenPricing(pricing) {
				return nil
			}
		}
		return tokenPricingUnavailableError(billingModel)
	}

	billingService := s.billingService
	if billingService == nil {
		billingService = NewBillingService(s.cfg, nil)
	}
	for _, candidate := range candidates {
		pricing, err := billingService.GetModelPricing(candidate)
		if err == nil && hasBillableTokenPricing(pricing) {
			return nil
		}
	}
	return tokenPricingUnavailableError(billingModel)
}

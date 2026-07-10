package service

import (
	"context"
	"strings"
)

// ValidateGatewayTokenPricingAvailable performs a fast preflight for gateway
// paths that cannot safely proceed when token pricing is missing. This is
// especially important for OpenCode Go, whose model catalog is open-ended and
// must not silently fall back to zero-cost billing for unknown models.
func (s *GatewayService) ValidateGatewayTokenPricingAvailable(ctx context.Context, apiKey *APIKey, account *Account, requestedModel string, mapping ChannelMappingResult) error {
	if s == nil || account == nil || !account.IsOpenCodeGo() {
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

	if s.resolver != nil && apiKey != nil && apiKey.GroupID != nil {
		resolved := s.resolver.Resolve(ctx, PricingInput{Model: billingModel, GroupID: apiKey.GroupID})
		if resolved == nil {
			return tokenPricingUnavailableError(billingModel)
		}
		if resolved.Mode == BillingModePerRequest || resolved.Mode == BillingModeImage {
			return nil
		}
		pricing := s.resolver.GetIntervalPricing(resolved, 0)
		if hasBillableTokenPricing(pricing) {
			return nil
		}
		return tokenPricingUnavailableError(billingModel)
	}

	billingService := s.billingService
	if billingService == nil {
		billingService = NewBillingService(s.cfg, nil)
	}
	pricing, err := billingService.GetModelPricing(billingModel)
	if err != nil {
		return err
	}
	if !hasBillableTokenPricing(pricing) {
		return tokenPricingUnavailableError(billingModel)
	}
	return nil
}

package service

import (
	"context"
	"fmt"
	"strings"
)

// ValidateGatewayTokenPricingAvailable performs a fast preflight for gateway
// paths that cannot safely proceed when token pricing is missing. This is
// especially important for providers with open-ended model catalogs, which
// must not silently fall back to zero-cost billing for unknown models.
func (s *GatewayService) ValidateGatewayTokenPricingAvailable(ctx context.Context, apiKey *APIKey, account *Account, requestedModel string, mapping ChannelMappingResult) error {
	if s == nil || account == nil || (!account.IsOpenCodeGo() && !account.IsClinePass() && !account.IsOpenRouter()) {
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
	billingService := s.billingService
	if billingService == nil {
		billingService = NewBillingService(s.cfg, nil)
	}
	var quotaCostErr error
	if s.resolver != nil && apiKey != nil && apiKey.GroupID != nil {
		for _, candidate := range candidates {
			candidate = strings.TrimSpace(candidate)
			if candidate == "" {
				continue
			}
			if account.IsOpenCodeGo() {
				if _, ok := billingService.GetOpenCodeGoQuotaCost(candidate); !ok {
					quotaCostErr = openCodeGoQuotaCostUnavailableError(candidate)
					continue
				}
			}
			resolved := s.resolver.Resolve(ctx, PricingInput{Model: candidate, GroupID: apiKey.GroupID, Platform: account.Platform})
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
		if quotaCostErr != nil {
			return quotaCostErr
		}
		return tokenPricingUnavailableError(billingModel)
	}

	for _, candidate := range candidates {
		if account.IsOpenCodeGo() {
			if _, ok := billingService.GetOpenCodeGoQuotaCost(candidate); !ok {
				quotaCostErr = openCodeGoQuotaCostUnavailableError(candidate)
				continue
			}
		}
		pricing, err := billingService.GetModelPricingForPlatform(account.Platform, candidate)
		if err == nil && hasBillableTokenPricing(pricing) {
			return nil
		}
	}
	if quotaCostErr != nil {
		return quotaCostErr
	}
	return tokenPricingUnavailableError(billingModel)
}

func openCodeGoQuotaCostUnavailableError(model string) error {
	return fmt.Errorf("%w: OpenCode Go quota cost multiplier unavailable for model: %s", ErrModelPricingUnavailable, strings.ToLower(strings.TrimSpace(model)))
}

package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestClinePassBillingModelPricingCandidatesUseBoundedPrefixes(t *testing.T) {
	require.Equal(t, []string{
		"cline-pass/kimi-k2.7-code",
		"clinepass/kimi-k2.7-code",
		"kimi-k2.7-code",
		"kimi-k2.7",
	}, billingModelPricingCandidates("cline-pass/kimi-k2.7-code"))
}

func TestGatewayTokenPricingClinePassFailsClosedAndUsesAccountMapping(t *testing.T) {
	pricingSvc := &PricingService{pricingData: map[string]*LiteLLMModelPricing{
		"minimax-m3": {
			Mode:               "chat",
			InputCostPerToken:  1e-6,
			OutputCostPerToken: 2e-6,
			LiteLLMProvider:    PlatformClinePass,
		},
	}}
	cfg := &config.Config{}
	svc := &GatewayService{cfg: cfg, billingService: NewBillingService(cfg, pricingSvc)}
	account := &Account{ID: 72, Platform: PlatformClinePass, Credentials: map[string]any{
		"model_mapping": map[string]any{"client-alias": "cline-pass/minimax-m3"},
	}}

	err := svc.ValidateGatewayTokenPricingAvailable(context.Background(), &APIKey{ID: 4}, account, "client-alias", ChannelMappingResult{
		MappedModel:        "client-alias",
		BillingModelSource: BillingModelSourceRequested,
	})
	require.NoError(t, err)

	err = svc.ValidateGatewayTokenPricingAvailable(context.Background(), &APIKey{ID: 4}, account, "new-unpriced-model", ChannelMappingResult{
		MappedModel:        "cline-pass/new-unpriced-model",
		BillingModelSource: BillingModelSourceChannelMapped,
	})
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
}

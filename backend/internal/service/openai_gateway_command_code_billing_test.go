//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestOpenAIGatewayRecordUsageCommandCodeCatalogBilling(t *testing.T) {
	for _, resolverEnabled := range []bool{false, true} {
		for _, groupPlatform := range []string{PlatformCommandCode, PlatformOpenAI, PlatformComposite} {
			for _, tc := range []struct {
				model  string
				input  float64
				output float64
				cache  float64
				quota  float64
			}{
				{"deepseek/deepseek-v4.1-flash", 0.15e-6, 0.60e-6, 0.003e-6, 70.0 / 60},
				{"gpt-5.6-luna", 0.2e-6, 1.2e-6, 0.02e-6, 70.0 / 20},
				{"claude-sonnet-5-5", 2e-6, 10e-6, 0.1e-6, 70.0 / 10},
				{"mistral/mistral-large-4", 1.36e-6, 4.18e-6, 0.14e-6, 70.0 / 40},
			} {
				t.Run(groupPlatform+"/"+tc.model+"/resolver="+map[bool]string{false: "false", true: "true"}[resolverEnabled], func(t *testing.T) {
					usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
					userRepo := &openAIRecordUsageUserRepoStub{}
					svc := newOpenAIRecordUsageServiceForTest(usageRepo, userRepo, &openAIRecordUsageSubRepoStub{}, nil)
					if resolverEnabled {
						svc.resolver = NewModelPricingResolver(nil, svc.billingService)
					}
					group := &Group{ID: 1, Platform: groupPlatform, RateMultiplier: 2, LongContextPricingEnabled: true}
					usage := OpenAIUsage{InputTokens: 1000, OutputTokens: 200, CacheReadInputTokens: 100}
					input := providerClaudeRecordUsageInput(PlatformCommandCode, tc.model, group, usage)
					input.PricingAt = time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
					input.Result.Stream = true

					require.NoError(t, svc.RecordUsage(context.Background(), input))

					expectedTotal := 900*tc.input + 200*tc.output + 100*tc.cache
					require.NotNil(t, usageRepo.lastLog)
					require.InDelta(t, expectedTotal, usageRepo.lastLog.TotalCost, 1e-12)
					require.InDelta(t, expectedTotal*2*tc.quota, usageRepo.lastLog.ActualCost, 1e-12)
					require.InDelta(t, 2*tc.quota, usageRepo.lastLog.RateMultiplier, 1e-12)
					require.Equal(t, 1, userRepo.deductCalls)
					require.InDelta(t, usageRepo.lastLog.ActualCost, userRepo.lastAmount, 1e-12)
				})
			}
		}
	}
}

func TestOpenAIGatewayRecordUsageCommandCodeQuotaBoundaryAndFree(t *testing.T) {
	for _, tc := range []struct {
		name  string
		model string
		at    time.Time
		quota float64
		free  bool
	}{
		{"活动内", "mistral/mistral-large-4", time.Date(2026, 10, 20, 23, 59, 59, 0, time.UTC), 70.0 / 40, false},
		{"活动截止", "mistral/mistral-large-4", time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC), 70.0 / 20, false},
		{"官方免费", "stealth/glyph-cluster:free", time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC), 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &openAIRecordUsageLogRepoStub{inserted: true}
			userRepo := &openAIRecordUsageUserRepoStub{}
			svc := newOpenAIRecordUsageServiceForTest(repo, userRepo, &openAIRecordUsageSubRepoStub{}, nil)
			svc.resolver = NewModelPricingResolver(nil, svc.billingService)
			group := &Group{ID: 1, Platform: PlatformCommandCode, RateMultiplier: 2}
			input := providerClaudeRecordUsageInput(PlatformCommandCode, tc.model, group, OpenAIUsage{InputTokens: 1000, OutputTokens: 200})
			input.PricingAt = tc.at
			require.NoError(t, svc.RecordUsage(context.Background(), input))
			require.NotNil(t, repo.lastLog)
			if tc.free {
				require.Zero(t, repo.lastLog.ActualCost)
				require.Zero(t, userRepo.deductCalls)
			} else {
				require.InDelta(t, (1000*1.36e-6+200*4.18e-6)*2*tc.quota, repo.lastLog.ActualCost, 1e-12)
			}
			require.InDelta(t, 2*tc.quota, repo.lastLog.RateMultiplier, 1e-12)
		})
	}
}

func TestOpenAIGatewayRecordUsageCommandCodeExplicitPricingAndAtomicBilling(t *testing.T) {
	repo := &openAIRecordUsageLogRepoStub{inserted: true}
	userRepo := &openAIRecordUsageUserRepoStub{}
	svc := newOpenAIRecordUsageServiceForTest(repo, userRepo, &openAIRecordUsageSubRepoStub{}, nil)
	billingRepo := &openAIRecordUsageBillingRepoStub{}
	svc.usageBillingRepo = billingRepo
	svc.resolver = NewModelPricingResolver(nil, svc.billingService)
	inputPrice, outputPrice, cachePrice := 1e-6, 2e-6, 0.3e-6
	group := &Group{ID: 1, Platform: PlatformComposite, RateMultiplier: 2, ModelPricing: []ChannelModelPricing{{
		Models: []string{"deepseek/deepseek-v4.1-flash"}, BillingMode: BillingModeToken,
		InputPrice: &inputPrice, OutputPrice: &outputPrice, CacheReadPrice: &cachePrice,
	}}}
	input := providerClaudeRecordUsageInput(PlatformCommandCode, "deepseek/deepseek-v4.1-flash", group, OpenAIUsage{InputTokens: 1000, OutputTokens: 200, CacheReadInputTokens: 100})
	input.PricingAt = time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	input.APIKey.Quota = 20
	input.APIKey.RateLimit5h = 10
	input.APIKeyService = &openAIRecordUsageAPIKeyQuotaStub{}
	require.NoError(t, svc.RecordUsage(context.Background(), input))
	expectedTotal := 900*inputPrice + 200*outputPrice + 100*cachePrice
	expectedActual := expectedTotal * 2 * 70 / 60
	require.InDelta(t, expectedTotal, repo.lastLog.TotalCost, 1e-12)
	require.InDelta(t, expectedActual, repo.lastLog.ActualCost, 1e-12)
	require.Equal(t, 1, billingRepo.calls)
	require.Equal(t, 0.00310333, billingRepo.lastCmd.BalanceCost)
	require.Equal(t, 0.00310333, billingRepo.lastCmd.APIKeyQuotaCost)
	require.Equal(t, 0.00310333, billingRepo.lastCmd.APIKeyRateLimitCost)
	require.Zero(t, userRepo.deductCalls)
}

//go:build unit

package service

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/stretchr/testify/require"
)

func newGatewayRecordUsageServiceForTest(usageRepo UsageLogRepository, userRepo UserRepository, subRepo UserSubscriptionRepository) *GatewayService {
	cfg := &config.Config{}
	cfg.Default.RateMultiplier = 1.1
	return NewGatewayService(
		nil,
		nil,
		usageRepo,
		nil,
		userRepo,
		subRepo,
		nil,
		nil,
		cfg,
		nil,
		nil,
		NewBillingService(cfg, nil),
		nil,
		&BillingCacheService{},
		nil,
		nil,
		&DeferredService{},
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil, // userPlatformQuotaRepo
	)
}

func newGatewayRecordUsageServiceWithBillingRepoForTest(usageRepo UsageLogRepository, billingRepo UsageBillingRepository, userRepo UserRepository, subRepo UserSubscriptionRepository) *GatewayService {
	svc := newGatewayRecordUsageServiceForTest(usageRepo, userRepo, subRepo)
	svc.usageBillingRepo = billingRepo
	return svc
}

type openAIRecordUsageBestEffortLogRepoStub struct {
	UsageLogRepository

	bestEffortErr   error
	createErr       error
	bestEffortCalls int
	createCalls     int
	lastLog         *UsageLog
	lastCtxErr      error
}

func (s *openAIRecordUsageBestEffortLogRepoStub) CreateBestEffort(ctx context.Context, log *UsageLog) error {
	s.bestEffortCalls++
	s.lastLog = log
	s.lastCtxErr = ctx.Err()
	return s.bestEffortErr
}

func (s *openAIRecordUsageBestEffortLogRepoStub) Create(ctx context.Context, log *UsageLog) (bool, error) {
	s.createCalls++
	s.lastLog = log
	s.lastCtxErr = ctx.Err()
	return false, s.createErr
}

func TestGatewayServiceRecordUsage_BillingUsesDetachedContext(t *testing.T) {
	usageRepo := &openAIRecordUsageLogRepoStub{inserted: false, err: context.DeadlineExceeded}
	userRepo := &openAIRecordUsageUserRepoStub{}
	subRepo := &openAIRecordUsageSubRepoStub{}
	quotaSvc := &openAIRecordUsageAPIKeyQuotaStub{}
	svc := newGatewayRecordUsageServiceForTest(usageRepo, userRepo, subRepo)

	reqCtx, cancel := context.WithCancel(context.Background())
	cancel()

	err := svc.RecordUsage(reqCtx, &RecordUsageInput{
		Result: &ForwardResult{
			RequestID: "gateway_detached_ctx",
			Usage: ClaudeUsage{
				InputTokens:  10,
				OutputTokens: 6,
			},
			Model:    "claude-sonnet-4",
			Duration: time.Second,
		},
		APIKey: &APIKey{
			ID:    501,
			Quota: 100,
		},
		User:          &User{ID: 601},
		Account:       &Account{ID: 701},
		APIKeyService: quotaSvc,
	})

	require.NoError(t, err)
	require.Equal(t, 1, usageRepo.calls)
	require.Equal(t, 1, userRepo.deductCalls)
	require.NoError(t, userRepo.lastCtxErr)
	require.Equal(t, 1, quotaSvc.quotaCalls)
	require.NoError(t, quotaSvc.lastQuotaCtxErr)
}

func TestGatewayServiceRecordUsage_BillingFingerprintIncludesRequestPayloadHash(t *testing.T) {
	usageRepo := &openAIRecordUsageLogRepoStub{}
	billingRepo := &openAIRecordUsageBillingRepoStub{result: &UsageBillingApplyResult{Applied: true}}
	svc := newGatewayRecordUsageServiceWithBillingRepoForTest(usageRepo, billingRepo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{})

	payloadHash := HashUsageRequestPayload([]byte(`{"messages":[{"role":"user","content":"hello"}]}`))
	err := svc.RecordUsage(context.Background(), &RecordUsageInput{
		Result: &ForwardResult{
			RequestID: "gateway_payload_hash",
			Usage: ClaudeUsage{
				InputTokens:  10,
				OutputTokens: 6,
			},
			Model:    "claude-sonnet-4",
			Duration: time.Second,
		},
		APIKey:             &APIKey{ID: 501, Quota: 100},
		User:               &User{ID: 601},
		Account:            &Account{ID: 701},
		RequestPayloadHash: payloadHash,
	})
	require.NoError(t, err)
	require.NotNil(t, billingRepo.lastCmd)
	require.Equal(t, payloadHash, billingRepo.lastCmd.RequestPayloadHash)
}

func TestGatewayServiceRecordUsage_BillingFingerprintFallsBackToContextRequestID(t *testing.T) {
	usageRepo := &openAIRecordUsageLogRepoStub{}
	billingRepo := &openAIRecordUsageBillingRepoStub{result: &UsageBillingApplyResult{Applied: true}}
	svc := newGatewayRecordUsageServiceWithBillingRepoForTest(usageRepo, billingRepo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{})

	ctx := context.WithValue(context.Background(), ctxkey.RequestID, "req-local-123")
	err := svc.RecordUsage(ctx, &RecordUsageInput{
		Result: &ForwardResult{
			RequestID: "gateway_payload_fallback",
			Usage: ClaudeUsage{
				InputTokens:  10,
				OutputTokens: 6,
			},
			Model:    "claude-sonnet-4",
			Duration: time.Second,
		},
		APIKey:  &APIKey{ID: 501, Quota: 100},
		User:    &User{ID: 601},
		Account: &Account{ID: 701},
	})
	require.NoError(t, err)
	require.NotNil(t, billingRepo.lastCmd)
	require.Equal(t, "local:req-local-123", billingRepo.lastCmd.RequestPayloadHash)
}

func TestGatewayServiceRecordUsage_PreservesRequestedAndUpstreamModels(t *testing.T) {
	usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
	svc := newGatewayRecordUsageServiceForTest(usageRepo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{})
	mappedModel := "claude-sonnet-4-20250514"

	err := svc.RecordUsage(context.Background(), &RecordUsageInput{
		Result: &ForwardResult{
			RequestID:     "gateway_models_split",
			Usage:         ClaudeUsage{InputTokens: 10, OutputTokens: 6},
			Model:         "claude-sonnet-4",
			UpstreamModel: mappedModel,
			Duration:      time.Second,
		},
		APIKey:  &APIKey{ID: 501, Quota: 100},
		User:    &User{ID: 601},
		Account: &Account{ID: 701},
	})

	require.NoError(t, err)
	require.NotNil(t, usageRepo.lastLog)
	require.Equal(t, "claude-sonnet-4", usageRepo.lastLog.Model)
	require.Equal(t, "claude-sonnet-4", usageRepo.lastLog.RequestedModel)
	require.NotNil(t, usageRepo.lastLog.UpstreamModel)
	require.Equal(t, mappedModel, *usageRepo.lastLog.UpstreamModel)
}

func TestGatewayServiceRecordUsage_AntigravityDefaultsBillingToUpstreamModel(t *testing.T) {
	usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
	userRepo := &openAIRecordUsageUserRepoStub{}
	svc := newGatewayRecordUsageServiceForTest(usageRepo, userRepo, &openAIRecordUsageSubRepoStub{})
	tokens := UsageTokens{InputTokens: 20, OutputTokens: 10}
	expectedCost, err := svc.billingService.CalculateCost("claude-sonnet-4", tokens, 1.1)
	require.NoError(t, err)

	err = svc.RecordUsage(context.Background(), &RecordUsageInput{
		Result: &ForwardResult{
			RequestID:     "gateway_antigravity_upstream_default",
			Usage:         ClaudeUsage{InputTokens: tokens.InputTokens, OutputTokens: tokens.OutputTokens},
			Model:         "not-priceable-antigravity-alias",
			UpstreamModel: "claude-sonnet-4",
			Duration:      time.Second,
		},
		APIKey:  &APIKey{ID: 501, Quota: 100},
		User:    &User{ID: 601},
		Account: &Account{ID: 701, Platform: PlatformAntigravity},
	})

	require.NoError(t, err)
	require.NotNil(t, usageRepo.lastLog)
	require.Equal(t, "not-priceable-antigravity-alias", usageRepo.lastLog.Model)
	require.Equal(t, "not-priceable-antigravity-alias", usageRepo.lastLog.RequestedModel)
	require.NotNil(t, usageRepo.lastLog.UpstreamModel)
	require.Equal(t, "claude-sonnet-4", *usageRepo.lastLog.UpstreamModel)
	require.InDelta(t, expectedCost.TotalCost, usageRepo.lastLog.TotalCost, 1e-12)
	require.InDelta(t, expectedCost.ActualCost, usageRepo.lastLog.ActualCost, 1e-12)
	require.InDelta(t, expectedCost.ActualCost, userRepo.lastAmount, 1e-12)
}

func TestGatewayServiceRecordUsage_BillingModelSourceOverrides(t *testing.T) {
	tests := []struct {
		name        string
		fields      ChannelUsageFields
		wantModel   string
		wantCost    func(*GatewayService, UsageTokens) *CostBreakdown
		requestID   string
		expectNoErr bool
	}{
		{
			name: "requested source uses original model",
			fields: ChannelUsageFields{
				OriginalModel:      "claude-sonnet-4",
				ChannelMappedModel: "claude-opus-4.6",
				BillingModelSource: BillingModelSourceRequested,
			},
			wantModel: "claude-sonnet-4",
			requestID: "gateway_billing_source_requested",
		},
		{
			name: "actual channel mapping uses mapped model",
			fields: ChannelUsageFields{
				OriginalModel:      "not-priceable-antigravity-alias",
				ChannelMappedModel: "claude-sonnet-4",
				BillingModelSource: BillingModelSourceChannelMapped,
			},
			wantModel: "claude-sonnet-4",
			requestID: "gateway_billing_source_channel_mapped",
		},
		{
			name: "unmapped channel source falls back to upstream",
			fields: ChannelUsageFields{
				OriginalModel:      "not-priceable-antigravity-alias",
				ChannelMappedModel: "not-priceable-antigravity-alias",
				BillingModelSource: BillingModelSourceChannelMapped,
			},
			wantModel: "claude-opus-4.6",
			requestID: "gateway_billing_source_channel_unmapped",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
			userRepo := &openAIRecordUsageUserRepoStub{}
			svc := newGatewayRecordUsageServiceForTest(usageRepo, userRepo, &openAIRecordUsageSubRepoStub{})
			tokens := UsageTokens{InputTokens: 20, OutputTokens: 10}
			expectedCost, err := svc.billingService.CalculateCost(tt.wantModel, tokens, 1.1)
			require.NoError(t, err)

			err = svc.RecordUsage(context.Background(), &RecordUsageInput{
				Result: &ForwardResult{
					RequestID:     tt.requestID,
					Usage:         ClaudeUsage{InputTokens: tokens.InputTokens, OutputTokens: tokens.OutputTokens},
					Model:         "not-priceable-antigravity-alias",
					UpstreamModel: "claude-opus-4.6",
					Duration:      time.Second,
				},
				APIKey:             &APIKey{ID: 501, Quota: 100},
				User:               &User{ID: 601},
				Account:            &Account{ID: 701, Platform: PlatformAntigravity},
				ChannelUsageFields: tt.fields,
			})

			require.NoError(t, err)
			require.NotNil(t, usageRepo.lastLog)
			require.InDelta(t, expectedCost.TotalCost, usageRepo.lastLog.TotalCost, 1e-12)
			require.InDelta(t, expectedCost.ActualCost, usageRepo.lastLog.ActualCost, 1e-12)
			require.InDelta(t, expectedCost.ActualCost, userRepo.lastAmount, 1e-12)
		})
	}
}

func TestGatewayServiceRecordUsage_BillingModelCandidatesFallBackToPriceableUpstream(t *testing.T) {
	usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
	userRepo := &openAIRecordUsageUserRepoStub{}
	svc := newGatewayRecordUsageServiceForTest(usageRepo, userRepo, &openAIRecordUsageSubRepoStub{})
	tokens := UsageTokens{InputTokens: 20, OutputTokens: 10}
	expectedCost, err := svc.billingService.CalculateCost("claude-sonnet-4", tokens, 1.1)
	require.NoError(t, err)

	err = svc.RecordUsage(context.Background(), &RecordUsageInput{
		Result: &ForwardResult{
			RequestID:     "gateway_billing_candidate_upstream",
			Usage:         ClaudeUsage{InputTokens: tokens.InputTokens, OutputTokens: tokens.OutputTokens},
			Model:         "not-priceable-antigravity-alias",
			UpstreamModel: "claude-sonnet-4",
			Duration:      time.Second,
		},
		APIKey:  &APIKey{ID: 501, Quota: 100},
		User:    &User{ID: 601},
		Account: &Account{ID: 701, Platform: PlatformAntigravity},
		ChannelUsageFields: ChannelUsageFields{
			OriginalModel:      "not-priceable-antigravity-alias",
			ChannelMappedModel: "not-priceable-antigravity-alias",
			BillingModelSource: BillingModelSourceRequested,
		},
	})

	require.NoError(t, err)
	require.NotNil(t, usageRepo.lastLog)
	require.InDelta(t, expectedCost.TotalCost, usageRepo.lastLog.TotalCost, 1e-12)
	require.InDelta(t, expectedCost.ActualCost, usageRepo.lastLog.ActualCost, 1e-12)
	require.InDelta(t, expectedCost.ActualCost, userRepo.lastAmount, 1e-12)
	require.True(t, usageRepo.lastLog.ActualCost > 0, "cost must not be zero")
}

func TestGatewayServiceRecordUsage_RejectsUnpricedBillingModels(t *testing.T) {
	usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
	userRepo := &openAIRecordUsageUserRepoStub{}
	svc := newGatewayRecordUsageServiceForTest(usageRepo, userRepo, &openAIRecordUsageSubRepoStub{})

	err := svc.RecordUsage(context.Background(), &RecordUsageInput{
		Result: &ForwardResult{
			RequestID: "gateway_unpriced_model",
			Usage: ClaudeUsage{
				InputTokens:  20,
				OutputTokens: 10,
			},
			Model:    "opencode-unpriced-model",
			Duration: time.Second,
		},
		APIKey:  &APIKey{ID: 501, Quota: 100},
		User:    &User{ID: 601},
		Account: &Account{ID: 701, Platform: PlatformOpenCodeGo},
	})

	require.ErrorIs(t, err, ErrModelPricingUnavailable)
	require.Equal(t, 0, usageRepo.calls)
	require.Equal(t, 0, userRepo.deductCalls)
}

func TestGatewayServiceValidateGatewayTokenPricingAvailable_RejectsUnpricedOpenCodeGoModel(t *testing.T) {
	svc := newGatewayRecordUsageServiceForTest(&openAIRecordUsageLogRepoStub{}, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{})
	apiKey := &APIKey{ID: 501, Quota: 100}
	account := &Account{ID: 701, Platform: PlatformOpenCodeGo}

	err := svc.ValidateGatewayTokenPricingAvailable(context.Background(), apiKey, account, "opencode-unpriced-model", ChannelMappingResult{
		MappedModel:        "opencode-unpriced-model",
		BillingModelSource: BillingModelSourceChannelMapped,
	})

	require.ErrorIs(t, err, ErrModelPricingUnavailable)
}

func TestGatewayServiceValidateGatewayTokenPricingAvailable_AllowsPriceableModel(t *testing.T) {
	svc := newGatewayRecordUsageServiceForTest(&openAIRecordUsageLogRepoStub{}, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{})
	apiKey := &APIKey{ID: 501, Quota: 100}
	account := &Account{ID: 701, Platform: PlatformOpenCodeGo}

	err := svc.ValidateGatewayTokenPricingAvailable(context.Background(), apiKey, account, "deepseek-v4-pro", ChannelMappingResult{
		MappedModel:        "deepseek-v4-pro",
		BillingModelSource: BillingModelSourceChannelMapped,
	})

	require.NoError(t, err)
}

func TestGatewayServiceValidateGatewayTokenPricingAvailable_AllowsOpenCodeGoModelsDevPricing(t *testing.T) {
	pricingSvc := &PricingService{
		pricingData: map[string]*LiteLLMModelPricing{
			"kimi-k2.5": {
				Mode:               "chat",
				InputCostPerToken:  0.60e-6,
				OutputCostPerToken: 3.00e-6,
				LiteLLMProvider:    PlatformOpenCodeGo,
			},
		},
	}
	svc := newGatewayRecordUsageServiceForTest(&openAIRecordUsageLogRepoStub{}, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{})
	svc.billingService = NewBillingService(svc.cfg, pricingSvc)
	apiKey := &APIKey{ID: 501, Quota: 100}
	account := &Account{ID: 701, Platform: PlatformOpenCodeGo}

	err := svc.ValidateGatewayTokenPricingAvailable(context.Background(), apiKey, account, "kimi-k2.5", ChannelMappingResult{
		MappedModel:        "kimi-k2.5",
		BillingModelSource: BillingModelSourceChannelMapped,
	})

	require.NoError(t, err)
}

func TestGatewayServiceRecordUsage_OpenCodeGoPricingPageMatchesChannelBillingPromotion(t *testing.T) {
	groupID := int64(10)
	groupMultiplier := 1.25
	inputPrice := 0.4e-6
	outputPrice := 0.8e-6
	cacheReadPrice := 0.04e-6
	pricingSvc := &PricingService{
		pricingData: map[string]*LiteLLMModelPricing{},
		openCodeGoPromotions: map[string]openCodeGoUsagePromotion{
			openCodeGoDeepSeekFlashPromoModel: {multiplier: 0.5, confirmedAt: time.Now()},
		},
	}
	billingSvc := NewBillingService(&config.Config{}, pricingSvc)
	channelSvc := &ChannelService{pricingService: pricingSvc, billingService: billingSvc}
	cache := newEmptyChannelCache()
	cache.channelByGroupID[groupID] = &Channel{ID: 1, Status: StatusActive}
	cache.groupPlatform[groupID] = PlatformOpenCodeGo
	cache.pricingByGroupModel[channelModelKey{
		groupID:  groupID,
		platform: PlatformOpenCodeGo,
		model:    openCodeGoDeepSeekFlashPromoModel,
	}] = &ChannelModelPricing{
		Platform:       PlatformOpenCodeGo,
		Models:         []string{openCodeGoDeepSeekFlashPromoModel},
		BillingMode:    BillingModeToken,
		InputPrice:     &inputPrice,
		OutputPrice:    &outputPrice,
		CacheReadPrice: &cacheReadPrice,
	}
	cache.loadedAt = time.Now()
	channelSvc.cache.Store(cache)

	displayed := channelSvc.BuildSupportedModelForPricingGroup(
		context.Background(),
		groupID,
		openCodeGoDeepSeekFlashPromoModel,
		PlatformOpenCodeGo,
		nil,
	)
	require.Equal(t, PricingSourceChannel, displayed.PricingSource)
	require.NotNil(t, displayed.Pricing)
	require.NotNil(t, displayed.Promotion)

	usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
	userRepo := &openAIRecordUsageUserRepoStub{}
	svc := newGatewayRecordUsageServiceForTest(usageRepo, userRepo, &openAIRecordUsageSubRepoStub{})
	svc.billingService = billingSvc
	svc.channelService = channelSvc
	svc.resolver = NewModelPricingResolver(channelSvc, billingSvc)
	tokens := UsageTokens{InputTokens: 1_000_000, OutputTokens: 500_000, CacheReadTokens: 250_000}

	err := svc.RecordUsage(context.Background(), &RecordUsageInput{
		Result: &ForwardResult{
			RequestID: "opencode_go_pricing_page_billing_match",
			Usage: ClaudeUsage{
				InputTokens:          tokens.InputTokens,
				OutputTokens:         tokens.OutputTokens,
				CacheReadInputTokens: tokens.CacheReadTokens,
			},
			Model:    openCodeGoDeepSeekFlashPromoModel,
			Duration: time.Second,
		},
		APIKey: &APIKey{
			ID:      501,
			Quota:   100,
			GroupID: &groupID,
			Group: &Group{
				ID:             groupID,
				Platform:       PlatformOpenCodeGo,
				RateMultiplier: groupMultiplier,
			},
		},
		User:    &User{ID: 601},
		Account: &Account{ID: 701, Platform: PlatformOpenCodeGo},
	})
	require.NoError(t, err)
	require.NotNil(t, usageRepo.lastLog)

	standardTotal := float64(tokens.InputTokens)*(*displayed.Pricing.InputPrice) +
		float64(tokens.OutputTokens)*(*displayed.Pricing.OutputPrice) +
		float64(tokens.CacheReadTokens)*(*displayed.Pricing.CacheReadPrice)
	expectedTotal := standardTotal * displayed.Promotion.CostMultiplier
	expectedActual := expectedTotal * groupMultiplier
	require.InDelta(t, expectedTotal, usageRepo.lastLog.TotalCost, 1e-12)
	require.InDelta(t, expectedActual, usageRepo.lastLog.ActualCost, 1e-12)
	require.InDelta(t, expectedActual, userRepo.lastAmount, 1e-12)
}

func TestGatewayServiceRecordUsage_OpenCodeGoDeepSeekFlashPromotionUsesAtomicBillingCosts(t *testing.T) {
	pricingSvc := &PricingService{
		pricingData: map[string]*LiteLLMModelPricing{},
		openCodeGoPromotions: map[string]openCodeGoUsagePromotion{
			openCodeGoDeepSeekFlashPromoModel: {multiplier: 0.5, confirmedAt: time.Now()},
		},
	}
	usageRepo := &openAIRecordUsageLogRepoStub{}
	billingRepo := &openAIRecordUsageBillingRepoStub{result: &UsageBillingApplyResult{Applied: true}}
	svc := newGatewayRecordUsageServiceWithBillingRepoForTest(usageRepo, billingRepo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{})
	svc.billingService = NewBillingService(svc.cfg, pricingSvc)
	svc.channelService = newTestChannelServiceForStats(t, &Channel{ID: 1, Status: StatusActive}, 10, PlatformOpenCodeGo)

	tokens := UsageTokens{InputTokens: 1_000_000, OutputTokens: 1_000_000, CacheReadTokens: 1_000_000}
	standardBilling := NewBillingService(&config.Config{}, nil)
	standardCost, err := standardBilling.CalculateCost(openCodeGoDeepSeekFlashPromoModel, tokens, 1.1)
	require.NoError(t, err)
	accountRateMultiplier := 1.25
	groupID := int64(10)
	quotaSvc := &openAIRecordUsageAPIKeyQuotaStub{}
	err = svc.RecordUsage(context.Background(), &RecordUsageInput{
		Result: &ForwardResult{
			RequestID: "opencode_go_deepseek_flash_promo_atomic",
			Usage: ClaudeUsage{
				InputTokens:          tokens.InputTokens,
				OutputTokens:         tokens.OutputTokens,
				CacheReadInputTokens: tokens.CacheReadTokens,
			},
			Model:    openCodeGoDeepSeekFlashPromoModel,
			Duration: time.Second,
		},
		APIKey: &APIKey{
			ID:          501,
			Quota:       100,
			RateLimit5h: 100,
			GroupID:     &groupID,
			Group:       &Group{ID: 10, Platform: PlatformOpenCodeGo, RateMultiplier: 1.1},
		},
		User: &User{ID: 601},
		Account: &Account{
			ID:             701,
			Platform:       PlatformOpenCodeGo,
			Type:           AccountTypeAPIKey,
			RateMultiplier: &accountRateMultiplier,
			Extra:          map[string]any{"quota_limit": 100.0},
		},
		APIKeyService: quotaSvc,
	})
	require.NoError(t, err)
	require.NotNil(t, billingRepo.lastCmd)
	require.InDelta(t, standardCost.ActualCost*0.5, billingRepo.lastCmd.BalanceCost, 1e-12)
	require.InDelta(t, standardCost.ActualCost*0.5, billingRepo.lastCmd.APIKeyQuotaCost, 1e-12)
	require.InDelta(t, standardCost.ActualCost*0.5, billingRepo.lastCmd.APIKeyRateLimitCost, 1e-12)
	require.InDelta(t, standardCost.TotalCost*0.5*accountRateMultiplier, billingRepo.lastCmd.AccountQuotaCost, 1e-12)

	subscriptionUsageRepo := &openAIRecordUsageLogRepoStub{}
	subscriptionBillingRepo := &openAIRecordUsageBillingRepoStub{result: &UsageBillingApplyResult{Applied: true}}
	subscriptionSvc := newGatewayRecordUsageServiceWithBillingRepoForTest(subscriptionUsageRepo, subscriptionBillingRepo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{})
	subscriptionSvc.billingService = NewBillingService(subscriptionSvc.cfg, pricingSvc)
	subscriptionSvc.channelService = newTestChannelServiceForStats(t, &Channel{ID: 1, Status: StatusActive}, 10, PlatformOpenCodeGo)
	err = subscriptionSvc.RecordUsage(context.Background(), &RecordUsageInput{
		Result: &ForwardResult{
			RequestID: "opencode_go_deepseek_flash_promo_subscription_atomic",
			Usage: ClaudeUsage{
				InputTokens:          tokens.InputTokens,
				OutputTokens:         tokens.OutputTokens,
				CacheReadInputTokens: tokens.CacheReadTokens,
			},
			Model:    openCodeGoDeepSeekFlashPromoModel,
			Duration: time.Second,
		},
		APIKey: &APIKey{
			ID:      502,
			GroupID: &groupID,
			Group: &Group{
				ID:               groupID,
				Platform:         PlatformOpenCodeGo,
				RateMultiplier:   1.1,
				SubscriptionType: SubscriptionTypeSubscription,
			},
		},
		User:         &User{ID: 602},
		Account:      &Account{ID: 702, Platform: PlatformOpenCodeGo},
		Subscription: &UserSubscription{ID: 802},
	})
	require.NoError(t, err)
	require.NotNil(t, subscriptionBillingRepo.lastCmd)
	require.Zero(t, subscriptionBillingRepo.lastCmd.BalanceCost)
	require.InDelta(t, standardCost.ActualCost*0.5, subscriptionBillingRepo.lastCmd.SubscriptionCost, 1e-12)
}

func TestApplyCostBreakdownMultiplierScalesEveryCostBucket(t *testing.T) {
	cost := &CostBreakdown{
		InputCost:         1,
		OutputCost:        2,
		ImageOutputCost:   3,
		CacheCreationCost: 4,
		CacheReadCost:     5,
		TotalCost:         15,
		ActualCost:        16.5,
		BillingMode:       string(BillingModeToken),
	}

	applyCostBreakdownMultiplier(cost, 0.5)

	require.Equal(t, 0.5, cost.InputCost)
	require.Equal(t, 1.0, cost.OutputCost)
	require.Equal(t, 1.5, cost.ImageOutputCost)
	require.Equal(t, 2.0, cost.CacheCreationCost)
	require.Equal(t, 2.5, cost.CacheReadCost)
	require.Equal(t, 7.5, cost.TotalCost)
	require.Equal(t, 8.25, cost.ActualCost)
	require.Equal(t, string(BillingModeToken), cost.BillingMode)

	unchanged := *cost
	applyCostBreakdownMultiplier(cost, math.NaN())
	require.Equal(t, unchanged, *cost)
	applyCostBreakdownMultiplier(cost, 1.5)
	require.Equal(t, unchanged, *cost)
}

func TestGatewayServiceRecordUsage_OpenCodeGoDeepSeekFlashPromotionHalvesUsageCosts(t *testing.T) {
	now := time.Now()
	pricingSvc := &PricingService{
		pricingData: map[string]*LiteLLMModelPricing{},
		openCodeGoPromotions: map[string]openCodeGoUsagePromotion{
			openCodeGoDeepSeekFlashPromoModel: {multiplier: 0.5, confirmedAt: now},
		},
	}

	newService := func(platform string) (*GatewayService, *openAIRecordUsageLogRepoStub, *openAIRecordUsageUserRepoStub) {
		usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
		userRepo := &openAIRecordUsageUserRepoStub{}
		svc := newGatewayRecordUsageServiceForTest(usageRepo, userRepo, &openAIRecordUsageSubRepoStub{})
		svc.billingService = NewBillingService(svc.cfg, pricingSvc)
		svc.channelService = newTestChannelServiceForStats(t, &Channel{ID: 1, Status: StatusActive}, 10, platform)
		return svc, usageRepo, userRepo
	}

	tokens := UsageTokens{InputTokens: 1_000_000, OutputTokens: 1_000_000, CacheReadTokens: 1_000_000}
	standardBilling := NewBillingService(&config.Config{}, nil)
	standardCost, err := standardBilling.CalculateCost(openCodeGoDeepSeekFlashPromoModel, tokens, 1.1)
	require.NoError(t, err)

	groupID := int64(10)
	svc, usageRepo, userRepo := newService(PlatformOpenCodeGo)
	err = svc.RecordUsage(context.Background(), &RecordUsageInput{
		Result: &ForwardResult{
			RequestID: "opencode_go_deepseek_flash_promo",
			Usage: ClaudeUsage{
				InputTokens:          tokens.InputTokens,
				OutputTokens:         tokens.OutputTokens,
				CacheReadInputTokens: tokens.CacheReadTokens,
			},
			Model:    openCodeGoDeepSeekFlashPromoModel,
			Duration: time.Second,
		},
		APIKey: &APIKey{ID: 501, Quota: 100, GroupID: &groupID, Group: &Group{ID: 10, Platform: PlatformOpenCodeGo, RateMultiplier: 1.1}},
		User:   &User{ID: 601},
		Account: &Account{
			ID:       701,
			Platform: PlatformOpenCodeGo,
		},
	})
	require.NoError(t, err)
	require.NotNil(t, usageRepo.lastLog)
	require.InDelta(t, standardCost.InputCost*0.5, usageRepo.lastLog.InputCost, 1e-12)
	require.InDelta(t, standardCost.OutputCost*0.5, usageRepo.lastLog.OutputCost, 1e-12)
	require.InDelta(t, standardCost.CacheReadCost*0.5, usageRepo.lastLog.CacheReadCost, 1e-12)
	require.InDelta(t, standardCost.TotalCost*0.5, usageRepo.lastLog.TotalCost, 1e-12)
	require.InDelta(t, standardCost.ActualCost*0.5, usageRepo.lastLog.ActualCost, 1e-12)
	require.InDelta(t, standardCost.ActualCost*0.5, userRepo.lastAmount, 1e-12)
	require.NotNil(t, usageRepo.lastLog.AccountStatsCost)
	require.InDelta(t, standardCost.TotalCost*0.5, *usageRepo.lastLog.AccountStatsCost, 1e-12)

	pricingSvc.replaceOpenCodeGoPromotions(nil, time.Now())
	endedUsageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
	endedUserRepo := &openAIRecordUsageUserRepoStub{}
	svc.usageLogRepo = endedUsageRepo
	svc.userRepo = endedUserRepo
	err = svc.RecordUsage(context.Background(), &RecordUsageInput{
		Result: &ForwardResult{
			RequestID: "opencode_go_deepseek_flash_promo_ended",
			Usage: ClaudeUsage{
				InputTokens:          tokens.InputTokens,
				OutputTokens:         tokens.OutputTokens,
				CacheReadInputTokens: tokens.CacheReadTokens,
			},
			Model:    openCodeGoDeepSeekFlashPromoModel,
			Duration: time.Second,
		},
		APIKey: &APIKey{ID: 503, Quota: 100, GroupID: &groupID, Group: &Group{ID: 10, Platform: PlatformOpenCodeGo, RateMultiplier: 1.1}},
		User:   &User{ID: 603},
		Account: &Account{
			ID:       703,
			Platform: PlatformOpenCodeGo,
		},
	})
	require.NoError(t, err)
	require.NotNil(t, endedUsageRepo.lastLog)
	require.InDelta(t, standardCost.TotalCost, endedUsageRepo.lastLog.TotalCost, 1e-12)
	require.InDelta(t, standardCost.ActualCost, endedUsageRepo.lastLog.ActualCost, 1e-12)
	require.InDelta(t, standardCost.ActualCost, endedUserRepo.lastAmount, 1e-12)

	pricingSvc.replaceOpenCodeGoPromotions(map[string]float64{openCodeGoDeepSeekFlashPromoModel: 0.5}, time.Now())
	otherSvc, otherUsageRepo, otherUserRepo := newService(PlatformAnthropic)
	err = otherSvc.RecordUsage(context.Background(), &RecordUsageInput{
		Result: &ForwardResult{
			RequestID: "anthropic_deepseek_flash_no_promo",
			Usage: ClaudeUsage{
				InputTokens:          tokens.InputTokens,
				OutputTokens:         tokens.OutputTokens,
				CacheReadInputTokens: tokens.CacheReadTokens,
			},
			Model:    openCodeGoDeepSeekFlashPromoModel,
			Duration: time.Second,
		},
		APIKey: &APIKey{ID: 502, Quota: 100, GroupID: &groupID, Group: &Group{ID: 10, Platform: PlatformAnthropic, RateMultiplier: 1.1}},
		User:   &User{ID: 602},
		Account: &Account{
			ID:       702,
			Platform: PlatformAnthropic,
		},
	})
	require.NoError(t, err)
	require.NotNil(t, otherUsageRepo.lastLog)
	require.InDelta(t, standardCost.TotalCost, otherUsageRepo.lastLog.TotalCost, 1e-12)
	require.InDelta(t, standardCost.ActualCost, otherUsageRepo.lastLog.ActualCost, 1e-12)
	require.InDelta(t, standardCost.ActualCost, otherUserRepo.lastAmount, 1e-12)
}

func TestGatewayServiceRecordUsage_OpenCodeGoHy3PromotionEightTimesUsageCosts(t *testing.T) {
	now := time.Now()
	pricingSvc := &PricingService{
		pricingData: map[string]*LiteLLMModelPricing{},
		openCodeGoPromotions: map[string]openCodeGoUsagePromotion{
			openCodeGoHy3PromoModel: {multiplier: 0.125, confirmedAt: now},
		},
	}

	newService := func(platform string) (*GatewayService, *openAIRecordUsageLogRepoStub, *openAIRecordUsageUserRepoStub) {
		usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
		userRepo := &openAIRecordUsageUserRepoStub{}
		svc := newGatewayRecordUsageServiceForTest(usageRepo, userRepo, &openAIRecordUsageSubRepoStub{})
		svc.billingService = NewBillingService(svc.cfg, pricingSvc)
		svc.channelService = newTestChannelServiceForStats(t, &Channel{ID: 1, Status: StatusActive}, 10, platform)
		return svc, usageRepo, userRepo
	}

	tokens := UsageTokens{InputTokens: 1_000_000, OutputTokens: 1_000_000, CacheReadTokens: 1_000_000}
	standardBilling := NewBillingService(&config.Config{}, nil)
	standardCost, err := standardBilling.CalculateCostForPlatform(PlatformOpenCodeGo, openCodeGoHy3PromoModel, tokens, 1.1)
	require.NoError(t, err)

	groupID := int64(10)
	svc, usageRepo, userRepo := newService(PlatformOpenCodeGo)
	err = svc.RecordUsage(context.Background(), &RecordUsageInput{
		Result: &ForwardResult{
			RequestID: "opencode_go_hy3_promo",
			Usage: ClaudeUsage{
				InputTokens:          tokens.InputTokens,
				OutputTokens:         tokens.OutputTokens,
				CacheReadInputTokens: tokens.CacheReadTokens,
			},
			Model:    openCodeGoHy3PromoModel,
			Duration: time.Second,
		},
		APIKey: &APIKey{ID: 501, Quota: 100, GroupID: &groupID, Group: &Group{ID: 10, Platform: PlatformOpenCodeGo, RateMultiplier: 1.1}},
		User:   &User{ID: 601},
		Account: &Account{
			ID:       701,
			Platform: PlatformOpenCodeGo,
		},
	})
	require.NoError(t, err)
	require.NotNil(t, usageRepo.lastLog)
	require.InDelta(t, standardCost.InputCost*0.125, usageRepo.lastLog.InputCost, 1e-12)
	require.InDelta(t, standardCost.OutputCost*0.125, usageRepo.lastLog.OutputCost, 1e-12)
	require.InDelta(t, standardCost.CacheReadCost*0.125, usageRepo.lastLog.CacheReadCost, 1e-12)
	require.InDelta(t, standardCost.TotalCost*0.125, usageRepo.lastLog.TotalCost, 1e-12)
	require.InDelta(t, standardCost.ActualCost*0.125, usageRepo.lastLog.ActualCost, 1e-12)
	require.InDelta(t, standardCost.ActualCost*0.125, userRepo.lastAmount, 1e-12)
	require.NotNil(t, usageRepo.lastLog.AccountStatsCost)
	require.InDelta(t, standardCost.TotalCost*0.125, *usageRepo.lastLog.AccountStatsCost, 1e-12)

	// 活动结束/文档未获取到时，自动恢复标准计费
	pricingSvc.replaceOpenCodeGoPromotions(nil, time.Now())
	endedUsageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
	endedUserRepo := &openAIRecordUsageUserRepoStub{}
	svc.usageLogRepo = endedUsageRepo
	svc.userRepo = endedUserRepo
	err = svc.RecordUsage(context.Background(), &RecordUsageInput{
		Result: &ForwardResult{
			RequestID: "opencode_go_hy3_promo_ended",
			Usage: ClaudeUsage{
				InputTokens:          tokens.InputTokens,
				OutputTokens:         tokens.OutputTokens,
				CacheReadInputTokens: tokens.CacheReadTokens,
			},
			Model:    openCodeGoHy3PromoModel,
			Duration: time.Second,
		},
		APIKey: &APIKey{ID: 503, Quota: 100, GroupID: &groupID, Group: &Group{ID: 10, Platform: PlatformOpenCodeGo, RateMultiplier: 1.1}},
		User:   &User{ID: 603},
		Account: &Account{
			ID:       703,
			Platform: PlatformOpenCodeGo,
		},
	})
	require.NoError(t, err)
	require.NotNil(t, endedUsageRepo.lastLog)
	require.InDelta(t, standardCost.TotalCost, endedUsageRepo.lastLog.TotalCost, 1e-12)
	require.InDelta(t, standardCost.ActualCost, endedUsageRepo.lastLog.ActualCost, 1e-12)
	require.InDelta(t, standardCost.ActualCost, endedUserRepo.lastAmount, 1e-12)
}

func TestGatewayServiceRecordUsage_AntigravityCacheReadWriteCostsPersisted(t *testing.T) {
	usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
	userRepo := &openAIRecordUsageUserRepoStub{}
	svc := newGatewayRecordUsageServiceForTest(usageRepo, userRepo, &openAIRecordUsageSubRepoStub{})
	tokens := UsageTokens{
		InputTokens:         20,
		OutputTokens:        10,
		CacheCreationTokens: 7,
		CacheReadTokens:     13,
	}
	expectedCost, err := svc.billingService.CalculateCost("claude-sonnet-4", tokens, 1.1)
	require.NoError(t, err)

	err = svc.RecordUsage(context.Background(), &RecordUsageInput{
		Result: &ForwardResult{
			RequestID: "gateway_antigravity_cache_cost",
			Usage: ClaudeUsage{
				InputTokens:              tokens.InputTokens,
				OutputTokens:             tokens.OutputTokens,
				CacheCreationInputTokens: tokens.CacheCreationTokens,
				CacheReadInputTokens:     tokens.CacheReadTokens,
			},
			Model:         "not-priceable-antigravity-alias",
			UpstreamModel: "claude-sonnet-4",
			Duration:      time.Second,
		},
		APIKey:  &APIKey{ID: 501, Quota: 100},
		User:    &User{ID: 601},
		Account: &Account{ID: 701, Platform: PlatformAntigravity},
	})

	require.NoError(t, err)
	require.NotNil(t, usageRepo.lastLog)
	require.Equal(t, tokens.CacheCreationTokens, usageRepo.lastLog.CacheCreationTokens)
	require.Equal(t, tokens.CacheReadTokens, usageRepo.lastLog.CacheReadTokens)
	require.InDelta(t, expectedCost.CacheCreationCost, usageRepo.lastLog.CacheCreationCost, 1e-12)
	require.InDelta(t, expectedCost.CacheReadCost, usageRepo.lastLog.CacheReadCost, 1e-12)
	require.InDelta(t, expectedCost.TotalCost, usageRepo.lastLog.TotalCost, 1e-12)
	require.InDelta(t, expectedCost.ActualCost, userRepo.lastAmount, 1e-12)
}

func TestGatewayServiceRecordUsageWithLongContext_AntigravityCacheReadWriteCostsPersisted(t *testing.T) {
	usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
	userRepo := &openAIRecordUsageUserRepoStub{}
	svc := newGatewayRecordUsageServiceForTest(usageRepo, userRepo, &openAIRecordUsageSubRepoStub{})
	tokens := UsageTokens{
		InputTokens:         20,
		OutputTokens:        10,
		CacheCreationTokens: 7,
		CacheReadTokens:     13,
	}
	expectedCost, err := svc.billingService.CalculateCostWithLongContext("claude-sonnet-4", tokens, 1.1, 10, 2)
	require.NoError(t, err)

	err = svc.RecordUsageWithLongContext(context.Background(), &RecordUsageLongContextInput{
		Result: &ForwardResult{
			RequestID: "gateway_antigravity_cache_long_context_cost",
			Usage: ClaudeUsage{
				InputTokens:              tokens.InputTokens,
				OutputTokens:             tokens.OutputTokens,
				CacheCreationInputTokens: tokens.CacheCreationTokens,
				CacheReadInputTokens:     tokens.CacheReadTokens,
			},
			Model:         "not-priceable-antigravity-alias",
			UpstreamModel: "claude-sonnet-4",
			Duration:      time.Second,
		},
		APIKey:                &APIKey{ID: 501, Quota: 100},
		User:                  &User{ID: 601},
		Account:               &Account{ID: 701, Platform: PlatformAntigravity},
		LongContextThreshold:  10,
		LongContextMultiplier: 2,
	})

	require.NoError(t, err)
	require.NotNil(t, usageRepo.lastLog)
	require.Equal(t, tokens.CacheCreationTokens, usageRepo.lastLog.CacheCreationTokens)
	require.Equal(t, tokens.CacheReadTokens, usageRepo.lastLog.CacheReadTokens)
	require.InDelta(t, expectedCost.CacheCreationCost, usageRepo.lastLog.CacheCreationCost, 1e-12)
	require.InDelta(t, expectedCost.CacheReadCost, usageRepo.lastLog.CacheReadCost, 1e-12)
	require.InDelta(t, expectedCost.TotalCost, usageRepo.lastLog.TotalCost, 1e-12)
	require.InDelta(t, expectedCost.ActualCost, userRepo.lastAmount, 1e-12)
}

func TestGatewayServiceRecordUsage_PreservesChannelMappedUpstreamModel(t *testing.T) {
	usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
	svc := newGatewayRecordUsageServiceForTest(usageRepo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{})

	err := svc.RecordUsage(context.Background(), &RecordUsageInput{
		Result: &ForwardResult{
			RequestID:     "gateway_channel_mapping_models",
			Usage:         ClaudeUsage{InputTokens: 10, OutputTokens: 6},
			Model:         "gpt-5.6-terra",
			UpstreamModel: "gpt-5.6-terra",
			Duration:      time.Second,
		},
		APIKey:  &APIKey{ID: 501, Quota: 100},
		User:    &User{ID: 601},
		Account: &Account{ID: 701},
		ChannelUsageFields: ChannelUsageFields{
			OriginalModel:      "gpt-5.6-sol",
			ChannelMappedModel: "gpt-5.6-terra",
		},
	})

	require.NoError(t, err)
	require.NotNil(t, usageRepo.lastLog)
	require.Equal(t, "gpt-5.6-sol", usageRepo.lastLog.RequestedModel)
	require.Equal(t, "gpt-5.6-terra", usageRepo.lastLog.Model)
	require.NotNil(t, usageRepo.lastLog.UpstreamModel)
	require.Equal(t, "gpt-5.6-terra", *usageRepo.lastLog.UpstreamModel)
}

func TestGatewayServiceRecordUsage_PreservesLoopedChannelAndAccountUpstreamModel(t *testing.T) {
	usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
	svc := newGatewayRecordUsageServiceForTest(usageRepo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{})

	err := svc.RecordUsage(context.Background(), &RecordUsageInput{
		Result: &ForwardResult{
			RequestID:     "gateway_looped_mapping_models",
			Usage:         ClaudeUsage{InputTokens: 10, OutputTokens: 6},
			Model:         "gpt-5.6-terra",
			UpstreamModel: "gpt-5.6-sol",
			Duration:      time.Second,
		},
		APIKey:  &APIKey{ID: 501, Quota: 100},
		User:    &User{ID: 601},
		Account: &Account{ID: 701},
		ChannelUsageFields: ChannelUsageFields{
			OriginalModel:      "gpt-5.6-sol",
			ChannelMappedModel: "gpt-5.6-terra",
		},
	})

	require.NoError(t, err)
	require.NotNil(t, usageRepo.lastLog)
	require.Equal(t, "gpt-5.6-sol", usageRepo.lastLog.RequestedModel)
	require.Equal(t, "gpt-5.6-terra", usageRepo.lastLog.Model)
	require.NotNil(t, usageRepo.lastLog.UpstreamModel)
	require.Equal(t, "gpt-5.6-sol", *usageRepo.lastLog.UpstreamModel)
}

func TestGatewayServiceRecordUsage_EmptyImageSizeDefaultsBeforeBillingAndPersistence(t *testing.T) {
	imagePrice2K := 0.19
	groupID := int64(901)
	usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
	svc := newGatewayRecordUsageServiceForTest(usageRepo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{})

	err := svc.RecordUsage(context.Background(), &RecordUsageInput{
		Result: &ForwardResult{
			RequestID:      "gateway_image_default_size",
			Model:          "gemini-image",
			ImageCount:     1,
			ImageInputSize: "auto",
			Duration:       time.Second,
		},
		APIKey: &APIKey{
			ID:      801,
			GroupID: i64p(groupID),
			Group: &Group{
				ID:             groupID,
				RateMultiplier: 1.0,
				ImagePrice2K:   &imagePrice2K,
			},
		},
		User:    &User{ID: 601},
		Account: &Account{ID: 701},
	})

	require.NoError(t, err)
	require.NotNil(t, usageRepo.lastLog)
	require.Equal(t, 1, usageRepo.lastLog.ImageCount)
	require.NotNil(t, usageRepo.lastLog.ImageSize)
	require.Equal(t, ImageBillingSize2K, *usageRepo.lastLog.ImageSize)
	require.NotNil(t, usageRepo.lastLog.ImageInputSize)
	require.Equal(t, "auto", *usageRepo.lastLog.ImageInputSize)
	require.NotNil(t, usageRepo.lastLog.ImageSizeSource)
	require.Equal(t, ImageSizeSourceDefault, *usageRepo.lastLog.ImageSizeSource)
	require.InDelta(t, 0.19, usageRepo.lastLog.TotalCost, 1e-12)
	require.InDelta(t, 0.19, usageRepo.lastLog.ActualCost, 1e-12)
}

func TestGatewayServiceRecordUsage_PeakRateAffectsTokenModeImageOutputTokens(t *testing.T) {
	groupID := int64(902)
	usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
	userRepo := &openAIRecordUsageUserRepoStub{}
	svc := newGatewayRecordUsageServiceForTest(usageRepo, userRepo, &openAIRecordUsageSubRepoStub{})
	svc.resolver = newOpenAITokenImageChannelPricingResolverForTest(t, groupID, "gemini-image")

	err := svc.RecordUsage(context.Background(), &RecordUsageInput{
		Result: &ForwardResult{
			RequestID:  "gateway_peak_image_tokens",
			Model:      "gemini-image",
			ImageCount: 1,
			Usage: ClaudeUsage{
				InputTokens:       1000,
				OutputTokens:      600,
				ImageOutputTokens: 100,
			},
			Duration: time.Second,
		},
		APIKey: &APIKey{
			ID:      802,
			GroupID: i64p(groupID),
			Group: &Group{
				ID:                 groupID,
				RateMultiplier:     1.0,
				SubscriptionType:   SubscriptionTypeSubscription,
				PeakRateEnabled:    true,
				PeakStart:          "00:00",
				PeakEnd:            "23:59",
				PeakRateMultiplier: 3.0,
			},
		},
		User:    &User{ID: 602},
		Account: &Account{ID: 702},
	})

	require.NoError(t, err)
	require.NotNil(t, usageRepo.lastLog)
	require.NotNil(t, usageRepo.lastLog.BillingMode)
	require.Equal(t, string(BillingModeToken), *usageRepo.lastLog.BillingMode)
	require.Equal(t, 3.0, usageRepo.lastLog.RateMultiplier)

	textInput := 1000 * 3e-6
	textOutput := 500 * 15e-6
	imageOutput := 100 * 15e-6
	expectedActual := (textInput + textOutput + imageOutput) * 3.0

	require.InDelta(t, textInput+textOutput+imageOutput, usageRepo.lastLog.TotalCost, 1e-12)
	require.InDelta(t, imageOutput, usageRepo.lastLog.ImageOutputCost, 1e-12)
	require.InDelta(t, expectedActual, usageRepo.lastLog.ActualCost, 1e-12)
	require.InDelta(t, expectedActual, userRepo.lastAmount, 1e-12)
}

func TestGatewayServiceRecordUsage_UsageLogWriteErrorDoesNotSkipBilling(t *testing.T) {
	usageRepo := &openAIRecordUsageLogRepoStub{inserted: false, err: MarkUsageLogCreateNotPersisted(context.Canceled)}
	userRepo := &openAIRecordUsageUserRepoStub{}
	subRepo := &openAIRecordUsageSubRepoStub{}
	quotaSvc := &openAIRecordUsageAPIKeyQuotaStub{}
	svc := newGatewayRecordUsageServiceForTest(usageRepo, userRepo, subRepo)

	err := svc.RecordUsage(context.Background(), &RecordUsageInput{
		Result: &ForwardResult{
			RequestID: "gateway_not_persisted",
			Usage: ClaudeUsage{
				InputTokens:  10,
				OutputTokens: 6,
			},
			Model:    "claude-sonnet-4",
			Duration: time.Second,
		},
		APIKey: &APIKey{
			ID:    503,
			Quota: 100,
		},
		User:          &User{ID: 603},
		Account:       &Account{ID: 703},
		APIKeyService: quotaSvc,
	})

	require.NoError(t, err)
	require.Equal(t, 1, usageRepo.calls)
	require.Equal(t, 1, userRepo.deductCalls)
	require.Equal(t, 1, quotaSvc.quotaCalls)
}

func TestGatewayServiceRecordUsageWithLongContext_BillingUsesDetachedContext(t *testing.T) {
	usageRepo := &openAIRecordUsageLogRepoStub{inserted: false, err: context.DeadlineExceeded}
	userRepo := &openAIRecordUsageUserRepoStub{}
	subRepo := &openAIRecordUsageSubRepoStub{}
	quotaSvc := &openAIRecordUsageAPIKeyQuotaStub{}
	svc := newGatewayRecordUsageServiceForTest(usageRepo, userRepo, subRepo)

	reqCtx, cancel := context.WithCancel(context.Background())
	cancel()

	err := svc.RecordUsageWithLongContext(reqCtx, &RecordUsageLongContextInput{
		Result: &ForwardResult{
			RequestID: "gateway_long_context_detached_ctx",
			Usage: ClaudeUsage{
				InputTokens:  12,
				OutputTokens: 8,
			},
			Model:    "claude-sonnet-4",
			Duration: time.Second,
		},
		APIKey: &APIKey{
			ID:    502,
			Quota: 100,
		},
		User:                  &User{ID: 602},
		Account:               &Account{ID: 702},
		LongContextThreshold:  200000,
		LongContextMultiplier: 2,
		APIKeyService:         quotaSvc,
	})

	require.NoError(t, err)
	require.Equal(t, 1, usageRepo.calls)
	require.Equal(t, 1, userRepo.deductCalls)
	require.NoError(t, userRepo.lastCtxErr)
	require.Equal(t, 1, quotaSvc.quotaCalls)
	require.NoError(t, quotaSvc.lastQuotaCtxErr)
}

func TestGatewayServiceRecordUsage_UsesFallbackRequestIDForUsageLog(t *testing.T) {
	usageRepo := &openAIRecordUsageLogRepoStub{}
	userRepo := &openAIRecordUsageUserRepoStub{}
	subRepo := &openAIRecordUsageSubRepoStub{}
	svc := newGatewayRecordUsageServiceForTest(usageRepo, userRepo, subRepo)

	ctx := context.WithValue(context.Background(), ctxkey.RequestID, "gateway-local-fallback")
	err := svc.RecordUsage(ctx, &RecordUsageInput{
		Result: &ForwardResult{
			RequestID: "",
			Usage: ClaudeUsage{
				InputTokens:  10,
				OutputTokens: 6,
			},
			Model:    "claude-sonnet-4",
			Duration: time.Second,
		},
		APIKey:  &APIKey{ID: 504},
		User:    &User{ID: 604},
		Account: &Account{ID: 704},
	})

	require.NoError(t, err)
	require.NotNil(t, usageRepo.lastLog)
	require.Equal(t, "local:gateway-local-fallback", usageRepo.lastLog.RequestID)
}

func TestGatewayServiceRecordUsage_PrefersClientRequestIDOverUpstreamRequestID(t *testing.T) {
	usageRepo := &openAIRecordUsageLogRepoStub{}
	billingRepo := &openAIRecordUsageBillingRepoStub{result: &UsageBillingApplyResult{Applied: true}}
	svc := newGatewayRecordUsageServiceWithBillingRepoForTest(usageRepo, billingRepo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{})

	ctx := context.WithValue(context.Background(), ctxkey.ClientRequestID, "client-stable-123")
	ctx = context.WithValue(ctx, ctxkey.RequestID, "req-local-ignored")
	err := svc.RecordUsage(ctx, &RecordUsageInput{
		Result: &ForwardResult{
			RequestID: "upstream-volatile-456",
			Usage: ClaudeUsage{
				InputTokens:  10,
				OutputTokens: 6,
			},
			Model:    "claude-sonnet-4",
			Duration: time.Second,
		},
		APIKey:  &APIKey{ID: 506},
		User:    &User{ID: 606},
		Account: &Account{ID: 706},
	})

	require.NoError(t, err)
	require.NotNil(t, billingRepo.lastCmd)
	require.Equal(t, "client:client-stable-123", billingRepo.lastCmd.RequestID)
	require.NotNil(t, usageRepo.lastLog)
	require.Equal(t, "client:client-stable-123", usageRepo.lastLog.RequestID)
}

func TestGatewayServiceRecordUsage_GeneratesRequestIDWhenAllSourcesMissing(t *testing.T) {
	usageRepo := &openAIRecordUsageLogRepoStub{}
	billingRepo := &openAIRecordUsageBillingRepoStub{result: &UsageBillingApplyResult{Applied: true}}
	svc := newGatewayRecordUsageServiceWithBillingRepoForTest(usageRepo, billingRepo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{})

	err := svc.RecordUsage(context.Background(), &RecordUsageInput{
		Result: &ForwardResult{
			RequestID: "",
			Usage: ClaudeUsage{
				InputTokens:  10,
				OutputTokens: 6,
			},
			Model:    "claude-sonnet-4",
			Duration: time.Second,
		},
		APIKey:  &APIKey{ID: 507},
		User:    &User{ID: 607},
		Account: &Account{ID: 707},
	})

	require.NoError(t, err)
	require.NotNil(t, billingRepo.lastCmd)
	require.True(t, strings.HasPrefix(billingRepo.lastCmd.RequestID, "generated:"))
	require.NotNil(t, usageRepo.lastLog)
	require.Equal(t, billingRepo.lastCmd.RequestID, usageRepo.lastLog.RequestID)
}

func TestGatewayServiceRecordUsage_DroppedUsageLogFallsBackToSyncCreate(t *testing.T) {
	// 计费成功后 best-effort 写入被丢弃（队列超时）时必须同步兜底，
	// 否则出现“已扣费但无 usage_log”的对账缺口（issue #3656）。
	usageRepo := &openAIRecordUsageBestEffortLogRepoStub{
		bestEffortErr: MarkUsageLogCreateDropped(errors.New("usage log best-effort queue full")),
	}
	billingRepo := &openAIRecordUsageBillingRepoStub{result: &UsageBillingApplyResult{Applied: true}}
	svc := newGatewayRecordUsageServiceWithBillingRepoForTest(usageRepo, billingRepo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{})

	err := svc.RecordUsage(context.Background(), &RecordUsageInput{
		Result: &ForwardResult{
			RequestID: "gateway_drop_usage_log",
			Usage: ClaudeUsage{
				InputTokens:  10,
				OutputTokens: 6,
			},
			Model:    "claude-sonnet-4",
			Duration: time.Second,
		},
		APIKey:  &APIKey{ID: 508},
		User:    &User{ID: 608},
		Account: &Account{ID: 708},
	})

	require.NoError(t, err)
	require.Equal(t, 1, usageRepo.bestEffortCalls)
	require.Equal(t, 1, usageRepo.createCalls)
	// 兜底调用使用的 ctx 必须仍然存活，不能带着已死的 ctx 走过场。
	require.NoError(t, usageRepo.lastCtxErr)
}

func TestGatewayServiceRecordUsage_BillingErrorSkipsUsageLogWrite(t *testing.T) {
	usageRepo := &openAIRecordUsageLogRepoStub{}
	billingRepo := &openAIRecordUsageBillingRepoStub{err: context.DeadlineExceeded}
	userRepo := &openAIRecordUsageUserRepoStub{}
	subRepo := &openAIRecordUsageSubRepoStub{}
	svc := newGatewayRecordUsageServiceWithBillingRepoForTest(usageRepo, billingRepo, userRepo, subRepo)

	err := svc.RecordUsage(context.Background(), &RecordUsageInput{
		Result: &ForwardResult{
			RequestID: "gateway_billing_fail",
			Usage: ClaudeUsage{
				InputTokens:  10,
				OutputTokens: 6,
			},
			Model:    "claude-sonnet-4",
			Duration: time.Second,
		},
		APIKey:  &APIKey{ID: 505},
		User:    &User{ID: 605},
		Account: &Account{ID: 705},
	})

	require.Error(t, err)
	require.Equal(t, 1, billingRepo.calls)
	require.Equal(t, 0, usageRepo.calls)
}

func TestGatewayServiceRecordUsage_ReasoningEffortPersisted(t *testing.T) {
	usageRepo := &openAIRecordUsageBestEffortLogRepoStub{}
	svc := newGatewayRecordUsageServiceForTest(usageRepo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{})

	effort := "max"
	err := svc.RecordUsage(context.Background(), &RecordUsageInput{
		Result: &ForwardResult{
			RequestID: "effort_test",
			Usage: ClaudeUsage{
				InputTokens:  10,
				OutputTokens: 5,
			},
			Model:           "claude-opus-4-6",
			Duration:        time.Second,
			ReasoningEffort: &effort,
		},
		APIKey:  &APIKey{ID: 1},
		User:    &User{ID: 1},
		Account: &Account{ID: 1},
	})

	require.NoError(t, err)
	require.NotNil(t, usageRepo.lastLog)
	require.NotNil(t, usageRepo.lastLog.ReasoningEffort)
	require.Equal(t, "max", *usageRepo.lastLog.ReasoningEffort)
}

func TestGatewayServiceRecordUsage_ReasoningEffortNil(t *testing.T) {
	usageRepo := &openAIRecordUsageBestEffortLogRepoStub{}
	svc := newGatewayRecordUsageServiceForTest(usageRepo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{})

	err := svc.RecordUsage(context.Background(), &RecordUsageInput{
		Result: &ForwardResult{
			RequestID: "no_effort_test",
			Usage: ClaudeUsage{
				InputTokens:  10,
				OutputTokens: 5,
			},
			Model:    "claude-sonnet-4",
			Duration: time.Second,
		},
		APIKey:  &APIKey{ID: 1},
		User:    &User{ID: 1},
		Account: &Account{ID: 1},
	})

	require.NoError(t, err)
	require.NotNil(t, usageRepo.lastLog)
	require.Nil(t, usageRepo.lastLog.ReasoningEffort)
}

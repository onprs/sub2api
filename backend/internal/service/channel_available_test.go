//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/stretchr/testify/require"
)

// stubGroupRepoForAvailable 是 ListAvailable 测试用的 GroupRepository stub，
// 仅实现 ListActive；其他方法对本测试无关，返回零值即可。
// listActiveErr 非 nil 时，ListActive 返回该错误用于错误传播测试。
// listActiveCalls 记录调用次数，用于断言「失败短路时不再访问 groupRepo」等行为。
type stubGroupRepoForAvailable struct {
	activeGroups    []Group
	listActiveErr   error
	listActiveCalls int
}

func (s *stubGroupRepoForAvailable) ListActive(ctx context.Context) ([]Group, error) {
	s.listActiveCalls++
	if s.listActiveErr != nil {
		return nil, s.listActiveErr
	}
	return s.activeGroups, nil
}

func (s *stubGroupRepoForAvailable) Create(ctx context.Context, group *Group) error { return nil }
func (s *stubGroupRepoForAvailable) GetByID(ctx context.Context, id int64) (*Group, error) {
	return nil, nil
}
func (s *stubGroupRepoForAvailable) GetByIDLite(ctx context.Context, id int64) (*Group, error) {
	return nil, nil
}
func (s *stubGroupRepoForAvailable) Update(ctx context.Context, group *Group) error { return nil }
func (s *stubGroupRepoForAvailable) Delete(ctx context.Context, id int64) error     { return nil }
func (s *stubGroupRepoForAvailable) DeleteCascade(ctx context.Context, id int64) ([]int64, error) {
	return nil, nil
}
func (s *stubGroupRepoForAvailable) List(ctx context.Context, params pagination.PaginationParams) ([]Group, *pagination.PaginationResult, error) {
	return nil, nil, nil
}
func (s *stubGroupRepoForAvailable) ListWithFilters(ctx context.Context, params pagination.PaginationParams, platform, status, search string, isExclusive *bool) ([]Group, *pagination.PaginationResult, error) {
	return nil, nil, nil
}
func (s *stubGroupRepoForAvailable) ListActiveByPlatform(ctx context.Context, platform string) ([]Group, error) {
	return nil, nil
}
func (s *stubGroupRepoForAvailable) ExistsByName(ctx context.Context, name string) (bool, error) {
	return false, nil
}
func (s *stubGroupRepoForAvailable) GetAccountCount(ctx context.Context, groupID int64) (int64, int64, error) {
	return 0, 0, nil
}
func (s *stubGroupRepoForAvailable) DeleteAccountGroupsByGroupID(ctx context.Context, groupID int64) (int64, error) {
	return 0, nil
}
func (s *stubGroupRepoForAvailable) GetAccountIDsByGroupIDs(ctx context.Context, groupIDs []int64) ([]int64, error) {
	return nil, nil
}
func (s *stubGroupRepoForAvailable) BindAccountsToGroup(ctx context.Context, groupID int64, accountIDs []int64) error {
	return nil
}
func (s *stubGroupRepoForAvailable) UpdateSortOrders(ctx context.Context, updates []GroupSortOrderUpdate) error {
	return nil
}

// newAvailableChannelService 构造一个 ChannelService，channelRepo.ListAll 返回给定 channels，
// groupRepo 由参数决定。传入空 stub 表示「活跃分组列表为空」。
func newAvailableChannelService(channels []Channel, groupRepo GroupRepository) *ChannelService {
	repo := &mockChannelRepository{
		listAllFn: func(ctx context.Context) ([]Channel, error) { return channels, nil },
	}
	return NewChannelService(repo, groupRepo, nil, nil, nil)
}

func TestListAvailable_EmptyActiveGroups_NoGroupsAttached(t *testing.T) {
	// 活跃分组列表为空时，渠道的 Groups 应为空切片，不报错。
	channels := []Channel{{
		ID:       1,
		Name:     "chA",
		Status:   StatusActive,
		GroupIDs: []int64{10, 20},
	}}
	svc := newAvailableChannelService(channels, &stubGroupRepoForAvailable{})
	out, err := svc.ListAvailable(context.Background())
	require.NoError(t, err)
	require.Len(t, out, 1)
	require.Empty(t, out[0].Groups)
}

func TestListAvailable_InactiveGroupIDSilentlyDropped(t *testing.T) {
	// 渠道 GroupIDs 中引用的 group 未出现在 ListActive 结果中（已停用或删除），应被静默丢弃。
	channels := []Channel{{
		ID:       1,
		Name:     "chA",
		Status:   StatusActive,
		GroupIDs: []int64{1, 99},
	}}
	groupRepo := &stubGroupRepoForAvailable{
		activeGroups: []Group{{ID: 1, Name: "g1", Platform: "anthropic"}},
	}
	svc := newAvailableChannelService(channels, groupRepo)
	out, err := svc.ListAvailable(context.Background())
	require.NoError(t, err)
	require.Len(t, out, 1)
	require.Len(t, out[0].Groups, 1)
	require.Equal(t, int64(1), out[0].Groups[0].ID)
}

func TestListAvailable_SortedByName(t *testing.T) {
	channels := []Channel{
		{ID: 1, Name: "beta"},
		{ID: 2, Name: "Alpha"},
		{ID: 3, Name: "charlie"},
	}
	svc := newAvailableChannelService(channels, &stubGroupRepoForAvailable{})
	out, err := svc.ListAvailable(context.Background())
	require.NoError(t, err)
	require.Len(t, out, 3)
	require.Equal(t, "Alpha", out[0].Name)
	require.Equal(t, "beta", out[1].Name)
	require.Equal(t, "charlie", out[2].Name)
}

func TestListAvailable_ListAllErrorPropagates(t *testing.T) {
	// ListAll 返回错误时 ListAvailable 应直接返回包装后的错误，且不再访问 groupRepo（短路）。
	sentinel := errors.New("list-all-boom")
	repo := &mockChannelRepository{
		listAllFn: func(ctx context.Context) ([]Channel, error) { return nil, sentinel },
	}
	groupRepo := &stubGroupRepoForAvailable{}
	svc := NewChannelService(repo, groupRepo, nil, nil, nil)
	out, err := svc.ListAvailable(context.Background())
	require.Nil(t, out)
	require.ErrorIs(t, err, sentinel)
	require.Contains(t, err.Error(), "list channels", "wrap 前缀缺失，可能 %w 被改为 %v")
	require.Equal(t, 0, groupRepo.listActiveCalls, "ListAll 失败后不应再调用 groupRepo.ListActive")
}

func TestListAvailable_ListActiveErrorPropagates(t *testing.T) {
	// groupRepo.ListActive 返回错误时 ListAvailable 应直接返回包装后的错误。
	sentinel := errors.New("list-active-boom")
	svc := newAvailableChannelService(
		[]Channel{{ID: 1, Name: "chA"}},
		&stubGroupRepoForAvailable{listActiveErr: sentinel},
	)
	out, err := svc.ListAvailable(context.Background())
	require.Nil(t, out)
	require.ErrorIs(t, err, sentinel)
	require.Contains(t, err.Error(), "list active groups", "wrap 前缀缺失，可能 %w 被改为 %v")
}

func TestListAvailable_DefaultsEmptyBillingModelSource(t *testing.T) {
	// 渠道 BillingModelSource 为空时应回填为 BillingModelSourceChannelMapped，
	// 显式值应原样保留（由 service 层统一处理，避免各 handler 重复默认逻辑）。
	channels := []Channel{
		{ID: 1, Name: "empty", BillingModelSource: ""},
		{ID: 2, Name: "explicit", BillingModelSource: BillingModelSourceUpstream},
	}
	svc := newAvailableChannelService(channels, &stubGroupRepoForAvailable{})
	out, err := svc.ListAvailable(context.Background())
	require.NoError(t, err)
	require.Len(t, out, 2)

	// 按 Name 查找，避免依赖排序副作用。
	byName := make(map[string]string, len(out))
	for _, ch := range out {
		byName[ch.Name] = ch.BillingModelSource
	}
	require.Equal(t, BillingModelSourceChannelMapped, byName["empty"])
	require.Equal(t, BillingModelSourceUpstream, byName["explicit"])
}

func TestPricingNeedsFallback(t *testing.T) {
	tests := []struct {
		name string
		in   *ChannelModelPricing
		want bool
	}{
		{"nil", nil, true},
		{"empty struct", &ChannelModelPricing{BillingMode: BillingModeToken}, true},
		{"all-empty intervals", &ChannelModelPricing{
			BillingMode: BillingModeImage,
			Intervals:   []PricingInterval{{TierLabel: "1K"}, {TierLabel: "2K"}},
		}, true},
		{"flat input set", &ChannelModelPricing{InputPrice: testPtrFloat64(3e-6)}, false},
		{"flat per_request set", &ChannelModelPricing{PerRequestPrice: testPtrFloat64(0.04)}, false},
		{"interval with price", &ChannelModelPricing{
			Intervals: []PricingInterval{{TierLabel: "1K", PerRequestPrice: testPtrFloat64(0.04)}},
		}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, pricingNeedsFallback(tt.in))
		})
	}
}

func TestSynthesizePricingFromLiteLLM_TokenMode(t *testing.T) {
	lp := &LiteLLMModelPricing{
		Mode:                        "chat",
		InputCostPerToken:           3e-6,
		OutputCostPerToken:          1.5e-5,
		CacheCreationInputTokenCost: 3.75e-6,
		CacheReadInputTokenCost:     3e-7,
	}
	got := synthesizePricingFromLiteLLM(lp, nil)
	require.NotNil(t, got)
	require.Equal(t, BillingModeToken, got.BillingMode)
	require.NotNil(t, got.InputPrice)
	require.InDelta(t, 3e-6, *got.InputPrice, 1e-12)
	require.NotNil(t, got.CacheReadPrice)
}

func TestSynthesizePricingFromLiteLLM_ImageGenerationMode(t *testing.T) {
	// LiteLLM mode=image_generation 且渠道未声明模式时，按 image 合成。
	lp := &LiteLLMModelPricing{
		Mode:                    "image_generation",
		OutputCostPerImageToken: 4e-5,
	}
	got := synthesizePricingFromLiteLLM(lp, nil)
	require.NotNil(t, got)
	require.Equal(t, BillingModeImage, got.BillingMode)
	require.Nil(t, got.PerRequestPrice)
	require.NotNil(t, got.ImageOutputPrice)
}

func TestSynthesizePricingFromLiteLLM_RespectsExistingChannelMode(t *testing.T) {
	// admin UI 选了 per_request 但没填价：LiteLLM 数据按 per_request 合成,
	// 即便 LiteLLM 标的是 chat 模式也尊重渠道选择。
	lp := &LiteLLMModelPricing{
		Mode:               "chat",
		InputCostPerToken:  5e-6,
		OutputCostPerImage: 0.04,
	}
	existing := &ChannelModelPricing{BillingMode: BillingModePerRequest}
	got := synthesizePricingFromLiteLLM(lp, existing)
	require.NotNil(t, got)
	require.Equal(t, BillingModePerRequest, got.BillingMode)
	require.NotNil(t, got.PerRequestPrice)
	require.InDelta(t, 0.04, *got.PerRequestPrice, 1e-12)
}

func TestFillGlobalPricingFallback_NilPricing(t *testing.T) {
	pricingSvc := newStubPricingServiceFromMap(map[string]*LiteLLMModelPricing{
		"claude-opus-4-5": {Mode: "chat", InputCostPerToken: 5e-6},
	})
	svc := &ChannelService{pricingService: pricingSvc}

	models := []SupportedModel{
		{Name: "claude-opus-4-5", Platform: "anthropic"},
	}
	svc.fillGlobalPricingFallback(models)
	require.NotNil(t, models[0].Pricing)
	require.NotNil(t, models[0].Pricing.InputPrice)
	require.InDelta(t, 5e-6, *models[0].Pricing.InputPrice, 1e-12)
	require.Equal(t, PricingSourceCatalog, models[0].PricingSource)
}

func TestFillGlobalPricingFallback_EmptyPricingFillsFromLiteLLM(t *testing.T) {
	// 核心场景：admin UI 建了 pricing 条目（image 模式）但没填价，应走 LiteLLM 兜底。
	pricingSvc := newStubPricingServiceFromMap(map[string]*LiteLLMModelPricing{
		"gpt-image-1": {
			Mode:                    "image_generation",
			OutputCostPerImageToken: 4e-5,
		},
	})
	svc := &ChannelService{pricingService: pricingSvc}

	models := []SupportedModel{
		{
			Name:     "gpt-image-1",
			Platform: "openai",
			Pricing: &ChannelModelPricing{
				BillingMode: BillingModeImage,
				Intervals:   []PricingInterval{{TierLabel: "1K"}, {TierLabel: "2K"}},
			},
		},
	}
	svc.fillGlobalPricingFallback(models)
	require.NotNil(t, models[0].Pricing)
	require.Equal(t, BillingModeImage, models[0].Pricing.BillingMode)
	require.NotNil(t, models[0].Pricing.ImageOutputPrice)
	require.InDelta(t, 4e-5, *models[0].Pricing.ImageOutputPrice, 1e-12)
	require.Equal(t, PricingSourceCatalog, models[0].PricingSource)
}

func TestFillGlobalPricingFallback_KeepsExistingPrice(t *testing.T) {
	// 渠道已经填了价格的条目不应被回落覆盖。
	pricingSvc := newStubPricingServiceFromMap(map[string]*LiteLLMModelPricing{
		"served-model": {Mode: "chat", InputCostPerToken: 1e-6},
	})
	svc := &ChannelService{pricingService: pricingSvc}

	existing := &ChannelModelPricing{
		BillingMode: BillingModeToken,
		InputPrice:  testPtrFloat64(9e-9),
	}
	models := []SupportedModel{
		{Name: "served-model", Platform: "anthropic", Pricing: existing},
	}
	svc.fillGlobalPricingFallback(models)
	require.Same(t, existing, models[0].Pricing)
	require.Equal(t, PricingSourceChannel, models[0].PricingSource)
}

func TestFillGlobalPricingFallback_MarksMissingWhenCatalogMisses(t *testing.T) {
	pricingSvc := newStubPricingServiceFromMap(map[string]*LiteLLMModelPricing{})
	svc := &ChannelService{pricingService: pricingSvc}

	models := []SupportedModel{
		{Name: "unknown-model", Platform: "anthropic"},
	}
	svc.fillGlobalPricingFallback(models)
	require.Nil(t, models[0].Pricing)
	require.Equal(t, PricingSourceMissing, models[0].PricingSource)
}

func TestFillGlobalPricingFallback_EmptyChannelPricingMarksMissingWhenCatalogMisses(t *testing.T) {
	pricingSvc := newStubPricingServiceFromMap(map[string]*LiteLLMModelPricing{})
	svc := &ChannelService{pricingService: pricingSvc}

	models := []SupportedModel{
		{
			Name:          "empty-channel-price",
			Platform:      "anthropic",
			Pricing:       &ChannelModelPricing{BillingMode: BillingModeToken},
			PricingSource: PricingSourceChannel,
		},
	}
	svc.fillGlobalPricingFallback(models)
	require.NotNil(t, models[0].Pricing)
	require.Equal(t, PricingSourceMissing, models[0].PricingSource)
}

func TestBuildCatalogSupportedModel_UsesLookupCandidateBeforeDisplayName(t *testing.T) {
	// Antigravity 等平台的展示模型可能是管理员自定义 mapping key；
	// 原价查询应优先用 mapping value / 官方模型名，而不是只查展示名。
	pricingSvc := newStubPricingServiceFromMap(map[string]*LiteLLMModelPricing{
		"gemini-3.1-pro-low": {
			Mode:                    "chat",
			InputCostPerToken:       2e-6,
			OutputCostPerToken:      12e-6,
			CacheReadInputTokenCost: 2e-7,
		},
	})
	svc := &ChannelService{pricingService: pricingSvc}

	got := svc.BuildCatalogSupportedModel(
		"gemini-3.5-flash-low",
		PlatformAntigravity,
		[]string{"gemini-3.1-pro-low"},
	)

	require.Equal(t, "gemini-3.5-flash-low", got.Name)
	require.Equal(t, PlatformAntigravity, got.Platform)
	require.Equal(t, PricingSourceCatalog, got.PricingSource)
	require.NotNil(t, got.Pricing)
	require.NotNil(t, got.Pricing.InputPrice)
	require.InDelta(t, 2e-6, *got.Pricing.InputPrice, 1e-12)
	require.NotNil(t, got.Pricing.CacheReadPrice)
}

func TestBuildCatalogSupportedModel_AntigravityDefaultMappingCandidate(t *testing.T) {
	pricingSvc := newStubPricingServiceFromMap(map[string]*LiteLLMModelPricing{
		"claude-opus-4-6-thinking": {
			Mode:               "chat",
			InputCostPerToken:  5e-6,
			OutputCostPerToken: 25e-6,
		},
	})
	svc := &ChannelService{pricingService: pricingSvc}

	got := svc.BuildCatalogSupportedModel("claude-opus-4-6", PlatformAntigravity, nil)

	require.Equal(t, PricingSourceCatalog, got.PricingSource)
	require.NotNil(t, got.Pricing)
	require.NotNil(t, got.Pricing.InputPrice)
	require.InDelta(t, 5e-6, *got.Pricing.InputPrice, 1e-12)
}

func TestBuildCatalogSupportedModel_OpenCodeGoCodeAliasCandidate(t *testing.T) {
	pricingSvc := newStubPricingServiceFromMap(map[string]*LiteLLMModelPricing{
		"kimi-k2.7": {
			Mode:                    "chat",
			InputCostPerToken:       0.95e-6,
			OutputCostPerToken:      4.00e-6,
			CacheReadInputTokenCost: 0.19e-6,
		},
	})
	svc := &ChannelService{pricingService: pricingSvc}

	got := svc.BuildCatalogSupportedModel("kimi-k2.7-code", PlatformOpenCodeGo, nil)

	require.Equal(t, PricingSourceCatalog, got.PricingSource)
	require.NotNil(t, got.Pricing)
	require.NotNil(t, got.Pricing.InputPrice)
	require.InDelta(t, 0.95e-6, *got.Pricing.InputPrice, 1e-12)
	require.NotNil(t, got.QuotaCost)
	require.Equal(t, 60.0, got.QuotaCost.IncludedMonthlyUsageUSD)
	require.Equal(t, 1.0, got.QuotaCost.CostMultiplier)
	require.NotNil(t, got.Pricing.CacheReadPrice)
}

func TestBuildCatalogSupportedModel_ClinePassUsesReferencePricingAndContextTiers(t *testing.T) {
	pricingSvc := newStubPricingServiceFromMap(map[string]*LiteLLMModelPricing{})
	billingSvc := NewBillingService(&config.Config{}, pricingSvc)
	svc := &ChannelService{pricingService: pricingSvc, billingService: billingSvc}

	for _, modelID := range ClinePassFallbackModelIDs() {
		got := svc.BuildCatalogSupportedModel(modelID, PlatformClinePass, nil)
		require.Equal(t, PricingSourceCatalog, got.PricingSource, modelID)
		require.NotNil(t, got.Pricing, modelID)
		require.NotNil(t, got.Pricing.InputPrice, modelID)
		require.NotNil(t, got.Pricing.OutputPrice, modelID)
	}

	qwen := svc.BuildCatalogSupportedModel("cline-pass/qwen3.7-plus", PlatformClinePass, nil)
	require.NotNil(t, qwen.Pricing)
	require.Len(t, qwen.Pricing.Intervals, 2)
	baseTier := qwen.Pricing.Intervals[0]
	require.Equal(t, 0, baseTier.MinTokens)
	require.NotNil(t, baseTier.MaxTokens)
	require.Equal(t, clinePassQwen37PlusLongContextThreshold, *baseTier.MaxTokens)
	require.InDelta(t, 0.4e-6, *baseTier.InputPrice, 1e-15)
	require.InDelta(t, 1.6e-6, *baseTier.OutputPrice, 1e-15)
	require.InDelta(t, 0.5e-6, *baseTier.CacheWritePrice, 1e-15)
	require.InDelta(t, 0.04e-6, *baseTier.CacheReadPrice, 1e-15)

	longTier := qwen.Pricing.Intervals[1]
	require.Equal(t, clinePassQwen37PlusLongContextThreshold, longTier.MinTokens)
	require.Nil(t, longTier.MaxTokens)
	require.InDelta(t, 1.2e-6, *longTier.InputPrice, 1e-15)
	require.InDelta(t, 4.8e-6, *longTier.OutputPrice, 1e-15)
	require.InDelta(t, 1.5e-6, *longTier.CacheWritePrice, 1e-15)
	require.InDelta(t, 0.12e-6, *longTier.CacheReadPrice, 1e-15)
}

func TestBuildCatalogSupportedModel_OpenCodeGoSupplementalModelWithoutUsageFailsClosed(t *testing.T) {
	pricingSvc := newStubPricingServiceFromMap(map[string]*LiteLLMModelPricing{
		"kimi-k2.5": {
			Mode:                    "chat",
			InputCostPerToken:       0.60e-6,
			OutputCostPerToken:      3.00e-6,
			CacheReadInputTokenCost: 0.10e-6,
			LiteLLMProvider:         PlatformOpenCodeGo,
		},
	})
	svc := &ChannelService{pricingService: pricingSvc}

	got := svc.BuildCatalogSupportedModel("kimi-k2.5", PlatformOpenCodeGo, nil)

	require.Equal(t, PricingSourceMissing, got.PricingSource)
	require.Nil(t, got.Pricing)
	require.Nil(t, got.QuotaCost)
}

func TestBuildCatalogSupportedModel_UsesBillingFallbackPricing(t *testing.T) {
	pricingSvc := newStubPricingServiceFromMap(map[string]*LiteLLMModelPricing{})
	billingSvc := NewBillingService(&config.Config{}, pricingSvc)
	svc := &ChannelService{pricingService: pricingSvc, billingService: billingSvc}

	got := svc.BuildCatalogSupportedModel("gpt-5.3-codex-spark", PlatformOpenAI, nil)

	require.Equal(t, PricingSourceCatalog, got.PricingSource)
	require.NotNil(t, got.Pricing)
	require.NotNil(t, got.Pricing.InputPrice)
	require.InDelta(t, 1.5e-6, *got.Pricing.InputPrice, 1e-12)
	require.NotNil(t, got.Pricing.OutputPrice)
	require.InDelta(t, 12e-6, *got.Pricing.OutputPrice, 1e-12)
	require.NotNil(t, got.Pricing.CacheReadPrice)
}

func TestBuildSupportedModelForPricingGroup_OpenCodeGoChannelPricingOverridesCatalog(t *testing.T) {
	groupID := int64(10)
	input := 0.9e-6
	output := 1.8e-6
	cacheRead := 0.09e-6
	pricingSvc := &PricingService{pricingData: map[string]*LiteLLMModelPricing{}}
	svc := &ChannelService{
		pricingService: pricingSvc,
		billingService: NewBillingService(&config.Config{}, pricingSvc),
	}
	cache := newEmptyChannelCache()
	cache.channelByGroupID[groupID] = &Channel{ID: 1, Status: StatusActive}
	cache.groupPlatform[groupID] = PlatformOpenCodeGo
	cache.pricingByGroupModel[channelModelKey{
		groupID:  groupID,
		platform: PlatformOpenCodeGo,
		model:    "deepseek-v4-flash",
	}] = &ChannelModelPricing{
		Platform:       PlatformOpenCodeGo,
		Models:         []string{"deepseek-v4-flash"},
		BillingMode:    BillingModeToken,
		InputPrice:     &input,
		OutputPrice:    &output,
		CacheReadPrice: &cacheRead,
	}
	cache.loadedAt = time.Now()
	svc.cache.Store(cache)

	model := svc.BuildSupportedModelForPricingGroup(
		context.Background(),
		groupID,
		"flash-alias",
		PlatformOpenCodeGo,
		[]string{"deepseek-v4-flash"},
	)

	require.Equal(t, PricingSourceChannel, model.PricingSource)
	require.NotNil(t, model.Pricing)
	require.InDelta(t, input, *model.Pricing.InputPrice, 1e-15)
	require.InDelta(t, output, *model.Pricing.OutputPrice, 1e-15)
	require.InDelta(t, cacheRead, *model.Pricing.CacheReadPrice, 1e-15)
	require.Empty(t, model.PricingTimeBands)
}

func TestBuildSupportedModelForPricingGroup_OpenCodeGoPartialChannelPricingUsesCatalogForRemainingFields(t *testing.T) {
	groupID := int64(11)
	input := 0.9e-6
	pricingSvc := &PricingService{pricingData: map[string]*LiteLLMModelPricing{}}
	svc := &ChannelService{
		pricingService: pricingSvc,
		billingService: NewBillingService(&config.Config{}, pricingSvc),
	}
	cache := newEmptyChannelCache()
	cache.channelByGroupID[groupID] = &Channel{ID: 2, Status: StatusActive}
	cache.groupPlatform[groupID] = PlatformOpenCodeGo
	cache.pricingByGroupModel[channelModelKey{
		groupID:  groupID,
		platform: PlatformOpenCodeGo,
		model:    "deepseek-v4-pro",
	}] = &ChannelModelPricing{
		Platform:    PlatformOpenCodeGo,
		Models:      []string{"deepseek-v4-pro"},
		BillingMode: BillingModeToken,
		InputPrice:  &input,
	}
	cache.loadedAt = time.Now()
	svc.cache.Store(cache)

	model := svc.BuildSupportedModelForPricingGroup(
		context.Background(),
		groupID,
		"deepseek-v4-pro",
		PlatformOpenCodeGo,
		nil,
	)

	require.Equal(t, PricingSourceChannel, model.PricingSource)
	require.NotNil(t, model.Pricing)
	require.InDelta(t, input, *model.Pricing.InputPrice, 1e-15)
	require.Len(t, model.PricingTimeBands, 2)
	offPeak := model.PricingTimeBands[0]
	require.Equal(t, "off_peak", offPeak.Code)
	require.Equal(t, "UTC", offPeak.TimeZone)
	require.Equal(t, []string{"00:00-01:00", "04:00-06:00", "10:00-24:00"}, offPeak.TimeRanges)
	require.InDelta(t, input, *offPeak.Pricing.InputPrice, 1e-15)
	require.InDelta(t, 1.98e-6, *offPeak.Pricing.OutputPrice, 1e-15)
	require.InDelta(t, 0.022e-6, *offPeak.Pricing.CacheReadPrice, 1e-15)
	peak := model.PricingTimeBands[1]
	require.Equal(t, "peak", peak.Code)
	require.Equal(t, []string{"01:00-04:00", "06:00-10:00"}, peak.TimeRanges)
	require.InDelta(t, input, *peak.Pricing.InputPrice, 1e-15)
	require.InDelta(t, 3.96e-6, *peak.Pricing.OutputPrice, 1e-15)
	require.InDelta(t, 0.044e-6, *peak.Pricing.CacheReadPrice, 1e-15)
}

func TestFillGlobalPricingFallbackCommandCodeAddsPromotionAfterCatalogResolution(t *testing.T) {
	pricingSvc := &PricingService{pricingData: map[string]*LiteLLMModelPricing{}}
	billingSvc := NewBillingService(&config.Config{}, pricingSvc)
	svc := &ChannelService{pricingService: pricingSvc, billingService: billingSvc}
	models := []SupportedModel{{
		Name:     "google/gemini-3.7-flash",
		Platform: PlatformCommandCode,
	}}

	svc.fillGlobalPricingFallback(models)

	require.Equal(t, PricingSourceCatalog, models[0].PricingSource)
	require.Equal(t, 1_048_576, models[0].ContextWindow)
	require.NotNil(t, models[0].Promotion)
	require.Equal(t, "50% off", models[0].Promotion.Label)
}

func TestBuildCatalogSupportedModel_CommandCode(t *testing.T) {
	pricingSvc := &PricingService{pricingData: map[string]*LiteLLMModelPricing{}}
	billingSvc := NewBillingService(&config.Config{}, pricingSvc)
	svc := &ChannelService{pricingService: pricingSvc, billingService: billingSvc}

	// 1. DeepSeek peak/off-peak
	ds := svc.BuildCatalogSupportedModel("deepseek/deepseek-v4-pro", PlatformCommandCode, nil)
	require.Equal(t, PricingSourceCatalog, ds.PricingSource)
	require.Equal(t, 1_000_000, ds.ContextWindow)
	require.Nil(t, ds.Promotion)
	require.NotNil(t, ds.QuotaCost)
	require.InDelta(t, 20, ds.QuotaCost.IncludedMonthlyUsageUSD, 1e-9)
	require.InDelta(t, 3.5, ds.QuotaCost.CostMultiplier, 1e-9)
	require.Len(t, ds.PricingTimeBands, 2)
	offPeak := ds.PricingTimeBands[0]
	require.Equal(t, "off_peak", offPeak.Code)
	require.InDelta(t, 0.66e-6, *offPeak.Pricing.InputPrice, 1e-15)
	require.InDelta(t, 1.98e-6, *offPeak.Pricing.OutputPrice, 1e-15)
	require.InDelta(t, 0.022e-6, *offPeak.Pricing.CacheReadPrice, 1e-15)
	peak := ds.PricingTimeBands[1]
	require.Equal(t, "peak", peak.Code)
	require.InDelta(t, 1.32e-6, *peak.Pricing.InputPrice, 1e-15)
	require.InDelta(t, 3.96e-6, *peak.Pricing.OutputPrice, 1e-15)
	require.InDelta(t, 0.044e-6, *peak.Pricing.CacheReadPrice, 1e-15)

	// 2. Qwen 3.7 Max flat with cache write
	qwen := svc.BuildCatalogSupportedModel("Qwen/Qwen3.7-Max", PlatformCommandCode, nil)
	require.Equal(t, PricingSourceCatalog, qwen.PricingSource)
	require.NotNil(t, qwen.Pricing)
	require.NotNil(t, qwen.QuotaCost)
	require.InDelta(t, 33, qwen.QuotaCost.IncludedMonthlyUsageUSD, 1e-9)
	require.InDelta(t, 70.0/33.0, qwen.QuotaCost.CostMultiplier, 1e-9)
	require.Empty(t, qwen.PricingTimeBands)
	require.InDelta(t, 2.5e-6, *qwen.Pricing.InputPrice, 1e-15)
	require.InDelta(t, 7.5e-6, *qwen.Pricing.OutputPrice, 1e-15)
	require.InDelta(t, 0.5e-6, *qwen.Pricing.CacheReadPrice, 1e-15)
	require.InDelta(t, 3.13e-6, *qwen.Pricing.CacheWritePrice, 1e-15)

	// 3. 官方免费模型与活动元数据
	free := svc.BuildCatalogSupportedModel("poolside/laguna-s-2.1-free", PlatformCommandCode, nil)
	require.Equal(t, PricingSourceCatalog, free.PricingSource)
	require.Equal(t, 256_000, free.ContextWindow)
	require.NotNil(t, free.Promotion)
	require.True(t, free.Promotion.Free)
	require.Equal(t, "Free", free.Promotion.Label)
	require.NotNil(t, free.Pricing)
	require.InDelta(t, 0.0, *free.Pricing.InputPrice, 1e-15)
	require.InDelta(t, 0.0, *free.Pricing.OutputPrice, 1e-15)

	// 4. 官方促销与上下文档位
	gemini := svc.BuildCatalogSupportedModel("google/gemini-3.7-flash", PlatformCommandCode, nil)
	require.Equal(t, 1_048_576, gemini.ContextWindow)
	require.NotNil(t, gemini.Promotion)
	require.Equal(t, "50% off", gemini.Promotion.Label)

	qwenFlash := svc.BuildCatalogSupportedModel("Qwen/Qwen3.7-Flash", PlatformCommandCode, nil)
	require.Len(t, qwenFlash.Pricing.Intervals, 3)
	require.Equal(t, 32_000, *qwenFlash.Pricing.Intervals[0].MaxTokens)
	require.Equal(t, 256_000, *qwenFlash.Pricing.Intervals[1].MaxTokens)
}

func TestBuildCatalogSupportedModel_OpenCodeGoShowsOfficialPeakAndOffPeakPricing(t *testing.T) {
	pricingSvc := &PricingService{pricingData: map[string]*LiteLLMModelPricing{}}
	billingSvc := NewBillingService(&config.Config{}, pricingSvc)
	svc := &ChannelService{pricingService: pricingSvc, billingService: billingSvc}

	model := svc.BuildCatalogSupportedModel("deepseek-v4-flash-vision-exp", PlatformOpenCodeGo, nil)

	require.Equal(t, PricingSourceCatalog, model.PricingSource)
	require.NotNil(t, model.Pricing)
	require.Len(t, model.PricingTimeBands, 2)
	offPeak := model.PricingTimeBands[0]
	require.Equal(t, "off_peak", offPeak.Code)
	require.Equal(t, "UTC", offPeak.TimeZone)
	require.Equal(t, []string{"00:00-01:00", "04:00-06:00", "10:00-24:00"}, offPeak.TimeRanges)
	require.InDelta(t, 0.22e-6, *offPeak.Pricing.InputPrice, 1e-15)
	require.InDelta(t, 0.66e-6, *offPeak.Pricing.OutputPrice, 1e-15)
	require.InDelta(t, 0.007e-6, *offPeak.Pricing.CacheReadPrice, 1e-15)
	peak := model.PricingTimeBands[1]
	require.Equal(t, "peak", peak.Code)
	require.Equal(t, []string{"01:00-04:00", "06:00-10:00"}, peak.TimeRanges)
	require.InDelta(t, 0.44e-6, *peak.Pricing.InputPrice, 1e-15)
	require.InDelta(t, 1.32e-6, *peak.Pricing.OutputPrice, 1e-15)
	require.InDelta(t, 0.014e-6, *peak.Pricing.CacheReadPrice, 1e-15)

	hy3 := svc.BuildCatalogSupportedModel("hy3", PlatformOpenCodeGo, nil)
	require.Empty(t, hy3.PricingTimeBands)
}

func TestBuildCatalogSupportedModel_OpenCodeGoUsageOfferPreservesOriginalPricingAndAdjustsQuotaCost(t *testing.T) {
	confirmedAt := time.Now()
	pricingSvc := &PricingService{
		pricingData: map[string]*LiteLLMModelPricing{},
		openCodeGoUsageOffers: map[string]openCodeGoUsageOffer{
			"deepseek-v4-flash": {usageMultiplier: 2, confirmedAt: confirmedAt},
		},
	}
	billingSvc := NewBillingService(&config.Config{}, pricingSvc)
	svc := &ChannelService{pricingService: pricingSvc, billingService: billingSvc}

	model := svc.BuildCatalogSupportedModel("deepseek-v4-flash", PlatformOpenCodeGo, nil)

	require.NotNil(t, model.Pricing)
	require.InDelta(t, 0.22e-6, *model.Pricing.InputPrice, 1e-15)
	require.InDelta(t, 0.66e-6, *model.Pricing.OutputPrice, 1e-15)
	require.NotNil(t, model.QuotaCost)
	require.Equal(t, 60.0, model.QuotaCost.IncludedMonthlyUsageUSD)
	require.Equal(t, 1.0, model.QuotaCost.CostMultiplier)
	require.NotNil(t, model.UsageOffer)
	require.Equal(t, "opencode_go_usage_offer", model.UsageOffer.Code)
	require.Equal(t, 2.0, model.UsageOffer.UsageMultiplier)
}

func TestBuildCatalogSupportedModel_OpenCodeGoOfficialZeroRateIsDisplayedAsCatalogPricing(t *testing.T) {
	pricingSvc := &PricingService{
		openCodeGoPricing: map[string]*LiteLLMModelPricing{
			"ox-alpha-free": {
				LiteLLMProvider:            PlatformOpenCodeGo,
				OpenCodeGoPricingAuthority: openCodeGoPricingAuthorityOfficial,
				OpenCodeGoExplicitZeroRate: true,
			},
		},
		openCodeGoPricingConfirmedAt: time.Now(),
	}
	billingSvc := NewBillingService(&config.Config{}, pricingSvc)
	svc := &ChannelService{pricingService: pricingSvc, billingService: billingSvc}

	model := svc.BuildCatalogSupportedModel("ox-alpha-free", PlatformOpenCodeGo, nil)

	require.Equal(t, PricingSourceCatalog, model.PricingSource)
	require.NotNil(t, model.Pricing)
	require.NotNil(t, model.Pricing.InputPrice)
	require.NotNil(t, model.Pricing.OutputPrice)
	require.Zero(t, *model.Pricing.InputPrice)
	require.NotNil(t, model.QuotaCost)
	require.Zero(t, model.QuotaCost.IncludedMonthlyUsageUSD)
	require.Equal(t, 1.0, model.QuotaCost.CostMultiplier)
	require.Zero(t, *model.Pricing.OutputPrice)
}

func TestBuildCatalogSupportedModel_OpenCodeGoReferenceCatalogRequiresQuotaCostEvidence(t *testing.T) {
	pricingSvc := newStubPricingServiceFromMap(map[string]*LiteLLMModelPricing{})
	billingSvc := NewBillingService(&config.Config{}, pricingSvc)
	svc := &ChannelService{pricingService: pricingSvc, billingService: billingSvc}

	for modelID := range openCodeGoSeedCatalog() {
		if modelID == "ox-alpha-free" {
			continue
		}
		model := svc.BuildCatalogSupportedModel(modelID, PlatformOpenCodeGo, nil)
		quotaCost, known := openCodeGoReferenceQuotaCost(modelID)
		if !known {
			require.Equal(t, PricingSourceMissing, model.PricingSource, modelID)
			require.Nil(t, model.Pricing, modelID)
			require.Nil(t, model.QuotaCost, modelID)
			continue
		}
		require.Equal(t, PricingSourceCatalog, model.PricingSource, modelID)
		require.NotNil(t, model.Pricing, modelID)
		require.NotNil(t, model.Pricing.InputPrice, modelID)
		require.NotNil(t, model.Pricing.OutputPrice, modelID)
		require.NotNil(t, model.QuotaCost, modelID)
		require.Equal(t, quotaCost.IncludedMonthlyUsageUSD, model.QuotaCost.IncludedMonthlyUsageUSD, modelID)
		require.Equal(t, quotaCost.Multiplier, model.QuotaCost.CostMultiplier, modelID)
	}
}

func newStubPricingServiceFromMap(data map[string]*LiteLLMModelPricing) *PricingService {
	return &PricingService{pricingData: data}
}

//go:build unit

package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestUserAvailableChannel_Unauthenticated401(t *testing.T) {
	// 没有 AuthSubject 注入时，handler 应返回 401 且不触达 service 依赖。
	gin.SetMode(gin.TestMode)
	h := &AvailableChannelHandler{} // nil services — 401 路径不会调用它们
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/channels/available", nil)

	h.List(c)

	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestFilterUserVisibleGroups_IntersectionOnly(t *testing.T) {
	// 渠道挂在 {g1, g2, g3}，用户只允许 {g1, g3} —— 响应必须仅含 g1/g3。
	groups := []service.AvailableGroupRef{
		{ID: 1, Name: "g1", Platform: "anthropic"},
		{ID: 2, Name: "g2", Platform: "anthropic"},
		{ID: 3, Name: "g3", Platform: "openai"},
	}
	allowed := map[int64]struct{}{1: {}, 3: {}}

	visible := filterUserVisibleGroups(groups, allowed)
	require.Len(t, visible, 2)
	ids := []int64{visible[0].ID, visible[1].ID}
	require.ElementsMatch(t, []int64{1, 3}, ids)
}

func TestUserAvailableGroupFromService_PreservesPeakRateFields(t *testing.T) {
	pricingAt := time.Date(2026, 8, 22, 15, 30, 0, 0, timezone.Location())
	got := userAvailableGroupFromServiceAt(service.Group{
		ID:                 17,
		Name:               "OpenCode Go",
		Platform:           service.PlatformOpenCodeGo,
		SubscriptionType:   service.SubscriptionTypeSubscription,
		RateMultiplier:     0.75,
		PeakRateEnabled:    true,
		PeakStart:          "14:00",
		PeakEnd:            "18:00",
		PeakRateMultiplier: 2.5,
		IsExclusive:        true,
	}, pricingAt)

	require.Equal(t, int64(17), got.ID)
	require.True(t, got.PeakRateEnabled)
	require.Equal(t, "14:00", got.PeakStart)
	require.Equal(t, "18:00", got.PeakEnd)
	require.Equal(t, 2.5, got.PeakRateMultiplier)
	require.Equal(t, 2.5, got.CurrentPeakMultiplier)
}

func TestToUserSupportedModels_FiltersByAllowedPlatforms(t *testing.T) {
	// 用户可访问分组只覆盖 anthropic；anthropic 平台的模型保留，openai 模型被剔除。
	src := []service.SupportedModel{
		{Name: "claude-sonnet-4-6", Platform: "anthropic", Pricing: nil},
		{Name: "gpt-4o", Platform: "openai", Pricing: nil},
	}
	allowed := map[string]struct{}{"anthropic": {}}
	out := toUserSupportedModels(src, allowed)
	require.Len(t, out, 1)
	require.Equal(t, "claude-sonnet-4-6", out[0].Name)
}

func TestToUserSupportedModels_NilAllowedPlatformsKeepsAll(t *testing.T) {
	// 显式传 nil allowedPlatforms 表示不做过滤。
	src := []service.SupportedModel{
		{Name: "a", Platform: "anthropic"},
		{Name: "b", Platform: "openai"},
	}
	require.Len(t, toUserSupportedModels(src, nil), 2)
}

func TestToUserSupportedModels_ExposesPricingTimeBands(t *testing.T) {
	inputPrice := 0.22e-6
	outputPrice := 0.66e-6
	src := []service.SupportedModel{
		{
			Name:       "deepseek-v4-flash",
			Platform:   service.PlatformOpenCodeGo,
			Pricing:    &service.ChannelModelPricing{},
			QuotaCost:  &service.ModelQuotaCost{IncludedMonthlyUsageUSD: 30, CostMultiplier: 2},
			UsageOffer: &service.ModelUsageOffer{Code: "opencode_go_usage_offer", UsageMultiplier: 2},
			PricingTimeBands: []service.ModelPricingTimeBand{
				{
					Code:       "off_peak",
					TimeZone:   "UTC",
					TimeRanges: []string{"00:00-01:00", "04:00-06:00", "10:00-24:00"},
					Pricing: &service.ChannelModelPricing{
						InputPrice:  &inputPrice,
						OutputPrice: &outputPrice,
					},
				},
			},
		},
	}

	out := toUserSupportedModels(src, nil)
	require.Len(t, out, 1)
	require.NotNil(t, out[0].Pricing)
	require.Len(t, out[0].Pricing.TimeBands, 1)
	require.Equal(t, "off_peak", out[0].Pricing.TimeBands[0].Code)
	require.Equal(t, "UTC", out[0].Pricing.TimeBands[0].TimeZone)
	require.Equal(t, []string{"00:00-01:00", "04:00-06:00", "10:00-24:00"}, out[0].Pricing.TimeBands[0].TimeRanges)
	require.InDelta(t, inputPrice, *out[0].Pricing.TimeBands[0].InputPrice, 1e-15)
	require.InDelta(t, outputPrice, *out[0].Pricing.TimeBands[0].OutputPrice, 1e-15)
	require.NotNil(t, out[0].ModelSpecificMultiplier)
	require.Equal(t, 2.0, *out[0].ModelSpecificMultiplier)
	require.NotNil(t, out[0].UsageOffer)
	require.Equal(t, "opencode_go_usage_offer", out[0].UsageOffer.Code)
	require.Equal(t, 2.0, out[0].UsageOffer.UsageMultiplier)

	raw, err := json.Marshal(out[0])
	require.NoError(t, err)
	require.NotContains(t, string(raw), "included_monthly_usage_usd")
	require.NotContains(t, string(raw), `"quota_cost"`)
	require.NotContains(t, string(raw), `"cost_multiplier"`)
	var decoded map[string]any
	require.NoError(t, json.Unmarshal(raw, &decoded))
	pricing, ok := decoded["pricing"].(map[string]any)
	require.True(t, ok)
	timeBands, ok := pricing["time_bands"].([]any)
	require.True(t, ok)
	require.Len(t, timeBands, 1)
	require.Equal(t, float64(2), decoded["model_specific_multiplier"])
	require.NotContains(t, decoded, "quota_cost")
	require.NotContains(t, decoded, "included_monthly_usage_usd")
	usageOffer, ok := decoded["usage_offer"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "opencode_go_usage_offer", usageOffer["code"])
	require.Equal(t, float64(2), usageOffer["usage_multiplier"])
	require.NotContains(t, usageOffer, "cost_multiplier")
}

func TestAvailableChannelFeatureEnabled_ModelPricingPurposeUsesIndependentFlag(t *testing.T) {
	gin.SetMode(gin.TestMode)
	settingSvc := service.NewSettingService(&settingHandlerPublicRepoStub{
		values: map[string]string{
			"available_channels_enabled": "false",
			"model_pricing_enabled":      "true",
		},
	}, &config.Config{})
	h := &AvailableChannelHandler{settingService: settingSvc}

	regularRecorder := httptest.NewRecorder()
	regularContext, _ := gin.CreateTestContext(regularRecorder)
	regularContext.Request = httptest.NewRequest(http.MethodGet, "/api/v1/channels/available", nil)
	require.False(t, h.featureEnabled(regularContext))

	pricingRecorder := httptest.NewRecorder()
	pricingContext, _ := gin.CreateTestContext(pricingRecorder)
	pricingContext.Request = httptest.NewRequest(http.MethodGet, "/api/v1/channels/available?purpose=model_pricing", nil)
	require.True(t, h.featureEnabled(pricingContext))
}

func TestUserAvailableChannel_FieldWhitelist(t *testing.T) {
	// 通过序列化 userAvailableChannel 结构体验证响应形状：
	// 只有 name / description / platforms；不含管理端字段。
	row := userAvailableChannel{
		Name:        "ch",
		Description: "d",
		Platforms: []userChannelPlatformSection{
			{
				Platform:        "anthropic",
				Groups:          []userAvailableGroup{{ID: 1, Name: "g1", Platform: "anthropic"}},
				SupportedModels: []userSupportedModel{},
			},
		},
	}
	raw, err := json.Marshal(row)
	require.NoError(t, err)
	var decoded map[string]any
	require.NoError(t, json.Unmarshal(raw, &decoded))

	for _, key := range []string{"id", "status", "billing_model_source", "restrict_models"} {
		_, exists := decoded[key]
		require.Falsef(t, exists, "user DTO must not expose %q", key)
	}
	for _, key := range []string{"name", "description", "platforms"} {
		_, exists := decoded[key]
		require.Truef(t, exists, "user DTO must expose %q", key)
	}

	// 验证 section 的字段（platform / groups / supported_models）。
	rawSection, err := json.Marshal(row.Platforms[0])
	require.NoError(t, err)
	var sectionDecoded map[string]any
	require.NoError(t, json.Unmarshal(rawSection, &sectionDecoded))
	for _, key := range []string{"platform", "groups", "supported_models"} {
		_, exists := sectionDecoded[key]
		require.Truef(t, exists, "platform section must expose %q", key)
	}

	// Group DTO 暴露区分专属/公开、订阅类型、默认倍率和高峰倍率规则所需的字段，
	// 前端据此渲染 GroupBadge 并与 API 密钥页保持一致的视觉。
	rawGroup, err := json.Marshal(row.Platforms[0].Groups[0])
	require.NoError(t, err)
	var groupDecoded map[string]any
	require.NoError(t, json.Unmarshal(rawGroup, &groupDecoded))
	for _, key := range []string{"id", "name", "platform", "subscription_type", "rate_multiplier", "peak_rate_enabled", "peak_start", "peak_end", "peak_rate_multiplier", "current_peak_multiplier", "is_exclusive"} {
		_, exists := groupDecoded[key]
		require.Truef(t, exists, "group DTO must expose %q", key)
	}

	// pricing interval 白名单：不应暴露 id / sort_order。
	inputPrice := 1e-6
	pricing := toUserPricing(&service.ChannelModelPricing{
		BillingMode: service.BillingModeToken,
		InputPrice:  &inputPrice,
		Intervals: []service.PricingInterval{
			{ID: 7, MinTokens: 0, MaxTokens: nil, SortOrder: 3},
		},
	}, service.PricingSourceChannel)
	require.NotNil(t, pricing)
	rawPricing, err := json.Marshal(pricing)
	require.NoError(t, err)
	var pricingDecoded map[string]any
	require.NoError(t, json.Unmarshal(rawPricing, &pricingDecoded))
	require.Equal(t, "channel", pricingDecoded["pricing_source"])
	require.Equal(t, "modelPricing.sources.channel", pricingDecoded["pricing_source_label"])
	require.Equal(t, "channel_model_pricing", pricingDecoded["pricing_source_detail"])
	require.Len(t, pricing.Intervals, 1)
	rawIv, err := json.Marshal(pricing.Intervals[0])
	require.NoError(t, err)
	var ivDecoded map[string]any
	require.NoError(t, json.Unmarshal(rawIv, &ivDecoded))
	for _, key := range []string{"id", "pricing_id", "sort_order"} {
		_, exists := ivDecoded[key]
		require.Falsef(t, exists, "user pricing interval must not expose %q", key)
	}
}

func TestToUserPricing_MissingSourceReturnsInspectablePricingObject(t *testing.T) {
	pricing := toUserPricing(nil, service.PricingSourceMissing)

	require.NotNil(t, pricing)
	require.Equal(t, string(service.BillingModeToken), pricing.BillingMode)
	require.Equal(t, service.PricingSourceMissing, pricing.PricingSource)
	require.Equal(t, "modelPricing.sources.missing", pricing.PricingSourceLabel)
	require.Equal(t, "pricing_missing", pricing.PricingSourceDetail)
	require.Nil(t, pricing.InputPrice)
	require.Empty(t, pricing.Intervals)
}

func TestBuildPlatformSections_GroupsByPlatform(t *testing.T) {
	// 一个渠道横跨 anthropic / openai / 空平台：应该生成 2 个 section，
	// 按 platform 字母序排序，各自 groups 和 supported_models 只含同平台条目。
	ch := service.AvailableChannel{
		Name: "ch",
		SupportedModels: []service.SupportedModel{
			{Name: "claude-sonnet-4-6", Platform: "anthropic"},
			{Name: "gpt-4o", Platform: "openai"},
		},
	}
	visible := []userAvailableGroup{
		{ID: 1, Name: "g-openai", Platform: "openai"},
		{ID: 2, Name: "g-ant", Platform: "anthropic"},
		{ID: 3, Name: "g-empty", Platform: ""},
	}
	sections := buildPlatformSections(ch, visible)
	require.Len(t, sections, 2)
	require.Equal(t, "anthropic", sections[0].Platform)
	require.Equal(t, "openai", sections[1].Platform)
	require.Len(t, sections[0].Groups, 1)
	require.Equal(t, int64(2), sections[0].Groups[0].ID)
	require.Len(t, sections[0].SupportedModels, 1)
	require.Equal(t, "claude-sonnet-4-6", sections[0].SupportedModels[0].Name)
}

type modelPricingGatewayStub struct {
	modelsByGroup     map[int64][]string
	candidatesByGroup map[int64]map[string][]string
}

func (s *modelPricingGatewayStub) GetAvailableModels(_ context.Context, groupID *int64, _ string) []string {
	if groupID == nil {
		return nil
	}
	return append([]string(nil), s.modelsByGroup[*groupID]...)
}

func (s *modelPricingGatewayStub) GetAvailableModelPricingCandidates(_ context.Context, groupID *int64, _ string, models []string) map[string][]string {
	out := make(map[string][]string, len(models))
	if groupID != nil {
		for model, candidates := range s.candidatesByGroup[*groupID] {
			out[model] = append([]string(nil), candidates...)
		}
	}
	for _, model := range models {
		if _, ok := out[model]; !ok {
			out[model] = []string{model}
		}
	}
	return out
}

func TestBuildModelPricingChannels_RootsAtGroupsAndUsesMappedPricingCandidate(t *testing.T) {
	pricingSvc := newHandlerPricingService(t, `{
		"gemini-3.1-pro-low": {
			"input_cost_per_token": 0.000002,
			"output_cost_per_token": 0.000012,
			"cache_read_input_token_cost": 0.0000002,
			"litellm_provider": "vertex_ai-language-models",
			"mode": "chat"
		}
	}`)
	h := &AvailableChannelHandler{
		channelService: service.NewChannelService(nil, nil, nil, pricingSvc, service.NewBillingService(&config.Config{}, pricingSvc)),
		modelPricingModels: &modelPricingGatewayStub{
			modelsByGroup: map[int64][]string{
				8: {"gemini-3.5-flash-low"},
			},
			candidatesByGroup: map[int64]map[string][]string{
				8: {
					"gemini-3.5-flash-low": {"gemini-3.1-pro-low"},
				},
			},
		},
	}

	got := h.buildModelPricingChannels(context.Background(), []service.Group{
		{
			ID:               8,
			Name:             "Antigravity-TEST",
			Platform:         service.PlatformAntigravity,
			RateMultiplier:   1.2,
			SubscriptionType: service.SubscriptionTypeStandard,
		},
	}, nil)

	require.Len(t, got, 1)
	require.Equal(t, "Antigravity-TEST", got[0].Name)
	require.Len(t, got[0].Platforms, 1)
	section := got[0].Platforms[0]
	require.Equal(t, service.PlatformAntigravity, section.Platform)
	require.Len(t, section.Groups, 1)
	require.Equal(t, int64(8), section.Groups[0].ID)
	require.Len(t, section.SupportedModels, 1)
	model := section.SupportedModels[0]
	require.Equal(t, "gemini-3.5-flash-low", model.Name)
	require.NotNil(t, model.Pricing)
	require.Equal(t, service.PricingSourceCatalog, model.Pricing.PricingSource)
	require.NotNil(t, model.Pricing.InputPrice)
	require.InDelta(t, 0.000002, *model.Pricing.InputPrice, 1e-12)
	require.NotNil(t, model.Pricing.CacheReadPrice)
}

func TestBuildModelPricingChannels_OpenCodeGoFallbackIncludesOfficialCatalogModel(t *testing.T) {
	pricingSvc := newHandlerPricingService(t, `{
		"glm-5.2": {
			"input_cost_per_token": 0.0000014,
			"output_cost_per_token": 0.0000044,
			"cache_read_input_token_cost": 0.00000026,
			"litellm_provider": "opencode_go",
			"mode": "chat"
		},
		"kimi-k2.5": {
			"input_cost_per_token": 0.0000006,
			"output_cost_per_token": 0.000003,
			"cache_read_input_token_cost": 0.0000001,
			"litellm_provider": "opencode_go",
			"mode": "chat"
		}
	}`)
	h := &AvailableChannelHandler{
		channelService: service.NewChannelService(nil, nil, nil, pricingSvc, service.NewBillingService(&config.Config{}, pricingSvc)),
	}

	got := h.buildModelPricingChannels(context.Background(), []service.Group{
		{
			ID:               18,
			Name:             "OpenCode Go",
			Platform:         service.PlatformOpenCodeGo,
			RateMultiplier:   1,
			SubscriptionType: service.SubscriptionTypeStandard,
		},
	}, nil)

	require.Len(t, got, 1)
	section := got[0].Platforms[0]
	require.Equal(t, service.PlatformOpenCodeGo, section.Platform)
	models := map[string]userSupportedModel{}
	for _, model := range section.SupportedModels {
		models[model.Name] = model
	}
	glm := models["glm-5.2"]
	require.Equal(t, "glm-5.2", glm.Name)
	require.NotNil(t, glm.Pricing)
	require.Equal(t, service.PricingSourceCatalog, glm.Pricing.PricingSource)
	require.NotNil(t, glm.Pricing.InputPrice)
	require.NotNil(t, glm.ModelSpecificMultiplier)
	require.Equal(t, 1.0, *glm.ModelSpecificMultiplier)
	require.InDelta(t, 0.0000014, *glm.Pricing.InputPrice, 1e-12)

	kimi25 := models["kimi-k2.5"]
	require.Equal(t, "kimi-k2.5", kimi25.Name)
	require.NotNil(t, kimi25.Pricing)
	require.Equal(t, service.PricingSourceMissing, kimi25.Pricing.PricingSource)
	require.Nil(t, kimi25.Pricing.InputPrice)
	require.Nil(t, kimi25.ModelSpecificMultiplier)
}

func TestBuildModelPricingChannels_ClinePassIncludesEveryReferencePrice(t *testing.T) {
	h := &AvailableChannelHandler{
		channelService: service.NewChannelService(nil, nil, nil, nil, service.NewBillingService(&config.Config{}, nil)),
		modelPricingModels: &modelPricingGatewayStub{
			modelsByGroup: map[int64][]string{
				28: service.ClinePassFallbackModelIDs(),
			},
		},
	}

	got := h.buildModelPricingChannels(context.Background(), []service.Group{
		{
			ID:               28,
			Name:             "ClinePass",
			Platform:         service.PlatformClinePass,
			RateMultiplier:   1,
			SubscriptionType: service.SubscriptionTypeStandard,
		},
	}, nil)

	require.Len(t, got, 1)
	section := got[0].Platforms[0]
	require.Equal(t, service.PlatformClinePass, section.Platform)
	require.Len(t, section.SupportedModels, len(service.ClinePassFallbackModelIDs()))
	models := make(map[string]userSupportedModel, len(section.SupportedModels))
	for _, model := range section.SupportedModels {
		require.NotNil(t, model.Pricing, model.Name)
		require.Equal(t, service.PricingSourceCatalog, model.Pricing.PricingSource, model.Name)
		require.NotNil(t, model.Pricing.InputPrice, model.Name)
		require.NotNil(t, model.Pricing.OutputPrice, model.Name)
		models[model.Name] = model
	}

	qwen := models["cline-pass/qwen3.7-plus"]
	require.NotNil(t, qwen.Pricing)
	require.Len(t, qwen.Pricing.Intervals, 2)
	require.Equal(t, 256000, *qwen.Pricing.Intervals[0].MaxTokens)
	require.Equal(t, 256000, qwen.Pricing.Intervals[1].MinTokens)
}

func newHandlerPricingService(t *testing.T, data string) *service.PricingService {
	t.Helper()
	dataDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dataDir, "model_pricing.json"), []byte(data), 0644))
	pricingSvc := service.NewPricingService(&config.Config{
		Pricing: config.PricingConfig{
			DataDir:                  dataDir,
			UpdateIntervalHours:      24,
			HashCheckIntervalMinutes: 600,
		},
	}, nil)
	require.NoError(t, pricingSvc.Initialize())
	t.Cleanup(pricingSvc.Stop)
	return pricingSvc
}

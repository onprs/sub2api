package handler

import (
	"context"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// AvailableChannelHandler 处理用户侧「可用渠道」查询。
//
// 用户侧接口委托 ChannelService.ListAvailable，并在返回前做三层过滤：
//  1. 行过滤：只保留状态为 Active 且与当前用户可访问分组有交集的渠道；
//  2. 分组过滤：渠道的 Groups 只保留用户可访问的那些；
//  3. 平台过滤：渠道的 SupportedModels 只保留平台在用户可见 Groups 中出现过的模型，
//     防止"渠道同时挂在 antigravity / anthropic 两个平台的分组上，用户只访问
//     antigravity，却看到 anthropic 模型"这类跨平台信息泄漏；
//  4. 字段白名单：仅返回用户需要的字段（省略 BillingModelSource / RestrictModels
//     / 内部 ID / Status 等管理字段）。
type AvailableChannelHandler struct {
	channelService     *service.ChannelService
	apiKeyService      *service.APIKeyService
	settingService     *service.SettingService
	modelPricingModels modelPricingModelProvider
}

// NewAvailableChannelHandler 创建用户侧可用渠道 handler。
func NewAvailableChannelHandler(
	channelService *service.ChannelService,
	apiKeyService *service.APIKeyService,
	settingService *service.SettingService,
	gatewayService *service.GatewayService,
) *AvailableChannelHandler {
	return &AvailableChannelHandler{
		channelService:     channelService,
		apiKeyService:      apiKeyService,
		settingService:     settingService,
		modelPricingModels: gatewayService,
	}
}

type modelPricingModelProvider interface {
	GetAvailableModels(ctx context.Context, groupID *int64, platform string) []string
	GetAvailableModelPricingCandidates(ctx context.Context, groupID *int64, platform string, models []string) map[string][]string
}

// featureEnabled 返回当前查询目的对应的用户侧渠道聚合开关是否启用。
// 默认请求仍受 available-channels 控制；模型计费页通过 purpose=model_pricing
// 使用独立 model-pricing 开关，避免两个侧边栏入口互相绑定。
func (h *AvailableChannelHandler) featureEnabled(c *gin.Context) bool {
	if h.settingService == nil {
		return false
	}
	if c.Query("purpose") == "model_pricing" {
		return h.settingService.GetModelPricingRuntime(c.Request.Context()).Enabled
	}
	return h.settingService.GetAvailableChannelsRuntime(c.Request.Context()).Enabled
}

// userAvailableGroup 用户可见的分组概要（白名单字段）。
//
// 前端据此区分专属 vs 公开分组（IsExclusive）、订阅 vs 标准分组（SubscriptionType，
// 订阅视觉加深），并展示默认倍率与高峰倍率规则；用户专属倍率前端走
// /groups/rates，和 API 密钥页面保持一致。
type userAvailableGroup struct {
	ID                    int64   `json:"id"`
	Name                  string  `json:"name"`
	Platform              string  `json:"platform"`
	SubscriptionType      string  `json:"subscription_type"`
	RateMultiplier        float64 `json:"rate_multiplier"`
	PeakRateEnabled       bool    `json:"peak_rate_enabled"`
	PeakStart             string  `json:"peak_start"`
	PeakEnd               string  `json:"peak_end"`
	PeakRateMultiplier    float64 `json:"peak_rate_multiplier"`
	CurrentPeakMultiplier float64 `json:"current_peak_multiplier"`
	IsExclusive           bool    `json:"is_exclusive"`
}

// userSupportedModelPricing 用户可见的定价字段白名单。
type userSupportedModelPricing struct {
	BillingMode         string                   `json:"billing_mode"`
	PricingSource       string                   `json:"pricing_source"`
	PricingSourceLabel  string                   `json:"pricing_source_label"`
	PricingSourceDetail string                   `json:"pricing_source_detail,omitempty"`
	InputPrice          *float64                 `json:"input_price"`
	OutputPrice         *float64                 `json:"output_price"`
	CacheWritePrice     *float64                 `json:"cache_write_price"`
	CacheReadPrice      *float64                 `json:"cache_read_price"`
	ImageOutputPrice    *float64                 `json:"image_output_price"`
	PerRequestPrice     *float64                 `json:"per_request_price"`
	Intervals           []userPricingIntervalDTO `json:"intervals"`
	TimeBands           []userPricingTimeBandDTO `json:"time_bands"`
}

// userPricingTimeBandDTO 分时定价白名单。
type userPricingTimeBandDTO struct {
	Code            string   `json:"code"`
	TimeZone        string   `json:"time_zone"`
	TimeRanges      []string `json:"time_ranges"`
	InputPrice      *float64 `json:"input_price"`
	OutputPrice     *float64 `json:"output_price"`
	CacheWritePrice *float64 `json:"cache_write_price"`
	CacheReadPrice  *float64 `json:"cache_read_price"`
}

// userPricingIntervalDTO 定价区间白名单（去掉内部 ID、SortOrder 等前端不渲染的字段）。
type userPricingIntervalDTO struct {
	MinTokens       int      `json:"min_tokens"`
	MaxTokens       *int     `json:"max_tokens"`
	TierLabel       string   `json:"tier_label,omitempty"`
	InputPrice      *float64 `json:"input_price"`
	OutputPrice     *float64 `json:"output_price"`
	CacheWritePrice *float64 `json:"cache_write_price"`
	CacheReadPrice  *float64 `json:"cache_read_price"`
	PerRequestPrice *float64 `json:"per_request_price"`
}

// userSupportedModelUsageOffer 用户可见的官方 Usage 活动快照。
type userSupportedModelUsageOffer struct {
	Code            string  `json:"code"`
	UsageMultiplier float64 `json:"usage_multiplier"`
}

// userSupportedModel 用户可见的支持模型条目。
type userSupportedModel struct {
	Name                    string                        `json:"name"`
	Platform                string                        `json:"platform"`
	Pricing                 *userSupportedModelPricing    `json:"pricing"`
	ModelSpecificMultiplier *float64                      `json:"model_specific_multiplier,omitempty"`
	UsageOffer              *userSupportedModelUsageOffer `json:"usage_offer,omitempty"`
}

// userChannelPlatformSection 单渠道内某个平台的子视图：用户可见的分组 + 该平台
// 支持的模型。按 platform 聚合后让前端可以把渠道名作为 row-group 一次渲染，
// 后面的平台行按 sections 顺序铺开。
type userChannelPlatformSection struct {
	Platform        string               `json:"platform"`
	Groups          []userAvailableGroup `json:"groups"`
	SupportedModels []userSupportedModel `json:"supported_models"`
}

// userAvailableChannel 用户可见的渠道条目（白名单字段）。
//
// 每个渠道聚合为一条记录，内嵌 platforms 子数组：每个 section 对应一个平台，
// 包含该平台的 groups 和 supported_models。
type userAvailableChannel struct {
	Name        string                       `json:"name"`
	Description string                       `json:"description"`
	Platforms   []userChannelPlatformSection `json:"platforms"`
}

// List 列出当前用户可见的「可用渠道」。
// GET /api/v1/channels/available
func (h *AvailableChannelHandler) List(c *gin.Context) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}

	// Feature 未启用时返回空数组（不暴露渠道信息）。检查放在认证之后，
	// 保持与未开关前的 401 行为一致：未登录先 401，登录后再按开关决定。
	if !h.featureEnabled(c) {
		response.Success(c, []userAvailableChannel{})
		return
	}

	userGroups, err := h.apiKeyService.GetAvailableGroups(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	allowedGroupIDs := make(map[int64]struct{}, len(userGroups))
	for i := range userGroups {
		allowedGroupIDs[userGroups[i].ID] = struct{}{}
	}

	if c.Query("purpose") == "model_pricing" {
		response.Success(c, h.buildModelPricingChannels(
			c.Request.Context(),
			userGroups,
			h.modelPricingChannelMeta(c.Request.Context()),
		))
		return
	}

	channels, err := h.channelService.ListAvailable(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	out := make([]userAvailableChannel, 0, len(channels))
	for _, ch := range channels {
		if ch.Status != service.StatusActive {
			continue
		}
		visibleGroups := filterUserVisibleGroups(ch.Groups, allowedGroupIDs)
		if len(visibleGroups) == 0 {
			continue
		}
		sections := buildPlatformSections(ch, visibleGroups)
		if len(sections) == 0 {
			continue
		}
		out = append(out, userAvailableChannel{
			Name:        ch.Name,
			Description: ch.Description,
			Platforms:   sections,
		})
	}

	response.Success(c, out)
}

type modelPricingChannelMeta struct {
	name        string
	description string
}

func (h *AvailableChannelHandler) modelPricingChannelMeta(ctx context.Context) map[int64]modelPricingChannelMeta {
	if h == nil || h.channelService == nil {
		return nil
	}
	channels, err := h.channelService.ListAvailable(ctx)
	if err != nil {
		return nil
	}
	meta := make(map[int64]modelPricingChannelMeta)
	for _, ch := range channels {
		if ch.Status != service.StatusActive {
			continue
		}
		for _, group := range ch.Groups {
			meta[group.ID] = modelPricingChannelMeta{
				name:        ch.Name,
				description: ch.Description,
			}
		}
	}
	return meta
}

func (h *AvailableChannelHandler) buildModelPricingChannels(
	ctx context.Context,
	groups []service.Group,
	channelMeta map[int64]modelPricingChannelMeta,
) []userAvailableChannel {
	type bucket struct {
		name        string
		description string
		sections    []userChannelPlatformSection
	}

	buckets := make(map[string]*bucket, len(groups))
	pricingAt := time.Now()
	for i := range groups {
		group := groups[i]
		if strings.TrimSpace(group.Platform) == "" {
			continue
		}

		modelIDs := h.modelIDsForPricingGroup(ctx, &group)
		if len(modelIDs) == 0 {
			continue
		}
		supported := h.supportedModelsForPricingGroup(ctx, &group, modelIDs)
		if len(supported) == 0 {
			continue
		}

		key := "group:" + strings.ToLower(group.Name)
		name := group.Name
		description := group.Description
		if meta, ok := channelMeta[group.ID]; ok && strings.TrimSpace(meta.name) != "" {
			key = "channel:" + strings.ToLower(meta.name)
			name = meta.name
			description = meta.description
		}
		if strings.TrimSpace(name) == "" {
			name = group.Platform
		}

		b, ok := buckets[key]
		if !ok {
			b = &bucket{name: name, description: description}
			buckets[key] = b
		}
		b.sections = append(b.sections, userChannelPlatformSection{
			Platform:        group.Platform,
			Groups:          []userAvailableGroup{userAvailableGroupFromServiceAt(group, pricingAt)},
			SupportedModels: toUserSupportedModels(supported, map[string]struct{}{group.Platform: {}}),
		})
	}

	keys := make([]string, 0, len(buckets))
	for key := range buckets {
		keys = append(keys, key)
	}
	sort.SliceStable(keys, func(i, j int) bool {
		left := buckets[keys[i]]
		right := buckets[keys[j]]
		if strings.EqualFold(left.name, right.name) {
			return keys[i] < keys[j]
		}
		return strings.ToLower(left.name) < strings.ToLower(right.name)
	})

	out := make([]userAvailableChannel, 0, len(keys))
	for _, key := range keys {
		b := buckets[key]
		sort.SliceStable(b.sections, func(i, j int) bool {
			if b.sections[i].Platform != b.sections[j].Platform {
				return b.sections[i].Platform < b.sections[j].Platform
			}
			return strings.ToLower(b.sections[i].Groups[0].Name) < strings.ToLower(b.sections[j].Groups[0].Name)
		})
		out = append(out, userAvailableChannel{
			Name:        b.name,
			Description: b.description,
			Platforms:   b.sections,
		})
	}
	return out
}

func (h *AvailableChannelHandler) modelIDsForPricingGroup(ctx context.Context, group *service.Group) []string {
	if group == nil {
		return nil
	}
	var groupID *int64
	if group.ID > 0 {
		gid := group.ID
		groupID = &gid
	}

	var available []string
	if h != nil && h.modelPricingModels != nil {
		available = h.modelPricingModels.GetAvailableModels(ctx, groupID, group.Platform)
	}
	fallback := defaultModelIDsForPlatform(group.Platform)
	if group.CustomModelsListEnabled() {
		return filterModelsByCustomList(available, fallback, group.ModelsListConfig.Models)
	}
	if len(available) > 0 {
		return available
	}
	return fallback
}

func (h *AvailableChannelHandler) supportedModelsForPricingGroup(ctx context.Context, group *service.Group, modelIDs []string) []service.SupportedModel {
	if group == nil {
		return nil
	}
	var groupID *int64
	if group.ID > 0 {
		gid := group.ID
		groupID = &gid
	}

	candidates := map[string][]string{}
	if h != nil && h.modelPricingModels != nil {
		candidates = h.modelPricingModels.GetAvailableModelPricingCandidates(ctx, groupID, group.Platform, modelIDs)
	}

	out := make([]service.SupportedModel, 0, len(modelIDs))
	seen := make(map[string]struct{}, len(modelIDs))
	for _, modelID := range modelIDs {
		modelID = strings.TrimSpace(modelID)
		if modelID == "" || strings.Contains(modelID, "*") {
			continue
		}
		key := strings.ToLower(modelID)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}

		if h != nil && h.channelService != nil {
			out = append(out, h.channelService.BuildSupportedModelForPricingGroup(
				ctx,
				group.ID,
				modelID,
				group.Platform,
				candidates[modelID],
			))
			continue
		}
		out = append(out, service.SupportedModel{
			Name:          modelID,
			Platform:      group.Platform,
			PricingSource: service.PricingSourceMissing,
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// buildPlatformSections 把一个渠道按 visibleGroups 的平台集合拆成有序的 section 列表：
// 每个 section 对应一个平台，只包含该平台的 groups 和 supported_models。
// 输出按 platform 字母序稳定排序，便于前端等效比较与回归测试。
func buildPlatformSections(
	ch service.AvailableChannel,
	visibleGroups []userAvailableGroup,
) []userChannelPlatformSection {
	groupsByPlatform := make(map[string][]userAvailableGroup, 4)
	for _, g := range visibleGroups {
		if g.Platform == "" {
			continue
		}
		groupsByPlatform[g.Platform] = append(groupsByPlatform[g.Platform], g)
	}
	if len(groupsByPlatform) == 0 {
		return nil
	}

	platforms := make([]string, 0, len(groupsByPlatform))
	for p := range groupsByPlatform {
		platforms = append(platforms, p)
	}
	sort.Strings(platforms)

	sections := make([]userChannelPlatformSection, 0, len(platforms))
	for _, platform := range platforms {
		platformSet := map[string]struct{}{platform: {}}
		sections = append(sections, userChannelPlatformSection{
			Platform:        platform,
			Groups:          groupsByPlatform[platform],
			SupportedModels: toUserSupportedModels(ch.SupportedModels, platformSet),
		})
	}
	return sections
}

// filterUserVisibleGroups 仅保留用户可访问的分组。
func filterUserVisibleGroups(
	groups []service.AvailableGroupRef,
	allowed map[int64]struct{},
) []userAvailableGroup {
	visible := make([]userAvailableGroup, 0, len(groups))
	pricingAt := time.Now()
	for _, g := range groups {
		if _, ok := allowed[g.ID]; !ok {
			continue
		}
		visible = append(visible, userAvailableGroup{
			ID:                    g.ID,
			Name:                  g.Name,
			Platform:              g.Platform,
			SubscriptionType:      g.SubscriptionType,
			RateMultiplier:        g.RateMultiplier,
			PeakRateEnabled:       g.PeakRateEnabled,
			PeakStart:             g.PeakStart,
			PeakEnd:               g.PeakEnd,
			PeakRateMultiplier:    g.PeakRateMultiplier,
			CurrentPeakMultiplier: currentPeakMultiplierAt(g.SubscriptionType, g.PeakRateEnabled, g.PeakStart, g.PeakEnd, g.PeakRateMultiplier, pricingAt),
			IsExclusive:           g.IsExclusive,
		})
	}
	return visible
}

func userAvailableGroupFromServiceAt(g service.Group, pricingAt time.Time) userAvailableGroup {
	return userAvailableGroup{
		ID:                    g.ID,
		Name:                  g.Name,
		Platform:              g.Platform,
		SubscriptionType:      g.SubscriptionType,
		RateMultiplier:        g.RateMultiplier,
		PeakRateEnabled:       g.PeakRateEnabled,
		PeakStart:             g.PeakStart,
		PeakEnd:               g.PeakEnd,
		PeakRateMultiplier:    g.PeakRateMultiplier,
		CurrentPeakMultiplier: g.PeakMultiplierAt(pricingAt),
		IsExclusive:           g.IsExclusive,
	}
}

func currentPeakMultiplierAt(subscriptionType string, enabled bool, start, end string, multiplier float64, pricingAt time.Time) float64 {
	group := service.Group{
		SubscriptionType:   subscriptionType,
		PeakRateEnabled:    enabled,
		PeakStart:          start,
		PeakEnd:            end,
		PeakRateMultiplier: multiplier,
	}
	return group.PeakMultiplierAt(pricingAt)
}

// toUserSupportedModels 将 service 层支持模型转换为用户 DTO（字段白名单）。
// 仅保留平台在 allowedPlatforms 中的条目，防止跨平台模型信息泄漏。
// allowedPlatforms 为 nil 时不做平台过滤（保留全部，供测试或明确无过滤场景使用）。
func toUserSupportedModels(
	src []service.SupportedModel,
	allowedPlatforms map[string]struct{},
) []userSupportedModel {
	out := make([]userSupportedModel, 0, len(src))
	for i := range src {
		m := src[i]
		if allowedPlatforms != nil {
			if _, ok := allowedPlatforms[m.Platform]; !ok {
				continue
			}
		}
		pricing := toUserPricing(m.Pricing, m.PricingSource)
		pricing.TimeBands = toUserPricingTimeBands(m.PricingTimeBands)
		out = append(out, userSupportedModel{
			Name:                    m.Name,
			Platform:                m.Platform,
			Pricing:                 pricing,
			ModelSpecificMultiplier: toUserSupportedModelMultiplier(m.QuotaCost),
			UsageOffer:              toUserSupportedModelUsageOffer(m.UsageOffer),
		})
	}
	return out
}

func toUserSupportedModelMultiplier(quotaCost *service.ModelQuotaCost) *float64 {
	if quotaCost == nil || quotaCost.CostMultiplier <= 0 ||
		math.IsNaN(quotaCost.CostMultiplier) || math.IsInf(quotaCost.CostMultiplier, 0) {
		return nil
	}
	multiplier := quotaCost.CostMultiplier
	return &multiplier
}

func toUserSupportedModelUsageOffer(offer *service.ModelUsageOffer) *userSupportedModelUsageOffer {
	if offer == nil || strings.TrimSpace(offer.Code) == "" || offer.UsageMultiplier <= 1 ||
		math.IsNaN(offer.UsageMultiplier) || math.IsInf(offer.UsageMultiplier, 0) {
		return nil
	}
	return &userSupportedModelUsageOffer{
		Code:            offer.Code,
		UsageMultiplier: offer.UsageMultiplier,
	}
}

func toUserPricingTimeBands(src []service.ModelPricingTimeBand) []userPricingTimeBandDTO {
	out := make([]userPricingTimeBandDTO, 0, len(src))
	for _, band := range src {
		if band.Pricing == nil {
			continue
		}
		out = append(out, userPricingTimeBandDTO{
			Code:            band.Code,
			TimeZone:        band.TimeZone,
			TimeRanges:      append([]string(nil), band.TimeRanges...),
			InputPrice:      band.Pricing.InputPrice,
			OutputPrice:     band.Pricing.OutputPrice,
			CacheWritePrice: band.Pricing.CacheWritePrice,
			CacheReadPrice:  band.Pricing.CacheReadPrice,
		})
	}
	return out
}

// toUserPricing 将 service 层定价转换为用户 DTO；入参为 nil 时返回可检查的未配置对象。
func toUserPricing(p *service.ChannelModelPricing, pricingSource string) *userSupportedModelPricing {
	source, label, detail := userPricingSourceMeta(pricingSource)
	if p == nil {
		return &userSupportedModelPricing{
			BillingMode:         string(service.BillingModeToken),
			PricingSource:       source,
			PricingSourceLabel:  label,
			PricingSourceDetail: detail,
			Intervals:           []userPricingIntervalDTO{},
			TimeBands:           []userPricingTimeBandDTO{},
		}
	}
	intervals := make([]userPricingIntervalDTO, 0, len(p.Intervals))
	for _, iv := range p.Intervals {
		intervals = append(intervals, userPricingIntervalDTO{
			MinTokens:       iv.MinTokens,
			MaxTokens:       iv.MaxTokens,
			TierLabel:       iv.TierLabel,
			InputPrice:      iv.InputPrice,
			OutputPrice:     iv.OutputPrice,
			CacheWritePrice: iv.CacheWritePrice,
			CacheReadPrice:  iv.CacheReadPrice,
			PerRequestPrice: iv.PerRequestPrice,
		})
	}
	billingMode := string(p.BillingMode)
	if billingMode == "" {
		billingMode = string(service.BillingModeToken)
	}
	return &userSupportedModelPricing{
		BillingMode:         billingMode,
		PricingSource:       source,
		PricingSourceLabel:  label,
		PricingSourceDetail: detail,
		InputPrice:          p.InputPrice,
		OutputPrice:         p.OutputPrice,
		CacheWritePrice:     p.CacheWritePrice,
		CacheReadPrice:      p.CacheReadPrice,
		ImageOutputPrice:    p.ImageOutputPrice,
		PerRequestPrice:     p.PerRequestPrice,
		Intervals:           intervals,
		TimeBands:           []userPricingTimeBandDTO{},
	}
}

func userPricingSourceMeta(source string) (string, string, string) {
	switch source {
	case service.PricingSourceChannel:
		return service.PricingSourceChannel, "modelPricing.sources.channel", "channel_model_pricing"
	case service.PricingSourceCatalog:
		return service.PricingSourceCatalog, "modelPricing.sources.catalog", "pricing_catalog"
	case service.PricingSourceMissing:
		return service.PricingSourceMissing, "modelPricing.sources.missing", "pricing_missing"
	default:
		return service.PricingSourceMissing, "modelPricing.sources.missing", "pricing_missing"
	}
}

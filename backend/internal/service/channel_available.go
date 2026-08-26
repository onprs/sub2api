package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
)

// AvailableGroupRef 渠道视图中关联分组的简要信息。
//
// 用户侧「可用渠道」页面据此展示：专属分组 vs 公开分组（IsExclusive）、
// 订阅 vs 标准（SubscriptionType）、默认倍率（RateMultiplier）与高峰倍率规则。
// 用户专属倍率不在这里暴露，前端自己通过 /groups/rates 拉取，和 API 密钥页面保持一致。
type AvailableGroupRef struct {
	ID                 int64
	Name               string
	Platform           string
	SubscriptionType   string
	RateMultiplier     float64
	PeakRateEnabled    bool
	PeakStart          string
	PeakEnd            string
	PeakRateMultiplier float64
	IsExclusive        bool
}

// AvailableChannel 可用渠道视图：用于「可用渠道」页面展示渠道基础信息 +
// 关联的分组 + 推导出的支持模型列表（无通配符）。
type AvailableChannel struct {
	ID                 int64
	Name               string
	Description        string
	Status             string
	BillingModelSource string
	RestrictModels     bool
	Groups             []AvailableGroupRef
	SupportedModels    []SupportedModel
}

// ListAvailable 返回所有渠道的可用视图：每个渠道附带关联分组信息与支持模型列表。
//
// 支持模型通过 (*Channel).SupportedModels() 计算（mapping ∪ pricing 并联）。
// 对于渠道未配置定价的模型，进一步用 PricingService 的全局 LiteLLM 数据合成
// 一份展示用定价，让用户看到默认价格而非"未配置"。
//
// 关联分组信息通过 groupRepo.ListActive 查询后按 ID 映射；渠道 GroupIDs 中未在活跃列表中
// 的分组（已停用或删除）会被忽略。
//
// 前置条件：s.groupRepo 必须非 nil（由 wire DI 保证）。直接 nil-deref 用于 fail-fast，
// 避免静默掩盖注入缺失。
func (s *ChannelService) ListAvailable(ctx context.Context) ([]AvailableChannel, error) {
	channels, err := s.repo.ListAll(ctx)
	if err != nil {
		return nil, fmt.Errorf("list channels: %w", err)
	}

	groups, err := s.groupRepo.ListActive(ctx)
	if err != nil {
		return nil, fmt.Errorf("list active groups: %w", err)
	}
	groupByID := make(map[int64]AvailableGroupRef, len(groups))
	for i := range groups {
		g := groups[i]
		groupByID[g.ID] = AvailableGroupRef{
			ID:                 g.ID,
			Name:               g.Name,
			Platform:           g.Platform,
			SubscriptionType:   g.SubscriptionType,
			RateMultiplier:     g.RateMultiplier,
			PeakRateEnabled:    g.PeakRateEnabled,
			PeakStart:          g.PeakStart,
			PeakEnd:            g.PeakEnd,
			PeakRateMultiplier: g.PeakRateMultiplier,
			IsExclusive:        g.IsExclusive,
		}
	}

	out := make([]AvailableChannel, 0, len(channels))
	for i := range channels {
		ch := &channels[i]
		groups := make([]AvailableGroupRef, 0, len(ch.GroupIDs))
		for _, gid := range ch.GroupIDs {
			if ref, ok := groupByID[gid]; ok {
				groups = append(groups, ref)
			}
		}
		sort.SliceStable(groups, func(i, j int) bool { return groups[i].Name < groups[j].Name })

		ch.normalizeBillingModelSource()

		supported := ch.SupportedModels()
		s.fillGlobalPricingFallback(supported)
		s.fillModelQuotaCosts(supported)
		s.fillModelPricingTimeBands(supported)
		s.fillModelUsageOffers(supported)

		out = append(out, AvailableChannel{
			ID:                 ch.ID,
			Name:               ch.Name,
			Description:        ch.Description,
			Status:             ch.Status,
			BillingModelSource: ch.BillingModelSource,
			RestrictModels:     ch.RestrictModels,
			Groups:             groups,
			SupportedModels:    supported,
		})
	}

	sort.SliceStable(out, func(i, j int) bool {
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}

// fillGlobalPricingFallback 对未命中渠道定价的支持模型，从全局 LiteLLM 数据合成一份
// 展示用定价。仅用于「可用渠道」展示，不影响真实计费链路。
//
// 触发条件：
//  1. Pricing == nil（渠道完全没声明该模型的定价条目）
//  2. Pricing 非 nil 但所有价格字段为空（admin UI 建了条目但没填价格）
func (s *ChannelService) fillGlobalPricingFallback(models []SupportedModel) {
	for i := range models {
		s.fillCommandCodeMetadataForName(&models[i], models[i].Name)
		if !pricingNeedsFallback(models[i].Pricing) {
			if models[i].PricingSource == "" {
				models[i].PricingSource = PricingSourceChannel
			}
			continue
		}
		pricing, ok := s.displayPricingForModel(models[i].Platform, models[i].Name, models[i].Pricing)
		if !ok {
			models[i].PricingSource = PricingSourceMissing
			continue
		}
		models[i].Pricing = pricing
		models[i].PricingSource = PricingSourceCatalog
		s.fillCommandCodeMetadataForName(&models[i], models[i].Name)
	}
}

// BuildCatalogSupportedModel builds a display-only SupportedModel from the global
// pricing catalog. It never reads channel_model_pricing, so it is safe for the
// user-facing model-pricing page where the model list comes from group accounts.
func (s *ChannelService) BuildCatalogSupportedModel(displayName, platform string, pricingCandidates []string) SupportedModel {
	model := SupportedModel{
		Name:          strings.TrimSpace(displayName),
		Platform:      platform,
		PricingSource: PricingSourceMissing,
	}
	if model.Name == "" || s == nil {
		return model
	}

	for _, candidate := range catalogPricingLookupCandidates(platform, model.Name, pricingCandidates) {
		pricing, ok := s.displayPricingForModel(platform, candidate, nil)
		if !ok {
			continue
		}
		model.Pricing = pricing
		model.PricingSource = PricingSourceCatalog
		if !s.fillModelQuotaCostForName(&model, candidate) {
			model.Pricing = nil
			model.PricingSource = PricingSourceMissing
			model.QuotaCost = nil
			continue
		}
		s.fillModelPricingTimeBandsForName(&model, candidate, nil)
		s.fillModelUsageOfferForName(&model, candidate)
		s.fillCommandCodeMetadataForName(&model, candidate)
		return model
	}
	s.fillModelUsageOfferForName(&model, model.Name)
	return model
}

// BuildSupportedModelForPricingGroup 为用户价格页构建分组感知的模型价格。
// 渠道显式价格优先；未命中时再使用平台隔离的标准目录。
func (s *ChannelService) BuildSupportedModelForPricingGroup(
	ctx context.Context,
	groupID int64,
	displayName, platform string,
	pricingCandidates []string,
) SupportedModel {
	model := SupportedModel{
		Name:          strings.TrimSpace(displayName),
		Platform:      platform,
		PricingSource: PricingSourceMissing,
	}
	if model.Name == "" || s == nil {
		return model
	}

	for _, candidate := range catalogPricingLookupCandidates(platform, model.Name, pricingCandidates) {
		if groupID > 0 && s.channelPricingLookupAvailable() {
			pricing := s.GetChannelModelPricing(ctx, groupID, candidate)
			if !pricingNeedsFallback(pricing) {
				model.Pricing = s.displayResolvedChannelPricing(ctx, groupID, platform, candidate, pricing)
				model.PricingSource = PricingSourceChannel
				if !s.fillModelQuotaCostForName(&model, candidate) {
					model.Pricing = nil
					model.PricingSource = PricingSourceMissing
					model.QuotaCost = nil
					continue
				}
				s.fillModelPricingTimeBandsForName(&model, candidate, pricing)
				s.fillModelUsageOfferForName(&model, candidate)
				s.fillCommandCodeMetadataForName(&model, candidate)
				return model
			}
		}
		pricing, ok := s.displayPricingForModel(platform, candidate, nil)
		if !ok {
			continue
		}
		model.Pricing = pricing
		model.PricingSource = PricingSourceCatalog
		if !s.fillModelQuotaCostForName(&model, candidate) {
			model.Pricing = nil
			model.PricingSource = PricingSourceMissing
			model.QuotaCost = nil
			continue
		}
		s.fillModelPricingTimeBandsForName(&model, candidate, nil)
		s.fillModelUsageOfferForName(&model, candidate)
		s.fillCommandCodeMetadataForName(&model, candidate)
		return model
	}
	s.fillModelUsageOfferForName(&model, model.Name)
	return model
}

func (s *ChannelService) channelPricingLookupAvailable() bool {
	if s == nil {
		return false
	}
	if s.repo != nil {
		return true
	}
	cache, _ := s.cache.Load().(*channelCache)
	return cache != nil
}

func (s *ChannelService) displayResolvedChannelPricing(
	ctx context.Context,
	groupID int64,
	platform, model string,
	existing *ChannelModelPricing,
) *ChannelModelPricing {
	if s == nil || s.billingService == nil || existing == nil {
		return cloneChannelModelPricing(existing)
	}
	resolver := NewModelPricingResolver(s, s.billingService)
	resolved := resolver.Resolve(ctx, PricingInput{Model: model, GroupID: &groupID, Platform: platform})
	if resolved == nil || resolved.Source != PricingSourceChannel {
		return cloneChannelModelPricing(existing)
	}
	return synthesizePricingFromResolved(resolved, existing)
}

func cloneChannelModelPricing(pricing *ChannelModelPricing) *ChannelModelPricing {
	if pricing == nil {
		return nil
	}
	cloned := pricing.Clone()
	return &cloned
}

func synthesizePricingFromResolved(resolved *ResolvedPricing, existing *ChannelModelPricing) *ChannelModelPricing {
	if resolved == nil {
		return cloneChannelModelPricing(existing)
	}
	mode := resolved.Mode
	if mode == "" {
		mode = BillingModeToken
	}
	if mode == BillingModePerRequest || mode == BillingModeImage {
		pricing := cloneChannelModelPricing(existing)
		if pricing == nil {
			pricing = &ChannelModelPricing{}
		}
		pricing.BillingMode = mode
		return pricing
	}

	pricing := synthesizePricingFromModelPricing(resolved.BasePricing, existing)
	if pricing == nil {
		pricing = cloneChannelModelPricing(existing)
	}
	if pricing == nil {
		pricing = &ChannelModelPricing{}
	}
	pricing.BillingMode = BillingModeToken
	if len(resolved.Intervals) > 0 {
		pricing.Intervals = append([]PricingInterval(nil), resolved.Intervals...)
		if existing != nil && existing.ImageOutputPrice != nil {
			pricing.ImageOutputPrice = existing.ImageOutputPrice
		}
	} else {
		preserveExplicitDisplayPrices(pricing, existing)
	}
	return pricing
}

func preserveExplicitDisplayPrices(dst, src *ChannelModelPricing) {
	if dst == nil || src == nil {
		return
	}
	if src.InputPrice != nil {
		dst.InputPrice = src.InputPrice
	}
	if src.OutputPrice != nil {
		dst.OutputPrice = src.OutputPrice
	}
	if src.CacheWritePrice != nil {
		dst.CacheWritePrice = src.CacheWritePrice
	}
	if src.CacheReadPrice != nil {
		dst.CacheReadPrice = src.CacheReadPrice
	}
	if src.ImageOutputPrice != nil {
		dst.ImageOutputPrice = src.ImageOutputPrice
	}
}

func (s *ChannelService) displayPricingForModel(platform, model string, existing *ChannelModelPricing) (*ChannelModelPricing, bool) {
	return s.displayPricingForModelAt(platform, model, existing, time.Now())
}

func (s *ChannelService) displayPricingForModelAt(platform, model string, existing *ChannelModelPricing, now time.Time) (*ChannelModelPricing, bool) {
	if s == nil {
		return nil, false
	}
	if s.billingService != nil {
		if pricing, err := s.billingService.getModelPricingForPlatformAt(platform, model, now); err == nil && pricing != nil {
			return synthesizePricingFromModelPricing(pricing, existing), true
		}
	}
	if s.pricingService != nil {
		if isOpenCodeGoPricingPlatform(platform) {
			if lp := s.pricingService.GetOpenCodeGoModelPricingExact(model); lp != nil &&
				isOpenCodeGoPricingPlatform(lp.LiteLLMProvider) &&
				lp.OpenCodeGoPricingAuthority == openCodeGoPricingAuthorityOfficial {
				return synthesizePricingFromLiteLLM(openCodeGoPricingAt(lp, now), existing), true
			}
			if pricing, ok := openCodeGoReferencePricingAt(model, now); ok {
				return synthesizePricingFromModelPricing(pricing, existing), true
			}
		} else if lp := s.pricingService.GetModelPricing(model); lp != nil && !isOpenCodeGoPricingPlatform(lp.LiteLLMProvider) {
			return synthesizePricingFromLiteLLM(lp, existing), true
		}
	}
	return nil, false
}

const openCodeGoPricingTimeZone = "UTC"

var (
	openCodeGoOffPeakTimeRanges = []string{"00:00-01:00", "04:00-06:00", "10:00-24:00"}
	openCodeGoPeakTimeRanges    = []string{"01:00-04:00", "06:00-10:00"}
	openCodeGoOffPeakSampleTime = time.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC)
	openCodeGoPeakSampleTime    = time.Date(2000, time.January, 1, 1, 0, 0, 0, time.UTC)
)

func (s *ChannelService) fillModelUsageOffers(models []SupportedModel) {
	for i := range models {
		s.fillModelUsageOfferForName(&models[i], models[i].Name)
	}
}

func (s *ChannelService) fillModelQuotaCosts(models []SupportedModel) {
	for i := range models {
		if !isOpenCodeGoPricingPlatform(models[i].Platform) {
			continue
		}
		candidates := []string{models[i].Name}
		if models[i].Pricing != nil {
			candidates = append(candidates, models[i].Pricing.Models...)
		}
		resolved := false
		for _, candidate := range candidates {
			if s.fillModelQuotaCostForName(&models[i], candidate) {
				resolved = true
				break
			}
		}
		if !resolved {
			models[i].Pricing = nil
			models[i].PricingSource = PricingSourceMissing
		}
	}
}

func (s *ChannelService) fillModelQuotaCostForName(model *SupportedModel, pricingModel string) bool {
	if s == nil || model == nil {
		return false
	}
	billingService := s.billingService
	if billingService == nil {
		billingService = &BillingService{pricingService: s.pricingService}
	}
	var quotaCost OpenCodeGoQuotaCost
	var ok bool
	switch {
	case isOpenCodeGoPricingPlatform(model.Platform):
		quotaCost, ok = billingService.GetOpenCodeGoQuotaCost(pricingModel)
	case model.Platform == PlatformCommandCode:
		quotaCost, ok = billingService.GetCommandCodeQuotaCost(pricingModel)
		if !ok {
			// 免费模型无 credits 属于正常情况，返回成功但不设置倍率。
			pricing, pricingOK := commandCodeReferencePricingAt(pricingModel, time.Now())
			if pricingOK && pricing.AllowZeroRate {
				return true
			}
			return false
		}
	default:
		return true
	}
	if !ok {
		return false
	}
	model.QuotaCost = &ModelQuotaCost{
		IncludedMonthlyUsageUSD: quotaCost.IncludedMonthlyUsageUSD,
		CostMultiplier:          quotaCost.Multiplier,
	}
	return true
}

func (s *ChannelService) fillCommandCodeMetadataForName(model *SupportedModel, pricingModel string) {
	if model == nil || model.Platform != PlatformCommandCode {
		return
	}
	contextWindow, promotion, ok := commandCodeReferenceMetadataAt(pricingModel, time.Now())
	if !ok {
		return
	}
	model.ContextWindow = contextWindow
	if model.PricingSource == PricingSourceCatalog {
		model.Promotion = promotion
		if promotion != nil {
			// 官方促销（50% off / Free）在价格页“官方额度活动”列展示。
			model.UsageOffer = &ModelUsageOffer{
				Code:            promotion.Code,
				Label:           promotion.Label,
				UsageMultiplier: 1,
			}
		}
	}
}

func (s *ChannelService) fillModelUsageOfferForName(model *SupportedModel, pricingModel string) {
	if s == nil || model == nil || !isOpenCodeGoPricingPlatform(model.Platform) || s.pricingService == nil {
		return
	}
	multiplier := s.pricingService.OpenCodeGoUsageOfferMultiplier(pricingModel, time.Now())
	if multiplier <= 1 {
		return
	}
	model.UsageOffer = &ModelUsageOffer{
		Code:            "opencode_go_usage_offer",
		UsageMultiplier: multiplier,
	}
}

func (s *ChannelService) fillModelPricingTimeBands(models []SupportedModel) {
	for i := range models {
		var existing *ChannelModelPricing
		if models[i].PricingSource == PricingSourceChannel {
			existing = models[i].Pricing
		}
		s.fillModelPricingTimeBandsForName(&models[i], models[i].Name, existing)
	}
}

func (s *ChannelService) fillModelPricingTimeBandsForName(model *SupportedModel, pricingModel string, existing *ChannelModelPricing) {
	if s == nil || model == nil || model.Pricing == nil || (!isOpenCodeGoPricingPlatform(model.Platform) && model.Platform != PlatformCommandCode) {
		return
	}
	if existing != nil && (existing.BillingMode == BillingModePerRequest || existing.BillingMode == BillingModeImage || len(existing.Intervals) > 0) {
		return
	}
	if model.Platform == PlatformCommandCode {
		bands := commandCodeReferencePricingTimeBandsAt(pricingModel, time.Now())
		if len(bands) == 0 {
			return
		}
		if existing != nil {
			for index := range bands {
				preserveExplicitDisplayPrices(bands[index].Pricing, existing)
			}
		}
		model.PricingTimeBands = bands
		return
	}
	offPeak, offPeakOK := s.displayPricingForModelAt(model.Platform, pricingModel, nil, openCodeGoOffPeakSampleTime)
	peak, peakOK := s.displayPricingForModelAt(model.Platform, pricingModel, nil, openCodeGoPeakSampleTime)
	if !offPeakOK || !peakOK {
		return
	}
	if existing != nil {
		preserveExplicitDisplayPrices(offPeak, existing)
		preserveExplicitDisplayPrices(peak, existing)
	}
	if channelPricingValuesEqual(offPeak, peak) {
		return
	}
	model.PricingTimeBands = []ModelPricingTimeBand{
		{
			Code:       "off_peak",
			TimeZone:   openCodeGoPricingTimeZone,
			TimeRanges: append([]string(nil), openCodeGoOffPeakTimeRanges...),
			Pricing:    offPeak,
		},
		{
			Code:       "peak",
			TimeZone:   openCodeGoPricingTimeZone,
			TimeRanges: append([]string(nil), openCodeGoPeakTimeRanges...),
			Pricing:    peak,
		},
	}
}

func channelPricingValuesEqual(left, right *ChannelModelPricing) bool {
	if left == nil || right == nil {
		return left == right
	}
	return optionalPriceEqual(left.InputPrice, right.InputPrice) &&
		optionalPriceEqual(left.OutputPrice, right.OutputPrice) &&
		optionalPriceEqual(left.CacheWritePrice, right.CacheWritePrice) &&
		optionalPriceEqual(left.CacheReadPrice, right.CacheReadPrice) &&
		optionalPriceEqual(left.ImageOutputPrice, right.ImageOutputPrice) &&
		optionalPriceEqual(left.PerRequestPrice, right.PerRequestPrice)
}

func optionalPriceEqual(left, right *float64) bool {
	if left == nil || right == nil {
		return left == right
	}
	return *left == *right
}

func catalogPricingLookupCandidates(platform, displayName string, pricingCandidates []string) []string {
	seen := make(map[string]struct{}, len(pricingCandidates)+4)
	out := make([]string, 0, len(pricingCandidates)+4)
	add := func(candidate string) {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" || strings.Contains(candidate, "*") {
			return
		}
		key := strings.ToLower(candidate)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, candidate)
	}

	for _, candidate := range pricingCandidates {
		add(candidate)
	}
	if platform == PlatformAntigravity {
		add(domain.DefaultAntigravityModelMapping[displayName])
	}
	if platform == PlatformOpenCodeGo && strings.HasSuffix(displayName, "-code") {
		add(strings.TrimSuffix(displayName, "-code"))
	}
	add(displayName)
	return out
}

// pricingNeedsFallback 判定一个 ChannelModelPricing 是否需要走全局回落。
// 价格全部缺失（无 flat 字段且无任何带价 interval）即视为未配置。
func pricingNeedsFallback(p *ChannelModelPricing) bool {
	if p == nil {
		return true
	}
	if p.InputPrice != nil || p.OutputPrice != nil ||
		p.CacheWritePrice != nil || p.CacheReadPrice != nil ||
		p.ImageOutputPrice != nil || p.PerRequestPrice != nil {
		return false
	}
	for _, iv := range p.Intervals {
		if iv.InputPrice != nil || iv.OutputPrice != nil ||
			iv.CacheWritePrice != nil || iv.CacheReadPrice != nil ||
			iv.PerRequestPrice != nil {
			return false
		}
	}
	return true
}

// synthesizePricingFromLiteLLM 把 LiteLLM 的定价数据转成 ChannelModelPricing 形态，
// 仅用于展示。
//
// 计费模式优先级：
//  1. 渠道已选 BillingMode（admin 在 UI 里选了 image / per_request 但没填价的场景，
//     按选定模式合成对应字段）
//  2. LiteLLM mode="image_generation" → image
//  3. 默认 token
//
// LiteLLM 中字段 0 视为未配置，不带入展示。
func synthesizePricingFromLiteLLM(lp *LiteLLMModelPricing, existing *ChannelModelPricing) *ChannelModelPricing {
	if lp == nil {
		return existing
	}

	mode := BillingModeToken
	switch {
	case existing != nil && existing.BillingMode != "":
		mode = existing.BillingMode
	case lp.Mode == "image_generation":
		mode = BillingModeImage
	}

	if mode == BillingModeImage || mode == BillingModePerRequest {
		return &ChannelModelPricing{
			BillingMode:      mode,
			PerRequestPrice:  nonZeroPtr(lp.OutputCostPerImage),
			ImageOutputPrice: nonZeroPtr(lp.OutputCostPerImageToken),
			InputPrice:       nonZeroPtr(lp.InputCostPerToken),
			OutputPrice:      nonZeroPtr(lp.OutputCostPerToken),
		}
	}
	return &ChannelModelPricing{
		BillingMode:      mode,
		InputPrice:       nonZeroPtr(lp.InputCostPerToken),
		OutputPrice:      nonZeroPtr(lp.OutputCostPerToken),
		CacheWritePrice:  nonZeroPtr(lp.CacheCreationInputTokenCost),
		CacheReadPrice:   nonZeroPtr(lp.CacheReadInputTokenCost),
		ImageOutputPrice: nonZeroPtr(lp.OutputCostPerImageToken),
	}
}

func synthesizePricingFromModelPricing(mp *ModelPricing, existing *ChannelModelPricing) *ChannelModelPricing {
	if mp == nil {
		return existing
	}
	mode := BillingModeToken
	if existing != nil && existing.BillingMode != "" {
		mode = existing.BillingMode
	}
	pricing := &ChannelModelPricing{
		BillingMode:      mode,
		InputPrice:       nonZeroPtr(mp.InputPricePerToken),
		OutputPrice:      nonZeroPtr(mp.OutputPricePerToken),
		CacheWritePrice:  nonZeroPtr(mp.CacheCreationPricePerToken),
		CacheReadPrice:   nonZeroPtr(mp.CacheReadPricePerToken),
		ImageOutputPrice: nonZeroPtr(mp.ImageOutputPricePerToken),
	}
	if mp.AllowZeroRate {
		zero := float64(0)
		if mp.InputPricePerToken == 0 {
			pricing.InputPrice = &zero
		}
		if mp.OutputPricePerToken == 0 {
			pricing.OutputPrice = &zero
		}
	}
	if mode == BillingModeToken {
		if len(mp.Intervals) > 0 {
			pricing.Intervals = append([]PricingInterval(nil), mp.Intervals...)
		} else {
			pricing.Intervals = longContextDisplayIntervals(mp)
		}
	}
	return pricing
}

func longContextDisplayIntervals(mp *ModelPricing) []PricingInterval {
	if mp == nil || mp.LongContextInputThreshold <= 0 {
		return nil
	}
	inputMultiplier := mp.LongContextInputMultiplier
	if inputMultiplier <= 0 {
		inputMultiplier = 1
	}
	outputMultiplier := mp.LongContextOutputMultiplier
	if outputMultiplier <= 0 {
		outputMultiplier = 1
	}
	if inputMultiplier == 1 && outputMultiplier == 1 {
		return nil
	}

	threshold := mp.LongContextInputThreshold
	return []PricingInterval{
		{
			MinTokens:       0,
			MaxTokens:       &threshold,
			InputPrice:      nonZeroPtr(mp.InputPricePerToken),
			OutputPrice:     nonZeroPtr(mp.OutputPricePerToken),
			CacheWritePrice: nonZeroPtr(mp.CacheCreationPricePerToken),
			CacheReadPrice:  nonZeroPtr(mp.CacheReadPricePerToken),
		},
		{
			MinTokens:       threshold,
			InputPrice:      nonZeroPtr(mp.InputPricePerToken * inputMultiplier),
			OutputPrice:     nonZeroPtr(mp.OutputPricePerToken * outputMultiplier),
			CacheWritePrice: nonZeroPtr(mp.CacheCreationPricePerToken * inputMultiplier),
			CacheReadPrice:  nonZeroPtr(mp.CacheReadPricePerToken * inputMultiplier),
		},
	}
}

func nonZeroPtr(v float64) *float64 {
	if v == 0 {
		return nil
	}
	return &v
}

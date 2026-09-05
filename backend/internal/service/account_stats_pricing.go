package service

import (
	"context"
	"strings"
)

// resolveAccountStatsCost 计算账号统计定价费用。
// 返回 nil 表示不覆盖，使用默认公式（total_cost × account_rate_multiplier）。
//
// 优先级（先命中为准）：
//  1. 自定义规则（始终尝试，不依赖 ApplyPricingToAccountStats 开关）
//  2. ApplyPricingToAccountStats 启用时，直接使用调用方提供的账号基础成本
//     （不含用户组倍率；可包含平台要求的活动折算后模型倍率）
//  3. 模型定价文件（LiteLLM）中上游模型的默认价格
//  4. nil → 走默认公式（total_cost × account_rate_multiplier）
//
// upstreamModel 是最终发往上游的模型 ID。
// accountBaseCost 是调用方预计算的账号基础成本，不包含用户组倍率；
// OpenCode Go 调用方会在这里保留活动折算后的模型倍率。
// serviceTier 是最终参与用户计费的 OpenAI 服务层级，用于优先级 3。
// reasoningEffort 是最终转发等级；Fable 5.1 max 默认按 3 倍额度消耗。
func resolveAccountStatsCost(
	ctx context.Context,
	channelService *ChannelService,
	billingService *BillingService,
	accountID int64,
	groupID int64,
	upstreamModel string,
	tokens UsageTokens,
	requestCount int,
	accountBaseCost float64,
	serviceTier string,
	reasoningEfforts ...string,
) *float64 {
	reasoningEffort := ""
	if len(reasoningEfforts) > 0 {
		reasoningEffort = reasoningEfforts[0]
	}
	if channelService == nil || upstreamModel == "" {
		return nil
	}
	channel, err := channelService.GetChannelForGroup(ctx, groupID)
	if err != nil || channel == nil {
		return nil
	}

	platform := channelService.GetGroupPlatform(ctx, groupID)

	// 优先级 1：自定义规则（始终尝试）
	if cost := tryCustomRules(channel, accountID, groupID, platform, upstreamModel, tokens, requestCount, reasoningEffort); cost != nil {
		return applyOpenCodeGoQuotaCostToAccountStats(billingService, platform, upstreamModel, cost)
	}

	// 优先级 2：渠道开启"应用模型定价到账号统计"时，直接使用账号基础成本
	if channel.ApplyPricingToAccountStats {
		cost := accountBaseCost
		if cost <= 0 {
			return nil
		}
		return &cost
	}

	// 优先级 3：模型定价文件（LiteLLM）默认价格
	if billingService != nil {
		return tryModelFilePricingForPlatform(billingService, platform, upstreamModel, tokens, serviceTier, reasoningEffort)
	}

	return nil
}

// tryModelFilePricingForPlatform 使用平台限定的模型定价文件（LiteLLM/fallback）标准价计算费用。
// 与用户计费共用同一条定价管线，避免新增定价特性时维护第二份计算实现。
// 平台参数只限定模型目录，不引入渠道自定义定价。
func tryModelFilePricingForPlatform(billingService *BillingService, platform, model string, tokens UsageTokens, serviceTier string, reasoningEfforts ...string) *float64 {
	reasoningEffort := ""
	if len(reasoningEfforts) > 0 {
		reasoningEffort = reasoningEfforts[0]
	}
	normalizedTier := normalizeBillingServiceTier(serviceTier)
	breakdown, err := billingService.calculateCostForPlatformWithServiceTier(platform, model, tokens, 1, normalizedTier)
	if err != nil || breakdown == nil || breakdown.TotalCost <= 0 {
		return nil
	}
	applyCostBreakdownMultiplier(breakdown, maxReasoningEffortBillingMultiplier(model, reasoningEffort, nil))
	return applyOpenCodeGoQuotaCostToAccountStats(billingService, platform, model, &breakdown.TotalCost)
}

func applyOpenCodeGoQuotaCostToAccountStats(billingService *BillingService, platform, model string, cost *float64) *float64 {
	if cost == nil || !isOpenCodeGoPricingPlatform(platform) {
		return cost
	}
	if billingService == nil {
		return nil
	}
	quotaCost, ok := billingService.GetOpenCodeGoQuotaCost(model)
	if !ok {
		return nil
	}
	weightedCost := *cost * quotaCost.Multiplier
	return &weightedCost
}

// tryCustomRules 遍历自定义规则，按数组顺序先命中为准。
func tryCustomRules(
	channel *Channel, accountID, groupID int64,
	platform, model string, tokens UsageTokens, requestCount int,
	reasoningEfforts ...string,
) *float64 {
	reasoningEffort := ""
	if len(reasoningEfforts) > 0 {
		reasoningEffort = reasoningEfforts[0]
	}
	modelLower := strings.ToLower(model)
	for _, rule := range channel.AccountStatsPricingRules {
		if !matchAccountStatsRule(&rule, accountID, groupID) {
			continue
		}
		pricing := findPricingForModelCandidates(rule.Pricing, platform, modelLower)
		if pricing == nil {
			continue // 规则匹配但模型不在规则定价中，继续下一条
		}
		cost := calculateStatsCost(pricing, tokens, requestCount)
		if cost != nil {
			*cost *= maxReasoningEffortBillingMultiplier(model, reasoningEffort, nil)
		}
		return cost
	}
	return nil
}

func findPricingForModelCandidates(pricingList []ChannelModelPricing, platform, modelLower string) *ChannelModelPricing {
	for _, candidate := range billingModelPricingCandidates(modelLower) {
		if pricing := findPricingForModel(pricingList, platform, candidate); pricing != nil {
			return pricing
		}
	}
	return nil
}

// matchAccountStatsRule 检查规则是否匹配指定的 accountID 和 groupID。
// 匹配条件：accountID ∈ rule.AccountIDs 或 groupID ∈ rule.GroupIDs。
// 如果规则的 AccountIDs 和 GroupIDs 都为空，视为不匹配。
func matchAccountStatsRule(rule *AccountStatsPricingRule, accountID, groupID int64) bool {
	if len(rule.AccountIDs) == 0 && len(rule.GroupIDs) == 0 {
		return false
	}
	for _, id := range rule.AccountIDs {
		if id == accountID {
			return true
		}
	}
	for _, id := range rule.GroupIDs {
		if id == groupID {
			return true
		}
	}
	return false
}

// findPricingForModel 在定价列表中查找匹配的模型定价。
// 先精确匹配，再通配符匹配（按配置顺序，先匹配先使用）。
func findPricingForModel(pricingList []ChannelModelPricing, platform, modelLower string) *ChannelModelPricing {
	// 精确匹配优先
	for i := range pricingList {
		p := &pricingList[i]
		if !isPlatformMatch(platform, p.Platform) {
			continue
		}
		for _, m := range p.Models {
			if strings.ToLower(m) == modelLower {
				return p
			}
		}
	}
	// 通配符匹配：按配置顺序，先匹配先使用
	for i := range pricingList {
		p := &pricingList[i]
		if !isPlatformMatch(platform, p.Platform) {
			continue
		}
		for _, m := range p.Models {
			ml := strings.ToLower(m)
			if !strings.HasSuffix(ml, "*") {
				continue
			}
			prefix := strings.TrimSuffix(ml, "*")
			if strings.HasPrefix(modelLower, prefix) {
				return p
			}
		}
	}
	return nil
}

// isPlatformMatch 判断平台是否匹配（空平台视为不限平台）。
func isPlatformMatch(queryPlatform, pricingPlatform string) bool {
	if queryPlatform == "" || pricingPlatform == "" {
		return true
	}
	return queryPlatform == pricingPlatform
}

// calculateStatsCost 使用给定的定价计算费用（不含任何倍率，原始费用）。
func calculateStatsCost(pricing *ChannelModelPricing, tokens UsageTokens, requestCount int) *float64 {
	if pricing == nil {
		return nil
	}
	switch pricing.BillingMode {
	case BillingModePerRequest, BillingModeImage:
		return calculatePerRequestStatsCost(pricing, requestCount)
	default:
		return calculateTokenStatsCost(pricing, tokens)
	}
}

// calculatePerRequestStatsCost 按次/图片计费。
func calculatePerRequestStatsCost(pricing *ChannelModelPricing, requestCount int) *float64 {
	if pricing.PerRequestPrice == nil || *pricing.PerRequestPrice <= 0 {
		return nil
	}
	cost := *pricing.PerRequestPrice * float64(requestCount)
	return &cost
}

// calculateTokenStatsCost Token 计费。
// If the pricing has intervals, find the matching interval by total token count
// and use its prices instead of the flat pricing fields.
func calculateTokenStatsCost(pricing *ChannelModelPricing, tokens UsageTokens) *float64 {
	p := pricing
	if len(pricing.Intervals) > 0 {
		totalTokens := tokens.InputTokens + tokens.OutputTokens + tokens.CacheCreationTokens + tokens.CacheReadTokens
		if iv := FindMatchingInterval(pricing.Intervals, totalTokens); iv != nil {
			p = &ChannelModelPricing{
				InputPrice:        iv.InputPrice,
				OutputPrice:       iv.OutputPrice,
				CacheWritePrice:   iv.CacheWritePrice,
				CacheWrite1hPrice: iv.CacheWrite1hPrice,
				CacheReadPrice:    iv.CacheReadPrice,
				PerRequestPrice:   iv.PerRequestPrice,
			}
		}
	}
	deref := func(ptr *float64) float64 {
		if ptr == nil {
			return 0
		}
		return *ptr
	}
	cacheCreationCost := float64(tokens.CacheCreationTokens) * deref(p.CacheWritePrice)
	if p.CacheWrite1hPrice != nil {
		cache5m, cache1h := normalizeCacheCreationBreakdown(tokens)
		if cache5m > 0 || cache1h > 0 {
			cacheCreationCost = float64(cache5m)*deref(p.CacheWritePrice) +
				float64(cache1h)*deref(p.CacheWrite1hPrice)
		}
	}
	cost := float64(tokens.InputTokens)*deref(p.InputPrice) +
		float64(tokens.OutputTokens)*deref(p.OutputPrice) +
		cacheCreationCost +
		float64(tokens.CacheReadTokens)*deref(p.CacheReadPrice) +
		float64(tokens.ImageOutputTokens)*deref(p.ImageOutputPrice)
	if cost <= 0 {
		return nil
	}
	return &cost
}

// applyAccountStatsCost resolves the account stats cost for a usage log entry.
// It resolves the upstream model (falling back to the requested model) and calls
// the 4-level priority chain via resolveAccountStatsCost.
func applyAccountStatsCost(
	ctx context.Context,
	usageLog *UsageLog,
	cs *ChannelService, bs *BillingService,
	accountID int64, groupID int64,
	upstreamModel, requestedModel string,
	tokens UsageTokens,
	accountBaseCost float64,
) {
	model := upstreamModel
	if model == "" {
		model = requestedModel
	}
	requestCount := 1
	if usageLog != nil && usageLog.ImageCount > 0 {
		requestCount = usageLog.ImageCount
	}
	serviceTier := ""
	reasoningEffort := ""
	if usageLog != nil && usageLog.ServiceTier != nil {
		serviceTier = *usageLog.ServiceTier
	}
	if usageLog != nil && usageLog.ReasoningEffort != nil {
		reasoningEffort = *usageLog.ReasoningEffort
	}
	usageLog.AccountStatsCost = resolveAccountStatsCost(
		ctx, cs, bs, accountID, groupID, model, tokens, requestCount, accountBaseCost, serviceTier, reasoningEffort,
	)
}

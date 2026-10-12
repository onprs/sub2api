package service

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"
)

// CommandCodeChannelModelIDs 返回关联渠道声明的模型；空列表仍表示渠道已接管目录。
func (s *ChannelService) CommandCodeChannelModelIDs(ctx context.Context, groupID int64) ([]string, bool) {
	lookup, err := s.lookupGroupChannel(ctx, groupID)
	if err != nil {
		slog.Warn("加载 Command Code 渠道模型失败", "group_id", groupID, "error", err)
		return []string{}, true
	}
	if lookup == nil {
		return nil, false
	}
	channel := lookup.channel
	models := make([]string, 0)
	resolver := NewModelPricingResolver(s, s.billingService)
	for _, model := range channel.SupportedModels() {
		if model.Platform != PlatformCommandCode || model.Pricing == nil {
			continue
		}
		if channel.BillingModelSource == BillingModelSourceRequested && resolver.resolveChannelModelPricing(ctx, groupID, model.Name) == nil {
			continue
		}
		models = append(models, model.Name)
	}
	sort.Strings(models)
	return models, true
}

func commandCodeChannelPricingIsFree(pricing *ChannelModelPricing) bool {
	if pricing == nil || (pricing.BillingMode != "" && pricing.BillingMode != BillingModeToken) ||
		pricing.InputPrice == nil || pricing.OutputPrice == nil ||
		*pricing.InputPrice != 0 || *pricing.OutputPrice != 0 {
		return false
	}
	for _, price := range []*float64{pricing.CacheWritePrice, pricing.CacheWrite1hPrice, pricing.CacheReadPrice, pricing.ImageInputPrice, pricing.ImageOutputPrice} {
		if price != nil && *price != 0 {
			return false
		}
	}
	for _, interval := range pricing.Intervals {
		for _, price := range []*float64{interval.InputPrice, interval.OutputPrice, interval.CacheWritePrice, interval.CacheWrite1hPrice, interval.CacheReadPrice} {
			if price != nil && *price != 0 {
				return false
			}
		}
	}
	return true
}

func (s *ChannelService) buildCommandCodeChannelSupportedModel(ctx context.Context, groupID int64, name string, candidates []string) (SupportedModel, bool) {
	model := SupportedModel{Name: strings.TrimSpace(name), Platform: PlatformCommandCode, PricingSource: PricingSourceMissing}
	lookup, err := s.lookupGroupChannel(ctx, groupID)
	if err != nil {
		return model, true
	}
	if lookup == nil {
		return model, false
	}
	channel := lookup.channel
	pricingModel := model.Name
	if channel.BillingModelSource == BillingModelSourceChannelMapped {
		pricingModel = s.ResolveChannelMapping(ctx, groupID, pricingModel).MappedModel
	}
	lookupModels := []string{pricingModel}
	if channel.BillingModelSource == BillingModelSourceUpstream {
		lookupModels = append(append([]string{}, candidates...), pricingModel)
	}
	resolver := NewModelPricingResolver(s, s.billingService)
	for _, candidate := range lookupModels {
		pricing := resolver.resolveChannelModelPricing(ctx, groupID, candidate)
		if pricing == nil || pricingNeedsFallback(pricing) {
			continue
		}
		model.Pricing = commandCodeChannelDisplayPricingAt(pricing, time.Now())
		model.PricingSource = PricingSourceChannel
		if !s.fillModelQuotaCostForName(&model, candidate) {
			model.Pricing = nil
			model.PricingSource = PricingSourceMissing
			return model, true
		}
		s.fillCommandCodeMetadataForName(&model, candidate)
		s.fillModelCapability(ctx, &model)
		s.fillModelPricingTimeBandsForName(&model, candidate, pricing)
		return model, true
	}
	return model, true
}

func commandCodeChannelDisplayPricingAt(pricing *ChannelModelPricing, at time.Time) *ChannelModelPricing {
	cloned := pricing.Clone()
	if cloned.BillingMode == "" {
		cloned.BillingMode = BillingModeToken
	}
	if cloned.BillingMode != BillingModeToken {
		return &cloned
	}
	effective := func(price, base, multiplier *float64) *float64 {
		if price != nil || base == nil {
			return price
		}
		value := *base
		if multiplier != nil {
			value *= *multiplier
		}
		return &value
	}
	for i := range cloned.Intervals {
		interval := &cloned.Intervals[i]
		interval.InputPrice = effective(interval.InputPrice, cloned.InputPrice, interval.InputMultiplier)
		interval.OutputPrice = effective(interval.OutputPrice, cloned.OutputPrice, interval.OutputMultiplier)
		interval.CacheWritePrice = effective(interval.CacheWritePrice, cloned.CacheWritePrice, interval.CacheWriteMultiplier)
		interval.CacheReadPrice = effective(interval.CacheReadPrice, cloned.CacheReadPrice, interval.CacheReadMultiplier)
	}
	// 同时配置阶梯和分时时，展示当前时段的完整阶梯价格。
	if len(cloned.Intervals) > 0 {
		multiplier := cloned.TimePricing.MultiplierAt(at)
		for i := range cloned.Intervals {
			interval := &cloned.Intervals[i]
			for _, price := range []**float64{&interval.InputPrice, &interval.OutputPrice, &interval.CacheWritePrice, &interval.CacheReadPrice} {
				if *price != nil {
					value := **price * multiplier
					*price = &value
				}
			}
		}
	}
	return &cloned
}

// 渠道分时展示直接展开同一份倍率配置，覆盖全天并保留周末的基础价格。
func commandCodeChannelPricingTimeBandsAt(pricing *ChannelModelPricing, at time.Time) []ModelPricingTimeBand {
	if pricing == nil || pricing.TimePricing == nil || len(pricing.TimePricing.Periods) == 0 || len(pricing.Intervals) > 0 {
		return nil
	}
	config := pricing.TimePricing
	location, err := loadChannelTimePricingLocation(config.Timezone)
	if err != nil {
		return nil
	}
	periods, err := parseChannelTimePeriods(config.Periods)
	if err != nil {
		return nil
	}
	local := at.In(location)
	if config.WeekdaysOnly && (local.Weekday() == time.Saturday || local.Weekday() == time.Sunday) {
		periods = nil
	}
	bands := make([]ModelPricingTimeBand, 0, len(periods)*2+1)
	byMultiplier := make(map[float64]int)
	lowestMultiplier := 1.0
	for _, period := range periods {
		lowestMultiplier = min(lowestMultiplier, period.multiplier)
	}
	add := func(start, end int, multiplier float64) {
		if start == end {
			return
		}
		code := "off_peak"
		if multiplier != lowestMultiplier {
			code = "peak"
		}
		cloned := pricing.Clone()
		for _, price := range []**float64{&cloned.InputPrice, &cloned.OutputPrice, &cloned.CacheWritePrice, &cloned.CacheReadPrice} {
			if *price != nil {
				value := **price * multiplier
				*price = &value
			}
		}
		format := func(second int) string {
			return fmt.Sprintf("%02d:%02d:%02d", second/3600, second/60%60, second%60)
		}
		timeRange := format(start) + "-" + format(end)
		if index, ok := byMultiplier[multiplier]; ok {
			bands[index].TimeRanges = append(bands[index].TimeRanges, timeRange)
			return
		}
		byMultiplier[multiplier] = len(bands)
		bands = append(bands, ModelPricingTimeBand{
			Code: code, TimeZone: config.Timezone,
			TimeRanges: []string{timeRange}, Pricing: &cloned,
		})
	}
	previous := 0
	for _, period := range periods {
		add(previous, period.start, 1)
		add(period.start, period.end, period.multiplier)
		previous = period.end
	}
	add(previous, 24*3600, 1)
	return bands
}

// applyCommandCodeChannelQuotaCostAt 使用同一次解析的渠道价格和月额度规则。
func applyCommandCodeChannelQuotaCostAt(cost *CostBreakdown, pricing *ChannelModelPricing, at time.Time) error {
	if cost == nil || pricing == nil {
		return nil
	}
	if at.IsZero() {
		at = time.Now()
	}
	multiplier := 1.0
	if !cost.AllowZeroRate && pricing.MonthlyQuota != nil {
		quota, ok := pricing.MonthlyQuota.ResolveAt(at, commandCodeSharedMonthlyQuotaUSD)
		if !ok {
			return quotaCostUnavailableError(strings.Join(pricing.Models, ", "))
		}
		multiplier = quota.CostMultiplier
	}
	applyModelSpecificMultiplierToCost(cost, multiplier)
	cost.commandCodeQuotaApplied = true
	return nil
}

package service

import (
	"context"
	_ "embed"
	"encoding/json"
	"slices"
	"sort"
	"strings"
	"time"
)

const openCodeSharedMonthlyQuotaUSD = 60.0

// 官方 Go 定价快照，只收录同时公布了模型 ID 和价格的模型。
// 来源：https://opencode.ai/docs/go/（2026-10-14）。
// 快照用于初始化渠道和无渠道时的回退；渠道建立后以管理员保存的配置为准。
//
//go:embed opencode_channel_pricing.json
var openCodeChannelPricingJSON []byte

var openCodeOfficialPricing = func() []ChannelModelPricing {
	var pricing []ChannelModelPricing
	if err := json.Unmarshal(openCodeChannelPricingJSON, &pricing); err != nil {
		panic(err)
	}
	return pricing
}()

func openCodeReferencePricing(model string) (*ChannelModelPricing, bool) {
	model = normalizeOpenCodeGoModelID(model)
	for _, entry := range openCodeOfficialPricing {
		if strings.EqualFold(entry.Models[0], model) {
			cloned := entry.Clone()
			return &cloned, true
		}
	}
	return nil, false
}

// Zen 目录同样只保留官方有定价且当前网关支持原生协议的模型。
// 来源：https://opencode.ai/docs/zen/（2026-10-14）。
//
//go:embed opencode_zen_priced_models.json
var openCodeZenPricedModelsJSON []byte

var openCodeZenPricedModels = func() []string {
	var models []string
	if err := json.Unmarshal(openCodeZenPricedModelsJSON, &models); err != nil {
		panic(err)
	}
	return models
}()

// IsOpenCodeOfficiallyPricedModel 按账号模式检查官方是否公布了该模型的价格。
func IsOpenCodeOfficiallyPricedModel(account *Account, model string) bool {
	model = normalizeOpenCodeGoModelID(model)
	if account != nil && account.IsOpenCodeZen() {
		return slices.Contains(openCodeZenPricedModels, model)
	}
	_, ok := openCodeReferencePricing(model)
	return ok
}

// OpenCodeDefaultModelIDs 返回账号模式对应的官方已定价目录。
func OpenCodeDefaultModelIDs(account *Account) []string {
	if account != nil && account.IsOpenCodeZen() {
		return slices.Clone(openCodeZenPricedModels)
	}
	return DefaultOpenCodeGoModelIDs()
}

func openCodeReferenceModelPricingAt(model string, at time.Time) (*ModelPricing, bool) {
	entry, ok := openCodeReferencePricing(model)
	if !ok {
		return nil, false
	}
	// 使用同一份渠道格式解析参考价，保持阶梯和峰谷时段一致。
	entry.MonthlyQuota = nil
	channel := &Channel{ModelPricing: []ChannelModelPricing{*entry}}
	prices := channel.AccountStatsRulePricingAt(AccountStatsPricingRule{PricingSource: AccountStatsPricingSourceChannel, Platform: PlatformOpenCodeGo}, at)
	pricing := &ModelPricing{AllowZeroRate: commandCodeChannelPricingIsFree(entry), Intervals: prices[0].Intervals}
	applyChannelTokenPriceOverrides(pricing, &prices[0])
	return pricing, true
}

// OpenCodeChannelModelIDs 让模型列表跟随渠道增删，避免使用账号中的陈旧目录。
func (s *ChannelService) OpenCodeChannelModelIDs(ctx context.Context, groupID int64) ([]string, bool) {
	lookup, err := s.lookupGroupChannel(ctx, groupID)
	if err != nil || lookup == nil {
		return nil, false
	}
	models := make([]string, 0)
	for _, model := range lookup.channel.SupportedModels() {
		if model.Platform == PlatformOpenCodeGo && model.Pricing != nil && !pricingNeedsFallback(model.Pricing) && !IsOpenCodeUnsupportedModel(model.Name) {
			models = append(models, model.Name)
		}
	}
	sort.Strings(models)
	return models, true
}

func applyOpenCodeChannelQuotaCostAt(cost *CostBreakdown, pricing *ChannelModelPricing, at time.Time) error {
	if cost == nil || pricing == nil {
		return nil
	}
	multiplier := 1.0
	if pricing.MonthlyQuota != nil && !commandCodeChannelPricingIsFree(pricing) {
		if at.IsZero() {
			at = time.Now()
		}
		quota, ok := pricing.MonthlyQuota.ResolveAt(at, openCodeSharedMonthlyQuotaUSD)
		if !ok {
			return quotaCostUnavailableError(strings.Join(pricing.Models, ","))
		}
		multiplier = quota.CostMultiplier
	}
	applyModelSpecificMultiplierToCost(cost, multiplier)
	if commandCodeChannelPricingIsFree(pricing) {
		cost.AllowZeroRate = true
	}
	cost.platformQuotaApplied = true
	return nil
}

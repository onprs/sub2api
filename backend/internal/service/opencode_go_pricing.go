package service

import (
	"math"
	"strings"
	"time"
)

const openCodeGoSharedMonthlyQuotaUSD = 60.0

// openCodeGoReferencePrices 是 OpenCode Go 的平台限定标准价目录。
//
// 当前正式型号以 https://opencode.ai/docs/go/ 的价格表为准；官方 /models
// 仍返回但价格表已移除的兼容型号，使用 models.dev 的 opencode-go provider
// 历史/legacy 条目。价格单位均为 USD/token。该目录只允许通过
// GetModelPricingForPlatform(PlatformOpenCodeGo, ...) 使用，不能污染其他平台的
// 同名模型。
var openCodeGoReferencePrices = map[string]ModelPricing{
	"deepseek-v4-flash": {
		InputPricePerToken:     0.22e-6,
		OutputPricePerToken:    0.66e-6,
		CacheReadPricePerToken: 0.007e-6,
	},
	"deepseek-v4-flash-vision-exp": {
		InputPricePerToken:     0.22e-6,
		OutputPricePerToken:    0.66e-6,
		CacheReadPricePerToken: 0.007e-6,
	},
	"deepseek-v4-pro": {
		InputPricePerToken:     0.66e-6,
		OutputPricePerToken:    1.98e-6,
		CacheReadPricePerToken: 0.022e-6,
	},
	"glm-5": {
		InputPricePerToken:     1e-6,
		OutputPricePerToken:    3.2e-6,
		CacheReadPricePerToken: 0.2e-6,
	},
	"glm-5.1": {
		InputPricePerToken:     1.4e-6,
		OutputPricePerToken:    4.4e-6,
		CacheReadPricePerToken: 0.26e-6,
	},
	"glm-5.2": {
		InputPricePerToken:     1.4e-6,
		OutputPricePerToken:    4.4e-6,
		CacheReadPricePerToken: 0.26e-6,
	},
	"glm-5.3": {
		InputPricePerToken:     1.4e-6,
		OutputPricePerToken:    4.4e-6,
		CacheReadPricePerToken: 0.26e-6,
	},
	"gpt-5.6-luna": {
		InputPricePerToken:          0.2e-6,
		OutputPricePerToken:         1.2e-6,
		CacheCreationPricePerToken:  0.25e-6,
		CacheReadPricePerToken:      0.02e-6,
		LongContextInputThreshold:   272000,
		LongContextInputMultiplier:  2,
		LongContextOutputMultiplier: 1.5,
	},
	"grok-4.5": {
		InputPricePerToken:     2e-6,
		OutputPricePerToken:    6e-6,
		CacheReadPricePerToken: 0.3e-6,
	},
	"muse-spark-1.2-contributor": {
		InputPricePerToken:     0.1e-6,
		OutputPricePerToken:    0.2e-6,
		CacheReadPricePerToken: 0.002e-6,
	},
	"hy3": {
		InputPricePerToken:     0.14e-6,
		OutputPricePerToken:    0.58e-6,
		CacheReadPricePerToken: 0.035e-6,
	},
	"hy3-preview": {
		InputPricePerToken:     0.285714e-6,
		OutputPricePerToken:    1.142857e-6,
		CacheReadPricePerToken: 0.114286e-6,
	},
	"kimi-k2.5": {
		InputPricePerToken:     0.6e-6,
		OutputPricePerToken:    3e-6,
		CacheReadPricePerToken: 0.1e-6,
	},
	"kimi-k2.6": {
		InputPricePerToken:     0.95e-6,
		OutputPricePerToken:    4e-6,
		CacheReadPricePerToken: 0.16e-6,
	},
	"kimi-k2.7-code": {
		InputPricePerToken:     0.95e-6,
		OutputPricePerToken:    4e-6,
		CacheReadPricePerToken: 0.19e-6,
	},
	"kimi-k3": {
		InputPricePerToken:     3e-6,
		OutputPricePerToken:    15e-6,
		CacheReadPricePerToken: 0.3e-6,
	},
	"mimo-v2-omni": {
		InputPricePerToken:     0.4e-6,
		OutputPricePerToken:    2e-6,
		CacheReadPricePerToken: 0.08e-6,
	},
	"mimo-v2-pro": {
		InputPricePerToken:          1e-6,
		OutputPricePerToken:         3e-6,
		CacheReadPricePerToken:      0.2e-6,
		LongContextInputThreshold:   256000,
		LongContextInputMultiplier:  2,
		LongContextOutputMultiplier: 2,
	},
	"mimo-v2.5": {
		InputPricePerToken:     0.14e-6,
		OutputPricePerToken:    0.28e-6,
		CacheReadPricePerToken: 0.0028e-6,
	},
	"mimo-v2.5-pro": {
		InputPricePerToken:     0.435e-6,
		OutputPricePerToken:    0.87e-6,
		CacheReadPricePerToken: 0.003625e-6,
	},
	"minimax-m2.5": {
		InputPricePerToken:         0.3e-6,
		OutputPricePerToken:        1.2e-6,
		CacheCreationPricePerToken: 0.375e-6,
		CacheReadPricePerToken:     0.06e-6,
	},
	"minimax-m2.7": {
		InputPricePerToken:         0.3e-6,
		OutputPricePerToken:        1.2e-6,
		CacheCreationPricePerToken: 0.375e-6,
		CacheReadPricePerToken:     0.06e-6,
	},
	"minimax-m3": {
		InputPricePerToken:     0.3e-6,
		OutputPricePerToken:    1.2e-6,
		CacheReadPricePerToken: 0.06e-6,
	},
	"qwen3.5-plus": {
		InputPricePerToken:         0.2e-6,
		OutputPricePerToken:        1.2e-6,
		CacheCreationPricePerToken: 0.25e-6,
		CacheReadPricePerToken:     0.02e-6,
	},
	"qwen3.6-plus": {
		InputPricePerToken:          0.5e-6,
		OutputPricePerToken:         3e-6,
		CacheCreationPricePerToken:  0.625e-6,
		CacheReadPricePerToken:      0.05e-6,
		LongContextInputThreshold:   256000,
		LongContextInputMultiplier:  4,
		LongContextOutputMultiplier: 2,
	},
	"qwen3.7-max": {
		InputPricePerToken:         2.5e-6,
		OutputPricePerToken:        7.5e-6,
		CacheCreationPricePerToken: 3.125e-6,
		CacheReadPricePerToken:     0.5e-6,
	},
	"qwen3.7-plus": {
		InputPricePerToken:          0.4e-6,
		OutputPricePerToken:         1.6e-6,
		CacheCreationPricePerToken:  0.5e-6,
		CacheReadPricePerToken:      0.04e-6,
		LongContextInputThreshold:   256000,
		LongContextInputMultiplier:  3,
		LongContextOutputMultiplier: 3,
	},
	"qwen3.8-max": {
		InputPricePerToken:         2e-6,
		OutputPricePerToken:        6e-6,
		CacheCreationPricePerToken: 2.5e-6,
		CacheReadPricePerToken:     0.25e-6,
	},
}

// OpenCodeGoQuotaCost 描述 OpenCode Go 模型当前有效的月可用额度及额度成本乘数。
// 基础规则由业务方确认：共享月额度除以该模型的官方月可用额度；
// 有效的官方 usage offer 会先扩大月可用额度，再据此计算乘数。
type OpenCodeGoQuotaCost struct {
	IncludedMonthlyUsageUSD float64
	Multiplier              float64
}

// openCodeGoReferenceMonthlyUsageUSD 只收录官方 Go 价格表明确给出 Usage 的型号。
// 兼容目录中没有 Usage 证据的型号不得默认按 1x 计费。
var openCodeGoReferenceMonthlyUsageUSD = map[string]float64{
	"grok-4.5":                     15,
	"gpt-5.6-luna":                 15,
	"glm-5.1":                      60,
	"glm-5.2":                      60,
	"glm-5.3":                      15,
	"kimi-k2.6":                    60,
	"kimi-k2.7-code":               60,
	"kimi-k3":                      15,
	"mimo-v2.5":                    60,
	"mimo-v2.5-pro":                15,
	"minimax-m2.5":                 60,
	"minimax-m2.7":                 60,
	"minimax-m3":                   60,
	"muse-spark-1.2-contributor":   60,
	"qwen3.6-plus":                 60,
	"qwen3.7-max":                  60,
	"qwen3.7-plus":                 60,
	"qwen3.8-max":                  15,
	"deepseek-v4-pro":              15,
	"deepseek-v4-flash":            30,
	"deepseek-v4-flash-vision-exp": 15,
	"hy3":                          60,
}

func openCodeGoQuotaCostFromMonthlyUsage(monthlyUsageUSD float64) (OpenCodeGoQuotaCost, bool) {
	if monthlyUsageUSD <= 0 || math.IsNaN(monthlyUsageUSD) || math.IsInf(monthlyUsageUSD, 0) {
		return OpenCodeGoQuotaCost{}, false
	}
	multiplier := openCodeGoSharedMonthlyQuotaUSD / monthlyUsageUSD
	if multiplier <= 0 || math.IsNaN(multiplier) || math.IsInf(multiplier, 0) {
		return OpenCodeGoQuotaCost{}, false
	}
	return OpenCodeGoQuotaCost{
		IncludedMonthlyUsageUSD: monthlyUsageUSD,
		Multiplier:              multiplier,
	}, true
}

func openCodeGoReferenceQuotaCost(model string) (OpenCodeGoQuotaCost, bool) {
	model = billingModelAliasLookupKey(model)
	if model == "kimi-k2.7" {
		model = "kimi-k2.7-code"
	}
	monthlyUsageUSD, ok := openCodeGoReferenceMonthlyUsageUSD[model]
	if !ok {
		return OpenCodeGoQuotaCost{}, false
	}
	return openCodeGoQuotaCostFromMonthlyUsage(monthlyUsageUSD)
}

var openCodeGoReferencePeakPrices = map[string]ModelPricing{
	"deepseek-v4-flash": {
		InputPricePerToken:     0.44e-6,
		OutputPricePerToken:    1.32e-6,
		CacheReadPricePerToken: 0.014e-6,
	},
	"deepseek-v4-flash-vision-exp": {
		InputPricePerToken:     0.44e-6,
		OutputPricePerToken:    1.32e-6,
		CacheReadPricePerToken: 0.014e-6,
	},
	"deepseek-v4-pro": {
		InputPricePerToken:     1.32e-6,
		OutputPricePerToken:    3.96e-6,
		CacheReadPricePerToken: 0.044e-6,
	},
}

var openCodeGoModelsDevSupplementalModels = map[string]struct{}{
	"hy3-preview":  {},
	"kimi-k2.5":    {},
	"mimo-v2-omni": {},
	"mimo-v2-pro":  {},
	"qwen3.5-plus": {},
}

func isOpenCodeGoModelsDevSupplementalModel(model string) bool {
	model = billingModelAliasLookupKey(model)
	_, ok := openCodeGoModelsDevSupplementalModels[model]
	return ok
}

func openCodeGoReferencePricing(model string) (*ModelPricing, bool) {
	return openCodeGoReferencePricingAt(model, time.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC))
}

func openCodeGoReferencePricingAt(model string, now time.Time) (*ModelPricing, bool) {
	model = billingModelAliasLookupKey(model)
	if model == "kimi-k2.7" {
		model = "kimi-k2.7-code"
	}
	prices := openCodeGoReferencePrices
	if isOpenCodeGoPeakTime(now) {
		if _, ok := openCodeGoReferencePeakPrices[model]; ok {
			prices = openCodeGoReferencePeakPrices
		}
	}
	pricing, ok := prices[model]
	if !ok {
		return nil, false
	}
	cloned := pricing
	return &cloned, true
}

func openCodeGoRequiresTimeBandPricing(model string) bool {
	model = billingModelAliasLookupKey(model)
	_, ok := openCodeGoReferencePeakPrices[model]
	return ok
}

func isOpenCodeGoPeakTime(now time.Time) bool {
	if now.IsZero() {
		now = time.Now()
	}
	hour := now.UTC().Hour()
	return (hour >= 1 && hour < 4) || (hour >= 6 && hour < 10)
}

func isOpenCodeGoPricingPlatform(platform string) bool {
	return strings.EqualFold(strings.TrimSpace(platform), PlatformOpenCodeGo)
}

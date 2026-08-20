package service

import "strings"

// openCodeGoReferencePrices 是 OpenCode Go 的平台限定标准价目录。
//
// 当前正式型号以 https://opencode.ai/docs/go/ 的价格表为准；官方 /models
// 仍返回但价格表已移除的兼容型号，使用 models.dev 的 opencode-go provider
// 历史/legacy 条目。价格单位均为 USD/token。该目录只允许通过
// GetModelPricingForPlatform(PlatformOpenCodeGo, ...) 使用，不能污染其他平台的
// 同名模型。
var openCodeGoReferencePrices = map[string]ModelPricing{
	"deepseek-v4-flash": {
		InputPricePerToken:     0.14e-6,
		OutputPricePerToken:    0.28e-6,
		CacheReadPricePerToken: 0.0028e-6,
	},
	"deepseek-v4-pro": {
		InputPricePerToken:     0.435e-6,
		OutputPricePerToken:    0.87e-6,
		CacheReadPricePerToken: 0.003625e-6,
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
	model = billingModelAliasLookupKey(model)
	if model == "kimi-k2.7" {
		model = "kimi-k2.7-code"
	}
	pricing, ok := openCodeGoReferencePrices[model]
	if !ok {
		return nil, false
	}
	cloned := pricing
	return &cloned, true
}

func isOpenCodeGoPricingPlatform(platform string) bool {
	return strings.EqualFold(strings.TrimSpace(platform), PlatformOpenCodeGo)
}

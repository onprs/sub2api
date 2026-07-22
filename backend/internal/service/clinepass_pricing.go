package service

import "strings"

const clinePassQwen37PlusLongContextThreshold = 256000

// clinePassReferencePrices mirrors Cline's published ClinePass reference rates.
// Prices are USD per token. ClinePass quota usage is measured against these
// rates even though the upstream product itself is a monthly subscription.
// Source: https://docs.cline.bot/getting-started/clinepass
var clinePassReferencePrices = map[string]ModelPricing{
	"glm-5.2": {
		InputPricePerToken:     1.4e-6,
		OutputPricePerToken:    4.4e-6,
		CacheReadPricePerToken: 0.26e-6,
	},
	"kimi-k3": {
		InputPricePerToken:     3e-6,
		OutputPricePerToken:    15e-6,
		CacheReadPricePerToken: 0.3e-6,
	},
	"deepseek-v4-pro": {
		InputPricePerToken:     1.74e-6,
		OutputPricePerToken:    3.48e-6,
		CacheReadPricePerToken: 0.0145e-6,
	},
	"deepseek-v4-flash": {
		InputPricePerToken:     0.14e-6,
		OutputPricePerToken:    0.28e-6,
		CacheReadPricePerToken: 0.0028e-6,
	},
	"kimi-k2.7-code": {
		InputPricePerToken:     0.95e-6,
		OutputPricePerToken:    4e-6,
		CacheReadPricePerToken: 0.19e-6,
	},
	"kimi-k2.6": {
		InputPricePerToken:     0.95e-6,
		OutputPricePerToken:    4e-6,
		CacheReadPricePerToken: 0.16e-6,
	},
	"mimo-v2.5-pro": {
		InputPricePerToken:     1.74e-6,
		OutputPricePerToken:    3.48e-6,
		CacheReadPricePerToken: 0.0145e-6,
	},
	"mimo-v2.5": {
		InputPricePerToken:     0.14e-6,
		OutputPricePerToken:    0.28e-6,
		CacheReadPricePerToken: 0.0028e-6,
	},
	"minimax-m3": {
		InputPricePerToken:     0.3e-6,
		OutputPricePerToken:    1.2e-6,
		CacheReadPricePerToken: 0.06e-6,
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
		LongContextInputThreshold:   clinePassQwen37PlusLongContextThreshold,
		LongContextInputMultiplier:  3,
		LongContextOutputMultiplier: 3,
	},
}

// clinePassReferencePricing returns (pricing, true) for any explicit
// cline-pass/ or clinepass/ slug. A nil pricing with true means the slug is in
// the ClinePass namespace but has no published rate and must fail closed.
func clinePassReferencePricing(model string) (*ModelPricing, bool) {
	normalized := strings.ToLower(strings.TrimSpace(model))
	var modelID string
	switch {
	case strings.HasPrefix(normalized, "cline-pass/"):
		modelID = strings.TrimPrefix(normalized, "cline-pass/")
	case strings.HasPrefix(normalized, "clinepass/"):
		modelID = strings.TrimPrefix(normalized, "clinepass/")
	default:
		return nil, false
	}

	pricing, ok := clinePassReferencePrices[modelID]
	if !ok {
		return nil, true
	}
	cloned := pricing
	return &cloned, true
}

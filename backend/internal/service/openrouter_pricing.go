package service

import "strings"

// openRouterReferencePrices mirrors OpenRouter's published reference rates.
// Prices are USD per token. Free models default to zero rates.
var openRouterReferencePrices = map[string]ModelPricing{
	"dots-studio/dots-3-note-preview:free":               {InputPricePerToken: 0, OutputPricePerToken: 0},
	"google/gemma-4-26b-a4b-it:free":                     {InputPricePerToken: 0, OutputPricePerToken: 0},
	"google/gemma-4-31b-it:free":                         {InputPricePerToken: 0, OutputPricePerToken: 0},
	"liquid/lfm-2.5-2.6b:free":                           {InputPricePerToken: 0, OutputPricePerToken: 0},
	"nvidia/nemotron-3.5-lightning:free":                 {InputPricePerToken: 0, OutputPricePerToken: 0},
	"nvidia/nemotron-3.5-content-safety:free":            {InputPricePerToken: 0, OutputPricePerToken: 0},
	"nvidia/nemotron-3-ultra-550b-a55b:free":             {InputPricePerToken: 0, OutputPricePerToken: 0},
	"nvidia/nemotron-3-nano-omni-30b-a3b-reasoning:free": {InputPricePerToken: 0, OutputPricePerToken: 0},
	"nvidia/nemotron-3-super-120b-a12b:free":             {InputPricePerToken: 0, OutputPricePerToken: 0},
	"nvidia/nemotron-3-nano-30b-a3b:free":                {InputPricePerToken: 0, OutputPricePerToken: 0},
	"nvidia/nemotron-nano-12b-v2-vl:free":                {InputPricePerToken: 0, OutputPricePerToken: 0},
	"nvidia/nemotron-nano-9b-v2:free":                    {InputPricePerToken: 0, OutputPricePerToken: 0},
	"openai/gpt-oss-20b:free":                            {InputPricePerToken: 0, OutputPricePerToken: 0},
	"openrouter/free":                                    {InputPricePerToken: 0, OutputPricePerToken: 0},
	"poolside/laguna-s-2.1:free":                         {InputPricePerToken: 0, OutputPricePerToken: 0},
	"poolside/laguna-xs-2.1:free":                        {InputPricePerToken: 0, OutputPricePerToken: 0},
	"z-ai/glm-5.2:free":                                  {InputPricePerToken: 0, OutputPricePerToken: 0},
}

// openRouterReferencePricing returns (pricing, true) for OpenRouter models.
// Free-tier models (with :free suffix or openrouter/free) return 0-rate pricing.
func openRouterReferencePricing(model string) (*ModelPricing, bool) {
	normalized := strings.ToLower(strings.TrimSpace(model))
	if normalized == "" {
		return nil, false
	}

	if pricing, ok := openRouterReferencePrices[normalized]; ok {
		cloned := pricing
		return &cloned, true
	}

	if strings.HasSuffix(normalized, ":free") || normalized == "openrouter/free" {
		pricing := ModelPricing{InputPricePerToken: 0, OutputPricePerToken: 0}
		return &pricing, true
	}

	return nil, false
}

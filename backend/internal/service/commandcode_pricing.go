package service

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// Command Code 内置目录只作为官方 GOAT 文档暂时不可用时的启动降级。
// 在线快照会按官方 minPlanName=Go/GOAT 自动替换这些条目。
var commandCodeFallbackModels = []string{
	"gpt-5.6-sol",
	"gpt-5.6-luna",
	"google/gemini-3.7-flash",
	"xai/grok-4.6",
	"xai/grok-4.5",
	"meta/muse-spark-1.2",
	"meta/muse-spark-1.2-contributor",
	"deepseek/deepseek-v4-pro",
	"deepseek/deepseek-v4-flash",
	"deepseek/deepseek-v4-flash-vision-exp",
	"moonshotai/Kimi-K3",
	"moonshotai/Kimi-K2.7-Code",
	"moonshotai/Kimi-K2.7-Code-Highspeed",
	"moonshotai/Kimi-K2.6",
	"moonshotai/Kimi-K2.5",
	"z-ai/glm-5.3-flash",
	"zai-org/GLM-5.3",
	"zai-org/GLM-5.2",
	"zai-org/GLM-5.2-Fast",
	"zai-org/GLM-5.1",
	"zai-org/GLM-5",
	"MiniMaxAI/MiniMax-M3",
	"MiniMaxAI/MiniMax-M2.7",
	"minimax/minimax-m3-free",
	"minimax/minimax-m2.7-free",
	"MiniMaxAI/MiniMax-M2.5",
	"xiaomi/mimo-v2.5-pro",
	"xiaomi/mimo-v2.5",
	"Qwen/Qwen3.8-Max",
	"Qwen/Qwen3.8-27B",
	"Qwen/Qwen3.7-Max",
	"Qwen/Qwen3.7-Plus",
	"Qwen/Qwen3.7-Flash",
	"Qwen/Qwen3.6-Max-Preview",
	"Qwen/Qwen3.6-Plus",
	"stepfun/Step-3.7-Flash",
	"stepfun/Step-3.5-Flash",
	"tencent/hy3-paid",
	"nvidia/nemotron-3-ultra-550b-a55b",
	"thinkingmachines/inkling",
	"thinkingmachines/inkling-small",
	"poolside/laguna-s-2.1-free",
}

type commandCodePriceEntry struct {
	input      float64
	output     float64
	cacheRead  float64
	cacheWrite float64
	allowZero  bool
}

// commandCodeReferencePrices 是当前官方 GOAT 价格的内置降级副本，单位为 USD / 1M token。
var commandCodeReferencePrices = map[string]commandCodePriceEntry{
	"gpt-5.6-sol":                           {input: 5, output: 30, cacheRead: 0.5, cacheWrite: 6.25},
	"gpt-5.6-luna":                          {input: 0.2, output: 1.2, cacheRead: 0.02, cacheWrite: 0.25},
	"google/gemini-3.7-flash":               {input: 0.75, output: 3.75, cacheRead: 0.075, cacheWrite: 0.04167},
	"xai/grok-4.6":                          {input: 2, output: 6, cacheRead: 0.5},
	"xai/grok-4.5":                          {input: 2, output: 6, cacheRead: 0.5},
	"meta/muse-spark-1.2":                   {input: 1.25, output: 4.25, cacheRead: 0.15},
	"meta/muse-spark-1.2-contributor":       {input: 0.1, output: 0.2, cacheRead: 0.002},
	"deepseek/deepseek-v4-pro":              {input: 0.66, output: 1.98, cacheRead: 0.022},
	"deepseek/deepseek-v4-flash":            {input: 0.22, output: 0.66, cacheRead: 0.007},
	"deepseek/deepseek-v4-flash-vision-exp": {input: 0.22, output: 0.66, cacheRead: 0.007},
	"moonshotai/kimi-k3":                    {input: 3, output: 15, cacheRead: 0.3},
	"moonshotai/kimi-k2.7-code":             {input: 0.95, output: 4, cacheRead: 0.19},
	"moonshotai/kimi-k2.7-code-highspeed":   {input: 1.9, output: 8, cacheRead: 0.38},
	"moonshotai/kimi-k2.6":                  {input: 0.95, output: 4, cacheRead: 0.16},
	"moonshotai/kimi-k2.5":                  {input: 0.6, output: 3, cacheRead: 0.1},
	"z-ai/glm-5.3-flash":                    {input: 0.15, output: 0.5, cacheRead: 0.03},
	"zai-org/glm-5.3":                       {input: 1.4, output: 4.4, cacheRead: 0.26},
	"zai-org/glm-5.2":                       {input: 1.4, output: 4.4, cacheRead: 0.26},
	"zai-org/glm-5.2-fast":                  {input: 3, output: 10.25, cacheRead: 0.5},
	"zai-org/glm-5.1":                       {input: 1.4, output: 4.4, cacheRead: 0.26},
	"zai-org/glm-5":                         {input: 1, output: 3.2, cacheRead: 0.2},
	"minimaxai/minimax-m3":                  {input: 0.3, output: 1.2, cacheRead: 0.06},
	"minimaxai/minimax-m2.7":                {input: 0.3, output: 1.2, cacheRead: 0.06},
	"minimaxai/minimax-m2.5":                {input: 0.3, output: 1.2, cacheRead: 0.03},
	"minimax/minimax-m3-free":               {allowZero: true},
	"minimax/minimax-m2.7-free":             {allowZero: true},
	"xiaomi/mimo-v2.5-pro":                  {input: 0.435, output: 0.87, cacheRead: 0.0036},
	"xiaomi/mimo-v2.5":                      {input: 0.14, output: 0.28, cacheRead: 0.0028},
	"qwen/qwen3.8-max":                      {input: 2, output: 6, cacheRead: 0.25, cacheWrite: 2.5},
	"qwen/qwen3.8-27b":                      {input: 0.4, output: 3, cacheRead: 0.04},
	"qwen/qwen3.7-max":                      {input: 2.5, output: 7.5, cacheRead: 0.5, cacheWrite: 3.13},
	"qwen/qwen3.7-plus":                     {input: 0.4, output: 1.6, cacheRead: 0.08, cacheWrite: 0.5},
	"qwen/qwen3.7-flash":                    {input: 0.03, output: 0.13, cacheRead: 0.006, cacheWrite: 0.038},
	"qwen/qwen3.6-max-preview":              {input: 1.3, output: 7.8, cacheRead: 0.26, cacheWrite: 1.63},
	"qwen/qwen3.6-plus":                     {input: 0.5, output: 3, cacheRead: 0.1},
	"stepfun/step-3.7-flash":                {input: 0.2, output: 1.15, cacheRead: 0.04},
	"stepfun/step-3.5-flash":                {input: 0.1, output: 0.3, cacheRead: 0.02},
	"tencent/hy3-paid":                      {input: 0.14, output: 0.58, cacheRead: 0.035},
	"nvidia/nemotron-3-ultra-550b-a55b":     {input: 0.6, output: 2.4, cacheRead: 0.12},
	"thinkingmachines/inkling":              {input: 1, output: 4.05, cacheRead: 0.17},
	"thinkingmachines/inkling-small":        {input: 0.5, output: 1.2, cacheRead: 0.1},
	"poolside/laguna-s-2.1-free":            {allowZero: true},
}

// commandCodeFallbackMonthlyCreditsUSD 是官方 GOAT 计划 Monthly credits 的启动降级副本（USD）。
var commandCodeFallbackMonthlyCreditsUSD = map[string]float64{
	"gpt-5.6-sol": 70, "gpt-5.6-luna": 20,
	"google/gemini-3.7-flash": 40,
	"xai/grok-4.6":            20, "xai/grok-4.5": 20,
	"meta/muse-spark-1.2": 20, "meta/muse-spark-1.2-contributor": 20,
	"deepseek/deepseek-v4-pro": 20, "deepseek/deepseek-v4-flash": 60,
	"deepseek/deepseek-v4-flash-vision-exp": 20,
	"moonshotai/kimi-k3":                    20, "moonshotai/kimi-k2.7-code": 60,
	"moonshotai/kimi-k2.7-code-highspeed": 20, "moonshotai/kimi-k2.6": 20,
	"moonshotai/kimi-k2.5": 20,
	"z-ai/glm-5.3-flash":   40, "zai-org/glm-5.3": 20,
	"zai-org/glm-5.2": 70, "zai-org/glm-5.2-fast": 20,
	"zai-org/glm-5.1": 20, "zai-org/glm-5": 20,
	"minimaxai/minimax-m3": 47, "minimaxai/minimax-m2.7": 20,
	"minimaxai/minimax-m2.5": 20, "minimax/minimax-m3-free": 20,
	"minimax/minimax-m2.7-free": 20,
	"xiaomi/mimo-v2.5-pro":      20, "xiaomi/mimo-v2.5": 30,
	"qwen/qwen3.8-max": 20, "qwen/qwen3.8-27b": 70,
	"qwen/qwen3.7-max": 33, "qwen/qwen3.7-plus": 33,
	"qwen/qwen3.7-flash": 20, "qwen/qwen3.6-max-preview": 20,
	"qwen/qwen3.6-plus":      33,
	"stepfun/step-3.7-flash": 20, "stepfun/step-3.5-flash": 20,
	"tencent/hy3-paid": 70, "nvidia/nemotron-3-ultra-550b-a55b": 20,
	"thinkingmachines/inkling": 20, "thinkingmachines/inkling-small": 20,
	"poolside/laguna-s-2.1-free": 20,
}

var commandCodeFallbackContextWindows = map[string]int{
	"gpt-5.6-sol": 1_050_000, "gpt-5.6-luna": 1_050_000,
	"google/gemini-3.7-flash": 1_048_576,
	"xai/grok-4.6":            500_000, "xai/grok-4.5": 500_000,
	"meta/muse-spark-1.2": 1_048_576, "meta/muse-spark-1.2-contributor": 1_048_576,
	"deepseek/deepseek-v4-pro": 1_000_000, "deepseek/deepseek-v4-flash": 1_000_000,
	"deepseek/deepseek-v4-flash-vision-exp": 1_000_000,
	"moonshotai/kimi-k3":                    1_000_000, "moonshotai/kimi-k2.7-code": 256_000,
	"moonshotai/kimi-k2.7-code-highspeed": 262_000, "moonshotai/kimi-k2.6": 256_000,
	"moonshotai/kimi-k2.5": 256_000,
	"z-ai/glm-5.3-flash":   1_048_576, "zai-org/glm-5.3": 1_000_000,
	"zai-org/glm-5.2": 1_000_000, "zai-org/glm-5.2-fast": 1_000_000,
	"zai-org/glm-5.1": 200_000, "zai-org/glm-5": 200_000,
	"minimaxai/minimax-m3": 1_000_000, "minimaxai/minimax-m2.7": 200_000,
	"minimaxai/minimax-m2.5": 200_000, "minimax/minimax-m3-free": 1_000_000,
	"minimax/minimax-m2.7-free": 197_000,
	"xiaomi/mimo-v2.5-pro":      1_000_000, "xiaomi/mimo-v2.5": 1_000_000,
	"qwen/qwen3.8-max": 1_000_000, "qwen/qwen3.8-27b": 262_144,
	"qwen/qwen3.7-max": 1_000_000, "qwen/qwen3.7-plus": 1_000_000,
	"qwen/qwen3.7-flash": 1_000_000, "qwen/qwen3.6-max-preview": 200_000,
	"qwen/qwen3.6-plus":      200_000,
	"stepfun/step-3.7-flash": 256_000, "stepfun/step-3.5-flash": 1_000_000,
	"tencent/hy3-paid": 262_144, "nvidia/nemotron-3-ultra-550b-a55b": 1_000_000,
	"thinkingmachines/inkling": 256_000, "thinkingmachines/inkling-small": 1_000_000,
	"poolside/laguna-s-2.1-free": 256_000,
}

var commandCodeModelAliases = map[string]string{
	"deepseek-v4-pro": "deepseek/deepseek-v4-pro", "deepseek-v4-flash": "deepseek/deepseek-v4-flash",
	"deepseek-v4-flash-vision-exp": "deepseek/deepseek-v4-flash-vision-exp",
	"kimi-k3":                      "moonshotai/kimi-k3", "kimi-k2.7-code": "moonshotai/kimi-k2.7-code",
	"kimi-k2.7-code-highspeed": "moonshotai/kimi-k2.7-code-highspeed", "kimi-k2.6": "moonshotai/kimi-k2.6",
	"kimi-k2.5":     "moonshotai/kimi-k2.5",
	"glm-5.3-flash": "z-ai/glm-5.3-flash", "glm-5.3": "zai-org/glm-5.3",
	"glm-5.2": "zai-org/glm-5.2", "glm-5.2-fast": "zai-org/glm-5.2-fast",
	"glm-5.1": "zai-org/glm-5.1", "glm-5": "zai-org/glm-5",
	"minimax-m3": "minimaxai/minimax-m3", "minimax-m2.7": "minimaxai/minimax-m2.7",
	"minimax-m2.5": "minimaxai/minimax-m2.5", "minimax-m3-free": "minimax/minimax-m3-free",
	"minimax-m2.7-free": "minimax/minimax-m2.7-free",
	"mimo-v2.5-pro":     "xiaomi/mimo-v2.5-pro", "mimo-v2.5": "xiaomi/mimo-v2.5",
	"qwen3.8-max": "qwen/qwen3.8-max", "qwen-3.8-max": "qwen/qwen3.8-max",
	"qwen3.8-27b": "qwen/qwen3.8-27b", "qwen3.7-max": "qwen/qwen3.7-max",
	"qwen-3.7-max": "qwen/qwen3.7-max", "qwen3.7-plus": "qwen/qwen3.7-plus",
	"qwen-3.7-plus": "qwen/qwen3.7-plus", "qwen3.7-flash": "qwen/qwen3.7-flash",
	"qwen-3.7-flash": "qwen/qwen3.7-flash", "qwen3.6-max-preview": "qwen/qwen3.6-max-preview",
	"qwen-3.6-max-preview": "qwen/qwen3.6-max-preview", "qwen3.6-plus": "qwen/qwen3.6-plus",
	"qwen-3.6-plus":  "qwen/qwen3.6-plus",
	"step-3.7-flash": "stepfun/step-3.7-flash", "step-3.5-flash": "stepfun/step-3.5-flash",
	"hy3-paid": "tencent/hy3-paid", "gemini-3.7-flash": "google/gemini-3.7-flash",
	"nemotron-3-ultra-550b-a55b": "nvidia/nemotron-3-ultra-550b-a55b",
	"nemotron-3-ultra":           "nvidia/nemotron-3-ultra-550b-a55b",
	"inkling":                    "thinkingmachines/inkling", "inkling-small": "thinkingmachines/inkling-small",
	"laguna-s-2.1-free": "poolside/laguna-s-2.1-free", "laguna-s-2.1": "poolside/laguna-s-2.1-free",
	"muse-spark-1.2": "meta/muse-spark-1.2", "muse-spark-1.2-contributor": "meta/muse-spark-1.2-contributor",
	"grok-4.5": "xai/grok-4.5", "grok-4.6": "xai/grok-4.6",
}

func commandCodeCanonicalModelID(model string) string {
	key := strings.ToLower(strings.TrimSpace(model))
	key = strings.TrimPrefix(key, "commandcode/")
	if key == "" {
		return ""
	}
	if aliased, ok := commandCodeModelAliases[key]; ok {
		return aliased
	}
	if idx := strings.Index(key, "/"); idx >= 0 {
		if aliased, ok := commandCodeModelAliases[key[idx+1:]]; ok {
			return aliased
		}
	}
	return key
}

func commandCodeFallbackCatalogEntries() map[string]commandCodeCatalogEntry {
	entries := make(map[string]commandCodeCatalogEntry, len(commandCodeFallbackModels))
	for _, id := range commandCodeFallbackModels {
		key := strings.ToLower(id)
		price, ok := commandCodeReferencePrices[key]
		if !ok {
			continue
		}
		entries[key] = commandCodeCatalogEntry{
			ID:                id,
			Name:              id,
			ContextWindow:     commandCodeFallbackContextWindows[key],
			MonthlyCreditsUSD: commandCodeFallbackMonthlyCreditsUSD[key],
			Tiers: []commandCodeCatalogTier{{
				Rates: commandCodeCatalogRatesFromReference(price),
			}},
		}
	}

	setCommandCodeFallbackTiers(entries)
	setCommandCodeFallbackDeals(entries)
	setCommandCodeFallbackTimeBands(entries)
	return entries
}

func commandCodeCatalogRatesFromReference(price commandCodePriceEntry) commandCodeCatalogRates {
	return commandCodeCatalogRates{
		Input:         price.input,
		Output:        price.output,
		CacheRead:     price.cacheRead,
		CacheWrite:    price.cacheWrite,
		HasCacheWrite: price.cacheWrite > 0,
	}
}

func commandCodeRates(input, output, cacheRead float64, cacheWrite ...float64) commandCodeCatalogRates {
	rates := commandCodeCatalogRates{Input: input, Output: output, CacheRead: cacheRead}
	if len(cacheWrite) > 0 {
		rates.CacheWrite = cacheWrite[0]
		rates.HasCacheWrite = true
	}
	return rates
}

func commandCodeTier(minTokens int, maxTokens *int, rates commandCodeCatalogRates) commandCodeCatalogTier {
	return commandCodeCatalogTier{MinTokens: minTokens, MaxTokens: maxTokens, Rates: rates}
}

func commandCodeInt(value int) *int { return &value }

func setCommandCodeFallbackTiers(entries map[string]commandCodeCatalogEntry) {
	set := func(key string, tiers []commandCodeCatalogTier) {
		entry := entries[key]
		entry.Tiers = tiers
		entries[key] = entry
	}
	set("gpt-5.6-sol", []commandCodeCatalogTier{
		commandCodeTier(0, commandCodeInt(272_000), commandCodeRates(5, 30, 0.5, 6.25)),
		commandCodeTier(272_000, nil, commandCodeRates(10, 45, 1, 12.5)),
	})
	set("gpt-5.6-luna", []commandCodeCatalogTier{
		commandCodeTier(0, commandCodeInt(272_000), commandCodeRates(0.2, 1.2, 0.02, 0.25)),
		commandCodeTier(272_000, nil, commandCodeRates(0.4, 1.8, 0.04, 0.5)),
	})
	set("xai/grok-4.6", []commandCodeCatalogTier{
		commandCodeTier(0, commandCodeInt(200_000), commandCodeRates(2, 6, 0.5)),
		commandCodeTier(200_000, nil, commandCodeRates(4, 12, 1)),
	})
	set("qwen/qwen3.7-plus", []commandCodeCatalogTier{
		commandCodeTier(0, commandCodeInt(256_000), commandCodeRates(0.4, 1.6, 0.08, 0.5)),
		commandCodeTier(256_000, nil, commandCodeRates(1.2, 4.8, 0.24, 1.5)),
	})
	set("qwen/qwen3.7-flash", []commandCodeCatalogTier{
		commandCodeTier(0, commandCodeInt(32_000), commandCodeRates(0.03, 0.13, 0.006, 0.038)),
		commandCodeTier(32_000, commandCodeInt(256_000), commandCodeRates(0.1, 0.4, 0.02, 0.125)),
		commandCodeTier(256_000, nil, commandCodeRates(0.2, 0.8, 0.04, 0.25)),
	})
	set("qwen/qwen3.6-plus", []commandCodeCatalogTier{
		commandCodeTier(0, commandCodeInt(256_000), commandCodeRates(0.5, 3, 0.1)),
		commandCodeTier(256_000, nil, commandCodeRates(2, 6, 0.2)),
	})
	standard := commandCodeRates(0.3, 1.2, 0.06)
	list := commandCodeRates(0.6, 2.4, 0.12)
	set("minimaxai/minimax-m3", []commandCodeCatalogTier{
		{MinTokens: 0, MaxTokens: commandCodeInt(512_000), Rates: standard, ListRates: &list},
		{MinTokens: 512_000, Rates: standard, ListRates: &list},
	})
}

var (
	commandCodeGemini37FlashDealExpiry = time.Date(2026, 12, 31, 23, 59, 59, 0, time.UTC)
	commandCodeMiniMaxFreeDealExpiry   = time.Date(2026, 9, 5, 23, 59, 59, 0, time.UTC)
)

func setCommandCodeFallbackDeals(entries map[string]commandCodeCatalogEntry) {
	set := func(key string, deal commandCodeCatalogDeal) {
		entry := entries[key]
		entry.Deal = &deal
		entries[key] = entry
	}
	set("google/gemini-3.7-flash", commandCodeCatalogDeal{
		Code: "gemini-3.7-flash-50-off", Label: "50% off", DiscountPercent: 50,
		Term: "ends December 31, 2026", ExpiresAt: commandCodeGemini37FlashDealExpiry,
	})
	entry := entries["google/gemini-3.7-flash"]
	list := commandCodeRates(1.5, 7.5, 0.15, 0.08334)
	entry.Tiers[0].ListRates = &list
	entries["google/gemini-3.7-flash"] = entry

	set("minimaxai/minimax-m3", commandCodeCatalogDeal{
		Code: "minimax-m3-2x-usage", Label: "50% off", DiscountPercent: 50,
	})
	set("xiaomi/mimo-v2.5", commandCodeCatalogDeal{
		Code: "mimo-v2.5-98-off", Label: "98% off", DiscountPercent: 98,
	})
	set("xiaomi/mimo-v2.5-pro", commandCodeCatalogDeal{
		Code: "mimo-v2.5-pro-99-off", Label: "99% off", DiscountPercent: 99,
	})
	set("minimax/minimax-m3-free", commandCodeCatalogDeal{
		Code: "minimax-free", Label: "Free", DiscountPercent: 100, Free: true,
		Term: "ends September 5, 2026", ExpiresAt: commandCodeMiniMaxFreeDealExpiry,
	})
	set("minimax/minimax-m2.7-free", commandCodeCatalogDeal{
		Code: "minimax-free", Label: "Free", DiscountPercent: 100, Free: true,
		Term: "ends September 5, 2026", ExpiresAt: commandCodeMiniMaxFreeDealExpiry,
	})
	set("poolside/laguna-s-2.1-free", commandCodeCatalogDeal{
		Code: "laguna-s-2.1-free", Label: "Free", DiscountPercent: 100, Free: true,
		Term: "while capacity lasts",
	})
}

func setCommandCodeFallbackTimeBands(entries map[string]commandCodeCatalogEntry) {
	effective := time.Date(2026, 8, 16, 16, 0, 0, 0, time.UTC)
	windows := []commandCodeCatalogTimeWindow{{StartHourUTC: 1, EndHourUTC: 4}, {StartHourUTC: 6, EndHourUTC: 10}}
	set := func(key string, peak, offPeak commandCodeCatalogRates) {
		entry := entries[key]
		entry.TimeOfDay = &commandCodeCatalogTimeOfDay{
			Effective: effective,
			Peak:      peak,
			OffPeak:   offPeak,
			Windows:   append([]commandCodeCatalogTimeWindow(nil), windows...),
		}
		entries[key] = entry
	}
	set("deepseek/deepseek-v4-pro", commandCodeRates(1.32, 3.96, 0.044), commandCodeRates(0.66, 1.98, 0.022))
	set("deepseek/deepseek-v4-flash", commandCodeRates(0.44, 1.32, 0.014), commandCodeRates(0.22, 0.66, 0.007))
	set("deepseek/deepseek-v4-flash-vision-exp", commandCodeRates(0.44, 1.32, 0.014), commandCodeRates(0.22, 0.66, 0.007))
}

// commandCodeSharedMonthlyQuotaUSD 是 GOAT 计划的官方月度额度池（USD）：$10 买 $70 credits。
const commandCodeSharedMonthlyQuotaUSD = 70.0

// commandCodeReferenceQuotaCost 返回模型当前月可用 credits 对应的额度成本乘数。
// 倍率 = 官方月度额度池 / 模型 Monthly credits（如 GPT-5.6 Luna：70/20 = 3.5x，
// 意味着 GOAT 额度按官方费率计费时，每 1 美元模型成本只消耗 1/3.5 的 credits 池）。
func commandCodeReferenceQuotaCost(model string) (OpenCodeGoQuotaCost, bool) {
	entry, ok := defaultCommandCodeCatalog.entry(model)
	if !ok {
		return OpenCodeGoQuotaCost{}, false
	}
	monthlyCreditsUSD := entry.MonthlyCreditsUSD
	if monthlyCreditsUSD <= 0 || math.IsNaN(monthlyCreditsUSD) || math.IsInf(monthlyCreditsUSD, 0) {
		return OpenCodeGoQuotaCost{}, false
	}
	multiplier := commandCodeSharedMonthlyQuotaUSD / monthlyCreditsUSD
	if multiplier <= 0 || math.IsNaN(multiplier) || math.IsInf(multiplier, 0) {
		return OpenCodeGoQuotaCost{}, false
	}
	return OpenCodeGoQuotaCost{
		IncludedMonthlyUsageUSD: monthlyCreditsUSD,
		Multiplier:              multiplier,
	}, true
}

// GetCommandCodeQuotaCost 返回 Command Code 模型当前月可用 credits 对应的额度成本乘数。
func (s *BillingService) GetCommandCodeQuotaCost(model string) (OpenCodeGoQuotaCost, bool) {
	for _, candidate := range billingModelPricingCandidates(model) {
		if quotaCost, ok := commandCodeReferenceQuotaCost(candidate); ok {
			return quotaCost, true
		}
	}
	return OpenCodeGoQuotaCost{}, false
}

func commandCodeReferencePricingAt(model string, now time.Time) (*ModelPricing, bool) {
	entry, ok := defaultCommandCodeCatalog.entry(model)
	if !ok {
		return nil, false
	}
	return commandCodeCatalogPricingAt(entry, now)
}

func commandCodeCatalogPricingAt(entry commandCodeCatalogEntry, now time.Time) (*ModelPricing, bool) {
	tiers, ok := commandCodeCatalogTiersAt(entry, now)
	if !ok || len(tiers) == 0 {
		return nil, false
	}
	if entry.ScheduledChange != nil && !now.Before(entry.ScheduledChange.Effective) {
		tiers[0].Rates = entry.ScheduledChange.Rates
	}
	if entry.TimeOfDay != nil && !now.Before(entry.TimeOfDay.Effective) {
		if commandCodeTimeOfDayIsPeak(entry.TimeOfDay, now) {
			tiers[0].Rates = entry.TimeOfDay.Peak
		} else {
			tiers[0].Rates = entry.TimeOfDay.OffPeak
		}
	}

	pricing := commandCodeModelPricingFromRates(tiers[0].Rates, entry.Deal != nil && entry.Deal.Free)
	if len(tiers) > 1 || tiers[0].MinTokens > 0 || tiers[0].MaxTokens != nil {
		pricing.IntervalsRequireMatch = true
		pricing.Intervals = make([]PricingInterval, 0, len(tiers))
		for _, tier := range tiers {
			pricing.Intervals = append(pricing.Intervals, commandCodePricingInterval(tier))
		}
	}
	return pricing, true
}

func commandCodeCatalogTiersAt(entry commandCodeCatalogEntry, now time.Time) ([]commandCodeCatalogTier, bool) {
	if !commandCodeCatalogEntryAvailableAt(entry, now) {
		return nil, false
	}
	expired := entry.Deal != nil && !entry.Deal.ExpiresAt.IsZero() && now.After(entry.Deal.ExpiresAt)
	tiers := make([]commandCodeCatalogTier, len(entry.Tiers))
	copy(tiers, entry.Tiers)
	if expired {
		for index := range tiers {
			if tiers[index].ListRates == nil {
				return nil, false
			}
			tiers[index].Rates = *tiers[index].ListRates
		}
	}
	return tiers, true
}

func commandCodeModelPricingFromRates(rates commandCodeCatalogRates, allowZero bool) *ModelPricing {
	pricing := &ModelPricing{
		InputPricePerToken:     rates.Input * 1e-6,
		OutputPricePerToken:    rates.Output * 1e-6,
		CacheReadPricePerToken: rates.CacheRead * 1e-6,
		AllowZeroRate:          allowZero,
	}
	if rates.HasCacheWrite {
		pricing.CacheCreationPricePerToken = rates.CacheWrite * 1e-6
		pricing.CacheCreationPriceExplicit = true
	}
	return pricing
}

func commandCodePricingInterval(tier commandCodeCatalogTier) PricingInterval {
	input := tier.Rates.Input * 1e-6
	output := tier.Rates.Output * 1e-6
	cacheRead := tier.Rates.CacheRead * 1e-6
	interval := PricingInterval{
		MinTokens:      tier.MinTokens,
		MaxTokens:      tier.MaxTokens,
		InputPrice:     &input,
		OutputPrice:    &output,
		CacheReadPrice: &cacheRead,
	}
	if tier.Rates.HasCacheWrite {
		cacheWrite := tier.Rates.CacheWrite * 1e-6
		interval.CacheWritePrice = &cacheWrite
	}
	return interval
}

func commandCodeReferenceMetadataAt(model string, now time.Time) (int, *ModelPromotion, bool) {
	entry, ok := defaultCommandCodeCatalog.entry(model)
	if !ok || !commandCodeCatalogEntryAvailableAt(entry, now) {
		return 0, nil, false
	}
	var promotion *ModelPromotion
	if entry.Deal != nil && (entry.Deal.ExpiresAt.IsZero() || !now.After(entry.Deal.ExpiresAt)) {
		promotion = &ModelPromotion{
			Code:            entry.Deal.Code,
			Label:           entry.Deal.Label,
			DiscountPercent: entry.Deal.DiscountPercent,
			Free:            entry.Deal.Free,
			Term:            entry.Deal.Term,
		}
		if !entry.Deal.ExpiresAt.IsZero() {
			expiresAt := entry.Deal.ExpiresAt
			promotion.ExpiresAt = &expiresAt
		}
	}
	return entry.ContextWindow, promotion, true
}

func commandCodeReferencePricingTimeBandsAt(model string, now time.Time) []ModelPricingTimeBand {
	entry, ok := defaultCommandCodeCatalog.entry(model)
	if !ok || entry.TimeOfDay == nil || now.Before(entry.TimeOfDay.Effective) ||
		!commandCodeCatalogEntryAvailableAt(entry, now) {
		return nil
	}
	return []ModelPricingTimeBand{
		{
			Code:       "off_peak",
			TimeZone:   "UTC",
			TimeRanges: commandCodeOffPeakTimeRanges(entry.TimeOfDay.Windows),
			Pricing:    commandCodeChannelPricingFromRates(entry.TimeOfDay.OffPeak),
		},
		{
			Code:       "peak",
			TimeZone:   "UTC",
			TimeRanges: commandCodePeakTimeRanges(entry.TimeOfDay.Windows),
			Pricing:    commandCodeChannelPricingFromRates(entry.TimeOfDay.Peak),
		},
	}
}

func commandCodeChannelPricingFromRates(rates commandCodeCatalogRates) *ChannelModelPricing {
	pricing := synthesizePricingFromModelPricing(commandCodeModelPricingFromRates(rates, false), nil)
	pricing.BillingMode = BillingModeToken
	return pricing
}

func commandCodeTimeOfDayIsPeak(schedule *commandCodeCatalogTimeOfDay, now time.Time) bool {
	if schedule == nil {
		return false
	}
	hour := now.UTC().Hour()
	for _, window := range schedule.Windows {
		if hour >= window.StartHourUTC && hour < window.EndHourUTC {
			return true
		}
	}
	return false
}

func commandCodePeakTimeRanges(windows []commandCodeCatalogTimeWindow) []string {
	ranges := make([]string, 0, len(windows))
	for _, window := range windows {
		ranges = append(ranges, fmt.Sprintf("%02d:00-%02d:00", window.StartHourUTC, window.EndHourUTC))
	}
	return ranges
}

func commandCodeOffPeakTimeRanges(windows []commandCodeCatalogTimeWindow) []string {
	ranges := make([]string, 0, len(windows)+1)
	cursor := 0
	for _, window := range windows {
		if cursor < window.StartHourUTC {
			ranges = append(ranges, fmt.Sprintf("%02d:00-%02d:00", cursor, window.StartHourUTC))
		}
		cursor = window.EndHourUTC
	}
	if cursor < 24 {
		ranges = append(ranges, fmt.Sprintf("%02d:00-24:00", cursor))
	}
	return ranges
}

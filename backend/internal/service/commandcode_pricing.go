package service

import (
	"strings"
	"time"
)

// Command Code 参考价格表。
// 来源：https://commandcode.ai/docs/resources/pricing-limits（官方每 1M token 价格，
// USD）。价格与官方 Provider API 计费一致（pay-as-you-go 无加价，促销自动生效）。
// 所有字段按每 1M token 计，构造 ModelPricing 时换算为每 token 价格。
type commandCodePriceEntry struct {
	input        float64
	output       float64
	cacheRead    float64
	cacheWrite   float64
	allowZero    bool // 明确零费率（免费模型），不得视为缺失价格
	longCtxAbove int  // 超过该 context token 数后整次会话按倍率计价（0 表示无分层）
	longCtxIn    float64
	longCtxOut   float64
}

// commandCodeDeepSeekPeakHoursUTC：DeepSeek V4 峰时窗口（UTC 01–04 与 06–10，
// 每天 7 小时）。峰时输入/输出/缓存读价格 ×2。
var commandCodeDeepSeekPeakModels = map[string]bool{
	"deepseek/deepseek-v4-flash-vision-exp": true,
	"deepseek/deepseek-v4-flash":            true,
	"deepseek/deepseek-v4-pro":              true,
}

// gemini-3.7-flash 50% 促销截止时间（UTC）。到期后恢复列表价。
var commandCodeGemini37FlashDealExpiry = time.Date(2026, 12, 31, 23, 59, 59, 0, time.UTC)

// minimax 免费模型促销截止时间（UTC）。
var commandCodeMiniMaxFreeDealExpiry = time.Date(2026, 9, 5, 23, 59, 59, 0, time.UTC)

var commandCodeReferencePrices = map[string]commandCodePriceEntry{
	// Anthropic（上游原生 /provider/v1/messages）
	"claude-sonnet-5":            {input: 2, output: 10, cacheRead: 0.2, cacheWrite: 2.5},
	"claude-sonnet-4-6":          {input: 3, output: 15, cacheRead: 0.3, cacheWrite: 3.75},
	"claude-sonnet-4-5-20250929": {input: 3, output: 15, cacheRead: 0.3, cacheWrite: 3.75},
	"claude-fable-5":             {input: 10, output: 50, cacheRead: 1, cacheWrite: 12.5},
	"claude-opus-5":              {input: 5, output: 25, cacheRead: 0.5, cacheWrite: 6.25},
	"claude-opus-4-8":            {input: 5, output: 25, cacheRead: 0.5, cacheWrite: 6.25},
	"claude-opus-4-7":            {input: 5, output: 25, cacheRead: 0.5, cacheWrite: 6.25},
	"claude-opus-4-6":            {input: 5, output: 25, cacheRead: 0.5, cacheWrite: 6.25},
	"claude-haiku-4-5-20251001":  {input: 1, output: 5, cacheRead: 0.1, cacheWrite: 1.25},

	// OpenAI
	"gpt-5.6-sol":   {input: 5, output: 30, cacheRead: 0.5, cacheWrite: 6.25},
	"gpt-5.6-terra": {input: 2, output: 12, cacheRead: 0.2, cacheWrite: 2.5},
	"gpt-5.6-luna":  {input: 0.2, output: 1.2, cacheRead: 0.02, cacheWrite: 0.25},
	"gpt-5.5":       {input: 5, output: 30, cacheRead: 0.5},
	"gpt-5.4":       {input: 2.5, output: 15, cacheRead: 0.25},
	"gpt-5.4-mini":  {input: 0.75, output: 4.5, cacheRead: 0.075},
	"gpt-5.3-codex": {input: 2, output: 8, cacheRead: 0.5},

	// DeepSeek（峰时窗口 ×2，见 commandCodeDeepSeekPeakModels）
	"deepseek/deepseek-v4-pro":              {input: 0.66, output: 1.98, cacheRead: 0.022},
	"deepseek/deepseek-v4-flash":            {input: 0.22, output: 0.66, cacheRead: 0.007},
	"deepseek/deepseek-v4-flash-vision-exp": {input: 0.22, output: 0.66, cacheRead: 0.007},

	// Moonshot
	"moonshotai/kimi-k3":                  {input: 3, output: 15, cacheRead: 0.3},
	"moonshotai/kimi-k2.7-code":           {input: 0.95, output: 4, cacheRead: 0.19},
	"moonshotai/kimi-k2.7-code-highspeed": {input: 1.9, output: 8, cacheRead: 0.38},
	"moonshotai/kimi-k2.6":                {input: 0.95, output: 4, cacheRead: 0.16},
	"moonshotai/kimi-k2.5":                {input: 0.6, output: 3, cacheRead: 0.1},

	// Z.ai
	"zai-org/glm-5.3":      {input: 1.4, output: 4.4, cacheRead: 0.26},
	"zai-org/glm-5.2":      {input: 1.4, output: 4.4, cacheRead: 0.26},
	"zai-org/glm-5.2-fast": {input: 3, output: 10.25, cacheRead: 0.5},
	"zai-org/glm-5.1":      {input: 1.4, output: 4.4, cacheRead: 0.26},
	"zai-org/glm-5":        {input: 1, output: 3.2, cacheRead: 0.2},

	// MiniMax（M3 50% 促销价；免费变体至 2026-09-05）
	"minimaxai/minimax-m3":      {input: 0.3, output: 1.2, cacheRead: 0.06},
	"minimaxai/minimax-m2.7":    {input: 0.3, output: 1.2, cacheRead: 0.06},
	"minimaxai/minimax-m2.5":    {input: 0.3, output: 1.2, cacheRead: 0.03},
	"minimax/minimax-m3-free":   {input: 0, output: 0, cacheRead: 0, allowZero: true},
	"minimax/minimax-m2.7-free": {input: 0, output: 0, cacheRead: 0, allowZero: true},

	// Xiaomi（99%/98% 促销价）
	"xiaomi/mimo-v2.5-pro": {input: 0.435, output: 0.87, cacheRead: 0.0036},
	"xiaomi/mimo-v2.5":     {input: 0.14, output: 0.28, cacheRead: 0.0028},

	// Qwen
	"qwen/qwen3.8-max":         {input: 2, output: 6, cacheRead: 0.25, cacheWrite: 2.5},
	"qwen/qwen3.8-27b":         {input: 0.4, output: 3, cacheRead: 0.04},
	"qwen/qwen3.7-max":         {input: 2.5, output: 7.5, cacheRead: 0.5, cacheWrite: 3.13},
	"qwen/qwen3.7-plus":        {input: 0.4, output: 1.6, cacheRead: 0.08, cacheWrite: 0.5},
	"qwen/qwen3.7-flash":       {input: 0.03, output: 0.13, cacheRead: 0.006, cacheWrite: 0.038},
	"qwen/qwen3.6-max-preview": {input: 1.3, output: 7.8, cacheRead: 0.26, cacheWrite: 1.63},
	"qwen/qwen3.6-plus":        {input: 0.5, output: 3, cacheRead: 0.1},

	// StepFun
	"stepfun/step-3.7-flash": {input: 0.2, output: 1.15, cacheRead: 0.04},
	"stepfun/step-3.5-flash": {input: 0.1, output: 0.3, cacheRead: 0.02},

	// Tencent
	"tencent/hy3-paid": {input: 0.14, output: 0.58, cacheRead: 0.035},

	// Google（gemini-3.7-flash 促销价，到期恢复列表价，见 commandCodeGemini37FlashDealExpiry）
	"google/gemini-3.7-flash":      {input: 0.75, output: 3.75, cacheRead: 0.075, cacheWrite: 0.04167},
	"google/gemini-3.6-flash":      {input: 1.5, output: 7.5, cacheRead: 0.15},
	"google/gemini-3.5-flash":      {input: 1.5, output: 9, cacheRead: 0.15},
	"google/gemini-3.5-flash-lite": {input: 0.3, output: 2.5, cacheRead: 0.03},
	"google/gemini-3.1-flash-lite": {input: 0.25, output: 1.5, cacheRead: 0.03},

	// 其他
	"sakana/fugu-ultra":                 {input: 5, output: 30, cacheRead: 0.5},
	"nvidia/nemotron-3-ultra-550b-a55b": {input: 0.6, output: 2.4, cacheRead: 0.12},
	"thinkingmachines/inkling":          {input: 1, output: 4.05, cacheRead: 0.17},
	"thinkingmachines/inkling-small":    {input: 0.5, output: 1.2, cacheRead: 0.1},
	"stealth/ox-alpha":                  {input: 0, output: 0, cacheRead: 0, allowZero: true},
	"poolside/laguna-s-2.1-free":        {input: 0, output: 0, cacheRead: 0, allowZero: true},
	"ling/ling-3.0-flash":               {input: 0, output: 0, cacheRead: 0, allowZero: true},
	"meta/muse-spark-1.1":               {input: 1.25, output: 4.25, cacheRead: 0.15},
	"meta/muse-spark-1.2":               {input: 1.25, output: 4.25, cacheRead: 0.15},
	"meta/muse-spark-1.2-contributor":   {input: 0.1, output: 0.2, cacheRead: 0.002},
	"xai/grok-4.5":                      {input: 2, output: 6, cacheRead: 0.5},
	"xai/grok-4.6":                      {input: 2, output: 6, cacheRead: 0.5},
}

// commandCodeModelAliases 把官方文档/CLI 常用的短名映射到 Provider API 模型 ID。
var commandCodeModelAliases = map[string]string{
	"claude-haiku-4-5":  "claude-haiku-4-5-20251001",
	"claude-sonnet-4-5": "claude-sonnet-4-5-20250929",

	"deepseek-v4-pro":              "deepseek/deepseek-v4-pro",
	"deepseek-v4-flash":            "deepseek/deepseek-v4-flash",
	"deepseek-v4-flash-vision-exp": "deepseek/deepseek-v4-flash-vision-exp",

	"kimi-k3":                  "moonshotai/kimi-k3",
	"kimi-k2.7-code":           "moonshotai/kimi-k2.7-code",
	"kimi-k2.7-code-highspeed": "moonshotai/kimi-k2.7-code-highspeed",
	"kimi-k2.6":                "moonshotai/kimi-k2.6",
	"kimi-k2.5":                "moonshotai/kimi-k2.5",

	"glm-5.3":      "zai-org/glm-5.3",
	"glm-5.2":      "zai-org/glm-5.2",
	"glm-5.2-fast": "zai-org/glm-5.2-fast",
	"glm-5.1":      "zai-org/glm-5.1",
	"glm-5":        "zai-org/glm-5",

	"minimax-m3":      "minimaxai/minimax-m3",
	"minimax-m2.7":    "minimaxai/minimax-m2.7",
	"minimax-m2.5":    "minimaxai/minimax-m2.5",
	"minimax-m3-free": "minimax/minimax-m3-free",

	"mimo-v2.5-pro": "xiaomi/mimo-v2.5-pro",
	"mimo-v2.5":     "xiaomi/mimo-v2.5",

	"qwen3.8-max":          "qwen/qwen3.8-max",
	"qwen3.8-27b":          "qwen/qwen3.8-27b",
	"qwen3.7-max":          "qwen/qwen3.7-max",
	"qwen3.7-plus":         "qwen/qwen3.7-plus",
	"qwen3.7-flash":        "qwen/qwen3.7-flash",
	"qwen3.6-max-preview":  "qwen/qwen3.6-max-preview",
	"qwen3.6-plus":         "qwen/qwen3.6-plus",
	"qwen-3.8-max":         "qwen/qwen3.8-max",
	"qwen-3.7-max":         "qwen/qwen3.7-max",
	"qwen-3.7-plus":        "qwen/qwen3.7-plus",
	"qwen-3.7-flash":       "qwen/qwen3.7-flash",
	"qwen-3.6-max-preview": "qwen/qwen3.6-max-preview",
	"qwen-3.6-plus":        "qwen/qwen3.6-plus",

	"ling-3.0-flash": "ling/ling-3.0-flash",
	"ling-3-flash":   "ling/ling-3.0-flash",

	"step-3.7-flash": "stepfun/step-3.7-flash",
	"step-3.5-flash": "stepfun/step-3.5-flash",

	"hy3-paid": "tencent/hy3-paid",

	"gemini-3.7-flash":      "google/gemini-3.7-flash",
	"gemini-3.6-flash":      "google/gemini-3.6-flash",
	"gemini-3.5-flash":      "google/gemini-3.5-flash",
	"gemini-3.5-flash-lite": "google/gemini-3.5-flash-lite",
	"gemini-3.1-flash-lite": "google/gemini-3.1-flash-lite",

	"fugu-ultra":                 "sakana/fugu-ultra",
	"nemotron-3-ultra-550b-a55b": "nvidia/nemotron-3-ultra-550b-a55b",
	"nemotron-3-ultra":           "nvidia/nemotron-3-ultra-550b-a55b",
	"inkling":                    "thinkingmachines/inkling",
	"inkling-small":              "thinkingmachines/inkling-small",
	"ox-alpha":                   "stealth/ox-alpha",
	"laguna-s-2.1-free":          "poolside/laguna-s-2.1-free",
	"laguna-s-2.1":               "poolside/laguna-s-2.1-free",

	"muse-spark-1.1":             "meta/muse-spark-1.1",
	"muse-spark-1.2":             "meta/muse-spark-1.2",
	"muse-spark-1.2-contributor": "meta/muse-spark-1.2-contributor",

	"grok-4.5": "xai/grok-4.5",
	"grok-4.6": "xai/grok-4.6",
}

// commandCodeCanonicalModelID 归一化 Command Code 模型标识：
// 小写化、剥离 commandcode/ 命名空间前缀，并把文档短名映射到 API 模型 ID。
func commandCodeCanonicalModelID(model string) string {
	key := strings.ToLower(strings.TrimSpace(model))
	key = strings.TrimPrefix(key, "commandcode/")
	if key == "" {
		return ""
	}
	if _, ok := commandCodeReferencePrices[key]; ok {
		return key
	}
	if aliased, ok := commandCodeModelAliases[key]; ok {
		return aliased
	}
	// 剥离一层 provider 前缀后再尝试（例如 openrouter/deepseek/deepseek-v4-flash）。
	if idx := strings.Index(key, "/"); idx >= 0 {
		suffix := key[idx+1:]
		if _, ok := commandCodeReferencePrices[suffix]; ok {
			return suffix
		}
		if aliased, ok := commandCodeModelAliases[suffix]; ok {
			return aliased
		}
	}
	return key
}

// commandCodeIsDeepSeekPeakUTC 判断给定时间是否处于 DeepSeek V4 峰时窗口
// （UTC 01–04 与 06–10，共 7 小时/天）。
func commandCodeIsDeepSeekPeakUTC(now time.Time) bool {
	hour := now.UTC().Hour()
	return (hour >= 1 && hour < 4) || (hour >= 6 && hour < 10)
}

// commandCodeReferencePricingAt 返回 Command Code 平台隔离的模型价格。
// ok 为 false 表示该模型不属于 Command Code 参考价表（调用方应 fail-closed）。
func commandCodeReferencePricingAt(model string, now time.Time) (*ModelPricing, bool) {
	key := commandCodeCanonicalModelID(model)
	if key == "" {
		return nil, false
	}
	entry, ok := commandCodeReferencePrices[key]
	if !ok {
		return nil, false
	}

	input := entry.input
	output := entry.output
	cacheRead := entry.cacheRead

	// DeepSeek V4 峰时价格 ×2（输入/输出/缓存读）。
	if commandCodeDeepSeekPeakModels[key] && commandCodeIsDeepSeekPeakUTC(now) {
		input *= 2
		output *= 2
		cacheRead *= 2
	}

	// gemini-3.7-flash 50% 促销到期后恢复列表价。
	if key == "google/gemini-3.7-flash" && !now.Before(commandCodeGemini37FlashDealExpiry) {
		input = 1.5
		output = 7.5
		cacheRead = 0.15
		entry.cacheWrite = 0.08334
	}

	// minimax 免费模型促销到期（2026-09-05）后恢复标准 MiniMax 价格。
	if (key == "minimax/minimax-m3-free" || key == "minimax/minimax-m2.7-free") && now.After(commandCodeMiniMaxFreeDealExpiry) {
		input = 0.3
		output = 1.2
		cacheRead = 0.06
		entry.allowZero = false
	}

	pricing := &ModelPricing{
		InputPricePerToken:     input * 1e-6,
		OutputPricePerToken:    output * 1e-6,
		CacheReadPricePerToken: cacheRead * 1e-6,
		AllowZeroRate:          entry.allowZero,
	}
	if entry.cacheWrite > 0 {
		pricing.CacheCreationPricePerToken = entry.cacheWrite * 1e-6
	}
	if entry.longCtxAbove > 0 && entry.longCtxIn > 0 && entry.longCtxOut > 0 {
		pricing.LongContextInputThreshold = entry.longCtxAbove
		pricing.LongContextInputMultiplier = entry.longCtxIn
		pricing.LongContextOutputMultiplier = entry.longCtxOut
	}
	return pricing, true
}

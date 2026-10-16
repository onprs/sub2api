package service

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// geminiThinkingParam 是上游模型接受的思考参数形态。
type geminiThinkingParam int

const (
	geminiThinkingParamNone geminiThinkingParam = iota
	geminiThinkingParamBudget
	geminiThinkingParamLevel
)

const (
	gemini25ProMaxThinkingBudget       = 32768
	gemini25ProMinThinkingBudget       = 128
	gemini25FlashMaxThinkingBudget     = 24576
	gemini25FlashLiteMinThinkingBudget = 512
)

// geminiThinkingParamForModel 按上游模型名判断思考参数形态：2.5 系列用 thinkingBudget，
// 3 及以上用 thinkingLevel。2.5 之前的版本、无法解析版本号的名字、图像/语音/向量类模型
// 不写思考参数。
func geminiThinkingParamForModel(model string) geminiThinkingParam {
	name := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(model)), "models/")
	rest, ok := strings.CutPrefix(name, "gemini-")
	if !ok {
		return geminiThinkingParamNone
	}
	for _, kind := range []string{"image", "tts", "embedding", "live", "audio"} {
		if strings.Contains(rest, kind) {
			return geminiThinkingParamNone
		}
	}
	version, _, _ := strings.Cut(rest, "-")
	majorText, minorText, _ := strings.Cut(version, ".")
	major, err := strconv.Atoi(majorText)
	if err != nil {
		return geminiThinkingParamNone
	}
	minor := 0
	if minorText != "" {
		if minor, err = strconv.Atoi(minorText); err != nil {
			return geminiThinkingParamNone
		}
	}
	switch {
	case major >= 3:
		return geminiThinkingParamLevel
	case major == 2 && minor >= 5:
		return geminiThinkingParamBudget
	default:
		return geminiThinkingParamNone
	}
}

// geminiModelNameCarriesThinkingTier 判断模型名是否以档位后缀结尾（如 -low/-high），
// 这类名字由上游按名字选档，不再叠加思考档位参数。
func geminiModelNameCarriesThinkingTier(model string) bool {
	name := strings.ToLower(strings.TrimSpace(model))
	for _, suffix := range []string{"-minimal", "-low", "-medium", "-high", "-xhigh", "-max", "-tiered"} {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return false
}

// claudeThinkingIntent 是 Anthropic 请求体里与思考相关的意图。
type claudeThinkingIntent struct {
	thinkingType string // enabled / adaptive / disabled / 空
	tier         string // minimal / low / medium / high / 空
	budgetTokens int    // thinking.type=enabled 时的 budget_tokens
}

// parseClaudeThinkingIntent 读取 Anthropic 请求的思考参数。thinking.type=disabled 优先于
// output_config.effort；effort 归一到 minimal/low/medium/high，未识别的取值视为未指定。
func parseClaudeThinkingIntent(claudeBody []byte) claudeThinkingIntent {
	intent := claudeThinkingIntent{
		thinkingType: strings.ToLower(strings.TrimSpace(gjson.GetBytes(claudeBody, "thinking.type").String())),
	}
	if intent.thinkingType == "disabled" {
		intent.tier = "minimal"
		return intent
	}
	switch strings.ToLower(strings.TrimSpace(gjson.GetBytes(claudeBody, "output_config.effort").String())) {
	case "none", "minimal":
		intent.tier = "minimal"
	case "low":
		intent.tier = "low"
	case "medium":
		intent.tier = "medium"
	case "high", "xhigh", "max":
		intent.tier = "high"
	}
	if intent.thinkingType == "enabled" {
		intent.budgetTokens = int(gjson.GetBytes(claudeBody, "thinking.budget_tokens").Int())
	}
	return intent
}

// claudeRequestEnablesThinking 判断 Anthropic 请求是否开启了思考输出。
func claudeRequestEnablesThinking(claudeBody []byte) bool {
	switch parseClaudeThinkingIntent(claudeBody).thinkingType {
	case "enabled", "adaptive":
		return true
	default:
		return false
	}
}

// geminiThinkingLevelFor 把思考意图映射为 thinkingLevel；返回空串表示交给模型默认档位。
// minimal 只发给 flash 系列，其余模型用 low；medium 只发给 flash 系列与 Gemini 3.1 Pro，
// 其余模型用 high（Gemini 3 及以上未指定档位时的默认档位）。
func geminiThinkingLevelFor(model string, intent claudeThinkingIntent) string {
	tier := intent.tier
	if tier == "" && intent.budgetTokens > 0 {
		switch {
		case intent.budgetTokens <= 1024:
			tier = "low"
		case intent.budgetTokens <= 8192:
			tier = "medium"
		default:
			tier = "high"
		}
	}
	name := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(model)), "models/")
	isFlash := strings.Contains(name, "flash")
	if tier == "minimal" && !isFlash {
		tier = "low"
	}
	if tier == "medium" && !isFlash && !strings.HasPrefix(name, "gemini-3.1-pro") {
		tier = "high"
	}
	return tier
}

// geminiThinkingBudgetFor 把思考意图映射为 Gemini 2.5 的 thinkingBudget；ok=false 表示交给
// 模型动态预算。Pro 不能关闭思考（最小 128，最大 32768），Flash 为 0–24576，
// Flash-Lite 的正预算最小 512。
func geminiThinkingBudgetFor(model string, intent claudeThinkingIntent) (budget int, ok bool) {
	name := strings.ToLower(model)
	isPro := strings.Contains(name, "pro")
	maxBudget, minPositive := gemini25FlashMaxThinkingBudget, 1
	switch {
	case isPro:
		maxBudget, minPositive = gemini25ProMaxThinkingBudget, gemini25ProMinThinkingBudget
	case strings.Contains(name, "flash-lite"):
		minPositive = gemini25FlashLiteMinThinkingBudget
	}
	switch intent.tier {
	case "minimal":
		if isPro {
			return gemini25ProMinThinkingBudget, true
		}
		return 0, true
	case "low":
		budget = 1024
	case "medium":
		budget = 8192
	case "high":
		budget = maxBudget
	default:
		if intent.budgetTokens <= 0 {
			return 0, false
		}
		budget = intent.budgetTokens
	}
	budget = min(max(budget, minPositive), maxBudget)
	return budget, true
}

// applyGeminiCompatThinkingConfig 把兼容入口（Anthropic 形态请求体）的思考意图写进转换后的
// Gemini 请求 generationConfig.thinkingConfig。includeThoughts 为 true 时要求上游返回
// 思考摘要（thought=true 的 part）；思考被关闭（预算为 0）时不要求。
func applyGeminiCompatThinkingConfig(geminiReq, claudeBody []byte, upstreamModel string, includeThoughts bool) []byte {
	param := geminiThinkingParamForModel(upstreamModel)
	if param == geminiThinkingParamNone {
		return geminiReq
	}
	intent := parseClaudeThinkingIntent(claudeBody)
	out := geminiReq
	thinkingOff := false
	if !geminiModelNameCarriesThinkingTier(upstreamModel) {
		switch param {
		case geminiThinkingParamLevel:
			if level := geminiThinkingLevelFor(upstreamModel, intent); level != "" {
				out = setGeminiThinkingField(out, "thinkingLevel", level)
			}
		case geminiThinkingParamBudget:
			if budget, ok := geminiThinkingBudgetFor(upstreamModel, intent); ok {
				out = setGeminiThinkingField(out, "thinkingBudget", budget)
				thinkingOff = budget == 0
			}
		}
	}
	if includeThoughts && !thinkingOff {
		out = setGeminiThinkingField(out, "includeThoughts", true)
	}
	return out
}

func setGeminiThinkingField(body []byte, field string, value any) []byte {
	updated, err := sjson.SetBytes(body, "generationConfig.thinkingConfig."+field, value)
	if err != nil {
		return body
	}
	return updated
}

// dropClaudeHistoryThinkingBlocks 删除 Anthropic 请求历史消息里的 thinking/redacted_thinking 块，
// 只含思考块的消息整条删除；与 convertClaudeMessagesToGeminiContents 的取舍一致，供签名降级
// 重试在把思考块改写成正文之前使用。
func dropClaudeHistoryThinkingBlocks(claudeBody []byte) []byte {
	var req map[string]any
	if err := json.Unmarshal(claudeBody, &req); err != nil {
		return claudeBody
	}
	messages, ok := req["messages"].([]any)
	if !ok {
		return claudeBody
	}
	changed := false
	kept := make([]any, 0, len(messages))
	for _, m := range messages {
		mm, ok := m.(map[string]any)
		if !ok {
			kept = append(kept, m)
			continue
		}
		blocks, ok := mm["content"].([]any)
		if !ok {
			kept = append(kept, m)
			continue
		}
		filtered := make([]any, 0, len(blocks))
		for _, block := range blocks {
			if bm, ok := block.(map[string]any); ok {
				if bt, _ := bm["type"].(string); bt == "thinking" || bt == "redacted_thinking" {
					continue
				}
			}
			filtered = append(filtered, block)
		}
		if len(filtered) == len(blocks) {
			kept = append(kept, m)
			continue
		}
		// 原消息是多块，转换器会过滤空白正文块；删掉思考块后同样过滤，避免剩下的单块空白正文被原样发出。
		nonBlank := filtered[:0]
		for _, block := range filtered {
			if bm, ok := block.(map[string]any); ok {
				if bt, _ := bm["type"].(string); bt == "text" {
					if text, _ := bm["text"].(string); strings.TrimSpace(text) == "" {
						continue
					}
				}
			}
			nonBlank = append(nonBlank, block)
		}
		filtered = nonBlank
		changed = true
		if len(filtered) == 0 {
			continue
		}
		mm["content"] = filtered
		kept = append(kept, mm)
	}
	if !changed {
		return claudeBody
	}
	req["messages"] = kept
	out, err := json.Marshal(req)
	if err != nil {
		return claudeBody
	}
	return out
}

// resetGeminiThinkingConfigForCompat 清空标准协议转换写入的 thinkingConfig：兼容入口的档位
// 规则（applyGeminiCompatThinkingConfig）是唯一来源，避免两套映射互相覆盖。
func resetGeminiThinkingConfigForCompat(body []byte) []byte {
	if !bytes.Contains(body, []byte(`"thinkingConfig"`)) {
		return body
	}
	reset, err := sjson.DeleteBytes(body, "generationConfig.thinkingConfig")
	if err != nil {
		return body
	}
	return reset
}

// geminiCompatThinkingIntentFromChatBody 把 Chat 请求的 reasoning_effort 归一为
// Anthropic 形态的思考意图，让 Chat 与 Messages 兼容入口共用同一套档位映射。
func geminiCompatThinkingIntentFromChatBody(body []byte) []byte {
	effort := strings.TrimSpace(gjson.GetBytes(body, "reasoning_effort").String())
	if effort == "" {
		effort = strings.TrimSpace(gjson.GetBytes(body, "reasoning.effort").String())
	}
	if effort == "" {
		return nil
	}
	intent, err := sjson.SetBytes([]byte(`{}`), "output_config.effort", effort)
	if err != nil {
		return nil
	}
	return intent
}

// applyGeminiCompatThinkingForChat 为 Chat 兼容入口写入 Gemini 思考档位。
// includeThoughts 跟随客户端是否流式：非流式不请求思考摘要。
func applyGeminiCompatThinkingForChat(geminiReq, chatBody []byte, upstreamModel string, stream bool) []byte {
	reset := resetGeminiThinkingConfigForCompat(geminiReq)
	return applyGeminiCompatThinkingConfig(reset, geminiCompatThinkingIntentFromChatBody(chatBody), upstreamModel, stream)
}

// dropChatReasoningHistory 删除 Chat 请求历史里的推理内容：Gemini 的思考连续性由
// thoughtSignature 承载，回传推理正文会让模型按上下文模仿，把思考写进可见正文。
func dropChatReasoningHistory(body []byte) []byte {
	messages := gjson.GetBytes(body, "messages")
	if !messages.IsArray() {
		return body
	}
	out := body
	changed := false
	for index, message := range messages.Array() {
		path := "messages." + strconv.Itoa(index)
		for _, field := range []string{"reasoning_content", "reasoning"} {
			if !message.Get(field).Exists() {
				continue
			}
			updated, err := sjson.DeleteBytes(out, path+"."+field)
			if err != nil {
				continue
			}
			out = updated
			changed = true
		}
		if message.Get("role").String() != "assistant" {
			continue
		}
		content := message.Get("content")
		if !content.IsArray() {
			continue
		}
		parts := content.Array()
		kept := make([]json.RawMessage, 0, len(parts))
		for _, part := range parts {
			switch part.Get("type").String() {
			case "thinking", "reasoning":
				continue
			}
			kept = append(kept, json.RawMessage(part.Raw))
		}
		if len(kept) == len(parts) {
			continue
		}
		encoded, err := json.Marshal(kept)
		if err != nil {
			continue
		}
		updated, err := sjson.SetRawBytes(out, path+".content", encoded)
		if err != nil {
			continue
		}
		out = updated
		changed = true
	}
	if !changed {
		return body
	}
	return out
}

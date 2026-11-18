package service

import (
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
)

func NormalizeOpenAICompatRequestedModel(model string) string {
	if base := openai.GPT62ModelID(model); base != "" {
		return base
	}
	if openai.IsGPT61TerraModelSpelling(model) {
		return "gpt-6.1-terra"
	}
	if openai.IsGPT61SolModelSpelling(model) {
		return "gpt-6.1-sol"
	}
	trimmed := strings.TrimSpace(model)
	if trimmed == "" {
		return ""
	}

	normalized, _, ok := splitOpenAICompatReasoningModel(trimmed)
	if !ok || normalized == "" {
		return trimmed
	}
	return normalized
}

func applyOpenAICompatModelNormalization(req *apicompat.AnthropicRequest) {
	if req != nil && (openai.GPT62ModelID(req.Model) != "" || openai.IsGPT61SolModelSpelling(req.Model) || openai.IsGPT61TerraModelSpelling(req.Model)) {
		canonical := openai.CanonicalizeOpenAIModelAliasSpelling(req.Model)
		baseModel := NormalizeOpenAICompatRequestedModel(req.Model)
		if effort, ok := strings.CutPrefix(canonical, baseModel+"-"); ok && effort != "openai-compact" {
			req.Model = baseModel
			if req.OutputConfig == nil {
				req.OutputConfig = &apicompat.AnthropicOutputConfig{}
			}
			if req.OutputConfig.Effort == "" {
				req.OutputConfig.Effort = effort
			}
			return
		}
	}

	if req == nil {
		return
	}

	originalModel := strings.TrimSpace(req.Model)
	if originalModel == "" {
		return
	}

	normalizedModel, derivedEffort, hasReasoningSuffix := splitOpenAICompatReasoningModel(originalModel)
	if hasReasoningSuffix && normalizedModel != "" {
		req.Model = normalizedModel
	}

	if req.OutputConfig != nil && strings.TrimSpace(req.OutputConfig.Effort) != "" {
		return
	}

	claudeEffort := openAIReasoningEffortToClaudeOutputEffort(derivedEffort)
	if claudeEffort == "" {
		return
	}

	if req.OutputConfig == nil {
		req.OutputConfig = &apicompat.AnthropicOutputConfig{}
	}
	req.OutputConfig.Effort = claudeEffort
}

func splitOpenAICompatReasoningModel(model string) (normalizedModel string, reasoningEffort string, ok bool) {
	trimmed := strings.TrimSpace(model)
	if trimmed == "" {
		return "", "", false
	}

	modelID := trimmed
	if strings.Contains(modelID, "/") {
		parts := strings.Split(modelID, "/")
		modelID = parts[len(parts)-1]
	}
	modelID = strings.TrimSpace(modelID)
	if !strings.HasPrefix(strings.ToLower(modelID), "gpt-") {
		return trimmed, "", false
	}

	parts := strings.FieldsFunc(strings.ToLower(modelID), func(r rune) bool {
		switch r {
		case '-', '_', ' ':
			return true
		default:
			return false
		}
	})
	if len(parts) == 0 {
		return trimmed, "", false
	}

	last := strings.NewReplacer("-", "", "_", "", " ", "").Replace(parts[len(parts)-1])
	switch last {
	case "none", "minimal":
	case "low", "medium", "high":
		reasoningEffort = last
	case "xhigh", "extrahigh":
		reasoningEffort = "xhigh"
	default:
		return trimmed, "", false
	}

	return normalizeCodexModel(modelID), reasoningEffort, true
}

func openAIReasoningEffortToClaudeOutputEffort(effort string) string {
	switch strings.TrimSpace(effort) {
	case "low", "medium", "high":
		return effort
	case "xhigh":
		return "max"
	default:
		return ""
	}
}

// openAIResponsesReasoningModel 判断上游模型是否只支持推理调用：GPT-5 及之后的
// 代际（含 GPT-6 sol/luna 的各个 effort 后缀）不接受 temperature/top_p。
func openAIResponsesReasoningModel(model string) bool {
	if openai.IsGPT6SolOrLunaModelSpelling(model) {
		return true
	}
	rest, ok := strings.CutPrefix(strings.ToLower(strings.TrimSpace(model)), "gpt-")
	if !ok {
		return false
	}
	major, digits := 0, 0
	for _, r := range rest {
		if r < '0' || r > '9' {
			break
		}
		major = major*10 + int(r-'0')
		digits++
	}
	return digits > 0 && major >= 5
}

// openAICompatAnthropicReasoningEffort resolves the effort emitted by the
// Anthropic bridge after the final upstream model is known. Anthropic's max is
// normally translated to OpenAI xhigh, but GPT-5.6 accepts the original max
// value on Responses and Chat Completions.
func openAICompatAnthropicReasoningEffort(req *apicompat.AnthropicRequest, upstreamModel, convertedEffort string) string {
	if req != nil && req.Thinking != nil && strings.EqualFold(strings.TrimSpace(req.Thinking.Type), "disabled") {
		return "none"
	}
	if convertedEffort == "none" {
		return convertedEffort
	}
	if req == nil || req.OutputConfig == nil || !strings.EqualFold(strings.TrimSpace(req.OutputConfig.Effort), "max") {
		return convertedEffort
	}
	if normalized := normalizeOpenAIReasoningEffortForModel(req.OutputConfig.Effort, upstreamModel); normalized != "" {
		return normalized
	}
	return convertedEffort
}

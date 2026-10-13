package service

import (
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	"github.com/tidwall/gjson"
)

func normalizeGatewayAnthropicThinking(req *apicompat.AnthropicRequest, model, requestedEffort string) {
	if req == nil || (!claude.IsOpus55(model) && !claude.IsSonnet55(model) && !claude.IsHaiku55(model)) {
		return
	}
	// Haiku 5.5 允许关闭思考；其他思考设置统一使用 adaptive。
	if claude.IsHaiku55(model) && strings.EqualFold(strings.TrimSpace(requestedEffort), "none") {
		req.Thinking = &apicompat.AnthropicThinking{Type: "disabled"}
		if req.OutputConfig == nil {
			req.OutputConfig = &apicompat.AnthropicOutputConfig{}
		}
		req.OutputConfig.Effort = "low"
		return
	}
	req.Thinking = &apicompat.AnthropicThinking{Type: "adaptive"}
}

func gatewayAnthropicForwardedReasoningEffort(body []byte, model string) *string {
	effort := NormalizeClaudeOutputEffort(gjson.GetBytes(body, "output_config.effort").String())
	return ApplyThinkingEnabledFallback(effort, body, model)
}

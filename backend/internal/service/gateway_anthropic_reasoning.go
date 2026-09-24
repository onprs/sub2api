package service

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	"github.com/tidwall/gjson"
)

func normalizeGatewayAnthropicThinking(req *apicompat.AnthropicRequest, model string) {
	if req == nil || !claude.IsOpus55(model) {
		return
	}
	req.Thinking = &apicompat.AnthropicThinking{Type: "adaptive"}
}

func gatewayAnthropicForwardedReasoningEffort(body []byte, model string) *string {
	effort := NormalizeClaudeOutputEffort(gjson.GetBytes(body, "output_config.effort").String())
	return ApplyThinkingEnabledFallback(effort, body, model)
}

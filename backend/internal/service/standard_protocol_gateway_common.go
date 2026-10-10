package service

import (
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// 本文件是 fork 保留的定制平台（ClinePass / OpenRouter / Command Code）
// 标准协议网关的共享代码，随这些平台引入，不包含 OpenCode 语义。

// standardGatewayErrorFormat 标识标准协议网关输出错误时使用的协议格式。
type standardGatewayErrorFormat string

const (
	standardGatewayErrorFormatChat      standardGatewayErrorFormat = "chat"
	standardGatewayErrorFormatResponses standardGatewayErrorFormat = "responses"
	standardGatewayErrorFormatAnthropic standardGatewayErrorFormat = "anthropic"
	standardGatewayErrorFormatGoogle    standardGatewayErrorFormat = "google"
)

// standardGatewayResponseMode 标识标准协议网关请求/响应的协议模式。
type standardGatewayResponseMode string

const (
	standardGatewayResponseChat      standardGatewayResponseMode = "chat"
	standardGatewayResponseResponses standardGatewayResponseMode = "responses"
	standardGatewayResponseAnthropic standardGatewayResponseMode = "anthropic"
	standardGatewayResponseGoogle    standardGatewayResponseMode = "google"
)

func (m standardGatewayResponseMode) protocol() protocolconv.Protocol {
	switch m {
	case standardGatewayResponseResponses:
		return protocolconv.ProtocolOpenAIResponses
	case standardGatewayResponseAnthropic:
		return protocolconv.ProtocolAnthropic
	case standardGatewayResponseGoogle:
		return protocolconv.ProtocolGoogleGenAI
	default:
		return protocolconv.ProtocolOpenAIChat
	}
}

func (m standardGatewayResponseMode) errorFormat() standardGatewayErrorFormat {
	return standardGatewayErrorFormatForProtocol(m.protocol())
}

func standardGatewayErrorFormatForProtocol(protocol protocolconv.Protocol) standardGatewayErrorFormat {
	switch protocol {
	case protocolconv.ProtocolOpenAIResponses:
		return standardGatewayErrorFormatResponses
	case protocolconv.ProtocolAnthropic:
		return standardGatewayErrorFormatAnthropic
	case protocolconv.ProtocolGoogleGenAI:
		return standardGatewayErrorFormatGoogle
	default:
		return standardGatewayErrorFormatChat
	}
}

func (f standardGatewayErrorFormat) protocol() protocolconv.Protocol {
	switch f {
	case standardGatewayErrorFormatResponses:
		return protocolconv.ProtocolOpenAIResponses
	case standardGatewayErrorFormatAnthropic:
		return protocolconv.ProtocolAnthropic
	case standardGatewayErrorFormatGoogle:
		return protocolconv.ProtocolGoogleGenAI
	default:
		return protocolconv.ProtocolOpenAIChat
	}
}

func claudeUsageFromChatBody(body []byte) ClaudeUsage {
	return normalizeGatewayChatUsage(ClaudeUsage{
		InputTokens:              int(gjson.GetBytes(body, "usage.prompt_tokens").Int()),
		OutputTokens:             int(gjson.GetBytes(body, "usage.completion_tokens").Int()),
		CacheCreationInputTokens: int(gjson.GetBytes(body, "usage.cache_creation_input_tokens").Int()),
		CacheReadInputTokens:     int(gjson.GetBytes(body, "usage.prompt_tokens_details.cached_tokens").Int()),
		ImageOutputTokens:        int(gjson.GetBytes(body, "usage.completion_tokens_details.image_tokens").Int()),
	})
}

func normalizeGatewayChatUsage(usage ClaudeUsage) ClaudeUsage {
	if usage.CacheReadInputTokens <= 0 {
		return usage
	}
	usage.InputTokens -= usage.CacheReadInputTokens
	if usage.InputTokens < 0 {
		usage.InputTokens = 0
	}
	return usage
}

func standardGatewayForwardResult(resp *http.Response, usage ClaudeUsage, model string, upstreamModel string, actualProtocol protocolconv.Protocol, stream bool, startTime time.Time) *ForwardResult {
	return &ForwardResult{
		RequestID:      resp.Header.Get("x-request-id"),
		ActualProtocol: actualProtocol,
		Usage:          usage,
		Model:          model,
		UpstreamModel:  optionalUpstreamModel(model, upstreamModel),
		Stream:         stream,
		Duration:       time.Since(startTime),
	}
}

func optionalUpstreamModel(model string, upstreamModel string) string {
	if strings.TrimSpace(upstreamModel) == "" || upstreamModel == model {
		return ""
	}
	return upstreamModel
}

func writeStandardGatewayError(c *gin.Context, status int, format standardGatewayErrorFormat, errType string, message string) {
	if c == nil || c.Writer == nil || c.Writer.Written() {
		return
	}
	renderer, err := protocolconv.NewRenderer(format.protocol())
	if err == nil {
		err = renderer.RenderError(c.Writer, status, errType, errType, message)
	}
	if err != nil {
		_ = c.Error(err)
	}
}

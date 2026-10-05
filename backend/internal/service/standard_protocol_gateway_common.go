package service

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv"
	protocoltransport "github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv/transport"
	"github.com/Wei-Shaw/sub2api/internal/util/responseheaders"
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

func standardGatewayUsageFromBody(body []byte, actualProtocol protocolconv.Protocol) ClaudeUsage {
	switch actualProtocol {
	case protocolconv.ProtocolAnthropic:
		return claudeUsageFromAnthropicBody(body)
	case protocolconv.ProtocolOpenAIResponses:
		if usage, ok := extractOpenAIUsageFromJSONBytes(body); ok {
			return claudeUsageFromOpenAIUsage(usage)
		}
	}
	return claudeUsageFromChatBody(body)
}

func claudeUsageFromOpenAIUsage(usage OpenAIUsage) ClaudeUsage {
	inputTokens := usage.InputTokens - usage.CacheReadInputTokens - usage.CacheCreationInputTokens
	if inputTokens < 0 {
		inputTokens = 0
	}
	return ClaudeUsage{
		InputTokens:              inputTokens,
		OutputTokens:             usage.OutputTokens,
		CacheCreationInputTokens: usage.CacheCreationInputTokens,
		CacheReadInputTokens:     usage.CacheReadInputTokens,
		CacheCreation5mTokens:    usage.CacheCreation5mTokens,
		CacheCreation1hTokens:    usage.CacheCreation1hTokens,
		ImageOutputTokens:        usage.ImageOutputTokens,
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

func claudeUsageFromAnthropicBody(body []byte) ClaudeUsage {
	return ClaudeUsage{
		InputTokens:              int(gjson.GetBytes(body, "usage.input_tokens").Int()),
		OutputTokens:             int(gjson.GetBytes(body, "usage.output_tokens").Int()),
		CacheCreationInputTokens: int(gjson.GetBytes(body, "usage.cache_creation_input_tokens").Int()),
		CacheReadInputTokens:     int(gjson.GetBytes(body, "usage.cache_read_input_tokens").Int()),
	}
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

func mergeStandardGatewayStreamUsage(usage *ClaudeUsage, payload []byte, actualProtocol protocolconv.Protocol) {
	if usage == nil {
		return
	}
	if actualProtocol == protocolconv.ProtocolAnthropic {
		var event apicompat.AnthropicStreamEvent
		if json.Unmarshal(payload, &event) != nil {
			return
		}
		if event.Type == "message_start" && event.Message != nil {
			mergeAnthropicUsage(usage, event.Message.Usage)
		}
		if event.Type == "message_delta" && event.Usage != nil {
			mergeAnthropicUsage(usage, *event.Usage)
		}
		return
	}
	if actualProtocol == protocolconv.ProtocolOpenAIResponses {
		if extracted, ok := extractOpenAIUsageFromJSONBytes(payload); ok {
			*usage = claudeUsageFromOpenAIUsage(extracted)
		}
		return
	}
	if extracted := extractCCStreamUsage(string(payload)); extracted != nil {
		*usage = normalizeGatewayChatUsage(ClaudeUsage{
			InputTokens:              extracted.InputTokens,
			OutputTokens:             extracted.OutputTokens,
			CacheReadInputTokens:     extracted.CacheReadInputTokens,
			CacheCreationInputTokens: extracted.CacheCreationInputTokens,
			ImageOutputTokens:        extracted.ImageOutputTokens,
		})
	}
}

func writeStandardGatewayUpstreamResponse(c *gin.Context, upstream protocoltransport.Response, filter *responseheaders.CompiledHeaderFilter, format standardGatewayErrorFormat) {
	if c == nil || c.Writer == nil {
		return
	}
	if filter != nil {
		responseheaders.WriteFilteredHeaders(c.Writer.Header(), upstream.Headers, filter)
	}
	// Existing Chat and Messages clients depend on raw provider error bodies.
	// New cross-protocol sources must receive their own standard error envelope.
	if format == standardGatewayErrorFormatResponses || format == standardGatewayErrorFormatGoogle {
		message := sanitizeUpstreamErrorMessage(strings.TrimSpace(extractUpstreamErrorMessage(upstream.Body)))
		if message == "" {
			message = http.StatusText(upstream.StatusCode)
		}
		writeStandardGatewayError(c, upstream.StatusCode, format, "upstream_error", message)
		return
	}
	c.Writer.Header().Set("Content-Type", "application/json")
	c.Writer.WriteHeader(upstream.StatusCode)
	_, _ = c.Writer.Write(upstream.Body)
}

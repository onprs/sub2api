package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv"
	protocoltransport "github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv/transport"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

// ForwardAsChatCompletions accepts an OpenAI Chat Completions API request body,
// converts it to Anthropic Messages format (chained via Responses format),
// forwards to the Anthropic upstream, and converts the response back to Chat
// Completions format. This enables Chat Completions clients to access Anthropic
// models through Anthropic platform groups.
func (s *GatewayService) ForwardAsChatCompletions(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	body []byte,
	parsed *ParsedRequest,
) (*ForwardResult, error) {
	startTime := time.Now()

	// 1. Parse Chat Completions request
	var ccReq apicompat.ChatCompletionsRequest
	if err := json.Unmarshal(body, &ccReq); err != nil {
		return nil, fmt.Errorf("parse chat completions request: %w", err)
	}
	originalModel := ccReq.Model
	clientStream := ccReq.Stream
	includeUsage := ccReq.StreamOptions != nil && ccReq.StreamOptions.IncludeUsage

	// 2. Resolve the upstream model before creating the request-scoped route.
	mappedModel := originalModel
	if account.Type == AccountTypeAPIKey || account.Type == AccountTypeServiceAccount {
		mappedModel = account.GetMappedModel(originalModel)
	}
	if mappedModel == originalModel && account.Platform == PlatformAnthropic && account.Type == AccountTypeServiceAccount {
		normalized := normalizeVertexAnthropicModelID(claude.NormalizeModelID(originalModel))
		if normalized != originalModel {
			mappedModel = normalized
		}
	} else if mappedModel == originalModel && account.Platform == PlatformAnthropic && account.Type != AccountTypeAPIKey {
		normalized := claude.NormalizeModelID(originalModel)
		if normalized != originalModel {
			mappedModel = normalized
		}
	}

	// 3. Convert Chat Completions → Anthropic with one pipeline retained for
	// the successful response or stream from this upstream attempt.
	pipeline, err := protocolconv.NewPipeline(standardProtocolRegistry, protocolconv.PipelineConfig{
		Route: protocolconv.Route{
			Source:         protocolconv.ProtocolOpenAIChat,
			IntendedTarget: protocolconv.ProtocolAnthropic,
			ClientModel:    originalModel,
			UpstreamModel:  mappedModel,
			Provider:       account.Platform,
			AccountID:      account.ID,
		},
		Options: protocolconv.Options{SourceModel: mappedModel, LossPolicy: protocolconv.LossError},
	})
	if err != nil {
		return nil, fmt.Errorf("create chat anthropic pipeline: %w", err)
	}
	convertedRequest, err := pipeline.ConvertRequest(body)
	if err != nil {
		return nil, fmt.Errorf("convert chat completions to anthropic: %w", err)
	}
	var anthropicReq apicompat.AnthropicRequest
	if err := json.Unmarshal(convertedRequest.Body, &anthropicReq); err != nil {
		return nil, fmt.Errorf("decode converted anthropic request: %w", err)
	}
	anthropicReq.Model = mappedModel
	anthropicReq.Stream = true
	reqStream := true

	logger.L().Debug("gateway forward_as_chat_completions: model mapping applied",
		zap.Int64("account_id", account.ID),
		zap.String("original_model", originalModel),
		zap.String("mapped_model", mappedModel),
		zap.Bool("client_stream", clientStream),
	)

	// 4. Marshal mapped Anthropic request body
	anthropicBody, err := json.Marshal(anthropicReq)
	if err != nil {
		return nil, fmt.Errorf("marshal anthropic request: %w", err)
	}

	// 6. Apply Claude Code mimicry for OAuth accounts.
	// Chat Completions 协议进来的请求永远不是 Claude Code 客户端，所以对 OAuth 账号
	// 必须完整执行 /v1/messages 主路径上的伪装链路（system 重写 + normalize + metadata 注入），
	// 否则会被 Anthropic 判为第三方应用并扣 extra usage。
	// 见 applyClaudeCodeOAuthMimicryToBody 的 godoc。
	isClaudeCode := false
	shouldMimicClaudeCode := account.IsOAuth() && !isClaudeCode

	if shouldMimicClaudeCode {
		anthropicBody = s.applyClaudeCodeOAuthMimicryToBody(ctx, c, account, anthropicBody, anthropicReq.System, mappedModel)
	}

	// 7. Enforce cache_control block limit
	anthropicBody = enforceCacheControlLimit(anthropicBody)

	// 8. Get access token
	token, tokenType, err := s.GetAccessToken(ctx, account)
	if err != nil {
		return nil, fmt.Errorf("get access token: %w", err)
	}

	// 9. Get proxy URL
	proxyURL := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}

	// 10. Build upstream request
	upstreamCtx, releaseUpstreamCtx := detachStreamUpstreamContext(ctx, reqStream)
	upstreamReq, _, err := s.buildUpstreamRequest(upstreamCtx, c, account, anthropicBody, token, tokenType, mappedModel, reqStream, shouldMimicClaudeCode)
	releaseUpstreamCtx()
	if err != nil {
		return nil, fmt.Errorf("build upstream request: %w", err)
	}

	// 11. Send request
	resp, err := s.httpUpstream.DoWithTLS(upstreamReq, proxyURL, account.ID, account.Concurrency, s.tlsFPProfileService.ResolveTLSProfile(account))
	if err != nil {
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		safeErr := sanitizeUpstreamErrorMessage(err.Error())
		setOpsUpstreamError(c, 0, safeErr, "")
		appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
			Platform:           account.Platform,
			AccountID:          account.ID,
			AccountName:        account.Name,
			UpstreamStatusCode: 0,
			Kind:               "request_error",
			Message:            safeErr,
		})
		writeGatewayCCError(c, http.StatusBadGateway, "server_error", "Upstream request failed")
		return nil, fmt.Errorf("upstream request failed: %s", safeErr)
	}

	// 12. Handle error response with failover
	if resp.StatusCode >= 400 {
		respBody, _ := s.readUpstreamErrorBody(resp)
		_ = resp.Body.Close()
		resp.Body = io.NopCloser(bytes.NewReader(respBody))

		upstreamMsg := strings.TrimSpace(extractUpstreamErrorMessage(respBody))
		upstreamMsg = sanitizeUpstreamErrorMessage(upstreamMsg)

		if s.shouldFailoverUpstreamError(resp.StatusCode) {
			appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
				Platform:           account.Platform,
				AccountID:          account.ID,
				AccountName:        account.Name,
				UpstreamStatusCode: resp.StatusCode,
				UpstreamRequestID:  resp.Header.Get("x-request-id"),
				Kind:               "failover",
				Message:            upstreamMsg,
			})
			if s.rateLimitService != nil {
				s.rateLimitService.HandleUpstreamError(ctx, account, resp.StatusCode, resp.Header, respBody, mappedModel)
			}
			return nil, &UpstreamFailoverError{
				StatusCode:   resp.StatusCode,
				ResponseBody: respBody,
			}
		}

		writeGatewayCCError(c, mapUpstreamStatusCode(resp.StatusCode), "server_error", upstreamMsg)
		return nil, fmt.Errorf("upstream error: %d %s", resp.StatusCode, upstreamMsg)
	}

	// 13. Extract reasoning effort from CC request body
	reasoningEffort := extractCCReasoningEffortFromBody(body)
	// 国产模型默认 effort 补充：本路径是客户端 CC 请求 → Anthropic 上游，
	// 如果上游是 passback-required 国产模型 (Kimi-anthropic / GLM-anthropic / MiniMax)
	// 且客户端在 body 里传了 thinking.type=enabled，补中默认 effort。
	reasoningEffort = ApplyThinkingEnabledFallback(reasoningEffort, body, mappedModel)

	// 14. Handle normal response
	// Read Anthropic SSE → convert to Responses events → convert to CC format
	var result *ForwardResult
	var handleErr error
	if clientStream {
		result, handleErr = s.handleCCStreamingFromAnthropic(resp, c, pipeline, originalModel, mappedModel, reasoningEffort, startTime, includeUsage)
	} else {
		result, handleErr = s.handleCCBufferedFromAnthropic(resp, c, pipeline, originalModel, mappedModel, reasoningEffort, startTime)
	}

	return result, handleErr
}

// extractCCReasoningEffortFromBody reads reasoning effort from a Chat Completions
// request body. It checks both nested (reasoning.effort) and flat (reasoning_effort)
// formats used by OpenAI-compatible clients.
func extractCCReasoningEffortFromBody(body []byte) *string {
	raw := strings.TrimSpace(gjson.GetBytes(body, "reasoning.effort").String())
	if raw == "" {
		raw = strings.TrimSpace(gjson.GetBytes(body, "reasoning_effort").String())
	}
	if raw == "" {
		return nil
	}
	normalized := normalizeOpenAIReasoningEffort(raw)
	if normalized == "" {
		return nil
	}
	return &normalized
}

// handleCCBufferedFromAnthropic reads Anthropic SSE events, assembles the full
// response, then converts Anthropic → Responses → Chat Completions.
func (s *GatewayService) handleCCBufferedFromAnthropic(
	resp *http.Response,
	c *gin.Context,
	pipeline *protocolconv.Pipeline,
	originalModel string,
	mappedModel string,
	reasoningEffort *string,
	startTime time.Time,
) (*ForwardResult, error) {
	stream, err := s.collectAnthropicProtocolStream(resp, startTime)
	if err != nil {
		return nil, err
	}
	defer func() { _ = stream.Close() }()
	requestID := stream.RequestID
	finalResp, usage, err := collectBufferedAnthropicResponse(stream)
	if err != nil {
		writeGatewayCCError(c, http.StatusBadGateway, "server_error", "Upstream stream ended without a response")
		return nil, err
	}
	anthropicResponseBody, err := json.Marshal(finalResp)
	if err != nil {
		return nil, fmt.Errorf("marshal anthropic response: %w", err)
	}
	converted, err := pipeline.ConvertResponse(anthropicResponseBody, stream.ActualProtocol)
	if err != nil {
		return nil, fmt.Errorf("convert anthropic response to chat completions: %w", err)
	}
	converted.Body = reverseToolNamesIfPresent(c, converted.Body)
	renderer, err := protocolconv.NewRenderer(protocolconv.ProtocolOpenAIChat)
	if err != nil {
		return nil, err
	}
	if err := renderer.RenderJSON(c.Writer, stream.StatusCode, stream.Headers, converted.Body); err != nil {
		return nil, fmt.Errorf("render chat completions response: %w", err)
	}

	return &ForwardResult{
		RequestID:       requestID,
		Usage:           usage,
		Model:           originalModel,
		UpstreamModel:   mappedModel,
		ReasoningEffort: reasoningEffort,
		Stream:          false,
		Duration:        time.Since(startTime),
	}, nil
}

func collectBufferedAnthropicResponse(stream *protocoltransport.Stream) (*apicompat.AnthropicResponse, ClaudeUsage, error) {
	var finalResp *apicompat.AnthropicResponse
	var usage ClaudeUsage
	terminalSeen := false
readLoop:
	for {
		record, err := stream.Events.Next(context.Background())
		if errors.Is(err, io.EOF) || errors.Is(err, protocoltransport.ErrSSEDone) {
			break
		}
		if err != nil {
			return nil, usage, fmt.Errorf("read anthropic stream: %w", err)
		}
		var event apicompat.AnthropicStreamEvent
		if err := json.Unmarshal(record.Data, &event); err != nil {
			return nil, usage, fmt.Errorf("decode anthropic stream event: %w", err)
		}
		switch event.Type {
		case "message_start":
			if event.Message != nil {
				finalResp = event.Message
				mergeAnthropicUsage(&usage, event.Message.Usage)
			}
		case "message_delta":
			if event.Usage != nil {
				mergeAnthropicUsage(&usage, *event.Usage)
			}
			if event.Delta != nil && event.Delta.StopReason != "" && finalResp != nil {
				finalResp.StopReason = event.Delta.StopReason
			}
		case "content_block_start":
			if event.ContentBlock != nil && finalResp != nil {
				finalResp.Content = append(finalResp.Content, *event.ContentBlock)
			}
		case "content_block_delta":
			if event.Delta != nil && finalResp != nil && event.Index != nil && *event.Index < len(finalResp.Content) {
				switch event.Delta.Type {
				case "text_delta":
					finalResp.Content[*event.Index].Text += event.Delta.Text
				case "thinking_delta":
					finalResp.Content[*event.Index].Thinking += event.Delta.Thinking
				case "input_json_delta":
					finalResp.Content[*event.Index].Input = appendRawJSON(finalResp.Content[*event.Index].Input, event.Delta.PartialJSON)
				}
			}
		case "message_stop":
			terminalSeen = true
			break readLoop
		}
	}
	if finalResp == nil {
		return nil, usage, errors.New("upstream stream ended without response")
	}
	if !terminalSeen {
		return nil, usage, errors.New("anthropic stream ended without message_stop")
	}
	finalResp.Usage = apicompat.AnthropicUsage{
		InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens,
		CacheCreationInputTokens: usage.CacheCreationInputTokens, CacheReadInputTokens: usage.CacheReadInputTokens,
	}
	return finalResp, usage, nil
}

// handleCCStreamingFromAnthropic reads Anthropic SSE events, converts each
// to Responses events, then to Chat Completions chunks, and writes them.
func (s *GatewayService) handleCCStreamingFromAnthropic(
	resp *http.Response,
	c *gin.Context,
	pipeline *protocolconv.Pipeline,
	originalModel string,
	mappedModel string,
	reasoningEffort *string,
	startTime time.Time,
	includeUsage bool,
) (*ForwardResult, error) {
	stream, err := s.collectAnthropicProtocolStream(resp, startTime)
	if err != nil {
		return nil, err
	}
	defer func() { _ = stream.Close() }()
	requestID := stream.RequestID
	session, err := pipeline.NewStreamProcessor(stream.ActualProtocol)
	if err != nil {
		return nil, err
	}
	renderer, err := protocolconv.NewRenderer(protocolconv.ProtocolOpenAIChat)
	if err != nil {
		return nil, err
	}
	var usage ClaudeUsage
	var firstTokenMs *int
	headersWritten := false

	resultWithUsage := func() *ForwardResult {
		return &ForwardResult{
			RequestID:       requestID,
			Usage:           usage,
			Model:           originalModel,
			UpstreamModel:   mappedModel,
			ReasoningEffort: reasoningEffort,
			Stream:          true,
			Duration:        time.Since(startTime),
			FirstTokenMs:    firstTokenMs,
		}
	}
	writePayloads := func(payloads [][]byte) (bool, error) {
		var visible [][]byte
		for _, payload := range payloads {
			if includeUsage || !isOpenAIChatUsageOnlyStreamChunk(string(payload)) {
				visible = append(visible, payload)
			}
		}
		if len(visible) == 0 {
			return false, nil
		}
		if !headersWritten {
			if err := renderer.WriteStreamHeaders(c.Writer, stream.StatusCode, stream.Headers); err != nil {
				return false, err
			}
			headersWritten = true
		}
		for _, payload := range visible {
			payload = reverseToolNamesIfPresent(c, payload)
			framed, err := renderer.FrameStreamEvent(payload)
			if err != nil {
				return false, err
			}
			if _, err := c.Writer.Write(framed); err != nil {
				return true, nil
			}
		}
		c.Writer.Flush()
		return false, nil
	}

	for {
		record, err := stream.Events.Next(context.Background())
		if errors.Is(err, io.EOF) || errors.Is(err, protocoltransport.ErrSSEDone) {
			break
		}
		if err != nil {
			return resultWithUsage(), fmt.Errorf("read anthropic stream: %w", err)
		}
		var event apicompat.AnthropicStreamEvent
		if err := json.Unmarshal(record.Data, &event); err != nil {
			return resultWithUsage(), fmt.Errorf("decode anthropic stream event: %w", err)
		}
		if firstTokenMs == nil {
			ms := int(time.Since(startTime).Milliseconds())
			firstTokenMs = &ms
		}
		if event.Type == "message_delta" && event.Usage != nil {
			mergeAnthropicUsage(&usage, *event.Usage)
		}
		if event.Type == "message_start" && event.Message != nil {
			mergeAnthropicUsage(&usage, event.Message.Usage)
		}
		converted, _, err := session.Convert(record.Data)
		if err != nil {
			return resultWithUsage(), fmt.Errorf("convert anthropic stream event: %w", err)
		}
		disconnected, err := writePayloads(converted)
		if err != nil || disconnected {
			return resultWithUsage(), err
		}
		if event.Type == "message_stop" {
			break
		}
	}
	finalPayloads, _, err := session.Finalize()
	if err != nil {
		return resultWithUsage(), err
	}
	disconnected, err := writePayloads(finalPayloads)
	if err != nil || disconnected {
		return resultWithUsage(), err
	}
	if !headersWritten {
		if err := renderer.WriteStreamHeaders(c.Writer, stream.StatusCode, stream.Headers); err != nil {
			return resultWithUsage(), err
		}
	}
	if terminal := renderer.StreamTerminal(); len(terminal) > 0 {
		_, _ = c.Writer.Write(terminal)
	}
	c.Writer.Flush()
	return resultWithUsage(), nil
}

// writeGatewayCCError writes an error in OpenAI Chat Completions format for
// the Anthropic-upstream CC forwarding path.
func writeGatewayCCError(c *gin.Context, statusCode int, errType, message string) {
	MarkResponseCommitted(c)
	c.JSON(statusCode, gin.H{
		"error": gin.H{
			"type":    errType,
			"message": message,
		},
	})
}

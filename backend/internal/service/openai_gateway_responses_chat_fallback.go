package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv"
	protocoltransport "github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv/transport"
	"github.com/Wei-Shaw/sub2api/internal/util/responseheaders"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// forwardResponsesViaRawChatCompletions serves /v1/responses clients through an
// upstream that only supports /v1/chat/completions.
func (s *OpenAIGatewayService) forwardResponsesViaRawChatCompletionsWithOutput(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	body []byte,
	output openAIProtocolOutput,
) (*OpenAIForwardResult, error) {
	startTime := time.Now()

	var responsesReq apicompat.ResponsesRequest
	if err := json.Unmarshal(body, &responsesReq); err != nil {
		writeOpenAIResponsesFallbackError(c, http.StatusBadRequest, "invalid_request_error", "Failed to parse request body")
		return nil, fmt.Errorf("parse responses request: %w", err)
	}
	originalModel := strings.TrimSpace(responsesReq.Model)
	if originalModel == "" {
		writeOpenAIResponsesFallbackError(c, http.StatusBadRequest, "invalid_request_error", "model is required")
		return nil, fmt.Errorf("missing model in request")
	}

	clientStream := responsesReq.Stream
	billingModel, upstreamModel := resolveOpenAIForwardMappedModels(account, originalModel, false)
	pipeline, err := protocolconv.NewPipeline(standardProtocolRegistry, protocolconv.PipelineConfig{
		Route: protocolconv.Route{
			Source:         protocolconv.ProtocolOpenAIResponses,
			IntendedTarget: protocolconv.ProtocolOpenAIChat,
			ClientModel:    originalModel,
			UpstreamModel:  upstreamModel,
			Provider:       account.Platform,
			AccountID:      account.ID,
		},
		Options: protocolconv.Options{SourceModel: upstreamModel, LossPolicy: protocolconv.LossError},
	})
	if err != nil {
		return nil, fmt.Errorf("create responses chat fallback pipeline: %w", err)
	}
	// 历史中带明文 summary 的 reasoning item 先刷新缓存；只有密文的条目再从
	// 请求外的缓存补回明文，之后仍由 request-scoped Pipeline 完成协议转换。
	s.recacheReasoningItemsFromInput(responsesReq.Input)
	body = s.hydrateResponsesReasoningInputFromCache(body)
	convertedRequest, err := pipeline.ConvertRequest(body)
	if err != nil {
		writeOpenAIResponsesFallbackError(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return nil, fmt.Errorf("convert responses to chat completions: %w", err)
	}
	var chatReq apicompat.ChatCompletionsRequest
	if err := json.Unmarshal(convertedRequest.Body, &chatReq); err != nil {
		return nil, fmt.Errorf("decode converted chat completions request: %w", err)
	}
	reasoningEffort := extractOpenAIReasoningEffortFromBody(body, upstreamModel, billingModel, originalModel)
	// 国产模型默认 effort 补充：需要 mappedModel 判定，推迟到 billingModel 算出之后。
	reasoningEffort = ApplyThinkingEnabledFallback(reasoningEffort, body, billingModel)
	chatReq.Model = upstreamModel
	if clientStream {
		chatReq.StreamOptions = &apicompat.ChatStreamOptions{IncludeUsage: true}
	}

	chatBody, err := json.Marshal(chatReq)
	if err != nil {
		return nil, fmt.Errorf("marshal chat completions fallback request: %w", err)
	}
	chatBody, err = s.applyOpenAIFastPolicyToBody(ctx, account, upstreamModel, chatBody)
	if err != nil {
		var blocked *OpenAIFastBlockedError
		if errors.As(err, &blocked) {
			writeOpenAIFastPolicyBlockedResponse(c, blocked)
		}
		return nil, err
	}
	// Keep the final outbound tier for usage-time reconciliation. A policy
	// filter that removes the field therefore leaves this nil.
	serviceTier := extractOpenAIServiceTierFromBody(chatBody)

	logger.L().Debug("openai responses: forwarding via raw chat completions",
		zap.Int64("account_id", account.ID),
		zap.String("original_model", originalModel),
		zap.String("billing_model", billingModel),
		zap.String("upstream_model", upstreamModel),
		zap.Bool("stream", clientStream),
	)
	SetOpsUpstreamModel(c, upstreamModel)

	// Build and send upstream request via the shared CC pipeline
	apiKey, targetURL, err := s.resolveCCFallbackTarget(account)
	if err != nil {
		return nil, err
	}
	resp, err := s.sendCCUpstreamRequest(ctx, c, account, targetURL, chatBody, clientStream, apiKey, account.GetOpenAIUserAgent(), "")
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		defer func() { _ = resp.Body.Close() }()
		upstream, upstreamMsg, collectErr := s.collectOpenAIStructuredUpstreamError(resp, protocolconv.ProtocolOpenAIChat, clientStream)
		if collectErr != nil {
			return nil, collectErr
		}
		if foErr := s.failoverOpenAIStructuredUpstreamError(ctx, c, account, upstream, upstreamMsg, upstreamModel); foErr != nil {
			return nil, foErr
		}
		return s.handleStructuredErrorResponse(ctx, upstream, c, account, chatBody, billingModel)
	}

	if clientStream {
		return s.streamChatCompletionsAsResponses(c, resp, pipeline, originalModel, billingModel, upstreamModel, reasoningEffort, serviceTier, startTime, output)
	}
	defer func() { _ = resp.Body.Close() }()
	return s.bufferChatCompletionsAsResponses(c, resp, pipeline, originalModel, billingModel, upstreamModel, reasoningEffort, serviceTier, startTime, output)
}

func (s *OpenAIGatewayService) bufferChatCompletionsAsResponses(
	c *gin.Context,
	resp *http.Response,
	pipeline *protocolconv.Pipeline,
	originalModel string,
	billingModel string,
	upstreamModel string,
	reasoningEffort *string,
	serviceTier *string,
	startTime time.Time,
	output openAIProtocolOutput,
) (*OpenAIForwardResult, error) {
	ccResp, usage, err := s.readCCUpstreamJSONResponse(c, resp, writeOpenAIResponsesFallbackError)
	if err != nil {
		return nil, err
	}
	upstreamBody, err := json.Marshal(ccResp)
	if err != nil {
		return nil, err
	}

	structured, result, err := s.collectBufferedChatCompletionsResponse(
		c,
		resp.StatusCode,
		resp.Header,
		upstreamBody,
		usage,
		originalModel,
		billingModel,
		upstreamModel,
		reasoningEffort,
		serviceTier,
		startTime,
	)
	if err != nil {
		writeOpenAIResponsesFallbackError(c, http.StatusBadGateway, "api_error", "Failed to collect upstream response")
		return nil, err
	}
	if output != nil {
		if err := output.WriteResponse(structured); err != nil {
			return nil, fmt.Errorf("render structured Chat Completions result: %w", err)
		}
		return result, nil
	}
	convertedResponse, err := pipeline.ConvertResponse(structured.Body, structured.ActualProtocol)
	if err != nil {
		writeOpenAIResponsesFallbackError(c, http.StatusBadGateway, "api_error", "Failed to convert upstream response")
		return nil, fmt.Errorf("convert responses fallback response: %w", err)
	}
	s.cacheReasoningItemsFromPayloads(convertedResponse.Body)
	downstreamBody := convertedResponse.Body
	renderer, err := protocolconv.NewRenderer(protocolconv.ProtocolOpenAIResponses)
	if err != nil {
		return nil, err
	}
	if err := renderer.RenderJSON(c.Writer, http.StatusOK, structured.Headers, downstreamBody); err != nil {
		return nil, fmt.Errorf("render responses fallback result: %w", err)
	}
	return result, nil
}

func (s *OpenAIGatewayService) collectBufferedChatCompletionsResponse(
	c *gin.Context,
	statusCode int,
	headers http.Header,
	upstreamBody []byte,
	usage OpenAIUsage,
	originalModel string,
	billingModel string,
	upstreamModel string,
	reasoningEffort *string,
	serviceTier *string,
	startTime time.Time,
) (protocoltransport.Response, *OpenAIForwardResult, error) {
	var ccResp apicompat.ChatCompletionsResponse
	if err := json.Unmarshal(upstreamBody, &ccResp); err != nil {
		return protocoltransport.Response{}, nil, fmt.Errorf("parse buffered chat completions response: %w", err)
	}
	filteredHeaders := make(http.Header)
	if s.responseHeaderFilter != nil {
		filteredHeaders = responseheaders.FilterHeaders(headers, s.responseHeaderFilter)
	}
	structured := protocoltransport.Response{
		StatusCode:     statusCode,
		Headers:        filteredHeaders,
		Body:           append([]byte(nil), upstreamBody...),
		ActualProtocol: protocolconv.ProtocolOpenAIChat,
		RequestID:      headers.Get("x-request-id"),
		ResponseID:     ccResp.ID,
		Duration:       time.Since(startTime),
	}
	if err := structured.Validate(); err != nil {
		return protocoltransport.Response{}, nil, fmt.Errorf("validate responses fallback result: %w", err)
	}
	result := &OpenAIForwardResult{
		RequestID:                   structured.RequestID,
		UpstreamHeaders:             headers,
		ResponseID:                  structured.ResponseID,
		ActualProtocol:              structured.ActualProtocol,
		Usage:                       usage,
		Model:                       originalModel,
		BillingModel:                billingModel,
		UpstreamModel:               upstreamModel,
		ReasoningEffort:             reasoningEffort,
		UpstreamResponseServiceTier: observedUpstreamResponseServiceTier(c),
		ServiceTier:                 resolvedOpenAIUpstreamServiceTier(c, serviceTier),
		Stream:                      false,
		Duration:                    structured.Duration,
	}
	return structured, result, nil

}

func (s *OpenAIGatewayService) streamChatCompletionsAsResponses(
	c *gin.Context,
	resp *http.Response,
	pipeline *protocolconv.Pipeline,
	originalModel string,
	billingModel string,
	upstreamModel string,
	reasoningEffort *string,
	serviceTier *string,
	startTime time.Time,
	output openAIProtocolOutput,
) (*OpenAIForwardResult, error) {
	stream, err := s.collectCCUpstreamStream(resp, startTime)
	if err != nil {
		return nil, err
	}
	defer func() { _ = stream.Close() }()
	requestID := stream.RequestID
	if output != nil {
		result, err := s.streamChatCompletionsWithProtocolOutput(c, stream, output, originalModel, billingModel, upstreamModel, reasoningEffort, serviceTier, startTime)
		if result != nil {
			result.UpstreamHeaders = resp.Header
		}
		return result, err
	}
	session, err := pipeline.NewStreamProcessor(stream.ActualProtocol)
	if err != nil {
		return nil, fmt.Errorf("create responses fallback stream processor: %w", err)
	}
	renderer, err := protocolconv.NewRenderer(protocolconv.ProtocolOpenAIResponses)
	if err != nil {
		return nil, err
	}

	headersWritten := false
	clientDisconnected := false
	writePayloads := func(payloads [][]byte) error {
		if clientDisconnected || len(payloads) == 0 {
			return nil
		}
		if !headersWritten {
			if err := renderer.WriteStreamHeaders(c.Writer, stream.StatusCode, stream.Headers); err != nil {
				return err
			}
			headersWritten = true
		}
		for _, payload := range payloads {
			framed, err := renderer.FrameStreamEvent(payload)
			if err != nil {
				return err
			}
			if _, err := c.Writer.Write(framed); err != nil {
				clientDisconnected = true
				logger.L().Debug("openai responses chat fallback: client disconnected, continuing to drain upstream for billing",
					zap.Error(err),
					zap.String("request_id", requestID),
				)
				return nil
			}
		}
		c.Writer.Flush()
		return nil
	}

	var conversionErr error
	scan := s.scanCCStreamEvents(stream, "openai responses chat fallback", startTime, func(chunk *apicompat.ChatCompletionsChunk) {
		if conversionErr != nil {
			return
		}
		raw, err := json.Marshal(chunk)
		if err != nil {
			conversionErr = err
			return
		}
		if observer := upstreamResponseModelObserverFromContext(c); observer != nil {
			observer.ObserveOpenAI(raw, "")
		}
		payloads, _, err := session.Convert(raw)
		if err != nil {
			conversionErr = err
			return
		}
		for _, payload := range payloads {
			s.cacheReasoningItemsFromPayloads(payload)
		}
		conversionErr = writePayloads(payloads)
	})
	result := &OpenAIForwardResult{
		RequestID:                   requestID,
		UpstreamHeaders:             resp.Header,
		ResponseID:                  scan.ResponseID,
		ActualProtocol:              stream.ActualProtocol,
		Usage:                       scan.Usage,
		Model:                       originalModel,
		BillingModel:                billingModel,
		UpstreamModel:               upstreamModel,
		ReasoningEffort:             reasoningEffort,
		UpstreamResponseServiceTier: observedUpstreamResponseServiceTier(c),
		ServiceTier:                 resolvedOpenAIUpstreamServiceTier(c, serviceTier),
		Stream:                      true,
		Duration:                    time.Since(startTime),
		FirstTokenMs:                scan.FirstTokenMs,
	}
	if scan.Err != nil {
		return result, fmt.Errorf("stream usage incomplete: %w", scan.Err)
	}
	if conversionErr != nil {
		return result, fmt.Errorf("convert responses fallback stream: %w", conversionErr)
	}

	finalPayloads, _, err := session.Finalize()
	if err != nil {
		return result, fmt.Errorf("finalize responses fallback stream: %w", err)
	}
	for _, payload := range finalPayloads {
		s.cacheReasoningItemsFromPayloads(payload)
	}
	if err := writePayloads(finalPayloads); err != nil {
		return result, fmt.Errorf("render responses fallback stream: %w", err)
	}
	if !clientDisconnected {
		if !headersWritten {
			if err := renderer.WriteStreamHeaders(c.Writer, stream.StatusCode, stream.Headers); err != nil {
				return result, err
			}
			headersWritten = true
		}
		if terminal := renderer.StreamTerminal(); len(terminal) > 0 {
			if _, err := c.Writer.Write(terminal); err != nil {
				clientDisconnected = true
			}

		}
		if !clientDisconnected {
			c.Writer.Flush()
		}
	}
	if !scan.SawDone {
		logCCStreamMissingDoneSentinel("openai responses chat fallback", requestID)
	}
	result.ClientDisconnect = clientDisconnected
	return result, nil
}

func (s *OpenAIGatewayService) streamChatCompletionsWithProtocolOutput(
	c *gin.Context,
	stream *protocoltransport.Stream,
	output openAIProtocolOutput,
	originalModel string,
	billingModel string,
	upstreamModel string,
	reasoningEffort *string,
	serviceTier *string,
	startTime time.Time,
) (*OpenAIForwardResult, error) {
	headersWritten := false
	var outputErr error
	scan := s.scanCCStreamEvents(stream, "openai structured chat fallback", startTime, func(chunk *apicompat.ChatCompletionsChunk) {
		if outputErr != nil {
			return
		}
		raw, err := json.Marshal(chunk)
		if err != nil {
			outputErr = err
			return
		}
		if observer := upstreamResponseModelObserverFromContext(c); observer != nil {
			observer.ObserveOpenAI(raw, "")
		}
		if !headersWritten {
			if err := output.WriteStreamHeaders(stream.StatusCode, stream.Headers, stream.ActualProtocol); err != nil {
				outputErr = err
				return
			}
			headersWritten = true
		}
		outputErr = output.WriteStreamEvent(stream.ActualProtocol, raw)
	})
	result := &OpenAIForwardResult{
		RequestID:                   stream.RequestID,
		ResponseID:                  scan.ResponseID,
		ActualProtocol:              stream.ActualProtocol,
		Usage:                       scan.Usage,
		Model:                       originalModel,
		BillingModel:                billingModel,
		UpstreamModel:               upstreamModel,
		ReasoningEffort:             reasoningEffort,
		UpstreamResponseServiceTier: observedUpstreamResponseServiceTier(c),
		ServiceTier:                 resolvedOpenAIUpstreamServiceTier(c, serviceTier),
		Stream:                      true,
		Duration:                    time.Since(startTime),
		FirstTokenMs:                scan.FirstTokenMs,
	}
	if scan.Err != nil {
		return result, fmt.Errorf("stream usage incomplete: %w", scan.Err)
	}
	if outputErr != nil {
		return result, fmt.Errorf("render structured Chat Completions stream: %w", outputErr)
	}
	if !headersWritten {
		if err := output.WriteStreamHeaders(stream.StatusCode, stream.Headers, stream.ActualProtocol); err != nil {
			return result, err
		}
	}
	if err := output.FinalizeStream(stream.ActualProtocol); err != nil {
		return result, fmt.Errorf("finalize structured Chat Completions stream: %w", err)
	}
	result.ClientDisconnect = output.ClientDisconnected()
	if !scan.SawDone {
		logCCStreamMissingDoneSentinel("openai structured chat fallback", stream.RequestID)
	}
	return result, nil

}

func chatChunkStartsResponsesOutput(chunk *apicompat.ChatCompletionsChunk) bool {
	if chunk == nil {
		return false
	}
	for _, choice := range chunk.Choices {
		if choice.Delta.Content != nil || choice.Delta.ReasoningContent != nil || len(choice.Delta.ToolCalls) > 0 {
			return true
		}
	}
	return false
}

// responsesReasoningCacheTTL 是 reasoning 缓存（按 reasoning item id）的过期时间。
// Codex 会话可能跨多天恢复历史，取 7 天。
const responsesReasoningCacheTTL = 7 * 24 * time.Hour

// reasoningContentByID 按 reasoning item id 回查缓存的 reasoning 全文，供
// Responses→CC 桥接在客户端不回传明文 summary（encrypted-only reasoning
// item）时回注 reasoning_content。任何失败都 fail-open 返回 ""（维持桥接原
// 行为），因为缓存只是优化而非正确性前提。
func (s *OpenAIGatewayService) reasoningContentByID(itemID string) string {
	if s == nil || s.cache == nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	content, err := s.cache.GetReasoningContent(ctx, itemID)
	if err != nil {
		return ""
	}
	return content
}

// recacheReasoningItemsFromInput 把请求历史里带明文 summary 的 reasoning item
// 重新写入缓存（best-effort）。Codex 多数时候会原样回传明文 summary，借机
// 刷新 TTL 并自愈 Redis 被 flush / 跨实例漂移造成的缓存缺失。
func (s *OpenAIGatewayService) recacheReasoningItemsFromInput(inputRaw json.RawMessage) {
	if s == nil || s.cache == nil {
		return
	}
	inputRaw = bytes.TrimSpace(inputRaw)
	if len(inputRaw) == 0 || inputRaw[0] != '[' {
		return
	}
	var items []json.RawMessage
	if err := json.Unmarshal(inputRaw, &items); err != nil {
		return
	}
	for _, raw := range items {
		id, text, ok := apicompat.ExtractResponsesReasoningItem(raw)
		if !ok || id == "" || text == "" {
			continue
		}
		s.setReasoningContent(id, text)
	}
}

// hydrateResponsesReasoningInputFromCache 在进入统一 Pipeline 前为只有密文的
// reasoning 历史补回缓存中的明文 summary。缓存命中后移除 Chat 无法表达的
// provider-only encrypted_content；未命中时保留原字段，让 strict loss policy
// 拒绝无法无损转换的请求。解析或缓存失败均保持原请求。
func (s *OpenAIGatewayService) hydrateResponsesReasoningInputFromCache(body []byte) []byte {
	if s == nil || s.cache == nil || len(bytes.TrimSpace(body)) == 0 {
		return body
	}
	var request map[string]json.RawMessage
	if err := json.Unmarshal(body, &request); err != nil {
		return body
	}
	var items []json.RawMessage
	if err := json.Unmarshal(request["input"], &items); err != nil {
		return body
	}
	changed := false
	for index, raw := range items {
		id, text, ok := apicompat.ExtractResponsesReasoningItem(raw)
		if !ok || id == "" || text != "" {
			continue
		}
		cached := strings.TrimSpace(s.reasoningContentByID(id))
		if cached == "" {
			continue
		}
		var item map[string]json.RawMessage
		if err := json.Unmarshal(raw, &item); err != nil {
			continue
		}
		summary, err := json.Marshal([]apicompat.ResponsesSummary{{Type: "summary_text", Text: cached}})
		if err != nil {
			continue
		}
		item["summary"] = summary
		delete(item, "encrypted_content")
		updated, err := json.Marshal(item)
		if err != nil {
			continue
		}
		items[index] = updated
		changed = true
	}
	if !changed {
		return body
	}
	input, err := json.Marshal(items)
	if err != nil {
		return body
	}
	request["input"] = input
	updated, err := json.Marshal(request)
	if err != nil {
		return body
	}
	return updated
}

// cacheReasoningItemsFromPayloads 接受完整 Responses JSON 或单个 Responses SSE
// 事件负载，统一提取 reasoning 输出写入缓存。
func (s *OpenAIGatewayService) cacheReasoningItemsFromPayloads(payload []byte) {
	if len(bytes.TrimSpace(payload)) == 0 {
		return
	}
	var response apicompat.ResponsesResponse
	if err := json.Unmarshal(payload, &response); err == nil && len(response.Output) > 0 {
		s.cacheReasoningItemsFromOutput(response.Output)
		return
	}
	var event apicompat.ResponsesStreamEvent
	if err := json.Unmarshal(payload, &event); err == nil {
		s.cacheReasoningItemsFromEvents([]apicompat.ResponsesStreamEvent{event})
	}
}

// cacheReasoningItemsFromEvents 从 Responses 流事件里提取完成的 reasoning
// item 写入缓存（覆盖一个流中的多个 reasoning item）。
func (s *OpenAIGatewayService) cacheReasoningItemsFromEvents(events []apicompat.ResponsesStreamEvent) {
	for _, event := range events {
		if event.Type != "response.output_item.done" || event.Item == nil {
			continue
		}
		s.cacheReasoningItem(event.Item)
	}
}

// cacheReasoningItemsFromOutput 从非流式 Responses 响应的 output 里提取
// reasoning item 写入缓存。
func (s *OpenAIGatewayService) cacheReasoningItemsFromOutput(output []apicompat.ResponsesOutput) {
	for i := range output {
		s.cacheReasoningItem(&output[i])
	}
}

func (s *OpenAIGatewayService) cacheReasoningItem(item *apicompat.ResponsesOutput) {
	if item == nil || item.Type != "reasoning" || item.ID == "" {
		return
	}
	var parts []string
	for _, sum := range item.Summary {
		if t := strings.TrimSpace(sum.Text); t != "" {
			parts = append(parts, t)
		}
	}
	if len(parts) == 0 {
		return
	}
	s.setReasoningContent(item.ID, strings.Join(parts, "\n"))
}

// setReasoningContent 写入缓存，使用 detached ctx：客户端断连后仍在 drain
// 上游流（计费需要），此时的 reasoning 也是后续轮次回注所依赖的，不能随
// 请求 ctx 一起取消。失败仅记日志，不影响转发。
func (s *OpenAIGatewayService) setReasoningContent(itemID, content string) {
	if s == nil || s.cache == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := s.cache.SetReasoningContent(ctx, itemID, content, responsesReasoningCacheTTL); err != nil {
		logger.L().Warn("openai responses chat fallback: cache reasoning content failed",
			zap.Error(err),
			zap.String("item_id", itemID),
		)
	}
}

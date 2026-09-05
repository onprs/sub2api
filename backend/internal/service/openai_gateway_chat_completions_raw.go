package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv"
	protocoltransport "github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv/transport"
	"github.com/Wei-Shaw/sub2api/internal/util/responseheaders"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
	"go.uber.org/zap"
)

// openaiCCRawAllowedHeaders 是 CC 直转路径专用的客户端 header 透传白名单。
//
// **关键**：不能复用 openaiAllowedHeaders——后者含 Codex 客户端专属 header
// （originator / session_id / x-codex-turn-state / x-codex-turn-metadata / conversation_id），
// 这些在 ChatGPT OAuth 上游是必需的，但透传给 DeepSeek/Kimi/GLM 等第三方
// OpenAI 兼容上游会造成：
//   - 完全忽略（多数友好厂商）——隐性污染上游统计
//   - 400 "unknown parameter"（严格上游）——可见错误
//
// 这里仅放行通用 HTTP header；content-type / authorization / accept 由上下文
// 显式设置，不依赖透传。
//
// 参见决策记录：
// pensieve/short-term/maxims/dont-reuse-shared-headers-whitelist-across-different-upstream-trust-domains
var openaiCCRawAllowedHeaders = map[string]bool{
	"accept-language": true,
	"user-agent":      true,
}

// forwardAsRawChatCompletions 将客户端 Chat Completions 请求发往上游
// `{base_url}/v1/chat/completions`。标准语义走同协议 Pipeline，不做 CC↔Responses 转换。
//
// 适用场景：account.platform=openai && account.type=apikey && 上游已被探测确认
// 不支持 /v1/responses 端点（如 GLM/Qwen 等第三方 OpenAI 兼容上游）；CN 供应商
// 固定 chat_completions 协议也走此路径。
//
// 与 ForwardAsChatCompletions 的关键差异：
//
//   - 不调用 apicompat.ChatCompletionsToResponses，body 仅做模型 ID 改写
//   - 上游 URL 拼到 /v1/chat/completions 而非 /v1/responses
//   - 流式响应以 structured Chat events 进入同请求 Pipeline，再由 Chat renderer framing
//   - 非流式响应保留原始 JSON bytes，经 structured result 和 Chat renderer 输出
//   - 不应用 codex OAuth transform（APIKey 路径无 OAuth）
//   - 不注入 prompt_cache_key（OAuth 专属机制）
//
// 调用入口：openai_gateway_chat_completions.go::ForwardAsChatCompletions
// 在函数顶部按 openai_compat.ShouldUseResponsesAPI 分流。
func (s *OpenAIGatewayService) forwardAsRawChatCompletions(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	body []byte,
	defaultMappedModel string,
) (*OpenAIForwardResult, error) {
	startTime := time.Now()

	// 1. Parse minimal fields needed for routing/billing
	originalModel := gjson.GetBytes(body, "model").String()
	if originalModel == "" {
		writeChatCompletionsError(c, http.StatusBadRequest, "invalid_request_error", "model is required")
		return nil, fmt.Errorf("missing model in request")
	}
	clientStream := gjson.GetBytes(body, "stream").Bool()

	// 2. Resolve model mapping (same as ForwardAsChatCompletions)
	billingModel := resolveOpenAIForwardModel(account, originalModel, defaultMappedModel)
	upstreamModel := normalizeOpenAIModelForUpstream(account, billingModel)
	SetOpsUpstreamModel(c, upstreamModel)
	grokCacheIdentity := ""
	if account.Platform == PlatformGrok {
		// Resolve before image bridging or other body rewrites so the fallback is
		// anchored to the client's stable conversation prefix.
		grokCacheIdentity = resolveGrokCacheIdentity(c, body, "", upstreamModel)
	}
	reasoningEffort := extractOpenAIReasoningEffortFromBody(body, upstreamModel, billingModel, originalModel)
	// 国产模型默认 effort 补充：需要 mappedModel 判定，推迟到 billingModel 算出之后。
	reasoningEffort = ApplyThinkingEnabledFallback(reasoningEffort, body, billingModel)

	// 3. Rewrite model in body (no protocol conversion)
	upstreamBody := body
	if upstreamModel != originalModel {
		upstreamBody = ReplaceModelInBody(body, upstreamModel)
	}
	if normalizedBody, normalized := NormalizeGLMOpenAIReasoningEffort(upstreamBody, upstreamModel); normalized {
		upstreamBody = normalizedBody
	}

	// 4. Apply OpenAI fast policy on the CC body
	updatedBody, policyErr := s.applyOpenAIFastPolicyToBody(ctx, account, upstreamModel, upstreamBody)
	if policyErr != nil {
		var blocked *OpenAIFastBlockedError
		if errors.As(policyErr, &blocked) {
			MarkOpsClientBusinessLimited(c, OpsClientBusinessLimitedReasonLocalPolicyDenied)
			writeChatCompletionsError(c, http.StatusForbidden, "permission_error", blocked.Message)
		}
		return nil, policyErr
	}
	upstreamBody = updatedBody
	// Keep the final outbound tier separate from the observed response tier so
	// usage recording can apply the selected credential's response contract.
	serviceTier := extractOpenAIServiceTierFromBody(upstreamBody)
	if account.Platform == PlatformGrok {
		strippedBody, stripErr := stripRedundantGrokChatViewImageTool(upstreamBody)
		if stripErr != nil {
			return nil, fmt.Errorf("strip redundant Grok Chat view_image tool: %w", stripErr)
		}
		upstreamBody = strippedBody
	}

	// Grok Composer does not accept image_url parts directly, but Grok Build
	// can describe the images first. Bridge only this exact failure mode.
	token, tokenKind, err := s.getRequestCredential(ctx, c, account)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(token) == "" {
		return nil, fmt.Errorf("account %d missing %s credential", account.ID, tokenKind)
	}

	var bridgeUsage OpenAIUsage
	if account.Platform == PlatformGrok {
		bridgedBody, usage, bridged, bridgeErr := s.bridgeGrokComposerImageInputs(ctx, c, account, upstreamBody, token)
		if bridgeErr != nil {
			var failoverErr *UpstreamFailoverError
			if !errors.As(bridgeErr, &failoverErr) && c != nil && c.Writer != nil && !c.Writer.Written() {
				writeChatCompletionsError(c, http.StatusBadGateway, "upstream_error", bridgeErr.Error())
			}
			return nil, bridgeErr
		}
		if bridged {
			upstreamBody = bridgedBody
			addOpenAIUsage(&bridgeUsage, usage)
		}
	}

	if clientStream {
		var usageErr error
		upstreamBody, usageErr = ensureOpenAIChatStreamUsage(upstreamBody)
		if usageErr != nil {
			return nil, fmt.Errorf("enable stream usage: %w", usageErr)
		}
	}
	if account.Platform == PlatformGrok {
		upstreamBody, err = stripGrokChatPromptCacheKey(upstreamBody)
		if err != nil {
			return nil, fmt.Errorf("remove Responses-only Grok prompt cache key: %w", err)
		}
		upstreamBody, err = normalizeGrokChatReasoningEffort(upstreamBody, upstreamModel)
		if err != nil {
			return nil, fmt.Errorf("normalize Grok chat reasoning effort: %w", err)
		}
	}
	upstreamBody = applyOllamaCloudRawChatCompletionsRequest(account, upstreamBody)

	pipeline, err := newRawChatCompletionsPipeline(account, originalModel, upstreamModel)
	if err != nil {
		writeChatCompletionsError(c, http.StatusBadRequest, "invalid_request_error", "Failed to validate chat completions request")
		return nil, err
	}
	convertedRequest, err := pipeline.ConvertRequest(upstreamBody)
	if err != nil {
		writeChatCompletionsError(c, http.StatusBadRequest, "invalid_request_error", "Failed to validate chat completions request")
		return nil, err
	}
	upstreamBody = convertedRequest.Body

	logger.L().Debug("openai chat_completions raw: forwarding through identity protocol pipeline",
		zap.Int64("account_id", account.ID),
		zap.String("original_model", originalModel),
		zap.String("billing_model", billingModel),
		zap.String("upstream_model", upstreamModel),
		zap.Bool("stream", clientStream),
	)

	// 5. Build and send upstream request via the shared CC pipeline
	targetURL, err := s.rawChatCompletionsURL(account)
	if err != nil {
		return nil, err
	}
	SetActualOpenAIUpstreamEndpoint(c, grokChatRawEndpoint)
	customUA := account.GetOpenAIUserAgent()
	if customUA == "" && account.IsGrokOAuth() {
		customUA = defaultGrokUpstreamUserAgent()
	}
	resp, err := s.sendCCUpstreamRequest(ctx, c, account, targetURL, upstreamBody, clientStream, token, customUA, grokCacheIdentity)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	// 7. Handle error response with failover
	if resp.StatusCode >= 400 {
		upstream, upstreamMsg, collectErr := s.collectOpenAIStructuredUpstreamError(resp, protocolconv.ProtocolOpenAIChat, clientStream)
		if collectErr != nil {
			return nil, collectErr
		}
		if account.Platform == PlatformGrok {
			shouldFailover := s.shouldFailoverGrokUpstreamError(upstream.StatusCode, upstream.Body)
			kind := "http_error"
			if shouldFailover {
				kind = "failover"
			}
			appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
				ProxyID:            opsUpstreamProxyID(account),
				ProxyName:          opsUpstreamProxyName(account),
				Platform:           account.Platform,
				AccountID:          account.ID,
				AccountName:        account.Name,
				UpstreamStatusCode: upstream.StatusCode,
				UpstreamRequestID:  firstNonEmpty(upstream.RequestID, upstream.Headers.Get("xai-request-id")),
				Kind:               kind,
				Message:            upstreamMsg,
			})
			s.handleGrokAccountUpstreamError(withGrokTeamRateLimitModel(ctx, upstreamModel), account, upstream.StatusCode, upstream.Headers, upstream.Body)
			if shouldFailover {
				retryable, retryDelay, retryDeadline, retryMax := grokSameAccountRetryMetadata(account, upstream.StatusCode, upstream.Body)
				return nil, &UpstreamFailoverError{
					StatusCode:               upstream.StatusCode,
					ResponseBody:             upstream.Body,
					ResponseHeaders:          protocoltransport.CloneHeaders(upstream.Headers),
					RetryableOnSameAccount:   retryable,
					RequestScopedTransient:   retryable && upstream.StatusCode == http.StatusTooManyRequests,
					SameAccountRetryDelay:    retryDelay,
					SameAccountRetryDeadline: retryDeadline,
					SameAccountRetryMax:      retryMax,
				}
			}
			return s.handleChatCompletionsErrorResponse(upstream, c, account, billingModel)
		}
		if foErr := s.failoverOpenAIStructuredUpstreamError(ctx, c, account, upstream, upstreamMsg, upstreamModel); foErr != nil {
			return nil, foErr
		}
		return s.handleChatCompletionsErrorResponse(upstream, c, account, upstreamModel)
	}

	if account.Platform == PlatformGrok {
		s.updateGrokUsageFromResponse(withGrokTeamRateLimitModel(ctx, upstreamModel), account, resp.Header, resp.StatusCode)
	}

	// 8. Forward response
	var result *OpenAIForwardResult
	var forwardErr error
	if clientStream {
		result, forwardErr = s.streamRawChatCompletions(c, resp, account, pipeline, originalModel, billingModel, upstreamModel, reasoningEffort, serviceTier, startTime, len(body))
	} else {
		result, forwardErr = s.bufferRawChatCompletions(c, resp, account, pipeline, originalModel, billingModel, upstreamModel, reasoningEffort, serviceTier, startTime)
	}
	if result != nil {
		addOpenAIUsage(&result.Usage, bridgeUsage)
		result.UpstreamEndpoint = grokChatRawEndpoint
	}
	return result, forwardErr
}

func newRawChatCompletionsPipeline(
	account *Account,
	clientModel string,
	upstreamModel string,
) (*protocolconv.Pipeline, error) {
	route := protocolconv.Route{
		Source:         protocolconv.ProtocolOpenAIChat,
		IntendedTarget: protocolconv.ProtocolOpenAIChat,
		ClientModel:    clientModel,
		UpstreamModel:  upstreamModel,
	}
	if account != nil {
		route.Provider = account.Platform
		route.AccountID = account.ID
	}
	return protocolconv.NewPipeline(standardProtocolRegistry, protocolconv.PipelineConfig{
		Route:   route,
		Options: protocolconv.Options{SourceModel: upstreamModel, LossPolicy: protocolconv.LossError},
	})
}

func (s *OpenAIGatewayService) rawChatCompletionsURL(account *Account) (string, error) {
	if account.Platform == PlatformGrok {
		targetURL, err := buildGrokChatCompletionsURL(account, s.cfg, s.settingService)
		if err != nil {
			return "", fmt.Errorf("invalid grok base_url: %w", err)
		}
		return targetURL, nil
	}

	return s.openAIChatCompletionsTargetURL(account)
}

// streamRawChatCompletions 结构化转发上游 CC SSE 流到客户端，并提取 usage（包括
// 末尾 [DONE] 之前的 chunk 中的 usage 字段，按 OpenAI CC 协议）。
//
// usage 字段仅在客户端请求 stream_options.include_usage=true 时出现于上游响应中。
// 网关会对上游强制打开 include_usage 以保证计费完整，并原样向下游透传 usage，
// 让级联代理或下游计费系统也能拿到完整用量。
func (s *OpenAIGatewayService) streamRawChatCompletions(
	c *gin.Context,
	resp *http.Response,
	account *Account,
	pipeline *protocolconv.Pipeline,
	originalModel string,
	billingModel string,
	upstreamModel string,
	reasoningEffort *string,
	serviceTier *string,
	startTime time.Time,
	requestBodyLen int,
) (*OpenAIForwardResult, error) {
	observer := upstreamResponseModelObserverFromContext(c)
	if observer == nil {
		observer = beginUpstreamResponseModelObservation(c)
	}
	stream, err := s.collectCCUpstreamStream(resp, startTime)
	if err != nil {
		return nil, err
	}
	defer func() { _ = stream.Close() }()
	requestID := stream.RequestID
	session, err := pipeline.NewStreamProcessor(stream.ActualProtocol)
	if err != nil {
		return nil, fmt.Errorf("create raw Chat Completions stream processor: %w", err)
	}
	renderer, err := protocolconv.NewRenderer(protocolconv.ProtocolOpenAIChat)
	if err != nil {
		return nil, err
	}

	var usage OpenAIUsage
	var firstTokenMs *int
	responseID := ""
	clientDisconnected := false
	clientOutputStarted := false
	pendingPayloads := make([][]byte, 0, 4)
	refusalDetector := newOpenAIChatSilentRefusalDetector(requestBodyLen)
	var terminal openAIRawStreamTerminalState

	writePayloads := func(payloads [][]byte, force bool) error {
		if clientDisconnected || len(payloads) == 0 {
			return nil
		}
		if !clientOutputStarted && !force && !refusalDetector.ShouldReleaseClientOutput() {
			for _, payload := range payloads {
				pendingPayloads = append(pendingPayloads, append([]byte(nil), payload...))
			}
			return nil
		}
		allPayloads := make([][]byte, 0, len(pendingPayloads)+len(payloads))
		allPayloads = append(allPayloads, pendingPayloads...)
		allPayloads = append(allPayloads, payloads...)
		pendingPayloads = pendingPayloads[:0]
		framedPayloads := make([][]byte, 0, len(allPayloads))
		for _, payload := range allPayloads {
			framed, err := renderer.FrameStreamEvent(payload)
			if err != nil {
				return err
			}
			framedPayloads = append(framedPayloads, framed)
		}
		if !clientOutputStarted {
			if err := renderer.WriteStreamHeaders(c.Writer, stream.StatusCode, stream.Headers); err != nil {
				return err
			}
			clientOutputStarted = true
		}
		for _, framed := range framedPayloads {
			if _, err := c.Writer.Write(framed); err != nil {
				clientDisconnected = true
				logger.L().Debug("openai chat_completions raw: client disconnected, continuing to drain upstream for billing",
					zap.Error(err),
					zap.String("request_id", requestID),
				)
				return nil
			}
		}
		c.Writer.Flush()
		return nil
	}

	var streamReadErr error
	for {
		record, nextErr := stream.Events.Next(context.Background())
		if errors.Is(nextErr, protocoltransport.ErrSSEDone) {
			terminal.ObserveDataLine("[DONE]")
			break
		}
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			if !errors.Is(nextErr, context.Canceled) && !errors.Is(nextErr, context.DeadlineExceeded) {
				logger.L().Warn("openai chat_completions raw: stream read error",
					zap.Error(nextErr),
					zap.String("request_id", requestID),
				)
			}
			streamReadErr = nextErr
			break
		}

		terminal.ObserveDataLine(strings.TrimSpace(string(record.Data)))
		payload := applyOllamaCloudRawChatCompletionsResponse(account, record.Data)
		payload, _ = stripEmptyChatToolCallIdentity(payload)
		payloadText := string(payload)
		eventType := strings.TrimSpace(gjson.GetBytes(payload, "type").String())
		observer.ObserveOpenAI(payload, eventType)
		usageOnlyChunk := isOpenAIChatUsageOnlyStreamChunk(payloadText)
		if u := extractCCStreamUsage(payloadText); u != nil {
			usage = *u
		}
		if responseID == "" {
			responseID = strings.TrimSpace(gjson.GetBytes(payload, "id").String())
			stream.ResponseID = responseID
		}
		if firstTokenMs == nil && !usageOnlyChunk {
			elapsed := int(time.Since(startTime).Milliseconds())
			firstTokenMs = &elapsed
		}
		refusalDetector.ObservePayload(payload)
		converted, _, err := session.Convert(payload)
		if err != nil {
			return nil, fmt.Errorf("convert raw Chat Completions stream event: %w", err)
		}
		if err := writePayloads(converted, false); err != nil {
			return nil, fmt.Errorf("render raw Chat Completions stream event: %w", err)
		}
	}

	result := &OpenAIForwardResult{
		RequestID: requestID, ResponseID: responseID, ActualProtocol: stream.ActualProtocol,
		UpstreamHeaders: resp.Header,
		Usage:           usage, Model: originalModel,
		BillingModel: billingModel, UpstreamModel: upstreamModel,
		UpstreamResponseModel:         observedUpstreamResponseModel(c),
		UpstreamResponseModelConflict: observedUpstreamResponseModelConflict(c),
		UpstreamResponseServiceTier:   observedUpstreamResponseServiceTier(c),
		ReasoningEffort:               reasoningEffort,
		ServiceTier:                   resolvedOpenAIUpstreamServiceTier(c, serviceTier),
		Stream:                        true,
		Duration:                      time.Since(startTime),
		FirstTokenMs:                  firstTokenMs,
		ClientDisconnect:              clientDisconnected,
	}

	// 客户端取消/断开后上游读失败与上游截断不可区分，按已收到的用量正常收尾计费。
	clientAborted := clientDisconnected ||
		errors.Is(streamReadErr, context.Canceled) ||
		errors.Is(streamReadErr, context.DeadlineExceeded)

	// 上游在任何终止信号之前结束时，未提交响应可透明换号；已写出则返回类型化错误。
	if !clientAborted && terminal.IsTruncated(clientOutputStarted) {
		cause := streamReadErr
		if cause == nil {
			cause = ErrOpenAIUpstreamStreamTruncated
		}
		logger.L().Warn("openai chat_completions raw: upstream stream truncated before terminal chunk",
			zap.Error(cause),
			zap.String("request_id", requestID),
			zap.Int64("account_id", account.ID),
			zap.String("upstream_model", upstreamModel),
			zap.Bool("saw_sse_data", terminal.sawDataLine),
			zap.Bool("client_output_started", clientOutputStarted),
		)
		if !clientOutputStarted {
			return nil, newOpenAIRawStreamTruncatedFailoverError(c, account, requestID, cause)
		}
		recordOpenAIRawStreamTruncation(c, account, requestID, cause, "http_error")
		return result, newOpenAIUpstreamStreamReadError(cause)
	}
	if clientAborted {
		return result, nil
	}

	finalPayloads, _, err := session.Finalize()
	if err != nil {
		return result, fmt.Errorf("finalize raw Chat Completions stream: %w", err)
	}
	if err := writePayloads(finalPayloads, false); err != nil {
		return result, fmt.Errorf("render finalized raw Chat Completions stream: %w", err)
	}
	if !clientDisconnected && !clientOutputStarted {
		if refusalDetector.IsSilentRefusal() {
			return nil, newOpenAISilentRefusalFailoverError(c, account, requestID)
		}
		if len(pendingPayloads) > 0 {
			pending := append([][]byte(nil), pendingPayloads...)
			pendingPayloads = pendingPayloads[:0]
			if err := writePayloads(pending, true); err != nil {
				return result, fmt.Errorf("render pending raw Chat Completions stream: %w", err)
			}
		}
	}
	if !clientDisconnected {
		if !clientOutputStarted {
			if err := renderer.WriteStreamHeaders(c.Writer, stream.StatusCode, stream.Headers); err != nil {
				return result, err
			}
			clientOutputStarted = true
		}
		if _, err := c.Writer.Write(renderer.StreamTerminal()); err != nil {
			clientDisconnected = true
		}
		if !clientDisconnected {
			c.Writer.Flush()
		}
	}
	result.ClientDisconnect = clientDisconnected
	return result, nil
}

// ensureOpenAIChatStreamUsage 确保 raw Chat Completions 流式请求会让上游返回 usage。
// usage 也会继续向下游透传，支持级联代理和下游计费系统。
func ensureOpenAIChatStreamUsage(body []byte) ([]byte, error) {
	updated, err := sjson.SetBytes(body, "stream_options.include_usage", true)
	if err != nil {
		return body, err
	}
	return updated, nil
}

func isOpenAIChatUsageOnlyStreamChunk(payload string) bool {
	if strings.TrimSpace(payload) == "" {
		return false
	}
	if !gjson.Get(payload, "usage").Exists() {
		return false
	}
	choices := gjson.Get(payload, "choices")
	return choices.Exists() && choices.IsArray() && len(choices.Array()) == 0
}

// extractCCStreamUsage 从单个 CC 流式 chunk 的 payload 中提取 usage 字段。
// CC 协议中 usage 仅出现在末尾 chunk（且仅当 include_usage 生效时），
// 但上游可能在多个 chunk 中重复——总是用最新值。
func extractCCStreamUsage(payload string) *OpenAIUsage {
	usageResult := gjson.Get(payload, "usage")
	if !usageResult.Exists() || !usageResult.IsObject() {
		return nil
	}
	u, ok := openAIUsageFromGJSON(usageResult)
	if !ok {
		return nil
	}
	return &u
}

// bufferRawChatCompletions 透传上游 CC 非流式 JSON 响应。
func (s *OpenAIGatewayService) bufferRawChatCompletions(
	c *gin.Context,
	resp *http.Response,
	account *Account,
	pipeline *protocolconv.Pipeline,
	originalModel string,
	billingModel string,
	upstreamModel string,
	reasoningEffort *string,
	serviceTier *string,
	startTime time.Time,
) (*OpenAIForwardResult, error) {
	respBody, err := ReadUpstreamResponseBody(resp.Body, s.cfg, c, openAITooLargeError)
	if err != nil {
		if !errors.Is(err, ErrUpstreamResponseBodyTooLarge) {
			writeChatCompletionsError(c, http.StatusBadGateway, "api_error", "Failed to read upstream response")
		}
		return nil, fmt.Errorf("read upstream body: %w", err)
	}
	observer := upstreamResponseModelObserverFromContext(c)
	if observer == nil {
		observer = beginUpstreamResponseModelObservation(c)
	}
	observer.ObserveOpenAI(respBody, strings.TrimSpace(gjson.GetBytes(respBody, "type").String()))
	respBody = applyOllamaCloudRawChatCompletionsResponse(account, respBody)

	if len(respBody) == 0 {
		if requiresBillableGrokChatUsage(account, billingModel, upstreamModel) {
			upstreamRequestID := firstNonEmpty(resp.Header.Get("x-request-id"), resp.Header.Get("xai-request-id"))
			return nil, newGrokMissingUsageFailoverError(c, account, upstreamRequestID)
		}
		if s.responseHeaderFilter != nil {
			responseheaders.WriteFilteredHeaders(c.Writer.Header(), resp.Header, s.responseHeaderFilter)
		}
		if ct := resp.Header.Get("Content-Type"); ct != "" {
			c.Writer.Header().Set("Content-Type", ct)
		} else {
			c.Writer.Header().Set("Content-Type", "application/json")
		}
		c.Writer.WriteHeader(http.StatusOK)
		return &OpenAIForwardResult{
			RequestID:                   resp.Header.Get("x-request-id"),
			UpstreamHeaders:             resp.Header,
			ActualProtocol:              protocolconv.ProtocolOpenAIChat,
			Model:                       originalModel,
			BillingModel:                billingModel,
			UpstreamModel:               upstreamModel,
			UpstreamResponseServiceTier: observedUpstreamResponseServiceTier(c),
			ReasoningEffort:             reasoningEffort,
			ServiceTier:                 resolvedOpenAIUpstreamServiceTier(c, serviceTier),
			Stream:                      false,
			Duration:                    time.Since(startTime),
		}, nil
	}

	structured, result, err := s.collectRawChatCompletionsJSON(
		resp.StatusCode,
		resp.Header,
		respBody,
		originalModel,
		billingModel,
		upstreamModel,
		reasoningEffort,
		serviceTier,
		startTime,
	)
	if err != nil {
		writeChatCompletionsError(c, http.StatusBadGateway, "api_error", "Failed to parse upstream response")
		return nil, err
	}
	result.UpstreamResponseModel = observedUpstreamResponseModel(c)
	result.UpstreamResponseModelConflict = observedUpstreamResponseModelConflict(c)
	result.UpstreamResponseServiceTier = observedUpstreamResponseServiceTier(c)
	result.ServiceTier = resolvedOpenAIUpstreamServiceTier(c, serviceTier)
	responseModel := gjson.GetBytes(respBody, "model").String()
	if requiresBillableGrokChatUsage(account, billingModel, upstreamModel, responseModel) && !hasBillableGrokChatUsage(result.Usage) {
		upstreamRequestID := firstNonEmpty(result.RequestID, resp.Header.Get("xai-request-id"))
		return nil, newGrokMissingUsageFailoverError(c, account, upstreamRequestID)
	}
	converted, err := pipeline.ConvertResponse(structured.Body, structured.ActualProtocol)
	if err != nil {
		writeChatCompletionsError(c, http.StatusBadGateway, "api_error", "Failed to validate upstream response")
		return nil, fmt.Errorf("convert raw Chat Completions response: %w", err)
	}
	renderer, err := protocolconv.NewRenderer(protocolconv.ProtocolOpenAIChat)
	if err != nil {
		return nil, err
	}
	if err := renderer.RenderJSON(c.Writer, structured.StatusCode, structured.Headers, converted.Body); err != nil {
		return nil, fmt.Errorf("render raw Chat Completions response: %w", err)
	}

	return result, nil
}

func (s *OpenAIGatewayService) collectRawChatCompletionsJSON(
	statusCode int,
	headers http.Header,
	body []byte,
	originalModel string,
	billingModel string,
	upstreamModel string,
	reasoningEffort *string,
	serviceTier *string,
	startTime time.Time,
) (protocoltransport.Response, *OpenAIForwardResult, error) {
	usage := OpenAIUsage{}
	if parsedUsage, ok := extractOpenAIUsageFromJSONBytes(body); ok {
		usage = parsedUsage
	}
	filteredHeaders := make(http.Header)
	if s.responseHeaderFilter != nil {
		filteredHeaders = responseheaders.FilterHeaders(headers, s.responseHeaderFilter)
	}
	structured := protocoltransport.Response{
		StatusCode:     statusCode,
		Headers:        filteredHeaders,
		Body:           append([]byte(nil), body...),
		ActualProtocol: protocolconv.ProtocolOpenAIChat,
		RequestID:      headers.Get("x-request-id"),
		ResponseID:     extractOpenAIResponseIDFromJSONBytes(body),
		Duration:       time.Since(startTime),
	}
	if err := structured.Validate(); err != nil {
		return protocoltransport.Response{}, nil, fmt.Errorf("validate Chat Completions result: %w", err)
	}
	result := &OpenAIForwardResult{
		RequestID:       structured.RequestID,
		UpstreamHeaders: headers,
		ResponseID:      structured.ResponseID,
		ActualProtocol:  structured.ActualProtocol,
		Usage:           usage,
		Model:           originalModel,
		BillingModel:    billingModel,
		UpstreamModel:   upstreamModel,
		ReasoningEffort: reasoningEffort,
		ServiceTier:     serviceTier,
		Stream:          false,
		Duration:        structured.Duration,
	}
	return structured, result, nil

}

// buildOpenAIChatCompletionsURL 拼接上游 Chat Completions 端点 URL。
//
//   - base 已是 /chat/completions：原样返回
//   - base 以 /v1 结尾：追加 /chat/completions
//   - base 以其他版本段结尾（如 /v4）：追加 /chat/completions
//   - 其他情况：追加 /v1/chat/completions
//
// 与 buildOpenAIResponsesURL 是姐妹函数。
func buildOpenAIChatCompletionsURL(base string) string {
	return buildOpenAIEndpointURL(base, "/v1/chat/completions")
}

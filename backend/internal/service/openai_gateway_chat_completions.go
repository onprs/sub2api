package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai_compat"
	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv"
	protocoltransport "github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv/transport"
	"github.com/Wei-Shaw/sub2api/internal/util/responseheaders"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
	"go.uber.org/zap"
)

// cursorResponsesUnsupportedFields are top-level Responses API parameters that
// Codex upstreams reject with "Unsupported parameter: ...". They must be
// stripped when forwarding a raw client body through the Responses-shape
// short-circuit in ForwardAsChatCompletions (see isResponsesShape branch).
// The normal Chat Completions → Responses conversion path is unaffected
// because ChatCompletionsRequest has no fields for these parameters — unknown
// fields are dropped naturally by json.Unmarshal. Kept semantically in sync
// with the list in openai_gateway_service.go:2034 used by the /v1/responses
// passthrough path.
var cursorResponsesUnsupportedFields = []string{
	"prompt_cache_retention",
	"safety_identifier",
	"metadata",
	"stream_options",
}

// newOpenAIChatResponsesPipeline 保持请求转换严格，同时允许 OpenAI API Key
// 的 Responses 响应丢弃标准 Chat Completions 无法表达的 provider 专属字段。
func newOpenAIChatResponsesPipeline(account *Account, clientModel string, upstreamModel string) (*protocolconv.Pipeline, error) {
	config := protocolconv.PipelineConfig{
		Route: protocolconv.Route{
			Source:         protocolconv.ProtocolOpenAIChat,
			IntendedTarget: protocolconv.ProtocolOpenAIResponses,
			ClientModel:    clientModel,
			UpstreamModel:  upstreamModel,
			Provider:       account.Platform,
			AccountID:      account.ID,
		},
		Options: protocolconv.Options{SourceModel: upstreamModel, LossPolicy: protocolconv.LossError},
	}
	if account.IsOpenAIApiKey() {
		config.ResponseOptions = &protocolconv.Options{SourceModel: upstreamModel, LossPolicy: protocolconv.LossWarn}
	}
	return protocolconv.NewPipeline(standardProtocolRegistry, config)
}

// ForwardAsChatCompletions accepts a Chat Completions request body, converts it
// to OpenAI Responses API format, forwards to the OpenAI upstream, and converts
// the response back to Chat Completions format.
//
// 历史背景：该函数原本对所有 OpenAI 账号无差别走 CC→Responses 转换 + /v1/responses
// 端点——这在 OAuth（ChatGPT 内部 API 仅支持 Responses）和官方 APIKey 账号上是
// 正确的，但 sub2api 接入 DeepSeek/Kimi/GLM 等第三方 OpenAI 兼容上游后假设破裂：
// 这些上游普遍只支持 /v1/chat/completions，无 /v1/responses 端点。
//
// 当前路由策略（基于账号覆盖模式/探测标记，详见 openai_compat.ShouldUseResponsesAPI）：
//   - APIKey 账号 + 强制或探测确认不支持 Responses → 走 forwardAsRawChatCompletions
//     直转上游 /v1/chat/completions，不做协议转换
//   - 其他所有情况（OAuth、APIKey 强制/探测确认支持、未探测）→ 走原有 CC→Responses
//     转换路径（保留旧行为，存量未探测账号零兼容破坏）
func (s *OpenAIGatewayService) ForwardAsChatCompletions(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	body []byte,
	promptCacheKey string,
	defaultMappedModel string,
) (*OpenAIForwardResult, error) {
	restrictionResult := s.detectCodexClientRestriction(c, account, body)
	logCodexCLIOnlyDetection(ctx, c, account, getAPIKeyIDFromContext(c), restrictionResult, body)
	if restrictionResult.Enabled && !restrictionResult.Matched {
		MarkOpsClientBusinessLimited(c, OpsClientBusinessLimitedReasonLocalPolicyDenied)
		c.JSON(http.StatusForbidden, gin.H{
			"error": gin.H{
				"type":    "forbidden_error",
				"message": "This account only allows Codex official clients",
			},
		})
		return nil, errors.New("codex_cli_only restriction: only codex official clients are allowed")
	}

	if account.Platform == PlatformGrok {
		if account.IsGrokOAuth() {
			if eligible, reason := grokChatResponsesBridgeEligibility(body); eligible {
				return s.forwardGrokChatCompletionsViaResponses(ctx, c, account, body, promptCacheKey, defaultMappedModel)
			} else {
				logger.L().Debug("grok chat_completions: using raw fallback",
					zap.Int64("account_id", account.ID),
					zap.String("reason", reason),
				)
			}
		}
		return s.forwardAsRawChatCompletions(ctx, c, account, body, defaultMappedModel)
	}

	// 入口分流：APIKey 账号 + 强制或已探测确认上游不支持 Responses，走 CC 直转。
	// 自动模式下标记缺失（未探测）按"现状即证据"原则继续走下方原 Responses 转换路径。
	if account.Type == AccountTypeAPIKey && !openai_compat.ShouldUseResponsesAPI(account.Extra) {
		return s.forwardAsRawChatCompletions(ctx, c, account, body, defaultMappedModel)
	}

	startTime := time.Now()

	// 1. Parse Chat Completions request
	var chatReq apicompat.ChatCompletionsRequest
	if err := json.Unmarshal(body, &chatReq); err != nil {
		return nil, fmt.Errorf("parse chat completions request: %w", err)
	}
	originalModel := chatReq.Model
	clientStream := chatReq.Stream

	// 2. Resolve model mapping early so compat prompt_cache_key injection can
	// derive a stable seed from the final upstream model family.
	billingModel := resolveOpenAIForwardModel(account, originalModel, defaultMappedModel)
	upstreamModel := normalizeOpenAIModelForUpstream(account, billingModel)

	promptCacheKey = strings.TrimSpace(promptCacheKey)
	compatPromptCacheInjected := false
	if promptCacheKey == "" && account.Type == AccountTypeOAuth && shouldAutoInjectPromptCacheKeyForCompat(upstreamModel) {
		promptCacheKey = deriveCompatPromptCacheKey(&chatReq, upstreamModel)
		compatPromptCacheInjected = promptCacheKey != ""
	}

	// 3. Build the upstream (Responses API) body.
	//
	// Cursor compatibility: some clients (notably Cursor cloud) send Responses
	// API shaped bodies — `input: [...]` with no `messages` field — to the
	// /v1/chat/completions URL. Running those through ChatCompletionsToResponses
	// would silently drop Cursor's `input` array (the struct has no Input field)
	// and produce `input: null`, which Codex upstreams reject with
	// "Invalid type for 'input': expected a string, but got an object".
	//
	// Detect that shape and forward the raw body as-is, only rewriting `model`
	// to the resolved upstream model. The downstream codex OAuth transform will
	// still normalize store/stream/instructions/etc.
	isResponsesShape := !gjson.GetBytes(body, "messages").Exists() && gjson.GetBytes(body, "input").Exists()

	var (
		responsesReq  *apicompat.ResponsesRequest
		responsesBody []byte
		pipeline      *protocolconv.Pipeline
		err           error
	)
	if isResponsesShape {
		responsesBody, err = sjson.SetBytes(body, "model", upstreamModel)
		if err != nil {
			return nil, fmt.Errorf("rewrite model in responses-shape body: %w", err)
		}
		// Strip Responses API parameters that no Codex upstream accepts.
		// Because this branch forwards the raw body (the normal path rebuilds
		// it from ChatCompletionsRequest and drops unknown fields naturally),
		// we must filter these fields explicitly here — otherwise the upstream
		// rejects the request with "Unsupported parameter: ...".
		for _, field := range cursorResponsesUnsupportedFields {
			if stripped, derr := sjson.DeleteBytes(responsesBody, field); derr == nil {
				responsesBody = stripped
			}
		}
		responsesBody, normalizedServiceTier, err := normalizeResponsesBodyServiceTier(responsesBody)
		if err != nil {
			return nil, fmt.Errorf("normalize service_tier in responses-shape body: %w", err)
		}
		// Minimal stub populated from the raw body so downstream billing
		// propagation (ServiceTier, ReasoningEffort) keeps working.
		responsesReq = &apicompat.ResponsesRequest{
			Model:       upstreamModel,
			ServiceTier: normalizedServiceTier,
		}
		if effort := gjson.GetBytes(responsesBody, "reasoning.effort").String(); effort != "" {
			responsesReq.Reasoning = &apicompat.ResponsesReasoning{Effort: effort}
		}
	} else {
		// Normal path: keep one request-scoped pipeline for request conversion and
		// the non-stream response conversion. Streaming retains its characterized
		// service policy loop until that transport boundary is structured.
		pipeline, err = newOpenAIChatResponsesPipeline(account, originalModel, upstreamModel)
		if err != nil {
			return nil, fmt.Errorf("create chat responses pipeline: %w", err)
		}
		convertedRequest, convertErr := pipeline.ConvertRequest(body)
		if convertErr != nil {
			return nil, fmt.Errorf("convert chat completions to responses: %w", convertErr)
		}
		responsesBody = convertedRequest.Body
		if err := json.Unmarshal(responsesBody, &responsesReq); err != nil {
			return nil, fmt.Errorf("decode converted responses request: %w", err)
		}
		responsesReq.Model = upstreamModel
		responsesReq.Stream = true
		normalizeResponsesRequestServiceTier(responsesReq)
		responsesBody, err = json.Marshal(responsesReq)
		if err != nil {
			return nil, fmt.Errorf("marshal responses request: %w", err)
		}
	}

	logFields := []zap.Field{
		zap.Int64("account_id", account.ID),
		zap.String("original_model", originalModel),
		zap.String("billing_model", billingModel),
		zap.String("upstream_model", upstreamModel),
		zap.Bool("stream", clientStream),
		zap.Bool("responses_shape", isResponsesShape),
	}
	if compatPromptCacheInjected {
		logFields = append(logFields,
			zap.Bool("compat_prompt_cache_key_injected", true),
			zap.String("compat_prompt_cache_key_sha256", hashSensitiveValueForLog(promptCacheKey)),
		)
	}
	logger.L().Debug("openai chat_completions: model mapping applied", logFields...)

	if account.Type == AccountTypeOAuth {
		var reqBody map[string]any
		if err := json.Unmarshal(responsesBody, &reqBody); err != nil {
			return nil, fmt.Errorf("unmarshal for codex transform: %w", err)
		}
		codexResult := applyCodexOAuthTransformWithOptions(reqBody, codexOAuthTransformOptions{
			SkipDefaultInstructions: !isResponsesShape,
		})
		if !isResponsesShape {
			ensureCodexOAuthInstructionsField(reqBody)
		}
		if codexResult.NormalizedModel != "" {
			upstreamModel = codexResult.NormalizedModel
		}
		if codexResult.PromptCacheKey != "" {
			promptCacheKey = codexResult.PromptCacheKey
		} else if promptCacheKey != "" {
			reqBody["prompt_cache_key"] = promptCacheKey
		}
		responsesBody, err = json.Marshal(reqBody)
		if err != nil {
			return nil, fmt.Errorf("remarshal after codex transform: %w", err)
		}
	}

	if account.Type == AccountTypeAPIKey {
		if trimmedKey := strings.TrimSpace(promptCacheKey); trimmedKey != "" {
			var reqBody map[string]any
			if err := json.Unmarshal(responsesBody, &reqBody); err != nil {
				return nil, fmt.Errorf("unmarshal for prompt cache key injection: %w", err)
			}
			if existing, ok := reqBody["prompt_cache_key"].(string); !ok || strings.TrimSpace(existing) == "" {
				reqBody["prompt_cache_key"] = trimmedKey
				responsesBody, err = json.Marshal(reqBody)
				if err != nil {
					return nil, fmt.Errorf("remarshal after prompt cache key injection: %w", err)
				}
			}
		}
	}

	// 4b. Apply OpenAI fast policy (may filter service_tier or block the request).
	updatedBody, policyErr := s.applyOpenAIFastPolicyToBody(ctx, account, upstreamModel, responsesBody)
	if policyErr != nil {
		var blocked *OpenAIFastBlockedError
		if errors.As(policyErr, &blocked) {
			MarkOpsClientBusinessLimited(c, OpsClientBusinessLimitedReasonLocalPolicyDenied)
			writeChatCompletionsError(c, http.StatusForbidden, "permission_error", blocked.Message)
		}
		return nil, policyErr
	}
	responsesBody = updatedBody

	// 5. Get access token
	token, _, err := s.GetAccessToken(ctx, account)
	if err != nil {
		return nil, fmt.Errorf("get access token: %w", err)
	}

	// 6. Build upstream request
	upstreamCtx, releaseUpstreamCtx := detachUpstreamContext(ctx)
	upstreamReq, err := s.buildUpstreamRequest(upstreamCtx, c, account, responsesBody, token, true, promptCacheKey, false)
	releaseUpstreamCtx()
	if err != nil {
		return nil, fmt.Errorf("build upstream request: %w", err)
	}

	if promptCacheKey != "" {
		apiKeyID := getAPIKeyIDFromContext(c)
		upstreamReq.Header.Set("session_id", generateSessionUUID(isolateOpenAISessionID(apiKeyID, promptCacheKey)))
	}

	// 7. Send request
	proxyURL := ""
	if account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}
	resp, err := s.httpUpstream.Do(upstreamReq, proxyURL, account.ID, account.Concurrency)
	if err != nil {
		return nil, s.handleOpenAIUpstreamTransportError(ctx, c, account, err, false)
	}
	defer func() { _ = resp.Body.Close() }()

	// 8. Handle error response with failover
	if resp.StatusCode >= 400 {
		upstream, upstreamMsg, collectErr := s.collectOpenAIStructuredUpstreamError(resp, protocolconv.ProtocolOpenAIResponses, true)
		if collectErr != nil {
			return nil, collectErr
		}
		if account.Type == AccountTypeAPIKey &&
			openai_compat.ResolveResponsesSupport(account.Extra) == openai_compat.ResponsesSupportUnknown &&
			!isResponsesEndpointSupportedByStatus(upstream.StatusCode) {
			logger.L().Info("openai chat_completions: /responses unsupported, falling back to raw chat completions",
				zap.Int64("account_id", account.ID),
				zap.Int("upstream_status", upstream.StatusCode),
				zap.String("upstream_message", upstreamMsg),
			)
			return s.forwardAsRawChatCompletions(ctx, c, account, body, defaultMappedModel)
		}
		if foErr := s.failoverOpenAIStructuredUpstreamError(ctx, c, account, upstream, upstreamMsg, upstreamModel); foErr != nil {
			return nil, foErr
		}
		return s.handleChatCompletionsErrorResponse(upstream, c, account, upstreamModel)
	}

	// 9. Handle normal response
	var result *OpenAIForwardResult
	var handleErr error
	if clientStream {
		result, handleErr = s.handleChatStreamingResponse(resp, c, account, pipeline, originalModel, billingModel, upstreamModel, startTime, len(body))
	} else {
		result, handleErr = s.handleChatBufferedStreamingResponse(resp, c, account, pipeline, originalModel, billingModel, upstreamModel, startTime)
	}

	// cyber_policy：标记已设、error 已按 Chat Completions 格式发给客户端。丢弃 result、
	// 返回哨兵，使 handler 落入 tokens=0 免费用量行（对齐 /v1/responses），不计费、不 failover。
	if GetOpsCyberPolicy(c) != nil {
		if handleErr == nil {
			handleErr = errOpenAICyberPolicyForwarded
		}
		return nil, handleErr
	}

	// Propagate ServiceTier and ReasoningEffort to result for billing
	if handleErr == nil && result != nil {
		if responsesReq.ServiceTier != "" {
			st := responsesReq.ServiceTier
			result.ServiceTier = &st
		}
		if responsesReq.Reasoning != nil && responsesReq.Reasoning.Effort != "" {
			re := responsesReq.Reasoning.Effort
			result.ReasoningEffort = &re
		}
	}

	// Extract and save Codex usage snapshot from response headers (for OAuth accounts).
	// 排除 spark 影子:其 codex_* 仅由 QueryUsage(/wham/usage bengalfox)更新(外审第7轮 P1)。
	if handleErr == nil && account.Type == AccountTypeOAuth && !account.IsShadow() {
		if snapshot := ParseCodexRateLimitHeaders(resp.Header); snapshot != nil {
			s.updateCodexUsageSnapshot(ctx, account.ID, snapshot)
		}
	}

	return result, handleErr
}

func normalizeResponsesRequestServiceTier(req *apicompat.ResponsesRequest) {
	if req == nil {
		return
	}
	req.ServiceTier = normalizedOpenAIServiceTierValue(req.ServiceTier)
}

func normalizeResponsesBodyServiceTier(body []byte) ([]byte, string, error) {
	if len(body) == 0 {
		return body, "", nil
	}
	rawServiceTier := gjson.GetBytes(body, "service_tier").String()
	if rawServiceTier == "" {
		return body, "", nil
	}
	normalizedServiceTier := normalizedOpenAIServiceTierValue(rawServiceTier)
	if normalizedServiceTier == "" {
		trimmed, err := sjson.DeleteBytes(body, "service_tier")
		return trimmed, "", err
	}
	if normalizedServiceTier == rawServiceTier {
		return body, normalizedServiceTier, nil
	}
	trimmed, err := sjson.SetBytes(body, "service_tier", normalizedServiceTier)
	return trimmed, normalizedServiceTier, err
}

func normalizedOpenAIServiceTierValue(raw string) string {
	normalized := normalizeOpenAIServiceTier(raw)
	if normalized == nil {
		return ""
	}
	return *normalized
}

func openAICompatFailedResponseMessage(resp *apicompat.ResponsesResponse) string {
	if resp == nil || resp.Error == nil {
		return ""
	}
	return strings.TrimSpace(resp.Error.Message)
}

// handleChatCompletionsErrorResponse applies Chat source policy to a structured
// upstream error.
func (s *OpenAIGatewayService) handleChatCompletionsErrorResponse(
	upstream protocoltransport.Response,
	c *gin.Context,
	account *Account,
	requestedModel ...string,
) (*OpenAIForwardResult, error) {
	return s.handleCompatErrorResponse(upstream, c, account, writeChatCompletionsError, requestedModel...)
}

// handleChatBufferedStreamingResponse reads all Responses SSE events from the
// upstream, finds the terminal event, converts to a Chat Completions JSON
// response, and writes it to the client.
func (s *OpenAIGatewayService) handleChatBufferedStreamingResponse(
	resp *http.Response,
	c *gin.Context,
	account *Account,
	pipeline *protocolconv.Pipeline,
	originalModel string,
	billingModel string,
	upstreamModel string,
	startTime time.Time,
) (*OpenAIForwardResult, error) {
	requestID := resp.Header.Get("x-request-id")

	terminal, err := s.collectOpenAICompatBufferedTerminal(resp, "openai chat_completions buffered", startTime)
	if err != nil {
		return nil, err
	}
	finalResponse := terminal.Response
	usage := terminal.Usage

	if finalResponse == nil {
		writeChatCompletionsError(c, http.StatusBadGateway, "api_error", "Upstream stream ended without a terminal response event")
		return nil, fmt.Errorf("upstream stream ended without terminal event")
	}
	if strings.TrimSpace(finalResponse.Status) == "failed" {
		payload, _ := json.Marshal(gin.H{"type": "response.failed", "response": finalResponse})
		// cyber_policy 致命不可重试：不 failover，以 Chat Completions 错误格式回写（F4），
		// 标记供 handler 事后写风控/邮件/tokens=0 用量行。
		if hit, code, msg := detectOpenAICyberPolicy(payload); hit {
			MarkOpsCyberPolicy(c, CyberPolicyMark{
				Code:           code,
				Message:        msg,
				Body:           truncateString(string(payload), 4096),
				UpstreamStatus: http.StatusOK,
				UpstreamInTok:  usage.InputTokens,
				UpstreamOutTok: usage.OutputTokens,
			})
			clientMsg := msg
			if clientMsg == "" {
				clientMsg = "Request blocked by upstream cyber-security policy"
			}
			writeChatCompletionsError(c, http.StatusBadRequest, "invalid_request_error", clientMsg)
			return nil, fmt.Errorf("openai cyber_policy: %s", msg)
		}
		message := openAICompatFailedResponseMessage(finalResponse)
		if openAIStreamFailedEventShouldFailover(payload, message) {
			return nil, s.newOpenAIStreamFailoverError(c, account, false, requestID, payload, message, resp.Header)
		}
		message = s.recordOpenAIStreamUpstreamError(c, account, false, requestID, "http_error", payload, message)
		// response.failed 到达在 HTTP 200 SSE 流上，无真实 HTTP 错误码；统一走语义
		// 状态推断 + body 归一化（与 /v1/responses 路径一致），使按错误码配置的规则可命中。
		if status, errType, errMsg, matched := applyOpenAIStreamFailedErrorPassthroughRule(
			c, account.Platform, payload, message,
		); matched {
			if errMsg == "" {
				errMsg = message
			}
			MarkResponseCommitted(c)
			writeChatCompletionsError(c, status, errType, errMsg)
			return nil, fmt.Errorf("upstream response failed (passthrough): %s", errMsg)
		}
		writeChatCompletionsError(c, http.StatusBadGateway, "upstream_error", message)
		return nil, fmt.Errorf("upstream response failed: %s", message)
	}

	terminal.Upstream.Body, err = prepareOpenAICompatBufferedResponseBody(finalResponse, terminal.Upstream.Body, terminal.Accumulator)
	if err != nil {
		return nil, err
	}

	if pipeline == nil {
		// Cursor may send a Responses-shaped body to /chat/completions. That
		// compatibility short-circuit has no Chat request pipeline by design.
		chatResp := apicompat.ResponsesToChatCompletions(finalResponse, originalModel)
		responseheaders.WriteFilteredHeaders(c.Writer.Header(), resp.Header, s.responseHeaderFilter)
		c.Writer.Header().Set("Content-Type", "application/json; charset=utf-8")
		c.JSON(http.StatusOK, chatResp)
	} else {
		if err := terminal.Upstream.Validate(); err != nil {
			return nil, fmt.Errorf("validate buffered Responses result: %w", err)
		}
		converted, err := pipeline.ConvertResponse(terminal.Upstream.Body, terminal.Upstream.ActualProtocol)
		if err != nil {
			return nil, fmt.Errorf("convert buffered Responses to Chat Completions: %w", err)
		}
		renderer, err := protocolconv.NewRenderer(protocolconv.ProtocolOpenAIChat)
		if err != nil {
			return nil, err
		}
		if err := renderer.RenderJSON(c.Writer, terminal.Upstream.StatusCode, terminal.Upstream.Headers, converted.Body); err != nil {
			return nil, fmt.Errorf("render buffered Chat Completions response: %w", err)
		}
	}

	return &OpenAIForwardResult{
		RequestID:      requestID,
		ResponseID:     finalResponse.ID,
		ActualProtocol: terminal.Upstream.ActualProtocol,
		Usage:          usage,
		Model:          originalModel,
		BillingModel:   billingModel,
		UpstreamModel:  upstreamModel,
		Stream:         false,
		Duration:       time.Since(startTime),
	}, nil
}

// handleChatStreamingResponse reads Responses SSE events from upstream,
// converts each to Chat Completions SSE chunks, and writes them to the client.
func (s *OpenAIGatewayService) handleChatStreamingResponse(
	resp *http.Response,
	c *gin.Context,
	account *Account,
	pipeline *protocolconv.Pipeline,
	originalModel string,
	billingModel string,
	upstreamModel string,
	startTime time.Time,
	requestBodyLen int,
) (*OpenAIForwardResult, error) {
	stream, parser, err := s.collectOpenAICompatResponsesStream(resp, startTime)
	if err != nil {
		return nil, err
	}
	defer func() { _ = stream.Close() }()
	requestID := stream.RequestID
	var session *protocolconv.StreamSession
	var legacyState *apicompat.ResponsesEventToChatState
	if pipeline == nil {
		// Cursor may send a Responses-shaped request to /chat/completions. It has
		// no Chat-source request pipeline by design, so retain the characterized
		// response-only compatibility bridge for this explicit exceptional shape.
		legacyState = apicompat.NewResponsesEventToChatState()
		legacyState.Model = originalModel
		legacyState.IncludeUsage = true
	} else {
		var err error
		session, err = pipeline.NewStreamProcessor(stream.ActualProtocol)
		if err != nil {
			return nil, fmt.Errorf("create Chat-over-Responses stream processor: %w", err)
		}
	}
	renderer, err := protocolconv.NewRenderer(protocolconv.ProtocolOpenAIChat)
	if err != nil {
		return nil, err
	}
	writeStreamHeaders := s.newStreamHeaderWriter(c, stream.Headers)

	var usage OpenAIUsage
	var firstTokenMs *int
	firstChunk := true
	clientDisconnected := false
	clientOutputStarted := false
	pendingPayloads := make([][]byte, 0, 4)
	refusalDetector := newOpenAIChatSilentRefusalDetector(requestBodyLen)
	var streamFailoverErr *UpstreamFailoverError
	var streamNonFailoverErr error

	streamInterval := time.Duration(0)
	if s.cfg != nil && s.cfg.Gateway.StreamDataIntervalTimeout > 0 {
		streamInterval = time.Duration(s.cfg.Gateway.StreamDataIntervalTimeout) * time.Second
	}
	var intervalTicker *time.Ticker
	if streamInterval > 0 {
		intervalTicker = time.NewTicker(streamInterval)
		defer intervalTicker.Stop()
	}
	var intervalCh <-chan time.Time
	if intervalTicker != nil {
		intervalCh = intervalTicker.C
	}

	resultWithUsage := func() *OpenAIForwardResult {
		return &OpenAIForwardResult{
			RequestID:      requestID,
			ActualProtocol: stream.ActualProtocol,
			Usage:          usage,
			Model:          originalModel,
			BillingModel:   billingModel,
			UpstreamModel:  upstreamModel,
			Stream:         true,
			Duration:       time.Since(startTime),
			FirstTokenMs:   firstTokenMs,
		}
	}

	processDataLine := func(payload string) bool {
		if firstChunk {
			firstChunk = false
			ms := int(time.Since(startTime).Milliseconds())
			firstTokenMs = &ms
		}

		var event apicompat.ResponsesStreamEvent
		if err := json.Unmarshal([]byte(payload), &event); err != nil {
			logger.L().Warn("openai chat_completions stream: failed to parse event",
				zap.Error(err),
				zap.String("request_id", requestID),
			)
			return false
		}
		refusalDetector.ObservePayload([]byte(payload))

		isTerminalEvent := isOpenAICompatResponsesTerminalEvent(event.Type)
		if isTerminalEvent {
			if event.Usage != nil {
				usage = copyOpenAIUsageFromResponsesUsage(event.Usage)
			}
			if event.Response != nil && event.Response.Usage != nil {
				usage = copyOpenAIUsageFromResponsesUsage(event.Response.Usage)
			}
		}
		if strings.TrimSpace(event.Type) == "response.failed" {
			payloadBytes := []byte(payload)
			message := extractOpenAISSEErrorMessage(payloadBytes)
			if hit, code, msg := detectOpenAICyberPolicy(payloadBytes); hit {
				// cyber_policy 致命且不可重试：不 failover。下发标准 error chunk +
				// [DONE]，让程序化客户端可感知并停止重试（F4）；标记供 handler 事后
				// 写风控/邮件。
				MarkOpsCyberPolicy(c, CyberPolicyMark{
					Code:           code,
					Message:        msg,
					Body:           truncateString(string(payloadBytes), 4096),
					UpstreamStatus: http.StatusOK,
					UpstreamInTok:  usage.InputTokens,
					UpstreamOutTok: usage.OutputTokens,
				})
				if !clientDisconnected {
					// 被 refusal 检测扣留的 pendingSSE 有意丢弃——cyber 拦截优先于部分内容下发。
					writeStreamHeaders()
					clientMsg := msg
					if clientMsg == "" {
						clientMsg = "Request blocked by upstream cyber-security policy"
					}
					if _, err := fmt.Fprint(c.Writer, buildChatStreamErrorSSE(code, clientMsg)); err == nil {
						_, _ = fmt.Fprint(c.Writer, "data: [DONE]\n\n")
						if fl, ok := c.Writer.(http.Flusher); ok {
							fl.Flush()
						}
					}
					// 无条件置位：成功路径防 finalizeStream 重复 [DONE]；写失败意味着连接已不可写，
					// finalizeStream 的 [DONE] 同样发不出去，统一抑制。
					clientDisconnected = true
				}
				return true
			}
			if openAIStreamFailedEventShouldFailover(payloadBytes, message) {
				streamFailoverErr = s.newOpenAIStreamFailoverError(c, account, false, requestID, payloadBytes, message, resp.Header)
				return true
			}
			message = s.recordOpenAIStreamUpstreamError(c, account, false, requestID, "http_error", payloadBytes, message)
			defaultStatus, defaultErrType, defaultMsg := http.StatusBadGateway, "upstream_error", message
			// 统一走语义状态推断 + body 归一化（与 /v1/responses 路径一致），
			// 使按错误码配置的透传规则可命中。
			if status, errType, errMsg, matched := applyOpenAIStreamFailedErrorPassthroughRule(
				c, account.Platform, payloadBytes, message,
			); matched {
				if errMsg == "" {
					errMsg = defaultMsg
				}
				defaultStatus, defaultErrType, defaultMsg = status, errType, errMsg
				MarkResponseCommitted(c)
			}
			errorPayload, _ := json.Marshal(gin.H{
				"error": gin.H{
					"type":    defaultErrType,
					"message": defaultMsg,
				},
			})
			if c != nil && c.Writer != nil && !c.Writer.Written() {
				writeChatCompletionsError(c, defaultStatus, defaultErrType, defaultMsg)
				clientOutputStarted = true
			} else if c != nil && c.Writer != nil && !clientDisconnected {
				if _, err := fmt.Fprintf(c.Writer, "data: %s\n\n", errorPayload); err != nil {
					clientDisconnected = true
					logger.L().Info("openai chat_completions stream: client disconnected while writing upstream error",
						zap.String("request_id", requestID),
					)
				}
			}
			if !clientDisconnected {
				c.Writer.Flush()
			}
			streamNonFailoverErr = fmt.Errorf("upstream response failed: %s", message)
			return true
		}

		var convertedPayloads [][]byte
		if session != nil {
			var convertErr error
			convertedPayloads, _, convertErr = session.Convert([]byte(payload))
			if convertErr != nil {
				streamNonFailoverErr = fmt.Errorf("convert Responses stream event to Chat Completions: %w", convertErr)
				return true
			}
		} else {
			for _, chunk := range apicompat.ResponsesEventToChatChunks(&event, legacyState) {
				encoded, err := json.Marshal(chunk)
				if err != nil {
					streamNonFailoverErr = err
					return true
				}
				convertedPayloads = append(convertedPayloads, encoded)
			}
		}
		for _, convertedPayload := range convertedPayloads {
			var chunk apicompat.ChatCompletionsChunk
			if err := json.Unmarshal(convertedPayload, &chunk); err != nil {
				streamNonFailoverErr = fmt.Errorf("decode converted Chat Completions chunk: %w", err)
				return true
			}
			refusalDetector.ObserveChatChunk(chunk)
			if clientDisconnected {
				continue
			}
			if !clientOutputStarted && !refusalDetector.ShouldReleaseClientOutput() {
				pendingPayloads = append(pendingPayloads, append([]byte(nil), convertedPayload...))
				continue
			}
			if !clientOutputStarted {
				writeStreamHeaders()
				for _, pending := range pendingPayloads {
					framed, err := renderer.FrameStreamEvent(pending)
					if err != nil {
						streamNonFailoverErr = err
						return true
					}
					if _, err := c.Writer.Write(framed); err != nil {
						clientDisconnected = true
						logger.L().Info("openai chat_completions stream: client disconnected while flushing pending chunks", zap.String("request_id", requestID))
						break
					}
				}
				pendingPayloads = pendingPayloads[:0]
				clientOutputStarted = !clientDisconnected
			}
			if clientDisconnected {
				continue
			}
			framed, err := renderer.FrameStreamEvent(convertedPayload)
			if err != nil {
				streamNonFailoverErr = err
				return true
			}
			if _, err := c.Writer.Write(framed); err != nil {
				clientDisconnected = true
				logger.L().Info("openai chat_completions stream: client disconnected, continuing to drain upstream for billing", zap.String("request_id", requestID))
			}
		}
		if len(convertedPayloads) > 0 && !clientDisconnected && clientOutputStarted {
			c.Writer.Flush()
		}
		return isTerminalEvent
	}

	finalizeStream := func() (*OpenAIForwardResult, error) {
		if streamFailoverErr != nil {
			if c == nil || c.Writer == nil || !c.Writer.Written() {
				return nil, streamFailoverErr
			}
			return resultWithUsage(), streamFailoverErr
		}
		if streamNonFailoverErr != nil {
			return resultWithUsage(), streamNonFailoverErr
		}
		var finalPayloads [][]byte
		if session != nil {
			var err error
			finalPayloads, _, err = session.Finalize()
			if err != nil {
				return resultWithUsage(), fmt.Errorf("finalize Chat-over-Responses stream: %w", err)
			}
		} else {
			for _, chunk := range apicompat.FinalizeResponsesChatStream(legacyState) {
				encoded, err := json.Marshal(chunk)
				if err != nil {
					return resultWithUsage(), err
				}
				finalPayloads = append(finalPayloads, encoded)
			}
		}
		for _, finalPayload := range finalPayloads {
			var chunk apicompat.ChatCompletionsChunk
			if err := json.Unmarshal(finalPayload, &chunk); err != nil {
				return resultWithUsage(), fmt.Errorf("decode finalized Chat Completions chunk: %w", err)
			}
			refusalDetector.ObserveChatChunk(chunk)
			if clientDisconnected {
				continue
			}
			if !clientOutputStarted && !refusalDetector.ShouldReleaseClientOutput() {
				pendingPayloads = append(pendingPayloads, append([]byte(nil), finalPayload...))
				continue
			}
			if !clientOutputStarted {
				writeStreamHeaders()
				for _, pending := range pendingPayloads {
					framed, err := renderer.FrameStreamEvent(pending)
					if err != nil {
						return resultWithUsage(), err
					}
					if _, err := c.Writer.Write(framed); err != nil {
						clientDisconnected = true
						break
					}
				}
				pendingPayloads = pendingPayloads[:0]
				clientOutputStarted = !clientDisconnected
			}
			if clientDisconnected {
				continue
			}
			framed, err := renderer.FrameStreamEvent(finalPayload)
			if err != nil {
				return resultWithUsage(), err
			}
			if _, err := c.Writer.Write(framed); err != nil {
				clientDisconnected = true
			}
		}
		if !clientDisconnected && !clientOutputStarted {
			if refusalDetector.IsSilentRefusal() {
				return nil, newOpenAISilentRefusalFailoverError(c, account, requestID)
			}
			if len(pendingPayloads) > 0 {
				writeStreamHeaders()
				for _, pending := range pendingPayloads {
					framed, err := renderer.FrameStreamEvent(pending)
					if err != nil {
						return resultWithUsage(), err
					}
					if _, err := c.Writer.Write(framed); err != nil {
						clientDisconnected = true
						logger.L().Info("openai chat_completions stream: client disconnected during final pending flush", zap.String("request_id", requestID))
						break
					}
				}
				pendingPayloads = pendingPayloads[:0]
				clientOutputStarted = !clientDisconnected
			}
		}
		// Send [DONE] sentinel
		if !clientDisconnected {
			writeStreamHeaders()
			if _, err := c.Writer.Write(renderer.StreamTerminal()); err != nil {
				clientDisconnected = true
				logger.L().Info("openai chat_completions stream: client disconnected during done flush",
					zap.String("request_id", requestID),
				)
			}
			clientOutputStarted = !clientDisconnected
		}
		if !clientDisconnected {
			c.Writer.Flush()
		}
		return resultWithUsage(), nil
	}

	missingTerminalErr := func() (*OpenAIForwardResult, error) {
		return resultWithUsage(), fmt.Errorf("stream usage incomplete: missing terminal event")
	}

	events := make(chan openAICompatStreamEvent, 16)
	done := make(chan struct{})
	go func() {
		defer close(events)
		for {
			record, nextErr := stream.Events.Next(context.Background())
			select {
			case events <- openAICompatStreamEvent{record: record, err: nextErr}:
			case <-done:
				return
			}
			if nextErr != nil {
				return
			}
		}
	}()
	defer close(done)

	keepaliveInterval := time.Duration(0)
	if s.cfg != nil && s.cfg.Gateway.StreamKeepaliveInterval > 0 {
		keepaliveInterval = time.Duration(s.cfg.Gateway.StreamKeepaliveInterval) * time.Second
	}
	var keepaliveTicker *time.Ticker
	if keepaliveInterval > 0 {
		keepaliveTicker = time.NewTicker(keepaliveInterval)
		defer keepaliveTicker.Stop()
	}
	var keepaliveCh <-chan time.Time
	if keepaliveTicker != nil {
		keepaliveCh = keepaliveTicker.C
	}
	lastDataAt := time.Now()

	for {
		select {
		case event, ok := <-events:
			if !ok {
				return missingTerminalErr()
			}
			if event.err != nil {
				if errors.Is(event.err, protocoltransport.ErrSSEDone) || errors.Is(event.err, io.EOF) {
					return missingTerminalErr()
				}
				if !errors.Is(event.err, context.Canceled) && !errors.Is(event.err, context.DeadlineExceeded) {
					logger.L().Warn("openai chat_completions stream: read error",
						zap.Error(event.err),
						zap.String("request_id", requestID),
					)
				}
				return resultWithUsage(), fmt.Errorf("stream usage incomplete: %w", event.err)
			}
			lastDataAt = time.Now()
			payload := openAICompatPayloadWithEventType(string(event.record.Data), event.record.Event)
			if processDataLine(payload) {
				return finalizeStream()
			}

		case <-intervalCh:
			if time.Since(parser.Progress().LastReadAt) < streamInterval {
				continue
			}
			if clientDisconnected {
				return resultWithUsage(), fmt.Errorf("stream usage incomplete after timeout")
			}
			logger.L().Warn("openai chat_completions stream: data interval timeout",
				zap.String("request_id", requestID),
				zap.String("model", originalModel),
				zap.Duration("interval", streamInterval),
			)
			return resultWithUsage(), fmt.Errorf("stream data interval timeout")

		case <-keepaliveCh:
			if clientDisconnected {
				continue
			}
			if refusalDetector.Enabled() && !clientOutputStarted {
				continue
			}
			if parser.Progress().InRecord || time.Since(lastDataAt) < keepaliveInterval {
				continue
			}
			writeStreamHeaders()
			if _, err := fmt.Fprint(c.Writer, ":\n\n"); err != nil {
				logger.L().Info("openai chat_completions stream: client disconnected during keepalive",
					zap.String("request_id", requestID),
				)
				clientDisconnected = true
				continue
			}
			c.Writer.Flush()
		}
	}
}

// writeChatCompletionsError writes an error response in OpenAI Chat Completions format.
func writeChatCompletionsError(c *gin.Context, statusCode int, errType, message string) {
	MarkResponseCommitted(c)
	c.JSON(statusCode, gin.H{
		"error": gin.H{
			"type":    errType,
			"message": message,
		},
	})
}

// buildChatStreamErrorSSE builds one SSE data frame carrying an OpenAI chat
// streaming error object. Used when the stream must terminate with a visible
// error (e.g. upstream cyber_policy), so programmatic clients stop retrying.
// Marshal 失败的兜底会丢弃 message 原文，仅保留 code 与固定提示。
func buildChatStreamErrorSSE(code, message string) string {
	payload, err := json.Marshal(gin.H{
		"error": gin.H{
			"type":    "invalid_request_error",
			"code":    code,
			"message": message,
		},
	})
	if err != nil {
		return "data: {\"error\":{\"type\":\"invalid_request_error\",\"code\":\"" + code + "\",\"message\":\"upstream error\"}}\n\n"
	}
	return "data: " + string(payload) + "\n\n"
}

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
	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai_compat"
	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv"
	protocoltransport "github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv/transport"
	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
	"github.com/Wei-Shaw/sub2api/internal/util/responseheaders"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
	"go.uber.org/zap"
)

// ForwardAsAnthropic accepts an Anthropic Messages request body, converts it
// to OpenAI Responses API format, forwards to the OpenAI upstream, and converts
// the response back to Anthropic Messages format. This enables Claude Code
// clients to access OpenAI models through the standard /v1/messages endpoint.
func (s *OpenAIGatewayService) ForwardAsAnthropic(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	body []byte,
	promptCacheKey string,
	defaultMappedModel string,
) (*OpenAIForwardResult, error) {
	// 入口分流：APIKey 账号 + 上游不支持 Responses API → 走 CC 直转（与
	// ForwardAsChatCompletions 对称）。缺少此分流时，/v1/messages 入站请求
	// 会被无条件转为 Responses 格式发往上游 /v1/responses，导致只支持
	// /v1/chat/completions 的第三方 OpenAI 兼容上游全部 400。
	if account.Type == AccountTypeAPIKey && !openai_compat.ShouldUseResponsesAPI(account.Extra) {
		return s.forwardAnthropicViaRawChatCompletions(ctx, c, account, body, defaultMappedModel)
	}

	startTime := time.Now()

	// 1. Parse Anthropic request
	var anthropicReq apicompat.AnthropicRequest
	if err := json.Unmarshal(body, &anthropicReq); err != nil {
		return nil, fmt.Errorf("parse anthropic request: %w", err)
	}
	anthropicDigestReq := cloneAnthropicRequestForDigest(&anthropicReq)
	originalModel := anthropicReq.Model
	applyOpenAICompatModelNormalization(&anthropicReq)
	normalizedModel := anthropicReq.Model
	clientStream := anthropicReq.Stream // client's original stream preference

	// 2. Model mapping
	billingModel := resolveOpenAIForwardModel(account, normalizedModel, defaultMappedModel)
	upstreamModel := normalizeOpenAIModelForUpstream(account, billingModel)
	promptCacheKey = strings.TrimSpace(promptCacheKey)
	apiKeyID := getAPIKeyIDFromContext(c)
	anthropicDigestChain := ""
	anthropicMatchedDigestChain := ""
	compatPromptCacheInjected := false
	if promptCacheKey == "" && shouldAutoInjectPromptCacheKeyForCompat(upstreamModel) {
		promptCacheKey = promptCacheKeyFromAnthropicMetadataSession(&anthropicReq)
		if promptCacheKey == "" {
			promptCacheKey = deriveAnthropicCacheControlPromptCacheKey(&anthropicReq)
		}
		if promptCacheKey == "" {
			anthropicDigestChain = buildOpenAICompatAnthropicDigestChain(anthropicDigestReq)
			if reusedKey, matchedChain := s.findOpenAICompatAnthropicDigestPromptCacheKey(account, apiKeyID, anthropicDigestChain); reusedKey != "" {
				promptCacheKey = reusedKey
				anthropicMatchedDigestChain = matchedChain
			} else {
				promptCacheKey = promptCacheKeyFromAnthropicDigest(anthropicDigestChain)
			}
		}
		compatPromptCacheInjected = promptCacheKey != ""
	}
	compatReplayTrimmed := false
	compatReplayGuardEnabled := shouldAutoInjectPromptCacheKeyForCompat(upstreamModel)
	compatContinuationEnabled := openAICompatContinuationEnabled(account, upstreamModel)
	previousResponseID := ""
	if compatContinuationEnabled {
		previousResponseID = s.getOpenAICompatSessionResponseID(ctx, c, account, promptCacheKey)
	}
	compatContinuationDisabled := compatContinuationEnabled &&
		s.isOpenAICompatSessionContinuationDisabled(ctx, c, account, promptCacheKey)
	compatTurnState := ""
	// OAuth/Plus relies on session_id + x-codex-turn-state; trimming to a
	// sliding 12-message window makes the cached prefix stall at system/tools.
	// Keep full replay there so upstream prompt caching can grow turn by turn.
	if compatReplayGuardEnabled && account.Type != AccountTypeOAuth && previousResponseID == "" && !compatContinuationDisabled {
		compatReplayTrimmed = applyAnthropicCompatFullReplayGuard(&anthropicReq)
	}

	// Establish the standard request lifecycle from the source request after
	// source-owned normalization/replay policy. This exact Pipeline is retained
	// for the successful Responses reply and its request-scoped tool metadata.
	pipeline, err := protocolconv.NewPipeline(standardProtocolRegistry, protocolconv.PipelineConfig{
		Route: protocolconv.Route{
			Source: protocolconv.ProtocolAnthropic, IntendedTarget: protocolconv.ProtocolOpenAIResponses,
			ClientModel: originalModel, UpstreamModel: upstreamModel, Provider: account.Platform, AccountID: account.ID,
		},
		Options: protocolconv.Options{SourceModel: upstreamModel, LossPolicy: protocolconv.LossError},
	})
	if err != nil {
		return nil, fmt.Errorf("create Anthropic Responses pipeline: %w", err)
	}
	normalizedSourceBody, err := json.Marshal(&anthropicReq)
	if err != nil {
		return nil, fmt.Errorf("marshal normalized Anthropic request: %w", err)
	}
	convertedRequest, err := pipeline.ConvertRequest(normalizedSourceBody)
	if err != nil {
		return nil, fmt.Errorf("convert Anthropic request through standard pipeline: %w", err)
	}

	// Codex policy runs only after standard conversion. It adds provider
	// defaults, moves source system text into developer input, and preserves the
	// historical continuation/cache shape without rebuilding message semantics.
	responsesBody, err := applyOpenAICodexMessagesRequestPolicy(convertedRequest.Body, anthropicReq.System, upstreamModel)
	if err != nil {
		return nil, err
	}
	var responsesPolicyBody map[string]any
	if err := json.Unmarshal(responsesBody, &responsesPolicyBody); err != nil {
		return nil, fmt.Errorf("decode Codex Messages policy body: %w", err)
	}

	// Upstream always uses streaming (upstream may not support sync mode).
	// The client's original preference determines the response format.
	isStream := true

	// 3b. Handle BetaFastMode → service_tier: "priority"
	if containsBetaToken(c.GetHeader("anthropic-beta"), claude.BetaFastMode) {
		responsesPolicyBody["service_tier"] = "priority"
	}
	if previousResponseID != "" {
		responsesPolicyBody["previous_response_id"] = previousResponseID
		trimOpenAICompatResponsesBodyToLatestTurn(responsesPolicyBody)
	}
	if compatReplayGuardEnabled && account.Type != AccountTypeOAuth {
		appendOpenAICompatClaudeCodeTodoGuardToRequestBody(responsesPolicyBody)
	}
	responsesBody, err = json.Marshal(responsesPolicyBody)
	if err != nil {
		return nil, fmt.Errorf("marshal Codex Messages policy body: %w", err)
	}

	logFields := []zap.Field{
		zap.Int64("account_id", account.ID),
		zap.String("original_model", originalModel),
		zap.String("normalized_model", normalizedModel),
		zap.String("billing_model", billingModel),
		zap.String("upstream_model", upstreamModel),
		zap.Bool("stream", isStream),
	}
	if compatPromptCacheInjected {
		logFields = append(logFields,
			zap.Bool("compat_prompt_cache_key_injected", true),
			zap.String("compat_prompt_cache_key_sha256", hashSensitiveValueForLog(promptCacheKey)),
		)
	}
	if compatReplayTrimmed {
		logFields = append(logFields,
			zap.Bool("compat_full_replay_trimmed", true),
			zap.Int("compat_messages_after_trim", len(anthropicReq.Messages)),
		)
	}
	if previousResponseID != "" {
		logFields = append(logFields,
			zap.Bool("compat_previous_response_id_attached", true),
			zap.String("compat_previous_response_id", truncateOpenAIWSLogValue(previousResponseID, openAIWSIDValueMaxLen)),
		)
	}
	if compatTurnState != "" {
		logFields = append(logFields, zap.Bool("compat_turn_state_attached", true))
	}
	logger.L().Debug("openai messages: model mapping applied", logFields...)

	// 4. Apply OAuth Codex transport policy to the standard converted body.
	if account.Type == AccountTypeOAuth && account.Platform != PlatformGrok {
		var reqBody map[string]any
		if err := json.Unmarshal(responsesBody, &reqBody); err != nil {
			return nil, fmt.Errorf("unmarshal for codex transform: %w", err)
		}
		codexResult := applyCodexOAuthTransformWithOptions(reqBody, codexOAuthTransformOptions{
			SkipDefaultInstructions: true,
			PreserveToolCallIDs:     true,
		})
		forcedTemplateText := ""
		if s.cfg != nil {
			forcedTemplateText = s.cfg.Gateway.ForcedCodexInstructionsTemplate
		}
		templateUpstreamModel := upstreamModel
		if codexResult.NormalizedModel != "" {
			templateUpstreamModel = codexResult.NormalizedModel
		}
		existingInstructions, _ := reqBody["instructions"].(string)
		if strings.TrimSpace(existingInstructions) == "" {
			existingInstructions = extractPromptLikeInstructionsFromInput(reqBody)
		}
		if _, err := applyForcedCodexInstructionsTemplate(reqBody, forcedTemplateText, forcedCodexInstructionsTemplateData{
			ExistingInstructions: strings.TrimSpace(existingInstructions),
			OriginalModel:        originalModel,
			NormalizedModel:      normalizedModel,
			BillingModel:         billingModel,
			UpstreamModel:        templateUpstreamModel,
		}); err != nil {
			return nil, err
		}
		ensureCodexOAuthInstructionsField(reqBody)
		if shouldAutoInjectPromptCacheKeyForCompat(upstreamModel) {
			appendOpenAICompatClaudeCodeTodoGuardToRequestBody(reqBody)
		}
		if codexResult.NormalizedModel != "" {
			upstreamModel = codexResult.NormalizedModel
		}
		if codexResult.PromptCacheKey != "" {
			promptCacheKey = codexResult.PromptCacheKey
		}
		delete(reqBody, "prompt_cache_key")
		if shouldAutoInjectPromptCacheKeyForCompat(upstreamModel) {
			compatTurnState = s.getOpenAICompatSessionTurnState(ctx, c, account, promptCacheKey)
		}
		// OAuth codex transform forces stream=true upstream, so always use
		// the streaming response handler regardless of what the client asked.
		isStream = true
		responsesBody, err = json.Marshal(reqBody)
		if err != nil {
			return nil, fmt.Errorf("remarshal after codex transform: %w", err)
		}
	}

	// For API key accounts (including OpenAI-compatible upstream gateways),
	// ensure promptCacheKey is also propagated via the request body so that
	// upstreams using the Responses API can derive a stable session identifier
	// from prompt_cache_key. This makes our Anthropic /v1/messages compatibility
	// path behave more like a native Responses client.
	if account.Type == AccountTypeAPIKey {
		if trimmedKey := strings.TrimSpace(promptCacheKey); trimmedKey != "" {
			var reqBody map[string]any
			if err := json.Unmarshal(responsesBody, &reqBody); err != nil {
				return nil, fmt.Errorf("unmarshal for prompt cache key injection: %w", err)
			}
			if existing, ok := reqBody["prompt_cache_key"].(string); !ok || strings.TrimSpace(existing) == "" {
				reqBody["prompt_cache_key"] = trimmedKey
				updated, err := json.Marshal(reqBody)
				if err != nil {
					return nil, fmt.Errorf("remarshal after prompt cache key injection: %w", err)
				}
				responsesBody = updated
			}
		}
	}

	// 4c. Apply OpenAI fast policy (may filter service_tier or block the request).
	// Mirrors the Claude anthropic-beta "fast-mode-2026-02-01" filter, but keyed
	// on the body-level service_tier field (priority/flex).
	updatedBody, policyErr := s.applyOpenAIFastPolicyToBody(ctx, account, upstreamModel, responsesBody)
	if policyErr != nil {
		var blocked *OpenAIFastBlockedError
		if errors.As(policyErr, &blocked) {
			MarkOpsClientBusinessLimited(c, OpsClientBusinessLimitedReasonLocalPolicyDenied)
			writeAnthropicError(c, http.StatusForbidden, "forbidden_error", blocked.Message)
		}
		return nil, policyErr
	}
	responsesBody = updatedBody
	if account.Platform == PlatformGrok {
		patchedBody, patchErr := patchGrokResponsesBody(responsesBody, upstreamModel)
		if patchErr != nil {
			return nil, patchErr
		}
		responsesBody = patchedBody
	}

	// 5. Get access token
	token, _, err := s.GetAccessToken(ctx, account)
	if err != nil {
		return nil, fmt.Errorf("get access token: %w", err)
	}

	// 6. Build upstream request
	if account.Type == AccountTypeOAuth && account.Platform != PlatformGrok {
		// Messages 兼容桥即使 body 未带 todo-guard/prompt_cache_key 标记（如映射到非
		// gpt-5/codex 模型），也必须让 buildUpstreamRequest 走 bridge 分支：不带
		// originator、User-Agent 逐字透传，避免身份收口（issue #3901）误改本路径
		// 刻意最小化的请求形态（下方的 Del(OpenAI-Beta/originator) 兜底保持不变）。
		setOpenAICompatMessagesBridgeContext(c, true)
	}
	upstreamCtx, releaseUpstreamCtx := detachUpstreamContext(ctx)
	var upstreamReq *http.Request
	if account.Platform == PlatformGrok {
		upstreamReq, err = buildGrokResponsesRequest(upstreamCtx, c, account, responsesBody, token)
	} else {
		upstreamReq, err = s.buildUpstreamRequest(upstreamCtx, c, account, responsesBody, token, isStream, promptCacheKey, false)
	}
	releaseUpstreamCtx()
	if err != nil {
		return nil, fmt.Errorf("build upstream request: %w", err)
	}

	// Override session_id with a deterministic UUID derived from the isolated
	// session key, ensuring different API keys produce different upstream sessions.
	if promptCacheKey != "" {
		isolatedSessionID := generateSessionUUID(isolateOpenAISessionID(apiKeyID, promptCacheKey))
		upstreamReq.Header.Set("session_id", isolatedSessionID)
		if upstreamReq.Header.Get("conversation_id") != "" {
			upstreamReq.Header.Set("conversation_id", isolatedSessionID)
		}
	}
	if account.Type == AccountTypeOAuth && account.Platform != PlatformGrok {
		// Anthropic Messages compatibility uses the ChatGPT Codex SSE endpoint.
		// Match airgate-openai's request shape: the SSE endpoint does not need
		// the Responses experimental beta header, and forcing originator can make
		// ChatGPT select a different internal continuation path.
		upstreamReq.Header.Del("OpenAI-Beta")
		upstreamReq.Header.Del("originator")
	}
	if account.Type == AccountTypeOAuth && promptCacheKey != "" && strings.TrimSpace(c.GetHeader("conversation_id")) == "" {
		upstreamReq.Header.Del("conversation_id")
	}
	if compatTurnState != "" && upstreamReq.Header.Get("x-codex-turn-state") == "" {
		upstreamReq.Header.Set("x-codex-turn-state", compatTurnState)
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
		upstream, upstreamMsg, collectErr := s.collectOpenAIStructuredUpstreamError(resp, protocolconv.ProtocolOpenAIResponses, isStream)
		if collectErr != nil {
			return nil, collectErr
		}
		if account.Platform == PlatformGrok {
			s.updateGrokUsageSnapshot(ctx, account.ID, xai.ParseQuotaHeaders(upstream.Headers, upstream.StatusCode))
			s.handleGrokAccountUpstreamError(ctx, account, upstream.StatusCode, upstream.Headers, upstream.Body)
		}

		if previousResponseID != "" && (isOpenAICompatPreviousResponseNotFound(upstream.StatusCode, upstreamMsg, upstream.Body) || isOpenAICompatPreviousResponseUnsupported(upstream.StatusCode, upstreamMsg, upstream.Body)) {
			if isOpenAICompatPreviousResponseUnsupported(upstream.StatusCode, upstreamMsg, upstream.Body) {
				s.disableOpenAICompatSessionContinuation(ctx, c, account, promptCacheKey)
			} else {
				s.deleteOpenAICompatSessionResponseID(ctx, c, account, promptCacheKey)
			}
			logger.L().Info("openai messages: previous_response_id unavailable, retrying without continuation",
				zap.Int64("account_id", account.ID),
				zap.String("previous_response_id", truncateOpenAIWSLogValue(previousResponseID, openAIWSIDValueMaxLen)),
				zap.String("upstream_model", upstreamModel),
			)
			return s.ForwardAsAnthropic(ctx, c, account, body, promptCacheKey, defaultMappedModel)
		}
		if foErr := s.failoverOpenAIStructuredUpstreamError(ctx, c, account, upstream, upstreamMsg, upstreamModel); foErr != nil {
			return nil, foErr
		}
		return s.handleAnthropicErrorResponse(upstream, c, account, upstreamModel)
	}

	if account.Type == AccountTypeOAuth && promptCacheKey != "" {
		if turnState := strings.TrimSpace(resp.Header.Get("x-codex-turn-state")); turnState != "" {
			s.bindOpenAICompatSessionTurnState(ctx, c, account, promptCacheKey, turnState)
		}
	}

	// 9. Handle normal response
	// Upstream is always streaming; choose response format based on client preference.
	var result *OpenAIForwardResult
	var handleErr error
	if clientStream {
		result, handleErr = s.handleAnthropicStreamingResponse(resp, c, account, pipeline, originalModel, billingModel, upstreamModel, startTime)
	} else {
		// Client wants JSON: buffer the streaming response and assemble a JSON reply.
		result, handleErr = s.handleAnthropicBufferedStreamingResponse(resp, c, account, pipeline, originalModel, billingModel, upstreamModel, startTime)
	}

	// cyber_policy：标记已设、error 已按 Anthropic 格式发给客户端。丢弃 result、返回哨兵，
	// 使 handler 落入 tokens=0 免费用量行（对齐 /v1/responses），不计费、不 failover。
	if GetOpsCyberPolicy(c) != nil {
		if handleErr == nil {
			handleErr = errOpenAICyberPolicyForwarded
		}
		return nil, handleErr
	}

	// Propagate ServiceTier and ReasoningEffort to result for billing
	if handleErr == nil && result != nil {
		if compatContinuationEnabled && promptCacheKey != "" && result.ResponseID != "" {
			s.bindOpenAICompatSessionResponseID(ctx, c, account, promptCacheKey, result.ResponseID)
		}
		if promptCacheKey != "" && anthropicDigestChain != "" {
			s.bindOpenAICompatAnthropicDigestPromptCacheKey(account, apiKeyID, anthropicDigestChain, promptCacheKey, anthropicMatchedDigestChain)
		}
		result.ServiceTier = extractOpenAIServiceTierFromBody(responsesBody)
		result.ReasoningEffort = extractOpenAIReasoningEffortFromBody(responsesBody, upstreamModel)
	}

	// Extract and save Codex usage snapshot from response headers (for OAuth accounts).
	// 排除 spark 影子:其 codex_* 仅由 QueryUsage(/wham/usage bengalfox)更新(外审第7轮 P1)。
	if handleErr == nil && account.Type == AccountTypeOAuth && !account.IsShadow() {
		if account.Platform == PlatformGrok {
			s.updateGrokUsageSnapshot(ctx, account.ID, xai.ParseQuotaHeaders(resp.Header, resp.StatusCode))
		} else if snapshot := ParseCodexRateLimitHeaders(resp.Header); snapshot != nil {
			s.updateCodexUsageSnapshot(ctx, account.ID, snapshot)
		}
	}

	return result, handleErr
}

func ensureCodexOAuthInstructionsField(reqBody map[string]any) {
	if reqBody == nil {
		return
	}
	if value, ok := reqBody["instructions"]; !ok || value == nil {
		reqBody["instructions"] = ""
		return
	}
	if _, ok := reqBody["instructions"].(string); !ok {
		reqBody["instructions"] = ""
	}
}

// handleAnthropicErrorResponse applies Anthropic source policy to a structured
// upstream error.
func (s *OpenAIGatewayService) handleAnthropicErrorResponse(
	upstream protocoltransport.Response,
	c *gin.Context,
	account *Account,
	requestedModel ...string,
) (*OpenAIForwardResult, error) {
	return s.handleCompatErrorResponse(upstream, c, account, writeAnthropicError, requestedModel...)
}

// handleAnthropicBufferedStreamingResponse reads all Responses SSE events from
// the upstream streaming response, finds the terminal event (response.completed
// / response.incomplete / response.failed), converts the complete response to
// Anthropic Messages JSON format, and writes it to the client.
// This is used when the client requested stream=false but the upstream is always
// streaming.
func (s *OpenAIGatewayService) handleAnthropicBufferedStreamingResponse(
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

	terminal, err := s.collectOpenAICompatBufferedTerminal(resp, "openai messages buffered", startTime)
	if err != nil {
		return nil, err
	}
	finalResponse := terminal.Response
	usage := terminal.Usage

	if finalResponse == nil {
		writeAnthropicError(c, http.StatusBadGateway, "api_error", "Upstream stream ended without a terminal response event")
		return nil, fmt.Errorf("upstream stream ended without terminal event")
	}

	if strings.TrimSpace(finalResponse.Status) == "failed" {
		payload, _ := json.Marshal(gin.H{"type": "response.failed", "response": finalResponse})
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
			writeAnthropicError(c, http.StatusBadRequest, "invalid_request_error", clientMsg)
			return nil, fmt.Errorf("openai cyber_policy: %s", msg)
		}
		message := openAICompatFailedResponseMessage(finalResponse)
		if openAIStreamFailedEventShouldFailover(payload, message) {
			return nil, s.newOpenAIStreamFailoverError(c, account, false, requestID, payload, message, resp.Header)
		}
		message = s.recordOpenAIStreamUpstreamError(c, account, false, requestID, "http_error", payload, message)
		// 统一走语义状态推断 + body 归一化（与 /v1/responses 路径一致），
		// 使按错误码配置的透传规则可命中。
		if status, errType, errMsg, matched := applyOpenAIStreamFailedErrorPassthroughRule(
			c, account.Platform, payload, message,
		); matched {
			if errMsg == "" {
				errMsg = message
			}
			MarkResponseCommitted(c)
			writeAnthropicError(c, status, errType, errMsg)
			return nil, fmt.Errorf("upstream response failed (passthrough): %s", errMsg)
		}
		writeAnthropicError(c, http.StatusBadGateway, "api_error", message)
		return nil, fmt.Errorf("upstream response failed: %s", message)
	}

	terminal.Upstream.Body, err = prepareOpenAICompatBufferedResponseBody(finalResponse, terminal.Upstream.Body, terminal.Accumulator)
	if err != nil {
		return nil, err
	}
	if err := terminal.Upstream.Validate(); err != nil {
		return nil, fmt.Errorf("validate buffered Responses result: %w", err)
	}
	converted, err := pipeline.ConvertResponse(terminal.Upstream.Body, terminal.Upstream.ActualProtocol)
	if err != nil {
		return nil, fmt.Errorf("convert buffered Responses to Anthropic: %w", err)
	}
	renderer, err := protocolconv.NewRenderer(protocolconv.ProtocolAnthropic)
	if err != nil {
		return nil, err
	}
	if err := renderer.RenderJSON(c.Writer, terminal.Upstream.StatusCode, terminal.Upstream.Headers, converted.Body); err != nil {
		return nil, fmt.Errorf("render Anthropic response: %w", err)
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

func isOpenAICompatResponsesTerminalEvent(eventType string) bool {
	switch strings.TrimSpace(eventType) {
	case "response.completed", "response.done", "response.incomplete", "response.failed":
		return true
	default:
		return false
	}
}

func (s *OpenAIGatewayService) recordOpenAIMessagesStreamUpstreamError(c *gin.Context, account *Account, upstreamRequestID, kind, message string) {
	if c == nil {
		return
	}
	message = sanitizeUpstreamErrorMessage(message)
	setOpsUpstreamError(c, http.StatusBadGateway, message, "")
	event := OpsUpstreamErrorEvent{
		Platform:           PlatformOpenAI,
		UpstreamStatusCode: http.StatusBadGateway,
		UpstreamRequestID:  strings.TrimSpace(upstreamRequestID),
		Kind:               kind,
		Message:            message,
	}
	if account != nil {
		event.Platform = account.Platform
		event.AccountID = account.ID
		event.AccountName = account.Name
	}
	appendOpsUpstreamError(c, event)
}

type openAICompatBufferedTerminal struct {
	Upstream    protocoltransport.Response
	Response    *apicompat.ResponsesResponse
	Usage       OpenAIUsage
	Accumulator *apicompat.BufferedResponseAccumulator
}

type openAICompatStreamEvent struct {
	record protocoltransport.SSERecord
	err    error
}

func (s *OpenAIGatewayService) collectOpenAICompatResponsesStream(
	resp *http.Response,
	startTime time.Time,
) (*protocoltransport.Stream, *protocoltransport.SSEParser, error) {
	if resp == nil || resp.Body == nil {
		return nil, nil, errors.New("upstream response body is nil")
	}
	maxRecordSize := defaultMaxLineSize
	if s.cfg != nil && s.cfg.Gateway.MaxLineSize > 0 {
		maxRecordSize = s.cfg.Gateway.MaxLineSize
	}
	parser := protocoltransport.NewSSEParser(resp.Body, maxRecordSize)
	stream := &protocoltransport.Stream{
		StatusCode:     resp.StatusCode,
		Headers:        responseheaders.FilterHeaders(resp.Header, s.responseHeaderFilter),
		ActualProtocol: protocolconv.ProtocolOpenAIResponses,
		RequestID:      resp.Header.Get("x-request-id"),
		Duration:       time.Since(startTime),
		Events:         parser,
	}
	if err := stream.Validate(); err != nil {
		_ = stream.Close()
		return nil, nil, fmt.Errorf("validate Responses compat stream: %w", err)
	}
	return stream, parser, nil
}

func (s *OpenAIGatewayService) collectOpenAICompatBufferedTerminal(
	resp *http.Response,
	logPrefix string,
	startTime time.Time,
) (*openAICompatBufferedTerminal, error) {
	acc := apicompat.NewBufferedResponseAccumulator()
	result := &openAICompatBufferedTerminal{Accumulator: acc}
	stream, parser, err := s.collectOpenAICompatResponsesStream(resp, startTime)
	if err != nil {
		return result, err
	}
	defer func() { _ = stream.Close() }()

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

	for {
		select {
		case event, ok := <-events:
			if !ok {
				return result, nil
			}
			if event.err != nil {
				if errors.Is(event.err, protocoltransport.ErrSSEDone) || errors.Is(event.err, io.EOF) {
					return result, nil
				}
				if !errors.Is(event.err, context.Canceled) && !errors.Is(event.err, context.DeadlineExceeded) {
					logger.L().Warn(logPrefix+": read error",
						zap.Error(event.err),
						zap.String("request_id", stream.RequestID),
					)
				}
				return result, event.err
			}

			payload := openAICompatPayloadWithEventType(string(event.record.Data), event.record.Event)
			var parsed apicompat.ResponsesStreamEvent
			if err := json.Unmarshal([]byte(payload), &parsed); err != nil {
				logger.L().Warn(logPrefix+": failed to decode event",
					zap.Error(err),
					zap.String("request_id", stream.RequestID),
				)
				continue
			}
			acc.ProcessEvent(&parsed)
			if !isOpenAICompatResponsesTerminalEvent(parsed.Type) || parsed.Response == nil {
				continue
			}
			if parsed.Usage != nil {
				result.Usage = copyOpenAIUsageFromResponsesUsage(parsed.Usage)
				if parsed.Response.Usage == nil {
					parsed.Response.Usage = parsed.Usage
				}
			}
			if parsed.Response.Usage != nil {
				result.Usage = copyOpenAIUsageFromResponsesUsage(parsed.Response.Usage)
			}
			result.Response = parsed.Response
			result.Upstream = protocoltransport.Response{
				StatusCode: stream.StatusCode, Headers: protocoltransport.CloneHeaders(stream.Headers),
				Body: openAICompatTerminalResponseBody(payload), ActualProtocol: stream.ActualProtocol,
				RequestID: stream.RequestID, ResponseID: parsed.Response.ID, Duration: time.Since(startTime),
			}
			if err := result.Upstream.Validate(); err != nil {
				return result, fmt.Errorf("validate buffered Responses terminal: %w", err)
			}
			return result, nil

		case <-intervalCh:
			if time.Since(parser.Progress().LastReadAt) < streamInterval {
				continue
			}
			logger.L().Warn(logPrefix+": data interval timeout",
				zap.String("request_id", stream.RequestID),
				zap.Duration("interval", streamInterval),
			)
			return result, fmt.Errorf("stream data interval timeout")
		}
	}
}

func openAICompatTerminalResponseBody(payload string) []byte {
	response := gjson.Get(payload, "response")
	if !response.Exists() || response.Type != gjson.JSON {
		return nil
	}
	return append([]byte(nil), response.Raw...)
}

func prepareOpenAICompatBufferedResponseBody(
	finalResponse *apicompat.ResponsesResponse,
	terminalBody []byte,
	acc *apicompat.BufferedResponseAccumulator,
) ([]byte, error) {
	if finalResponse == nil {
		return nil, errors.New("buffered Responses terminal is nil")
	}
	terminalHadOutput := len(finalResponse.Output) > 0
	if acc != nil {
		acc.SupplementResponseOutput(finalResponse)
	}
	upstreamBody := append([]byte(nil), terminalBody...)
	if len(upstreamBody) == 0 {
		var err error
		upstreamBody, err = json.Marshal(finalResponse)
		if err != nil {
			return nil, fmt.Errorf("marshal buffered Responses result: %w", err)
		}
	}
	if !terminalHadOutput {
		output, err := json.Marshal(finalResponse.Output)
		if err != nil {
			return nil, fmt.Errorf("marshal reconstructed Responses output: %w", err)
		}
		upstreamBody, err = sjson.SetRawBytes(upstreamBody, "output", output)
		if err != nil {
			return nil, fmt.Errorf("set reconstructed Responses output: %w", err)
		}
	}
	if finalResponse.Usage != nil && !gjson.GetBytes(upstreamBody, "usage").Exists() {
		rawUsage, err := json.Marshal(finalResponse.Usage)
		if err != nil {
			return nil, fmt.Errorf("marshal buffered Responses usage: %w", err)
		}
		upstreamBody, err = sjson.SetRawBytes(upstreamBody, "usage", rawUsage)
		if err != nil {
			return nil, fmt.Errorf("set buffered Responses usage: %w", err)
		}
	}
	return upstreamBody, nil
}

// handleAnthropicStreamingResponse reads Responses SSE events from upstream,
// converts each to Anthropic SSE events, and writes them to the client.
// When StreamKeepaliveInterval is configured, it uses a goroutine + channel
// pattern to send Anthropic ping events during periods of upstream silence,
// preventing proxy/client timeout disconnections.
func (s *OpenAIGatewayService) handleAnthropicStreamingResponse(
	resp *http.Response,
	c *gin.Context,
	account *Account,
	pipeline *protocolconv.Pipeline,
	originalModel string,
	billingModel string,
	upstreamModel string,
	startTime time.Time,
) (*OpenAIForwardResult, error) {
	stream, parser, err := s.collectOpenAICompatResponsesStream(resp, startTime)
	if err != nil {
		return nil, err
	}
	defer func() { _ = stream.Close() }()
	requestID := stream.RequestID
	session, err := pipeline.NewStreamProcessor(stream.ActualProtocol)
	if err != nil {
		return nil, fmt.Errorf("create Anthropic Responses stream processor: %w", err)
	}
	renderer, err := protocolconv.NewRenderer(protocolconv.ProtocolAnthropic)
	if err != nil {
		return nil, err
	}
	headersWritten := false
	writeStreamHeaders := func() error {
		if headersWritten {
			return nil
		}
		if err := renderer.WriteStreamHeaders(c.Writer, stream.StatusCode, stream.Headers); err != nil {
			return err
		}
		headersWritten = true
		return nil
	}

	var usage OpenAIUsage
	responseID := ""
	var firstTokenMs *int
	firstChunk := true
	clientDisconnected := false
	clientOutputStarted := false
	var streamFailoverErr error
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

	// resultWithUsage builds the final result snapshot.
	resultWithUsage := func() *OpenAIForwardResult {
		return &OpenAIForwardResult{
			RequestID:        requestID,
			ResponseID:       responseID,
			ActualProtocol:   stream.ActualProtocol,
			Usage:            usage,
			Model:            originalModel,
			BillingModel:     billingModel,
			UpstreamModel:    upstreamModel,
			Stream:           true,
			Duration:         time.Since(startTime),
			FirstTokenMs:     firstTokenMs,
			ClientDisconnect: clientDisconnected,
		}
	}

	// processDataLine handles a single "data: ..." SSE line from upstream.
	processDataLine := func(payload string) bool {
		if firstChunk {
			firstChunk = false
			ms := int(time.Since(startTime).Milliseconds())
			firstTokenMs = &ms
		}

		var event apicompat.ResponsesStreamEvent
		if err := json.Unmarshal([]byte(payload), &event); err != nil {
			logger.L().Warn("openai messages stream: failed to parse event",
				zap.Error(err),
				zap.String("request_id", requestID),
			)
			return false
		}

		isTerminalEvent := isOpenAICompatResponsesTerminalEvent(event.Type)
		if isTerminalEvent {
			if event.Response != nil {
				if id := strings.TrimSpace(event.Response.ID); id != "" {
					responseID = id
				}
				if event.Response.Usage != nil {
					usage = copyOpenAIUsageFromResponsesUsage(event.Response.Usage)
				}
			}
			if event.Usage != nil {
				usage = copyOpenAIUsageFromResponsesUsage(event.Usage)
			}
			// cyber_policy 致命不可重试：标记供 handler 事后记录；以 Anthropic SSE error 事件
			// 回写让客户端感知并停止重试（F4），丢弃后续转换输出。
			if strings.TrimSpace(event.Type) == "response.failed" {
				payloadBytes := []byte(payload)
				if hit, code, msg := detectOpenAICyberPolicy(payloadBytes); hit {
					MarkOpsCyberPolicy(c, CyberPolicyMark{
						Code:           code,
						Message:        msg,
						Body:           truncateString(payload, 4096),
						UpstreamStatus: http.StatusOK,
						UpstreamInTok:  usage.InputTokens,
						UpstreamOutTok: usage.OutputTokens,
					})
					if !clientDisconnected {
						if err := writeStreamHeaders(); err != nil {
							streamNonFailoverErr = err
							return true
						}
						clientMsg := msg
						if clientMsg == "" {
							clientMsg = "Request blocked by upstream cyber-security policy"
						}
						if _, err := fmt.Fprint(c.Writer, buildAnthropicStreamErrorSSE("invalid_request_error", clientMsg)); err == nil {
							c.Writer.Flush()
						}
						clientDisconnected = true
					}
					return true
				}
				message := extractOpenAISSEErrorMessage(payloadBytes)
				// Once Anthropic output has started, switching accounts would splice
				// two model streams together. Surface a proper Anthropic error event
				// instead of returning a failover error that the handler cannot retry.
				if !clientOutputStarted && openAIStreamFailedEventShouldFailover(payloadBytes, message) {
					streamFailoverErr = s.newOpenAIStreamFailoverError(c, account, false, requestID, payloadBytes, message, resp.Header)
					return true
				}
				message = s.recordOpenAIStreamUpstreamError(c, account, false, requestID, "http_error", payloadBytes, message)
				errStatus, errType, errMsg := http.StatusBadGateway, "api_error", message
				// 统一走语义状态推断 + body 归一化（与 /v1/responses 路径一致），
				// 使按错误码配置的透传规则可命中。
				if status, et, em, matched := applyOpenAIStreamFailedErrorPassthroughRule(
					c, account.Platform, payloadBytes, message,
				); matched {
					if em == "" {
						em = errMsg
					}
					errStatus, errType, errMsg = status, et, em
					MarkResponseCommitted(c)
				}
				if !clientDisconnected {
					if !clientOutputStarted {
						writeAnthropicError(c, errStatus, errType, errMsg)
						clientOutputStarted = true
					} else {
						if err := writeStreamHeaders(); err != nil {
							streamNonFailoverErr = err
							return true
						}
						if _, err := fmt.Fprint(c.Writer, buildAnthropicStreamErrorSSE(errType, errMsg)); err == nil {
							c.Writer.Flush()
						}
					}
				}
				streamNonFailoverErr = fmt.Errorf("upstream response failed: %s", errMsg)
				return true
			}
		}

		payloads, _, conversionErr := session.Convert([]byte(payload))
		if conversionErr != nil {
			streamNonFailoverErr = fmt.Errorf("convert Responses stream event to Anthropic: %w", conversionErr)
			return true
		}
		if !clientDisconnected {
			for _, converted := range payloads {
				framed, err := renderer.FrameStreamEvent(converted)
				if err != nil {
					streamNonFailoverErr = fmt.Errorf("frame Anthropic stream event: %w", err)
					return true
				}
				if err := writeStreamHeaders(); err != nil {
					streamNonFailoverErr = err
					return true
				}
				if _, err := c.Writer.Write(framed); err != nil {
					clientDisconnected = true
					logger.L().Info("openai messages stream: client disconnected, continuing to drain upstream for billing",
						zap.String("request_id", requestID),
					)
					break
				}
				clientOutputStarted = true
			}
		}
		if len(payloads) > 0 && !clientDisconnected {
			c.Writer.Flush()
		}
		return isTerminalEvent
	}

	// finalizeStream sends any remaining Anthropic events and returns the result.
	finalizeStream := func() (*OpenAIForwardResult, error) {
		if streamFailoverErr != nil {
			return resultWithUsage(), streamFailoverErr
		}
		if streamNonFailoverErr != nil {
			return resultWithUsage(), streamNonFailoverErr
		}
		finalPayloads, _, err := session.Finalize()
		if err != nil {
			return resultWithUsage(), fmt.Errorf("finalize Anthropic Responses stream: %w", err)
		}
		if !clientDisconnected {
			for _, converted := range finalPayloads {
				framed, err := renderer.FrameStreamEvent(converted)
				if err != nil {
					return resultWithUsage(), err
				}
				if err := writeStreamHeaders(); err != nil {
					return resultWithUsage(), err
				}
				if _, err := c.Writer.Write(framed); err != nil {
					clientDisconnected = true
					logger.L().Info("openai messages stream: client disconnected during final flush",
						zap.String("request_id", requestID),
					)
					break
				}
				clientOutputStarted = true
			}
			if len(finalPayloads) > 0 && !clientDisconnected {
				c.Writer.Flush()
			}
		}
		return resultWithUsage(), nil
	}

	missingTerminalErr := func() (*OpenAIForwardResult, error) {
		result := resultWithUsage()
		if clientDisconnected {
			return result, fmt.Errorf("stream usage incomplete: missing terminal event")
		}
		message := "OpenAI messages stream ended before a terminal event"
		if !clientOutputStarted {
			return result, s.newOpenAIStreamFailoverError(c, account, false, requestID, nil, message)
		}
		s.recordOpenAIMessagesStreamUpstreamError(c, account, requestID, "stream_missing_terminal", message)
		return result, fmt.Errorf("stream usage incomplete: missing terminal event")
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
				if errors.Is(event.err, context.Canceled) || errors.Is(event.err, context.DeadlineExceeded) {
					return resultWithUsage(), fmt.Errorf("stream usage incomplete: %w", event.err)
				}
				logger.L().Warn("openai messages stream: read error",
					zap.Error(event.err),
					zap.String("request_id", requestID),
				)
				if clientDisconnected {
					return resultWithUsage(), fmt.Errorf("stream usage incomplete after disconnect: %w", event.err)
				}
				if !clientOutputStarted {
					message := "OpenAI messages stream disconnected before completion: " + sanitizeStreamError(event.err)
					return resultWithUsage(), s.newOpenAIStreamFailoverError(c, account, false, requestID, nil, message)
				}
				s.recordOpenAIMessagesStreamUpstreamError(c, account, requestID, "stream_read_error", event.err.Error())
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
			logger.L().Warn("openai messages stream: data interval timeout",
				zap.String("request_id", requestID),
				zap.String("model", originalModel),
				zap.Duration("interval", streamInterval),
			)
			return resultWithUsage(), fmt.Errorf("stream data interval timeout")

		case <-keepaliveCh:
			if clientDisconnected {
				continue
			}
			if parser.Progress().InRecord || time.Since(lastDataAt) < keepaliveInterval {
				continue
			}
			framed, err := renderer.FrameStreamEvent([]byte(`{"type":"ping"}`))
			if err != nil {
				return resultWithUsage(), err
			}
			if err := writeStreamHeaders(); err != nil {
				return resultWithUsage(), err
			}
			if _, err := c.Writer.Write(framed); err != nil {
				logger.L().Info("openai messages stream: client disconnected during keepalive",
					zap.String("request_id", requestID),
				)
				clientDisconnected = true
				continue
			}
			clientOutputStarted = true
			c.Writer.Flush()
		}
	}
}

// writeAnthropicError writes an error response in Anthropic Messages API format.
func writeAnthropicError(c *gin.Context, statusCode int, errType, message string) {
	c.JSON(statusCode, gin.H{
		"type": "error",
		"error": gin.H{
			"type":    errType,
			"message": message,
		},
	})
}

// buildAnthropicStreamErrorSSE builds one Anthropic SSE `error` event so a
// streaming response can terminate with a visible error (e.g. upstream
// cyber_policy) and programmatic clients stop retrying.
// Marshal 失败的兜底仅保留固定提示。
func buildAnthropicStreamErrorSSE(errType, message string) string {
	payload, err := json.Marshal(gin.H{
		"type": "error",
		"error": gin.H{
			"type":    errType,
			"message": message,
		},
	})
	if err != nil {
		return "event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"" + errType + "\",\"message\":\"upstream error\"}}\n\n"
	}
	return "event: error\ndata: " + string(payload) + "\n\n"
}

func copyOpenAIUsageFromResponsesUsage(usage *apicompat.ResponsesUsage) OpenAIUsage {
	if usage == nil {
		return OpenAIUsage{}
	}
	result := OpenAIUsage{
		InputTokens:              usage.InputTokens,
		OutputTokens:             usage.OutputTokens,
		CacheCreationInputTokens: usage.CacheCreationInputTokens,
	}
	if usage.InputTokensDetails != nil {
		result.CacheReadInputTokens = usage.InputTokensDetails.CachedTokens
		result.CacheCreation5mTokens = firstPositiveInt(
			usage.InputTokensDetails.CacheCreation5mTokens,
			usage.InputTokensDetails.Ephemeral5mInputTokens,
		)
		result.CacheCreation1hTokens = firstPositiveInt(
			usage.InputTokensDetails.CacheCreation1hTokens,
			usage.InputTokensDetails.Ephemeral1hInputTokens,
		)
	}
	return result
}

func firstPositiveInt(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

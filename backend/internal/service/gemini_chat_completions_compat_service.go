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
	"github.com/Wei-Shaw/sub2api/internal/pkg/geminicli"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv"
	protocoltransport "github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv/transport"
	"github.com/Wei-Shaw/sub2api/internal/util/responseheaders"
	"github.com/gin-gonic/gin"
)

// ForwardAsChatCompletions serves OpenAI Chat Completions clients through
// Gemini accounts. It keeps the client-facing response in Chat Completions
// format while routing the upstream call through Gemini native endpoints.
func (s *GeminiMessagesCompatService) ForwardAsChatCompletions(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	body []byte,
) (*ForwardResult, error) {
	startTime := time.Now()

	var ccReq apicompat.ChatCompletionsRequest
	if err := json.Unmarshal(body, &ccReq); err != nil {
		return nil, s.writeChatCompletionsError(c, http.StatusBadRequest, "invalid_request_error", "Failed to parse request body")
	}
	if strings.TrimSpace(ccReq.Model) == "" {
		return nil, s.writeChatCompletionsError(c, http.StatusBadRequest, "invalid_request_error", "model is required")
	}

	originalModel := ccReq.Model
	clientStream := ccReq.Stream
	includeUsage := ccReq.StreamOptions != nil && ccReq.StreamOptions.IncludeUsage

	return s.forwardGeminiBodyAsChatCompletions(ctx, c, account, originalModel, clientStream, includeUsage, startTime, body)
}

func (s *GeminiMessagesCompatService) forwardGeminiBodyAsChatCompletions(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	originalModel string,
	clientStream bool,
	includeUsage bool,
	startTime time.Time,
	originalChatBody []byte,
) (*ForwardResult, error) {
	var req struct {
		Model  string `json:"model"`
		Stream bool   `json:"stream"`
	}
	if err := json.Unmarshal(originalChatBody, &req); err != nil {
		return nil, s.writeChatCompletionsError(c, http.StatusBadRequest, "invalid_request_error", "Failed to parse request body")
	}
	if strings.TrimSpace(req.Model) == "" {
		return nil, s.writeChatCompletionsError(c, http.StatusBadRequest, "invalid_request_error", "model is required")
	}

	mappedModel := req.Model
	if account.Type == AccountTypeAPIKey || account.Type == AccountTypeServiceAccount {
		mappedModel = account.GetMappedModel(req.Model)
	}
	pipeline, err := protocolconv.NewPipeline(standardProtocolRegistry, protocolconv.PipelineConfig{
		Route: protocolconv.Route{
			Source: protocolconv.ProtocolOpenAIChat, IntendedTarget: protocolconv.ProtocolGoogleGenAI,
			ClientModel: originalModel, UpstreamModel: mappedModel, Provider: account.Platform, AccountID: account.ID,
		},
		Options: protocolconv.Options{SourceModel: mappedModel, LossPolicy: protocolconv.LossError},
	})
	if err != nil {
		return nil, fmt.Errorf("create chat Google pipeline: %w", err)
	}
	convertedRequest, err := pipeline.ConvertRequest(originalChatBody)
	if err != nil {
		return nil, s.writeChatCompletionsError(c, http.StatusBadRequest, "invalid_request_error", err.Error())
	}
	geminiReq := ensureGeminiFunctionCallThoughtSignatures(convertedRequest.Body)

	proxyURL := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}

	useUpstreamStream := clientStream
	if account.Type == AccountTypeOAuth && !clientStream && strings.TrimSpace(account.GetCredential("project_id")) != "" {
		useUpstreamStream = true
	}

	buildReq, requestIDHeader := s.buildGeminiChatCompletionsUpstreamRequestFunc(
		account,
		mappedModel,
		geminiReq,
		clientStream,
		useUpstreamStream,
	)

	var resp *http.Response
	for attempt := 1; attempt <= geminiMaxRetries; attempt++ {
		upstreamReq, idHeader, err := buildReq(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil, err
			}
			return nil, s.writeChatCompletionsError(c, http.StatusBadGateway, "upstream_error", err.Error())
		}
		requestIDHeader = idHeader

		resp, err = s.httpUpstream.Do(upstreamReq, proxyURL, account.ID, account.Concurrency)
		if err != nil {
			safeErr := sanitizeUpstreamErrorMessage(err.Error())
			appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
				Platform:           account.Platform,
				AccountID:          account.ID,
				AccountName:        account.Name,
				UpstreamStatusCode: 0,
				Kind:               "request_error",
				Message:            safeErr,
			})
			if attempt < geminiMaxRetries {
				logger.LegacyPrintf("service.gemini_chat_completions", "Gemini account %d: upstream request failed, retry %d/%d: %v", account.ID, attempt, geminiMaxRetries, err)
				sleepGeminiBackoff(attempt)
				continue
			}
			setOpsUpstreamError(c, 0, safeErr, "")
			return nil, s.writeChatCompletionsError(c, http.StatusBadGateway, "upstream_error", "Upstream request failed after retries: "+safeErr)
		}

		if matched, rebuilt := s.checkErrorPolicyInLoop(ctx, account, resp); matched {
			resp = rebuilt
			break
		} else {
			resp = rebuilt
		}

		if resp.StatusCode >= 400 && s.shouldRetryGeminiUpstreamError(account, resp.StatusCode) {
			respBody := s.readUpstreamErrorBody(resp)
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusForbidden && isGeminiInsufficientScope(resp.Header, respBody) {
				resp = &http.Response{
					StatusCode: resp.StatusCode,
					Header:     resp.Header.Clone(),
					Body:       io.NopCloser(bytes.NewReader(respBody)),
				}
				break
			}
			if resp.StatusCode == http.StatusTooManyRequests {
				s.handleGeminiUpstreamError(ctx, account, resp.StatusCode, resp.Header, respBody)
			}
			if attempt < geminiMaxRetries {
				upstreamReqID := resp.Header.Get(requestIDHeader)
				if upstreamReqID == "" {
					upstreamReqID = resp.Header.Get("x-goog-request-id")
				}
				upstreamMsg := strings.TrimSpace(extractUpstreamErrorMessage(respBody))
				upstreamMsg = sanitizeUpstreamErrorMessage(upstreamMsg)
				appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
					Platform:           account.Platform,
					AccountID:          account.ID,
					AccountName:        account.Name,
					UpstreamStatusCode: resp.StatusCode,
					UpstreamRequestID:  upstreamReqID,
					Kind:               "retry",
					Message:            upstreamMsg,
				})
				logger.LegacyPrintf("service.gemini_chat_completions", "Gemini account %d: upstream status %d, retry %d/%d", account.ID, resp.StatusCode, attempt, geminiMaxRetries)
				sleepGeminiBackoff(attempt)
				continue
			}
			resp = &http.Response{
				StatusCode: resp.StatusCode,
				Header:     resp.Header.Clone(),
				Body:       io.NopCloser(bytes.NewReader(respBody)),
			}
			break
		}

		break
	}

	requestID := resp.Header.Get(requestIDHeader)
	if requestID == "" {
		requestID = resp.Header.Get("x-goog-request-id")
	}
	if requestID != "" {
		c.Header("x-request-id", requestID)
	}

	reasoningEffort := extractCCReasoningEffortFromBody(originalChatBody)
	// 国产模型默认 effort 补充（本路径上游是 Gemini，不会命中 passback-required）。
	// 保持与 OpenAI 网关路径调用模式一致，便于未来上游变异时语义一致。
	reasoningEffort = ApplyThinkingEnabledFallback(reasoningEffort, originalChatBody, mappedModel)

	if resp.StatusCode >= 400 {
		defer func() { _ = resp.Body.Close() }()
		respBody := s.readUpstreamErrorBody(resp)
		s.handleGeminiUpstreamError(ctx, account, resp.StatusCode, resp.Header, respBody)
		evBody := unwrapIfNeeded(account.Type == AccountTypeOAuth, respBody)

		if s.shouldFailoverGeminiUpstreamError(resp.StatusCode) {
			upstreamMsg := sanitizeUpstreamErrorMessage(strings.TrimSpace(extractUpstreamErrorMessage(evBody)))
			appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
				Platform:           account.Platform,
				AccountID:          account.ID,
				AccountName:        account.Name,
				UpstreamStatusCode: resp.StatusCode,
				UpstreamRequestID:  requestID,
				Kind:               "failover",
				Message:            upstreamMsg,
			})
			return nil, &UpstreamFailoverError{StatusCode: resp.StatusCode, ResponseBody: evBody}
		}

		return nil, s.writeGeminiChatCompletionsMappedError(c, account, resp.StatusCode, requestID, evBody)
	}

	var usage *ClaudeUsage
	var firstTokenMs *int
	if clientStream {
		streamRes, err := s.handleChatCompletionsStreamingResponseFromGemini(c, resp, pipeline, startTime, account.Type == AccountTypeOAuth, includeUsage)
		if err != nil {
			return nil, err
		}
		usage = streamRes.usage
		firstTokenMs = streamRes.firstTokenMs
	} else if useUpstreamStream {
		defer func() { _ = resp.Body.Close() }()
		collected, usageObj, err := collectGeminiSSE(resp.Body, account.Type == AccountTypeOAuth)
		if err != nil {
			return nil, s.writeChatCompletionsError(c, http.StatusBadGateway, "upstream_error", "Failed to read upstream stream")
		}
		collectedBytes, _ := json.Marshal(collected)
		usageObj2, err := s.renderGoogleChatResponse(c, resp, pipeline, collectedBytes, usageObj, startTime, nil)
		if err != nil {
			return nil, err
		}
		usage = usageObj2
	} else {
		defer func() { _ = resp.Body.Close() }()
		usageResp, err := s.handleChatCompletionsNonStreamingResponseFromGemini(c, resp, pipeline, account.Type == AccountTypeOAuth, startTime)
		if err != nil {
			return nil, err
		}
		usage = usageResp
	}

	if usage == nil {
		usage = &ClaudeUsage{}
	}

	imageCount := 0
	imageInputSize := s.extractImageInputSize(geminiReq)
	imageSize := normalizeOpenAIImageSizeTier(imageInputSize)
	if isImageGenerationModel(originalModel) {
		imageCount = 1
	}

	return &ForwardResult{
		RequestID:        requestID,
		Usage:            *usage,
		Model:            originalModel,
		UpstreamModel:    mappedModel,
		Stream:           clientStream,
		Duration:         time.Since(startTime),
		FirstTokenMs:     firstTokenMs,
		ReasoningEffort:  reasoningEffort,
		ImageCount:       imageCount,
		ImageSize:        imageSize,
		ImageInputSize:   imageInputSize,
		ClientDisconnect: false,
	}, nil
}

func (s *GeminiMessagesCompatService) buildGeminiChatCompletionsUpstreamRequestFunc(
	account *Account,
	mappedModel string,
	geminiReq []byte,
	clientStream bool,
	useUpstreamStream bool,
) (func(context.Context) (*http.Request, string, error), string) {
	switch account.Type {
	case AccountTypeAPIKey:
		return func(ctx context.Context) (*http.Request, string, error) {
			apiKey := account.GetCredential("api_key")
			if strings.TrimSpace(apiKey) == "" {
				return nil, "", errors.New("gemini api_key not configured")
			}

			baseURL := account.GetGeminiBaseURL(geminicli.AIStudioBaseURL)
			normalizedBaseURL, err := s.validateUpstreamBaseURL(baseURL)
			if err != nil {
				return nil, "", err
			}

			action := "generateContent"
			if clientStream {
				action = "streamGenerateContent"
			}
			fullURL := fmt.Sprintf("%s/v1beta/models/%s:%s", strings.TrimRight(normalizedBaseURL, "/"), mappedModel, action)
			if clientStream {
				fullURL += "?alt=sse"
			}

			restGeminiReq := normalizeGeminiRequestForAIStudio(geminiReq)
			upstreamReq, err := http.NewRequestWithContext(ctx, http.MethodPost, fullURL, bytes.NewReader(restGeminiReq))
			if err != nil {
				return nil, "", err
			}
			upstreamReq.Header.Set("Content-Type", "application/json")
			upstreamReq.Header.Set("x-goog-api-key", apiKey)
			return upstreamReq, "x-request-id", nil
		}, "x-request-id"

	case AccountTypeOAuth:
		return func(ctx context.Context) (*http.Request, string, error) {
			if s.tokenProvider == nil {
				return nil, "", errors.New("gemini token provider not configured")
			}
			accessToken, err := s.tokenProvider.GetAccessToken(ctx, account)
			if err != nil {
				return nil, "", err
			}

			projectID := strings.TrimSpace(account.GetCredential("project_id"))
			action := "generateContent"
			if useUpstreamStream {
				action = "streamGenerateContent"
			}

			if projectID != "" {
				baseURL, err := s.validateUpstreamBaseURL(geminicli.GeminiCliBaseURL)
				if err != nil {
					return nil, "", err
				}
				fullURL := fmt.Sprintf("%s/v1internal:%s", strings.TrimRight(baseURL, "/"), action)
				if useUpstreamStream {
					fullURL += "?alt=sse"
				}

				var inner any
				if err := json.Unmarshal(geminiReq, &inner); err != nil {
					return nil, "", fmt.Errorf("failed to parse gemini request: %w", err)
				}
				wrappedBytes, _ := json.Marshal(map[string]any{
					"model":   mappedModel,
					"project": projectID,
					"request": inner,
				})

				upstreamReq, err := http.NewRequestWithContext(ctx, http.MethodPost, fullURL, bytes.NewReader(wrappedBytes))
				if err != nil {
					return nil, "", err
				}
				upstreamReq.Header.Set("Content-Type", "application/json")
				upstreamReq.Header.Set("Authorization", "Bearer "+accessToken)
				upstreamReq.Header.Set("User-Agent", geminicli.GeminiCLIUserAgent)
				return upstreamReq, "x-request-id", nil
			}

			baseURL := account.GetGeminiBaseURL(geminicli.AIStudioBaseURL)
			normalizedBaseURL, err := s.validateUpstreamBaseURL(baseURL)
			if err != nil {
				return nil, "", err
			}

			fullURL := fmt.Sprintf("%s/v1beta/models/%s:%s", strings.TrimRight(normalizedBaseURL, "/"), mappedModel, action)
			if useUpstreamStream {
				fullURL += "?alt=sse"
			}

			restGeminiReq := normalizeGeminiRequestForAIStudio(geminiReq)
			upstreamReq, err := http.NewRequestWithContext(ctx, http.MethodPost, fullURL, bytes.NewReader(restGeminiReq))
			if err != nil {
				return nil, "", err
			}
			upstreamReq.Header.Set("Content-Type", "application/json")
			upstreamReq.Header.Set("Authorization", "Bearer "+accessToken)
			return upstreamReq, "x-request-id", nil
		}, "x-request-id"

	case AccountTypeServiceAccount:
		return func(ctx context.Context) (*http.Request, string, error) {
			if s.tokenProvider == nil {
				return nil, "", errors.New("gemini token provider not configured")
			}
			accessToken, err := s.tokenProvider.GetAccessToken(ctx, account)
			if err != nil {
				return nil, "", err
			}

			action := "generateContent"
			if clientStream {
				action = "streamGenerateContent"
			}
			fullURL, err := buildVertexGeminiURL(account.VertexProjectID(), account.VertexLocation(mappedModel), mappedModel, action, clientStream)
			if err != nil {
				return nil, "", err
			}

			restGeminiReq := normalizeGeminiRequestForAIStudio(geminiReq)
			upstreamReq, err := http.NewRequestWithContext(ctx, http.MethodPost, fullURL, bytes.NewReader(restGeminiReq))
			if err != nil {
				return nil, "", err
			}
			upstreamReq.Header.Set("Content-Type", "application/json")
			upstreamReq.Header.Set("Authorization", "Bearer "+accessToken)
			return upstreamReq, "x-request-id", nil
		}, "x-request-id"

	default:
		return func(context.Context) (*http.Request, string, error) {
			return nil, "", fmt.Errorf("unsupported account type: %s", account.Type)
		}, "x-request-id"
	}
}

func (s *GeminiMessagesCompatService) handleChatCompletionsNonStreamingResponseFromGemini(
	c *gin.Context,
	resp *http.Response,
	pipeline *protocolconv.Pipeline,
	isOAuth bool,
	startTime time.Time,
) (*ClaudeUsage, error) {
	respBody, err := ReadUpstreamResponseBody(resp.Body, s.cfg, c, openAITooLargeError)
	if err != nil {
		return nil, err
	}
	rawUpstreamBody := append([]byte(nil), respBody...)
	if isOAuth {
		if unwrappedBody, uwErr := unwrapGeminiResponse(respBody); uwErr == nil {
			respBody = unwrappedBody
		}
	}

	return s.renderGoogleChatResponse(c, resp, pipeline, respBody, nil, startTime, rawUpstreamBody)
}

func (s *GeminiMessagesCompatService) renderGoogleChatResponse(
	c *gin.Context,
	resp *http.Response,
	pipeline *protocolconv.Pipeline,
	googleBody []byte,
	usageOverride *ClaudeUsage,
	startTime time.Time,
	rawUpstreamBody []byte,
) (*ClaudeUsage, error) {
	googleBody = withGoogleUsageOverride(googleBody, usageOverride)
	usage := extractGeminiUsage(googleBody)
	if usage == nil {
		usage = &ClaudeUsage{}
	}
	var envelope struct {
		ResponseID string `json:"responseId"`
	}
	_ = json.Unmarshal(googleBody, &envelope)
	structured := protocoltransport.Response{
		StatusCode: resp.StatusCode, Headers: responseheaders.FilterHeaders(resp.Header, s.responseHeaderFilter),
		Body: googleBody, ActualProtocol: protocolconv.ProtocolGoogleGenAI,
		RequestID: resp.Header.Get("x-request-id"), ResponseID: envelope.ResponseID, Duration: time.Since(startTime),
	}
	if len(rawUpstreamBody) > 0 {
		structured.Metadata = map[string]any{"raw_upstream_body": append([]byte(nil), rawUpstreamBody...)}
	}
	if err := structured.Validate(); err != nil {
		return nil, fmt.Errorf("collect Google response: %w", err)
	}
	converted, err := pipeline.ConvertResponse(structured.Body, structured.ActualProtocol)
	if err != nil {
		return nil, s.writeChatCompletionsError(c, http.StatusBadGateway, "upstream_error", "Failed to convert upstream response")
	}
	renderer, err := protocolconv.NewRenderer(protocolconv.ProtocolOpenAIChat)
	if err != nil {
		return nil, err
	}
	if err := renderer.RenderJSON(c.Writer, structured.StatusCode, structured.Headers, converted.Body); err != nil {
		return nil, err
	}
	return usage, nil
}

func withGoogleUsageOverride(googleBody []byte, usage *ClaudeUsage) []byte {
	if usage == nil {
		return googleBody
	}
	var wire map[string]any
	if json.Unmarshal(googleBody, &wire) != nil {
		return googleBody
	}
	wire["usageMetadata"] = map[string]any{
		"promptTokenCount":        usage.InputTokens + usage.CacheReadInputTokens,
		"candidatesTokenCount":    usage.OutputTokens,
		"totalTokenCount":         usage.InputTokens + usage.CacheReadInputTokens + usage.OutputTokens,
		"cachedContentTokenCount": usage.CacheReadInputTokens,
	}
	converted, err := json.Marshal(wire)
	if err != nil {
		return googleBody
	}
	return converted
}

func (s *GeminiMessagesCompatService) handleChatCompletionsStreamingResponseFromGemini(
	c *gin.Context,
	resp *http.Response,
	pipeline *protocolconv.Pipeline,
	startTime time.Time,
	isOAuth bool,
	includeUsage bool,
) (*geminiStreamResult, error) {
	if _, ok := c.Writer.(http.Flusher); !ok {
		return nil, errors.New("streaming not supported")
	}
	maxRecordSize := protocoltransport.DefaultMaxSSERecordBytes
	if s.cfg != nil && s.cfg.Gateway.MaxLineSize > 0 {
		maxRecordSize = s.cfg.Gateway.MaxLineSize
	}
	var events protocoltransport.EventStream = protocoltransport.NewSSEParser(resp.Body, maxRecordSize)
	if isOAuth {
		transformed, err := protocoltransport.NewTransformEventStream(events, unwrapGeminiResponse)
		if err != nil {
			return nil, err
		}
		events = transformed
	}
	stream := &protocoltransport.Stream{
		StatusCode: resp.StatusCode, Headers: responseheaders.FilterHeaders(resp.Header, s.responseHeaderFilter),
		ActualProtocol: protocolconv.ProtocolGoogleGenAI, RequestID: resp.Header.Get("x-request-id"),
		Duration: time.Since(startTime), Events: events,
	}
	if err := stream.Validate(); err != nil {
		_ = stream.Close()
		return nil, err
	}
	defer func() { _ = stream.Close() }()
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
	writePayloads := func(payloads [][]byte) (bool, error) {
		visible := payloads[:0]
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
		payloads = visible
		wrote := false
		for _, payload := range payloads {
			framed, err := renderer.FrameStreamEvent(payload)
			if err != nil {
				return false, err
			}
			if _, err := c.Writer.Write(framed); err != nil {
				return true, nil
			}
			wrote = true
		}
		if wrote {
			c.Writer.Flush()
		}
		return false, nil
	}

	for {
		record, err := stream.Events.Next(context.Background())
		if errors.Is(err, io.EOF) || errors.Is(err, protocoltransport.ErrSSEDone) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("stream read error: %w", err)
		}
		if firstTokenMs == nil {
			ms := int(time.Since(startTime).Milliseconds())
			firstTokenMs = &ms
		}
		if current := extractGeminiUsage(record.Data); current != nil {
			usage = *current
		}
		converted, _, err := session.Convert(record.Data)
		if err != nil {
			return nil, fmt.Errorf("convert Google stream event: %w", err)
		}
		disconnected, err := writePayloads(converted)
		if err != nil {
			return nil, err
		}
		if disconnected {
			return &geminiStreamResult{usage: &usage, firstTokenMs: firstTokenMs}, nil
		}
	}
	finalPayloads, _, err := session.Finalize()
	if err != nil {
		return nil, fmt.Errorf("finalize Google stream: %w", err)
	}
	disconnected, err := writePayloads(finalPayloads)
	if err != nil {
		return nil, err
	}
	if disconnected {
		return &geminiStreamResult{usage: &usage, firstTokenMs: firstTokenMs}, nil
	}
	if !headersWritten {
		if err := renderer.WriteStreamHeaders(c.Writer, stream.StatusCode, stream.Headers); err != nil {
			return nil, err
		}
	}
	_, _ = c.Writer.Write(renderer.StreamTerminal())
	c.Writer.Flush()
	return &geminiStreamResult{usage: &usage, firstTokenMs: firstTokenMs}, nil
}

func (s *GeminiMessagesCompatService) writeGeminiChatCompletionsMappedError(
	c *gin.Context,
	account *Account,
	upstreamStatus int,
	upstreamRequestID string,
	body []byte,
) error {
	upstreamMsg := sanitizeUpstreamErrorMessage(strings.TrimSpace(extractUpstreamErrorMessage(body)))
	setOpsUpstreamError(c, upstreamStatus, upstreamMsg, "")
	if account != nil {
		appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
			Platform:           account.Platform,
			AccountID:          account.ID,
			AccountName:        account.Name,
			UpstreamStatusCode: upstreamStatus,
			UpstreamRequestID:  upstreamRequestID,
			Kind:               "http_error",
			Message:            upstreamMsg,
		})
	}

	if status, errType, errMsg, matched := applyErrorPassthroughRule(
		c,
		PlatformGemini,
		upstreamStatus,
		body,
		http.StatusBadGateway,
		"upstream_error",
		"Upstream request failed",
	); matched {
		return s.writeChatCompletionsError(c, status, errType, errMsg)
	}

	statusCode := http.StatusBadGateway
	errType := "upstream_error"
	errMsg := "Upstream request failed"
	if mapped := mapGeminiErrorBodyToClaudeError(body); mapped != nil {
		if mapped.Type != "" {
			errType = mapped.Type
		}
		if mapped.Message != "" {
			errMsg = mapped.Message
		}
		if mapped.StatusCode > 0 {
			statusCode = mapped.StatusCode
		}
	}

	switch upstreamStatus {
	case http.StatusBadRequest:
		if statusCode == http.StatusBadGateway {
			statusCode = http.StatusBadRequest
		}
		if errType == "upstream_error" {
			errType = "invalid_request_error"
		}
		if errMsg == "Upstream request failed" {
			errMsg = "Invalid request"
		}
	case http.StatusNotFound:
		statusCode = http.StatusNotFound
		if errType == "upstream_error" {
			errType = "not_found_error"
		}
		if errMsg == "Upstream request failed" {
			errMsg = "Resource not found"
		}
	case http.StatusTooManyRequests:
		statusCode = http.StatusTooManyRequests
		if errType == "upstream_error" {
			errType = "rate_limit_error"
		}
		if errMsg == "Upstream request failed" {
			errMsg = "Upstream rate limit exceeded, please retry later"
		}
	case 529:
		statusCode = http.StatusServiceUnavailable
		if errType == "upstream_error" {
			errType = "overloaded_error"
		}
		if errMsg == "Upstream request failed" {
			errMsg = "Upstream service overloaded, please retry later"
		}
	}

	if upstreamMsg != "" && errMsg == "Upstream request failed" {
		errMsg = upstreamMsg
	}
	return s.writeChatCompletionsError(c, statusCode, errType, errMsg)
}

func (s *GeminiMessagesCompatService) writeChatCompletionsError(c *gin.Context, status int, errType, message string) error {
	c.JSON(status, gin.H{
		"error": gin.H{
			"type":    errType,
			"message": message,
		},
	})
	return fmt.Errorf("%s", message)
}

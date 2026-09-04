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

	return s.forwardAsChatCompletionsWithClientModel(ctx, c, account, body, ccReq.Model, startTime)
}

// ForwardAsChatCompletionsWithClientModel preserves the inbound model when the
// handler has already applied a channel-level model mapping to body.
func (s *GeminiMessagesCompatService) ForwardAsChatCompletionsWithClientModel(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	body []byte,
	clientModel string,
) (*ForwardResult, error) {
	return s.forwardAsChatCompletionsWithClientModel(ctx, c, account, body, clientModel, time.Now())
}

func (s *GeminiMessagesCompatService) forwardAsChatCompletionsWithClientModel(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	body []byte,
	clientModel string,
	startTime time.Time,
) (*ForwardResult, error) {
	var req apicompat.ChatCompletionsRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, s.writeChatCompletionsError(c, http.StatusBadRequest, "invalid_request_error", "Failed to parse request body")
	}
	if strings.TrimSpace(clientModel) == "" {
		clientModel = req.Model
	}
	includeUsage := req.StreamOptions != nil && req.StreamOptions.IncludeUsage
	return s.forwardGeminiBodyAsChatCompletions(ctx, c, account, clientModel, req.Stream, includeUsage, startTime, body)
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
	pipelineConfig := protocolconv.PipelineConfig{
		Route: protocolconv.Route{
			Source: protocolconv.ProtocolOpenAIChat, IntendedTarget: protocolconv.ProtocolGoogleGenAI,
			ClientModel: originalModel, UpstreamModel: mappedModel, Provider: account.Platform, AccountID: account.ID,
		},
		Options: protocolconv.Options{SourceModel: mappedModel, LossPolicy: protocolconv.LossError},
	}
	s.configureGoogleMetadataBridge(ctx, account, &pipelineConfig)
	pipeline, err := protocolconv.NewPipeline(standardProtocolRegistry, pipelineConfig)
	if err != nil {
		return nil, fmt.Errorf("create chat Google pipeline: %w", err)
	}
	convertedRequest, err := pipeline.ConvertRequest(originalChatBody)
	if err != nil {
		return nil, s.writeChatCompletionsError(c, http.StatusBadRequest, "invalid_request_error", err.Error())
	}
	geminiReq := ensureGeminiFunctionCallThoughtSignatures(convertedRequest.Body)
	return s.forwardGoogleProtocolRequest(ctx, c, account, googleProtocolRequest{
		Source: protocolconv.ProtocolOpenAIChat, Pipeline: pipeline, GoogleBody: geminiReq,
		OriginalBody: originalChatBody, OriginalModel: originalModel, MappedModel: mappedModel,
		ClientStream: clientStream, UpstreamStream: clientStream, IncludeUsage: includeUsage, StartTime: startTime,
		WriteError: func(status int, errorType, message string) error {
			return s.writeChatCompletionsError(c, status, errorType, message)
		},
		WriteMappedError: func(status int, requestID string, body []byte) error {
			return s.writeGeminiChatCompletionsMappedError(c, account, status, requestID, body)
		},
	})
}

func extractGoogleCompatReasoningEffort(source protocolconv.Protocol, body []byte) *string {
	if source == protocolconv.ProtocolOpenAIResponses {
		return ExtractResponsesReasoningEffortFromBody(body)
	}
	return extractCCReasoningEffortFromBody(body)
}

type googleProtocolRequest struct {
	Source           protocolconv.Protocol
	Pipeline         *protocolconv.Pipeline
	GoogleBody       []byte
	OriginalBody     []byte
	OriginalModel    string
	MappedModel      string
	ClientStream     bool
	UpstreamStream   bool
	IncludeUsage     bool
	StartTime        time.Time
	WriteError       func(status int, errorType, message string) error
	WriteMappedError func(status int, requestID string, body []byte) error
}

func (s *GeminiMessagesCompatService) forwardGoogleProtocolRequest(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	input googleProtocolRequest,
) (*ForwardResult, error) {
	originalModel := input.OriginalModel
	mappedModel := input.MappedModel
	clientStream := input.ClientStream
	useUpstreamStream := input.UpstreamStream
	pipeline := input.Pipeline
	geminiReq := input.GoogleBody
	startTime := input.StartTime
	writeError := input.WriteError
	if writeError == nil {
		return nil, errors.New("error writer is required for Google protocol forwarding")
	}

	proxyURL := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}

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
	var lastError *protocoltransport.Response
	for attempt := 1; attempt <= geminiMaxRetries; attempt++ {
		upstreamReq, idHeader, err := buildReq(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil, err
			}
			return nil, writeError(http.StatusBadGateway, "upstream_error", err.Error())
		}
		requestIDHeader = idHeader

		resp, err = s.httpUpstream.Do(upstreamReq, proxyURL, account.ID, account.Concurrency)
		if err != nil {
			safeErr := sanitizeUpstreamErrorMessage(err.Error())
			appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
				ProxyID:            opsUpstreamProxyID(account),
				ProxyName:          opsUpstreamProxyName(account),
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
			return nil, writeError(http.StatusBadGateway, "upstream_error", "Upstream request failed after retries: "+safeErr)
		}

		if resp.StatusCode >= http.StatusBadRequest {
			upstream, collectErr := s.collectGeminiStructuredUpstreamError(resp, useUpstreamStream, requestIDHeader)
			if collectErr != nil {
				return nil, writeError(http.StatusBadGateway, "upstream_error", "Failed to read upstream error")
			}
			lastError = &upstream
			if s.checkStructuredErrorPolicyInLoop(ctx, account, upstream, mappedModel) {
				break
			}
			if s.shouldRetryGeminiUpstreamError(account, upstream.StatusCode) {
				if upstream.StatusCode == http.StatusForbidden && isGeminiInsufficientScope(upstream.Headers, upstream.Body) {
					break
				}
				if upstream.StatusCode == http.StatusTooManyRequests {
					s.handleGeminiUpstreamError(ctx, account, upstream.StatusCode, upstream.Headers, upstream.Body)
				}
				if attempt < geminiMaxRetries {
					upstreamMsg := sanitizeUpstreamErrorMessage(strings.TrimSpace(extractUpstreamErrorMessage(upstream.Body)))
					appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
						ProxyID:            opsUpstreamProxyID(account),
						ProxyName:          opsUpstreamProxyName(account),
						Platform:           account.Platform,
						AccountID:          account.ID,
						AccountName:        account.Name,
						UpstreamStatusCode: upstream.StatusCode,
						UpstreamRequestID:  upstream.RequestID,
						Kind:               "retry",
						Message:            upstreamMsg,
					})
					logger.LegacyPrintf("service.gemini_chat_completions", "Gemini account %d: upstream status %d, retry %d/%d", account.ID, upstream.StatusCode, attempt, geminiMaxRetries)
					lastError = nil
					sleepGeminiBackoff(attempt)
					continue
				}
			}
			break
		}

		lastError = nil
		break
	}

	requestID := ""
	if lastError != nil {
		requestID = lastError.RequestID
	} else if resp != nil {
		requestID = resp.Header.Get(requestIDHeader)
		if requestID == "" {
			requestID = resp.Header.Get("x-goog-request-id")
		}
	}
	if requestID != "" {
		c.Header("x-request-id", requestID)
	}

	reasoningEffort := extractGoogleCompatReasoningEffort(input.Source, input.OriginalBody)
	// 国产模型默认 effort 补充（本路径上游是 Gemini，不会命中 passback-required）。
	// 保持与 OpenAI 网关路径调用模式一致，便于未来上游变异时语义一致。
	reasoningEffort = ApplyThinkingEnabledFallback(reasoningEffort, input.OriginalBody, mappedModel)

	if lastError != nil {
		policy := ErrorPolicyNone
		if s.rateLimitService != nil {
			policy = s.rateLimitService.CheckErrorPolicy(ctx, account, lastError.StatusCode, lastError.Body, mappedModel)
		}
		// 只有未命中策略或已明确匹配的策略继续执行账号状态处理。
		if policy == ErrorPolicyNone || policy == ErrorPolicyMatched {
			s.handleGeminiUpstreamError(ctx, account, lastError.StatusCode, lastError.Headers, lastError.Body)
		}
		evBody := unwrapIfNeeded(account.Type == AccountTypeOAuth, lastError.Body)

		if s.shouldFailoverGeminiUpstreamError(lastError.StatusCode) {
			upstreamMsg := sanitizeUpstreamErrorMessage(strings.TrimSpace(extractUpstreamErrorMessage(evBody)))
			appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
				ProxyID:            opsUpstreamProxyID(account),
				ProxyName:          opsUpstreamProxyName(account),
				Platform:           account.Platform,
				AccountID:          account.ID,
				AccountName:        account.Name,
				UpstreamStatusCode: lastError.StatusCode,
				UpstreamRequestID:  requestID,
				Kind:               "failover",
				Message:            upstreamMsg,
			})
			return nil, &UpstreamFailoverError{
				StatusCode:             lastError.StatusCode,
				ResponseBody:           evBody,
				ResponseHeaders:        protocoltransport.CloneHeaders(lastError.Headers),
				RetryableOnSameAccount: account.IsPoolMode() && account.IsPoolModeRetryableStatus(lastError.StatusCode),
			}
		}

		if policy == ErrorPolicySkipped && account.IsCustomErrorCodesEnabled() {
			return nil, s.writeGeminiCustomCodeSkippedError(c, account, lastError.StatusCode, requestID, evBody, func() {
				_ = writeError(http.StatusInternalServerError, "api_error", geminiCustomCodeSkippedClientMessage)
			})
		}
		if input.WriteMappedError == nil {
			return nil, writeError(http.StatusBadGateway, "upstream_error", "Upstream request failed")
		}
		return nil, input.WriteMappedError(lastError.StatusCode, requestID, evBody)
	}

	var usage *ClaudeUsage
	var firstTokenMs *int
	clientDisconnected := false
	if clientStream {
		streamRes, err := s.handleGoogleProtocolStreamingResponse(c, resp, pipeline, input.Source, startTime, account.Type == AccountTypeOAuth, input.IncludeUsage)
		if err != nil {
			return nil, err
		}
		usage = streamRes.usage
		firstTokenMs = streamRes.firstTokenMs
		clientDisconnected = streamRes.clientDisconnected
	} else if useUpstreamStream {
		collected, usageObj, rawStreamBody, err := s.collectGeminiSSEWithRaw(resp.Body, account.Type == AccountTypeOAuth)
		if err != nil {
			return nil, writeError(http.StatusBadGateway, "upstream_error", "Failed to read upstream stream")
		}
		collectedBytes, _ := json.Marshal(collected)
		usageObj2, err := s.renderGoogleProtocolResponse(c, resp, pipeline, input.Source, collectedBytes, usageObj, startTime, rawStreamBody, writeError)
		if err != nil {
			return nil, err
		}
		usage = usageObj2
	} else {
		defer func() { _ = resp.Body.Close() }()
		usageResp, err := s.handleGoogleProtocolNonStreamingResponse(c, resp, pipeline, input.Source, account.Type == AccountTypeOAuth, startTime, writeError)
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
		ActualProtocol:   protocolconv.ProtocolGoogleGenAI,
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
		ClientDisconnect: clientDisconnected,
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
			fullURL, err := buildGeminiAIStudioModelActionURL(normalizedBaseURL, mappedModel, action, clientStream)
			if err != nil {
				return nil, "", err
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

			fullURL, err := buildGeminiAIStudioModelActionURL(normalizedBaseURL, mappedModel, action, useUpstreamStream)
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

func (s *GeminiMessagesCompatService) handleGoogleProtocolNonStreamingResponse(
	c *gin.Context,
	resp *http.Response,
	pipeline *protocolconv.Pipeline,
	source protocolconv.Protocol,
	isOAuth bool,
	startTime time.Time,
	writeError func(status int, errorType, message string) error,
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

	return s.renderGoogleProtocolResponse(c, resp, pipeline, source, respBody, nil, startTime, rawUpstreamBody, writeError)
}

func (s *GeminiMessagesCompatService) renderGoogleProtocolResponse(
	c *gin.Context,
	resp *http.Response,
	pipeline *protocolconv.Pipeline,
	source protocolconv.Protocol,
	googleBody []byte,
	usageOverride *ClaudeUsage,
	startTime time.Time,
	rawUpstreamBody []byte,
	writeError func(status int, errorType, message string) error,
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
		return nil, writeError(http.StatusBadGateway, "upstream_error", "Failed to convert upstream response")
	}
	renderer, err := protocolconv.NewRenderer(source)
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

func (s *GeminiMessagesCompatService) handleGoogleProtocolStreamingResponse(
	c *gin.Context,
	resp *http.Response,
	pipeline *protocolconv.Pipeline,
	source protocolconv.Protocol,
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
	renderer, err := protocolconv.NewRenderer(source)
	if err != nil {
		return nil, err
	}
	var usage ClaudeUsage
	var firstTokenMs *int
	headersWritten := false
	clientDisconnected := false
	writePayloads := func(payloads [][]byte) (bool, error) {
		if clientDisconnected {
			return true, nil
		}
		visible := payloads[:0]
		for _, payload := range payloads {
			if source != protocolconv.ProtocolOpenAIChat || includeUsage || !isOpenAIChatUsageOnlyStreamChunk(string(payload)) {
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
				clientDisconnected = true
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
		_ = disconnected
	}
	finalPayloads, _, err := session.Finalize()
	if err != nil {
		return nil, fmt.Errorf("finalize Google stream: %w", err)
	}
	disconnected, err := writePayloads(finalPayloads)
	if err != nil {
		return nil, err
	}
	_ = disconnected
	if clientDisconnected {
		return &geminiStreamResult{usage: &usage, firstTokenMs: firstTokenMs, clientDisconnected: true}, nil
	}
	if !headersWritten {
		if err := renderer.WriteStreamHeaders(c.Writer, stream.StatusCode, stream.Headers); err != nil {
			return nil, err
		}
	}
	_, _ = c.Writer.Write(renderer.StreamTerminal())
	c.Writer.Flush()
	return &geminiStreamResult{usage: &usage, firstTokenMs: firstTokenMs, clientDisconnected: clientDisconnected}, nil
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
			ProxyID:            opsUpstreamProxyID(account),
			ProxyName:          opsUpstreamProxyName(account),
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
		// 400 是确定性的请求错误：回传上游 message（已脱敏），客户端据此定位非法字段。
		if errMsg == "Upstream request failed" {
			if upstreamMsg != "" {
				errMsg = upstreamMsg
			} else {
				errMsg = "Invalid request"
			}
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

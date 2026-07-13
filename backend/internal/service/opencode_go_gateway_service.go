package service

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/Wei-Shaw/sub2api/internal/util/responseheaders"
	"github.com/Wei-Shaw/sub2api/internal/util/urlvalidator"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
	"go.uber.org/zap"
)

var openCodeGoAllowedHeaders = map[string]bool{
	"accept-language": true,
}

const openCodeGoUpstreamUserAgent = "opencode/1.0.0 (linux; x64) node/24.3.0"

// OpenCodeGoGatewayService forwards requests to the OpenCode Go API.
type OpenCodeGoGatewayService struct {
	httpUpstream         HTTPUpstream
	cfg                  *config.Config
	responseHeaderFilter *responseheaders.CompiledHeaderFilter
	tlsFPProfileService  *TLSFingerprintProfileService
	rateLimitService     *RateLimitService
}

// NewOpenCodeGoGatewayService creates an OpenCode Go gateway service.
func NewOpenCodeGoGatewayService(
	httpUpstream HTTPUpstream,
	cfg *config.Config,
	tlsFPProfileService *TLSFingerprintProfileService,
	rateLimitService *RateLimitService,
) *OpenCodeGoGatewayService {
	return &OpenCodeGoGatewayService{
		httpUpstream:         httpUpstream,
		cfg:                  cfg,
		responseHeaderFilter: compileResponseHeaderFilter(cfg),
		tlsFPProfileService:  tlsFPProfileService,
		rateLimitService:     rateLimitService,
	}
}

// ForwardChatCompletions handles inbound OpenAI Chat Completions requests.
func (s *OpenCodeGoGatewayService) ForwardChatCompletions(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	body []byte,
) (*ForwardResult, error) {
	model, ok := s.validateJSONModel(c, body, openCodeGoErrorFormatChat)
	if !ok {
		return nil, fmt.Errorf("invalid opencode go chat completions request")
	}
	upstreamModel, protocol, ok := s.resolveUpstreamModelProtocol(c, account, model, openCodeGoErrorFormatChat)
	if !ok {
		return nil, fmt.Errorf("opencode go model protocol not configured for %q", model)
	}
	upstreamBody, err := sjson.SetBytes(body, "model", upstreamModel)
	if err != nil {
		writeOpenCodeGoError(c, http.StatusBadRequest, openCodeGoErrorFormatChat, "invalid_request_error", "Failed to rewrite model")
		return nil, err
	}

	switch protocol {
	case OpenCodeGoProtocolChatCompletions:
		if gjson.GetBytes(upstreamBody, "stream").Bool() {
			upstreamBody, err = ensureOpenAIChatStreamUsage(upstreamBody)
			if err != nil {
				return nil, fmt.Errorf("enable chat stream usage: %w", err)
			}
		}
		return s.forwardChatBody(ctx, c, account, upstreamBody, model, upstreamModel, openCodeGoResponseChat, protocol, nil)
	case OpenCodeGoProtocolMessages:
		pipeline, converted, err := newOpenCodeGoPipelineRequest(upstreamBody, protocolconv.ProtocolOpenAIChat, protocolconv.ProtocolAnthropic, account, model, upstreamModel)
		if err != nil {
			writeOpenCodeGoError(c, http.StatusBadRequest, openCodeGoErrorFormatChat, "invalid_request_error", "Failed to convert chat completions request to messages")
			return nil, err
		}
		converted = prepareOpenCodeGoMessagesCacheBody(converted)
		return s.forwardMessagesBody(ctx, c, account, converted, model, upstreamModel, openCodeGoResponseChat, protocol, pipeline)
	default:
		writeOpenCodeGoError(c, http.StatusBadRequest, openCodeGoErrorFormatChat, "invalid_request_error", "Unsupported model protocol")
		return nil, fmt.Errorf("unsupported opencode go model protocol %q", protocol)
	}
}

// ForwardMessages handles inbound Anthropic Messages requests.
func (s *OpenCodeGoGatewayService) ForwardMessages(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	body []byte,
) (*ForwardResult, error) {
	model, ok := s.validateJSONModel(c, body, openCodeGoErrorFormatAnthropic)
	if !ok {
		return nil, fmt.Errorf("invalid opencode go messages request")
	}
	upstreamModel, protocol, ok := s.resolveUpstreamModelProtocol(c, account, model, openCodeGoErrorFormatAnthropic)
	if !ok {
		return nil, fmt.Errorf("opencode go model protocol not configured for %q", model)
	}
	upstreamBody, err := sjson.SetBytes(body, "model", upstreamModel)
	if err != nil {
		writeOpenCodeGoError(c, http.StatusBadRequest, openCodeGoErrorFormatAnthropic, "invalid_request_error", "Failed to rewrite model")
		return nil, err
	}

	switch protocol {
	case OpenCodeGoProtocolMessages:
		upstreamBody = prepareOpenCodeGoMessagesCacheBody(upstreamBody)
		return s.forwardMessagesBody(ctx, c, account, upstreamBody, model, upstreamModel, openCodeGoResponseAnthropic, protocol, nil)
	case OpenCodeGoProtocolChatCompletions:
		pipeline, converted, err := newOpenCodeGoPipelineRequest(upstreamBody, protocolconv.ProtocolAnthropic, protocolconv.ProtocolOpenAIChat, account, model, upstreamModel)
		if err != nil {
			writeOpenCodeGoError(c, http.StatusBadRequest, openCodeGoErrorFormatAnthropic, "invalid_request_error", "Failed to convert messages request to chat completions")
			return nil, err
		}
		if gjson.GetBytes(converted, "stream").Bool() {
			converted, err = ensureOpenAIChatStreamUsage(converted)
			if err != nil {
				return nil, fmt.Errorf("enable chat stream usage: %w", err)
			}
		}
		return s.forwardChatBody(ctx, c, account, converted, model, upstreamModel, openCodeGoResponseAnthropic, protocol, pipeline)
	default:
		writeOpenCodeGoError(c, http.StatusBadRequest, openCodeGoErrorFormatAnthropic, "invalid_request_error", "Unsupported model protocol")
		return nil, fmt.Errorf("unsupported opencode go model protocol %q", protocol)
	}
}

func (s *OpenCodeGoGatewayService) forwardChatBody(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	body []byte,
	originalModel string,
	upstreamModel string,
	responseMode openCodeGoResponseMode,
	protocol string,
	pipeline *protocolconv.Pipeline,
) (*ForwardResult, error) {
	targetURL, err := s.openCodeGoEndpointURL(account, "/v1/chat/completions")
	if err != nil {
		writeOpenCodeGoError(c, http.StatusBadRequest, responseMode.errorFormat(), "invalid_request_error", err.Error())
		return nil, err
	}
	startTime := time.Now()
	resp, err := s.sendUpstream(ctx, c, account, targetURL, body, gjson.GetBytes(body, "stream").Bool(), responseMode.errorFormat(), protocol)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if err := s.handleUpstreamError(ctx, c, account, resp, responseMode.errorFormat()); err != nil {
		return nil, err
	}
	if gjson.GetBytes(body, "stream").Bool() {
		if responseMode == openCodeGoResponseAnthropic {
			return s.streamChatToAnthropic(c, resp, originalModel, upstreamModel, startTime)
		}
		return s.streamChatPassthrough(c, resp, originalModel, upstreamModel, startTime)
	}
	if responseMode == openCodeGoResponseAnthropic {
		return s.bufferChatToAnthropic(c, resp, pipeline, originalModel, upstreamModel, startTime)
	}
	return s.bufferChatPassthrough(c, resp, originalModel, upstreamModel, startTime, protocol)
}

func (s *OpenCodeGoGatewayService) forwardMessagesBody(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	body []byte,
	originalModel string,
	upstreamModel string,
	responseMode openCodeGoResponseMode,
	protocol string,
	pipeline *protocolconv.Pipeline,
) (*ForwardResult, error) {
	targetURL, err := s.openCodeGoEndpointURL(account, "/v1/messages")
	if err != nil {
		writeOpenCodeGoError(c, http.StatusBadRequest, responseMode.errorFormat(), "invalid_request_error", err.Error())
		return nil, err
	}
	startTime := time.Now()
	resp, err := s.sendUpstream(ctx, c, account, targetURL, body, gjson.GetBytes(body, "stream").Bool(), responseMode.errorFormat(), protocol)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if err := s.handleUpstreamError(ctx, c, account, resp, responseMode.errorFormat()); err != nil {
		return nil, err
	}
	if gjson.GetBytes(body, "stream").Bool() {
		if responseMode == openCodeGoResponseChat {
			return s.streamAnthropicToChat(c, resp, originalModel, upstreamModel, startTime)
		}
		return s.streamAnthropicPassthrough(c, resp, originalModel, upstreamModel, startTime)
	}
	if responseMode == openCodeGoResponseChat {
		return s.bufferAnthropicToChat(c, resp, pipeline, originalModel, upstreamModel, startTime)
	}
	return s.bufferAnthropicPassthrough(c, resp, originalModel, upstreamModel, startTime, protocol)
}

func (s *OpenCodeGoGatewayService) sendUpstream(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	targetURL string,
	body []byte,
	stream bool,
	format openCodeGoErrorFormat,
	protocol string,
) (*http.Response, error) {
	apiKey := account.GetOpenCodeGoAPIKey()
	if apiKey == "" {
		writeOpenCodeGoError(c, http.StatusBadGateway, format, "upstream_error", "OpenCode Go account is missing api_key")
		return nil, fmt.Errorf("opencode go account %d missing api_key", account.ID)
	}

	upstreamCtx, releaseUpstreamCtx := detachUpstreamContext(ctx)
	upstreamReq, err := http.NewRequestWithContext(upstreamCtx, http.MethodPost, targetURL, bytes.NewReader(body))
	releaseUpstreamCtx()
	if err != nil {
		writeOpenCodeGoError(c, http.StatusBadGateway, format, "upstream_error", "Failed to build upstream request")
		return nil, err
	}
	upstreamReq.Header.Set("Content-Type", "application/json")
	if protocol == OpenCodeGoProtocolMessages {
		upstreamReq.Header.Set("x-api-key", apiKey)
		upstreamReq.Header.Set("anthropic-version", "2023-06-01")
	} else {
		upstreamReq.Header.Set("Authorization", "Bearer "+apiKey)
	}
	if stream {
		upstreamReq.Header.Set("Accept", "text/event-stream")
	} else {
		upstreamReq.Header.Set("Accept", "application/json")
	}
	upstreamReq.Header.Set("User-Agent", openCodeGoUpstreamUserAgent)
	if c != nil && c.Request != nil {
		for key, values := range c.Request.Header {
			if !openCodeGoAllowedHeaders[strings.ToLower(key)] {
				continue
			}
			for _, value := range values {
				upstreamReq.Header.Add(key, value)
			}
		}
	}

	proxyURL := ""
	if account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}
	var profile *tlsfingerprint.Profile
	if s.tlsFPProfileService != nil {
		profile = s.tlsFPProfileService.ResolveTLSProfile(account)
	}
	resp, err := s.httpUpstream.DoWithTLS(upstreamReq, proxyURL, account.ID, account.Concurrency, profile)
	if err != nil {
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
		return nil, &UpstreamFailoverError{
			StatusCode:   http.StatusBadGateway,
			ResponseBody: []byte(safeErr),
		}
	}
	return resp, nil
}

func (s *OpenCodeGoGatewayService) handleUpstreamError(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	resp *http.Response,
	format openCodeGoErrorFormat,
) error {
	if resp.StatusCode < 400 {
		return nil
	}
	body := readOpenCodeGoErrorBody(resp.Body)
	upstreamMsg := sanitizeUpstreamErrorMessage(strings.TrimSpace(extractUpstreamErrorMessage(body)))
	if upstreamMsg == "" {
		upstreamMsg = http.StatusText(resp.StatusCode)
	}
	detail := ""
	if s.cfg != nil && s.cfg.Gateway.LogUpstreamErrorBody {
		maxBytes := s.cfg.Gateway.LogUpstreamErrorBodyMaxBytes
		if maxBytes <= 0 {
			maxBytes = 2048
		}
		detail = truncateString(string(body), maxBytes)
	}
	setOpsUpstreamError(c, resp.StatusCode, upstreamMsg, detail)

	if shouldFailoverOpenCodeGoResponse(resp.StatusCode, body) {
		s.applyOpenCodeGoFailureSideEffects(ctx, account, resp.StatusCode, resp.Header, body)
		appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
			Platform:           account.Platform,
			AccountID:          account.ID,
			AccountName:        account.Name,
			UpstreamStatusCode: resp.StatusCode,
			UpstreamRequestID:  resp.Header.Get("x-request-id"),
			Kind:               "failover",
			Message:            upstreamMsg,
			Detail:             detail,
		})
		return &UpstreamFailoverError{
			StatusCode:      resp.StatusCode,
			ResponseBody:    body,
			ResponseHeaders: resp.Header.Clone(),
		}
	}

	appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
		Platform:           account.Platform,
		AccountID:          account.ID,
		AccountName:        account.Name,
		UpstreamStatusCode: resp.StatusCode,
		UpstreamRequestID:  resp.Header.Get("x-request-id"),
		Kind:               "passthrough",
		Message:            upstreamMsg,
		Detail:             detail,
	})
	writeOpenCodeGoUpstreamResponse(c, resp, body, s.responseHeaderFilter, format)
	return fmt.Errorf("opencode go upstream returned status %d", resp.StatusCode)
}

func (s *OpenCodeGoGatewayService) applyOpenCodeGoFailureSideEffects(ctx context.Context, account *Account, statusCode int, headers http.Header, body []byte) {
	if s == nil || s.rateLimitService == nil || account == nil {
		return
	}
	s.rateLimitService.HandleUpstreamError(ctx, account, statusCode, headers, body)
}

func (s *OpenCodeGoGatewayService) bufferChatPassthrough(
	c *gin.Context,
	resp *http.Response,
	originalModel string,
	upstreamModel string,
	startTime time.Time,
	protocol string,
) (*ForwardResult, error) {
	body, err := ReadUpstreamResponseBody(resp.Body, s.cfg, c, openAITooLargeError)
	if err != nil {
		if !errors.Is(err, ErrUpstreamResponseBodyTooLarge) {
			writeOpenCodeGoError(c, http.StatusBadGateway, openCodeGoErrorFormatChat, "upstream_error", "Failed to read upstream response")
		}
		return nil, err
	}
	usage := claudeUsageFromChatBody(body)
	writeOpenCodeGoUpstreamResponse(c, resp, body, s.responseHeaderFilter, openCodeGoErrorFormatChat)
	return openCodeGoForwardResult(resp, usage, originalModel, upstreamModel, false, startTime), nil
}

func (s *OpenCodeGoGatewayService) bufferAnthropicPassthrough(
	c *gin.Context,
	resp *http.Response,
	originalModel string,
	upstreamModel string,
	startTime time.Time,
	protocol string,
) (*ForwardResult, error) {
	body, err := ReadUpstreamResponseBody(resp.Body, s.cfg, c, anthropicTooLargeError)
	if err != nil {
		if !errors.Is(err, ErrUpstreamResponseBodyTooLarge) {
			writeOpenCodeGoError(c, http.StatusBadGateway, openCodeGoErrorFormatAnthropic, "upstream_error", "Failed to read upstream response")
		}
		return nil, err
	}
	usage := claudeUsageFromAnthropicBody(body)
	writeOpenCodeGoUpstreamResponse(c, resp, body, s.responseHeaderFilter, openCodeGoErrorFormatAnthropic)
	return openCodeGoForwardResult(resp, usage, originalModel, upstreamModel, false, startTime), nil
}

func (s *OpenCodeGoGatewayService) bufferAnthropicToChat(
	c *gin.Context,
	resp *http.Response,
	pipeline *protocolconv.Pipeline,
	originalModel string,
	upstreamModel string,
	startTime time.Time,
) (*ForwardResult, error) {
	body, err := ReadUpstreamResponseBody(resp.Body, s.cfg, c, openAITooLargeError)
	if err != nil {
		return nil, err
	}
	var anth apicompat.AnthropicResponse
	if err := json.Unmarshal(body, &anth); err != nil {
		writeOpenCodeGoError(c, http.StatusBadGateway, openCodeGoErrorFormatChat, "upstream_error", "Failed to parse upstream messages response")
		return nil, err
	}
	usage := claudeUsageFromAnthropicUsage(anth.Usage)
	converted, err := pipeline.ConvertResponse(body, protocolconv.ProtocolAnthropic)
	if err != nil {
		writeOpenCodeGoError(c, http.StatusBadGateway, openCodeGoErrorFormatChat, "upstream_error", "Failed to convert upstream messages response")
		return nil, err
	}
	writeOpenCodeGoUpstreamResponse(c, resp, converted.Body, s.responseHeaderFilter, openCodeGoErrorFormatChat)
	return openCodeGoForwardResult(resp, usage, originalModel, upstreamModel, false, startTime), nil
}

func (s *OpenCodeGoGatewayService) bufferChatToAnthropic(
	c *gin.Context,
	resp *http.Response,
	pipeline *protocolconv.Pipeline,
	originalModel string,
	upstreamModel string,
	startTime time.Time,
) (*ForwardResult, error) {
	body, err := ReadUpstreamResponseBody(resp.Body, s.cfg, c, anthropicTooLargeError)
	if err != nil {
		return nil, err
	}
	var cc apicompat.ChatCompletionsResponse
	if err := json.Unmarshal(body, &cc); err != nil {
		writeOpenCodeGoError(c, http.StatusBadGateway, openCodeGoErrorFormatAnthropic, "upstream_error", "Failed to parse upstream chat completions response")
		return nil, err
	}
	usage := claudeUsageFromChatUsage(cc.Usage)
	converted, err := pipeline.ConvertResponse(body, protocolconv.ProtocolOpenAIChat)
	if err != nil {
		writeOpenCodeGoError(c, http.StatusBadGateway, openCodeGoErrorFormatAnthropic, "upstream_error", "Failed to convert upstream chat completions response")
		return nil, err
	}
	writeOpenCodeGoUpstreamResponse(c, resp, converted.Body, s.responseHeaderFilter, openCodeGoErrorFormatAnthropic)
	return openCodeGoForwardResult(resp, usage, originalModel, upstreamModel, false, startTime), nil
}

func (s *OpenCodeGoGatewayService) streamChatPassthrough(
	c *gin.Context,
	resp *http.Response,
	originalModel string,
	upstreamModel string,
	startTime time.Time,
) (*ForwardResult, error) {
	usage := ClaudeUsage{}
	firstTokenMs := (*int)(nil)
	result := s.copySSE(c, resp, openCodeGoErrorFormatChat, func(payload string) {
		if strings.TrimSpace(payload) == "[DONE]" {
			return
		}
		usageOnlyChunk := isOpenAIChatUsageOnlyStreamChunk(payload)
		if u := extractCCStreamUsage(payload); u != nil {
			usage = normalizeOpenCodeGoChatUsage(ClaudeUsage{
				InputTokens:              u.InputTokens,
				OutputTokens:             u.OutputTokens,
				CacheReadInputTokens:     u.CacheReadInputTokens,
				CacheCreationInputTokens: u.CacheCreationInputTokens,
				ImageOutputTokens:        u.ImageOutputTokens,
			})
		}
		if firstTokenMs == nil && !usageOnlyChunk {
			ms := int(time.Since(startTime).Milliseconds())
			firstTokenMs = &ms
		}
	})
	out := openCodeGoForwardResult(resp, usage, originalModel, upstreamModel, true, startTime)
	out.ClientDisconnect = result.clientDisconnected
	out.FirstTokenMs = firstTokenMs
	return out, result.err
}

func (s *OpenCodeGoGatewayService) streamAnthropicPassthrough(
	c *gin.Context,
	resp *http.Response,
	originalModel string,
	upstreamModel string,
	startTime time.Time,
) (*ForwardResult, error) {
	usage := ClaudeUsage{}
	firstTokenMs := (*int)(nil)
	result := s.copySSE(c, resp, openCodeGoErrorFormatAnthropic, func(payload string) {
		var event apicompat.AnthropicStreamEvent
		if err := json.Unmarshal([]byte(payload), &event); err != nil {
			return
		}
		if firstTokenMs == nil && event.Type != "" {
			ms := int(time.Since(startTime).Milliseconds())
			firstTokenMs = &ms
		}
		if event.Type == "message_start" && event.Message != nil {
			mergeAnthropicUsage(&usage, event.Message.Usage)
		}
		if event.Type == "message_delta" && event.Usage != nil {
			mergeAnthropicUsage(&usage, *event.Usage)
		}
	})
	out := openCodeGoForwardResult(resp, usage, originalModel, upstreamModel, true, startTime)
	out.ClientDisconnect = result.clientDisconnected
	out.FirstTokenMs = firstTokenMs
	return out, result.err
}

func (s *OpenCodeGoGatewayService) streamAnthropicToChat(
	c *gin.Context,
	resp *http.Response,
	originalModel string,
	upstreamModel string,
	startTime time.Time,
) (*ForwardResult, error) {
	requestID := resp.Header.Get("x-request-id")
	s.writeStreamHeaders(c, resp, openCodeGoErrorFormatChat)
	session, err := newStandardStreamSession(protocolconv.ProtocolAnthropic, protocolconv.ProtocolOpenAIChat)
	if err != nil {
		return nil, err
	}
	usage := ClaudeUsage{}
	var firstTokenMs *int
	clientDisconnected := false
	var conversionErr error

	writePayloads := func(payloads [][]byte) {
		for _, payload := range payloads {
			if _, err := fmt.Fprintf(c.Writer, "data: %s\n\n", payload); err != nil {
				clientDisconnected = true
				return
			}
		}
		if len(payloads) > 0 {
			c.Writer.Flush()
		}
	}
	process := func(payload string) {
		if conversionErr != nil {
			return
		}
		var event apicompat.AnthropicStreamEvent
		if err := json.Unmarshal([]byte(payload), &event); err == nil {
			if firstTokenMs == nil {
				ms := int(time.Since(startTime).Milliseconds())
				firstTokenMs = &ms
			}
			if event.Type == "message_start" && event.Message != nil {
				mergeAnthropicUsage(&usage, event.Message.Usage)
			}
			if event.Type == "message_delta" && event.Usage != nil {
				mergeAnthropicUsage(&usage, *event.Usage)
			}
		}
		payloads, _, err := session.Convert([]byte(payload))
		if err != nil {
			conversionErr = err
			return
		}
		if !clientDisconnected {
			writePayloads(payloads)
		}
	}
	s.scanSSE(resp, process)
	if conversionErr == nil {
		payloads, _, err := session.Finalize()
		conversionErr = err
		if !clientDisconnected {
			writePayloads(payloads)
			_, _ = fmt.Fprint(c.Writer, "data: [DONE]\n\n")
			c.Writer.Flush()
		}
	}
	return &ForwardResult{RequestID: requestID, Usage: usage, Model: originalModel, UpstreamModel: optionalUpstreamModel(originalModel, upstreamModel), Stream: true, Duration: time.Since(startTime), FirstTokenMs: firstTokenMs, ClientDisconnect: clientDisconnected}, conversionErr
}

func (s *OpenCodeGoGatewayService) streamChatToAnthropic(
	c *gin.Context,
	resp *http.Response,
	originalModel string,
	upstreamModel string,
	startTime time.Time,
) (*ForwardResult, error) {
	requestID := resp.Header.Get("x-request-id")
	s.writeStreamHeaders(c, resp, openCodeGoErrorFormatAnthropic)
	session, err := newStandardStreamSession(protocolconv.ProtocolOpenAIChat, protocolconv.ProtocolAnthropic)
	if err != nil {
		return nil, err
	}
	usage := ClaudeUsage{}
	var firstTokenMs *int
	clientDisconnected := false
	var conversionErr error

	writePayloads := func(payloads [][]byte) {
		for _, payload := range payloads {
			var event struct {
				Type string `json:"type"`
			}
			if json.Unmarshal(payload, &event) != nil || event.Type == "" {
				continue
			}
			if _, err := fmt.Fprintf(c.Writer, "event: %s\ndata: %s\n\n", event.Type, payload); err != nil {
				clientDisconnected = true
				return
			}
		}
		if len(payloads) > 0 {
			c.Writer.Flush()
		}
	}
	process := func(payload string) {
		if conversionErr != nil || strings.TrimSpace(payload) == "[DONE]" {
			return
		}
		if u := extractCCStreamUsage(payload); u != nil {
			usage = normalizeOpenCodeGoChatUsage(ClaudeUsage{InputTokens: u.InputTokens, OutputTokens: u.OutputTokens, CacheReadInputTokens: u.CacheReadInputTokens})
		}
		if firstTokenMs == nil && !isOpenAIChatUsageOnlyStreamChunk(payload) {
			ms := int(time.Since(startTime).Milliseconds())
			firstTokenMs = &ms
		}
		payloads, _, err := session.Convert([]byte(payload))
		if err != nil {
			conversionErr = err
			return
		}
		if !clientDisconnected {
			writePayloads(payloads)
		}
	}
	s.scanSSE(resp, process)
	if conversionErr == nil {
		payloads, _, err := session.Finalize()
		conversionErr = err
		if !clientDisconnected {
			writePayloads(payloads)
		}
	}
	return &ForwardResult{RequestID: requestID, Usage: usage, Model: originalModel, UpstreamModel: optionalUpstreamModel(originalModel, upstreamModel), Stream: true, Duration: time.Since(startTime), FirstTokenMs: firstTokenMs, ClientDisconnect: clientDisconnected}, conversionErr
}

type openCodeGoCopySSEResult struct {
	clientDisconnected bool
	err                error
}

func (s *OpenCodeGoGatewayService) copySSE(c *gin.Context, resp *http.Response, format openCodeGoErrorFormat, observe func(payload string)) openCodeGoCopySSEResult {
	s.writeStreamHeaders(c, resp, format)
	scanner := s.newSSEScanner(resp.Body)
	clientDisconnected := false
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data:") && observe != nil {
			observe(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
		if !clientDisconnected {
			if _, err := c.Writer.WriteString(line + "\n"); err != nil {
				clientDisconnected = true
			}
			if line == "" {
				c.Writer.Flush()
			}
		}
	}
	if !clientDisconnected {
		c.Writer.Flush()
	}
	err := scanner.Err()
	if err != nil && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) {
		err = nil
	}
	return openCodeGoCopySSEResult{clientDisconnected: clientDisconnected, err: err}
}

func (s *OpenCodeGoGatewayService) scanSSE(resp *http.Response, process func(payload string)) {
	scanner := s.newSSEScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data:") && process != nil {
			process(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	if err := scanner.Err(); err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		logger.L().Warn("opencode go stream: read error", zap.Error(err), zap.String("request_id", resp.Header.Get("x-request-id")))
	}
}

func (s *OpenCodeGoGatewayService) newSSEScanner(r io.Reader) *bufio.Scanner {
	scanner := bufio.NewScanner(r)
	maxLineSize := defaultMaxLineSize
	if s.cfg != nil && s.cfg.Gateway.MaxLineSize > 0 {
		maxLineSize = s.cfg.Gateway.MaxLineSize
	}
	scanner.Buffer(make([]byte, 0, 64*1024), maxLineSize)
	return scanner
}

func (s *OpenCodeGoGatewayService) writeStreamHeaders(c *gin.Context, resp *http.Response, format openCodeGoErrorFormat) {
	if s.responseHeaderFilter != nil {
		responseheaders.WriteFilteredHeaders(c.Writer.Header(), resp.Header, s.responseHeaderFilter)
	}
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
	c.Writer.WriteHeader(http.StatusOK)
}

func (s *OpenCodeGoGatewayService) openCodeGoEndpointURL(account *Account, endpoint string) (string, error) {
	baseURL := account.GetOpenCodeGoBaseURL()
	if baseURL == "" {
		baseURL = DefaultOpenCodeGoBaseURL
	}
	validated, err := s.validateUpstreamBaseURL(baseURL)
	if err != nil {
		return "", err
	}
	return buildOpenAIEndpointURL(validated, endpoint), nil
}

func (s *OpenCodeGoGatewayService) validateJSONModel(c *gin.Context, body []byte, format openCodeGoErrorFormat) (string, bool) {
	if len(body) == 0 {
		writeOpenCodeGoError(c, http.StatusBadRequest, format, "invalid_request_error", "Request body is empty")
		return "", false
	}
	if !gjson.ValidBytes(body) {
		writeOpenCodeGoError(c, http.StatusBadRequest, format, "invalid_request_error", "Failed to parse request body")
		return "", false
	}
	model := strings.TrimSpace(gjson.GetBytes(body, "model").String())
	if model == "" {
		writeOpenCodeGoError(c, http.StatusBadRequest, format, "invalid_request_error", "model is required")
		return "", false
	}
	return model, true
}

func (s *OpenCodeGoGatewayService) resolveUpstreamModelProtocol(c *gin.Context, account *Account, model string, format openCodeGoErrorFormat) (string, string, bool) {
	if account == nil || !account.IsOpenCodeGoAPIKey() {
		writeOpenCodeGoError(c, http.StatusBadGateway, format, "upstream_error", "OpenCode Go account must use API key credentials")
		return "", "", false
	}
	upstreamModel := strings.TrimSpace(account.GetMappedModel(model))
	if upstreamModel == "" {
		upstreamModel = model
	}
	protocol, ok := account.ResolveOpenCodeGoModelProtocol(upstreamModel)
	if !ok {
		writeOpenCodeGoError(c, http.StatusBadRequest, format, "invalid_request_error", "OpenCode Go model protocol is not configured for upstream model: "+upstreamModel)
		return "", "", false
	}
	return upstreamModel, protocol, true
}

func (s *OpenCodeGoGatewayService) validateUpstreamBaseURL(raw string) (string, error) {
	if s.cfg != nil && !s.cfg.Security.URLAllowlist.Enabled {
		normalized, err := urlvalidator.ValidateURLFormat(raw, s.cfg.Security.URLAllowlist.AllowInsecureHTTP)
		if err != nil {
			return "", fmt.Errorf("invalid base_url: %w", err)
		}
		return normalized, nil
	}
	if s.cfg == nil {
		normalized, err := urlvalidator.ValidateURLFormat(raw, false)
		if err != nil {
			return "", fmt.Errorf("invalid base_url: %w", err)
		}
		return normalized, nil
	}
	normalized, err := urlvalidator.ValidateHTTPSURL(raw, urlvalidator.ValidationOptions{
		AllowedHosts:     s.cfg.Security.URLAllowlist.UpstreamHosts,
		RequireAllowlist: true,
		AllowPrivate:     s.cfg.Security.URLAllowlist.AllowPrivateHosts,
	})
	if err != nil {
		return "", fmt.Errorf("invalid base_url: %w", err)
	}
	return normalized, nil
}

func prepareOpenCodeGoMessagesCacheBody(body []byte) []byte {
	body = ensureOpenCodeGoSystemCacheAnchor(body)
	body = stripMessageCacheControl(body)
	body = addMessageCacheBreakpoints(body)
	body = applyToolsLastCacheBreakpoint(body)
	body = forceEphemeralCacheControlTTL(body, cacheTTLTarget1h)
	body = enforceCacheControlLimit(body)
	return forceEphemeralCacheControlTTL(body, cacheTTLTarget1h)
}

func ensureOpenCodeGoSystemCacheAnchor(body []byte) []byte {
	system := gjson.GetBytes(body, "system")
	if !system.Exists() {
		return body
	}
	if system.Type == gjson.String {
		raw := fmt.Sprintf(
			`[{"type":"text","text":%s,"cache_control":{"type":"ephemeral","ttl":%q}}]`,
			mustJSONString(system.String()),
			cacheTTLTarget1h,
		)
		if next, err := sjson.SetRawBytes(body, "system", []byte(raw)); err == nil {
			body = next
		}
		return body
	}
	if !system.IsArray() {
		return body
	}

	arr := system.Array()
	for i := len(arr) - 1; i >= 0; i-- {
		block := arr[i]
		if block.Get("type").String() != "text" || strings.TrimSpace(block.Get("text").String()) == "" {
			continue
		}
		pathPrefix := fmt.Sprintf("system.%d.cache_control", i)
		if next, err := sjson.SetBytes(body, pathPrefix+".type", "ephemeral"); err == nil {
			body = next
		}
		if next, err := sjson.SetBytes(body, pathPrefix+".ttl", cacheTTLTarget1h); err == nil {
			body = next
		}
		break
	}
	return body
}

func newOpenCodeGoPipelineRequest(
	body []byte,
	source protocolconv.Protocol,
	target protocolconv.Protocol,
	account *Account,
	clientModel string,
	upstreamModel string,
) (*protocolconv.Pipeline, []byte, error) {
	route := protocolconv.Route{
		Source:         source,
		IntendedTarget: target,
		ClientModel:    clientModel,
		UpstreamModel:  upstreamModel,
	}
	if account != nil {
		route.Provider = account.Platform
		route.AccountID = account.ID
	}
	pipeline, err := protocolconv.NewPipeline(standardProtocolRegistry, protocolconv.PipelineConfig{
		Route:   route,
		Options: protocolconv.Options{SourceModel: upstreamModel, LossPolicy: protocolconv.LossError},
	})
	if err != nil {
		return nil, nil, err
	}
	converted, err := pipeline.ConvertRequest(body)
	if err != nil {
		return nil, nil, err
	}
	return pipeline, converted.Body, nil
}

func convertChatCompletionsBodyToAnthropicBody(body []byte) ([]byte, error) {
	model := gjson.GetBytes(body, "model").String()
	_, converted, err := newOpenCodeGoPipelineRequest(body, protocolconv.ProtocolOpenAIChat, protocolconv.ProtocolAnthropic, nil, model, model)
	return converted, err
}

func convertAnthropicBodyToChatCompletionsBody(body []byte) ([]byte, error) {
	model := gjson.GetBytes(body, "model").String()
	_, converted, err := newOpenCodeGoPipelineRequest(body, protocolconv.ProtocolAnthropic, protocolconv.ProtocolOpenAIChat, nil, model, model)
	return converted, err
}

func claudeUsageFromChatBody(body []byte) ClaudeUsage {
	return normalizeOpenCodeGoChatUsage(ClaudeUsage{
		InputTokens:          int(gjson.GetBytes(body, "usage.prompt_tokens").Int()),
		OutputTokens:         int(gjson.GetBytes(body, "usage.completion_tokens").Int()),
		CacheReadInputTokens: int(gjson.GetBytes(body, "usage.prompt_tokens_details.cached_tokens").Int()),
		ImageOutputTokens:    int(gjson.GetBytes(body, "usage.completion_tokens_details.image_tokens").Int()),
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

func claudeUsageFromChatUsage(usage *apicompat.ChatUsage) ClaudeUsage {
	if usage == nil {
		return ClaudeUsage{}
	}
	out := ClaudeUsage{
		InputTokens:  usage.PromptTokens,
		OutputTokens: usage.CompletionTokens,
	}
	if usage.PromptTokensDetails != nil {
		out.CacheReadInputTokens = usage.PromptTokensDetails.CachedTokens
	}
	return normalizeOpenCodeGoChatUsage(out)
}

func normalizeOpenCodeGoChatUsage(usage ClaudeUsage) ClaudeUsage {
	if usage.CacheReadInputTokens <= 0 {
		return usage
	}
	usage.InputTokens -= usage.CacheReadInputTokens
	if usage.InputTokens < 0 {
		usage.InputTokens = 0
	}
	return usage
}

func claudeUsageFromAnthropicUsage(usage apicompat.AnthropicUsage) ClaudeUsage {
	return ClaudeUsage{
		InputTokens:              usage.InputTokens,
		OutputTokens:             usage.OutputTokens,
		CacheCreationInputTokens: usage.CacheCreationInputTokens,
		CacheReadInputTokens:     usage.CacheReadInputTokens,
	}
}

func openCodeGoForwardResult(resp *http.Response, usage ClaudeUsage, model string, upstreamModel string, stream bool, startTime time.Time) *ForwardResult {
	return &ForwardResult{
		RequestID:     resp.Header.Get("x-request-id"),
		Usage:         usage,
		Model:         model,
		UpstreamModel: optionalUpstreamModel(model, upstreamModel),
		Stream:        stream,
		Duration:      time.Since(startTime),
	}
}

func optionalUpstreamModel(model string, upstreamModel string) string {
	if strings.TrimSpace(upstreamModel) == "" || upstreamModel == model {
		return ""
	}
	return upstreamModel
}

func writeOpenCodeGoUpstreamResponse(c *gin.Context, resp *http.Response, body []byte, filter *responseheaders.CompiledHeaderFilter, format openCodeGoErrorFormat) {
	if filter != nil {
		responseheaders.WriteFilteredHeaders(c.Writer.Header(), resp.Header, filter)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		c.Writer.Header().Set("Content-Type", ct)
	} else if format == openCodeGoErrorFormatAnthropic {
		c.Writer.Header().Set("Content-Type", "application/json")
	} else {
		c.Writer.Header().Set("Content-Type", "application/json")
	}
	c.Writer.WriteHeader(resp.StatusCode)
	_, _ = c.Writer.Write(body)
}

func readOpenCodeGoErrorBody(body io.Reader) []byte {
	if body == nil {
		return nil
	}
	data, _ := io.ReadAll(io.LimitReader(body, openAIUpstreamErrorBodyReadLimit+1))
	if int64(len(data)) > openAIUpstreamErrorBodyReadLimit {
		return data[:openAIUpstreamErrorBodyReadLimit]
	}
	return data
}

func shouldFailoverOpenCodeGoResponse(status int, body []byte) bool {
	if status == http.StatusUnauthorized || status == http.StatusForbidden || status == http.StatusTooManyRequests {
		return true
	}
	if status >= http.StatusInternalServerError {
		return true
	}
	if status != http.StatusBadRequest {
		return false
	}

	// Console Go sometimes wraps a transient provider failure in HTTP 400. It is
	// not a client validation error, so let the handler try another account.
	errType := strings.ToLower(strings.TrimSpace(gjson.GetBytes(body, "error.type").String()))
	message := strings.ToLower(strings.TrimSpace(gjson.GetBytes(body, "error.message").String()))
	return errType == "invalid_request_error" &&
		strings.Contains(message, "error from provider") &&
		strings.Contains(message, "upstream request failed")
}

type openCodeGoErrorFormat string

const (
	openCodeGoErrorFormatChat      openCodeGoErrorFormat = "chat"
	openCodeGoErrorFormatAnthropic openCodeGoErrorFormat = "anthropic"
)

type openCodeGoResponseMode string

const (
	openCodeGoResponseChat      openCodeGoResponseMode = "chat"
	openCodeGoResponseAnthropic openCodeGoResponseMode = "anthropic"
)

func (m openCodeGoResponseMode) errorFormat() openCodeGoErrorFormat {
	if m == openCodeGoResponseAnthropic {
		return openCodeGoErrorFormatAnthropic
	}
	return openCodeGoErrorFormatChat
}

func writeOpenCodeGoError(c *gin.Context, status int, format openCodeGoErrorFormat, errType string, message string) {
	if c == nil || c.Writer == nil || c.Writer.Written() {
		return
	}
	if format == openCodeGoErrorFormatAnthropic {
		c.JSON(status, gin.H{
			"type": "error",
			"error": gin.H{
				"type":    errType,
				"message": message,
			},
		})
		return
	}
	c.JSON(status, gin.H{
		"error": gin.H{
			"type":    errType,
			"message": message,
		},
	})
}

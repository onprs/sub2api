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

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv"
	protocoltransport "github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv/transport"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/Wei-Shaw/sub2api/internal/util/responseheaders"
	"github.com/Wei-Shaw/sub2api/internal/util/urlvalidator"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
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
		pipeline, converted, err := newOpenCodeGoPipelineRequest(upstreamBody, protocolconv.ProtocolOpenAIChat, protocolconv.ProtocolOpenAIChat, account, model, upstreamModel)
		if err != nil {
			writeOpenCodeGoError(c, http.StatusBadRequest, openCodeGoErrorFormatChat, "invalid_request_error", "Failed to validate chat completions request")
			return nil, err
		}
		upstreamBody = converted
		if gjson.GetBytes(upstreamBody, "stream").Bool() {
			upstreamBody, err = ensureOpenAIChatStreamUsage(upstreamBody)
			if err != nil {
				return nil, fmt.Errorf("enable chat stream usage: %w", err)
			}
		}
		return s.forwardChatBody(ctx, c, account, upstreamBody, model, upstreamModel, openCodeGoResponseChat, protocol, pipeline)
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

// ForwardResponses handles inbound OpenAI Responses requests.
func (s *OpenCodeGoGatewayService) ForwardResponses(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	body []byte,
	clientModel string,
) (*ForwardResult, error) {
	return s.forwardStandardRequest(ctx, c, account, body, clientModel, "", false, protocolconv.ProtocolOpenAIResponses, openCodeGoResponseResponses)
}

// ForwardGoogleGenAI handles inbound Google generateContent requests. The model
// and streaming mode are carried by the URL rather than the Google JSON body.
func (s *OpenCodeGoGatewayService) ForwardGoogleGenAI(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	body []byte,
	clientModel string,
	routingModel string,
	stream bool,
) (*ForwardResult, error) {
	return s.forwardStandardRequest(ctx, c, account, body, clientModel, routingModel, stream, protocolconv.ProtocolGoogleGenAI, openCodeGoResponseGoogle)
}

func (s *OpenCodeGoGatewayService) forwardStandardRequest(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	body []byte,
	clientModel string,
	routingModel string,
	stream bool,
	source protocolconv.Protocol,
	responseMode openCodeGoResponseMode,
) (*ForwardResult, error) {
	format := responseMode.errorFormat()
	if len(body) == 0 {
		writeOpenCodeGoError(c, http.StatusBadRequest, format, "invalid_request_error", "Request body is empty")
		return nil, fmt.Errorf("invalid opencode go %s request", source)
	}
	if !gjson.ValidBytes(body) {
		writeOpenCodeGoError(c, http.StatusBadRequest, format, "invalid_request_error", "Failed to parse request body")
		return nil, fmt.Errorf("invalid opencode go %s request", source)
	}
	if source != protocolconv.ProtocolGoogleGenAI {
		routingModel = strings.TrimSpace(gjson.GetBytes(body, "model").String())
		stream = gjson.GetBytes(body, "stream").Bool()
	}
	if routingModel == "" {
		writeOpenCodeGoError(c, http.StatusBadRequest, format, "invalid_request_error", "model is required")
		return nil, fmt.Errorf("invalid opencode go %s request", source)
	}
	if clientModel == "" {
		clientModel = routingModel
	}
	upstreamModel, protocol, ok := s.resolveUpstreamModelProtocol(c, account, routingModel, format)
	if !ok {
		return nil, fmt.Errorf("opencode go model protocol not configured for %q", routingModel)
	}

	sourceBody := body
	if source != protocolconv.ProtocolGoogleGenAI {
		var err error
		sourceBody, err = sjson.SetBytes(sourceBody, "model", upstreamModel)
		if err != nil {
			writeOpenCodeGoError(c, http.StatusBadRequest, format, "invalid_request_error", "Failed to rewrite model")
			return nil, err
		}
	}

	var target protocolconv.Protocol
	switch protocol {
	case OpenCodeGoProtocolChatCompletions:
		target = protocolconv.ProtocolOpenAIChat
	case OpenCodeGoProtocolMessages:
		target = protocolconv.ProtocolAnthropic
	default:
		writeOpenCodeGoError(c, http.StatusBadRequest, format, "invalid_request_error", "Unsupported model protocol")
		return nil, fmt.Errorf("unsupported opencode go model protocol %q", protocol)
	}

	pipeline, converted, err := newOpenCodeGoPipelineRequest(sourceBody, source, target, account, clientModel, upstreamModel)
	if err != nil {
		writeOpenCodeGoError(c, http.StatusBadRequest, format, "invalid_request_error", "Request cannot be represented for the selected OpenCode Go model protocol")
		return nil, err
	}
	converted, err = sjson.SetBytes(converted, "stream", stream)
	if err != nil {
		return nil, fmt.Errorf("set OpenCode Go upstream stream mode: %w", err)
	}

	if protocol == OpenCodeGoProtocolMessages {
		converted = prepareOpenCodeGoMessagesCacheBody(converted)
		return s.forwardMessagesBody(ctx, c, account, converted, clientModel, upstreamModel, responseMode, protocol, pipeline)
	}
	if stream {
		converted, err = ensureOpenAIChatStreamUsage(converted)
		if err != nil {
			return nil, fmt.Errorf("enable chat stream usage: %w", err)
		}
	}
	return s.forwardChatBody(ctx, c, account, converted, clientModel, upstreamModel, responseMode, protocol, pipeline)
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
		pipeline, converted, err := newOpenCodeGoPipelineRequest(upstreamBody, protocolconv.ProtocolAnthropic, protocolconv.ProtocolAnthropic, account, model, upstreamModel)
		if err != nil {
			writeOpenCodeGoError(c, http.StatusBadRequest, openCodeGoErrorFormatAnthropic, "invalid_request_error", "Failed to validate messages request")
			return nil, err
		}
		upstreamBody = prepareOpenCodeGoMessagesCacheBody(converted)
		return s.forwardMessagesBody(ctx, c, account, upstreamBody, model, upstreamModel, openCodeGoResponseAnthropic, protocol, pipeline)
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

	if err := s.handleUpstreamError(ctx, c, account, resp, protocolconv.ProtocolOpenAIChat, responseMode.errorFormat(), gjson.GetBytes(body, "stream").Bool()); err != nil {
		return nil, err
	}
	if gjson.GetBytes(body, "stream").Bool() {
		switch responseMode {
		case openCodeGoResponseAnthropic:
			return s.streamChatToAnthropic(c, resp, pipeline, originalModel, upstreamModel, startTime)
		case openCodeGoResponseChat:
			return s.streamChatPassthrough(c, resp, pipeline, originalModel, upstreamModel, startTime)
		default:
			return s.streamStandardResponse(c, resp, pipeline, protocolconv.ProtocolOpenAIChat, responseMode.protocol(), originalModel, upstreamModel, startTime)
		}
	}
	switch responseMode {
	case openCodeGoResponseAnthropic:
		return s.bufferChatToAnthropic(c, resp, pipeline, originalModel, upstreamModel, startTime)
	case openCodeGoResponseChat:
		return s.bufferChatPassthrough(c, resp, pipeline, originalModel, upstreamModel, startTime)
	default:
		return s.bufferStandardResponse(c, resp, pipeline, protocolconv.ProtocolOpenAIChat, responseMode.protocol(), originalModel, upstreamModel, startTime)
	}
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

	if err := s.handleUpstreamError(ctx, c, account, resp, protocolconv.ProtocolAnthropic, responseMode.errorFormat(), gjson.GetBytes(body, "stream").Bool()); err != nil {
		return nil, err
	}
	if gjson.GetBytes(body, "stream").Bool() {
		switch responseMode {
		case openCodeGoResponseChat:
			return s.streamAnthropicToChat(c, resp, pipeline, originalModel, upstreamModel, startTime)
		case openCodeGoResponseAnthropic:
			return s.streamAnthropicPassthrough(c, resp, pipeline, originalModel, upstreamModel, startTime)
		default:
			return s.streamStandardResponse(c, resp, pipeline, protocolconv.ProtocolAnthropic, responseMode.protocol(), originalModel, upstreamModel, startTime)
		}
	}
	switch responseMode {
	case openCodeGoResponseChat:
		return s.bufferAnthropicToChat(c, resp, pipeline, originalModel, upstreamModel, startTime)
	case openCodeGoResponseAnthropic:
		return s.bufferAnthropicPassthrough(c, resp, pipeline, originalModel, upstreamModel, startTime)
	default:
		return s.bufferStandardResponse(c, resp, pipeline, protocolconv.ProtocolAnthropic, responseMode.protocol(), originalModel, upstreamModel, startTime)
	}
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
	actualProtocol protocolconv.Protocol,
	format openCodeGoErrorFormat,
	streamRequested bool,
) error {
	if resp.StatusCode < 400 {
		return nil
	}
	upstream, err := collectOpenCodeGoErrorResponse(resp, actualProtocol, streamRequested)
	if err != nil {
		return err
	}
	body := upstream.Body
	upstreamMsg := sanitizeUpstreamErrorMessage(strings.TrimSpace(extractUpstreamErrorMessage(body)))
	if upstreamMsg == "" {
		upstreamMsg = http.StatusText(upstream.StatusCode)
	}
	detail := ""
	if s.cfg != nil && s.cfg.Gateway.LogUpstreamErrorBody {
		maxBytes := s.cfg.Gateway.LogUpstreamErrorBodyMaxBytes
		if maxBytes <= 0 {
			maxBytes = 2048
		}
		detail = truncateString(string(body), maxBytes)
	}
	setOpsUpstreamError(c, upstream.StatusCode, upstreamMsg, detail)

	if shouldFailoverOpenCodeGoResponse(upstream.StatusCode, body) {
		s.applyOpenCodeGoFailureSideEffects(ctx, account, upstream.StatusCode, upstream.Headers, body)
		appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
			Platform:           account.Platform,
			AccountID:          account.ID,
			AccountName:        account.Name,
			UpstreamStatusCode: upstream.StatusCode,
			UpstreamRequestID:  upstream.RequestID,
			Kind:               "failover",
			Message:            upstreamMsg,
			Detail:             detail,
		})
		return &UpstreamFailoverError{
			StatusCode:      upstream.StatusCode,
			ResponseBody:    body,
			ResponseHeaders: protocoltransport.CloneHeaders(upstream.Headers),
		}
	}

	appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
		Platform:           account.Platform,
		AccountID:          account.ID,
		AccountName:        account.Name,
		UpstreamStatusCode: upstream.StatusCode,
		UpstreamRequestID:  upstream.RequestID,
		Kind:               "passthrough",
		Message:            upstreamMsg,
		Detail:             detail,
	})
	writeOpenCodeGoUpstreamResponse(c, upstream, s.responseHeaderFilter, format)
	return fmt.Errorf("opencode go upstream returned status %d", upstream.StatusCode)
}

func (s *OpenCodeGoGatewayService) applyOpenCodeGoFailureSideEffects(ctx context.Context, account *Account, statusCode int, headers http.Header, body []byte) {
	if s == nil || s.rateLimitService == nil || account == nil {
		return
	}
	s.rateLimitService.HandleUpstreamError(ctx, account, statusCode, headers, body)
}

func (s *OpenCodeGoGatewayService) bufferStandardResponse(
	c *gin.Context,
	resp *http.Response,
	pipeline *protocolconv.Pipeline,
	actualProtocol protocolconv.Protocol,
	sourceProtocol protocolconv.Protocol,
	originalModel string,
	upstreamModel string,
	startTime time.Time,
) (*ForwardResult, error) {
	body, err := ReadUpstreamResponseBody(resp.Body, s.cfg, c, func(c *gin.Context) {
		writeOpenCodeGoError(c, http.StatusBadGateway, openCodeGoErrorFormatForProtocol(sourceProtocol), "upstream_error", "Upstream response too large")
	})
	if err != nil {
		if !errors.Is(err, ErrUpstreamResponseBodyTooLarge) {
			writeOpenCodeGoError(c, http.StatusBadGateway, openCodeGoErrorFormatForProtocol(sourceProtocol), "upstream_error", "Failed to read upstream response")
		}
		return nil, err
	}
	usage := openCodeGoUsageFromBody(body, actualProtocol)
	if err := s.renderOpenCodeGoResponse(c, resp, pipeline, actualProtocol, body, startTime); err != nil {
		writeOpenCodeGoError(c, http.StatusBadGateway, openCodeGoErrorFormatForProtocol(sourceProtocol), "upstream_error", "Failed to convert upstream response")
		return nil, err
	}
	return openCodeGoForwardResult(resp, usage, originalModel, upstreamModel, actualProtocol, false, startTime), nil
}

func (s *OpenCodeGoGatewayService) streamStandardResponse(
	c *gin.Context,
	resp *http.Response,
	pipeline *protocolconv.Pipeline,
	actualProtocol protocolconv.Protocol,
	sourceProtocol protocolconv.Protocol,
	originalModel string,
	upstreamModel string,
	startTime time.Time,
) (*ForwardResult, error) {
	usage := ClaudeUsage{}
	var firstTokenMs *int
	observe := func(payload []byte) {
		if firstTokenMs == nil && openCodeGoStreamPayloadHasOutput(payload, actualProtocol) {
			ms := int(time.Since(startTime).Milliseconds())
			firstTokenMs = &ms
		}
		mergeOpenCodeGoStreamUsage(&usage, payload, actualProtocol)
	}
	clientDisconnected, err := s.convertOpenCodeGoStream(c, resp, pipeline, actualProtocol, sourceProtocol, observe)
	out := openCodeGoForwardResult(resp, usage, originalModel, upstreamModel, actualProtocol, true, startTime)
	out.ClientDisconnect = clientDisconnected
	out.FirstTokenMs = firstTokenMs
	return out, err
}

func openCodeGoUsageFromBody(body []byte, actualProtocol protocolconv.Protocol) ClaudeUsage {
	if actualProtocol == protocolconv.ProtocolAnthropic {
		return claudeUsageFromAnthropicBody(body)
	}
	return claudeUsageFromChatBody(body)
}

func mergeOpenCodeGoStreamUsage(usage *ClaudeUsage, payload []byte, actualProtocol protocolconv.Protocol) {
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
	if extracted := extractCCStreamUsage(string(payload)); extracted != nil {
		*usage = normalizeOpenCodeGoChatUsage(ClaudeUsage{
			InputTokens:              extracted.InputTokens,
			OutputTokens:             extracted.OutputTokens,
			CacheReadInputTokens:     extracted.CacheReadInputTokens,
			CacheCreationInputTokens: extracted.CacheCreationInputTokens,
			ImageOutputTokens:        extracted.ImageOutputTokens,
		})
	}
}

func openCodeGoStreamPayloadHasOutput(payload []byte, actualProtocol protocolconv.Protocol) bool {
	if actualProtocol == protocolconv.ProtocolAnthropic {
		typeName := gjson.GetBytes(payload, "type").String()
		return typeName != "" && typeName != "message_start" && typeName != "message_stop"
	}
	return !isOpenAIChatUsageOnlyStreamChunk(string(payload))
}

func (s *OpenCodeGoGatewayService) bufferChatPassthrough(
	c *gin.Context,
	resp *http.Response,
	pipeline *protocolconv.Pipeline,
	originalModel string,
	upstreamModel string,
	startTime time.Time,
) (*ForwardResult, error) {
	body, err := ReadUpstreamResponseBody(resp.Body, s.cfg, c, openAITooLargeError)
	if err != nil {
		if !errors.Is(err, ErrUpstreamResponseBodyTooLarge) {
			writeOpenCodeGoError(c, http.StatusBadGateway, openCodeGoErrorFormatChat, "upstream_error", "Failed to read upstream response")
		}
		return nil, err
	}
	usage := claudeUsageFromChatBody(body)
	if err := s.renderOpenCodeGoResponse(c, resp, pipeline, protocolconv.ProtocolOpenAIChat, body, startTime); err != nil {
		writeOpenCodeGoError(c, http.StatusBadGateway, openCodeGoErrorFormatChat, "upstream_error", "Failed to validate upstream chat completions response")
		return nil, err
	}
	return openCodeGoForwardResult(resp, usage, originalModel, upstreamModel, protocolconv.ProtocolOpenAIChat, false, startTime), nil
}

func (s *OpenCodeGoGatewayService) bufferAnthropicPassthrough(
	c *gin.Context,
	resp *http.Response,
	pipeline *protocolconv.Pipeline,
	originalModel string,
	upstreamModel string,
	startTime time.Time,
) (*ForwardResult, error) {
	body, err := ReadUpstreamResponseBody(resp.Body, s.cfg, c, anthropicTooLargeError)
	if err != nil {
		if !errors.Is(err, ErrUpstreamResponseBodyTooLarge) {
			writeOpenCodeGoError(c, http.StatusBadGateway, openCodeGoErrorFormatAnthropic, "upstream_error", "Failed to read upstream response")
		}
		return nil, err
	}
	usage := claudeUsageFromAnthropicBody(body)
	if err := s.renderOpenCodeGoResponse(c, resp, pipeline, protocolconv.ProtocolAnthropic, body, startTime); err != nil {
		writeOpenCodeGoError(c, http.StatusBadGateway, openCodeGoErrorFormatAnthropic, "upstream_error", "Failed to validate upstream messages response")
		return nil, err
	}
	return openCodeGoForwardResult(resp, usage, originalModel, upstreamModel, protocolconv.ProtocolAnthropic, false, startTime), nil
}

func (s *OpenCodeGoGatewayService) renderOpenCodeGoResponse(
	c *gin.Context,
	resp *http.Response,
	pipeline *protocolconv.Pipeline,
	actualProtocol protocolconv.Protocol,
	body []byte,
	startTime time.Time,
) error {
	structured := protocoltransport.Response{
		StatusCode:     resp.StatusCode,
		Headers:        responseheaders.FilterHeaders(resp.Header, s.responseHeaderFilter),
		Body:           append([]byte(nil), body...),
		ActualProtocol: actualProtocol,
		RequestID:      resp.Header.Get("x-request-id"),
		ResponseID:     extractOpenAIResponseIDFromJSONBytes(body),
		Duration:       time.Since(startTime),
	}
	if err := structured.Validate(); err != nil {
		return fmt.Errorf("validate OpenCode Go response: %w", err)
	}
	converted, err := pipeline.ConvertResponse(structured.Body, structured.ActualProtocol)
	if err != nil {
		return fmt.Errorf("convert OpenCode Go response: %w", err)
	}
	renderer, err := protocolconv.NewRenderer(converted.Source)
	if err != nil {
		return err
	}
	return renderer.RenderJSON(c.Writer, structured.StatusCode, structured.Headers, converted.Body)
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
	if err := s.renderOpenCodeGoResponse(c, resp, pipeline, protocolconv.ProtocolAnthropic, body, startTime); err != nil {
		writeOpenCodeGoError(c, http.StatusBadGateway, openCodeGoErrorFormatChat, "upstream_error", "Failed to convert upstream messages response")
		return nil, err
	}
	return openCodeGoForwardResult(resp, usage, originalModel, upstreamModel, protocolconv.ProtocolAnthropic, false, startTime), nil
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
	if err := s.renderOpenCodeGoResponse(c, resp, pipeline, protocolconv.ProtocolOpenAIChat, body, startTime); err != nil {
		writeOpenCodeGoError(c, http.StatusBadGateway, openCodeGoErrorFormatAnthropic, "upstream_error", "Failed to convert upstream chat completions response")
		return nil, err
	}
	return openCodeGoForwardResult(resp, usage, originalModel, upstreamModel, protocolconv.ProtocolOpenAIChat, false, startTime), nil
}

func (s *OpenCodeGoGatewayService) streamChatPassthrough(
	c *gin.Context,
	resp *http.Response,
	pipeline *protocolconv.Pipeline,
	originalModel string,
	upstreamModel string,
	startTime time.Time,
) (*ForwardResult, error) {
	usage := ClaudeUsage{}
	var firstTokenMs *int
	observe := func(payload []byte) {
		text := string(payload)
		usageOnlyChunk := isOpenAIChatUsageOnlyStreamChunk(text)
		if u := extractCCStreamUsage(text); u != nil {
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
	}
	clientDisconnected, err := s.convertOpenCodeGoStream(c, resp, pipeline, protocolconv.ProtocolOpenAIChat, protocolconv.ProtocolOpenAIChat, observe)
	out := openCodeGoForwardResult(resp, usage, originalModel, upstreamModel, protocolconv.ProtocolOpenAIChat, true, startTime)
	out.ClientDisconnect = clientDisconnected
	out.FirstTokenMs = firstTokenMs
	return out, err
}

func (s *OpenCodeGoGatewayService) streamAnthropicPassthrough(
	c *gin.Context,
	resp *http.Response,
	pipeline *protocolconv.Pipeline,
	originalModel string,
	upstreamModel string,
	startTime time.Time,
) (*ForwardResult, error) {
	usage := ClaudeUsage{}
	var firstTokenMs *int
	observe := func(payload []byte) {
		var event apicompat.AnthropicStreamEvent
		if err := json.Unmarshal(payload, &event); err != nil {
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
	}
	clientDisconnected, err := s.convertOpenCodeGoStream(c, resp, pipeline, protocolconv.ProtocolAnthropic, protocolconv.ProtocolAnthropic, observe)
	out := openCodeGoForwardResult(resp, usage, originalModel, upstreamModel, protocolconv.ProtocolAnthropic, true, startTime)
	out.ClientDisconnect = clientDisconnected
	out.FirstTokenMs = firstTokenMs
	return out, err
}

func (s *OpenCodeGoGatewayService) streamAnthropicToChat(
	c *gin.Context,
	resp *http.Response,
	pipeline *protocolconv.Pipeline,
	originalModel string,
	upstreamModel string,
	startTime time.Time,
) (*ForwardResult, error) {
	usage := ClaudeUsage{}
	var firstTokenMs *int
	observe := func(payload []byte) {
		var event apicompat.AnthropicStreamEvent
		if json.Unmarshal(payload, &event) != nil {
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
	}
	clientDisconnected, err := s.convertOpenCodeGoStream(c, resp, pipeline, protocolconv.ProtocolAnthropic, protocolconv.ProtocolOpenAIChat, observe)
	return &ForwardResult{
		RequestID: resp.Header.Get("x-request-id"), ActualProtocol: protocolconv.ProtocolAnthropic,
		Usage: usage, Model: originalModel,
		UpstreamModel: optionalUpstreamModel(originalModel, upstreamModel), Stream: true,
		Duration: time.Since(startTime), FirstTokenMs: firstTokenMs, ClientDisconnect: clientDisconnected,
	}, err
}

func (s *OpenCodeGoGatewayService) streamChatToAnthropic(
	c *gin.Context,
	resp *http.Response,
	pipeline *protocolconv.Pipeline,
	originalModel string,
	upstreamModel string,
	startTime time.Time,
) (*ForwardResult, error) {
	usage := ClaudeUsage{}
	var firstTokenMs *int
	observe := func(payload []byte) {
		text := string(payload)
		if u := extractCCStreamUsage(text); u != nil {
			usage = normalizeOpenCodeGoChatUsage(ClaudeUsage{
				InputTokens: u.InputTokens, OutputTokens: u.OutputTokens, CacheReadInputTokens: u.CacheReadInputTokens,
			})
		}
		if firstTokenMs == nil && !isOpenAIChatUsageOnlyStreamChunk(text) {
			ms := int(time.Since(startTime).Milliseconds())
			firstTokenMs = &ms
		}
	}
	clientDisconnected, err := s.convertOpenCodeGoStream(c, resp, pipeline, protocolconv.ProtocolOpenAIChat, protocolconv.ProtocolAnthropic, observe)
	return &ForwardResult{
		RequestID: resp.Header.Get("x-request-id"), ActualProtocol: protocolconv.ProtocolOpenAIChat,
		Usage: usage, Model: originalModel,
		UpstreamModel: optionalUpstreamModel(originalModel, upstreamModel), Stream: true,
		Duration: time.Since(startTime), FirstTokenMs: firstTokenMs, ClientDisconnect: clientDisconnected,
	}, err
}

func (s *OpenCodeGoGatewayService) convertOpenCodeGoStream(
	c *gin.Context,
	resp *http.Response,
	pipeline *protocolconv.Pipeline,
	actualProtocol protocolconv.Protocol,
	sourceProtocol protocolconv.Protocol,
	observe func([]byte),
) (bool, error) {
	maxRecordSize := defaultMaxLineSize
	if s.cfg != nil && s.cfg.Gateway.MaxLineSize > 0 {
		maxRecordSize = s.cfg.Gateway.MaxLineSize
	}
	filteredHeaders := make(http.Header)
	if s.responseHeaderFilter != nil {
		filteredHeaders = responseheaders.FilterHeaders(resp.Header, s.responseHeaderFilter)
	}
	parser := protocoltransport.NewSSEParser(resp.Body, maxRecordSize)
	stream := &protocoltransport.Stream{
		StatusCode: resp.StatusCode, Headers: filteredHeaders, ActualProtocol: actualProtocol,
		RequestID: resp.Header.Get("x-request-id"), Events: parser,
	}
	if err := stream.Validate(); err != nil {
		_ = stream.Close()
		return false, err
	}
	defer func() { _ = stream.Close() }()
	session, err := pipeline.NewStreamProcessor(stream.ActualProtocol)
	if err != nil {
		return false, err
	}
	renderer, err := protocolconv.NewRenderer(sourceProtocol)
	if err != nil {
		return false, err
	}
	headersWritten := false
	clientDisconnected := false
	lastDownstreamWriteAt := time.Now()
	ensureHeaders := func() error {
		if headersWritten {
			return nil
		}
		if err := renderer.WriteStreamHeaders(c.Writer, stream.StatusCode, stream.Headers); err != nil {
			return err
		}
		headersWritten = true
		return nil
	}
	writePayloads := func(payloads [][]byte) error {
		if clientDisconnected || len(payloads) == 0 {
			return nil
		}
		framedPayloads := make([][]byte, 0, len(payloads))
		for _, payload := range payloads {
			framed, err := renderer.FrameStreamEvent(payload)
			if err != nil {
				return err
			}
			framedPayloads = append(framedPayloads, framed)
		}
		if err := ensureHeaders(); err != nil {
			return err
		}
		for _, framed := range framedPayloads {
			if _, err := c.Writer.Write(framed); err != nil {
				clientDisconnected = true
				return nil
			}
		}
		c.Writer.Flush()
		lastDownstreamWriteAt = time.Now()
		return nil
	}

	type streamReadResult struct {
		record protocoltransport.SSERecord
		err    error
	}
	events := make(chan streamReadResult, 16)
	done := make(chan struct{})
	go func() {
		defer close(events)
		for {
			record, nextErr := stream.Events.Next(context.Background())
			select {
			case events <- streamReadResult{record: record, err: nextErr}:
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

	identityStream := actualProtocol == sourceProtocol
	identityTerminal := false
	readComplete := false
	for !readComplete {
		select {
		case event, ok := <-events:
			if !ok {
				readComplete = true
				continue
			}
			nextErr := event.err
			if errors.Is(nextErr, protocoltransport.ErrSSEDone) {
				if identityStream && actualProtocol == protocolconv.ProtocolOpenAIChat {
					identityTerminal = true
				}
				readComplete = true
				continue
			}
			if errors.Is(nextErr, io.EOF) {
				readComplete = true
				continue
			}
			if nextErr != nil {
				return clientDisconnected, nextErr
			}
			record := event.record
			if identityStream && actualProtocol == protocolconv.ProtocolAnthropic && gjson.GetBytes(record.Data, "type").String() == "message_stop" {
				identityTerminal = true
			}
			if observe != nil {
				observe(record.Data)
			}
			payloads, _, convertErr := session.Convert(record.Data)
			if convertErr != nil {
				return clientDisconnected, convertErr
			}
			if err := writePayloads(payloads); err != nil {
				return clientDisconnected, err
			}

		case <-intervalCh:
			if time.Since(parser.Progress().LastReadAt) < streamInterval {
				continue
			}
			if clientDisconnected {
				return clientDisconnected, errors.New("OpenCode Go stream usage incomplete after timeout")
			}
			return clientDisconnected, errors.New("OpenCode Go stream data interval timeout")

		case <-keepaliveCh:
			if clientDisconnected || parser.Progress().InRecord || time.Since(lastDownstreamWriteAt) < keepaliveInterval {
				continue
			}
			if err := ensureHeaders(); err != nil {
				return clientDisconnected, err
			}
			if _, err := c.Writer.Write(renderer.StreamKeepalive()); err != nil {
				clientDisconnected = true
				continue
			}
			c.Writer.Flush()
			lastDownstreamWriteAt = time.Now()
		}
	}
	if identityStream && !identityTerminal {
		return clientDisconnected, fmt.Errorf("OpenCode Go %s stream ended without terminal event", actualProtocol)
	}
	payloads, _, err := session.Finalize()
	if err != nil {
		return clientDisconnected, err
	}
	if err := writePayloads(payloads); err != nil {
		return clientDisconnected, err
	}
	if !clientDisconnected {
		if err := ensureHeaders(); err != nil {
			return false, err
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
	return clientDisconnected, nil
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
		Route:           route,
		Options:         protocolconv.Options{SourceModel: upstreamModel, LossPolicy: protocolconv.LossError},
		ResponseOptions: &protocolconv.Options{SourceModel: upstreamModel, LossPolicy: protocolconv.LossWarn},
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

func claudeUsageFromChatBody(body []byte) ClaudeUsage {
	return normalizeOpenCodeGoChatUsage(ClaudeUsage{
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

func claudeUsageFromChatUsage(usage *apicompat.ChatUsage) ClaudeUsage {
	if usage == nil {
		return ClaudeUsage{}
	}
	out := ClaudeUsage{
		InputTokens:              usage.PromptTokens,
		OutputTokens:             usage.CompletionTokens,
		CacheCreationInputTokens: usage.CacheCreationInputTokens,
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

func openCodeGoForwardResult(resp *http.Response, usage ClaudeUsage, model string, upstreamModel string, actualProtocol protocolconv.Protocol, stream bool, startTime time.Time) *ForwardResult {
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

func collectOpenCodeGoErrorResponse(resp *http.Response, actualProtocol protocolconv.Protocol, streamRequested bool) (protocoltransport.Response, error) {
	if resp == nil {
		return protocoltransport.Response{}, fmt.Errorf("collect OpenCode Go error response: nil response")
	}
	headers := protocoltransport.CloneHeaders(resp.Header)
	requestID := resp.Header.Get("x-request-id")
	var body []byte
	if streamRequested {
		stream := &protocoltransport.Stream{
			StatusCode:     resp.StatusCode,
			Headers:        headers,
			ActualProtocol: actualProtocol,
			RequestID:      requestID,
			ErrorBody:      resp.Body,
		}
		if err := stream.Validate(); err != nil {
			return protocoltransport.Response{}, fmt.Errorf("validate OpenCode Go error stream: %w", err)
		}
		resp.Body = http.NoBody
		body = readOpenCodeGoErrorBody(stream.ErrorBody)
		_ = stream.Close()
	} else {
		body = readOpenCodeGoErrorBody(resp.Body)
	}
	upstream := protocoltransport.Response{
		StatusCode:     resp.StatusCode,
		Headers:        headers,
		Body:           body,
		ActualProtocol: actualProtocol,
		RequestID:      requestID,
	}
	if err := upstream.Validate(); err != nil {
		return protocoltransport.Response{}, fmt.Errorf("validate OpenCode Go error response: %w", err)
	}
	if !upstream.IsError() {
		return protocoltransport.Response{}, fmt.Errorf("collect OpenCode Go error response: status %d is not an error", upstream.StatusCode)
	}
	return upstream, nil
}

func writeOpenCodeGoUpstreamResponse(c *gin.Context, upstream protocoltransport.Response, filter *responseheaders.CompiledHeaderFilter, format openCodeGoErrorFormat) {
	if c == nil || c.Writer == nil {
		return
	}
	if filter != nil {
		responseheaders.WriteFilteredHeaders(c.Writer.Header(), upstream.Headers, filter)
	}
	// Existing Chat and Messages clients depend on raw provider error bodies.
	// New cross-protocol sources must receive their own standard error envelope.
	if format == openCodeGoErrorFormatResponses || format == openCodeGoErrorFormatGoogle {
		message := sanitizeUpstreamErrorMessage(strings.TrimSpace(extractUpstreamErrorMessage(upstream.Body)))
		if message == "" {
			message = http.StatusText(upstream.StatusCode)
		}
		writeOpenCodeGoError(c, upstream.StatusCode, format, "upstream_error", message)
		return
	}
	c.Writer.Header().Set("Content-Type", "application/json")
	c.Writer.WriteHeader(upstream.StatusCode)
	_, _ = c.Writer.Write(upstream.Body)
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
	openCodeGoErrorFormatResponses openCodeGoErrorFormat = "responses"
	openCodeGoErrorFormatAnthropic openCodeGoErrorFormat = "anthropic"
	openCodeGoErrorFormatGoogle    openCodeGoErrorFormat = "google"
)

type openCodeGoResponseMode string

const (
	openCodeGoResponseChat      openCodeGoResponseMode = "chat"
	openCodeGoResponseResponses openCodeGoResponseMode = "responses"
	openCodeGoResponseAnthropic openCodeGoResponseMode = "anthropic"
	openCodeGoResponseGoogle    openCodeGoResponseMode = "google"
)

func (m openCodeGoResponseMode) protocol() protocolconv.Protocol {
	switch m {
	case openCodeGoResponseResponses:
		return protocolconv.ProtocolOpenAIResponses
	case openCodeGoResponseAnthropic:
		return protocolconv.ProtocolAnthropic
	case openCodeGoResponseGoogle:
		return protocolconv.ProtocolGoogleGenAI
	default:
		return protocolconv.ProtocolOpenAIChat
	}
}

func (m openCodeGoResponseMode) errorFormat() openCodeGoErrorFormat {
	return openCodeGoErrorFormatForProtocol(m.protocol())
}

func openCodeGoErrorFormatForProtocol(protocol protocolconv.Protocol) openCodeGoErrorFormat {
	switch protocol {
	case protocolconv.ProtocolOpenAIResponses:
		return openCodeGoErrorFormatResponses
	case protocolconv.ProtocolAnthropic:
		return openCodeGoErrorFormatAnthropic
	case protocolconv.ProtocolGoogleGenAI:
		return openCodeGoErrorFormatGoogle
	default:
		return openCodeGoErrorFormatChat
	}
}

func (f openCodeGoErrorFormat) protocol() protocolconv.Protocol {
	switch f {
	case openCodeGoErrorFormatResponses:
		return protocolconv.ProtocolOpenAIResponses
	case openCodeGoErrorFormatAnthropic:
		return protocolconv.ProtocolAnthropic
	case openCodeGoErrorFormatGoogle:
		return protocolconv.ProtocolGoogleGenAI
	default:
		return protocolconv.ProtocolOpenAIChat
	}
}

func writeOpenCodeGoError(c *gin.Context, status int, format openCodeGoErrorFormat, errType string, message string) {
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

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
	"github.com/Wei-Shaw/sub2api/internal/util/responseheaders"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// ClinePassGatewayService forwards all supported ingress protocols to Cline's
// Chat Completions transport.
type ClinePassGatewayService struct {
	client               *ClinePassClient
	cfg                  *config.Config
	responseHeaderFilter *responseheaders.CompiledHeaderFilter
	rateLimitService     *RateLimitService
}

func NewClinePassGatewayService(client *ClinePassClient, cfg *config.Config, rateLimitService *RateLimitService) *ClinePassGatewayService {
	return &ClinePassGatewayService{
		client:               client,
		cfg:                  cfg,
		responseHeaderFilter: compileResponseHeaderFilter(cfg),
		rateLimitService:     rateLimitService,
	}
}

func (s *ClinePassGatewayService) ForwardChatCompletions(ctx context.Context, c *gin.Context, account *Account, body []byte) (*ForwardResult, error) {
	return s.forward(ctx, c, account, body, "", "", false, protocolconv.ProtocolOpenAIChat, openCodeGoResponseChat)
}

func (s *ClinePassGatewayService) ForwardResponses(ctx context.Context, c *gin.Context, account *Account, body []byte, clientModel string) (*ForwardResult, error) {
	return s.forward(ctx, c, account, body, clientModel, "", false, protocolconv.ProtocolOpenAIResponses, openCodeGoResponseResponses)
}

func (s *ClinePassGatewayService) ForwardMessages(ctx context.Context, c *gin.Context, account *Account, body []byte) (*ForwardResult, error) {
	return s.forward(ctx, c, account, body, "", "", false, protocolconv.ProtocolAnthropic, openCodeGoResponseAnthropic)
}

func (s *ClinePassGatewayService) ForwardGoogleGenAI(ctx context.Context, c *gin.Context, account *Account, body []byte, clientModel, routingModel string, stream bool) (*ForwardResult, error) {
	return s.forward(ctx, c, account, body, clientModel, routingModel, stream, protocolconv.ProtocolGoogleGenAI, openCodeGoResponseGoogle)
}

func (s *ClinePassGatewayService) forward(
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
	if len(body) == 0 || !gjson.ValidBytes(body) {
		writeOpenCodeGoError(c, http.StatusBadRequest, format, "invalid_request_error", "Failed to parse request body")
		return nil, fmt.Errorf("invalid ClinePass %s request", source)
	}
	if source != protocolconv.ProtocolGoogleGenAI {
		routingModel = strings.TrimSpace(gjson.GetBytes(body, "model").String())
		stream = gjson.GetBytes(body, "stream").Bool()
	}
	if routingModel == "" {
		writeOpenCodeGoError(c, http.StatusBadRequest, format, "invalid_request_error", "model is required")
		return nil, fmt.Errorf("ClinePass model is required")
	}
	if clientModel == "" {
		clientModel = routingModel
	}
	if account == nil || !account.IsClinePassAPIKey() {
		writeOpenCodeGoError(c, http.StatusBadGateway, format, "upstream_error", "ClinePass account must use API key credentials")
		return nil, fmt.Errorf("invalid ClinePass account")
	}
	upstreamModel := strings.TrimSpace(account.GetMappedModel(routingModel))
	if upstreamModel == "" {
		upstreamModel = routingModel
	}
	if !isClinePassModelID(upstreamModel) {
		writeOpenCodeGoError(c, http.StatusBadRequest, format, "invalid_request_error", "ClinePass upstream model must be a full cline-pass/... slug")
		return nil, fmt.Errorf("invalid ClinePass upstream model %q", upstreamModel)
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
	pipeline, converted, err := newClinePassPipelineRequest(sourceBody, source, account, clientModel, upstreamModel)
	if err != nil {
		writeOpenCodeGoError(c, http.StatusBadRequest, format, "invalid_request_error", "Request cannot be represented as ClinePass Chat Completions")
		return nil, err
	}
	converted, err = sjson.SetBytes(converted, "stream", stream)
	if err != nil {
		return nil, err
	}
	if stream {
		converted, err = ensureOpenAIChatStreamUsage(converted)
		if err != nil {
			return nil, err
		}
	}
	converted, err = normalizeClinePassChatRequest(converted, upstreamModel)
	if err != nil {
		writeOpenCodeGoError(c, http.StatusBadRequest, format, "invalid_request_error", "Failed to normalize Chat Completions request")
		return nil, err
	}

	endpoint, err := s.client.endpointURL(account, clinePassChatCompletionsPath)
	if err != nil {
		writeOpenCodeGoError(c, http.StatusBadRequest, format, "invalid_request_error", err.Error())
		return nil, err
	}
	startTime := time.Now()
	resp, err := s.send(ctx, c, account, endpoint, converted, stream, format)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if err := s.handleError(ctx, c, account, resp, format); err != nil {
		return nil, err
	}
	if stream {
		return s.stream(c, account, resp, pipeline, responseMode.protocol(), clientModel, upstreamModel, startTime)
	}
	return s.buffer(c, account, resp, pipeline, responseMode.protocol(), clientModel, upstreamModel, startTime)
}

func (s *ClinePassGatewayService) send(ctx context.Context, c *gin.Context, account *Account, endpoint string, body []byte, stream bool, format openCodeGoErrorFormat) (*http.Response, error) {
	apiKey := account.GetClinePassAPIKey()
	if apiKey == "" {
		writeOpenCodeGoError(c, http.StatusBadGateway, format, "upstream_error", "ClinePass account is missing api_key")
		return nil, fmt.Errorf("ClinePass account %d missing api_key", account.ID)
	}
	upstreamCtx, release := detachUpstreamContext(ctx)
	req, err := http.NewRequestWithContext(upstreamCtx, http.MethodPost, endpoint, bytes.NewReader(body))
	release()
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	if stream {
		req.Header.Set("Accept", "text/event-stream")
	} else {
		req.Header.Set("Accept", "application/json")
	}
	resp, err := s.client.do(account, req)
	if err == nil {
		return resp, nil
	}
	safeErr := sanitizeUpstreamErrorMessage(err.Error())
	setOpsUpstreamError(c, 0, safeErr, "")
	appendOpsUpstreamError(c, OpsUpstreamErrorEvent{Platform: account.Platform, AccountID: account.ID, AccountName: account.Name, Kind: "request_error", Message: safeErr})
	return nil, &UpstreamFailoverError{StatusCode: http.StatusBadGateway, ResponseBody: []byte(safeErr)}
}

func (s *ClinePassGatewayService) handleError(ctx context.Context, c *gin.Context, account *Account, resp *http.Response, format openCodeGoErrorFormat) error {
	if resp.StatusCode < http.StatusBadRequest {
		return nil
	}
	body, err := readClinePassBody(resp.Body)
	if err != nil {
		return err
	}
	decoded := decodeClinePassError(resp.StatusCode, resp.Header, body)
	status := decoded.EffectiveStatus()
	setOpsUpstreamError(c, status, decoded.Message, "")

	shouldDisable := false
	if s.rateLimitService != nil {
		shouldDisable = s.rateLimitService.HandleUpstreamError(ctx, account, status, resp.Header, body)
	}

	kind := "passthrough"
	if decoded.Retryable || decoded.AccountAffecting || shouldDisable {
		kind = "failover"
	}
	appendOpsUpstreamError(c, OpsUpstreamErrorEvent{Platform: account.Platform, AccountID: account.ID, AccountName: account.Name, UpstreamStatusCode: status, UpstreamRequestID: decoded.RequestID, Kind: kind, Message: decoded.Message})

	if decoded.Retryable || decoded.AccountAffecting || shouldDisable {
		return &UpstreamFailoverError{StatusCode: status, ResponseBody: body, ResponseHeaders: protocoltransport.CloneHeaders(resp.Header)}
	}
	writeOpenCodeGoError(c, status, format, firstNonEmptyString(decoded.Type, decoded.Code, "upstream_error"), decoded.Message)
	return decoded
}

func (s *ClinePassGatewayService) buffer(c *gin.Context, account *Account, resp *http.Response, pipeline *protocolconv.Pipeline, sourceProtocol protocolconv.Protocol, originalModel, upstreamModel string, startTime time.Time) (*ForwardResult, error) {
	body, err := ReadUpstreamResponseBody(resp.Body, s.cfg, c, nil)
	if err != nil {
		return nil, s.preCommitFailure(c, account, resp, "response_read_error", err)
	}
	normalized, err := normalizeClinePassBufferedResponse(body, originalModel)
	if err != nil {
		return nil, s.preCommitFailure(c, account, resp, "invalid_response_envelope", err)
	}
	usage := claudeUsageFromChatBody(normalized)
	structured := protocoltransport.Response{
		StatusCode:     resp.StatusCode,
		Headers:        responseheaders.FilterHeaders(resp.Header, s.responseHeaderFilter),
		Body:           normalized,
		ActualProtocol: protocolconv.ProtocolOpenAIChat,
		RequestID:      resp.Header.Get("x-request-id"),
		ResponseID:     extractOpenAIResponseIDFromJSONBytes(normalized),
		Duration:       time.Since(startTime),
	}
	if err := structured.Validate(); err != nil {
		return nil, s.preCommitFailure(c, account, resp, "invalid_response_transport", err)
	}
	converted, err := pipeline.ConvertResponse(normalized, protocolconv.ProtocolOpenAIChat)
	if err != nil {
		return nil, s.preCommitFailure(c, account, resp, "response_conversion_error", err)
	}
	renderer, err := protocolconv.NewRenderer(converted.Source)
	if err != nil {
		return nil, err
	}
	if err := renderer.RenderJSON(c.Writer, structured.StatusCode, structured.Headers, converted.Body); err != nil {
		return nil, err
	}
	return openCodeGoForwardResult(resp, usage, originalModel, upstreamModel, protocolconv.ProtocolOpenAIChat, false, startTime), nil
}

func (s *ClinePassGatewayService) stream(c *gin.Context, account *Account, resp *http.Response, pipeline *protocolconv.Pipeline, sourceProtocol protocolconv.Protocol, originalModel, upstreamModel string, startTime time.Time) (*ForwardResult, error) {
	maxRecordSize := defaultMaxLineSize
	if s.cfg != nil && s.cfg.Gateway.MaxLineSize > 0 {
		maxRecordSize = s.cfg.Gateway.MaxLineSize
	}
	stream := &protocoltransport.Stream{
		StatusCode:     resp.StatusCode,
		Headers:        responseheaders.FilterHeaders(resp.Header, s.responseHeaderFilter),
		ActualProtocol: protocolconv.ProtocolOpenAIChat,
		RequestID:      resp.Header.Get("x-request-id"),
		Events:         protocoltransport.NewSSEParser(resp.Body, maxRecordSize),
	}
	if err := stream.Validate(); err != nil {
		_ = stream.Close()
		return nil, err
	}
	defer func() { _ = stream.Close() }()
	session, err := pipeline.NewStreamProcessor(protocolconv.ProtocolOpenAIChat)
	if err != nil {
		return nil, err
	}
	renderer, err := protocolconv.NewRenderer(sourceProtocol)
	if err != nil {
		return nil, err
	}
	normalizer := newClinePassStreamNormalizer(originalModel)
	usage := ClaudeUsage{}
	var firstTokenMs *int
	headersWritten := false
	clientDisconnected := false
	streamFailure := func(kind string, err error) error {
		if headersWritten {
			return err
		}
		return s.preCommitFailure(c, account, resp, kind, err)
	}
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
				return nil
			}
		}
		c.Writer.Flush()
		return nil
	}

	terminal := false
	for {
		record, nextErr := stream.Events.Next(context.Background())
		if errors.Is(nextErr, protocoltransport.ErrSSEDone) {
			terminal = true
			break
		}
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			return nil, streamFailure("stream_read_error", nextErr)
		}
		normalized, normalizeErr := normalizer.Normalize(record.Data)
		if normalizeErr != nil {
			return nil, streamFailure("invalid_stream_event", normalizeErr)
		}
		if extracted := extractCCStreamUsage(string(normalized)); extracted != nil {
			usage = normalizeOpenCodeGoChatUsage(ClaudeUsage{
				InputTokens:              extracted.InputTokens,
				OutputTokens:             extracted.OutputTokens,
				CacheReadInputTokens:     extracted.CacheReadInputTokens,
				CacheCreationInputTokens: extracted.CacheCreationInputTokens,
				ImageOutputTokens:        extracted.ImageOutputTokens,
			})
		}
		if firstTokenMs == nil && !isOpenAIChatUsageOnlyStreamChunk(string(normalized)) {
			ms := int(time.Since(startTime).Milliseconds())
			firstTokenMs = &ms
		}
		payloads, _, convertErr := session.Convert(normalized)
		if convertErr != nil {
			return nil, streamFailure("stream_conversion_error", convertErr)
		}
		if err := writePayloads(payloads); err != nil {
			return nil, err
		}
	}
	if !terminal {
		return nil, streamFailure("stream_missing_terminal", fmt.Errorf("ClinePass stream ended without [DONE]"))
	}
	payloads, _, err := session.Finalize()
	if err != nil {
		return nil, streamFailure("stream_finalize_error", err)
	}
	if err := writePayloads(payloads); err != nil {
		return nil, err
	}
	if !clientDisconnected {
		if !headersWritten {
			if err := renderer.WriteStreamHeaders(c.Writer, stream.StatusCode, stream.Headers); err != nil {
				return nil, err
			}
		}
		if end := renderer.StreamTerminal(); len(end) > 0 {
			if _, err := c.Writer.Write(end); err != nil {
				clientDisconnected = true
			}
		}
		if !clientDisconnected {
			c.Writer.Flush()
		}
	}
	result := openCodeGoForwardResult(resp, usage, originalModel, upstreamModel, protocolconv.ProtocolOpenAIChat, true, startTime)
	result.ClientDisconnect = clientDisconnected
	result.FirstTokenMs = firstTokenMs
	return result, nil
}

func (s *ClinePassGatewayService) preCommitFailure(c *gin.Context, account *Account, resp *http.Response, kind string, err error) error {
	message := "invalid ClinePass upstream response"
	if err != nil {
		message = sanitizeUpstreamErrorMessage(err.Error())
	}
	setOpsUpstreamError(c, http.StatusBadGateway, message, "")
	event := OpsUpstreamErrorEvent{Platform: PlatformClinePass, Kind: kind, UpstreamStatusCode: http.StatusBadGateway, Message: message}
	if account != nil {
		event.AccountID = account.ID
		event.AccountName = account.Name
	}
	if resp != nil {
		event.UpstreamRequestID = strings.TrimSpace(resp.Header.Get("x-request-id"))
	}
	appendOpsUpstreamError(c, event)
	failure := &UpstreamFailoverError{StatusCode: http.StatusBadGateway, ResponseBody: []byte(message)}
	if resp != nil {
		failure.ResponseHeaders = protocoltransport.CloneHeaders(resp.Header)
	}
	return failure
}

func newClinePassPipelineRequest(body []byte, source protocolconv.Protocol, account *Account, clientModel, upstreamModel string) (*protocolconv.Pipeline, []byte, error) {
	route := protocolconv.Route{Source: source, IntendedTarget: protocolconv.ProtocolOpenAIChat, ClientModel: clientModel, UpstreamModel: upstreamModel}
	if account != nil {
		route.Provider = account.Platform
		route.AccountID = account.ID
	}
	pipeline, err := protocolconv.NewPipeline(standardProtocolRegistry, protocolconv.PipelineConfig{
		Route:           route,
		Options:         protocolconv.Options{SourceModel: upstreamModel, LossPolicy: protocolconv.LossError, ChatExtensions: protocolconv.ChatExtensions{AnthropicCacheControl: true}},
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

func normalizeClinePassBufferedResponse(body []byte, clientModel string) ([]byte, error) {
	var envelope struct {
		Success bool            `json:"success"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, err
	}
	trimmed := bytes.TrimSpace(envelope.Data)
	if !envelope.Success || len(trimmed) == 0 || trimmed[0] != '{' {
		return nil, fmt.Errorf("ClinePass response envelope is unsuccessful or missing data")
	}
	var response apicompat.ChatCompletionsResponse
	if err := json.Unmarshal(trimmed, &response); err != nil {
		return nil, err
	}
	response.Model = clientModel
	for index := range response.Choices {
		if response.Choices[index].FinishReason == "stop" && len(response.Choices[index].Message.ToolCalls) > 0 {
			response.Choices[index].FinishReason = "tool_calls"
		}
	}
	return json.Marshal(response)
}

type clinePassStreamNormalizer struct {
	clientModel string
	toolChoices map[int]bool
}

func newClinePassStreamNormalizer(clientModel string) *clinePassStreamNormalizer {
	return &clinePassStreamNormalizer{clientModel: clientModel, toolChoices: make(map[int]bool)}
}

func (n *clinePassStreamNormalizer) Normalize(payload []byte) ([]byte, error) {
	var chunk apicompat.ChatCompletionsChunk
	if err := json.Unmarshal(payload, &chunk); err != nil {
		return nil, err
	}
	chunk.Model = n.clientModel
	for index := range chunk.Choices {
		choice := &chunk.Choices[index]
		if len(choice.Delta.ToolCalls) > 0 {
			n.toolChoices[choice.Index] = true
		}
		if choice.FinishReason != nil && *choice.FinishReason == "stop" && n.toolChoices[choice.Index] {
			finish := "tool_calls"
			choice.FinishReason = &finish
		}
	}
	return json.Marshal(chunk)
}

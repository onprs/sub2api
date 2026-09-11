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

// CommandCodeGatewayService 把四种入口协议（OpenAI Chat Completions、OpenAI
// Responses、Anthropic Messages、Google GenAI）转发到 Command Code Provider API。
// 上游协议由模型决定：claude-* 模型走 Anthropic Messages，其余模型走 OpenAI
// Chat Completions（Provider API 对错误端点返回 400 invalid_request_error）。
type CommandCodeGatewayService struct {
	client               *CommandCodeClient
	cfg                  *config.Config
	responseHeaderFilter *responseheaders.CompiledHeaderFilter
	rateLimitService     *RateLimitService
}

func NewCommandCodeGatewayService(client *CommandCodeClient, cfg *config.Config, rateLimitService *RateLimitService) *CommandCodeGatewayService {
	return &CommandCodeGatewayService{
		client:               client,
		cfg:                  cfg,
		responseHeaderFilter: compileResponseHeaderFilter(cfg),
		rateLimitService:     rateLimitService,
	}
}

// ForwardChatCompletions 处理 OpenAI Chat Completions 入口请求。
func (s *CommandCodeGatewayService) ForwardChatCompletions(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	body []byte,
) (*ForwardResult, error) {
	return s.forwardStandardRequest(ctx, c, account, body, "", "", false, protocolconv.ProtocolOpenAIChat, openCodeGoResponseChat)
}

// ForwardResponses 处理 OpenAI Responses 入口请求。
func (s *CommandCodeGatewayService) ForwardResponses(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	body []byte,
	clientModel string,
) (*ForwardResult, error) {
	return s.forwardStandardRequest(ctx, c, account, body, clientModel, "", false, protocolconv.ProtocolOpenAIResponses, openCodeGoResponseResponses)
}

// ForwardMessages 处理 Anthropic Messages 入口请求。
func (s *CommandCodeGatewayService) ForwardMessages(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	body []byte,
) (*ForwardResult, error) {
	return s.forwardStandardRequest(ctx, c, account, body, "", "", false, protocolconv.ProtocolAnthropic, openCodeGoResponseAnthropic)
}

// ForwardGoogleGenAI 处理 Google generateContent 入口请求。模型与流式模式
// 由 URL 携带而非 Google JSON body。
func (s *CommandCodeGatewayService) ForwardGoogleGenAI(
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

func (s *CommandCodeGatewayService) forwardStandardRequest(
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
		return nil, fmt.Errorf("invalid Command Code %s request", source)
	}
	if source != protocolconv.ProtocolGoogleGenAI {
		routingModel = strings.TrimSpace(gjson.GetBytes(body, "model").String())
		stream = gjson.GetBytes(body, "stream").Bool()
	}
	if routingModel == "" {
		writeOpenCodeGoError(c, http.StatusBadRequest, format, "invalid_request_error", "model is required")
		return nil, fmt.Errorf("commandcode model is required")
	}
	if clientModel == "" {
		clientModel = routingModel
	}
	if account == nil || !account.IsCommandCodeAPIKey() {
		writeOpenCodeGoError(c, http.StatusBadGateway, format, "upstream_error", "Command Code account must use API key credentials")
		return nil, fmt.Errorf("invalid Command Code account")
	}
	upstreamModel := strings.TrimSpace(account.GetMappedModel(routingModel))
	if upstreamModel == "" {
		upstreamModel = routingModel
	}
	protocol, ok := account.ResolveCommandCodeModelProtocol(upstreamModel)
	if !ok {
		writeOpenCodeGoError(c, http.StatusBadRequest, format, "invalid_request_error", "Command Code model protocol is not configured for upstream model: "+upstreamModel)
		return nil, fmt.Errorf("commandcode model protocol not configured for %q", upstreamModel)
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
	case CommandCodeProtocolChatCompletions:
		target = protocolconv.ProtocolOpenAIChat
	case CommandCodeProtocolMessages:
		target = protocolconv.ProtocolAnthropic
	default:
		writeOpenCodeGoError(c, http.StatusBadRequest, format, "invalid_request_error", "Unsupported model protocol")
		return nil, fmt.Errorf("unsupported Command Code model protocol %q", protocol)
	}

	pipeline, converted, err := newCommandCodePipelineRequest(sourceBody, source, target, account, clientModel, upstreamModel)
	if err != nil {
		writeOpenCodeGoError(c, http.StatusBadRequest, format, "invalid_request_error", "Request cannot be represented for the selected Command Code model protocol")
		return nil, err
	}
	converted, err = sjson.SetBytes(converted, "stream", stream)
	if err != nil {
		return nil, fmt.Errorf("set Command Code upstream stream mode: %w", err)
	}

	if protocol == CommandCodeProtocolMessages {
		return s.forwardMessagesBody(ctx, c, account, converted, clientModel, upstreamModel, responseMode, pipeline)
	}
	if stream {
		converted, err = ensureOpenAIChatStreamUsage(converted)
		if err != nil {
			return nil, fmt.Errorf("enable chat stream usage: %w", err)
		}
	}
	return s.forwardChatBody(ctx, c, account, converted, clientModel, upstreamModel, responseMode, pipeline)
}

func newCommandCodePipelineRequest(
	body []byte,
	source, target protocolconv.Protocol,
	account *Account,
	clientModel, upstreamModel string,
) (*protocolconv.Pipeline, []byte, error) {
	options := protocolconv.Options{SourceModel: upstreamModel, LossPolicy: protocolconv.LossError}
	// Responses 历史中的不透明签名无法在 Chat 上游重放，只为此路径允许带警告丢弃。
	options.AllowChatRequestSignatureLoss = source == protocolconv.ProtocolOpenAIResponses && target == protocolconv.ProtocolOpenAIChat
	pipeline, err := protocolconv.NewPipeline(standardProtocolRegistry, protocolconv.PipelineConfig{
		Route: protocolconv.Route{
			Source: source, IntendedTarget: target, ClientModel: clientModel, UpstreamModel: upstreamModel,
			Provider: account.Platform, AccountID: account.ID,
		},
		Options:         options,
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

func (s *CommandCodeGatewayService) forwardChatBody(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	body []byte,
	originalModel string,
	upstreamModel string,
	responseMode openCodeGoResponseMode,
	pipeline *protocolconv.Pipeline,
) (*ForwardResult, error) {
	targetURL, err := s.client.endpointURL(account, commandCodeChatCompletionsPath)
	if err != nil {
		writeOpenCodeGoError(c, http.StatusBadRequest, responseMode.errorFormat(), "invalid_request_error", err.Error())
		return nil, err
	}
	stream := gjson.GetBytes(body, "stream").Bool()
	startTime := time.Now()
	resp, err := s.sendUpstream(ctx, c, account, targetURL, body, stream, responseMode.errorFormat(), CommandCodeProtocolChatCompletions)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if err := s.handleUpstreamError(ctx, c, account, resp, originalModel, protocolconv.ProtocolOpenAIChat, responseMode.errorFormat()); err != nil {
		return nil, err
	}
	if stream {
		return s.streamResponse(c, account, resp, pipeline, protocolconv.ProtocolOpenAIChat, responseMode.protocol(), originalModel, upstreamModel, startTime)
	}
	return s.bufferResponse(c, account, resp, pipeline, protocolconv.ProtocolOpenAIChat, responseMode.protocol(), originalModel, upstreamModel, startTime)
}

func (s *CommandCodeGatewayService) forwardMessagesBody(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	body []byte,
	originalModel string,
	upstreamModel string,
	responseMode openCodeGoResponseMode,
	pipeline *protocolconv.Pipeline,
) (*ForwardResult, error) {
	targetURL, err := s.client.endpointURL(account, commandCodeMessagesPath)
	if err != nil {
		writeOpenCodeGoError(c, http.StatusBadRequest, responseMode.errorFormat(), "invalid_request_error", err.Error())
		return nil, err
	}
	stream := gjson.GetBytes(body, "stream").Bool()
	startTime := time.Now()
	resp, err := s.sendUpstream(ctx, c, account, targetURL, body, stream, responseMode.errorFormat(), CommandCodeProtocolMessages)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if err := s.handleUpstreamError(ctx, c, account, resp, originalModel, protocolconv.ProtocolAnthropic, responseMode.errorFormat()); err != nil {
		return nil, err
	}
	if stream {
		return s.streamResponse(c, account, resp, pipeline, protocolconv.ProtocolAnthropic, responseMode.protocol(), originalModel, upstreamModel, startTime)
	}
	return s.bufferResponse(c, account, resp, pipeline, protocolconv.ProtocolAnthropic, responseMode.protocol(), originalModel, upstreamModel, startTime)
}

func (s *CommandCodeGatewayService) sendUpstream(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	targetURL string,
	body []byte,
	stream bool,
	format openCodeGoErrorFormat,
	protocol string,
) (*http.Response, error) {
	apiKey := account.GetCommandCodeAPIKey()
	if apiKey == "" {
		writeOpenCodeGoError(c, http.StatusBadGateway, format, "upstream_error", "Command Code account is missing api_key")
		return nil, fmt.Errorf("commandcode account %d missing api_key", account.ID)
	}

	upstreamCtx, releaseUpstreamCtx := detachUpstreamContext(ctx)
	upstreamReq, err := http.NewRequestWithContext(upstreamCtx, http.MethodPost, targetURL, bytes.NewReader(body))
	releaseUpstreamCtx()
	if err != nil {
		writeOpenCodeGoError(c, http.StatusBadGateway, format, "upstream_error", "Failed to build upstream request")
		return nil, err
	}
	upstreamReq.Header.Set("Content-Type", "application/json")
	// 官方文档：任意路由接受 Authorization: Bearer；/messages 额外接受 x-api-key。
	upstreamReq.Header.Set("Authorization", "Bearer "+apiKey)
	if protocol == CommandCodeProtocolMessages {
		upstreamReq.Header.Set("x-api-key", apiKey)
		upstreamReq.Header.Set("anthropic-version", "2023-06-01")
	}
	if stream {
		upstreamReq.Header.Set("Accept", "text/event-stream")
	} else {
		upstreamReq.Header.Set("Accept", "application/json")
	}

	resp, err := s.client.do(account, upstreamReq)
	if err != nil {
		safeErr := sanitizeUpstreamErrorMessage(err.Error())
		setOpsUpstreamError(c, 0, safeErr, "")
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
		return nil, &UpstreamFailoverError{StatusCode: http.StatusBadGateway, ResponseBody: []byte(safeErr)}
	}
	return resp, nil
}

func (s *CommandCodeGatewayService) handleUpstreamError(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	resp *http.Response,
	requestedModel string,
	actualProtocol protocolconv.Protocol,
	format openCodeGoErrorFormat,
) error {
	if resp.StatusCode < http.StatusBadRequest {
		return nil
	}
	body, err := readCommandCodeBody(resp.Body)
	if err != nil {
		return err
	}
	upstream := protocoltransport.Response{
		StatusCode:     resp.StatusCode,
		Headers:        protocoltransport.CloneHeaders(resp.Header),
		Body:           body,
		ActualProtocol: actualProtocol,
		RequestID:      resp.Header.Get("x-request-id"),
	}
	upstreamMsg := sanitizeUpstreamErrorMessage(strings.TrimSpace(extractUpstreamErrorMessage(body)))
	if upstreamMsg == "" {
		upstreamMsg = http.StatusText(resp.StatusCode)
	}
	setOpsUpstreamError(c, upstream.StatusCode, upstreamMsg, "")

	shouldDisable := false
	if s.rateLimitService != nil {
		shouldDisable = s.rateLimitService.HandleUpstreamError(ctx, account, resp.StatusCode, resp.Header, body, requestedModel)
	}

	kind := "passthrough"
	shouldFailover := shouldFailoverCommandCodeResponse(resp.StatusCode)
	if shouldFailover || shouldDisable {
		kind = "failover"
	}
	appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
		ProxyID:            opsUpstreamProxyID(account),
		ProxyName:          opsUpstreamProxyName(account),
		Platform:           account.Platform,
		AccountID:          account.ID,
		AccountName:        account.Name,
		UpstreamStatusCode: upstream.StatusCode,
		UpstreamRequestID:  upstream.RequestID,
		Kind:               kind,
		Message:            upstreamMsg,
	})

	if shouldFailover || shouldDisable {
		return &UpstreamFailoverError{
			StatusCode:      upstream.StatusCode,
			ResponseBody:    body,
			ResponseHeaders: upstream.Headers,
		}
	}
	writeOpenCodeGoUpstreamResponse(c, upstream, s.responseHeaderFilter, format)
	return fmt.Errorf("commandcode upstream returned status %d", upstream.StatusCode)
}

func (s *CommandCodeGatewayService) bufferResponse(
	c *gin.Context,
	account *Account,
	resp *http.Response,
	pipeline *protocolconv.Pipeline,
	actualProtocol protocolconv.Protocol,
	sourceProtocol protocolconv.Protocol,
	originalModel string,
	upstreamModel string,
	startTime time.Time,
) (*ForwardResult, error) {
	body, err := ReadUpstreamResponseBody(resp.Body, s.cfg, c, nil)
	if err != nil {
		return nil, s.preCommitFailure(c, account, resp, "response_read_error", err)
	}
	usage := openCodeGoUsageFromBody(body, actualProtocol)
	structured := protocoltransport.Response{
		StatusCode:     resp.StatusCode,
		Headers:        responseheaders.FilterHeaders(resp.Header, s.responseHeaderFilter),
		Body:           body,
		ActualProtocol: actualProtocol,
		RequestID:      resp.Header.Get("x-request-id"),
		ResponseID:     extractOpenAIResponseIDFromJSONBytes(body),
		Duration:       time.Since(startTime),
	}
	if err := structured.Validate(); err != nil {
		return nil, s.preCommitFailure(c, account, resp, "invalid_response_transport", err)
	}
	converted, err := pipeline.ConvertResponse(structured.Body, structured.ActualProtocol)
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
	return openCodeGoForwardResult(resp, usage, originalModel, upstreamModel, actualProtocol, false, startTime), nil
}

var errCommandCodeEmptyStream = errors.New("commandcode upstream stream completed without output or usage")

type commandCodeStreamObservation struct {
	usageSeen  bool
	outputSeen bool
}

func (o *commandCodeStreamObservation) observe(payload []byte, actualProtocol protocolconv.Protocol) {
	if o == nil {
		return
	}
	if commandCodeStreamPayloadHasUsage(payload, actualProtocol) {
		o.usageSeen = true
	}
	if commandCodeStreamPayloadHasOutput(payload, actualProtocol) {
		o.outputSeen = true
	}
}

// 只有包含正的 token/图像 token 计数才算可用于记账的 usage；空对象和全零 usage 仍视为缺失。
func commandCodeStreamPayloadHasUsage(payload []byte, actualProtocol protocolconv.Protocol) bool {
	var usage gjson.Result
	switch actualProtocol {
	case protocolconv.ProtocolOpenAIChat:
		usage = gjson.GetBytes(payload, "usage")
	case protocolconv.ProtocolAnthropic:
		usage = gjson.GetBytes(payload, "usage")
		if !usage.Exists() {
			usage = gjson.GetBytes(payload, "message.usage")
		}
	default:
		return false
	}
	if !usage.Exists() || !usage.IsObject() {
		return false
	}
	for _, path := range []string{
		"input_tokens", "output_tokens", "prompt_tokens", "completion_tokens",
		"cache_read_input_tokens", "cache_creation_input_tokens", "total_tokens",
		"image_output_tokens", "input_tokens_details.image_tokens", "output_tokens_details.image_tokens",
	} {
		if usage.Get(path).Int() > 0 {
			return true
		}
	}
	return false
}

func commandCodeStreamPayloadHasOutput(payload []byte, actualProtocol protocolconv.Protocol) bool {
	payload = bytes.TrimSpace(payload)
	if len(payload) == 0 || bytes.Equal(payload, []byte("[DONE]")) {
		return false
	}
	switch actualProtocol {
	case protocolconv.ProtocolOpenAIChat:
		if gjson.GetBytes(payload, "error").Exists() {
			return false
		}
		var chunk apicompat.ChatCompletionsChunk
		if err := json.Unmarshal(payload, &chunk); err != nil {
			return false
		}
		return chatChunkStartsResponsesOutput(&chunk)
	case protocolconv.ProtocolAnthropic:
		eventType := strings.TrimSpace(gjson.GetBytes(payload, "type").String())
		switch eventType {
		case "content_block_start":
			contentType := strings.TrimSpace(gjson.GetBytes(payload, "content_block.type").String())
			return contentType == "tool_use" || contentType == "server_tool_use"
		case "content_block_delta":
			delta := gjson.GetBytes(payload, "delta")
			return strings.TrimSpace(delta.Get("text").String()) != "" ||
				strings.TrimSpace(delta.Get("thinking").String()) != "" ||
				strings.TrimSpace(delta.Get("partial_json").String()) != "" ||
				strings.TrimSpace(delta.Get("signature").String()) != ""
		default:
			return false
		}
	default:
		return false
	}
}

func commandCodeStreamRecordIsError(event string, payload []byte) bool {
	if strings.EqualFold(strings.TrimSpace(event), "error") {
		return true
	}
	eventType := strings.TrimSpace(gjson.GetBytes(payload, "type").String())
	return eventType == "error" || eventType == "response.failed" || gjson.GetBytes(payload, "error").Exists()
}

func commandCodeStreamErrorStatus(payload []byte) int {
	for _, path := range []string{"status_code", "error.status_code"} {
		status := int(gjson.GetBytes(payload, path).Int())
		if status >= http.StatusBadRequest {
			return status
		}
	}
	return http.StatusBadGateway
}

func commandCodeEmptyStreamBody() []byte {
	return []byte(`{"error":{"type":"upstream_error","code":"commandcode_empty_stream","message":"Command Code upstream returned an empty stream"}}`)
}

func recordCommandCodeStreamError(c *gin.Context, account *Account, resp *http.Response, status int, kind, message string) {
	if status < http.StatusBadRequest {
		status = http.StatusBadGateway
	}
	message = strings.TrimSpace(sanitizeUpstreamErrorMessage(message))
	if message == "" {
		message = "Command Code upstream stream failed"
	}
	setOpsUpstreamError(c, status, message, "")
	event := OpsUpstreamErrorEvent{
		ProxyID:            opsUpstreamProxyID(account),
		ProxyName:          opsUpstreamProxyName(account),
		Platform:           PlatformCommandCode,
		Kind:               kind,
		UpstreamStatusCode: status,
		Message:            message,
	}
	if account != nil {
		event.AccountID = account.ID
		event.AccountName = account.Name
	}
	if resp != nil {
		event.UpstreamRequestID = strings.TrimSpace(resp.Header.Get("x-request-id"))
	}
	appendOpsUpstreamError(c, event)
}

func newCommandCodeStreamFailoverError(c *gin.Context, account *Account, resp *http.Response, body []byte, kind, fallbackMessage string) *UpstreamFailoverError {
	if len(body) == 0 {
		body = commandCodeEmptyStreamBody()
	}
	status := commandCodeStreamErrorStatus(body)
	message := strings.TrimSpace(ExtractUpstreamErrorMessage(body))
	if message == "" {
		message = fallbackMessage
	}
	recordCommandCodeStreamError(c, account, resp, status, kind, message)
	return &UpstreamFailoverError{
		StatusCode:   status,
		ResponseBody: body,
		ResponseHeaders: func() http.Header {
			if resp == nil {
				return nil
			}
			return protocoltransport.CloneHeaders(resp.Header)
		}(),
		Scope:  GatewayFailureScopeProvider,
		Reason: GatewayFailureReason("commandcode_stream_error"),
	}
}

func (s *CommandCodeGatewayService) streamResponse(
	c *gin.Context,
	account *Account,
	resp *http.Response,
	pipeline *protocolconv.Pipeline,
	actualProtocol protocolconv.Protocol,
	sourceProtocol protocolconv.Protocol,
	originalModel string,
	upstreamModel string,
	startTime time.Time,
) (*ForwardResult, error) {
	usage := ClaudeUsage{}
	observation := &commandCodeStreamObservation{}
	var firstTokenMs *int
	observe := func(payload []byte) {
		observation.observe(payload, actualProtocol)
		if firstTokenMs == nil && observation.outputSeen {
			ms := int(time.Since(startTime).Milliseconds())
			firstTokenMs = &ms
		}
		mergeOpenCodeGoStreamUsage(&usage, payload, actualProtocol)
	}
	clientDisconnected, err := s.convertStream(c, resp, pipeline, actualProtocol, sourceProtocol, observe, observation)
	out := openCodeGoForwardResult(resp, usage, originalModel, upstreamModel, actualProtocol, true, startTime)
	out.ClientDisconnect = clientDisconnected
	out.FirstTokenMs = firstTokenMs
	if errors.Is(err, errCommandCodeEmptyStream) {
		err = newCommandCodeStreamFailoverError(c, account, resp, nil, "empty_stream", "Command Code upstream returned an empty stream")
	} else if err != nil {
		var streamErr *sseStreamErrorEventError
		if errors.As(err, &streamErr) {
			err = newCommandCodeStreamFailoverError(c, account, resp, []byte(streamErr.RawData), "stream_error", "Command Code upstream stream failed")
		}
	}
	if err == nil && !observation.usageSeen {
		const message = "Command Code upstream stream omitted usage"
		out.UsageUnavailable = true
		recordCommandCodeStreamError(c, account, resp, http.StatusBadGateway, "missing_usage", message)
		MarkOpsStreamError(c, "upstream_error", message, http.StatusBadGateway)
	}
	return out, err
}

// convertStream 把上游 SSE 流转换为客户端协议流。尚未确认有效输出前暂存结构化事件，
// 防止上游空流先提交 200 并被当作成功请求记账。
func (s *CommandCodeGatewayService) convertStream(
	c *gin.Context,
	resp *http.Response,
	pipeline *protocolconv.Pipeline,
	actualProtocol protocolconv.Protocol,
	sourceProtocol protocolconv.Protocol,
	observe func([]byte),
	observation *commandCodeStreamObservation,
) (bool, error) {
	maxRecordSize := defaultMaxLineSize
	if s.cfg != nil && s.cfg.Gateway.MaxLineSize > 0 {
		maxRecordSize = s.cfg.Gateway.MaxLineSize
	}
	filteredHeaders := make(http.Header)
	if s.responseHeaderFilter != nil {
		filteredHeaders = responseheaders.FilterHeaders(resp.Header, s.responseHeaderFilter)
	}
	stream := &protocoltransport.Stream{
		StatusCode:     resp.StatusCode,
		Headers:        filteredHeaders,
		ActualProtocol: actualProtocol,
		RequestID:      resp.Header.Get("x-request-id"),
		Events:         protocoltransport.NewSSEParser(resp.Body, maxRecordSize),
	}
	if err := stream.Validate(); err != nil {
		_ = stream.Close()
		return false, err
	}
	defer func() { _ = stream.Close() }()
	session, err := pipeline.NewStreamProcessor(actualProtocol)
	if err != nil {
		return false, err
	}
	renderer, err := protocolconv.NewRenderer(sourceProtocol)
	if err != nil {
		return false, err
	}
	headersWritten := false
	clientDisconnected := false
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
	writeFramedPayloads := func(payloads [][]byte) error {
		if clientDisconnected || len(payloads) == 0 {
			return nil
		}
		if err := ensureHeaders(); err != nil {
			return err
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
	pendingPayloads := make([][]byte, 0, 4)
	writePayloads := func(payloads [][]byte) error {
		if clientDisconnected {
			return nil
		}
		if observation != nil && !observation.outputSeen && !observation.usageSeen {
			for _, payload := range payloads {
				if len(payload) > 0 {
					pendingPayloads = append(pendingPayloads, append([]byte(nil), payload...))
				}
			}
			return nil
		}
		if len(pendingPayloads) > 0 {
			if err := writeFramedPayloads(pendingPayloads); err != nil {
				return err
			}
			pendingPayloads = pendingPayloads[:0]
		}
		return writeFramedPayloads(payloads)
	}

	identityStream := actualProtocol == sourceProtocol
	identityTerminal := false
	for {
		var record protocoltransport.SSERecord
		var nextErr error
		if rawParser, ok := stream.Events.(interface {
			NextRecord(context.Context) (protocoltransport.SSERecord, error)
		}); ok {
			record, nextErr = rawParser.NextRecord(context.Background())
		} else {
			record, nextErr = stream.Events.Next(context.Background())
		}
		if errors.Is(nextErr, protocoltransport.ErrSSEDone) {
			if identityStream && actualProtocol == protocolconv.ProtocolOpenAIChat {
				identityTerminal = true
			}
			break
		}
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			return clientDisconnected, nextErr
		}
		if commandCodeStreamRecordIsError(string(record.Event), record.Data) {
			return clientDisconnected, &sseStreamErrorEventError{RawData: string(record.Data)}
		}
		if len(record.Data) == 0 {
			continue
		}
		if !json.Valid(record.Data) {
			return clientDisconnected, errors.New("malformed SSE JSON payload")
		}
		if observe != nil {
			observe(record.Data)
		}
		if identityStream && actualProtocol == protocolconv.ProtocolAnthropic &&
			strings.TrimSpace(gjson.GetBytes(record.Data, "type").String()) == "message_stop" {
			identityTerminal = true
		}
		payloads, _, convertErr := session.Convert(record.Data)
		if convertErr != nil {
			return clientDisconnected, convertErr
		}
		if err := writePayloads(payloads); err != nil {
			return clientDisconnected, err
		}
	}
	if identityStream && !identityTerminal {
		return clientDisconnected, fmt.Errorf("commandcode %s stream ended without terminal event", actualProtocol)
	}
	if observation != nil && !observation.outputSeen && !observation.usageSeen && !clientDisconnected {
		return clientDisconnected, errCommandCodeEmptyStream
	}
	if err := writePayloads(nil); err != nil {
		return clientDisconnected, err
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

func shouldFailoverCommandCodeResponse(status int) bool {
	if status == http.StatusUnauthorized || status == http.StatusForbidden || status == http.StatusTooManyRequests {
		return true
	}
	return status >= http.StatusInternalServerError
}

func (s *CommandCodeGatewayService) preCommitFailure(c *gin.Context, account *Account, resp *http.Response, kind string, err error) error {
	message := "invalid Command Code upstream response"
	if err != nil {
		message = sanitizeUpstreamErrorMessage(err.Error())
	}
	setOpsUpstreamError(c, http.StatusBadGateway, message, "")
	event := OpsUpstreamErrorEvent{
		ProxyID: opsUpstreamProxyID(account), ProxyName: opsUpstreamProxyName(account),
		Platform: PlatformCommandCode, Kind: kind,
		UpstreamStatusCode: http.StatusBadGateway, Message: message,
	}
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

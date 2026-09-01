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
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv"
	protocoltransport "github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv/transport"
	"github.com/Wei-Shaw/sub2api/internal/util/responseheaders"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

// ForwardAsResponses accepts an OpenAI Responses API request body, converts it
// to Anthropic Messages format, forwards to the Anthropic upstream, and converts
// the response back to Responses format. This enables OpenAI Responses API
// clients to access Anthropic models through Anthropic platform groups.
//
// The method follows the same pattern as OpenAIGatewayService.ForwardAsAnthropic
// but in reverse direction: Responses → Anthropic upstream → Responses.
func (s *GatewayService) ForwardAsResponses(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	body []byte,
	parsed *ParsedRequest,
) (*ForwardResult, error) {
	startTime := time.Now()

	// 1. Lower Codex client-side tools to function tools understood by Anthropic.
	adaptedBody, clientToolMapping, err := adaptResponsesClientToolsForAnthropic(body)
	if err != nil {
		return nil, fmt.Errorf("adapt responses client tools: %w", err)
	}

	// 2. Parse Responses request
	var responsesReq apicompat.ResponsesRequest
	if err := json.Unmarshal(adaptedBody, &responsesReq); err != nil {
		return nil, fmt.Errorf("parse responses request: %w", err)
	}
	originalModel := responsesReq.Model
	clientStream := responsesReq.Stream

	// 3. Resolve the upstream model before creating the request-scoped route.
	reasoningEffort := ExtractResponsesReasoningEffortFromBody(body)
	mappedModel, err := resolveStandardAnthropicTargetModel(account, originalModel)
	if err != nil {
		return nil, err
	}
	// 国产模型默认 effort 补充：需要 mappedModel 判定，推迟到 mapping 完成之后。
	reasoningEffort = ApplyThinkingEnabledFallback(reasoningEffort, body, mappedModel)

	// 3. Convert Responses → Anthropic with one pipeline retained for the
	// successful response or stream from this upstream attempt.
	pipeline, err := protocolconv.NewPipeline(standardProtocolRegistry, protocolconv.PipelineConfig{
		Route: protocolconv.Route{
			Source:         protocolconv.ProtocolOpenAIResponses,
			IntendedTarget: protocolconv.ProtocolAnthropic,
			ClientModel:    originalModel,
			UpstreamModel:  mappedModel,
			Provider:       account.Platform,
			AccountID:      account.ID,
		},
		Options: protocolconv.Options{SourceModel: mappedModel, LossPolicy: protocolconv.LossError},
	})
	if err != nil {
		return nil, fmt.Errorf("create responses anthropic pipeline: %w", err)
	}
	convertedRequest, err := pipeline.ConvertRequest(adaptedBody)
	if err != nil {
		return nil, fmt.Errorf("convert responses to anthropic: %w", err)
	}
	var anthropicReq apicompat.AnthropicRequest
	if err := json.Unmarshal(convertedRequest.Body, &anthropicReq); err != nil {
		return nil, fmt.Errorf("decode converted anthropic request: %w", err)
	}
	anthropicReq.Model = mappedModel
	anthropicReq.Stream = true

	logger.L().Debug("gateway forward_as_responses: model mapping applied",
		zap.Int64("account_id", account.ID),
		zap.String("original_model", originalModel),
		zap.String("mapped_model", mappedModel),
		zap.Bool("client_stream", clientStream),
	)

	// 4. Marshal mapped Anthropic request body
	anthropicBody, err := json.Marshal(anthropicReq)
	if err != nil {
		return nil, fmt.Errorf("marshal anthropic request: %w", err)
	}

	resp, err := s.forwardStandardProtocolToAnthropic(ctx, c, account, anthropicBody, anthropicReq.System, mappedModel, func(statusCode int, errorType, message string) {
		writeResponsesError(c, statusCode, errorType, message)
	})
	if err != nil {
		return nil, err
	}

	// 13. Handle normal response (convert Anthropic → Responses)
	var result *ForwardResult
	var handleErr error
	if clientStream {
		result, handleErr = s.handleResponsesStreamingResponse(resp, c, pipeline, originalModel, mappedModel, reasoningEffort, startTime, clientToolMapping)
	} else {
		result, handleErr = s.handleResponsesBufferedStreamingResponse(resp, c, pipeline, originalModel, mappedModel, reasoningEffort, startTime, clientToolMapping)
	}
	if account.IsBedrock() {
		return s.handleStandardBedrockStreamError(ctx, c, account, mappedModel, result, handleErr)
	}
	return result, handleErr
}

func adaptResponsesClientToolsForAnthropic(body []byte) ([]byte, apicompat.ResponsesClientToolMapping, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var requestBody map[string]any
	if err := decoder.Decode(&requestBody); err != nil {
		return body, apicompat.ResponsesClientToolMapping{}, err
	}
	additionalToolsChanged, err := liftResponsesAdditionalTools(requestBody)
	if err != nil {
		return body, apicompat.ResponsesClientToolMapping{}, err
	}

	mapping, changed, err := apicompat.AdaptResponsesClientTools(requestBody)
	if err != nil {
		return body, apicompat.ResponsesClientToolMapping{}, err
	}
	changed = changed || additionalToolsChanged
	if !changed {
		return body, mapping, nil
	}
	rebuilt, err := json.Marshal(requestBody)
	if err != nil {
		return body, apicompat.ResponsesClientToolMapping{}, err
	}
	return rebuilt, mapping, nil
}

func liftResponsesAdditionalTools(requestBody map[string]any) (bool, error) {
	input, ok := requestBody["input"].([]any)
	if !ok {
		return false, nil
	}

	tools, _ := requestBody["tools"].([]any)
	kept := make([]any, 0, len(input))
	changed := false
	for _, raw := range input {
		item, ok := raw.(map[string]any)
		if !ok || strings.TrimSpace(fmt.Sprint(item["type"])) != "additional_tools" {
			kept = append(kept, raw)
			continue
		}
		additional, ok := item["tools"].([]any)
		if !ok {
			return false, fmt.Errorf("additional_tools.tools must be an array")
		}
		tools = append(tools, additional...)
		changed = true
	}
	if !changed {
		return false, nil
	}
	requestBody["tools"] = tools
	requestBody["input"] = kept
	return true, nil
}

// ExtractResponsesReasoningEffortFromBody reads Responses API reasoning.effort
// and normalizes it for usage logging.
func ExtractResponsesReasoningEffortFromBody(body []byte) *string {
	raw := strings.TrimSpace(gjson.GetBytes(body, "reasoning.effort").String())
	if raw == "" {
		return nil
	}
	normalized := normalizeOpenAIReasoningEffort(raw)
	if normalized == "" {
		return nil
	}
	return &normalized
}

func mergeAnthropicUsage(dst *ClaudeUsage, src apicompat.AnthropicUsage) {
	if dst == nil {
		return
	}
	if src.InputTokens > 0 {
		dst.InputTokens = src.InputTokens
	}
	if src.OutputTokens > 0 {
		dst.OutputTokens = src.OutputTokens
	}
	if src.CacheReadInputTokens > 0 {
		dst.CacheReadInputTokens = src.CacheReadInputTokens
	}
	if src.CacheCreationInputTokens > 0 {
		dst.CacheCreationInputTokens = src.CacheCreationInputTokens
	}
}

// parseAnthropicSSEField parses an SSE field line in the form "field:value" or "field: value".
// According to the SSE spec (https://html.spec.whatwg.org/multipage/server-sent-events.html#event-stream-interpretation),
// the space after the colon is optional. This function handles both formats.
func parseAnthropicSSEField(line, field string) (string, bool) {
	prefix := field + ":"
	if !strings.HasPrefix(line, prefix) {
		return "", false
	}
	return strings.TrimSpace(strings.TrimPrefix(line, prefix)), true
}

// handleResponsesBufferedStreamingResponse reads all Anthropic SSE events from
// the upstream streaming response, assembles them into a complete Anthropic
// response, converts to Responses API JSON format, and writes it to the client.
func (s *GatewayService) handleResponsesBufferedStreamingResponse(
	resp *http.Response,
	c *gin.Context,
	pipeline *protocolconv.Pipeline,
	originalModel string,
	mappedModel string,
	reasoningEffort *string,
	startTime time.Time,
	clientToolMapping apicompat.ResponsesClientToolMapping,
) (*ForwardResult, error) {
	stream, err := s.collectAnthropicProtocolStream(resp, startTime)
	if err != nil {
		return nil, err
	}
	defer func() { _ = stream.Close() }()
	requestID := stream.RequestID
	session, err := pipeline.NewStreamProcessor(stream.ActualProtocol)
	if err != nil {
		return nil, fmt.Errorf("create buffered responses stream processor: %w", err)
	}
	var usage ClaudeUsage
	var finalBody []byte

	for {
		record, nextErr := stream.Events.Next(context.Background())
		if errors.Is(nextErr, io.EOF) || errors.Is(nextErr, protocoltransport.ErrSSEDone) {
			break
		}
		if nextErr != nil {
			return nil, fmt.Errorf("read anthropic stream: %w", nextErr)
		}
		var event apicompat.AnthropicStreamEvent
		if err := json.Unmarshal(record.Data, &event); err != nil {
			return nil, fmt.Errorf("decode anthropic stream event: %w", err)
		}
		if event.Type == "message_start" && event.Message != nil {
			mergeAnthropicUsage(&usage, event.Message.Usage)
		}
		if event.Type == "message_delta" && event.Usage != nil {
			mergeAnthropicUsage(&usage, *event.Usage)
		}
		converted, _, err := session.Convert(record.Data)
		if err != nil {
			return nil, fmt.Errorf("convert anthropic stream event: %w", err)
		}
		if terminal := responsesTerminalBody(converted); len(terminal) > 0 {
			finalBody = terminal
		}
		if event.Type == "message_stop" {
			break
		}
	}
	converted, _, err := session.Finalize()
	if err != nil {
		return nil, fmt.Errorf("finalize anthropic stream: %w", err)
	}
	if terminal := responsesTerminalBody(converted); len(terminal) > 0 {
		finalBody = terminal
	}
	if len(finalBody) == 0 {
		writeResponsesError(c, http.StatusBadGateway, "server_error", "Upstream stream ended without a response")
		return nil, fmt.Errorf("upstream stream ended without response")
	}
	finalBody = reverseToolNamesIfPresent(c, finalBody)
	finalBody, _, err = apicompat.RestoreResponsesClientToolPayload(finalBody, clientToolMapping)
	if err != nil {
		return nil, fmt.Errorf("restore responses client tools: %w", err)
	}
	renderer, err := protocolconv.NewRenderer(protocolconv.ProtocolOpenAIResponses)
	if err != nil {
		return nil, err
	}
	if err := renderer.RenderJSON(c.Writer, stream.StatusCode, stream.Headers, finalBody); err != nil {
		return nil, fmt.Errorf("render responses response: %w", err)
	}

	return &ForwardResult{
		RequestID:       requestID,
		ActualProtocol:  stream.ActualProtocol,
		Usage:           usage,
		Model:           originalModel,
		UpstreamModel:   mappedModel,
		ReasoningEffort: reasoningEffort,
		Stream:          false,
		Duration:        time.Since(startTime),
	}, nil
}

// handleResponsesStreamingResponse reads Anthropic SSE events from upstream,
// converts each to Responses SSE events, and writes them to the client.
func (s *GatewayService) handleResponsesStreamingResponse(
	resp *http.Response,
	c *gin.Context,
	pipeline *protocolconv.Pipeline,
	originalModel string,
	mappedModel string,
	reasoningEffort *string,
	startTime time.Time,
	clientToolMapping apicompat.ResponsesClientToolMapping,
) (*ForwardResult, error) {
	stream, err := s.collectAnthropicProtocolStream(resp, startTime)
	if err != nil {
		return nil, err
	}
	requestID := stream.RequestID
	defer func() { _ = stream.Close() }()

	session, err := pipeline.NewStreamProcessor(stream.ActualProtocol)
	if err != nil {
		return nil, err
	}
	renderer, err := protocolconv.NewRenderer(protocolconv.ProtocolOpenAIResponses)
	if err != nil {
		return nil, err
	}
	clientToolRestorer := apicompat.NewResponsesClientToolStreamRestorer(clientToolMapping)
	var usage ClaudeUsage
	var firstTokenMs *int
	headersWritten := false
	clientDisconnected := false

	resultWithUsage := func() *ForwardResult {
		return &ForwardResult{
			RequestID:        requestID,
			ActualProtocol:   stream.ActualProtocol,
			Usage:            usage,
			Model:            originalModel,
			UpstreamModel:    mappedModel,
			ReasoningEffort:  reasoningEffort,
			Stream:           true,
			Duration:         time.Since(startTime),
			FirstTokenMs:     firstTokenMs,
			ClientDisconnect: clientDisconnected,
		}
	}
	writePayloads := func(payloads [][]byte) error {
		if len(payloads) == 0 || clientDisconnected {
			return nil
		}
		if !headersWritten {
			if err := renderer.WriteStreamHeaders(c.Writer, stream.StatusCode, stream.Headers); err != nil {
				return err
			}
			headersWritten = true
		}
		for _, body := range payloads {
			body = reverseToolNamesIfPresent(c, body)
			restoredBodies, _, err := clientToolRestorer.RestoreEvent(body)
			if err != nil {
				return fmt.Errorf("restore responses client tool stream event: %w", err)
			}
			for _, restoredBody := range restoredBodies {
				framed, err := renderer.FrameStreamEvent(restoredBody)
				if err != nil {
					return err
				}
				if _, err := c.Writer.Write(framed); err != nil {
					clientDisconnected = true
					logger.L().Info("forward_as_responses stream: client disconnected, continuing to drain upstream for billing", zap.String("request_id", requestID))
					return nil
				}
			}
		}
		c.Writer.Flush()
		return nil
	}

	for {
		record, err := stream.Events.Next(context.Background())
		if errors.Is(err, io.EOF) || errors.Is(err, protocoltransport.ErrSSEDone) {
			break
		}
		if err != nil {
			return resultWithUsage(), fmt.Errorf("read anthropic stream: %w", err)
		}
		var event apicompat.AnthropicStreamEvent
		if err := json.Unmarshal(record.Data, &event); err != nil {
			return resultWithUsage(), fmt.Errorf("decode anthropic stream event: %w", err)
		}
		if firstTokenMs == nil {
			ms := int(time.Since(startTime).Milliseconds())
			firstTokenMs = &ms
		}
		if event.Type == "message_delta" && event.Usage != nil {
			mergeAnthropicUsage(&usage, *event.Usage)
		}
		if event.Type == "message_start" && event.Message != nil {
			mergeAnthropicUsage(&usage, event.Message.Usage)
		}
		converted, _, err := session.Convert(record.Data)
		if err != nil {
			return resultWithUsage(), fmt.Errorf("convert anthropic stream event: %w", err)
		}
		if err := writePayloads(converted); err != nil {
			return resultWithUsage(), err
		}
		if event.Type == "message_stop" {
			break
		}
	}

	converted, _, err := session.Finalize()
	if err != nil {
		return resultWithUsage(), err
	}
	if err := writePayloads(converted); err != nil {
		return resultWithUsage(), err
	}
	if !headersWritten && !clientDisconnected {
		if err := renderer.WriteStreamHeaders(c.Writer, stream.StatusCode, stream.Headers); err != nil {
			return resultWithUsage(), err
		}
	}
	if !clientDisconnected {
		if terminal := renderer.StreamTerminal(); len(terminal) > 0 {
			_, _ = c.Writer.Write(terminal)
		}
		c.Writer.Flush()
	}
	return resultWithUsage(), nil
}

func (s *GatewayService) collectAnthropicProtocolStream(resp *http.Response, startTime time.Time) (*protocoltransport.Stream, error) {
	if resp == nil || resp.Body == nil {
		return nil, errors.New("nil anthropic upstream response")
	}
	maxRecordSize := defaultMaxLineSize
	if s.cfg != nil && s.cfg.Gateway.MaxLineSize > 0 {
		maxRecordSize = s.cfg.Gateway.MaxLineSize
	}
	stream := &protocoltransport.Stream{
		StatusCode:     resp.StatusCode,
		Headers:        responseheaders.FilterHeaders(resp.Header, s.responseHeaderFilter),
		ActualProtocol: protocolconv.ProtocolAnthropic,
		RequestID:      resp.Header.Get("x-request-id"),
		Duration:       time.Since(startTime),
		Events:         protocoltransport.NewSSEParser(resp.Body, maxRecordSize),
	}
	if err := stream.Validate(); err != nil {
		_ = stream.Close()
		return nil, fmt.Errorf("collect anthropic stream: %w", err)
	}
	return stream, nil
}

func responsesTerminalBody(payloads [][]byte) []byte {
	for index := len(payloads) - 1; index >= 0; index-- {
		var event apicompat.ResponsesStreamEvent
		if json.Unmarshal(payloads[index], &event) != nil || event.Response == nil {
			continue
		}
		if event.Type != "response.completed" && event.Type != "response.done" {
			continue
		}
		body, err := json.Marshal(event.Response)
		if err == nil {
			return body
		}
	}
	return nil
}

// appendRawJSON appends a JSON fragment string to existing raw JSON.
func appendRawJSON(existing json.RawMessage, fragment string) json.RawMessage {
	trimmed := bytes.TrimSpace(existing)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte(`{}`)) {
		return json.RawMessage(fragment)
	}
	return json.RawMessage(string(existing) + fragment)
}

// writeResponsesError writes an error response in OpenAI Responses API format.
func writeResponsesError(c *gin.Context, statusCode int, code, message string) {
	MarkResponseCommitted(c)
	c.JSON(statusCode, gin.H{
		"error": gin.H{
			"code":    code,
			"message": message,
		},
	})
}

// mapUpstreamStatusCode maps upstream HTTP status codes to appropriate client-facing codes.
func mapUpstreamStatusCode(code int) int {
	if code >= 500 {
		return http.StatusBadGateway
	}
	return code
}

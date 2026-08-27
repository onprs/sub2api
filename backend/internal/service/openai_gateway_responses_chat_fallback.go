package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv"
	protocoltransport "github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv/transport"
	"github.com/Wei-Shaw/sub2api/internal/util/responseheaders"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// forwardResponsesViaRawChatCompletions serves /v1/responses clients through an
// upstream that only supports /v1/chat/completions.
func (s *OpenAIGatewayService) forwardResponsesViaRawChatCompletionsWithOutput(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	body []byte,
	output openAIProtocolOutput,
) (*OpenAIForwardResult, error) {
	startTime := time.Now()

	var responsesReq apicompat.ResponsesRequest
	if err := json.Unmarshal(body, &responsesReq); err != nil {
		writeOpenAIResponsesFallbackError(c, http.StatusBadRequest, "invalid_request_error", "Failed to parse request body")
		return nil, fmt.Errorf("parse responses request: %w", err)
	}
	originalModel := strings.TrimSpace(responsesReq.Model)
	if originalModel == "" {
		writeOpenAIResponsesFallbackError(c, http.StatusBadRequest, "invalid_request_error", "model is required")
		return nil, fmt.Errorf("missing model in request")
	}

	clientStream := responsesReq.Stream
	serviceTier := extractOpenAIServiceTierFromBody(body)
	billingModel := resolveOpenAIForwardModel(account, originalModel, "")
	upstreamModel := normalizeOpenAIModelForUpstream(account, billingModel)
	pipeline, err := protocolconv.NewPipeline(standardProtocolRegistry, protocolconv.PipelineConfig{
		Route: protocolconv.Route{
			Source:         protocolconv.ProtocolOpenAIResponses,
			IntendedTarget: protocolconv.ProtocolOpenAIChat,
			ClientModel:    originalModel,
			UpstreamModel:  upstreamModel,
			Provider:       account.Platform,
			AccountID:      account.ID,
		},
		Options: protocolconv.Options{SourceModel: upstreamModel, LossPolicy: protocolconv.LossError},
	})
	if err != nil {
		return nil, fmt.Errorf("create responses chat fallback pipeline: %w", err)
	}
	convertedRequest, err := pipeline.ConvertRequest(body)
	if err != nil {
		writeOpenAIResponsesFallbackError(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return nil, fmt.Errorf("convert responses to chat completions: %w", err)
	}
	var chatReq apicompat.ChatCompletionsRequest
	if err := json.Unmarshal(convertedRequest.Body, &chatReq); err != nil {
		return nil, fmt.Errorf("decode converted chat completions request: %w", err)
	}
	reasoningEffort := extractOpenAIReasoningEffortFromBody(body, upstreamModel, billingModel, originalModel)
	// 国产模型默认 effort 补充：需要 mappedModel 判定，推迟到 billingModel 算出之后。
	reasoningEffort = ApplyThinkingEnabledFallback(reasoningEffort, body, billingModel)
	chatReq.Model = upstreamModel
	if clientStream {
		chatReq.StreamOptions = &apicompat.ChatStreamOptions{IncludeUsage: true}
	}

	chatBody, err := json.Marshal(chatReq)
	if err != nil {
		return nil, fmt.Errorf("marshal chat completions fallback request: %w", err)
	}
	chatBody, err = s.applyOpenAIFastPolicyToBody(ctx, account, upstreamModel, chatBody)
	if err != nil {
		var blocked *OpenAIFastBlockedError
		if errors.As(err, &blocked) {
			writeOpenAIFastPolicyBlockedResponse(c, blocked)
		}
		return nil, err
	}
	if serviceTier == nil {
		serviceTier = extractOpenAIServiceTierFromBody(chatBody)
	}

	logger.L().Debug("openai responses: forwarding via raw chat completions",
		zap.Int64("account_id", account.ID),
		zap.String("original_model", originalModel),
		zap.String("billing_model", billingModel),
		zap.String("upstream_model", upstreamModel),
		zap.Bool("stream", clientStream),
	)

	// Build and send upstream request via the shared CC pipeline
	apiKey, targetURL, err := s.resolveCCFallbackTarget(account)
	if err != nil {
		return nil, err
	}
	resp, err := s.sendCCUpstreamRequest(ctx, c, account, targetURL, chatBody, clientStream, apiKey, account.GetOpenAIUserAgent(), "")
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		defer func() { _ = resp.Body.Close() }()
		upstream, upstreamMsg, collectErr := s.collectOpenAIStructuredUpstreamError(resp, protocolconv.ProtocolOpenAIChat, clientStream)
		if collectErr != nil {
			return nil, collectErr
		}
		if foErr := s.failoverOpenAIStructuredUpstreamError(ctx, c, account, upstream, upstreamMsg, upstreamModel); foErr != nil {
			return nil, foErr
		}
		return s.handleStructuredErrorResponse(ctx, upstream, c, account, chatBody, billingModel)
	}

	if clientStream {
		return s.streamChatCompletionsAsResponses(c, resp, pipeline, originalModel, billingModel, upstreamModel, reasoningEffort, serviceTier, startTime, output)
	}
	defer func() { _ = resp.Body.Close() }()
	return s.bufferChatCompletionsAsResponses(c, resp, pipeline, originalModel, billingModel, upstreamModel, reasoningEffort, serviceTier, startTime, output)
}

func (s *OpenAIGatewayService) bufferChatCompletionsAsResponses(
	c *gin.Context,
	resp *http.Response,
	pipeline *protocolconv.Pipeline,
	originalModel string,
	billingModel string,
	upstreamModel string,
	reasoningEffort *string,
	serviceTier *string,
	startTime time.Time,
	output openAIProtocolOutput,
) (*OpenAIForwardResult, error) {
	_, usage, upstreamBody, err := s.readCCUpstreamJSONResult(c, resp, writeOpenAIResponsesFallbackError)
	if err != nil {
		return nil, err
	}

	structured, result, err := s.collectBufferedChatCompletionsResponse(
		resp.StatusCode,
		resp.Header,
		upstreamBody,
		usage,
		originalModel,
		billingModel,
		upstreamModel,
		reasoningEffort,
		serviceTier,
		startTime,
	)
	if err != nil {
		writeOpenAIResponsesFallbackError(c, http.StatusBadGateway, "api_error", "Failed to collect upstream response")
		return nil, err
	}
	if output != nil {
		if err := output.WriteResponse(structured); err != nil {
			return nil, fmt.Errorf("render structured Chat Completions result: %w", err)
		}
		return result, nil
	}
	convertedResponse, err := pipeline.ConvertResponse(structured.Body, structured.ActualProtocol)
	if err != nil {
		writeOpenAIResponsesFallbackError(c, http.StatusBadGateway, "api_error", "Failed to convert upstream response")
		return nil, fmt.Errorf("convert responses fallback response: %w", err)
	}
	downstreamBody := convertedResponse.Body
	renderer, err := protocolconv.NewRenderer(protocolconv.ProtocolOpenAIResponses)
	if err != nil {
		return nil, err
	}
	if err := renderer.RenderJSON(c.Writer, http.StatusOK, structured.Headers, downstreamBody); err != nil {
		return nil, fmt.Errorf("render responses fallback result: %w", err)
	}
	return result, nil
}

func (s *OpenAIGatewayService) collectBufferedChatCompletionsResponse(
	statusCode int,
	headers http.Header,
	upstreamBody []byte,
	usage OpenAIUsage,
	originalModel string,
	billingModel string,
	upstreamModel string,
	reasoningEffort *string,
	serviceTier *string,
	startTime time.Time,
) (protocoltransport.Response, *OpenAIForwardResult, error) {
	var ccResp apicompat.ChatCompletionsResponse
	if err := json.Unmarshal(upstreamBody, &ccResp); err != nil {
		return protocoltransport.Response{}, nil, fmt.Errorf("parse buffered chat completions response: %w", err)
	}
	filteredHeaders := make(http.Header)
	if s.responseHeaderFilter != nil {
		filteredHeaders = responseheaders.FilterHeaders(headers, s.responseHeaderFilter)
	}
	structured := protocoltransport.Response{
		StatusCode:     statusCode,
		Headers:        filteredHeaders,
		Body:           append([]byte(nil), upstreamBody...),
		ActualProtocol: protocolconv.ProtocolOpenAIChat,
		RequestID:      headers.Get("x-request-id"),
		ResponseID:     ccResp.ID,
		Duration:       time.Since(startTime),
	}
	if err := structured.Validate(); err != nil {
		return protocoltransport.Response{}, nil, fmt.Errorf("validate responses fallback result: %w", err)
	}
	result := &OpenAIForwardResult{
		RequestID:       structured.RequestID,
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

func (s *OpenAIGatewayService) streamChatCompletionsAsResponses(
	c *gin.Context,
	resp *http.Response,
	pipeline *protocolconv.Pipeline,
	originalModel string,
	billingModel string,
	upstreamModel string,
	reasoningEffort *string,
	serviceTier *string,
	startTime time.Time,
	output openAIProtocolOutput,
) (*OpenAIForwardResult, error) {
	stream, err := s.collectCCUpstreamStream(resp, startTime)
	if err != nil {
		return nil, err
	}
	defer func() { _ = stream.Close() }()
	requestID := stream.RequestID
	if output != nil {
		return s.streamChatCompletionsWithProtocolOutput(stream, output, originalModel, billingModel, upstreamModel, reasoningEffort, serviceTier, startTime)
	}
	session, err := pipeline.NewStreamProcessor(stream.ActualProtocol)
	if err != nil {
		return nil, fmt.Errorf("create responses fallback stream processor: %w", err)
	}
	renderer, err := protocolconv.NewRenderer(protocolconv.ProtocolOpenAIResponses)
	if err != nil {
		return nil, err
	}

	headersWritten := false
	clientDisconnected := false
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
				logger.L().Debug("openai responses chat fallback: client disconnected, continuing to drain upstream for billing",
					zap.Error(err),
					zap.String("request_id", requestID),
				)
				return nil
			}
		}
		c.Writer.Flush()
		return nil
	}

	scan := s.scanCCStream(stream, "openai responses chat fallback", startTime, func(raw []byte, _ *apicompat.ChatCompletionsChunk) error {
		payloads, _, err := session.Convert(raw)
		if err != nil {
			return fmt.Errorf("convert Chat Completions stream event: %w", err)
		}
		return writePayloads(payloads)
	})
	result := &OpenAIForwardResult{
		RequestID:       requestID,
		ResponseID:      scan.ResponseID,
		ActualProtocol:  stream.ActualProtocol,
		Usage:           scan.Usage,
		Model:           originalModel,
		BillingModel:    billingModel,
		UpstreamModel:   upstreamModel,
		ReasoningEffort: reasoningEffort,
		ServiceTier:     serviceTier,
		Stream:          true,
		Duration:        time.Since(startTime),
		FirstTokenMs:    scan.FirstTokenMs,
	}
	if scan.Err != nil {
		return result, fmt.Errorf("stream usage incomplete: %w", scan.Err)
	}

	finalPayloads, _, err := session.Finalize()
	if err != nil {
		return result, fmt.Errorf("finalize responses fallback stream: %w", err)
	}
	if err := writePayloads(finalPayloads); err != nil {
		return result, fmt.Errorf("render responses fallback stream: %w", err)
	}
	if !clientDisconnected {
		if !headersWritten {
			if err := renderer.WriteStreamHeaders(c.Writer, stream.StatusCode, stream.Headers); err != nil {
				return result, err
			}
			headersWritten = true
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
	if !scan.SawDone {
		logCCStreamMissingDoneSentinel("openai responses chat fallback", requestID)
	}
	return result, nil
}

func (s *OpenAIGatewayService) streamChatCompletionsWithProtocolOutput(
	stream *protocoltransport.Stream,
	output openAIProtocolOutput,
	originalModel string,
	billingModel string,
	upstreamModel string,
	reasoningEffort *string,
	serviceTier *string,
	startTime time.Time,
) (*OpenAIForwardResult, error) {
	headersWritten := false
	scan := s.scanCCStream(stream, "openai structured chat fallback", startTime, func(raw []byte, _ *apicompat.ChatCompletionsChunk) error {
		if !headersWritten {
			if err := output.WriteStreamHeaders(stream.StatusCode, stream.Headers, stream.ActualProtocol); err != nil {
				return err
			}
			headersWritten = true
		}
		return output.WriteStreamEvent(stream.ActualProtocol, raw)
	})
	result := &OpenAIForwardResult{
		RequestID:       stream.RequestID,
		ResponseID:      scan.ResponseID,
		ActualProtocol:  stream.ActualProtocol,
		Usage:           scan.Usage,
		Model:           originalModel,
		BillingModel:    billingModel,
		UpstreamModel:   upstreamModel,
		ReasoningEffort: reasoningEffort,
		ServiceTier:     serviceTier,
		Stream:          true,
		Duration:        time.Since(startTime),
		FirstTokenMs:    scan.FirstTokenMs,
	}
	if scan.Err != nil {
		return result, fmt.Errorf("stream usage incomplete: %w", scan.Err)
	}
	if !headersWritten {
		if err := output.WriteStreamHeaders(stream.StatusCode, stream.Headers, stream.ActualProtocol); err != nil {
			return result, err
		}
	}
	if err := output.FinalizeStream(stream.ActualProtocol); err != nil {
		return result, fmt.Errorf("finalize structured Chat Completions stream: %w", err)
	}
	result.ClientDisconnect = output.ClientDisconnected()
	if !scan.SawDone {
		logCCStreamMissingDoneSentinel("openai structured chat fallback", stream.RequestID)
	}
	return result, nil
}

func chatChunkStartsResponsesOutput(chunk *apicompat.ChatCompletionsChunk) bool {
	if chunk == nil {
		return false
	}
	for _, choice := range chunk.Choices {
		if choice.Delta.Content != nil || choice.Delta.ReasoningContent != nil || len(choice.Delta.ToolCalls) > 0 {
			return true
		}
	}
	return false
}

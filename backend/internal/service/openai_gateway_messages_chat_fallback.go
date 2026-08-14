package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// forwardAnthropicViaRawChatCompletions serves /v1/messages clients through
// an OpenAI-compatible upstream that only supports /v1/chat/completions.
//
// Requests, responses, and streams use one standard request-scoped pipeline.
//
// This is the /v1/messages counterpart of forwardResponsesViaRawChatCompletions
// (which serves /v1/responses clients). The same conversion bridges are reused;
// only the inbound/outbound framing differs.
func (s *OpenAIGatewayService) forwardAnthropicViaRawChatCompletions(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	body []byte,
	defaultMappedModel string,
) (*OpenAIForwardResult, error) {
	startTime := time.Now()

	// 1. Parse Anthropic request
	var anthropicReq apicompat.AnthropicRequest
	if err := json.Unmarshal(body, &anthropicReq); err != nil {
		writeAnthropicError(c, http.StatusBadRequest, "invalid_request_error", "Failed to parse request body")
		return nil, fmt.Errorf("parse anthropic request: %w", err)
	}
	originalModel := anthropicReq.Model
	if strings.TrimSpace(originalModel) == "" {
		writeAnthropicError(c, http.StatusBadRequest, "invalid_request_error", "model is required")
		return nil, fmt.Errorf("missing model in request")
	}
	applyOpenAICompatModelNormalization(&anthropicReq)
	clientStream := anthropicReq.Stream

	// 2. Apply the OpenAI-account model policy, then run standard Anthropic →
	// Chat conversion through one request-scoped pipeline.
	normalizedBody, err := json.Marshal(&anthropicReq)
	if err != nil {
		return nil, fmt.Errorf("marshal normalized anthropic request: %w", err)
	}
	billingModel := resolveOpenAIForwardModel(account, anthropicReq.Model, defaultMappedModel)
	upstreamModel := normalizeOpenAIModelForUpstream(account, billingModel)
	pipeline, err := protocolconv.NewPipeline(standardProtocolRegistry, protocolconv.PipelineConfig{
		Route: protocolconv.Route{
			Source:         protocolconv.ProtocolAnthropic,
			IntendedTarget: protocolconv.ProtocolOpenAIChat,
			ClientModel:    originalModel,
			UpstreamModel:  upstreamModel,
			Provider:       account.Platform,
			AccountID:      account.ID,
		},
		Options: protocolconv.Options{SourceModel: upstreamModel, LossPolicy: protocolconv.LossError},
	})
	if err != nil {
		return nil, fmt.Errorf("create anthropic chat fallback pipeline: %w", err)
	}
	convertedRequest, err := pipeline.ConvertRequest(normalizedBody)
	if err != nil {
		writeAnthropicError(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return nil, fmt.Errorf("convert anthropic to chat completions: %w", err)
	}
	var chatReq apicompat.ChatCompletionsRequest
	if err := json.Unmarshal(convertedRequest.Body, &chatReq); err != nil {
		return nil, fmt.Errorf("decode converted chat completions request: %w", err)
	}
	chatReq.Model = upstreamModel
	chatReq.Stream = clientStream
	if clientStream {
		chatReq.StreamOptions = &apicompat.ChatStreamOptions{IncludeUsage: true}
	}

	reasoningEffort := extractOpenAIReasoningEffortFromBody(body, upstreamModel, billingModel, originalModel)
	reasoningEffort = ApplyThinkingEnabledFallback(reasoningEffort, body, billingModel)
	serviceTier := extractOpenAIServiceTierFromBody(body)

	chatBody, err := json.Marshal(chatReq)
	if err != nil {
		return nil, fmt.Errorf("marshal chat completions request: %w", err)
	}
	if normalizedBody, normalized := NormalizeGLMOpenAIReasoningEffort(chatBody, upstreamModel); normalized {
		chatBody = normalizedBody
	}
	// Unlike forwardResponsesViaRawChatCompletions, applyOpenAIFastPolicyToBody
	// is intentionally skipped: Anthropic Messages bodies carry no service_tier,
	// so the converted Chat Completions body never contains one and the policy
	// would always be a no-op on this path.

	logger.L().Debug("openai messages: forwarding via raw chat completions",
		zap.Int64("account_id", account.ID),
		zap.String("original_model", originalModel),
		zap.String("billing_model", billingModel),
		zap.String("upstream_model", upstreamModel),
		zap.Bool("stream", clientStream),
	)

	// 3. Build and send upstream request via the shared CC pipeline
	apiKey, targetURL, err := s.resolveCCFallbackTarget(account)
	if err != nil {
		return nil, err
	}
	resp, err := s.sendCCUpstreamRequest(
		ctx, c, account, targetURL, chatBody, clientStream, apiKey, account.GetOpenAIUserAgent(), openAICCUpstreamRequestOptions{},
	)
	if err != nil {
		return nil, err
	}
	// 4. Handle error responses
	if resp.StatusCode >= 400 {
		defer func() { _ = resp.Body.Close() }()
		upstream, upstreamMsg, collectErr := s.collectOpenAIStructuredUpstreamError(resp, protocolconv.ProtocolOpenAIChat, clientStream)
		if collectErr != nil {
			return nil, collectErr
		}
		if foErr := s.failoverOpenAIStructuredUpstreamError(ctx, c, account, upstream, upstreamMsg, upstreamModel); foErr != nil {
			return nil, foErr
		}
		return s.handleAnthropicErrorResponse(upstream, c, account, upstreamModel)
	}

	// 5. Convert response
	if clientStream {
		return s.streamChatCompletionsAsAnthropic(c, resp, pipeline, originalModel, billingModel, upstreamModel, reasoningEffort, serviceTier, startTime)
	}
	defer func() { _ = resp.Body.Close() }()
	return s.bufferChatCompletionsAsAnthropic(c, resp, pipeline, originalModel, billingModel, upstreamModel, reasoningEffort, serviceTier, startTime)
}

func (s *OpenAIGatewayService) bufferChatCompletionsAsAnthropic(
	c *gin.Context,
	resp *http.Response,
	pipeline *protocolconv.Pipeline,
	originalModel string,
	billingModel string,
	upstreamModel string,
	reasoningEffort *string,
	serviceTier *string,
	startTime time.Time,
) (*OpenAIForwardResult, error) {
	_, usage, upstreamBody, err := s.readCCUpstreamJSONResult(c, resp, writeAnthropicError)
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
		writeAnthropicError(c, http.StatusBadGateway, "api_error", "Failed to collect upstream response")
		return nil, err
	}
	convertedResponse, err := pipeline.ConvertResponse(structured.Body, structured.ActualProtocol)
	if result != nil {
		result.ActualProtocol = structured.ActualProtocol
	}
	if err != nil {
		writeAnthropicError(c, http.StatusBadGateway, "api_error", "Failed to convert upstream response")
		return nil, fmt.Errorf("convert anthropic chat fallback response: %w", err)
	}
	renderer, err := protocolconv.NewRenderer(protocolconv.ProtocolAnthropic)
	if err != nil {
		return nil, err
	}
	if err := renderer.RenderJSON(c.Writer, http.StatusOK, structured.Headers, convertedResponse.Body); err != nil {
		return nil, fmt.Errorf("render anthropic chat fallback response: %w", err)
	}
	return result, nil
}

func (s *OpenAIGatewayService) streamChatCompletionsAsAnthropic(
	c *gin.Context,
	resp *http.Response,
	pipeline *protocolconv.Pipeline,
	originalModel string,
	billingModel string,
	upstreamModel string,
	reasoningEffort *string,
	serviceTier *string,
	startTime time.Time,
) (*OpenAIForwardResult, error) {
	stream, err := s.collectCCUpstreamStream(resp, startTime)
	if err != nil {
		return nil, err
	}
	defer func() { _ = stream.Close() }()
	requestID := stream.RequestID
	session, err := pipeline.NewStreamProcessor(stream.ActualProtocol)
	if err != nil {
		return nil, fmt.Errorf("create Anthropic fallback stream processor: %w", err)
	}
	renderer, err := protocolconv.NewRenderer(protocolconv.ProtocolAnthropic)
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
				return nil
			}
		}
		c.Writer.Flush()
		return nil
	}

	scan := s.scanCCStream(stream, "openai messages chat fallback", startTime, func(raw []byte, _ *apicompat.ChatCompletionsChunk) error {
		payloads, _, err := session.Convert(raw)
		if err != nil {
			return fmt.Errorf("convert Chat Completions stream event: %w", err)
		}
		return writePayloads(payloads)
	})
	result := &OpenAIForwardResult{
		RequestID:        requestID,
		ResponseID:       scan.ResponseID,
		ActualProtocol:   stream.ActualProtocol,
		Usage:            scan.Usage,
		Model:            originalModel,
		BillingModel:     billingModel,
		UpstreamModel:    upstreamModel,
		ReasoningEffort:  reasoningEffort,
		ServiceTier:      serviceTier,
		Stream:           true,
		Duration:         time.Since(startTime),
		FirstTokenMs:     scan.FirstTokenMs,
		ClientDisconnect: clientDisconnected,
	}
	if scan.Err != nil {
		return result, fmt.Errorf("stream usage incomplete: %w", scan.Err)
	}

	finalPayloads, _, err := session.Finalize()
	if err != nil {
		return result, fmt.Errorf("finalize Anthropic fallback stream: %w", err)
	}
	if err := writePayloads(finalPayloads); err != nil {
		return result, fmt.Errorf("render Anthropic fallback stream: %w", err)
	}
	result.ClientDisconnect = clientDisconnected
	if !scan.SawDone {
		logCCStreamMissingDoneSentinel("openai messages chat fallback", requestID)
	}
	return result, nil
}

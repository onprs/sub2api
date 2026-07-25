package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv"
	protocoltransport "github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv/transport"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// ForwardGoogleGenAI accepts a Google generateContent request and executes it
// through an Anthropic account while retaining one standard protocol pipeline
// for the request and successful response stream.
func (s *GatewayService) ForwardGoogleGenAI(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	clientModel string,
	routedModel string,
	clientStream bool,
	body []byte,
) (*ForwardResult, error) {
	if c == nil || c.Writer == nil {
		return nil, errors.New("response writer is required for Google GenAI forwarding")
	}
	if account == nil || account.Platform != PlatformAnthropic {
		return nil, errors.New("an Anthropic account is required for Google GenAI forwarding")
	}
	startTime := time.Now()
	mappedModel, err := resolveStandardAnthropicTargetModel(account, routedModel)
	if err != nil {
		return nil, err
	}

	pipeline, err := protocolconv.NewPipeline(standardProtocolRegistry, protocolconv.PipelineConfig{
		Route: protocolconv.Route{
			Source: protocolconv.ProtocolGoogleGenAI, IntendedTarget: protocolconv.ProtocolAnthropic,
			ClientModel: clientModel, UpstreamModel: mappedModel, Provider: account.Platform, AccountID: account.ID,
		},
		Options: protocolconv.Options{SourceModel: mappedModel, LossPolicy: protocolconv.LossError},
	})
	if err != nil {
		return nil, fmt.Errorf("create Google Anthropic pipeline: %w", err)
	}
	convertedRequest, err := pipeline.ConvertRequest(body)
	if err != nil {
		return nil, fmt.Errorf("convert Google request to Anthropic: %w", err)
	}
	var anthropicReq apicompat.AnthropicRequest
	if err := json.Unmarshal(convertedRequest.Body, &anthropicReq); err != nil {
		return nil, fmt.Errorf("decode converted Anthropic request: %w", err)
	}
	anthropicReq.Model = mappedModel
	anthropicReq.Stream = true
	if anthropicReq.MaxTokens <= 0 {
		anthropicReq.MaxTokens = 8192
	}
	anthropicBody, err := json.Marshal(&anthropicReq)
	if err != nil {
		return nil, fmt.Errorf("marshal Anthropic request: %w", err)
	}

	renderer, err := protocolconv.NewRenderer(protocolconv.ProtocolGoogleGenAI)
	if err != nil {
		return nil, err
	}
	resp, err := s.forwardStandardProtocolToAnthropic(ctx, c, account, anthropicBody, anthropicReq.System, mappedModel, func(statusCode int, errorType, message string) {
		_ = renderer.RenderError(c.Writer, statusCode, errorType, errorType, message)
	})
	if err != nil {
		return nil, err
	}

	var result *ForwardResult
	if clientStream {
		result, err = s.handleGoogleStreamingFromAnthropic(resp, c, pipeline, clientModel, mappedModel, startTime)
	} else {
		result, err = s.handleGoogleBufferedFromAnthropic(resp, c, pipeline, clientModel, mappedModel, startTime)
	}
	if account.IsBedrock() {
		return s.handleStandardBedrockStreamError(ctx, c, account, mappedModel, result, err)
	}
	return result, err
}

func (s *GatewayService) handleGoogleBufferedFromAnthropic(
	resp *http.Response,
	c *gin.Context,
	pipeline *protocolconv.Pipeline,
	clientModel string,
	mappedModel string,
	startTime time.Time,
) (*ForwardResult, error) {
	stream, err := s.collectAnthropicProtocolStream(resp, startTime)
	if err != nil {
		return nil, err
	}
	defer func() { _ = stream.Close() }()
	finalResp, usage, err := collectBufferedAnthropicResponse(stream)
	if err != nil {
		return nil, fmt.Errorf("collect Anthropic response for Google: %w", err)
	}
	anthropicBody, err := json.Marshal(finalResp)
	if err != nil {
		return nil, fmt.Errorf("marshal Anthropic response: %w", err)
	}
	converted, err := pipeline.ConvertResponse(anthropicBody, stream.ActualProtocol)
	if err != nil {
		return nil, fmt.Errorf("convert Anthropic response to Google: %w", err)
	}
	converted.Body = reverseToolNamesIfPresent(c, converted.Body)
	renderer, err := protocolconv.NewRenderer(protocolconv.ProtocolGoogleGenAI)
	if err != nil {
		return nil, err
	}
	if err := renderer.RenderJSON(c.Writer, stream.StatusCode, stream.Headers, converted.Body); err != nil {
		return nil, fmt.Errorf("render Google response: %w", err)
	}
	return &ForwardResult{
		RequestID: stream.RequestID, ActualProtocol: stream.ActualProtocol,
		Usage: usage, Model: clientModel, UpstreamModel: mappedModel,
		Stream: false, Duration: time.Since(startTime),
	}, nil
}

func (s *GatewayService) handleGoogleStreamingFromAnthropic(
	resp *http.Response,
	c *gin.Context,
	pipeline *protocolconv.Pipeline,
	clientModel string,
	mappedModel string,
	startTime time.Time,
) (*ForwardResult, error) {
	stream, err := s.collectAnthropicProtocolStream(resp, startTime)
	if err != nil {
		return nil, err
	}
	defer func() { _ = stream.Close() }()
	session, err := pipeline.NewStreamProcessor(stream.ActualProtocol)
	if err != nil {
		return nil, err
	}
	renderer, err := protocolconv.NewRenderer(protocolconv.ProtocolGoogleGenAI)
	if err != nil {
		return nil, err
	}
	var usage ClaudeUsage
	var firstTokenMs *int
	headersWritten := false
	clientDisconnected := false
	terminalSeen := false
	resultWithUsage := func() *ForwardResult {
		return &ForwardResult{
			RequestID: stream.RequestID, ActualProtocol: stream.ActualProtocol,
			Usage: usage, Model: clientModel, UpstreamModel: mappedModel,
			Stream: true, Duration: time.Since(startTime), FirstTokenMs: firstTokenMs, ClientDisconnect: clientDisconnected,
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
		for _, payload := range payloads {
			payload = reverseToolNamesIfPresent(c, payload)
			framed, err := renderer.FrameStreamEvent(payload)
			if err != nil {
				return err
			}
			if _, err := c.Writer.Write(framed); err != nil {
				clientDisconnected = true
				logger.L().Info("Google Anthropic stream: client disconnected, continuing to drain upstream for billing", zap.String("request_id", stream.RequestID))
				return nil
			}
		}
		c.Writer.Flush()
		return nil
	}

	for {
		record, nextErr := stream.Events.Next(context.Background())
		if errors.Is(nextErr, io.EOF) || errors.Is(nextErr, protocoltransport.ErrSSEDone) {
			break
		}
		if nextErr != nil {
			return resultWithUsage(), fmt.Errorf("read Anthropic stream for Google: %w", nextErr)
		}
		var event apicompat.AnthropicStreamEvent
		if err := json.Unmarshal(record.Data, &event); err != nil {
			return resultWithUsage(), fmt.Errorf("decode Anthropic stream event: %w", err)
		}
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
		converted, _, err := session.Convert(record.Data)
		if err != nil {
			return resultWithUsage(), fmt.Errorf("convert Anthropic stream event to Google: %w", err)
		}
		if err := writePayloads(converted); err != nil {
			return resultWithUsage(), err
		}
		if event.Type == "message_stop" {
			terminalSeen = true
			break
		}
	}
	if !terminalSeen {
		return resultWithUsage(), errors.New("stream ended without message_stop for Anthropic")
	}
	finalPayloads, _, err := session.Finalize()
	if err != nil {
		return resultWithUsage(), fmt.Errorf("finalize Google Anthropic stream: %w", err)
	}
	if err := writePayloads(finalPayloads); err != nil {
		return resultWithUsage(), err
	}
	if !headersWritten && !clientDisconnected {
		if err := renderer.WriteStreamHeaders(c.Writer, stream.StatusCode, stream.Headers); err != nil {
			return resultWithUsage(), err
		}
	}
	if !clientDisconnected {
		c.Writer.Flush()
	}
	return resultWithUsage(), nil
}

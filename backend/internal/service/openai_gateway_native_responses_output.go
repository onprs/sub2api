package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv"
	protocoltransport "github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv/transport"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
	"go.uber.org/zap"
)

func newOpenAIResponsesIdentityPipeline(account *Account, clientModel, upstreamModel string) (*protocolconv.Pipeline, error) {
	route := protocolconv.Route{
		Source:         protocolconv.ProtocolOpenAIResponses,
		IntendedTarget: protocolconv.ProtocolOpenAIResponses,
		ClientModel:    clientModel,
		UpstreamModel:  upstreamModel,
	}
	if account != nil {
		route.Provider = account.Platform
		route.AccountID = account.ID
	}
	return protocolconv.NewPipeline(standardProtocolRegistry, protocolconv.PipelineConfig{
		Route:   route,
		Options: protocolconv.Options{SourceModel: upstreamModel, LossPolicy: protocolconv.LossError},
	})
}

// nativeResponsesProtocolOutput renders one native Responses request after the
// provider service has completed routing, retry, usage, and response policy.
type nativeResponsesProtocolOutput struct {
	writer        http.ResponseWriter
	pipeline      *protocolconv.Pipeline
	renderer      *protocolconv.Renderer
	originalModel string
	mappedModel   string
	stream        bool

	session            *protocolconv.StreamSession
	actualProtocol     protocolconv.Protocol
	pendingStatus      int
	pendingHeaders     http.Header
	headersWritten     bool
	outputStarted      bool
	clientDisconnected bool
	pendingPayloads    [][]byte
}

func newNativeResponsesProtocolOutput(
	writer http.ResponseWriter,
	pipeline *protocolconv.Pipeline,
	originalModel string,
	mappedModel string,
	stream bool,
) (*nativeResponsesProtocolOutput, error) {
	renderer, err := protocolconv.NewRenderer(protocolconv.ProtocolOpenAIResponses)
	if err != nil {
		return nil, err
	}
	return &nativeResponsesProtocolOutput{
		writer: writer, pipeline: pipeline, renderer: renderer,
		originalModel: originalModel, mappedModel: mappedModel, stream: stream,
	}, nil
}

func (o *nativeResponsesProtocolOutput) WriteResponse(response protocoltransport.Response) error {
	if o.stream {
		return errors.New("streaming native Responses output received a complete response")
	}
	if err := response.Validate(); err != nil {
		return err
	}
	converted, err := o.pipeline.ConvertResponse(response.Body, response.ActualProtocol)
	if err != nil {
		return err
	}
	body := o.restoreModel(converted.Body)
	if err := o.renderer.RenderJSON(o.writer, response.StatusCode, response.Headers, body); err != nil {
		o.clientDisconnected = true
		return err
	}
	o.outputStarted = true
	return nil
}

func (o *nativeResponsesProtocolOutput) WriteStreamHeaders(status int, headers http.Header, actual protocolconv.Protocol) error {
	if !o.stream {
		return errors.New("non-streaming native Responses output received stream headers")
	}
	if o.session != nil {
		if actual != o.actualProtocol {
			return fmt.Errorf("actual stream protocol changed from %s to %s", o.actualProtocol, actual)
		}
		return nil
	}
	if err := actual.Validate(); err != nil {
		return err
	}
	session, err := o.pipeline.NewStreamProcessor(actual)
	if err != nil {
		return err
	}
	o.session = session
	o.actualProtocol = actual
	o.pendingStatus = status
	o.pendingHeaders = protocoltransport.CloneHeaders(headers)
	return nil
}

func (o *nativeResponsesProtocolOutput) WriteStreamEvent(actual protocolconv.Protocol, payload []byte) error {
	if o.clientDisconnected {
		return nil
	}
	if o.session == nil {
		return errors.New("stream event received before native Responses stream initialization")
	}
	if actual != o.actualProtocol {
		return fmt.Errorf("actual stream protocol changed from %s to %s", o.actualProtocol, actual)
	}
	payloads, _, err := o.session.Convert(payload)
	if err != nil {
		return err
	}
	for _, converted := range payloads {
		converted = o.restoreModel(converted)
		if !o.outputStarted && openAIStreamEventIsPreamble(gjson.GetBytes(converted, "type").String()) {
			o.pendingPayloads = append(o.pendingPayloads, append([]byte(nil), converted...))
			continue
		}
		toWrite := make([][]byte, 0, len(o.pendingPayloads)+1)
		toWrite = append(toWrite, o.pendingPayloads...)
		toWrite = append(toWrite, converted)
		o.pendingPayloads = nil
		if err := o.writeStreamPayloads(toWrite); err != nil {
			return err
		}
	}
	return nil
}

func (o *nativeResponsesProtocolOutput) FinalizeStream(actual protocolconv.Protocol) error {
	if !o.stream || o.session == nil {
		return errors.New("native Responses stream was not initialized")
	}
	if actual != o.actualProtocol {
		return fmt.Errorf("actual stream protocol changed from %s to %s", o.actualProtocol, actual)
	}
	payloads, _, err := o.session.Finalize()
	if err != nil {
		return err
	}
	for _, converted := range payloads {
		o.pendingPayloads = append(o.pendingPayloads, o.restoreModel(converted))
	}
	if len(o.pendingPayloads) > 0 {
		pending := o.pendingPayloads
		o.pendingPayloads = nil
		if err := o.writeStreamPayloads(pending); err != nil {
			return err
		}
	}
	if !o.clientDisconnected {
		if err := o.ensureStreamHeaders(); err != nil {
			return err
		}
		if terminal := o.renderer.StreamTerminal(); len(terminal) > 0 {
			if _, err := o.writer.Write(terminal); err != nil {
				o.clientDisconnected = true
			}
		}
		if !o.clientDisconnected {
			if flusher, ok := o.writer.(http.Flusher); ok {
				flusher.Flush()
			}
		}
	}
	return nil
}

func (o *nativeResponsesProtocolOutput) writeStreamPayloads(payloads [][]byte) error {
	if o.clientDisconnected || len(payloads) == 0 {
		return nil
	}
	framedPayloads := make([][]byte, 0, len(payloads))
	for _, payload := range payloads {
		framed, err := o.renderer.FrameStreamEvent(payload)
		if err != nil {
			return err
		}
		framedPayloads = append(framedPayloads, framed)
	}
	if err := o.ensureStreamHeaders(); err != nil {
		return err
	}
	for _, framed := range framedPayloads {
		if _, err := o.writer.Write(framed); err != nil {
			o.clientDisconnected = true
			return nil
		}
		o.outputStarted = true
	}
	if flusher, ok := o.writer.(http.Flusher); ok {
		flusher.Flush()
	}
	return nil
}

func (o *nativeResponsesProtocolOutput) ensureStreamHeaders() error {
	if o.headersWritten {
		return nil
	}
	if err := o.renderer.WriteStreamHeaders(o.writer, o.pendingStatus, o.pendingHeaders); err != nil {
		return err
	}
	o.headersWritten = true
	return nil
}

func (o *nativeResponsesProtocolOutput) restoreModel(body []byte) []byte {
	if o == nil || strings.TrimSpace(o.originalModel) == "" || strings.TrimSpace(o.mappedModel) == "" || o.originalModel == o.mappedModel {
		return body
	}
	for _, path := range []string{"model", "response.model"} {
		if model := gjson.GetBytes(body, path); model.Exists() && model.String() == o.mappedModel {
			if updated, err := sjson.SetBytes(body, path, o.originalModel); err == nil {
				body = updated
			}
		}
	}
	return body
}

func (o *nativeResponsesProtocolOutput) ClientOutputStarted() bool { return o.outputStarted }
func (o *nativeResponsesProtocolOutput) ClientDisconnected() bool  { return o.clientDisconnected }

func (s *OpenAIGatewayService) collectNativeResponsesStream(resp *http.Response, startTime time.Time) (*protocoltransport.Stream, error) {
	if resp == nil || resp.Body == nil {
		return nil, errors.New("nil OpenAI Responses upstream stream")
	}
	maxRecordSize := defaultMaxLineSize
	if s.cfg != nil && s.cfg.Gateway.MaxLineSize > 0 {
		maxRecordSize = s.cfg.Gateway.MaxLineSize
	}
	stream := &protocoltransport.Stream{
		StatusCode: resp.StatusCode,
		Headers: filterOpenAIPassthroughResponseHeaders(
			resp.Header,
			s.responseHeaderFilter,
		),
		ActualProtocol: protocolconv.ProtocolOpenAIResponses,
		RequestID:      resp.Header.Get("x-request-id"),
		Duration:       time.Since(startTime),
		Events:         protocoltransport.NewSSEParser(resp.Body, maxRecordSize),
	}
	if err := stream.Validate(); err != nil {
		_ = stream.Close()
		return nil, fmt.Errorf("validate OpenAI Responses stream: %w", err)
	}
	return stream, nil
}

func (s *OpenAIGatewayService) handleNativeResponsesStreamingResponse(
	ctx context.Context,
	resp *http.Response,
	c *gin.Context,
	account *Account,
	startTime time.Time,
	output *nativeResponsesProtocolOutput,
) (*openaiStreamingResultPassthrough, error) {
	stream, err := s.collectNativeResponsesStream(resp, startTime)
	if err != nil {
		return nil, err
	}
	defer func() { _ = stream.Close() }()
	if err := output.WriteStreamHeaders(stream.StatusCode, stream.Headers, stream.ActualProtocol); err != nil {
		return nil, err
	}

	usage := &OpenAIUsage{}
	imageCounter := newOpenAIImageOutputCounter()
	var firstTokenMs *int
	responseID := ""
	sawDone := false
	sawTerminalEvent := false
	sawFailedEvent := false
	failedMessage := ""
	upstreamRequestID := strings.TrimSpace(stream.RequestID)
	resultWithUsage := func() *openaiStreamingResultPassthrough {
		return &openaiStreamingResultPassthrough{
			usage:            usage,
			firstTokenMs:     firstTokenMs,
			responseID:       responseID,
			imageCount:       imageCounter.Count(),
			imageOutputSizes: imageCounter.Sizes(),
		}
	}

	for {
		record, nextErr := stream.Events.Next(context.Background())
		if errors.Is(nextErr, protocoltransport.ErrSSEDone) {
			sawDone = true
			break
		}
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			if sawTerminalEvent && !sawFailedEvent {
				if err := output.FinalizeStream(stream.ActualProtocol); err != nil {
					return resultWithUsage(), err
				}
				return resultWithUsage(), nil
			}
			if sawFailedEvent {
				return resultWithUsage(), fmt.Errorf("upstream response failed: %s", failedMessage)
			}
			if errors.Is(nextErr, context.Canceled) || errors.Is(nextErr, context.DeadlineExceeded) {
				return resultWithUsage(), fmt.Errorf("stream usage incomplete: %w", nextErr)
			}
			if !openAIStreamClientOutputStarted(c, output.ClientOutputStarted()) {
				message := "OpenAI stream disconnected before completion"
				if errText := strings.TrimSpace(nextErr.Error()); errText != "" {
					message += ": " + errText
				}
				return resultWithUsage(), s.newOpenAIStreamFailoverError(c, account, true, upstreamRequestID, nil, message)
			}
			if output.ClientDisconnected() {
				return resultWithUsage(), fmt.Errorf("stream usage incomplete after disconnect: %w", nextErr)
			}
			logger.FromContext(ctx).With(
				zap.String("component", "service.openai_gateway"),
				zap.Int64("account_id", account.ID),
				zap.String("upstream_request_id", upstreamRequestID),
				zap.Error(nextErr),
			).Info("OpenAI passthrough stream read failed")
			return resultWithUsage(), fmt.Errorf("stream read error: %w", nextErr)
		}

		dataBytes := record.Data
		eventType := strings.TrimSpace(gjson.GetBytes(dataBytes, "type").String())
		if normalizedData, normalized := normalizeOpenAIResponsesFunctionCallArguments(dataBytes); normalized {
			dataBytes = normalizedData
			eventType = strings.TrimSpace(gjson.GetBytes(dataBytes, "type").String())
		}
		if eventType == "response.failed" {
			failedMessage = extractOpenAISSEErrorMessage(dataBytes)
			s.parseSSEUsageBytes(dataBytes, usage)
			if hit, code, msg := detectOpenAICyberPolicy(dataBytes); hit {
				MarkOpsCyberPolicy(c, CyberPolicyMark{
					Code: code, Message: msg, Body: truncateString(string(dataBytes), 4096),
					UpstreamStatus: http.StatusOK, UpstreamInTok: usage.InputTokens, UpstreamOutTok: usage.OutputTokens,
				})
			}
			if !openAIStreamClientOutputStarted(c, output.ClientOutputStarted()) {
				if status, errType, errMsg, matched := applyOpenAIStreamFailedErrorPassthroughRule(c, account.Platform, dataBytes, failedMessage); matched {
					s.recordOpenAIStreamUpstreamError(c, account, true, upstreamRequestID, "http_error", dataBytes, failedMessage)
					MarkResponseCommitted(c)
					c.Writer.Header().Set("Content-Type", "application/json; charset=utf-8")
					c.JSON(status, gin.H{"error": gin.H{"type": errType, "message": errMsg}})
					return resultWithUsage(), fmt.Errorf("upstream response failed: passthrough rule matched message=%s", errMsg)
				}
				if openAIStreamFailedEventShouldFailover(dataBytes, failedMessage) {
					return resultWithUsage(), s.newOpenAIStreamFailoverError(c, account, true, upstreamRequestID, dataBytes, failedMessage)
				}
			}
			sawFailedEvent = true
		}
		if openAIStreamEventIsTerminal(string(dataBytes)) {
			sawTerminalEvent = true
		}
		if responseID == "" {
			responseID = extractOpenAIResponseIDFromJSONBytes(dataBytes)
			stream.ResponseID = responseID
		}
		imageCounter.AddSSEData(dataBytes)
		if sanitizedData, sanitized := sanitizeOpenAIResponseFailedEventForClient(
			dataBytes,
			eventType,
			openAIStreamClientOutputStarted(c, output.ClientOutputStarted()),
		); sanitized {
			dataBytes = sanitizedData
		}
		startsOutput := eventType == "response.failed" || openAIStreamDataStartsClientOutput(string(dataBytes), eventType)
		if firstTokenMs == nil && startsOutput {
			ms := int(time.Since(startTime).Milliseconds())
			firstTokenMs = &ms
		}
		s.parseSSEUsageBytes(dataBytes, usage)
		if err := output.WriteStreamEvent(stream.ActualProtocol, dataBytes); err != nil {
			return resultWithUsage(), err
		}
	}

	if sawFailedEvent {
		return resultWithUsage(), fmt.Errorf("upstream response failed: %s", failedMessage)
	}
	if !output.ClientDisconnected() && !sawDone && !sawTerminalEvent && ctx.Err() == nil {
		logger.FromContext(ctx).With(
			zap.String("component", "service.openai_gateway"),
			zap.Int64("account_id", account.ID),
			zap.String("upstream_request_id", upstreamRequestID),
		).Info("OpenAI passthrough 上游流在未收到 [DONE] 时结束，疑似断流")
		if !openAIStreamClientOutputStarted(c, output.ClientOutputStarted()) {
			return resultWithUsage(), s.newOpenAIStreamFailoverError(c, account, true, upstreamRequestID, nil, "OpenAI stream ended before a terminal event")
		}
		return resultWithUsage(), errors.New("stream usage incomplete: missing terminal event")
	}
	if err := output.FinalizeStream(stream.ActualProtocol); err != nil {
		return resultWithUsage(), err
	}
	return resultWithUsage(), nil
}

func isNativeResponsesProtocolOutput(output openAIProtocolOutput) bool {
	_, ok := output.(*nativeResponsesProtocolOutput)
	return ok
}

var _ openAIProtocolOutput = (*nativeResponsesProtocolOutput)(nil)

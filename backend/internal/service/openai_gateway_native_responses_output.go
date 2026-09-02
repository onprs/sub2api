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

	session             *protocolconv.StreamSession
	actualProtocol      protocolconv.Protocol
	pendingStatus       int
	pendingHeaders      http.Header
	headersWritten      bool
	outputStarted       bool
	clientDisconnected  bool
	pendingPayloads     [][]byte
	pendingPayloadBytes int64
	pendingPayloadLimit int64
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
	if o.clientDisconnected {
		return nil
	}
	for _, converted := range payloads {
		converted = o.restoreModel(converted)
		if !o.outputStarted && openAIStreamEventIsPreamble(gjson.GetBytes(converted, "type").String()) {
			if o.pendingPayloadLimit > 0 && int64(len(converted)) > o.pendingPayloadLimit-o.pendingPayloadBytes {
				return fmt.Errorf("%w: buffered=%d incoming=%d limit=%d", errOpenAIFirstOutputStageLimit, o.pendingPayloadBytes, len(converted), o.pendingPayloadLimit)
			}
			o.pendingPayloads = append(o.pendingPayloads, append([]byte(nil), converted...))
			o.pendingPayloadBytes += int64(len(converted))
			continue
		}
		if !o.outputStarted && o.pendingPayloadLimit > 0 && int64(len(converted)) > o.pendingPayloadLimit-o.pendingPayloadBytes {
			return fmt.Errorf("%w: buffered=%d incoming=%d limit=%d", errOpenAIFirstOutputStageLimit, o.pendingPayloadBytes, len(converted), o.pendingPayloadLimit)
		}
		toWrite := make([][]byte, 0, len(o.pendingPayloads)+1)
		toWrite = append(toWrite, o.pendingPayloads...)
		toWrite = append(toWrite, converted)
		o.pendingPayloads = nil
		o.pendingPayloadBytes = 0
		if err := o.writeStreamPayloads(toWrite); err != nil {
			return err
		}
	}
	return nil
}

func (o *nativeResponsesProtocolOutput) WriteStreamKeepalive() error {
	if !o.stream || o.session == nil {
		return errors.New("native Responses stream was not initialized")
	}
	if o.clientDisconnected {
		return nil
	}
	if err := o.ensureStreamHeaders(); err != nil {
		return err
	}
	if _, err := o.writer.Write(o.renderer.StreamKeepalive()); err != nil {
		o.clientDisconnected = true
		return nil
	}
	if flusher, ok := o.writer.(http.Flusher); ok {
		flusher.Flush()
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
	if state, ok := o.writer.(interface{ Written() bool }); ok && state.Written() {
		// A stable gateway keepalive may have committed the downstream response
		// before this attempt won. Do not append attempt-local headers or emit a
		// duplicate WriteHeader when semantic output eventually arrives.
		o.pendingHeaders = nil
		o.headersWritten = true
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

func (o *nativeResponsesProtocolOutput) enableFirstOutputGuard(limit int64) {
	if o == nil || limit <= 0 {
		return
	}
	o.pendingPayloadLimit = limit
}

func (s *OpenAIGatewayService) collectStructuredResponsesStream(
	resp *http.Response,
	startTime time.Time,
	output openAIProtocolOutput,
	passthrough bool,
	guardFirstOutput bool,
) (*protocoltransport.Stream, error) {
	if resp == nil || resp.Body == nil {
		return nil, errors.New("nil OpenAI Responses upstream stream")
	}
	maxRecordSize := defaultMaxLineSize
	if s.cfg != nil && s.cfg.Gateway.MaxLineSize > 0 {
		maxRecordSize = s.cfg.Gateway.MaxLineSize
	}
	if guardFirstOutput {
		guardLimit := openAIFirstOutputStageMaxBytes + openAIFirstOutputScannerFramingAllowance
		if maxRecordSize <= 0 || maxRecordSize > guardLimit {
			maxRecordSize = guardLimit
		}
	}
	filteredHeaders := responseheaders.FilterHeaders(resp.Header, s.responseHeaderFilter)
	if passthrough && isNativeResponsesProtocolOutput(output) {
		filteredHeaders = filterOpenAIPassthroughResponseHeaders(resp.Header, s.responseHeaderFilter)
	}
	stream := &protocoltransport.Stream{
		StatusCode:     resp.StatusCode,
		Headers:        filteredHeaders,
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

func (s *OpenAIGatewayService) handleStructuredResponsesStream(
	ctx context.Context,
	resp *http.Response,
	c *gin.Context,
	account *Account,
	startTime time.Time,
	originalModel string,
	output openAIProtocolOutput,
) (*openaiStreamingResult, error) {
	return s.handleStructuredResponsesStreamWithReasoning(ctx, resp, c, account, startTime, originalModel, output, "")
}

func (s *OpenAIGatewayService) handleStructuredResponsesStreamWithReasoning(
	ctx context.Context,
	resp *http.Response,
	c *gin.Context,
	account *Account,
	startTime time.Time,
	originalModel string,
	output openAIProtocolOutput,
	reasoningEffort string,
) (*openaiStreamingResult, error) {
	firstOutputTimeout := time.Duration(0)
	nativeOutput, nativeResponses := output.(*nativeResponsesProtocolOutput)
	guardFirstOutput := account != nil && account.Platform == PlatformOpenAI && nativeResponses
	if guardFirstOutput {
		firstOutputTimeout = s.openAIFirstOutputTimeout(reasoningEffort)
		guardFirstOutput = firstOutputTimeout > 0
	}
	if guardFirstOutput {
		nativeOutput.enableFirstOutputGuard(openAIFirstOutputStageMaxBytes)
	}
	stream, err := s.collectStructuredResponsesStream(resp, startTime, output, false, guardFirstOutput)
	if err != nil {
		return nil, err
	}
	defer func() { _ = stream.Close() }()
	if isNativeResponsesProtocolOutput(output) {
		stageOpenAICodexTurnState(&stream.Headers, resp.Header)
	}
	if err := output.WriteStreamHeaders(stream.StatusCode, stream.Headers, stream.ActualProtocol); err != nil {
		return nil, err
	}
	parser, ok := stream.Events.(*protocoltransport.SSEParser)
	if !ok {
		return nil, errors.New("structured Responses stream has no bounded SSE parser")
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
	streamOutputAccumulator := apicompat.NewBufferedResponseAccumulator()
	streamImageOutputs := make([]json.RawMessage, 0, 1)
	streamSeenImages := make(map[string]struct{})
	lastDownstreamWriteAt := time.Now()
	semanticOutputSeen := false
	resultWithUsage := func() *openaiStreamingResult {
		return &openaiStreamingResult{
			usage: usage, firstTokenMs: firstTokenMs, responseID: responseID,
			imageCount: imageCounter.Count(), imageOutputSizes: imageCounter.Sizes(),
		}
	}
	clientOutputStarted := func() bool {
		return openAIStreamClientOutputStarted(c, output.ClientOutputStarted())
	}
	turnStateProvenanceNoted := false
	noteTurnStateIfCommitted := func() {
		if turnStateProvenanceNoted || !isNativeResponsesProtocolOutput(output) {
			return
		}
		s.noteStagedOpenAICodexTurnStateCommitted(c, account, stream.Headers)
		turnStateProvenanceNoted = extractOpenAICodexTurnState(stream.Headers) != ""
	}
	var firstOutputTimer *time.Timer
	var firstOutputCh <-chan time.Time
	if guardFirstOutput {
		remaining := time.Until(startTime.Add(firstOutputTimeout))
		if remaining <= 0 {
			remaining = time.Nanosecond
		}
		firstOutputTimer = time.NewTimer(remaining)
		firstOutputCh = firstOutputTimer.C
		defer firstOutputTimer.Stop()
	}
	stopFirstOutputTimer := func() {
		if firstOutputTimer == nil {
			return
		}
		if !firstOutputTimer.Stop() {
			select {
			case <-firstOutputTimer.C:
			default:
			}
		}
		firstOutputTimer = nil
		firstOutputCh = nil
	}
	finalize := func() (*openaiStreamingResult, error) {
		if sawFailedEvent {
			return resultWithUsage(), fmt.Errorf("upstream response failed: %s", failedMessage)
		}
		if !sawDone && !sawTerminalEvent {
			if !clientOutputStarted() {
				failoverErr := s.newOpenAIStreamFailoverError(
					c, account, false, upstreamRequestID, nil,
					"OpenAI stream ended before a terminal event",
				)
				if guardFirstOutput {
					failoverErr.SafeToFailoverAfterWrite = true
				}
				return resultWithUsage(), failoverErr
			}
			return resultWithUsage(), errors.New("stream usage incomplete: missing terminal event")
		}
		if err := output.FinalizeStream(stream.ActualProtocol); err != nil {
			return resultWithUsage(), err
		}
		noteTurnStateIfCommitted()
		return resultWithUsage(), nil
	}
	handleReadError := func(readErr error) (*openaiStreamingResult, error, bool) {
		if readErr == nil {
			return nil, nil, false
		}
		if sawTerminalEvent && !sawFailedEvent {
			logger.LegacyPrintf("service.openai_gateway", "Upstream structured stream ended after terminal event: %v", readErr)
			result, finalizeErr := finalize()
			return result, finalizeErr, true
		}
		if sawFailedEvent {
			return resultWithUsage(), fmt.Errorf("upstream response failed: %s", failedMessage), true
		}
		if errors.Is(readErr, context.Canceled) || errors.Is(readErr, context.DeadlineExceeded) {
			return resultWithUsage(), fmt.Errorf("stream usage incomplete: %w", readErr), true
		}
		if !clientOutputStarted() {
			message := "OpenAI stream disconnected before completion"
			if errText := strings.TrimSpace(readErr.Error()); errText != "" {
				message += ": " + errText
			}
			failoverErr := s.newOpenAIStreamFailoverError(c, account, false, upstreamRequestID, nil, message)
			if guardFirstOutput {
				failoverErr.SafeToFailoverAfterWrite = true
			}
			return resultWithUsage(), failoverErr, true
		}
		if output.ClientDisconnected() {
			return resultWithUsage(), fmt.Errorf("stream usage incomplete after disconnect: %w", readErr), true
		}
		return resultWithUsage(), fmt.Errorf("stream read error: %w", readErr), true
	}
	processRecord := func(record protocoltransport.SSERecord) error {
		dataBytes := []byte(openAICompatPayloadWithEventType(string(record.Data), record.Event))
		eventType := strings.TrimSpace(gjson.GetBytes(dataBytes, "type").String())
		if openAIStreamEventIsTerminal(string(dataBytes)) {
			sawTerminalEvent = true
		}
		if responseID == "" {
			responseID = extractOpenAIResponseIDFromJSONBytes(dataBytes)
			stream.ResponseID = responseID
		}
		forceFailedOutput := false
		if eventType == "response.failed" {
			failedMessage = extractOpenAISSEErrorMessage(dataBytes)
			s.parseSSEUsageBytes(dataBytes, usage)
			if hit, code, msg := detectOpenAICyberPolicy(dataBytes); hit {
				MarkOpsCyberPolicy(c, CyberPolicyMark{
					Code: code, Message: msg, Body: truncateString(string(dataBytes), 4096),
					UpstreamStatus: http.StatusOK, UpstreamInTok: usage.InputTokens, UpstreamOutTok: usage.OutputTokens,
				})
			}
			if !clientOutputStarted() {
				if status, errType, errMsg, matched := applyOpenAIStreamFailedErrorPassthroughRule(c, account.Platform, dataBytes, failedMessage); matched {
					sawFailedEvent = true
					if isNativeResponsesProtocolOutput(output) {
						s.recordOpenAIStreamUpstreamError(c, account, false, upstreamRequestID, "http_error", dataBytes, failedMessage)
						MarkResponseCommitted(c)
						c.Writer.Header().Set("Content-Type", "application/json; charset=utf-8")
						c.JSON(status, gin.H{"error": gin.H{"type": errType, "message": errMsg}})
					}
					return fmt.Errorf("upstream response failed: passthrough rule matched message=%s", errMsg)
				}
				if openAIStreamFailedEventShouldFailover(dataBytes, failedMessage) {
					sawFailedEvent = true
					failoverErr := s.newOpenAIStreamFailoverError(c, account, false, upstreamRequestID, dataBytes, failedMessage)
					if guardFirstOutput {
						failoverErr.SafeToFailoverAfterWrite = true
					}
					return failoverErr
				}
			}
			forceFailedOutput = true
			sawFailedEvent = true
		}

		imageCounter.AddSSEData(dataBytes)
		if correctedData, corrected := s.toolCorrector.CorrectToolCallsInSSEBytes(dataBytes); corrected {
			dataBytes = correctedData
			eventType = strings.TrimSpace(gjson.GetBytes(dataBytes, "type").String())
		}
		if imageOutput, found := extractImageGenerationOutputFromSSEData(dataBytes, streamSeenImages); found {
			streamImageOutputs = append(streamImageOutputs, imageOutput)
		}
		if responsesStreamEventMayContributeToOutput(eventType) {
			var streamEvent apicompat.ResponsesStreamEvent
			if err := json.Unmarshal(dataBytes, &streamEvent); err == nil {
				streamOutputAccumulator.ProcessEvent(&streamEvent)
			}
		}
		if normalizedData, normalized := normalizeResponsesStreamingTerminalOutput(dataBytes, streamOutputAccumulator, streamImageOutputs); normalized {
			dataBytes = normalizedData
			eventType = strings.TrimSpace(gjson.GetBytes(dataBytes, "type").String())
		}
		if restoredData, restoreErr := restoreOpenAIResponsesNamespacePayload(c, dataBytes); restoreErr != nil {
			return fmt.Errorf("restore OpenAI namespace response: %w", restoreErr)
		} else if !bytes.Equal(restoredData, dataBytes) {
			dataBytes = restoredData
			eventType = strings.TrimSpace(gjson.GetBytes(dataBytes, "type").String())
		}
		if sanitizedData, sanitized := sanitizeOpenAIResponseFailedEventForClient(dataBytes, eventType, clientOutputStarted()); sanitized {
			dataBytes = sanitizedData
		}
		startsOutput := forceFailedOutput || openAIStreamDataStartsClientOutput(string(dataBytes), eventType)
		terminalEvent := eventType == "response.completed" || eventType == "response.done"
		if startsOutput && !terminalEvent {
			semanticOutputSeen = true
		}
		s.parseSSEUsageBytes(dataBytes, usage)
		if account != nil && account.Platform == PlatformOpenAI &&
			terminalEvent && !sawFailedEvent && !semanticOutputSeen && !clientOutputStarted() &&
			openAIResponsesCompletedEventIsEmpty(dataBytes, usage) {
			failoverErr := newOpenAIResponsesEmptyCompletedFailoverError(c, account, upstreamRequestID)
			if guardFirstOutput {
				failoverErr.SafeToFailoverAfterWrite = true
			}
			return failoverErr
		}
		wasStarted := output.ClientOutputStarted()
		if err := output.WriteStreamEvent(stream.ActualProtocol, dataBytes); err != nil {
			if guardFirstOutput && errors.Is(err, errOpenAIFirstOutputStageLimit) {
				failoverErr := s.newOpenAIStreamFailoverError(
					c, account, false, upstreamRequestID, nil,
					"OpenAI first-output staging limit exceeded",
				)
				failoverErr.SafeToFailoverAfterWrite = true
				return failoverErr
			}
			return err
		}
		if output.ClientOutputStarted() {
			noteTurnStateIfCommitted()
		}
		if firstTokenMs == nil && startsOutput {
			ms := int(time.Since(startTime).Milliseconds())
			firstTokenMs = &ms
			stopFirstOutputTimer()
		}
		if !output.ClientDisconnected() && (output.ClientOutputStarted() || wasStarted) {
			lastDownstreamWriteAt = time.Now()
		}
		return nil
	}

	streamInterval := time.Duration(0)
	keepaliveInterval := time.Duration(0)
	if s.cfg != nil {
		if s.cfg.Gateway.StreamDataIntervalTimeout > 0 {
			streamInterval = time.Duration(s.cfg.Gateway.StreamDataIntervalTimeout) * time.Second
		}
		if s.cfg.Gateway.StreamKeepaliveInterval > 0 {
			keepaliveInterval = time.Duration(s.cfg.Gateway.StreamKeepaliveInterval) * time.Second
		}
	}
	if streamInterval <= 0 && keepaliveInterval <= 0 && firstOutputTimeout <= 0 {
		for {
			record, nextErr := stream.Events.Next(context.Background())
			if errors.Is(nextErr, protocoltransport.ErrSSEDone) {
				sawDone = true
				break
			}
			if errors.Is(nextErr, io.EOF) {
				break
			}
			if result, readErr, done := handleReadError(nextErr); done {
				return result, readErr
			}
			if err := processRecord(record); err != nil {
				return resultWithUsage(), err
			}
		}
		return finalize()
	}

	type streamEvent struct {
		record    protocoltransport.SSERecord
		err       error
		processed chan struct{}
	}
	events := make(chan streamEvent, openAIFirstOutputEventQueueSize(guardFirstOutput))
	done := make(chan struct{})
	sendEvent := func(event streamEvent) bool {
		if guardFirstOutput {
			event.processed = make(chan struct{})
		}
		select {
		case events <- event:
		case <-done:
			return false
		}
		if event.processed == nil {
			return true
		}
		select {
		case <-event.processed:
			return true
		case <-done:
			return false
		}
	}
	markEventProcessed := func(event streamEvent) {
		if event.processed != nil {
			close(event.processed)
		}
	}
	go func() {
		defer close(events)
		for {
			record, nextErr := stream.Events.Next(context.Background())
			if !sendEvent(streamEvent{record: record, err: nextErr}) {
				return
			}
			if nextErr != nil {
				return
			}
		}
	}()
	defer close(done)

	var intervalTicker *time.Ticker
	if streamInterval > 0 {
		intervalTicker = time.NewTicker(streamInterval)
		defer intervalTicker.Stop()
	}
	var intervalCh <-chan time.Time
	if intervalTicker != nil {
		intervalCh = intervalTicker.C
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

	for {
		select {
		case event, open := <-events:
			if !open {
				return finalize()
			}
			if errors.Is(event.err, protocoltransport.ErrSSEDone) {
				sawDone = true
				markEventProcessed(event)
				return finalize()
			}
			if errors.Is(event.err, io.EOF) {
				markEventProcessed(event)
				return finalize()
			}
			if result, readErr, handled := handleReadError(event.err); handled {
				markEventProcessed(event)
				return result, readErr
			}
			if err := processRecord(event.record); err != nil {
				markEventProcessed(event)
				return resultWithUsage(), err
			}
			markEventProcessed(event)

		case <-intervalCh:
			if time.Since(parser.Progress().LastReadAt) < streamInterval {
				continue
			}
			if output.ClientDisconnected() {
				return resultWithUsage(), errors.New("stream usage incomplete after timeout")
			}
			logger.LegacyPrintf("service.openai_gateway", "Stream data interval timeout: account=%d model=%s interval=%s", account.ID, originalModel, streamInterval)
			if s.rateLimitService != nil {
				s.rateLimitService.HandleStreamTimeout(ctx, account, originalModel)
			}
			if guardFirstOutput && firstTokenMs == nil {
				_ = stream.Close()
				for event := range events {
					markEventProcessed(event)
				}
				failoverErr := s.newOpenAIStreamFailoverError(
					c, account, false, upstreamRequestID, nil,
					"OpenAI stream data interval timeout before first output",
				)
				failoverErr.SafeToFailoverAfterWrite = true
				return resultWithUsage(), failoverErr
			}
			if isNativeResponsesProtocolOutput(output) {
				payload := []byte(`{"type":"error","sequence_number":0,"error":{"type":"upstream_error","message":"stream_timeout","code":"stream_timeout"}}`)
				_ = output.WriteStreamEvent(stream.ActualProtocol, payload)
			}
			return resultWithUsage(), errors.New("stream data interval timeout")

		case <-firstOutputCh:
			if firstTokenMs != nil {
				stopFirstOutputTimer()
				continue
			}
			_ = stream.Close()
			for event := range events {
				markEventProcessed(event)
			}
			return resultWithUsage(), s.newOpenAIFirstOutputTimeoutError(
				ctx, c, account, startTime, originalModel, reasoningEffort,
				firstOutputTimeout, "semantic_output", resp.Header,
			)

		case <-keepaliveCh:
			if output.ClientDisconnected() || parser.Progress().InRecord || time.Since(lastDownstreamWriteAt) < keepaliveInterval {
				continue
			}
			if guardFirstOutput && firstTokenMs == nil {
				// Keep the downstream connection alive without committing attempt-local
				// headers or buffered preamble events.
				c.Header("Content-Type", "text/event-stream")
				c.Header("Cache-Control", "no-cache")
				c.Header("Connection", "keep-alive")
				c.Header("X-Accel-Buffering", "no")
				if _, err := c.Writer.Write([]byte(":\n\n")); err != nil {
					return resultWithUsage(), err
				}
				if flusher, ok := c.Writer.(http.Flusher); ok {
					flusher.Flush()
				}
				lastDownstreamWriteAt = time.Now()
				continue
			}
			if err := output.WriteStreamKeepalive(); err != nil {
				return resultWithUsage(), err
			}
			noteTurnStateIfCommitted()
			if !output.ClientDisconnected() {
				lastDownstreamWriteAt = time.Now()
			}
		}
	}
}

func (s *OpenAIGatewayService) handleStructuredResponsesPassthroughStream(
	ctx context.Context,
	resp *http.Response,
	c *gin.Context,
	account *Account,
	startTime time.Time,
	output openAIProtocolOutput,
) (*openaiStreamingResultPassthrough, error) {
	stream, err := s.collectStructuredResponsesStream(resp, startTime, output, true, false)
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
	semanticOutputSeen := false
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
		if restoredData, restoreErr := restoreOpenAIResponsesNamespacePayload(c, dataBytes); restoreErr != nil {
			return resultWithUsage(), fmt.Errorf("restore OpenAI namespace response: %w", restoreErr)
		} else if !bytes.Equal(restoredData, dataBytes) {
			dataBytes = restoredData
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
					if isNativeResponsesProtocolOutput(output) {
						s.recordOpenAIStreamUpstreamError(c, account, true, upstreamRequestID, "http_error", dataBytes, failedMessage)
						MarkResponseCommitted(c)
						c.Writer.Header().Set("Content-Type", "application/json; charset=utf-8")
						c.JSON(status, gin.H{"error": gin.H{"type": errType, "message": errMsg}})
					}
					return resultWithUsage(), fmt.Errorf("upstream response failed: passthrough rule matched message=%s", errMsg)
				}
				if openAIStreamFailedEventShouldFailover(dataBytes, failedMessage) {
					return resultWithUsage(), s.newOpenAIStreamFailoverError(
						c, account, true, upstreamRequestID, dataBytes, failedMessage, stream.Headers,
					)
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
		terminalEvent := eventType == "response.completed" || eventType == "response.done"
		if startsOutput && !terminalEvent {
			semanticOutputSeen = true
		}
		if firstTokenMs == nil && startsOutput {
			ms := int(time.Since(startTime).Milliseconds())
			firstTokenMs = &ms
		}
		s.parseSSEUsageBytes(dataBytes, usage)
		if terminalEvent && !sawFailedEvent && !semanticOutputSeen &&
			!openAIStreamClientOutputStarted(c, output.ClientOutputStarted()) &&
			openAIResponsesCompletedEventIsEmpty(dataBytes, usage) {
			return resultWithUsage(), newOpenAIResponsesEmptyCompletedFailoverError(c, account, upstreamRequestID)
		}
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

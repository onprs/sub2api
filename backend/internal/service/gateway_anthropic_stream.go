package service

import (
	"context"
	"encoding/json"
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
)

func (s *GatewayService) handleStructuredStreamingResponse(
	ctx context.Context,
	resp *http.Response,
	c *gin.Context,
	account *Account,
	pipeline *protocolconv.Pipeline,
	startTime time.Time,
	originalModel, mappedModel string,
) (*streamingResult, error) {
	if s.rateLimitService != nil {
		s.rateLimitService.UpdateSessionWindow(ctx, account, resp.Header)
	}
	stream, parser, err := s.collectAnthropicIdentityStream(resp, startTime)
	if err != nil {
		return nil, err
	}
	defer func() { _ = stream.Close() }()

	session, err := pipeline.NewStreamProcessor(stream.ActualProtocol)
	if err != nil {
		return nil, fmt.Errorf("create Anthropic stream processor: %w", err)
	}
	renderer, err := protocolconv.NewRenderer(protocolconv.ProtocolAnthropic)
	if err != nil {
		return nil, err
	}
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		return nil, errors.New("streaming not supported")
	}

	usage := &ClaudeUsage{}
	var firstTokenMs *int
	clientDisconnected := false
	sawTerminalEvent := false
	headersWritten := false
	lastDataAt := time.Now()
	useNoopDeltaKeepalive := c != nil && c.Request != nil && shouldUseClaudeCodeNoopDeltaKeepalive(c.GetHeader("User-Agent"))
	noopDeltaKeepaliveBlockIndex := -1
	noopDeltaKeepaliveDeltaType := ""

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
	writeFramed := func(framed []byte) error {
		if clientDisconnected {
			return nil
		}
		if err := ensureHeaders(); err != nil {
			return err
		}
		if _, err := c.Writer.Write(framed); err != nil {
			clientDisconnected = true
			logger.LegacyPrintf("service.gateway", "Client disconnected during Anthropic streaming, continuing to drain upstream for billing")
			return nil
		}
		flusher.Flush()
		lastDataAt = time.Now()
		return nil
	}
	writePayloads := func(payloads [][]byte) error {
		framedPayloads := make([][]byte, 0, len(payloads))
		for _, payload := range payloads {
			payload = reverseToolNamesIfPresent(c, payload)
			framed, err := renderer.FrameStreamEvent(payload)
			if err != nil {
				return err
			}
			framedPayloads = append(framedPayloads, framed)
		}
		for _, framed := range framedPayloads {
			if err := writeFramed(framed); err != nil {
				return err
			}
		}
		return nil
	}
	finalize := func() error {
		payloads, _, err := session.Finalize()
		if err != nil {
			return err
		}
		return writePayloads(payloads)
	}
	result := func() *streamingResult {
		return &streamingResult{usage: usage, firstTokenMs: firstTokenMs, clientDisconnect: clientDisconnected}
	}

	errorEventSent := false
	sendErrorEvent := func(reason, message string) {
		if errorEventSent || clientDisconnected {
			return
		}
		errorEventSent = true
		if message == "" {
			message = reason
		}
		body, buildErr := renderer.ErrorBody(http.StatusBadGateway, reason, reason, message)
		if buildErr != nil {
			return
		}
		framed, frameErr := renderer.FrameStreamEvent(body)
		if frameErr != nil {
			return
		}
		_ = writeFramed(framed)
	}

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
	var keepaliveTimer *time.Timer
	if keepaliveInterval > 0 {
		keepaliveTimer = time.NewTimer(keepaliveInterval)
		defer keepaliveTimer.Stop()
	}
	var keepaliveCh <-chan time.Time
	if keepaliveTimer != nil {
		keepaliveCh = keepaliveTimer.C
	}
	resetKeepaliveTimer := func() {
		if keepaliveTimer == nil {
			return
		}
		if !keepaliveTimer.Stop() {
			select {
			case <-keepaliveTimer.C:
			default:
			}
		}
		keepaliveTimer.Reset(keepaliveInterval)
	}

	events := make(chan anthropicStreamReadResult, 16)
	done := make(chan struct{})
	go func() {
		defer close(events)
		for {
			record, nextErr := parser.NextRecord(context.Background())
			select {
			case events <- anthropicStreamReadResult{record: record, err: nextErr}:
			case <-done:
				return
			}
			if nextErr != nil {
				return
			}
		}
	}()
	defer close(done)

	for {
		select {
		case event, ok := <-events:
			if !ok {
				if !sawTerminalEvent {
					return result(), errors.New("stream usage incomplete: missing terminal event")
				}
				if err := finalize(); err != nil {
					return result(), err
				}
				return result(), nil
			}
			if event.err != nil {
				switch {
				case errors.Is(event.err, protocoltransport.ErrSSEDone):
					if err := finalize(); err != nil {
						return result(), err
					}
					return result(), nil
				case errors.Is(event.err, io.EOF):
					if !sawTerminalEvent {
						return result(), errors.New("stream usage incomplete: missing terminal event")
					}
					if err := finalize(); err != nil {
						return result(), err
					}
					return result(), nil
				case sawTerminalEvent:
					if err := finalize(); err != nil {
						return result(), err
					}
					return result(), nil
				case errors.Is(event.err, context.Canceled), errors.Is(event.err, context.DeadlineExceeded):
					clientDisconnected = true
					return result(), fmt.Errorf("stream usage incomplete: %w", event.err)
				case clientDisconnected:
					return result(), fmt.Errorf("stream usage incomplete after disconnect: %w", event.err)
				case isSSERecordTooLarge(event.err):
					logger.LegacyPrintf("service.gateway", "SSE record too large: account=%d max_size=%d error=%v", account.ID, s.maxAnthropicSSERecordSize(), event.err)
					sendErrorEvent("response_too_large", fmt.Sprintf("upstream SSE record exceeded %d bytes", s.maxAnthropicSSERecordSize()))
					return result(), event.err
				default:
					disconnectMsg := "upstream stream disconnected: " + sanitizeStreamError(event.err)
					if !headersWritten && !c.Writer.Written() {
						logger.LegacyPrintf("service.gateway", "Upstream stream read error before any client output (account=%d), failing over: %v", account.ID, event.err)
						body, _ := json.Marshal(map[string]any{
							"type":  "error",
							"error": map[string]string{"type": "upstream_disconnected", "message": disconnectMsg},
						})
						return nil, &UpstreamFailoverError{StatusCode: http.StatusBadGateway, ResponseBody: body, RetryableOnSameAccount: true}
					}
					sendErrorEvent("stream_read_error", disconnectMsg)
					return result(), fmt.Errorf("stream read error: %w", event.err)
				}
			}

			eventType := strings.TrimSpace(event.record.Event)
			if strings.EqualFold(eventType, "error") {
				if clientDisconnected {
					return result(), nil
				}
				return nil, &sseStreamErrorEventError{RawData: string(event.record.Data)}
			}
			if len(event.record.Data) == 0 {
				continue
			}

			payload, usagePatch, terminal, policyErr := s.applyNativeAnthropicStreamPolicy(
				ctx, account, event.record.Data, originalModel, mappedModel,
				useNoopDeltaKeepalive, &noopDeltaKeepaliveBlockIndex, &noopDeltaKeepaliveDeltaType,
			)
			if policyErr != nil {
				return result(), policyErr
			}
			if terminal {
				sawTerminalEvent = true
			}
			if firstTokenMs == nil {
				ms := int(time.Since(startTime).Milliseconds())
				firstTokenMs = &ms
			}
			if usagePatch != nil {
				mergeSSEUsagePatch(usage, usagePatch)
			}
			payloads, _, convertErr := session.Convert(payload)
			if convertErr != nil {
				return result(), convertErr
			}
			if err := writePayloads(payloads); err != nil {
				return result(), err
			}
			if !clientDisconnected {
				resetKeepaliveTimer()
			}

		case <-intervalCh:
			progress := parser.Progress()
			if time.Since(progress.LastReadAt) < streamInterval {
				continue
			}
			if clientDisconnected {
				return result(), errors.New("stream usage incomplete after timeout")
			}
			logger.LegacyPrintf("service.gateway", "Stream data interval timeout: account=%d model=%s interval=%s", account.ID, originalModel, streamInterval)
			if s.rateLimitService != nil {
				s.rateLimitService.HandleStreamTimeout(ctx, account, originalModel)
			}
			sendErrorEvent("stream_timeout", fmt.Sprintf("upstream stream idle for %s", streamInterval))
			return result(), errors.New("stream data interval timeout")

		case <-keepaliveCh:
			if clientDisconnected {
				continue
			}
			if parser.Progress().InRecord {
				resetKeepaliveTimer()
				continue
			}
			if time.Since(lastDataAt) < keepaliveInterval {
				resetKeepaliveTimer()
				continue
			}
			keepalivePayload := []byte(`{"type": "ping"}`)
			if useNoopDeltaKeepalive && noopDeltaKeepaliveBlockIndex >= 0 {
				if payload, ok := buildClaudeCodeNoopDeltaKeepalive(noopDeltaKeepaliveBlockIndex, noopDeltaKeepaliveDeltaType); ok {
					keepalivePayload = payload
				}
			}
			framed, frameErr := renderer.FrameStreamEvent(keepalivePayload)
			if frameErr != nil {
				return result(), frameErr
			}
			if err := writeFramed(framed); err != nil {
				return result(), err
			}
			resetKeepaliveTimer()
		}
	}
}

func isSSERecordTooLarge(err error) bool {
	var tooLarge *protocoltransport.SSERecordTooLargeError
	return errors.As(err, &tooLarge)
}

func (s *GatewayService) maxAnthropicSSERecordSize() int {
	if s != nil && s.cfg != nil && s.cfg.Gateway.MaxLineSize > 0 {
		return s.cfg.Gateway.MaxLineSize
	}
	return defaultMaxLineSize
}

func (s *GatewayService) applyNativeAnthropicStreamPolicy(
	ctx context.Context,
	account *Account,
	payload []byte,
	originalModel, mappedModel string,
	useNoopDeltaKeepalive bool,
	noopBlockIndex *int,
	noopDeltaType *string,
) ([]byte, *sseUsagePatch, bool, error) {
	var event map[string]any
	if err := json.Unmarshal(payload, &event); err != nil {
		return nil, nil, false, fmt.Errorf("decode Anthropic stream event: %w", err)
	}
	eventType, _ := event["type"].(string)
	if eventType == "" {
		return nil, nil, false, errors.New("decode Anthropic stream event: missing type")
	}
	eventChanged := false

	if useNoopDeltaKeepalive {
		switch eventType {
		case "content_block_start":
			if idx, ok := sseEventIndex(event); ok {
				*noopBlockIndex = -1
				*noopDeltaType = ""
				if contentBlock, ok := event["content_block"].(map[string]any); ok {
					blockType, _ := contentBlock["type"].(string)
					if deltaType := claudeCodeKeepaliveDeltaTypeForContentBlock(blockType); deltaType != "" {
						*noopBlockIndex = idx
						*noopDeltaType = deltaType
					}
				}
			}
		case "content_block_delta":
			if idx, ok := sseEventIndex(event); ok {
				if delta, ok := event["delta"].(map[string]any); ok {
					deltaType, _ := delta["type"].(string)
					if claudeCodeKeepaliveFieldForDeltaType(deltaType) != "" {
						*noopBlockIndex = idx
						*noopDeltaType = deltaType
					}
				}
			}
		case "content_block_stop":
			if idx, ok := sseEventIndex(event); ok && idx == *noopBlockIndex {
				*noopBlockIndex = -1
				*noopDeltaType = ""
			}
		case "message_stop":
			*noopBlockIndex = -1
			*noopDeltaType = ""
		}
	}

	var usageObject map[string]any
	switch eventType {
	case "message_start":
		if message, ok := event["message"].(map[string]any); ok {
			usageObject, _ = message["usage"].(map[string]any)
			if originalModel != mappedModel {
				if model, ok := message["model"].(string); ok && model == mappedModel {
					message["model"] = originalModel
					eventChanged = true
				}
			}
		}
	case "message_delta":
		usageObject, _ = event["usage"].(map[string]any)
	}
	if usageObject != nil {
		eventChanged = reconcileCachedTokens(usageObject) || eventChanged
		if overrideTarget, ok := s.resolveCacheTTLUsageOverrideTarget(ctx, account); ok {
			eventChanged = rewriteCacheCreationJSON(usageObject, overrideTarget) || eventChanged
		}
	}
	usagePatch := s.extractSSEUsagePatch(event)
	terminal := eventType == "message_stop"
	if !eventChanged {
		return append([]byte(nil), payload...), usagePatch, terminal, nil
	}
	converted, err := json.Marshal(event)
	if err != nil {
		return nil, nil, false, fmt.Errorf("encode Anthropic stream event: %w", err)
	}
	return converted, usagePatch, terminal, nil
}

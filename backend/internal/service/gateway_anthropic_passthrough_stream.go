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
)

type anthropicStreamReadResult struct {
	record protocoltransport.SSERecord
	err    error
}

func (s *GatewayService) collectAnthropicIdentityStream(resp *http.Response, startTime time.Time) (*protocoltransport.Stream, *protocoltransport.SSEParser, error) {
	if resp == nil || resp.Body == nil {
		return nil, nil, errors.New("nil Anthropic passthrough stream")
	}
	maxRecordSize := s.maxAnthropicSSERecordSize()
	parser := protocoltransport.NewSSEParser(resp.Body, maxRecordSize)
	stream := &protocoltransport.Stream{
		StatusCode:     resp.StatusCode,
		Headers:        filterAnthropicPassthroughResponseHeaders(resp.Header, s.responseHeaderFilter),
		ActualProtocol: protocolconv.ProtocolAnthropic,
		RequestID:      resp.Header.Get("x-request-id"),
		Duration:       time.Since(startTime),
		Events:         parser,
	}
	if err := stream.Validate(); err != nil {
		_ = stream.Close()
		return nil, nil, fmt.Errorf("validate Anthropic passthrough stream: %w", err)
	}
	return stream, parser, nil
}

func (s *GatewayService) handleStructuredStreamingResponseAnthropicAPIKeyPassthrough(
	ctx context.Context,
	resp *http.Response,
	c *gin.Context,
	account *Account,
	pipeline *protocolconv.Pipeline,
	startTime time.Time,
	model string,
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
		return nil, fmt.Errorf("create Anthropic passthrough stream processor: %w", err)
	}
	renderer, err := protocolconv.NewRenderer(protocolconv.ProtocolAnthropic)
	if err != nil {
		return nil, err
	}

	observer := upstreamResponseModelObserverFromContext(c)
	if observer == nil {
		observer = beginUpstreamResponseModelObservation(c)
	}
	usage := &ClaudeUsage{}
	var firstTokenMs *int
	clientDisconnected := false
	sawTerminalEvent := false
	headersWritten := false
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
			payload = reverseToolNamesIfPresent(c, payload)
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
				logger.LegacyPrintf("service.gateway", "[Anthropic passthrough] Client disconnected during streaming, continue draining upstream for usage: account=%d", account.ID)
				return nil
			}
		}
		c.Writer.Flush()
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

	events := make(chan anthropicStreamReadResult, 16)
	done := make(chan struct{})
	go func() {
		defer close(events)
		for {
			record, nextErr := stream.Events.Next(context.Background())
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
	lastDataAt := time.Now()
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

	for {
		select {
		case event, ok := <-events:
			if !ok {
				if sawTerminalEvent {
					if err := finalize(); err != nil {
						return result(), err
					}
					return result(), nil
				}
				return result(), errors.New("stream usage incomplete: missing terminal event")
			}
			if event.err != nil {
				switch {
				case errors.Is(event.err, protocoltransport.ErrSSEDone):
					if err := finalize(); err != nil {
						return result(), err
					}
					return result(), nil
				case errors.Is(event.err, io.EOF):
					if sawTerminalEvent {
						if err := finalize(); err != nil {
							return result(), err
						}
						return result(), nil
					}
					if clientDisconnected && streamInterval > 0 && time.Since(parser.Progress().LastReadAt) >= streamInterval {
						return result(), errors.New("stream usage incomplete after timeout")
					}
					return result(), errors.New("stream usage incomplete: missing terminal event")
				case sawTerminalEvent:
					if err := finalize(); err != nil {
						return result(), err
					}
					return result(), nil
				case clientDisconnected:
					return result(), fmt.Errorf("stream usage incomplete after disconnect: %w", event.err)
				case errors.Is(event.err, context.Canceled), errors.Is(event.err, context.DeadlineExceeded):
					clientDisconnected = true
					return result(), fmt.Errorf("stream usage incomplete: %w", event.err)
				default:
					return result(), fmt.Errorf("stream read error: %w", event.err)
				}
			}

			payload := event.record.Data
			observer.ObserveAnthropic(payload)
			eventType := strings.TrimSpace(event.record.Event)
			if anthropicStreamEventIsTerminal(eventType, string(payload)) {
				sawTerminalEvent = true
			}
			if firstTokenMs == nil {
				ms := int(time.Since(startTime).Milliseconds())
				firstTokenMs = &ms
			}
			s.parseSSEUsagePassthrough(string(payload), usage)
			payloads, _, err := session.Convert(payload)
			if err != nil {
				return result(), err
			}
			if err := writePayloads(payloads); err != nil {
				return result(), err
			}
			if !clientDisconnected {
				lastDataAt = time.Now()
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
			logger.LegacyPrintf("service.gateway", "[Anthropic passthrough] Stream data interval timeout: account=%d model=%s interval=%s", account.ID, model, streamInterval)
			if s.rateLimitService != nil {
				s.rateLimitService.HandleStreamTimeout(ctx, account, model)
			}
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
			framed, err := renderer.FrameStreamEvent([]byte(`{"type": "ping"}`))
			if err != nil {
				return result(), err
			}
			if err := ensureHeaders(); err != nil {
				return result(), err
			}
			if _, err := c.Writer.Write(framed); err != nil {
				clientDisconnected = true
				logger.LegacyPrintf("service.gateway", "[Anthropic passthrough] Client disconnected during keepalive ping, continue draining upstream for usage: account=%d", account.ID)
				continue
			}
			c.Writer.Flush()
			lastDataAt = time.Now()
			resetKeepaliveTimer()
		}
	}
}

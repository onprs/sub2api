package service

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv"
	"github.com/gin-gonic/gin"
)

// ForwardGoogleGenAI converts one Google generateContent request to OpenAI
// Responses, runs the existing OpenAI/Codex provider core, and converts the
// rendered Responses wire back to Google. Scheduling, failover, concurrency,
// and billing remain owned by the handler and provider services.
func (s *OpenAIGatewayService) ForwardGoogleGenAI(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	model string,
	stream bool,
	body []byte,
) (*OpenAIForwardResult, error) {
	if c == nil || c.Writer == nil {
		return nil, errors.New("Google GenAI forward requires a response writer")
	}
	if account == nil || account.Platform != PlatformOpenAI {
		return nil, fmt.Errorf("Google GenAI OpenAI route requires an OpenAI account")
	}

	upstreamModel := account.GetMappedModel(model)
	pipeline, err := protocolconv.NewPipeline(standardProtocolRegistry, protocolconv.PipelineConfig{
		Route: protocolconv.Route{
			Source:         protocolconv.ProtocolGoogleGenAI,
			IntendedTarget: protocolconv.ProtocolOpenAIResponses,
			ClientModel:    model,
			UpstreamModel:  upstreamModel,
			Provider:       account.Platform,
			AccountID:      account.ID,
		},
		Options: protocolconv.Options{LossPolicy: protocolconv.LossError},
	})
	if err != nil {
		return nil, err
	}
	converted, err := pipeline.ConvertRequest(body)
	if err != nil {
		return nil, fmt.Errorf("convert Google GenAI request to OpenAI Responses: %w", err)
	}
	convertedBody, err := setOpenAIResponsesStream(converted.Body, stream)
	if err != nil {
		return nil, err
	}

	originalWriter := c.Writer
	maxRecordSize := defaultMaxLineSize
	if s.cfg != nil && s.cfg.Gateway.MaxLineSize > 0 {
		maxRecordSize = s.cfg.Gateway.MaxLineSize
	}
	adapter, err := newGoogleGenAIResponseAdapter(originalWriter, pipeline, stream, maxRecordSize)
	if err != nil {
		return nil, err
	}
	c.Writer = adapter
	defer func() { c.Writer = originalWriter }()

	result, forwardErr := s.Forward(ctx, c, account, convertedBody)
	if forwardErr != nil {
		return nil, forwardErr
	}
	if err := adapter.Complete(); err != nil {
		return nil, fmt.Errorf("convert OpenAI Responses to Google GenAI: %w", err)
	}
	if result != nil {
		result.Model = model
		result.Stream = stream
		result.ClientDisconnect = adapter.clientDisconnected
	}
	return result, nil
}

func setOpenAIResponsesStream(body []byte, stream bool) ([]byte, error) {
	var value map[string]any
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("decode converted OpenAI Responses request: %w", err)
	}
	if stream {
		value["stream"] = true
	} else {
		delete(value, "stream")
	}
	converted, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode converted OpenAI Responses request: %w", err)
	}
	return converted, nil
}

type googleGenAIResponseAdapter struct {
	original gin.ResponseWriter
	pipeline *protocolconv.Pipeline
	renderer *protocolconv.Renderer
	session  *protocolconv.StreamSession
	stream   bool

	header http.Header
	status int
	raw    bytes.Buffer
	maxRaw int

	headersWritten     bool
	outputSize         int
	clientDisconnected bool
	conversionErr      error
}

func newGoogleGenAIResponseAdapter(
	original gin.ResponseWriter,
	pipeline *protocolconv.Pipeline,
	stream bool,
	maxRecordSize int,
) (*googleGenAIResponseAdapter, error) {
	renderer, err := protocolconv.NewRenderer(protocolconv.ProtocolGoogleGenAI)
	if err != nil {
		return nil, err
	}
	adapter := &googleGenAIResponseAdapter{
		original:   original,
		pipeline:   pipeline,
		renderer:   renderer,
		stream:     stream,
		header:     make(http.Header),
		status:     http.StatusOK,
		maxRaw:     maxRecordSize,
		outputSize: -1,
	}
	if stream {
		adapter.session, err = pipeline.NewStreamProcessor(protocolconv.ProtocolOpenAIResponses)
		if err != nil {
			return nil, err
		}
	}
	return adapter, nil
}

func (w *googleGenAIResponseAdapter) Header() http.Header { return w.header }

func (w *googleGenAIResponseAdapter) WriteHeader(status int) {
	if status >= 100 && status <= 999 && !w.headersWritten {
		w.status = status
	}
}

func (w *googleGenAIResponseAdapter) Write(data []byte) (int, error) {
	if w.conversionErr != nil {
		return 0, w.conversionErr
	}
	if !w.stream && w.raw.Len()+len(data) > w.maxRaw {
		w.conversionErr = fmt.Errorf("buffered protocol output exceeds %d bytes", w.maxRaw)
		return 0, w.conversionErr
	}
	_, _ = w.raw.Write(data)
	if !w.stream && w.outputSize < 0 {
		w.outputSize = 0
	}
	if w.stream {
		w.consumeSSERecords(false)
		if w.conversionErr != nil {
			return 0, w.conversionErr
		}
		if w.raw.Len() > w.maxRaw {
			w.conversionErr = fmt.Errorf("buffered SSE record exceeds %d bytes", w.maxRaw)
			return 0, w.conversionErr
		}
	}
	return len(data), nil
}

func (w *googleGenAIResponseAdapter) WriteString(value string) (int, error) {
	return w.Write([]byte(value))
}

func (w *googleGenAIResponseAdapter) WriteHeaderNow() { w.WriteHeader(w.status) }
func (w *googleGenAIResponseAdapter) Status() int     { return w.status }
func (w *googleGenAIResponseAdapter) Size() int       { return w.outputSize }
func (w *googleGenAIResponseAdapter) Written() bool   { return w.outputSize >= 0 }

func (w *googleGenAIResponseAdapter) Flush() {
	if w.stream {
		w.consumeSSERecords(false)
	}
	if w.headersWritten && !w.clientDisconnected {
		w.original.Flush()
	}
}

func (w *googleGenAIResponseAdapter) CloseNotify() <-chan bool { return w.original.CloseNotify() }
func (w *googleGenAIResponseAdapter) Pusher() http.Pusher      { return w.original.Pusher() }
func (w *googleGenAIResponseAdapter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return w.original.Hijack()
}

func (w *googleGenAIResponseAdapter) Complete() error {
	if w.conversionErr != nil {
		return w.conversionErr
	}
	if !w.stream {
		converted, err := w.pipeline.ConvertResponse(w.raw.Bytes(), protocolconv.ProtocolOpenAIResponses)
		if err != nil {
			return err
		}
		return w.renderer.RenderJSON(w.original, w.status, w.header, converted.Body)
	}

	w.consumeSSERecords(true)
	if w.conversionErr != nil {
		return w.conversionErr
	}
	payloads, _, err := w.session.Finalize()
	if err != nil {
		return err
	}
	if err := w.writeConvertedPayloads(payloads); err != nil {
		return err
	}
	if !w.headersWritten {
		if err := w.renderer.WriteStreamHeaders(w.original, w.status, w.header); err != nil {
			return err
		}
		w.headersWritten = true
		w.outputSize = 0
	}
	if !w.clientDisconnected {
		w.original.Flush()
	}
	return nil
}

func (w *googleGenAIResponseAdapter) consumeSSERecords(final bool) {
	for w.conversionErr == nil {
		record, consumed, ok := nextBufferedSSERecord(w.raw.Bytes(), final)
		if !ok {
			return
		}
		w.raw.Next(consumed)
		data := sseRecordData(record)
		if len(data) == 0 || bytes.Equal(bytes.TrimSpace(data), []byte("[DONE]")) {
			continue
		}
		if !json.Valid(data) {
			w.conversionErr = errors.New("OpenAI Responses stream emitted malformed JSON")
			return
		}
		payloads, _, err := w.session.Convert(data)
		if err != nil {
			w.conversionErr = err
			return
		}
		if err := w.writeConvertedPayloads(payloads); err != nil {
			w.conversionErr = err
			return
		}
	}
}

func (w *googleGenAIResponseAdapter) writeConvertedPayloads(payloads [][]byte) error {
	if w.clientDisconnected || len(payloads) == 0 {
		return nil
	}
	if !w.headersWritten {
		if err := w.renderer.WriteStreamHeaders(w.original, w.status, w.header); err != nil {
			return err
		}
		w.headersWritten = true
		w.outputSize = 0
	}
	for _, payload := range payloads {
		framed, err := w.renderer.FrameStreamEvent(payload)
		if err != nil {
			return err
		}
		n, err := w.original.Write(framed)
		w.outputSize += n
		if err != nil {
			w.clientDisconnected = true
			return nil
		}
	}
	w.original.Flush()
	return nil
}

func nextBufferedSSERecord(data []byte, final bool) ([]byte, int, bool) {
	for index := 0; index < len(data); index++ {
		if data[index] != '\n' {
			continue
		}
		if index+1 < len(data) && data[index+1] == '\n' {
			return data[:index], index + 2, true
		}
		if index > 0 && data[index-1] == '\r' && index+2 < len(data) && data[index+1] == '\r' && data[index+2] == '\n' {
			return data[:index-1], index + 3, true
		}
	}
	if final && len(bytes.TrimSpace(data)) > 0 {
		return data, len(data), true
	}
	return nil, 0, false
}

func sseRecordData(record []byte) []byte {
	var lines [][]byte
	for _, line := range bytes.Split(record, []byte("\n")) {
		line = bytes.TrimSuffix(line, []byte("\r"))
		if len(line) == 0 || line[0] == ':' {
			continue
		}
		field, value, found := bytes.Cut(line, []byte(":"))
		if !found || !bytes.Equal(field, []byte("data")) {
			continue
		}
		value = bytes.TrimPrefix(value, []byte(" "))
		lines = append(lines, value)
	}
	return bytes.Join(lines, []byte("\n"))
}

var _ gin.ResponseWriter = (*googleGenAIResponseAdapter)(nil)

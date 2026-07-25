package transport

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv"
	"github.com/stretchr/testify/require"
)

func TestSSEParserHandlesFieldsCommentsCRLFAndMultipleRecords(t *testing.T) {
	body := strings.Join([]string{
		": keepalive\r\n",
		"event: response.created\r\n",
		"id: event-1\r\n",
		"retry: 1000\r\n",
		"data: {\"type\":\"response.created\",\r\n",
		"data: \"response\":{}}\r\n",
		"\r\n",
		"data: {\"type\":\"response.completed\"}\n\n",
	}, "")
	parser := NewSSEParser(io.NopCloser(strings.NewReader(body)), 1024)
	t.Cleanup(func() { require.NoError(t, parser.Close()) })

	first, err := parser.Next(context.Background())
	require.NoError(t, err)
	require.Equal(t, "response.created", first.Event)
	require.Equal(t, "event-1", first.ID)
	require.Equal(t, "1000", first.Retry)
	require.JSONEq(t, `{"type":"response.created","response":{}}`, string(first.Data))

	second, err := parser.Next(context.Background())
	require.NoError(t, err)
	require.JSONEq(t, `{"type":"response.completed"}`, string(second.Data))
	_, err = parser.Next(context.Background())
	require.ErrorIs(t, err, io.EOF)
}

func TestSSEParserHandlesSplitReads(t *testing.T) {
	reader := &chunkReadCloser{chunks: [][]byte{
		[]byte("event: message_sta"),
		[]byte("rt\ndata: {\"type\":"),
		[]byte("\"message_start\"}\n\n"),
	}}
	parser := NewSSEParser(reader, 1024)
	t.Cleanup(func() { require.NoError(t, parser.Close()) })

	record, err := parser.Next(context.Background())
	require.NoError(t, err)
	require.Equal(t, "message_start", record.Event)
	require.JSONEq(t, `{"type":"message_start"}`, string(record.Data))
}

func TestSSEParserProgressTracksPartialRecord(t *testing.T) {
	reader, writer := io.Pipe()
	parser := NewSSEParser(reader, 1024)
	t.Cleanup(func() { require.NoError(t, parser.Close()) })

	type result struct {
		record SSERecord
		err    error
	}
	resultCh := make(chan result, 1)
	go func() {
		record, err := parser.Next(context.Background())
		resultCh <- result{record: record, err: err}
	}()

	_, err := io.WriteString(writer, `data: {"type":"message_start"}`+"\n")
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		progress := parser.Progress()
		return progress.InRecord && !progress.LastReadAt.IsZero()
	}, time.Second, time.Millisecond)

	_, err = io.WriteString(writer, "\n")
	require.NoError(t, err)
	parsed := <-resultCh
	require.NoError(t, parsed.err)
	require.JSONEq(t, `{"type":"message_start"}`, string(parsed.record.Data))
	require.False(t, parser.Progress().InRecord)
	require.NoError(t, writer.Close())
}

func TestSSEParserDoneSentinelIsTerminal(t *testing.T) {
	parser := NewSSEParser(io.NopCloser(strings.NewReader("data: [DONE]\n\ndata: {}\n\n")), 1024)
	t.Cleanup(func() { require.NoError(t, parser.Close()) })

	_, err := parser.Next(context.Background())
	require.ErrorIs(t, err, ErrSSEDone)
	_, err = parser.Next(context.Background())
	require.ErrorIs(t, err, ErrSSEDone)
}

func TestSSEParserNextRecordPreservesServicePolicyRecords(t *testing.T) {
	parser := NewSSEParser(io.NopCloser(strings.NewReader(strings.Join([]string{
		"event: error\n\n",
		"event: error\ndata: not-json\n\n",
		"event: error\ndata: [DONE]\n\n",
		"data: [DONE]\n\n",
	}, ""))), 1024)
	t.Cleanup(func() { require.NoError(t, parser.Close()) })

	emptyError, err := parser.NextRecord(context.Background())
	require.NoError(t, err)
	require.Equal(t, "error", emptyError.Event)
	require.Empty(t, emptyError.Data)

	rawError, err := parser.NextRecord(context.Background())
	require.NoError(t, err)
	require.Equal(t, "error", rawError.Event)
	require.Equal(t, "not-json", string(rawError.Data))

	doneError, err := parser.NextRecord(context.Background())
	require.NoError(t, err)
	require.Equal(t, "error", doneError.Event)
	require.Equal(t, "[DONE]", string(doneError.Data))

	_, err = parser.NextRecord(context.Background())
	require.ErrorIs(t, err, ErrSSEDone)
	_, err = parser.NextRecord(context.Background())
	require.ErrorIs(t, err, ErrSSEDone)
}

func TestSSEParserNextRemainsStrictAfterRawRecordSupport(t *testing.T) {
	parser := NewSSEParser(io.NopCloser(strings.NewReader("event: error\ndata: not-json\n\n")), 1024)
	t.Cleanup(func() { require.NoError(t, parser.Close()) })

	_, err := parser.Next(context.Background())
	require.ErrorContains(t, err, "malformed SSE JSON")
}

func TestSSEParserRejectsMalformedJSONAndOversizedRecords(t *testing.T) {
	t.Run("malformed JSON", func(t *testing.T) {
		parser := NewSSEParser(io.NopCloser(strings.NewReader("data: {broken}\n\n")), 1024)
		t.Cleanup(func() { require.NoError(t, parser.Close()) })
		_, err := parser.Next(context.Background())
		require.ErrorContains(t, err, "malformed SSE JSON")
	})

	t.Run("oversized", func(t *testing.T) {
		parser := NewSSEParser(io.NopCloser(strings.NewReader("data: {\"value\":\""+strings.Repeat("x", 64)+"\"}\n\n")), 32)
		t.Cleanup(func() { require.NoError(t, parser.Close()) })
		_, err := parser.Next(context.Background())
		require.ErrorContains(t, err, "exceeds 32 bytes")
		var tooLarge *SSERecordTooLargeError
		require.ErrorAs(t, err, &tooLarge)
		require.Equal(t, 32, tooLarge.MaxBytes)
	})
}

func TestSSEParserReturnsPrematureEOFRecordThenEOF(t *testing.T) {
	parser := NewSSEParser(io.NopCloser(strings.NewReader("data: {\"type\":\"partial\"}")), 1024)
	t.Cleanup(func() { require.NoError(t, parser.Close()) })

	record, err := parser.Next(context.Background())
	require.NoError(t, err)
	require.JSONEq(t, `{"type":"partial"}`, string(record.Data))
	_, err = parser.Next(context.Background())
	require.ErrorIs(t, err, io.EOF)
}

func TestSSEParserCancellationClosesBlockedBody(t *testing.T) {
	body := newBlockingReadCloser()
	parser := NewSSEParser(body, 1024)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	_, err := parser.Next(ctx)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Eventually(t, func() bool { return body.closed.Load() }, time.Second, time.Millisecond)
}

func TestStructuredResultsExposeStatusProtocolAndDeterministicClose(t *testing.T) {
	response := Response{
		StatusCode:     http.StatusTooManyRequests,
		ActualProtocol: protocolconv.ProtocolAnthropic,
		Headers:        http.Header{"X-Request-Id": []string{"req-1"}},
		Body:           []byte(`{"type":"error"}`),
	}
	require.NoError(t, response.Validate())
	require.True(t, response.IsError())
	clone := CloneHeaders(response.Headers)
	clone.Set("X-Request-Id", "changed")
	require.Equal(t, "req-1", response.Headers.Get("X-Request-Id"))

	body := &trackingReadCloser{Reader: strings.NewReader("")}
	stream := &Stream{
		StatusCode:     http.StatusOK,
		ActualProtocol: protocolconv.ProtocolOpenAIResponses,
		Events:         NewSSEParser(body, 1024),
	}
	require.NoError(t, stream.Validate())
	require.False(t, stream.IsError())
	require.NoError(t, stream.Close())
	require.NoError(t, stream.Close())
	require.Equal(t, int32(1), body.closeCount.Load())
}

func TestStructuredResultsRejectMissingActualProtocolAndBodyShape(t *testing.T) {
	require.Error(t, (Response{StatusCode: http.StatusOK, Body: []byte(`{}`)}).Validate())
	require.ErrorContains(t, (Response{StatusCode: http.StatusOK, ActualProtocol: protocolconv.ProtocolOpenAIChat}).Validate(), "successful upstream response body is empty")
	require.NoError(t, (Response{StatusCode: http.StatusBadRequest, ActualProtocol: protocolconv.ProtocolOpenAIChat}).Validate())

	success := &Stream{StatusCode: http.StatusOK, ActualProtocol: protocolconv.ProtocolOpenAIChat}
	require.ErrorContains(t, success.Validate(), "no SSE parser")

	upstreamError := &Stream{StatusCode: http.StatusBadRequest, ActualProtocol: protocolconv.ProtocolAnthropic}
	require.ErrorContains(t, upstreamError.Validate(), "no error body")

	body := io.NopCloser(strings.NewReader(`{"error":{}}`))
	upstreamError.ErrorBody = body
	require.NoError(t, upstreamError.Validate())
	require.NoError(t, upstreamError.Close())
}

type chunkReadCloser struct {
	chunks [][]byte
	index  int
}

func (r *chunkReadCloser) Read(p []byte) (int, error) {
	if r.index >= len(r.chunks) {
		return 0, io.EOF
	}
	chunk := r.chunks[r.index]
	r.index++
	return copy(p, chunk), nil
}
func (*chunkReadCloser) Close() error { return nil }

type trackingReadCloser struct {
	io.Reader
	closeCount atomic.Int32
}

func (r *trackingReadCloser) Close() error {
	r.closeCount.Add(1)
	return nil
}

type blockingReadCloser struct {
	closed atomic.Bool
	wait   chan struct{}
}

func newBlockingReadCloser() *blockingReadCloser {
	return &blockingReadCloser{wait: make(chan struct{})}
}

func (r *blockingReadCloser) Read([]byte) (int, error) {
	<-r.wait
	return 0, io.ErrClosedPipe
}

func (r *blockingReadCloser) Close() error {
	if r.closed.CompareAndSwap(false, true) {
		close(r.wait)
		return nil
	}
	return errors.New("already closed")
}

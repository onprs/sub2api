package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv"
	protocoltransport "github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv/transport"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func nativeResponsesOutputTestPipeline(t *testing.T, clientModel, upstreamModel string, stream bool) *protocolconv.Pipeline {
	t.Helper()
	pipeline, err := newOpenAIResponsesIdentityPipeline(&Account{ID: 42, Platform: PlatformOpenAI}, clientModel, upstreamModel)
	require.NoError(t, err)
	body := []byte(`{"model":"` + clientModel + `","input":"hi"}`)
	if stream {
		body = []byte(`{"model":"` + clientModel + `","input":"hi","stream":true}`)
	}
	_, err = pipeline.ConvertRequest(body)
	require.NoError(t, err)
	return pipeline
}

func TestNativeResponsesProtocolOutputBufferedPreservesBytesAndRestoresModel(t *testing.T) {
	recorder := httptest.NewRecorder()
	pipeline := nativeResponsesOutputTestPipeline(t, "client-model", "upstream-model", false)
	output, err := newNativeResponsesProtocolOutput(recorder, pipeline, "client-model", "upstream-model", false)
	require.NoError(t, err)
	body := []byte(" {\n \"id\": \"resp_native\", \"model\": \"upstream-model\", \"output\": [], \"vendor_extension\": {\"opaque\": 1.00}\n}\n")

	err = output.WriteResponse(protocoltransport.Response{
		StatusCode: http.StatusCreated,
		Headers: http.Header{
			"X-Request-Id":                        []string{"rid_native"},
			"X-Codex-Primary-Window-Minutes":      []string{"300"},
			"X-Codex-Secondary-Window-Minutes":    []string{"10080"},
			"X-Codex-Primary-Reset-After-Seconds": []string{"12"},
		},
		Body: body, ActualProtocol: protocolconv.ProtocolOpenAIResponses,
	})

	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"model": "client-model"`)
	require.Contains(t, recorder.Body.String(), `"vendor_extension": {"opaque": 1.00}`)
	require.Equal(t, "rid_native", recorder.Header().Get("X-Request-Id"))
	require.Equal(t, "12", recorder.Header().Get("X-Codex-Primary-Reset-After-Seconds"))
	require.True(t, output.ClientOutputStarted())
}

func TestNativeResponsesProtocolOutputBufferedPreservesExactBytesWithoutModelRewrite(t *testing.T) {
	recorder := httptest.NewRecorder()
	pipeline := nativeResponsesOutputTestPipeline(t, "same-model", "same-model", false)
	output, err := newNativeResponsesProtocolOutput(recorder, pipeline, "same-model", "same-model", false)
	require.NoError(t, err)
	body := []byte(" {\n  \"id\": \"resp_exact\", \"model\": \"same-model\", \"output\": [], \"vendor_extension\": {\"opaque\": 1.00}\n}\n")

	err = output.WriteResponse(protocoltransport.Response{
		StatusCode: http.StatusOK, Body: body, ActualProtocol: protocolconv.ProtocolOpenAIResponses,
	})

	require.NoError(t, err)
	require.Equal(t, body, recorder.Body.Bytes())
}

func TestNativeResponsesProtocolOutputStreamBuffersPreambleAndRendersTerminal(t *testing.T) {
	recorder := httptest.NewRecorder()
	pipeline := nativeResponsesOutputTestPipeline(t, "client-model", "upstream-model", true)
	output, err := newNativeResponsesProtocolOutput(recorder, pipeline, "client-model", "upstream-model", true)
	require.NoError(t, err)
	require.NoError(t, output.WriteStreamHeaders(http.StatusOK, http.Header{"X-Request-Id": []string{"rid_stream"}}, protocolconv.ProtocolOpenAIResponses))

	created := []byte(`{"type":"response.created","response":{"id":"resp_stream","model":"upstream-model"},"vendor_extension":{"opaque":true}}`)
	require.NoError(t, output.WriteStreamEvent(protocolconv.ProtocolOpenAIResponses, created))
	require.False(t, output.ClientOutputStarted())
	require.Equal(t, 0, recorder.Body.Len())

	delta := []byte(`{"type":"response.output_text.delta","delta":"ok"}`)
	require.NoError(t, output.WriteStreamEvent(protocolconv.ProtocolOpenAIResponses, delta))
	require.True(t, output.ClientOutputStarted())
	completed := []byte(`{"type":"response.completed","response":{"id":"resp_stream","model":"upstream-model","output":[],"usage":{"input_tokens":2,"output_tokens":1}}}`)
	require.NoError(t, output.WriteStreamEvent(protocolconv.ProtocolOpenAIResponses, completed))
	require.NoError(t, output.FinalizeStream(protocolconv.ProtocolOpenAIResponses))

	wire := recorder.Body.String()
	require.Contains(t, wire, "event: response.created")
	require.Contains(t, wire, `"model":"client-model"`)
	require.Contains(t, wire, `"vendor_extension":{"opaque":true}`)
	require.Contains(t, wire, "event: response.output_text.delta")
	require.Contains(t, wire, "event: response.completed")
	require.True(t, len(wire) >= len("data: [DONE]\n\n"))
	require.Equal(t, "data: [DONE]\n\n", wire[len(wire)-len("data: [DONE]\n\n"):])
}

func TestNativeResponsesProtocolOutputRejectsMalformedEventBeforeCommit(t *testing.T) {
	recorder := httptest.NewRecorder()
	pipeline := nativeResponsesOutputTestPipeline(t, "client-model", "upstream-model", true)
	output, err := newNativeResponsesProtocolOutput(recorder, pipeline, "client-model", "upstream-model", true)
	require.NoError(t, err)
	require.NoError(t, output.WriteStreamHeaders(http.StatusOK, nil, protocolconv.ProtocolOpenAIResponses))

	err = output.WriteStreamEvent(protocolconv.ProtocolOpenAIResponses, []byte(`{"type":`))
	require.Error(t, err)
	require.Equal(t, 0, recorder.Body.Len())
	require.Equal(t, http.StatusOK, recorder.Code)
	require.False(t, output.ClientOutputStarted())
}

type nativeResponsesReadErrorBody struct {
	payload []byte
	read    bool
}

func (b *nativeResponsesReadErrorBody) Read(p []byte) (int, error) {
	if !b.read {
		b.read = true
		return copy(p, b.payload), nil
	}
	return 0, errors.New("upstream read failed after terminal")
}

func (b *nativeResponsesReadErrorBody) Close() error { return nil }

func TestHandleNativeResponsesStreamingResponseFinalizesAfterTerminalReadError(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	pipeline := nativeResponsesOutputTestPipeline(t, "client-model", "upstream-model", true)
	output, err := newNativeResponsesProtocolOutput(c.Writer, pipeline, "client-model", "upstream-model", true)
	require.NoError(t, err)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body: &nativeResponsesReadErrorBody{payload: []byte(
			"event: response.completed\n" +
				`data: {"type":"response.completed","response":{"id":"resp_terminal","model":"upstream-model","output":[],"usage":{"input_tokens":1,"output_tokens":1}}}` + "\n\n",
		)},
	}
	svc := &OpenAIGatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{MaxLineSize: 1024}}}

	result, err := svc.handleStructuredResponsesPassthroughStream(context.Background(), resp, c, &Account{ID: 42, Platform: PlatformOpenAI}, time.Now(), output)

	require.NoError(t, err)
	require.Equal(t, "resp_terminal", result.responseID)
	require.Contains(t, recorder.Body.String(), "event: response.completed")
	require.Equal(t, "data: [DONE]\n\n", recorder.Body.String()[recorder.Body.Len()-len("data: [DONE]\n\n"):])
}

type nativeResponsesFailingWriter struct {
	gin.ResponseWriter
}

func (w *nativeResponsesFailingWriter) Write([]byte) (int, error) {
	return 0, errors.New("client disconnected")
}

func TestHandleNativeResponsesStreamingResponseParsesMultilineRecords(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	pipeline := nativeResponsesOutputTestPipeline(t, "client-model", "upstream-model", true)
	output, err := newNativeResponsesProtocolOutput(c.Writer, pipeline, "client-model", "upstream-model", true)
	require.NoError(t, err)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}, "X-Request-Id": []string{"rid_multiline"}},
		Body: io.NopCloser(strings.NewReader(strings.Join([]string{
			"event: response.created",
			`data: {"type":"response.created",`,
			`data: "response":{"id":"resp_multiline","model":"upstream-model"}}`,
			"",
			"event: response.completed",
			`data: {"type":"response.completed","response":{"id":"resp_multiline","model":"upstream-model","output":[],"usage":{"input_tokens":3,"output_tokens":2}}}`,
			"",
			"data: [DONE]",
			"",
		}, "\n"))),
	}
	svc := &OpenAIGatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{MaxLineSize: 1024}}}

	result, err := svc.handleStructuredResponsesPassthroughStream(context.Background(), resp, c, &Account{ID: 42, Platform: PlatformOpenAI}, time.Now(), output)

	require.NoError(t, err)
	require.Equal(t, "resp_multiline", result.responseID)
	require.Equal(t, 3, result.usage.InputTokens)
	require.Equal(t, 2, result.usage.OutputTokens)
	require.Contains(t, recorder.Body.String(), "event: response.created")
	require.Contains(t, recorder.Body.String(), `"model":"client-model"`)
	require.Equal(t, "data: [DONE]\n\n", recorder.Body.String()[recorder.Body.Len()-len("data: [DONE]\n\n"):])
}

func TestHandleNativeResponsesStreamingResponseRejectsMalformedRecordBeforeCommit(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	pipeline := nativeResponsesOutputTestPipeline(t, "client-model", "upstream-model", true)
	output, err := newNativeResponsesProtocolOutput(c.Writer, pipeline, "client-model", "upstream-model", true)
	require.NoError(t, err)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader("data: {\"type\":\n\n")),
	}
	svc := &OpenAIGatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{MaxLineSize: 1024}}}

	_, err = svc.handleStructuredResponsesPassthroughStream(context.Background(), resp, c, &Account{ID: 42, Platform: PlatformOpenAI}, time.Now(), output)

	require.Error(t, err)
	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.Equal(t, http.StatusBadGateway, failoverErr.StatusCode)
	require.Contains(t, string(failoverErr.ResponseBody), "malformed SSE JSON payload")
	require.False(t, output.ClientOutputStarted())
	require.Empty(t, recorder.Body.String())
}

func TestHandleNativeResponsesStreamingResponseRejectsOversizedRecordBeforeCommit(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	pipeline := nativeResponsesOutputTestPipeline(t, "client-model", "upstream-model", true)
	output, err := newNativeResponsesProtocolOutput(c.Writer, pipeline, "client-model", "upstream-model", true)
	require.NoError(t, err)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader("data: {\"type\":\"response.output_text.delta\",\"delta\":\"" + strings.Repeat("x", 128) + "\"}\n\n")),
	}
	svc := &OpenAIGatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{MaxLineSize: 64}}}

	_, err = svc.handleStructuredResponsesPassthroughStream(context.Background(), resp, c, &Account{ID: 42, Platform: PlatformOpenAI}, time.Now(), output)

	require.Error(t, err)
	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.Equal(t, http.StatusBadGateway, failoverErr.StatusCode)
	require.Contains(t, string(failoverErr.ResponseBody), "SSE record exceeds 64 bytes")
	require.False(t, output.ClientOutputStarted())
	require.Empty(t, recorder.Body.String())
}

func TestHandleStructuredPassthroughSSEToJSONUsesIdentityPipelineAndRenderer(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	pipeline := nativeResponsesOutputTestPipeline(t, "client-model", "upstream-model", false)
	output, err := newNativeResponsesProtocolOutput(c.Writer, pipeline, "client-model", "upstream-model", false)
	require.NoError(t, err)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type":                        []string{"text/event-stream"},
			"X-Request-Id":                        []string{"rid_sse_unary"},
			"X-Codex-Primary-Reset-After-Seconds": []string{"18"},
		},
		Body: io.NopCloser(strings.NewReader(strings.Join([]string{
			`data: {"type":"response.created","response":{"id":"resp_sse_unary","model":"upstream-model","status":"in_progress","output":[]}}`,
			"",
			`data: {"type":"response.output_text.delta","delta":"hello"}`,
			"",
			`data: {"type":"response.completed","response":{"id":"resp_sse_unary","model":"upstream-model","status":"completed","output":[],"usage":{"input_tokens":5,"output_tokens":2},"vendor_extension":{"opaque":true}}}`,
			"",
		}, "\n"))),
	}
	svc := &OpenAIGatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{MaxLineSize: 2048}}}

	result, err := svc.handleNonStreamingResponsePassthroughWithOutput(
		context.Background(), resp, c, "client-model", "upstream-model", output,
	)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "resp_sse_unary", result.responseID)
	require.Equal(t, 5, result.usage.InputTokens)
	require.Equal(t, 2, result.usage.OutputTokens)
	require.Equal(t, "hello", gjson.Get(recorder.Body.String(), "output.0.content.0.text").String())
	require.Equal(t, "client-model", gjson.Get(recorder.Body.String(), "model").String())
	require.True(t, gjson.Get(recorder.Body.String(), "vendor_extension.opaque").Bool())
	require.Equal(t, "18", recorder.Header().Get("X-Codex-Primary-Reset-After-Seconds"))
	require.NotContains(t, recorder.Body.String(), "data:")
}

func TestHandleStructuredPassthroughSSEToJSONRejectsMalformedBeforeCommit(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	pipeline := nativeResponsesOutputTestPipeline(t, "client-model", "upstream-model", false)
	output, err := newNativeResponsesProtocolOutput(c.Writer, pipeline, "client-model", "upstream-model", false)
	require.NoError(t, err)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader("data: {broken}\n\n")),
	}
	svc := &OpenAIGatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{MaxLineSize: 1024}}}

	result, err := svc.handleNonStreamingResponsePassthroughWithOutput(
		context.Background(), resp, c, "client-model", "upstream-model", output,
	)

	require.ErrorContains(t, err, "malformed SSE JSON payload")
	require.Nil(t, result)
	require.False(t, output.ClientOutputStarted())
	require.Empty(t, recorder.Body.String())
}

func TestNativeResponsesProtocolOutputTracksClientDisconnect(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	writer := &nativeResponsesFailingWriter{ResponseWriter: c.Writer}
	pipeline := nativeResponsesOutputTestPipeline(t, "client-model", "upstream-model", true)
	output, err := newNativeResponsesProtocolOutput(writer, pipeline, "client-model", "upstream-model", true)
	require.NoError(t, err)
	require.NoError(t, output.WriteStreamHeaders(http.StatusOK, nil, protocolconv.ProtocolOpenAIResponses))

	err = output.WriteStreamEvent(protocolconv.ProtocolOpenAIResponses, []byte(`{"type":"response.output_text.delta","delta":"ok"}`))
	require.NoError(t, err)
	require.True(t, output.ClientDisconnected())
}

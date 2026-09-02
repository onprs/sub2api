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

func TestHandleStructuredResponsesStreamEmptyCompletedFailsOverBeforeCommit(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	pipeline := nativeResponsesOutputTestPipeline(t, "gpt-5", "gpt-5", true)
	output, err := newNativeResponsesProtocolOutput(c.Writer, pipeline, "gpt-5", "gpt-5", true)
	require.NoError(t, err)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}, "X-Request-Id": []string{"req_empty"}},
		Body: io.NopCloser(strings.NewReader(
			`data: {"type":"response.created","response":{"id":"resp_empty","model":"gpt-5"}}` + "\n\n" +
				`data: {"type":"response.completed","response":{"id":"resp_empty","model":"gpt-5","output":[]}}` + "\n\n",
		)),
	}
	svc := &OpenAIGatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{MaxLineSize: 1024}}}

	result, err := svc.handleStructuredResponsesStreamWithReasoning(
		context.Background(), resp, c, &Account{ID: 42, Platform: PlatformOpenAI},
		time.Now(), "gpt-5", output, "",
	)

	require.NotNil(t, result)
	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.Equal(t, http.StatusBadGateway, failoverErr.StatusCode)
	require.Equal(t, "req_empty", failoverErr.ResponseHeaders.Get("X-Request-Id"))
	require.Empty(t, recorder.Body.String())
	require.False(t, output.ClientOutputStarted())
}

func TestNativeResponsesProtocolOutputFirstOutputLimitIncludesSemanticEvent(t *testing.T) {
	recorder := httptest.NewRecorder()
	pipeline := nativeResponsesOutputTestPipeline(t, "gpt-5", "gpt-5", true)
	output, err := newNativeResponsesProtocolOutput(recorder, pipeline, "gpt-5", "gpt-5", true)
	require.NoError(t, err)
	require.NoError(t, output.WriteStreamHeaders(http.StatusOK, nil, protocolconv.ProtocolOpenAIResponses))

	created := []byte(`{"type":"response.created","response":{"id":"resp_limit","model":"gpt-5"}}`)
	delta := []byte(`{"type":"response.output_text.delta","delta":"hello"}`)
	output.enableFirstOutputGuard(int64(len(created) + len(delta) - 1))
	require.NoError(t, output.WriteStreamEvent(protocolconv.ProtocolOpenAIResponses, created))

	err = output.WriteStreamEvent(protocolconv.ProtocolOpenAIResponses, delta)
	require.ErrorIs(t, err, errOpenAIFirstOutputStageLimit)
	require.False(t, output.ClientOutputStarted())
	require.Empty(t, recorder.Body.String())
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

func TestForwardNativeResponsesBufferedUsesAttemptPipelineAndStructuredResponse(t *testing.T) {
	responseBody := []byte(" {\n  \"id\":\"resp_native_buffered\",\"model\":\"gpt-5\",\"status\":\"completed\",\"output\":[],\"usage\":{\"input_tokens\":2,\"output_tokens\":1},\"vendor_extension\":{\"opaque\":1.00}\n}\n")
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusCreated,
		Header: http.Header{
			"Content-Type":             []string{"application/vnd.openai+json"},
			"X-Request-Id":             []string{"rid_native_buffered"},
			"X-RateLimit-Limit-Tokens": []string{"60"},
		},
		Body: io.NopCloser(strings.NewReader(string(responseBody))),
	}}
	cfg := &config.Config{}
	cfg.Security.URLAllowlist.Enabled = false
	svc := &OpenAIGatewayService{cfg: cfg, httpUpstream: upstream}
	account := &Account{
		ID: 42, Name: "native-buffered", Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "test-key", "base_url": "https://example.com"},
		Extra:       map[string]any{"use_responses_api": true},
	}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	SetOpenAIClientTransport(c, OpenAIClientTransportHTTP)

	result, err := svc.Forward(context.Background(), c, account, []byte(`{"model":"gpt-5","stream":false,"input":"hi"}`))

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, 2, result.Usage.InputTokens)
	require.Equal(t, 1, result.Usage.OutputTokens)
	require.Equal(t, http.StatusCreated, recorder.Code)
	require.Equal(t, "60", recorder.Header().Get("X-RateLimit-Limit-Tokens"))
	require.Equal(t, responseBody, recorder.Body.Bytes())
}

func TestForwardNativeResponsesStreamingUsesAttemptPipelineAndStructuredTransport(t *testing.T) {
	bodyReader := &googleResponsesTrackingReadCloser{Reader: strings.NewReader(strings.Join([]string{
		"event: response.created",
		`data: {"type":"response.created",`,
		`data: "response":{"id":"resp_native_forward","model":"upstream-model","status":"in_progress","output":[]},"vendor_extension":{"opaque":true}}`,
		"",
		`data: {"type":"response.output_text.delta","delta":"hello"}`,
		"",
		`data: {"type":"response.completed","response":{"id":"resp_native_forward","model":"upstream-model","status":"completed","output":null,"usage":{"input_tokens":5,"output_tokens":2}}}`,
		"",
		"data: [DONE]",
		"",
	}, "\n"))}
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type":             []string{"text/event-stream"},
			"X-Request-Id":             []string{"rid_native_forward"},
			"X-RateLimit-Limit-Tokens": []string{"120"},
			"X-Internal-Secret":        []string{"drop-me"},
		},
		Body: bodyReader,
	}}
	cfg := &config.Config{}
	cfg.Security.URLAllowlist.Enabled = false
	svc := &OpenAIGatewayService{cfg: cfg, httpUpstream: upstream}
	account := &Account{
		ID: 43, Name: "native-responses", Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Concurrency: 1,
		Credentials: map[string]any{
			"api_key": "test-key", "base_url": "https://example.com",
			"model_mapping": map[string]any{"client-model": "upstream-model"},
		},
		Extra: map[string]any{"use_responses_api": true},
	}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	SetOpenAIClientTransport(c, OpenAIClientTransportHTTP)
	requestBody := []byte(`{"model":"client-model","stream":true,"input":"hi"}`)

	result, err := svc.Forward(context.Background(), c, account, requestBody)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "client-model", result.Model)
	require.Equal(t, "upstream-model", result.UpstreamModel)
	require.Equal(t, 5, result.Usage.InputTokens)
	require.Equal(t, 2, result.Usage.OutputTokens)
	require.Equal(t, int32(1), bodyReader.closeCount.Load())
	require.Equal(t, "120", recorder.Header().Get("X-RateLimit-Limit-Tokens"))
	require.Empty(t, recorder.Header().Get("X-Internal-Secret"))
	wire := recorder.Body.String()
	require.Contains(t, wire, "event: response.created")
	require.Contains(t, wire, `"model":"client-model"`)
	require.Contains(t, wire, `"vendor_extension":{"opaque":true}`)
	require.Contains(t, wire, `"text":"hello"`)
	require.Equal(t, "data: [DONE]\n\n", wire[len(wire)-len("data: [DONE]\n\n"):])
}

func TestForwardNativeResponsesStreamingRejectsInvalidRecordsBeforeCommit(t *testing.T) {
	tests := []struct {
		name string
		body string
		max  int
		want string
	}{
		{name: "malformed", body: "data: {broken}\n\n", max: 1024, want: "malformed SSE JSON payload"},
		{name: "oversized", body: `data: {"type":"response.output_text.delta","delta":"` + strings.Repeat("x", 128) + `"}` + "\n\n", max: 64, want: "SSE record exceeds 64 bytes"},
		{name: "premature eof", body: `data: {"type":"response.created","response":{"id":"resp_incomplete"}}` + "\n\n", max: 1024, want: "ended before a terminal event"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			upstream := &httpUpstreamRecorder{resp: &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body:       io.NopCloser(strings.NewReader(test.body)),
			}}
			cfg := &config.Config{Gateway: config.GatewayConfig{MaxLineSize: test.max}}
			cfg.Security.URLAllowlist.Enabled = false
			svc := &OpenAIGatewayService{cfg: cfg, httpUpstream: upstream}
			account := &Account{
				ID: 44, Name: "native-invalid", Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
				Credentials: map[string]any{"api_key": "test-key", "base_url": "https://example.com"},
				Extra:       map[string]any{"use_responses_api": true},
			}
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			SetOpenAIClientTransport(c, OpenAIClientTransportHTTP)

			result, err := svc.Forward(context.Background(), c, account, []byte(`{"model":"gpt-5","stream":true,"input":"hi"}`))

			require.Nil(t, result)
			var failoverErr *UpstreamFailoverError
			require.ErrorAs(t, err, &failoverErr)
			require.Contains(t, string(failoverErr.ResponseBody), test.want)
			require.False(t, c.Writer.Written())
			require.Empty(t, recorder.Body.String())
		})
	}
}

func TestStructuredNativeResponsesPreambleKeepaliveUsesDownstreamIdle(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	pipeline := nativeResponsesOutputTestPipeline(t, "gpt-5", "gpt-5", true)
	output, err := newNativeResponsesProtocolOutput(c.Writer, pipeline, "gpt-5", "gpt-5", true)
	require.NoError(t, err)
	reader, writer := io.Pipe()
	resp := &http.Response{StatusCode: http.StatusOK, Body: reader, Header: http.Header{"Content-Type": []string{"text/event-stream"}}}
	svc := &OpenAIGatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{
		MaxLineSize: 2048, StreamKeepaliveInterval: 1,
	}}}

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = io.WriteString(writer, `data: {"type":"response.created","response":{"id":"resp_keepalive","model":"gpt-5"}}`+"\n\n")
		for i := 0; i < 6; i++ {
			time.Sleep(250 * time.Millisecond)
			_, _ = io.WriteString(writer, `data: {"type":"response.in_progress","response":{"id":"resp_keepalive","model":"gpt-5"}}`+"\n\n")
		}
		_, _ = io.WriteString(writer, `data: {"type":"response.completed","response":{"id":"resp_keepalive","model":"gpt-5","output":[],"usage":{"input_tokens":2,"output_tokens":1}}}`+"\n\n")
		_ = writer.Close()
	}()

	result, err := svc.handleStructuredResponsesStream(context.Background(), resp, c, &Account{ID: 46, Platform: PlatformOpenAI}, time.Now(), "gpt-5", output)
	<-done

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, 2, result.usage.InputTokens)
	require.Equal(t, 1, result.usage.OutputTokens)
	require.Contains(t, recorder.Body.String(), ":\n\n")
	require.Contains(t, recorder.Body.String(), "event: response.completed")
}

func TestStructuredNativeResponsesFirstOutputTimeoutDoesNotLeakAttempt(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	pipeline := nativeResponsesOutputTestPipeline(t, "gpt-5", "gpt-5", true)
	output, err := newNativeResponsesProtocolOutput(c.Writer, pipeline, "gpt-5", "gpt-5", true)
	require.NoError(t, err)
	reader, writer := io.Pipe()
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       reader,
		Header: http.Header{
			"Content-Type": []string{"text/event-stream"},
			"X-Request-Id": []string{"rid-stalled-attempt"},
		},
	}
	svc := &OpenAIGatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{
		MaxLineSize:                     2048,
		StreamKeepaliveInterval:         1,
		OpenAIFirstOutputTimeoutSeconds: 2,
	}}}

	writerDone := make(chan struct{})
	releaseWriter := make(chan struct{})
	go func() {
		defer close(writerDone)
		defer func() { _ = writer.Close() }()
		_, _ = io.WriteString(writer, `data: {"type":"response.created","response":{"id":"resp_stalled","model":"gpt-5"}}`+"\n\n")
		<-releaseWriter
	}()

	started := time.Now()
	result, err := svc.handleStructuredResponsesStreamWithReasoning(
		context.Background(), resp, c, &Account{ID: 47, Platform: PlatformOpenAI},
		started, "gpt-5", output, "low",
	)
	close(releaseWriter)
	<-writerDone

	require.NotNil(t, result)
	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.True(t, failoverErr.SafeToFailoverAfterWrite)
	require.Contains(t, string(failoverErr.ResponseBody), "first_output_timeout")
	require.GreaterOrEqual(t, time.Since(started), 1900*time.Millisecond)
	require.Less(t, time.Since(started), 3*time.Second)
	require.False(t, output.ClientOutputStarted())
	require.NotContains(t, recorder.Body.String(), "data:")
	require.NotContains(t, recorder.Body.String(), "response.created")
	require.Contains(t, recorder.Body.String(), ":\n\n")
	require.Empty(t, recorder.Header().Values("X-Request-Id"))
}

func TestStructuredNativeResponsesStreamIntervalBeforeFirstOutputFailsOverWithoutLeak(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	pipeline := nativeResponsesOutputTestPipeline(t, "gpt-5", "gpt-5", true)
	output, err := newNativeResponsesProtocolOutput(c.Writer, pipeline, "gpt-5", "gpt-5", true)
	require.NoError(t, err)
	reader, writer := io.Pipe()
	resp := &http.Response{StatusCode: http.StatusOK, Body: reader, Header: http.Header{"Content-Type": []string{"text/event-stream"}}}
	svc := &OpenAIGatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{
		MaxLineSize:                     2048,
		StreamDataIntervalTimeout:       1,
		OpenAIFirstOutputTimeoutSeconds: 2,
	}}}

	writerDone := make(chan struct{})
	releaseWriter := make(chan struct{})
	go func() {
		defer close(writerDone)
		defer func() { _ = writer.Close() }()
		_, _ = io.WriteString(writer, `data: {"type":"response.created","response":{"id":"resp_interval","model":"gpt-5"}}`+"\n\n")
		<-releaseWriter
	}()

	started := time.Now()
	result, err := svc.handleStructuredResponsesStreamWithReasoning(
		context.Background(), resp, c, &Account{ID: 49, Platform: PlatformOpenAI},
		started, "gpt-5", output, "low",
	)
	close(releaseWriter)
	<-writerDone

	require.NotNil(t, result)
	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.True(t, failoverErr.SafeToFailoverAfterWrite)
	require.Contains(t, string(failoverErr.ResponseBody), "stream data interval timeout before first output")
	require.GreaterOrEqual(t, time.Since(started), 900*time.Millisecond)
	require.Less(t, time.Since(started), 1800*time.Millisecond)
	require.False(t, output.ClientOutputStarted())
	require.Empty(t, recorder.Body.String())
}

func TestStructuredNativeResponsesFirstOutputDisarmsOnSemanticEvent(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	pipeline := nativeResponsesOutputTestPipeline(t, "gpt-5", "gpt-5", true)
	output, err := newNativeResponsesProtocolOutput(c.Writer, pipeline, "gpt-5", "gpt-5", true)
	require.NoError(t, err)
	reader, writer := io.Pipe()
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       reader,
		Header: http.Header{
			"Content-Type": []string{"text/event-stream"},
			"X-Request-Id": []string{"rid-semantic-attempt"},
		},
	}
	svc := &OpenAIGatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{
		MaxLineSize:                     2048,
		StreamKeepaliveInterval:         1,
		OpenAIFirstOutputTimeoutSeconds: 2,
	}}}

	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		defer func() { _ = writer.Close() }()
		_, _ = io.WriteString(writer, `data: {"type":"response.created","response":{"id":"resp_semantic","model":"gpt-5"}}`+"\n\n")
		time.Sleep(1100 * time.Millisecond)
		_, _ = io.WriteString(writer, `data: {"type":"response.output_text.delta","delta":"hello"}`+"\n\n")
		time.Sleep(1100 * time.Millisecond)
		_, _ = io.WriteString(writer, `data: {"type":"response.completed","response":{"id":"resp_semantic","model":"gpt-5","output":[],"usage":{"input_tokens":3,"output_tokens":1}}}`+"\n\n")
	}()

	result, err := svc.handleStructuredResponsesStreamWithReasoning(
		context.Background(), resp, c, &Account{ID: 48, Platform: PlatformOpenAI},
		time.Now(), "gpt-5", output, "low",
	)
	<-writerDone

	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.firstTokenMs)
	require.Equal(t, 3, result.usage.InputTokens)
	require.Equal(t, 1, result.usage.OutputTokens)
	require.Contains(t, recorder.Body.String(), ":\n\n")
	require.Contains(t, recorder.Body.String(), "event: response.created")
	require.Contains(t, recorder.Body.String(), "event: response.output_text.delta")
	require.Contains(t, recorder.Body.String(), "event: response.completed")
	require.Empty(t, recorder.Header().Values("X-Request-Id"))
}

func TestForwardNativeResponsesStreamingDisconnectDrainsUsage(t *testing.T) {
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body: io.NopCloser(strings.NewReader(strings.Join([]string{
			`data: {"type":"response.created","response":{"id":"resp_native_disconnect","model":"gpt-5","status":"in_progress","output":[]}}`,
			"",
			`data: {"type":"response.output_text.delta","delta":"hello"}`,
			"",
			`data: {"type":"response.completed","response":{"id":"resp_native_disconnect","model":"gpt-5","status":"completed","output":[],"usage":{"input_tokens":9,"output_tokens":4}}}`,
			"",
		}, "\n"))),
	}}
	cfg := &config.Config{}
	cfg.Security.URLAllowlist.Enabled = false
	svc := &OpenAIGatewayService{cfg: cfg, httpUpstream: upstream}
	account := &Account{
		ID: 45, Name: "native-disconnect", Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "test-key", "base_url": "https://example.com"},
		Extra:       map[string]any{"use_responses_api": true},
	}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Writer = &failingGinWriter{ResponseWriter: c.Writer, failAfter: 0}
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	SetOpenAIClientTransport(c, OpenAIClientTransportHTTP)

	result, err := svc.Forward(context.Background(), c, account, []byte(`{"model":"gpt-5","stream":true,"input":"hi"}`))

	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.ClientDisconnect)
	require.Equal(t, 9, result.Usage.InputTokens)
	require.Equal(t, 4, result.Usage.OutputTokens)
}

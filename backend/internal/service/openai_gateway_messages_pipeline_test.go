package service

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai_compat"
	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type bufferedTerminalTrackingReadCloser struct {
	io.Reader
	closeCount atomic.Int32
}

func (r *bufferedTerminalTrackingReadCloser) Close() error {
	r.closeCount.Add(1)
	return nil
}

type bufferedTerminalBlockingReadCloser struct {
	closed atomic.Bool
	wait   chan struct{}
}

func newBufferedTerminalBlockingReadCloser() *bufferedTerminalBlockingReadCloser {
	return &bufferedTerminalBlockingReadCloser{wait: make(chan struct{})}
}

func (r *bufferedTerminalBlockingReadCloser) Read([]byte) (int, error) {
	<-r.wait
	return 0, io.ErrClosedPipe
}

func (r *bufferedTerminalBlockingReadCloser) Close() error {
	if r.closed.CompareAndSwap(false, true) {
		close(r.wait)
	}
	return nil
}

func TestCollectOpenAICompatBufferedTerminalPreservesStructuredRawResponse(t *testing.T) {
	upstreamBody := strings.Join([]string{
		"event: response.completed",
		`data: {"response":{"id":"resp_raw","model":"upstream-model",`,
		`data: "status":"completed","output":[],"vendor_extension":{"opaque":true}},"usage":{"input_tokens":4,"output_tokens":1,"total_tokens":5}}`,
		"",
	}, "\n")
	body := &bufferedTerminalTrackingReadCloser{Reader: strings.NewReader(upstreamBody)}
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"X-Request-Id": []string{"rid_raw"}, "X-Vendor-Secret": []string{"hidden"}},
		Body:       body,
	}

	terminal, err := (&OpenAIGatewayService{}).collectOpenAICompatBufferedTerminal(resp, nil, "raw terminal test", time.Now())
	require.NoError(t, err)
	require.NotNil(t, terminal.Response)
	require.Equal(t, "resp_raw", terminal.Response.ID)
	require.Equal(t, "rid_raw", terminal.Upstream.RequestID)
	require.Equal(t, "resp_raw", terminal.Upstream.ResponseID)
	require.Equal(t, protocolconv.ProtocolOpenAIResponses, terminal.Upstream.ActualProtocol)
	require.JSONEq(t, `{"id":"resp_raw","model":"upstream-model","status":"completed","output":[],"vendor_extension":{"opaque":true}}`, string(terminal.Upstream.Body))
	require.Equal(t, 4, terminal.Usage.InputTokens)
	require.Equal(t, 1, terminal.Usage.OutputTokens)
	require.Equal(t, "rid_raw", terminal.Upstream.Headers.Get("X-Request-Id"))
	require.Empty(t, terminal.Upstream.Headers.Get("X-Vendor-Secret"))
	require.Equal(t, int32(1), body.closeCount.Load())
}

func TestCollectOpenAICompatBufferedTerminalRejectsMalformedAndOversizedRecords(t *testing.T) {
	tests := []struct {
		name string
		body string
		max  int
		want string
	}{
		{name: "malformed", body: "data: {broken}\n\n", max: 1024, want: "malformed SSE JSON payload"},
		{name: "oversized", body: "data: {\"type\":\"response.completed\",\"padding\":\"" + strings.Repeat("x", 64) + "\"}\n\n", max: 32, want: "SSE record exceeds 32 bytes"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			svc := &OpenAIGatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{MaxLineSize: test.max}}}
			resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(test.body))}

			terminal, err := svc.collectOpenAICompatBufferedTerminal(resp, nil, "invalid terminal test", time.Now())

			require.ErrorContains(t, err, test.want)
			require.NotNil(t, terminal)
			require.Nil(t, terminal.Response)
		})
	}
}

func TestCollectOpenAICompatBufferedTerminalTimeoutClosesBlockedBody(t *testing.T) {
	body := newBufferedTerminalBlockingReadCloser()
	resp := &http.Response{StatusCode: http.StatusOK, Body: body}
	svc := &OpenAIGatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{StreamDataIntervalTimeout: 1}}}

	terminal, err := svc.collectOpenAICompatBufferedTerminal(resp, nil, "timeout terminal test", time.Now())

	require.ErrorContains(t, err, "stream data interval timeout")
	require.NotNil(t, terminal)
	require.Eventually(t, body.closed.Load, time.Second, time.Millisecond)
}

func TestPrepareOpenAICompatBufferedResponseBodyPreservesRawAndSupplementsTerminal(t *testing.T) {
	acc := apicompat.NewBufferedResponseAccumulator()
	acc.ProcessEvent(&apicompat.ResponsesStreamEvent{Type: "response.output_text.delta", Delta: "restored"})
	final := &apicompat.ResponsesResponse{
		ID: "resp_raw", Object: "response", Model: "upstream-model", Status: "completed",
		Usage: &apicompat.ResponsesUsage{InputTokens: 4, OutputTokens: 1, TotalTokens: 5},
	}
	raw := []byte(`{"id":"resp_raw","object":"response","model":"upstream-model","status":"completed","output":[],"vendor_extension":{"opaque":true}}`)

	body, err := prepareOpenAICompatBufferedResponseBody(final, raw, acc)
	require.NoError(t, err)
	require.Equal(t, "restored", gjson.GetBytes(body, "output.0.content.0.text").String())
	require.Equal(t, 4, int(gjson.GetBytes(body, "usage.input_tokens").Int()))
	require.True(t, gjson.GetBytes(body, "vendor_extension.opaque").Bool())
}

func newChatResponsesStreamPipelineForTest(t *testing.T) *protocolconv.Pipeline {
	t.Helper()
	pipeline, err := protocolconv.NewPipeline(standardProtocolRegistry, protocolconv.PipelineConfig{
		Route: protocolconv.Route{
			Source: protocolconv.ProtocolOpenAIChat, IntendedTarget: protocolconv.ProtocolOpenAIResponses,
			ClientModel: "client-model", UpstreamModel: "upstream-model", Provider: PlatformOpenAI, AccountID: 72,
		},
		Options: protocolconv.Options{SourceModel: "upstream-model", LossPolicy: protocolconv.LossError},
	})
	require.NoError(t, err)
	_, err = pipeline.ConvertRequest([]byte(`{"model":"client-model","stream":true,"messages":[{"role":"user","content":"hi"}]}`))
	require.NoError(t, err)
	return pipeline
}

func TestHandleChatStreamingResponseStructuredMultilineRecord(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"X-Request-Id": []string{"rid_chat_multiline"}},
		Body: io.NopCloser(strings.NewReader(strings.Join([]string{
			"event: response.created",
			`data: {"response":{"id":"resp_chat_multiline","model":"upstream-model",`,
			`data: "status":"in_progress","output":[]}}`,
			"",
			"event: response.output_text.delta",
			`data: {"delta":"hello"}`,
			"",
			"event: response.completed",
			`data: {"response":{"id":"resp_chat_multiline","model":"upstream-model","status":"completed","output":[],"usage":{"input_tokens":4,"output_tokens":1}}}`,
			"",
		}, "\n"))),
	}
	svc := &OpenAIGatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{MaxLineSize: 2048}}}

	result, err := svc.handleChatStreamingResponse(
		resp, c, &Account{ID: 72, Platform: PlatformOpenAI}, newChatResponsesStreamPipelineForTest(t),
		"client-model", "upstream-model", "upstream-model", time.Now(), 0,
	)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, 4, result.Usage.InputTokens)
	require.Equal(t, 1, result.Usage.OutputTokens)
	require.Contains(t, recorder.Body.String(), `"model":"client-model"`)
	require.Contains(t, recorder.Body.String(), `"content":"hello"`)
	require.Contains(t, recorder.Body.String(), "data: [DONE]")
}

func TestHandleChatStreamingResponseKeepaliveDoesNotSplitPartialRecord(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	reader, writer := io.Pipe()
	resp := &http.Response{StatusCode: http.StatusOK, Body: reader}
	svc := &OpenAIGatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{
		MaxLineSize: 2048, StreamKeepaliveInterval: 1,
	}}}

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = io.WriteString(writer, `data: {"type":"response.created","response":{"id":"resp_chat_partial",`+"\n")
		time.Sleep(1200 * time.Millisecond)
		_, _ = io.WriteString(writer, `data: "model":"upstream-model","status":"in_progress","output":[]}}`+"\n\n")
		_, _ = io.WriteString(writer, `data: {"type":"response.output_text.delta","delta":"ok"}`+"\n\n")
		_, _ = io.WriteString(writer, `data: {"type":"response.completed","response":{"id":"resp_chat_partial","status":"completed","output":[],"usage":{"input_tokens":2,"output_tokens":1}}}`+"\n\n")
		_ = writer.Close()
	}()

	result, err := svc.handleChatStreamingResponse(
		resp, c, &Account{ID: 72, Platform: PlatformOpenAI}, newChatResponsesStreamPipelineForTest(t),
		"client-model", "upstream-model", "upstream-model", time.Now(), 0,
	)
	<-done

	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotContains(t, recorder.Body.String(), ":\n\n")
	require.Contains(t, recorder.Body.String(), "data: [DONE]")
}

func TestHandleChatStreamingResponseRejectsMalformedBeforeCommit(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("data: {broken}\n\n"))}
	svc := &OpenAIGatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{MaxLineSize: 1024}}}

	result, err := svc.handleChatStreamingResponse(
		resp, c, &Account{ID: 72, Platform: PlatformOpenAI}, newChatResponsesStreamPipelineForTest(t),
		"client-model", "upstream-model", "upstream-model", time.Now(), 0,
	)

	require.ErrorContains(t, err, "malformed SSE JSON payload")
	require.NotNil(t, result)
	require.False(t, c.Writer.Written())
	require.Empty(t, recorder.Body.String())
}

func newAnthropicResponsesStreamPipelineForTest(t *testing.T) *protocolconv.Pipeline {
	t.Helper()
	pipeline, err := protocolconv.NewPipeline(standardProtocolRegistry, protocolconv.PipelineConfig{
		Route: protocolconv.Route{
			Source: protocolconv.ProtocolAnthropic, IntendedTarget: protocolconv.ProtocolOpenAIResponses,
			ClientModel: "client-model", UpstreamModel: "upstream-model", Provider: PlatformOpenAI, AccountID: 71,
		},
		Options: protocolconv.Options{SourceModel: "upstream-model", LossPolicy: protocolconv.LossError},
	})
	require.NoError(t, err)
	_, err = pipeline.ConvertRequest([]byte(`{"model":"client-model","max_tokens":32,"stream":true,"messages":[{"role":"user","content":"hi"}]}`))
	require.NoError(t, err)
	return pipeline
}

func TestHandleAnthropicStreamingResponseStructuredMultilineRecord(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"X-Request-Id": []string{"rid_multiline"}},
		Body: io.NopCloser(strings.NewReader(strings.Join([]string{
			"event: response.created",
			`data: {"response":{"id":"resp_multiline","model":"upstream-model",`,
			`data: "status":"in_progress","output":[]}}`,
			"",
			"event: response.output_text.delta",
			`data: {"delta":"hello"}`,
			"",
			"event: response.completed",
			`data: {"response":{"id":"resp_multiline","model":"upstream-model","status":"completed","output":[],"usage":{"input_tokens":3,"output_tokens":1}}}`,
			"",
		}, "\n"))),
	}
	svc := &OpenAIGatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{MaxLineSize: 2048}}}

	result, err := svc.handleAnthropicStreamingResponse(
		resp, c, &Account{ID: 71, Platform: PlatformOpenAI}, newAnthropicResponsesStreamPipelineForTest(t),
		"client-model", "upstream-model", "upstream-model", time.Now(),
	)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "resp_multiline", result.ResponseID)
	require.Equal(t, 3, result.Usage.InputTokens)
	require.Equal(t, 1, result.Usage.OutputTokens)
	require.Contains(t, recorder.Body.String(), "event: message_start")
	require.Contains(t, recorder.Body.String(), "hello")
	require.Contains(t, recorder.Body.String(), "event: message_stop")
	require.Equal(t, "rid_multiline", recorder.Header().Get("X-Request-Id"))
}

func TestHandleAnthropicStreamingResponseKeepaliveDoesNotSplitPartialRecord(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	reader, writer := io.Pipe()
	resp := &http.Response{StatusCode: http.StatusOK, Body: reader}
	svc := &OpenAIGatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{
		MaxLineSize: 2048, StreamKeepaliveInterval: 1,
	}}}

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = io.WriteString(writer, `data: {"type":"response.created","response":{"id":"resp_partial",`+"\n")
		time.Sleep(1200 * time.Millisecond)
		_, _ = io.WriteString(writer, `data: "model":"upstream-model","status":"in_progress","output":[]}}`+"\n\n")
		_, _ = io.WriteString(writer, `data: {"type":"response.output_text.delta","delta":"ok"}`+"\n\n")
		_, _ = io.WriteString(writer, `data: {"type":"response.completed","response":{"id":"resp_partial","status":"completed","output":[],"usage":{"input_tokens":2,"output_tokens":1}}}`+"\n\n")
		_ = writer.Close()
	}()

	result, err := svc.handleAnthropicStreamingResponse(
		resp, c, &Account{ID: 71, Platform: PlatformOpenAI}, newAnthropicResponsesStreamPipelineForTest(t),
		"client-model", "upstream-model", "upstream-model", time.Now(),
	)
	<-done

	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotContains(t, recorder.Body.String(), "event: ping")
	require.Contains(t, recorder.Body.String(), "event: message_stop")
}

func TestHandleAnthropicStreamingResponseTimeoutClosesBlockedBody(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	body := newBufferedTerminalBlockingReadCloser()
	resp := &http.Response{StatusCode: http.StatusOK, Body: body}
	svc := &OpenAIGatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{
		MaxLineSize: 1024, StreamDataIntervalTimeout: 1,
	}}}

	result, err := svc.handleAnthropicStreamingResponse(
		resp, c, &Account{ID: 71, Platform: PlatformOpenAI}, newAnthropicResponsesStreamPipelineForTest(t),
		"client-model", "upstream-model", "upstream-model", time.Now(),
	)

	require.ErrorContains(t, err, "stream data interval timeout")
	require.NotNil(t, result)
	require.Eventually(t, body.closed.Load, time.Second, time.Millisecond)
	require.False(t, c.Writer.Written())
	require.Empty(t, recorder.Body.String())
}

func TestHandleAnthropicStreamingResponseRejectsMalformedBeforeCommit(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader("event: response.created\ndata: {broken}\n\n")),
	}
	svc := &OpenAIGatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{MaxLineSize: 1024}}}

	result, err := svc.handleAnthropicStreamingResponse(
		resp, c, &Account{ID: 71, Platform: PlatformOpenAI}, newAnthropicResponsesStreamPipelineForTest(t),
		"client-model", "upstream-model", "upstream-model", time.Now(),
	)

	require.Error(t, err)
	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.Contains(t, string(failoverErr.ResponseBody), "upstream connection error")
	require.NotContains(t, string(failoverErr.ResponseBody), "{broken}")
	require.NotNil(t, result)
	require.False(t, c.Writer.Written())
	require.Empty(t, recorder.Body.String())
}

func TestForwardAsAnthropic_RequestUsesPipelineMultimodalToolResult(t *testing.T) {
	body := []byte(`{
		"model":"client-model",
		"max_tokens":32,
		"stream":false,
		"messages":[
			{"role":"assistant","content":[{"type":"tool_use","id":"call-image","name":"inspect","input":{}}]},
			{"role":"user","content":[{"type":"tool_result","tool_use_id":"call-image","content":[
				{"type":"text","text":"chart"},
				{"type":"image","source":{"type":"base64","media_type":"image/png","data":"AAAA"}}
			]}]}
		],
		"tools":[{"name":"inspect","input_schema":{"type":"object"}}]
	}`)
	upstreamBody := strings.Join([]string{
		`data: {"type":"response.completed","response":{"id":"resp_multimodal","object":"response","model":"upstream-model","status":"completed","output":[{"type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":8,"output_tokens":2,"total_tokens":10}}}`,
		"",
		"data: [DONE]",
		"",
	}, "\n")
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(upstreamBody)),
	}}
	svc := &OpenAIGatewayService{
		httpUpstream: upstream,
		cfg: &config.Config{Security: config.SecurityConfig{
			URLAllowlist: config.URLAllowlistConfig{Enabled: false},
		}},
	}
	account := &Account{
		ID: 73, Name: "responses-multimodal", Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key": "test-key", "base_url": "https://api.openai.com/v1",
			"model_mapping": map[string]any{"client-model": "upstream-model"},
		},
		Extra: map[string]any{
			openai_compat.ExtraKeyResponsesMode:      string(openai_compat.ResponsesSupportModeAuto),
			openai_compat.ExtraKeyResponsesSupported: true,
		},
	}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))

	result, err := svc.ForwardAsAnthropic(context.Background(), c, account, body, "", "")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, protocolconv.ProtocolOpenAIResponses, result.ActualProtocol)
	require.Equal(t, "function_call", gjson.GetBytes(upstream.lastBody, "input.0.type").String())
	require.Equal(t, "call-image", gjson.GetBytes(upstream.lastBody, "input.0.call_id").String())
	require.Equal(t, "function_call_output", gjson.GetBytes(upstream.lastBody, "input.1.type").String())
	require.Equal(t, "input_text", gjson.GetBytes(upstream.lastBody, "input.1.output.0.type").String())
	require.Equal(t, "chart", gjson.GetBytes(upstream.lastBody, "input.1.output.0.text").String())
	require.Equal(t, "input_image", gjson.GetBytes(upstream.lastBody, "input.1.output.1.type").String())
	require.Equal(t, "data:image/png;base64,AAAA", gjson.GetBytes(upstream.lastBody, "input.1.output.1.image_url").String())
}

func TestForwardAsAnthropic_ResponsesStreamUsesRequestPipeline(t *testing.T) {
	body := []byte(`{"model":"client-model","max_tokens":32,"stream":true,"messages":[{"role":"user","content":"lookup"}],"tools":[{"name":"lookup","input_schema":{"type":"object"}}]}`)
	upstreamBody := strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_pipeline","model":"upstream-model","status":"in_progress","output":[]}}`,
		"",
		`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","call_id":"call_pipeline","name":"lookup","arguments":""}}`,
		"",
		`data: {"type":"response.function_call_arguments.delta","output_index":0,"call_id":"call_pipeline","name":"lookup","delta":"{\"q\":\"x\"}"}`,
		"",
		`data: {"type":"response.output_item.done","output_index":0,"item":{"type":"function_call","call_id":"call_pipeline","name":"lookup","arguments":"{\"q\":\"x\"}"}}`,
		"",
		`data: {"type":"response.completed","response":{"id":"resp_pipeline","object":"response","model":"upstream-model","status":"completed","output":[{"type":"function_call","call_id":"call_pipeline","name":"lookup","arguments":"{\"q\":\"x\"}"}],"usage":{"input_tokens":8,"output_tokens":3,"total_tokens":11,"input_tokens_details":{"cached_tokens":2}}}}`,
		"",
		"data: [DONE]",
		"",
	}, "\n")
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}, "x-request-id": []string{"rid_pipeline"}},
		Body:       io.NopCloser(strings.NewReader(upstreamBody)),
	}}
	svc := &OpenAIGatewayService{
		httpUpstream: upstream,
		cfg: &config.Config{Security: config.SecurityConfig{
			URLAllowlist: config.URLAllowlistConfig{Enabled: false},
		}},
	}
	account := &Account{
		ID: 71, Name: "responses-pipeline", Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key": "test-key", "base_url": "https://api.openai.com/v1",
			"model_mapping": map[string]any{"client-model": "upstream-model"},
		},
		Extra: map[string]any{
			openai_compat.ExtraKeyResponsesMode:      string(openai_compat.ResponsesSupportModeAuto),
			openai_compat.ExtraKeyResponsesSupported: true,
		},
	}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))

	result, err := svc.ForwardAsAnthropic(context.Background(), c, account, body, "", "")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "client-model", result.Model)
	require.Equal(t, "upstream-model", result.UpstreamModel)
	require.Equal(t, 8, result.Usage.InputTokens)
	require.Equal(t, 3, result.Usage.OutputTokens)
	require.Equal(t, 2, result.Usage.CacheReadInputTokens)
	wire := rec.Body.String()
	require.Contains(t, wire, "event: message_start\n")
	require.Contains(t, wire, `"model":"client-model"`)
	require.Contains(t, wire, `"type":"tool_use"`)
	require.Contains(t, wire, `"id":"call_pipeline"`)
	require.Contains(t, wire, `"name":"lookup"`)
	require.Contains(t, wire, `"partial_json":"{\"q\":\"x\"}"`)
	require.Contains(t, wire, `"stop_reason":"tool_use"`)
	require.Contains(t, wire, "event: message_stop\n")
	require.NotContains(t, wire, "data: [DONE]")
}

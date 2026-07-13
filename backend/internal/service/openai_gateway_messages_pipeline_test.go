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

	terminal, err := (&OpenAIGatewayService{}).collectOpenAICompatBufferedTerminal(resp, "raw terminal test", time.Now())
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

			terminal, err := svc.collectOpenAICompatBufferedTerminal(resp, "invalid terminal test", time.Now())

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

	terminal, err := svc.collectOpenAICompatBufferedTerminal(resp, "timeout terminal test", time.Now())

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

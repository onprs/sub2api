package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

type openCodeGoHTTPUpstreamStub struct {
	req   *http.Request
	body  string
	resp  *http.Response
	err   error
	delay time.Duration
}

type openCodeGoCloseTrackingBody struct {
	io.Reader
	closeCount int
}

func (b *openCodeGoCloseTrackingBody) Close() error {
	b.closeCount++
	return nil
}

func (s *openCodeGoHTTPUpstreamStub) Do(req *http.Request, proxyURL string, accountID int64, accountConcurrency int) (*http.Response, error) {
	s.req = req
	if req != nil && req.Body != nil {
		payload, _ := io.ReadAll(req.Body)
		s.body = string(payload)
		req.Body = io.NopCloser(strings.NewReader(s.body))
	}
	if s.err != nil {
		return nil, s.err
	}
	if s.delay > 0 {
		time.Sleep(s.delay)
	}
	if s.resp != nil {
		return s.resp, nil
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"id":"chatcmpl_ok","model":"kimi-k2.7-code","choices":[],"usage":{"prompt_tokens":3,"completion_tokens":2}}`)),
	}, nil
}

func (s *openCodeGoHTTPUpstreamStub) DoWithTLS(req *http.Request, proxyURL string, accountID int64, accountConcurrency int, profile *tlsfingerprint.Profile) (*http.Response, error) {
	return s.Do(req, proxyURL, accountID, accountConcurrency)
}

type testGinContextRecorder struct {
	Context  *gin.Context
	Recorder *httptest.ResponseRecorder
}

func newTestGinContextRecorder(method string, path string, body string) *testGinContextRecorder {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	c.Request = req
	return &testGinContextRecorder{Context: c, Recorder: recorder}
}

func countOpenCodeGoCacheControlBlocks(body string) int {
	count := 0
	countCacheControl := func(value gjson.Result) {
		if value.Get("cache_control.type").String() == "ephemeral" {
			count++
		}
	}
	system := gjson.Get(body, "system")
	if system.IsArray() {
		system.ForEach(func(_, item gjson.Result) bool {
			countCacheControl(item)
			return true
		})
	}
	messages := gjson.Get(body, "messages")
	if messages.IsArray() {
		messages.ForEach(func(_, msg gjson.Result) bool {
			content := msg.Get("content")
			if !content.IsArray() {
				return true
			}
			content.ForEach(func(_, block gjson.Result) bool {
				countCacheControl(block)
				return true
			})
			return true
		})
	}
	tools := gjson.Get(body, "tools")
	if tools.IsArray() {
		tools.ForEach(func(_, tool gjson.Result) bool {
			countCacheControl(tool)
			return true
		})
	}
	return count
}

func assertOpenCodeGoCacheTTL(t *testing.T, body string, path string) {
	t.Helper()
	if got := gjson.Get(body, path+".cache_control.type").String(); got != "ephemeral" {
		t.Fatalf("expected %s cache_control.type=ephemeral, got %q body=%s", path, got, body)
	}
	if got := gjson.Get(body, path+".cache_control.ttl").String(); got != "1h" {
		t.Fatalf("expected %s cache_control.ttl=1h, got %q body=%s", path, got, body)
	}
}

func TestPrepareOpenCodeGoMessagesCacheBody_InjectsSystemToolsAndMessageBreakpoints(t *testing.T) {
	body := []byte(`{
		"model":"qwen3.7-plus",
		"system":"stable project instructions",
		"messages":[
			{"role":"user","content":[{"type":"text","text":"turn 1","cache_control":{"type":"ephemeral","ttl":"5m"}}]},
			{"role":"assistant","content":[{"type":"text","text":"ok"}]},
			{"role":"user","content":[{"type":"text","text":"turn 2"}]},
			{"role":"assistant","content":[{"type":"text","text":"ok"}]},
			{"role":"user","content":[{"type":"text","text":"turn 3","cache_control":{"type":"ephemeral","ttl":"5m"}}]}
		],
		"tools":[{"name":"sessions_list","input_schema":{"type":"object"}}]
	}`)

	out := string(prepareOpenCodeGoMessagesCacheBody(body))

	assertOpenCodeGoCacheTTL(t, out, "system.0")
	assertOpenCodeGoCacheTTL(t, out, "messages.2.content.0")
	assertOpenCodeGoCacheTTL(t, out, "messages.4.content.0")
	assertOpenCodeGoCacheTTL(t, out, "tools.0")
	if gjson.Get(out, "messages.0.content.0.cache_control").Exists() {
		t.Fatalf("expected stale first-turn message cache_control to be stripped, body=%s", out)
	}
	if got := countOpenCodeGoCacheControlBlocks(out); got != 4 {
		t.Fatalf("expected exactly 4 cache_control blocks, got %d body=%s", got, out)
	}
}

func TestPrepareOpenCodeGoMessagesCacheBody_NormalizesSystemArrayCacheTTL(t *testing.T) {
	body := []byte(`{
		"model":"qwen3.7-plus",
		"system":[
			{"type":"text","text":"stable project instructions","cache_control":{"type":"ephemeral","ttl":"5m"}},
			{"type":"thinking","thinking":"skip me"},
			{"type":"text","text":"latest stable instructions"}
		],
		"messages":[{"role":"user","content":"hi"}]
	}`)

	out := string(prepareOpenCodeGoMessagesCacheBody(body))

	assertOpenCodeGoCacheTTL(t, out, "system.0")
	assertOpenCodeGoCacheTTL(t, out, "system.2")
	if gjson.Get(out, "system.1.cache_control").Exists() {
		t.Fatalf("expected thinking system block not to receive cache_control, body=%s", out)
	}
	assertOpenCodeGoCacheTTL(t, out, "messages.0.content.0")
	if got := countOpenCodeGoCacheControlBlocks(out); got != 3 {
		t.Fatalf("expected 3 cache_control blocks, got %d body=%s", got, out)
	}
}

func TestPrepareOpenCodeGoMessagesCacheBody_StripsDriftingClientMessageAnchors(t *testing.T) {
	body := []byte(`{
		"model":"qwen3.7-plus",
		"messages":[
			{"role":"user","content":[{"type":"text","text":"old anchor","cache_control":{"type":"ephemeral","ttl":"1h"}}]},
			{"role":"assistant","content":[{"type":"text","text":"ok"}]},
			{"role":"user","content":[{"type":"text","text":"stable anchor"}]},
			{"role":"assistant","content":[{"type":"text","text":"ok"}]},
			{"role":"user","content":[{"type":"text","text":"latest anchor","cache_control":{"type":"ephemeral","ttl":"5m"}}]}
		]
	}`)

	out := string(prepareOpenCodeGoMessagesCacheBody(body))

	if gjson.Get(out, "messages.0.content.0.cache_control").Exists() {
		t.Fatalf("expected stale old anchor to be removed, body=%s", out)
	}
	assertOpenCodeGoCacheTTL(t, out, "messages.2.content.0")
	assertOpenCodeGoCacheTTL(t, out, "messages.4.content.0")
	if got := countOpenCodeGoCacheControlBlocks(out); got != 2 {
		t.Fatalf("expected exactly 2 regenerated message cache_control blocks, got %d body=%s", got, out)
	}
}

func TestNewOpenCodeGoPipelineRequestKeepsRequestSignatureLossStrict(t *testing.T) {
	_, _, err := newOpenCodeGoPipelineRequest(
		[]byte(`{"model":"minimax-m2.7","max_tokens":32,"messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"plan","signature":"provider-signature"}]},{"role":"user","content":"continue"}]}`),
		protocolconv.ProtocolAnthropic,
		protocolconv.ProtocolOpenAIChat,
		nil,
		"minimax-m2.7",
		"minimax-m2.7",
	)
	var conversionErr *protocolconv.Error
	if !errors.As(err, &conversionErr) {
		t.Fatalf("expected typed request conversion error, got %v", err)
	}
	if conversionErr.Code != protocolconv.ErrorUnsupportedCapability || conversionErr.Capability != protocolconv.CapabilitySignature {
		t.Fatalf("expected strict signature loss error, got %+v", conversionErr)
	}
}

func TestOpenCodeGoGatewayServiceBufferedAnthropicToChatDropsReasoningSignatureWithWarning(t *testing.T) {
	svc := &OpenCodeGoGatewayService{cfg: &config.Config{}}
	pipeline, _, err := newOpenCodeGoPipelineRequest(
		[]byte(`{"model":"minimax-m2.7","messages":[{"role":"user","content":"hi"}],"stream":false}`),
		protocolconv.ProtocolOpenAIChat,
		protocolconv.ProtocolAnthropic,
		nil,
		"minimax-m2.7",
		"minimax-m2.7",
	)
	if err != nil {
		t.Fatalf("newOpenCodeGoPipelineRequest error: %v", err)
	}
	body := `{"id":"msg_minimax","type":"message","role":"assistant","model":"minimax-m2.7","content":[{"type":"thinking","thinking":"plan","signature":"provider-signature"},{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":4,"output_tokens":2}}`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}, "X-Request-Id": []string{"rid_minimax"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
	rec := newTestGinContextRecorder(http.MethodPost, "/v1/chat/completions", "")

	result, err := svc.bufferAnthropicToChat(rec.Context, resp, pipeline, "minimax-m2.7", "minimax-m2.7", time.Now())
	if err != nil {
		t.Fatalf("bufferAnthropicToChat error: %v", err)
	}
	wire := rec.Recorder.Body.String()
	if got := gjson.Get(wire, "choices.0.message.reasoning_content").String(); got != "plan" {
		t.Fatalf("expected reasoning_content plan, got %q body=%s", got, wire)
	}
	if got := gjson.Get(wire, "choices.0.message.content").String(); got != "ok" {
		t.Fatalf("expected visible content ok, got %q body=%s", got, wire)
	}
	if strings.Contains(wire, "provider-signature") || strings.Contains(wire, `"signature"`) {
		t.Fatalf("Chat response must not expose Anthropic signature fields: %s", wire)
	}
	warnings := pipeline.Warnings()
	if len(warnings) != 1 || warnings[0].Code != protocolconv.WarningDroppedField || warnings[0].Capability != protocolconv.CapabilitySignature {
		t.Fatalf("expected one structured signature warning, got %+v", warnings)
	}
	if result == nil || result.RequestID != "rid_minimax" || result.Usage.InputTokens != 4 || result.Usage.OutputTokens != 2 {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestOpenCodeGoGatewayServiceStreamingAnthropicToChatDropsReasoningSignatureWithWarning(t *testing.T) {
	svc := &OpenCodeGoGatewayService{cfg: &config.Config{}}
	pipeline, _, err := newOpenCodeGoPipelineRequest(
		[]byte(`{"model":"minimax-m2.7","messages":[{"role":"user","content":"hi"}],"stream":true}`),
		protocolconv.ProtocolOpenAIChat,
		protocolconv.ProtocolAnthropic,
		nil,
		"minimax-m2.7",
		"minimax-m2.7",
	)
	if err != nil {
		t.Fatalf("newOpenCodeGoPipelineRequest error: %v", err)
	}
	upstreamBody := strings.Join([]string{
		`event: message_start`,
		`data: {"type":"message_start","message":{"id":"msg_minimax","type":"message","role":"assistant","model":"minimax-m2.7","content":[],"usage":{"input_tokens":4,"output_tokens":0}}}`,
		"",
		`event: content_block_start`,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`,
		"",
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"plan"}}`,
		"",
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"provider-signature"}}`,
		"",
		`event: content_block_stop`,
		`data: {"type":"content_block_stop","index":0}`,
		"",
		`event: content_block_start`,
		`data: {"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`,
		"",
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"ok"}}`,
		"",
		`event: content_block_stop`,
		`data: {"type":"content_block_stop","index":1}`,
		"",
		`event: message_delta`,
		`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2}}`,
		"",
		`event: message_stop`,
		`data: {"type":"message_stop"}`,
		"",
	}, "\n")
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}, "X-Request-Id": []string{"rid_minimax_stream"}},
		Body:       io.NopCloser(strings.NewReader(upstreamBody)),
	}
	rec := newTestGinContextRecorder(http.MethodPost, "/v1/chat/completions", "")

	result, err := svc.streamAnthropicToChat(rec.Context, resp, pipeline, "minimax-m2.7", "minimax-m2.7", time.Now())
	if err != nil {
		t.Fatalf("streamAnthropicToChat error: %v", err)
	}
	wire := rec.Recorder.Body.String()
	if !strings.Contains(wire, `"reasoning_content":"plan"`) || !strings.Contains(wire, `"content":"ok"`) || !strings.Contains(wire, "data: [DONE]") {
		t.Fatalf("expected completed Chat reasoning/text stream, got %s", wire)
	}
	if strings.Contains(wire, "provider-signature") || strings.Contains(wire, `"signature"`) {
		t.Fatalf("Chat stream must not expose Anthropic signature fields: %s", wire)
	}
	warnings := pipeline.Warnings()
	if len(warnings) != 1 || warnings[0].Code != protocolconv.WarningDroppedField || warnings[0].Capability != protocolconv.CapabilitySignature {
		t.Fatalf("expected one structured signature warning, got %+v", warnings)
	}
	if result == nil || result.RequestID != "rid_minimax_stream" || result.Usage.InputTokens != 4 || result.Usage.OutputTokens != 2 {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestOpenCodeGoGatewayServiceIdentityBufferedResponsePreservesRawJSON(t *testing.T) {
	svc := &OpenCodeGoGatewayService{cfg: &config.Config{}}
	pipeline, _, err := newOpenCodeGoPipelineRequest(
		[]byte(`{"model":"client-model","messages":[{"role":"user","content":"hi"}]}`),
		"openai_chat_completions",
		"openai_chat_completions",
		nil,
		"client-model",
		"upstream-model",
	)
	if err != nil {
		t.Fatalf("newOpenCodeGoPipelineRequest error: %v", err)
	}
	body := []byte(" {\n \"id\": \"chatcmpl_raw\", \"model\": \"upstream-model\", \"choices\": [], \"vendor_extension\": {\"opaque\": 1.00}\n}\n")
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}, "X-Request-Id": []string{"rid_raw"}},
		Body:       io.NopCloser(strings.NewReader(string(body))),
	}
	rec := newTestGinContextRecorder(http.MethodPost, "/v1/chat/completions", "")

	result, err := svc.bufferChatPassthrough(rec.Context, resp, pipeline, "client-model", "upstream-model", time.Now())
	if err != nil {
		t.Fatalf("bufferChatPassthrough error: %v", err)
	}
	if got := rec.Recorder.Body.Bytes(); string(got) != string(body) {
		t.Fatalf("response bytes changed:\n got: %q\nwant: %q", got, body)
	}
	if result == nil || result.RequestID != "rid_raw" || result.ActualProtocol != protocolconv.ProtocolOpenAIChat {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestOpenCodeGoGatewayServiceIdentityChatStreamPreservesVendorPayload(t *testing.T) {
	svc := &OpenCodeGoGatewayService{cfg: &config.Config{}}
	pipeline, _, err := newOpenCodeGoPipelineRequest(
		[]byte(`{"model":"client-model","messages":[{"role":"user","content":"hi"}],"stream":true}`),
		"openai_chat_completions",
		"openai_chat_completions",
		nil,
		"client-model",
		"upstream-model",
	)
	if err != nil {
		t.Fatalf("newOpenCodeGoPipelineRequest error: %v", err)
	}
	payload := `{"id":"chatcmpl_stream","object":"chat.completion.chunk","model":"upstream-model","choices":[{"index":0,"delta":{"content":"ok"}}],"vendor_extension":{"opaque":true}}`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader("data: " + payload + "\n\ndata: [DONE]\n\n")),
	}
	rec := newTestGinContextRecorder(http.MethodPost, "/v1/chat/completions", "")

	result, err := svc.streamChatPassthrough(rec.Context, resp, pipeline, "client-model", "upstream-model", time.Now())
	if err != nil {
		t.Fatalf("streamChatPassthrough error: %v", err)
	}
	want := "data: " + payload + "\n\ndata: [DONE]\n\n"
	if got := rec.Recorder.Body.String(); got != want {
		t.Fatalf("unexpected stream wire:\n got: %q\nwant: %q", got, want)
	}
	if result == nil || !result.Stream {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestOpenCodeGoGatewayServiceIdentityChatStreamSendsKeepaliveDuringUpstreamSilence(t *testing.T) {
	svc := &OpenCodeGoGatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{StreamKeepaliveInterval: 1}}}
	pipeline, _, err := newOpenCodeGoPipelineRequest(
		[]byte(`{"model":"client-model","messages":[{"role":"user","content":"hi"}],"stream":true}`),
		"openai_chat_completions",
		"openai_chat_completions",
		nil,
		"client-model",
		"upstream-model",
	)
	if err != nil {
		t.Fatalf("newOpenCodeGoPipelineRequest error: %v", err)
	}
	first := `{"id":"chatcmpl_stream","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}`
	arguments := `{"path":"tmp/plan.md","content":"` + strings.Repeat("x", 43*1024) + `"}`
	toolCall := `{"id":"chatcmpl_stream","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_write","type":"function","function":{"name":"write","arguments":` + strconv.Quote(arguments) + `}}]},"finish_reason":null}]}`
	finish := `{"id":"chatcmpl_stream","object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`
	reader, writer := io.Pipe()
	go func() {
		_, _ = io.WriteString(writer, "data: "+first+"\n\n")
		time.Sleep(3 * time.Second)
		_, _ = io.WriteString(writer, "data: "+toolCall+"\n\ndata: "+finish+"\n\ndata: [DONE]\n\n")
		_ = writer.Close()
	}()
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       reader,
	}
	rec := newTestGinContextRecorder(http.MethodPost, "/v1/chat/completions", "")

	result, err := svc.streamChatPassthrough(rec.Context, resp, pipeline, "client-model", "upstream-model", time.Now())
	if err != nil {
		t.Fatalf("streamChatPassthrough error: %v", err)
	}
	if result == nil || result.ClientDisconnect {
		t.Fatalf("unexpected result: %+v", result)
	}
	wire := rec.Recorder.Body.String()
	firstAt := strings.Index(wire, "data: "+first)
	keepaliveAt := strings.Index(wire, ":\n\n")
	toolCallAt := strings.Index(wire, "data: "+toolCall)
	finishAt := strings.Index(wire, "data: "+finish)
	if firstAt < 0 || keepaliveAt <= firstAt || toolCallAt <= keepaliveAt || finishAt <= toolCallAt {
		t.Fatalf("expected a keepalive before the large tool-call event, indexes=%d/%d/%d/%d body_bytes=%d", firstAt, keepaliveAt, toolCallAt, finishAt, len(wire))
	}
	if !strings.HasSuffix(wire, "data: [DONE]\n\n") {
		t.Fatalf("expected terminal sentinel after large tool-call event, body_bytes=%d", len(wire))
	}
}

func TestOpenCodeGoGatewayServiceIdentityChatStreamFramesMultilinePayload(t *testing.T) {
	svc := &OpenCodeGoGatewayService{cfg: &config.Config{}}
	pipeline, _, err := newOpenCodeGoPipelineRequest(
		[]byte(`{"model":"client-model","messages":[{"role":"user","content":"hi"}],"stream":true}`),
		"openai_chat_completions",
		"openai_chat_completions",
		nil,
		"client-model",
		"upstream-model",
	)
	if err != nil {
		t.Fatalf("newOpenCodeGoPipelineRequest error: %v", err)
	}
	payload := "{\n\"id\":\"chatcmpl_stream\",\n\"choices\":[]\n}"
	upstreamWire := "data: {\ndata: \"id\":\"chatcmpl_stream\",\ndata: \"choices\":[]\ndata: }\n\ndata: [DONE]\n\n"
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(upstreamWire)),
	}
	rec := newTestGinContextRecorder(http.MethodPost, "/v1/chat/completions", "")

	_, err = svc.streamChatPassthrough(rec.Context, resp, pipeline, "client-model", "upstream-model", time.Now())
	if err != nil {
		t.Fatalf("streamChatPassthrough error: %v", err)
	}
	want := "data: {\ndata: \"id\":\"chatcmpl_stream\",\ndata: \"choices\":[]\ndata: }\n\ndata: [DONE]\n\n"
	if got := rec.Recorder.Body.String(); got != want {
		t.Fatalf("unexpected stream wire for payload %q:\n got: %q\nwant: %q", payload, got, want)
	}
}

func TestOpenCodeGoGatewayServiceIdentityAnthropicStreamPreservesVendorPayload(t *testing.T) {
	svc := &OpenCodeGoGatewayService{cfg: &config.Config{}}
	pipeline, _, err := newOpenCodeGoPipelineRequest(
		[]byte(`{"model":"client-model","messages":[{"role":"user","content":"hi"}],"stream":true}`),
		"anthropic_messages",
		"anthropic_messages",
		nil,
		"client-model",
		"upstream-model",
	)
	if err != nil {
		t.Fatalf("newOpenCodeGoPipelineRequest error: %v", err)
	}
	start := `{"type":"message_start","message":{"id":"msg_stream","type":"message","role":"assistant","model":"upstream-model","content":[],"usage":{"input_tokens":10,"cache_read_input_tokens":6}},"vendor_extension":{"opaque":true}}`
	delta := `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":3}}`
	stop := `{"type":"message_stop"}`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body: io.NopCloser(strings.NewReader(
			"event: message_start\ndata: " + start + "\n\n" +
				"event: message_delta\ndata: " + delta + "\n\n" +
				"event: message_stop\ndata: " + stop + "\n\n",
		)),
	}
	rec := newTestGinContextRecorder(http.MethodPost, "/v1/messages", "")

	result, err := svc.streamAnthropicPassthrough(rec.Context, resp, pipeline, "client-model", "upstream-model", time.Now())
	if err != nil {
		t.Fatalf("streamAnthropicPassthrough error: %v", err)
	}
	want := "event: message_start\ndata: " + start + "\n\n" +
		"event: message_delta\ndata: " + delta + "\n\n" +
		"event: message_stop\ndata: " + stop + "\n\n"
	if got := rec.Recorder.Body.String(); got != want {
		t.Fatalf("unexpected stream wire:\n got: %q\nwant: %q", got, want)
	}
	if strings.Contains(rec.Recorder.Body.String(), "[DONE]") {
		t.Fatalf("Anthropic stream must not receive a terminal sentinel: %q", rec.Recorder.Body.String())
	}
	if result == nil || result.ActualProtocol != protocolconv.ProtocolAnthropic || result.Usage.InputTokens != 10 || result.Usage.CacheReadInputTokens != 6 || result.Usage.OutputTokens != 3 {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestOpenCodeGoGatewayServiceIdentityChatStreamDrainsUsageAfterClientDisconnect(t *testing.T) {
	svc := &OpenCodeGoGatewayService{cfg: &config.Config{}}
	pipeline, _, err := newOpenCodeGoPipelineRequest(
		[]byte(`{"model":"client-model","messages":[{"role":"user","content":"hi"}],"stream":true}`),
		"openai_chat_completions",
		"openai_chat_completions",
		nil,
		"client-model",
		"upstream-model",
	)
	if err != nil {
		t.Fatalf("newOpenCodeGoPipelineRequest error: %v", err)
	}
	upstreamBody := strings.Join([]string{
		`data: {"id":"chatcmpl_stream","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"ok"}}]}`,
		"",
		`data: {"id":"chatcmpl_stream","object":"chat.completion.chunk","choices":[],"usage":{"prompt_tokens":11,"completion_tokens":4,"prompt_tokens_details":{"cached_tokens":3}}}`,
		"",
		"data: [DONE]",
		"",
	}, "\n")
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(upstreamBody)),
	}
	rec := newTestGinContextRecorder(http.MethodPost, "/v1/chat/completions", "")
	rec.Context.Writer = &openAIChatFailingWriter{ResponseWriter: rec.Context.Writer, failAfter: 0}

	result, err := svc.streamChatPassthrough(rec.Context, resp, pipeline, "client-model", "upstream-model", time.Now())
	if err != nil {
		t.Fatalf("streamChatPassthrough error: %v", err)
	}
	if result == nil || !result.ClientDisconnect {
		t.Fatalf("expected client disconnect result, got %+v", result)
	}
	if result.Usage.InputTokens != 8 || result.Usage.CacheReadInputTokens != 3 || result.Usage.OutputTokens != 4 {
		t.Fatalf("expected terminal usage after disconnect, got %+v", result.Usage)
	}
}

func TestOpenCodeGoGatewayServiceIdentityChatStreamRejectsPrematureEOF(t *testing.T) {
	svc := &OpenCodeGoGatewayService{cfg: &config.Config{}}
	pipeline, _, err := newOpenCodeGoPipelineRequest(
		[]byte(`{"model":"client-model","messages":[{"role":"user","content":"hi"}],"stream":true}`),
		"openai_chat_completions",
		"openai_chat_completions",
		nil,
		"client-model",
		"upstream-model",
	)
	if err != nil {
		t.Fatalf("newOpenCodeGoPipelineRequest error: %v", err)
	}
	payload := `{"id":"chatcmpl_stream","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"partial"}}]}`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader("data: " + payload + "\n\n")),
	}
	rec := newTestGinContextRecorder(http.MethodPost, "/v1/chat/completions", "")

	_, err = svc.streamChatPassthrough(rec.Context, resp, pipeline, "client-model", "upstream-model", time.Now())
	if err == nil || !strings.Contains(err.Error(), "without terminal event") {
		t.Fatalf("expected premature EOF error, got %v", err)
	}
	if strings.Contains(rec.Recorder.Body.String(), "[DONE]") {
		t.Fatalf("premature stream must not receive a synthetic terminal: %q", rec.Recorder.Body.String())
	}
}

func TestOpenCodeGoGatewayServiceIdentityStreamRejectsMalformedJSONBeforeCommit(t *testing.T) {
	svc := &OpenCodeGoGatewayService{cfg: &config.Config{}}
	pipeline, _, err := newOpenCodeGoPipelineRequest(
		[]byte(`{"model":"client-model","messages":[{"role":"user","content":"hi"}],"stream":true}`),
		"openai_chat_completions",
		"openai_chat_completions",
		nil,
		"client-model",
		"upstream-model",
	)
	if err != nil {
		t.Fatalf("newOpenCodeGoPipelineRequest error: %v", err)
	}
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader("data: {\"id\":\n\n")),
	}
	rec := newTestGinContextRecorder(http.MethodPost, "/v1/chat/completions", "")

	_, err = svc.streamChatPassthrough(rec.Context, resp, pipeline, "client-model", "upstream-model", time.Now())
	if err == nil {
		t.Fatal("expected malformed SSE JSON error")
	}
	if rec.Context.Writer.Written() || rec.Recorder.Body.Len() != 0 {
		t.Fatalf("malformed stream must not commit downstream response, status=%d body=%q", rec.Recorder.Code, rec.Recorder.Body.String())
	}
}

func TestOpenCodeGoGatewayServiceForwardChatCompletionsDirectUsesOpenCodeGoEndpoint(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := &openCodeGoHTTPUpstreamStub{}
	svc := &OpenCodeGoGatewayService{
		httpUpstream: upstream,
		cfg:          &config.Config{},
	}
	account := &Account{
		ID:          42,
		Platform:    PlatformOpenCodeGo,
		Type:        AccountTypeAPIKey,
		Concurrency: 1,
		Credentials: map[string]any{
			"api_key":  "ocg-secret",
			"base_url": "https://opencode.ai/zen/go/v1",
			"model_mapping": map[string]any{
				"opencode-go/kimi": "kimi-k2.7-code",
			},
			"model_protocols": map[string]any{
				"kimi-k2.7-code": OpenCodeGoProtocolChatCompletions,
			},
		},
	}
	rec := newTestGinContextRecorder(http.MethodPost, "/v1/chat/completions", `{"model":"opencode-go/kimi","messages":[{"role":"user","content":"hi"}],"stream":false}`)

	result, err := svc.ForwardChatCompletions(context.Background(), rec.Context, account, []byte(`{"model":"opencode-go/kimi","messages":[{"role":"user","content":"hi"}],"stream":false}`))
	if err != nil {
		t.Fatalf("ForwardChatCompletions error: %v", err)
	}
	if result == nil {
		t.Fatalf("expected result")
	}
	if got := upstream.req.URL.String(); got != "https://opencode.ai/zen/go/v1/chat/completions" {
		t.Fatalf("unexpected upstream URL: %s", got)
	}
	if got := upstream.req.Header.Get("Authorization"); got != "Bearer ocg-secret" {
		t.Fatalf("unexpected authorization header: %s", got)
	}
	if !strings.Contains(upstream.body, `"model":"kimi-k2.7-code"`) {
		t.Fatalf("expected upstream model rewrite, body=%s", upstream.body)
	}
	if result.Model != "opencode-go/kimi" || result.UpstreamModel != "kimi-k2.7-code" {
		t.Fatalf("unexpected models: result=%+v", result)
	}
	if result.Usage.InputTokens != 3 || result.Usage.OutputTokens != 2 {
		t.Fatalf("unexpected usage: %+v", result.Usage)
	}
}

func newOpenCodeGoResponsesTestAccount() *Account {
	return &Account{
		ID: 42, Platform: PlatformOpenCodeGo, Type: AccountTypeAPIKey, Concurrency: 1,
		Credentials: map[string]any{
			"api_key": "ocg-secret", "base_url": "https://opencode.ai/zen/go/v1",
			"model_protocols": map[string]any{"gpt-5.6-luna": OpenCodeGoProtocolResponses},
		},
	}
}

func TestOpenCodeGoGatewayServiceResponsesUpstreamBufferedMatrix(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstreamBody := `{"id":"resp_luna","object":"response","model":"gpt-5.6-luna","status":"completed","output":[{"type":"message","id":"msg_luna","role":"assistant","status":"completed","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":100,"output_tokens":20,"total_tokens":120,"input_tokens_details":{"cached_tokens":60,"cache_write_tokens":10}}}`
	tests := []struct {
		name         string
		source       protocolconv.Protocol
		requestBody  string
		responsePath string
	}{
		{name: "chat", source: protocolconv.ProtocolOpenAIChat, requestBody: `{"model":"gpt-5.6-luna","messages":[{"role":"user","content":"hello"}],"stream":false}`, responsePath: "choices.0.message.content"},
		{name: "responses", source: protocolconv.ProtocolOpenAIResponses, requestBody: `{"model":"gpt-5.6-luna","input":"hello","stream":false}`, responsePath: "output.0.content.0.text"},
		{name: "messages", source: protocolconv.ProtocolAnthropic, requestBody: `{"model":"gpt-5.6-luna","messages":[{"role":"user","content":"hello"}],"max_tokens":32,"stream":false}`, responsePath: "content.0.text"},
		{name: "google", source: protocolconv.ProtocolGoogleGenAI, requestBody: `{"contents":[{"role":"user","parts":[{"text":"hello"}]}]}`, responsePath: "candidates.0.content.parts.0.text"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			upstream := &openCodeGoHTTPUpstreamStub{resp: &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}, "X-Request-Id": []string{"rid-luna-buffered"}},
				Body:       io.NopCloser(strings.NewReader(upstreamBody)),
			}}
			svc := &OpenCodeGoGatewayService{httpUpstream: upstream, cfg: &config.Config{}}
			account := newOpenCodeGoResponsesTestAccount()
			rec := newTestGinContextRecorder(http.MethodPost, "/matrix", tt.requestBody)

			var result *ForwardResult
			var err error
			switch tt.source {
			case protocolconv.ProtocolOpenAIChat:
				result, err = svc.ForwardChatCompletions(context.Background(), rec.Context, account, []byte(tt.requestBody))
			case protocolconv.ProtocolOpenAIResponses:
				result, err = svc.ForwardResponses(context.Background(), rec.Context, account, []byte(tt.requestBody), "gpt-5.6-luna")
			case protocolconv.ProtocolAnthropic:
				result, err = svc.ForwardMessages(context.Background(), rec.Context, account, []byte(tt.requestBody))
			case protocolconv.ProtocolGoogleGenAI:
				result, err = svc.ForwardGoogleGenAI(context.Background(), rec.Context, account, []byte(tt.requestBody), "gpt-5.6-luna", "gpt-5.6-luna", false)
			}
			if err != nil {
				t.Fatalf("forward error: %v", err)
			}
			if got := upstream.req.URL.String(); got != "https://opencode.ai/zen/go/v1/responses" {
				t.Fatalf("unexpected upstream URL: %s", got)
			}
			if got := upstream.req.Header.Get("Authorization"); got != "Bearer ocg-secret" {
				t.Fatalf("unexpected authorization header: %q", got)
			}
			if upstream.req.Header.Get("x-api-key") != "" || upstream.req.Header.Get("anthropic-version") != "" {
				t.Fatalf("Responses upstream must not receive Messages auth headers: %v", upstream.req.Header)
			}
			if gjson.Get(upstream.body, "model").String() != "gpt-5.6-luna" || !gjson.Get(upstream.body, "input").Exists() || !strings.Contains(upstream.body, "hello") {
				t.Fatalf("unexpected Responses request body: %s", upstream.body)
			}
			if got := gjson.GetBytes(rec.Recorder.Body.Bytes(), tt.responsePath).String(); got != "ok" {
				t.Fatalf("response semantic mismatch at %s: got=%q body=%s", tt.responsePath, got, rec.Recorder.Body.String())
			}
			if result == nil || result.ActualProtocol != protocolconv.ProtocolOpenAIResponses || result.RequestID != "rid-luna-buffered" {
				t.Fatalf("unexpected result: %+v", result)
			}
			if result.Usage.InputTokens != 30 || result.Usage.CacheReadInputTokens != 60 || result.Usage.CacheCreationInputTokens != 10 || result.Usage.OutputTokens != 20 {
				t.Fatalf("Responses usage buckets must be mutually exclusive: %+v", result.Usage)
			}
		})
	}
}

func TestOpenCodeGoGatewayServiceResponsesFailedBufferedTriggersFailover(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstreamBody := `{"id":"resp_failed_buffered","object":"response","model":"gpt-5.6-luna","status":"failed","output":[],"error":{"code":"server_is_overloaded","message":"provider overloaded"},"usage":{"input_tokens":12,"output_tokens":0,"total_tokens":12}}`
	upstream := &openCodeGoHTTPUpstreamStub{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}, "X-Request-Id": []string{"rid-luna-failed-buffered"}},
		Body:       io.NopCloser(strings.NewReader(upstreamBody)),
	}}
	svc := &OpenCodeGoGatewayService{httpUpstream: upstream, cfg: &config.Config{}}
	requestBody := `{"model":"gpt-5.6-luna","input":"hello","stream":false}`
	rec := newTestGinContextRecorder(http.MethodPost, "/v1/responses", requestBody)

	result, err := svc.ForwardResponses(context.Background(), rec.Context, newOpenCodeGoResponsesTestAccount(), []byte(requestBody), "gpt-5.6-luna")
	var failoverErr *UpstreamFailoverError
	if !errors.As(err, &failoverErr) {
		t.Fatalf("expected UpstreamFailoverError, got result=%+v err=%T %v", result, err, err)
	}
	if failoverErr.StatusCode != http.StatusServiceUnavailable || !strings.Contains(string(failoverErr.ResponseBody), "provider overloaded") {
		t.Fatalf("unexpected failover error: %+v body=%s", failoverErr, failoverErr.ResponseBody)
	}
	if result != nil {
		t.Fatalf("failed buffered response must not return a successful result: %+v", result)
	}
	if rec.Context.Writer.Written() || rec.Recorder.Body.Len() != 0 {
		t.Fatalf("buffered semantic failure must remain uncommitted before failover: status=%d body=%q", rec.Recorder.Code, rec.Recorder.Body.String())
	}
}

func TestOpenCodeGoGatewayServiceResponsesUpstreamStreamingMatrix(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstreamWire := strings.Join([]string{
		`event: response.created`,
		`data: {"type":"response.created","response":{"id":"resp_luna_stream","object":"response","model":"gpt-5.6-luna","status":"in_progress","output":[]}}`,
		"",
		`event: response.output_item.added`,
		`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"message","id":"msg_luna_stream","role":"assistant","status":"in_progress","content":[]}}`,
		"",
		`event: response.output_text.delta`,
		`data: {"type":"response.output_text.delta","output_index":0,"content_index":0,"item_id":"msg_luna_stream","delta":"ok"}`,
		"",
		`event: response.output_item.done`,
		`data: {"type":"response.output_item.done","output_index":0,"item":{"type":"message","id":"msg_luna_stream","role":"assistant","status":"completed","content":[{"type":"output_text","text":"ok"}]}}`,
		"",
		`event: response.completed`,
		`data: {"type":"response.completed","response":{"id":"resp_luna_stream","object":"response","model":"gpt-5.6-luna","status":"completed","output":[{"type":"message","id":"msg_luna_stream","role":"assistant","status":"completed","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":100,"output_tokens":20,"total_tokens":120,"input_tokens_details":{"cached_tokens":60,"cache_write_tokens":10}}}}`,
		"",
		`data: [DONE]`,
		"",
	}, "\n")
	tests := []struct {
		name        string
		source      protocolconv.Protocol
		requestBody string
		want        []string
		forbid      []string
	}{
		{name: "chat", source: protocolconv.ProtocolOpenAIChat, requestBody: `{"model":"gpt-5.6-luna","messages":[{"role":"user","content":"hello"}],"stream":true}`, want: []string{`"content":"ok"`, "data: [DONE]"}},
		{name: "responses", source: protocolconv.ProtocolOpenAIResponses, requestBody: `{"model":"gpt-5.6-luna","input":"hello","stream":true}`, want: []string{"event: response.created", `"delta":"ok"`, "event: response.completed", "data: [DONE]"}},
		{name: "messages", source: protocolconv.ProtocolAnthropic, requestBody: `{"model":"gpt-5.6-luna","messages":[{"role":"user","content":"hello"}],"max_tokens":32,"stream":true}`, want: []string{"event: message_start", `"text":"ok"`, "event: message_stop"}, forbid: []string{"data: [DONE]"}},
		{name: "google", source: protocolconv.ProtocolGoogleGenAI, requestBody: `{"contents":[{"role":"user","parts":[{"text":"hello"}]}]}`, want: []string{`"text":"ok"`, `"finishReason":"STOP"`, `"usageMetadata"`}, forbid: []string{"event:", "data: [DONE]"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			upstream := &openCodeGoHTTPUpstreamStub{resp: &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}, "X-Request-Id": []string{"rid-luna-stream"}},
				Body:       io.NopCloser(strings.NewReader(upstreamWire)),
			}}
			svc := &OpenCodeGoGatewayService{httpUpstream: upstream, cfg: &config.Config{}}
			account := newOpenCodeGoResponsesTestAccount()
			rec := newTestGinContextRecorder(http.MethodPost, "/matrix", tt.requestBody)

			var result *ForwardResult
			var err error
			switch tt.source {
			case protocolconv.ProtocolOpenAIChat:
				result, err = svc.ForwardChatCompletions(context.Background(), rec.Context, account, []byte(tt.requestBody))
			case protocolconv.ProtocolOpenAIResponses:
				result, err = svc.ForwardResponses(context.Background(), rec.Context, account, []byte(tt.requestBody), "gpt-5.6-luna")
			case protocolconv.ProtocolAnthropic:
				result, err = svc.ForwardMessages(context.Background(), rec.Context, account, []byte(tt.requestBody))
			case protocolconv.ProtocolGoogleGenAI:
				result, err = svc.ForwardGoogleGenAI(context.Background(), rec.Context, account, []byte(tt.requestBody), "gpt-5.6-luna", "gpt-5.6-luna", true)
			}
			if err != nil {
				t.Fatalf("forward error: %v\nwire=%s", err, rec.Recorder.Body.String())
			}
			wire := rec.Recorder.Body.String()
			for _, fragment := range tt.want {
				if !strings.Contains(wire, fragment) {
					t.Fatalf("missing %q in stream: %s", fragment, wire)
				}
			}
			for _, fragment := range tt.forbid {
				if strings.Contains(wire, fragment) {
					t.Fatalf("unexpected %q in stream: %s", fragment, wire)
				}
			}
			if result == nil || !result.Stream || result.ActualProtocol != protocolconv.ProtocolOpenAIResponses || result.FirstTokenMs == nil {
				t.Fatalf("unexpected result: %+v", result)
			}
			if result.Usage.InputTokens != 30 || result.Usage.CacheReadInputTokens != 60 || result.Usage.CacheCreationInputTokens != 10 || result.Usage.OutputTokens != 20 {
				t.Fatalf("Responses stream usage buckets must be mutually exclusive: %+v", result.Usage)
			}
		})
	}
}

func TestOpenCodeGoGatewayServiceResponsesFailedBeforeOutputTriggersFailover(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstreamWire := strings.Join([]string{
		`event: response.created`,
		`data: {"type":"response.created","response":{"id":"resp_failed","object":"response","model":"gpt-5.6-luna","status":"in_progress","output":[]}}`,
		"",
		`event: response.failed`,
		`data: {"type":"response.failed","response":{"id":"resp_failed","object":"response","model":"gpt-5.6-luna","status":"failed","output":[],"error":{"code":"server_is_overloaded","message":"provider overloaded"},"usage":{"input_tokens":12,"output_tokens":0,"total_tokens":12}}}`,
		"",
	}, "\n")
	upstream := &openCodeGoHTTPUpstreamStub{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}, "X-Request-Id": []string{"rid-luna-failed"}},
		Body:       io.NopCloser(strings.NewReader(upstreamWire)),
	}}
	svc := &OpenCodeGoGatewayService{httpUpstream: upstream, cfg: &config.Config{}}
	requestBody := `{"model":"gpt-5.6-luna","input":"hello","stream":true}`
	rec := newTestGinContextRecorder(http.MethodPost, "/v1/responses", requestBody)

	result, err := svc.ForwardResponses(context.Background(), rec.Context, newOpenCodeGoResponsesTestAccount(), []byte(requestBody), "gpt-5.6-luna")
	var failoverErr *UpstreamFailoverError
	if !errors.As(err, &failoverErr) {
		t.Fatalf("expected UpstreamFailoverError, got result=%+v err=%T %v", result, err, err)
	}
	if failoverErr.StatusCode != http.StatusServiceUnavailable || !strings.Contains(string(failoverErr.ResponseBody), "provider overloaded") {
		t.Fatalf("unexpected failover error: %+v body=%s", failoverErr, failoverErr.ResponseBody)
	}
	if rec.Context.Writer.Written() || rec.Recorder.Body.Len() != 0 {
		t.Fatalf("preamble must remain attempt-local before failover: status=%d body=%q", rec.Recorder.Code, rec.Recorder.Body.String())
	}
	if result == nil || result.Usage.InputTokens != 12 {
		t.Fatalf("failed Responses usage should still be captured for diagnostics: %+v", result)
	}
}

func TestOpenCodeGoGatewayServiceResponsesFailedAfterOutputPreservesUpstreamTerminal(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstreamWire := strings.Join([]string{
		`event: response.created`,
		`data: {"type":"response.created","response":{"id":"resp_failed_after_output","object":"response","model":"gpt-5.6-luna","status":"in_progress","output":[]}}`,
		"",
		`event: response.output_text.delta`,
		`data: {"type":"response.output_text.delta","output_index":0,"content_index":0,"item_id":"msg_failed","delta":"partial"}`,
		"",
		`event: response.failed`,
		`data: {"type":"response.failed","response":{"id":"resp_failed_after_output","object":"response","model":"gpt-5.6-luna","status":"failed","output":[],"error":{"code":"server_error","message":"specific upstream failure"},"usage":{"input_tokens":12,"output_tokens":1,"total_tokens":13}}}`,
		"",
	}, "\n")
	upstream := &openCodeGoHTTPUpstreamStub{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(upstreamWire)),
	}}
	svc := &OpenCodeGoGatewayService{httpUpstream: upstream, cfg: &config.Config{}}
	requestBody := `{"model":"gpt-5.6-luna","input":"hello","stream":true}`
	rec := newTestGinContextRecorder(http.MethodPost, "/v1/responses", requestBody)

	result, err := svc.ForwardResponses(context.Background(), rec.Context, newOpenCodeGoResponsesTestAccount(), []byte(requestBody), "gpt-5.6-luna")
	if err == nil {
		t.Fatal("expected semantic failure after output")
	}
	wire := rec.Recorder.Body.String()
	if !strings.Contains(wire, `"delta":"partial"`) || !strings.Contains(wire, "specific upstream failure") {
		t.Fatalf("expected partial output and upstream failure message: %s", wire)
	}
	if got := strings.Count(wire, `"type":"response.failed"`); got != 1 {
		t.Fatalf("expected exactly one response.failed event, got %d: %s", got, wire)
	}
	if strings.Contains(wire, "Upstream request failed") {
		t.Fatalf("generic handler error must not replace upstream failure: %s", wire)
	}
	if !IsResponseCommitted(rec.Context) {
		t.Fatal("service must mark the in-band failure as communicated")
	}
	if result == nil || result.Usage.InputTokens != 12 || result.Usage.OutputTokens != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestOpenCodeGoGatewayServiceResponsesAndGoogleBufferedMatrix(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name             string
		source           protocolconv.Protocol
		actual           protocolconv.Protocol
		requestBody      string
		upstreamBody     string
		wantEndpoint     string
		wantRequestPath  string
		wantRequestValue string
		wantResponsePath string
		wantUsagePath    string
	}{
		{
			name: "responses_to_chat", source: protocolconv.ProtocolOpenAIResponses, actual: protocolconv.ProtocolOpenAIChat,
			requestBody:  `{"model":"matrix-model","input":"hello","stream":false}`,
			upstreamBody: `{"id":"chatcmpl_matrix","object":"chat.completion","model":"matrix-model","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`,
			wantEndpoint: "/v1/chat/completions", wantRequestPath: "messages.0.content", wantRequestValue: "hello",
			wantResponsePath: "output.0.content.0.text", wantUsagePath: "usage.input_tokens",
		},
		{
			name: "responses_to_messages", source: protocolconv.ProtocolOpenAIResponses, actual: protocolconv.ProtocolAnthropic,
			requestBody:  `{"model":"matrix-model","input":"hello","stream":false}`,
			upstreamBody: `{"id":"msg_matrix","type":"message","role":"assistant","model":"matrix-model","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":3,"output_tokens":2}}`,
			wantEndpoint: "/v1/messages", wantRequestPath: "messages.0.content.0.text", wantRequestValue: "hello",
			wantResponsePath: "output.0.content.0.text", wantUsagePath: "usage.input_tokens",
		},
		{
			name: "google_to_chat", source: protocolconv.ProtocolGoogleGenAI, actual: protocolconv.ProtocolOpenAIChat,
			requestBody:  `{"contents":[{"role":"user","parts":[{"text":"hello"}]}]}`,
			upstreamBody: `{"id":"chatcmpl_matrix","object":"chat.completion","model":"matrix-model","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`,
			wantEndpoint: "/v1/chat/completions", wantRequestPath: "messages.0.content", wantRequestValue: "hello",
			wantResponsePath: "candidates.0.content.parts.0.text", wantUsagePath: "usageMetadata.promptTokenCount",
		},
		{
			name: "google_to_messages", source: protocolconv.ProtocolGoogleGenAI, actual: protocolconv.ProtocolAnthropic,
			requestBody:  `{"contents":[{"role":"user","parts":[{"text":"hello"}]}]}`,
			upstreamBody: `{"id":"msg_matrix","type":"message","role":"assistant","model":"matrix-model","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":3,"output_tokens":2}}`,
			wantEndpoint: "/v1/messages", wantRequestPath: "messages.0.content.0.text", wantRequestValue: "hello",
			wantResponsePath: "candidates.0.content.parts.0.text", wantUsagePath: "usageMetadata.promptTokenCount",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			protocol := OpenCodeGoProtocolChatCompletions
			if tt.actual == protocolconv.ProtocolAnthropic {
				protocol = OpenCodeGoProtocolMessages
			}
			upstream := &openCodeGoHTTPUpstreamStub{resp: &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}, "X-Request-Id": []string{"rid-matrix"}},
				Body:       io.NopCloser(strings.NewReader(tt.upstreamBody)),
			}}
			svc := &OpenCodeGoGatewayService{httpUpstream: upstream, cfg: &config.Config{}}
			account := &Account{
				ID: 42, Platform: PlatformOpenCodeGo, Type: AccountTypeAPIKey, Concurrency: 1,
				Credentials: map[string]any{
					"api_key": "ocg-secret", "base_url": "https://opencode.ai/zen/go/v1",
					"model_protocols": map[string]any{"matrix-model": protocol},
				},
			}
			rec := newTestGinContextRecorder(http.MethodPost, "/matrix", tt.requestBody)
			var result *ForwardResult
			var err error
			if tt.source == protocolconv.ProtocolOpenAIResponses {
				result, err = svc.ForwardResponses(context.Background(), rec.Context, account, []byte(tt.requestBody), "matrix-model")
			} else {
				result, err = svc.ForwardGoogleGenAI(context.Background(), rec.Context, account, []byte(tt.requestBody), "matrix-model", "matrix-model", false)
			}
			if err != nil {
				t.Fatalf("forward error: %v", err)
			}
			if upstream.req == nil || !strings.HasSuffix(upstream.req.URL.Path, tt.wantEndpoint) {
				t.Fatalf("unexpected endpoint: %v", upstream.req)
			}
			if got := gjson.Get(upstream.body, tt.wantRequestPath).String(); got != tt.wantRequestValue {
				t.Fatalf("request semantic mismatch at %s: got=%q body=%s", tt.wantRequestPath, got, upstream.body)
			}
			wire := rec.Recorder.Body.Bytes()
			if got := gjson.GetBytes(wire, tt.wantResponsePath).String(); got != "ok" {
				t.Fatalf("response semantic mismatch at %s: got=%q body=%s", tt.wantResponsePath, got, wire)
			}
			if got := gjson.GetBytes(wire, tt.wantUsagePath).Int(); got != 3 {
				t.Fatalf("response usage mismatch at %s: got=%d body=%s", tt.wantUsagePath, got, wire)
			}
			if result == nil || result.ActualProtocol != tt.actual || result.Usage.InputTokens != 3 || result.Usage.OutputTokens != 2 {
				t.Fatalf("unexpected result: %+v", result)
			}
		})
	}
}

func TestOpenCodeGoGatewayServiceResponsesAndGoogleRestoreClientModelAcrossMappings(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, source := range []protocolconv.Protocol{protocolconv.ProtocolOpenAIResponses, protocolconv.ProtocolGoogleGenAI} {
		for _, actual := range []protocolconv.Protocol{protocolconv.ProtocolOpenAIChat, protocolconv.ProtocolAnthropic} {
			t.Run(source.String()+"_from_"+actual.String(), func(t *testing.T) {
				protocol := OpenCodeGoProtocolChatCompletions
				upstreamBody := `{"id":"chatcmpl_map","object":"chat.completion","model":"upstream-model","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`
				if actual == protocolconv.ProtocolAnthropic {
					protocol = OpenCodeGoProtocolMessages
					upstreamBody = `{"id":"msg_map","type":"message","role":"assistant","model":"upstream-model","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":3,"output_tokens":2}}`
				}
				upstream := &openCodeGoHTTPUpstreamStub{resp: &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(strings.NewReader(upstreamBody)),
				}}
				svc := &OpenCodeGoGatewayService{httpUpstream: upstream, cfg: &config.Config{}}
				account := &Account{ID: 42, Platform: PlatformOpenCodeGo, Type: AccountTypeAPIKey, Concurrency: 1, Credentials: map[string]any{
					"api_key": "ocg-secret", "base_url": "https://opencode.ai/zen/go/v1",
					"model_mapping":   map[string]any{"channel-model": "upstream-model"},
					"model_protocols": map[string]any{"upstream-model": protocol},
				}}
				requestBody := `{"model":"channel-model","input":"hello","stream":false}`
				rec := newTestGinContextRecorder(http.MethodPost, "/matrix", requestBody)
				var result *ForwardResult
				var err error
				responseModelPath := "model"
				if source == protocolconv.ProtocolOpenAIResponses {
					result, err = svc.ForwardResponses(context.Background(), rec.Context, account, []byte(requestBody), "client-model")
				} else {
					requestBody = `{"contents":[{"role":"user","parts":[{"text":"hello"}]}]}`
					rec = newTestGinContextRecorder(http.MethodPost, "/matrix", requestBody)
					result, err = svc.ForwardGoogleGenAI(context.Background(), rec.Context, account, []byte(requestBody), "client-model", "channel-model", false)
					responseModelPath = "modelVersion"
				}
				if err != nil {
					t.Fatalf("forward error: %v", err)
				}
				if got := gjson.Get(upstream.body, "model").String(); got != "upstream-model" {
					t.Fatalf("upstream model mismatch: got=%q body=%s", got, upstream.body)
				}
				if got := gjson.GetBytes(rec.Recorder.Body.Bytes(), responseModelPath).String(); got != "client-model" {
					t.Fatalf("client model not restored at %s: got=%q body=%s", responseModelPath, got, rec.Recorder.Body.String())
				}
				if result == nil || result.Model != "client-model" || result.UpstreamModel != "upstream-model" || result.ActualProtocol != actual {
					t.Fatalf("unexpected result: %+v", result)
				}
			})
		}
	}
}

func TestOpenCodeGoGatewayServiceResponsesAndGoogleStreamingMatrix(t *testing.T) {
	gin.SetMode(gin.TestMode)
	chatStream := strings.Join([]string{
		`data: {"id":"chatcmpl_matrix","object":"chat.completion.chunk","model":"matrix-model","choices":[{"index":0,"delta":{"role":"assistant","content":"ok"},"finish_reason":null}]}`,
		"",
		`data: {"id":"chatcmpl_matrix","object":"chat.completion.chunk","model":"matrix-model","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`,
		"",
		`data: [DONE]`,
		"",
	}, "\n")
	messagesStream := strings.Join([]string{
		`event: message_start`,
		`data: {"type":"message_start","message":{"id":"msg_matrix","type":"message","role":"assistant","model":"matrix-model","content":[],"usage":{"input_tokens":3,"output_tokens":0}}}`,
		"",
		`event: content_block_start`,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		"",
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"ok"}}`,
		"",
		`event: content_block_stop`,
		`data: {"type":"content_block_stop","index":0}`,
		"",
		`event: message_delta`,
		`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2}}`,
		"",
		`event: message_stop`,
		`data: {"type":"message_stop"}`,
		"",
	}, "\n")

	for _, source := range []protocolconv.Protocol{protocolconv.ProtocolOpenAIResponses, protocolconv.ProtocolGoogleGenAI} {
		for _, actual := range []protocolconv.Protocol{protocolconv.ProtocolOpenAIChat, protocolconv.ProtocolAnthropic} {
			name := source.String() + "_from_" + actual.String()
			t.Run(name, func(t *testing.T) {
				protocol := OpenCodeGoProtocolChatCompletions
				upstreamWire := chatStream
				if actual == protocolconv.ProtocolAnthropic {
					protocol = OpenCodeGoProtocolMessages
					upstreamWire = messagesStream
				}
				upstream := &openCodeGoHTTPUpstreamStub{resp: &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"text/event-stream"}, "X-Request-Id": []string{"rid-stream-matrix"}},
					Body:       io.NopCloser(strings.NewReader(upstreamWire)),
				}}
				svc := &OpenCodeGoGatewayService{httpUpstream: upstream, cfg: &config.Config{}}
				account := &Account{
					ID: 42, Platform: PlatformOpenCodeGo, Type: AccountTypeAPIKey, Concurrency: 1,
					Credentials: map[string]any{
						"api_key": "ocg-secret", "base_url": "https://opencode.ai/zen/go/v1",
						"model_protocols": map[string]any{"matrix-model": protocol},
					},
				}
				var requestBody string
				if source == protocolconv.ProtocolOpenAIResponses {
					requestBody = `{"model":"matrix-model","input":"hello","stream":true}`
				} else {
					requestBody = `{"contents":[{"role":"user","parts":[{"text":"hello"}]}]}`
				}
				rec := newTestGinContextRecorder(http.MethodPost, "/matrix", requestBody)
				var result *ForwardResult
				var err error
				if source == protocolconv.ProtocolOpenAIResponses {
					result, err = svc.ForwardResponses(context.Background(), rec.Context, account, []byte(requestBody), "matrix-model")
				} else {
					result, err = svc.ForwardGoogleGenAI(context.Background(), rec.Context, account, []byte(requestBody), "matrix-model", "matrix-model", true)
				}
				if err != nil {
					t.Fatalf("forward error: %v\nwire=%s", err, rec.Recorder.Body.String())
				}
				wire := rec.Recorder.Body.String()
				if !strings.Contains(wire, "ok") {
					t.Fatalf("missing streamed text: %s", wire)
				}
				if source == protocolconv.ProtocolOpenAIResponses {
					if !strings.Contains(wire, "event: response.created") || !strings.Contains(wire, "event: response.completed") || !strings.Contains(wire, "data: [DONE]") {
						t.Fatalf("invalid Responses stream lifecycle: %s", wire)
					}
				} else {
					if !strings.Contains(wire, `"finishReason":"STOP"`) || !strings.Contains(wire, `"usageMetadata"`) || strings.Contains(wire, "[DONE]") || strings.Contains(wire, "event:") {
						t.Fatalf("invalid Google stream lifecycle: %s", wire)
					}
				}
				if result == nil || result.ActualProtocol != actual || !result.Stream || result.Usage.InputTokens != 3 || result.Usage.OutputTokens != 2 {
					t.Fatalf("unexpected result: %+v", result)
				}
			})
		}
	}
}

func TestOpenCodeGoGatewayServiceResponsesAndGoogleToolCallsPreserveCorrelation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, source := range []protocolconv.Protocol{protocolconv.ProtocolOpenAIResponses, protocolconv.ProtocolGoogleGenAI} {
		for _, actual := range []protocolconv.Protocol{protocolconv.ProtocolOpenAIChat, protocolconv.ProtocolAnthropic} {
			t.Run(source.String()+"_to_"+actual.String(), func(t *testing.T) {
				protocol := OpenCodeGoProtocolChatCompletions
				upstreamBody := `{"id":"chatcmpl_tool","object":"chat.completion","model":"matrix-model","choices":[{"index":0,"message":{"role":"assistant","content":null,"tool_calls":[{"id":"call_matrix","type":"function","function":{"name":"lookup","arguments":"{\"q\":\"x\"}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`
				if actual == protocolconv.ProtocolAnthropic {
					protocol = OpenCodeGoProtocolMessages
					upstreamBody = `{"id":"msg_tool","type":"message","role":"assistant","model":"matrix-model","content":[{"type":"tool_use","id":"call_matrix","name":"lookup","input":{"q":"x"}}],"stop_reason":"tool_use","usage":{"input_tokens":3,"output_tokens":2}}`
				}
				upstream := &openCodeGoHTTPUpstreamStub{resp: &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(strings.NewReader(upstreamBody)),
				}}
				svc := &OpenCodeGoGatewayService{httpUpstream: upstream, cfg: &config.Config{}}
				account := &Account{ID: 42, Platform: PlatformOpenCodeGo, Type: AccountTypeAPIKey, Concurrency: 1, Credentials: map[string]any{
					"api_key": "ocg-secret", "base_url": "https://opencode.ai/zen/go/v1",
					"model_protocols": map[string]any{"matrix-model": protocol},
				}}
				var requestBody string
				if source == protocolconv.ProtocolOpenAIResponses {
					requestBody = `{"model":"matrix-model","input":"use lookup","tools":[{"type":"function","name":"lookup","description":"lookup","parameters":{"type":"object","properties":{"q":{"type":"string"}},"required":["q"]}}],"tool_choice":{"type":"function","name":"lookup"},"stream":false}`
				} else {
					requestBody = `{"contents":[{"role":"user","parts":[{"text":"use lookup"}]}],"tools":[{"functionDeclarations":[{"name":"lookup","description":"lookup","parameters":{"type":"object","properties":{"q":{"type":"string"}},"required":["q"]}}]}],"toolConfig":{"functionCallingConfig":{"mode":"ANY","allowedFunctionNames":["lookup"]}}}`
				}
				rec := newTestGinContextRecorder(http.MethodPost, "/matrix", requestBody)
				var err error
				if source == protocolconv.ProtocolOpenAIResponses {
					_, err = svc.ForwardResponses(context.Background(), rec.Context, account, []byte(requestBody), "matrix-model")
				} else {
					_, err = svc.ForwardGoogleGenAI(context.Background(), rec.Context, account, []byte(requestBody), "matrix-model", "matrix-model", false)
				}
				if err != nil {
					t.Fatalf("forward error: %v", err)
				}
				if actual == protocolconv.ProtocolOpenAIChat {
					if gjson.Get(upstream.body, "tools.0.function.name").String() != "lookup" {
						t.Fatalf("Chat request lost tool: %s", upstream.body)
					}
				} else if gjson.Get(upstream.body, "tools.0.name").String() != "lookup" {
					t.Fatalf("Messages request lost tool: %s", upstream.body)
				}
				wire := rec.Recorder.Body.Bytes()
				if source == protocolconv.ProtocolOpenAIResponses {
					if gjson.GetBytes(wire, "output.0.type").String() != "function_call" || gjson.GetBytes(wire, "output.0.call_id").String() != "call_matrix" || gjson.GetBytes(wire, "output.0.name").String() != "lookup" {
						t.Fatalf("Responses output lost tool correlation: %s", wire)
					}
				} else {
					if gjson.GetBytes(wire, "candidates.0.content.parts.0.functionCall.id").String() != "call_matrix" || gjson.GetBytes(wire, "candidates.0.content.parts.0.functionCall.name").String() != "lookup" {
						t.Fatalf("Google output lost tool correlation: %s", wire)
					}
				}
			})
		}
	}
}

func TestOpenCodeGoGatewayServiceResponsesAndGoogleToolResultsPreserveCorrelation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, source := range []protocolconv.Protocol{protocolconv.ProtocolOpenAIResponses, protocolconv.ProtocolGoogleGenAI} {
		for _, actual := range []protocolconv.Protocol{protocolconv.ProtocolOpenAIChat, protocolconv.ProtocolAnthropic} {
			t.Run(source.String()+"_to_"+actual.String(), func(t *testing.T) {
				protocol := OpenCodeGoProtocolChatCompletions
				upstreamBody := `{"id":"chatcmpl_final","object":"chat.completion","model":"matrix-model","choices":[{"index":0,"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":1,"total_tokens":6}}`
				if actual == protocolconv.ProtocolAnthropic {
					protocol = OpenCodeGoProtocolMessages
					upstreamBody = `{"id":"msg_final","type":"message","role":"assistant","model":"matrix-model","content":[{"type":"text","text":"done"}],"stop_reason":"end_turn","usage":{"input_tokens":5,"output_tokens":1}}`
				}
				upstream := &openCodeGoHTTPUpstreamStub{resp: &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(strings.NewReader(upstreamBody)),
				}}
				svc := &OpenCodeGoGatewayService{httpUpstream: upstream, cfg: &config.Config{}}
				account := &Account{ID: 42, Platform: PlatformOpenCodeGo, Type: AccountTypeAPIKey, Concurrency: 1, Credentials: map[string]any{
					"api_key": "ocg-secret", "base_url": "https://opencode.ai/zen/go/v1",
					"model_protocols": map[string]any{"matrix-model": protocol},
				}}
				requestBody := `{"model":"matrix-model","input":[{"type":"function_call","call_id":"call_matrix","name":"lookup","arguments":"{\"q\":\"x\"}"},{"type":"function_call_output","call_id":"call_matrix","output":"found"}],"tools":[{"type":"function","name":"lookup","parameters":{"type":"object"}}],"stream":false}`
				if source == protocolconv.ProtocolGoogleGenAI {
					requestBody = `{"contents":[{"role":"model","parts":[{"functionCall":{"id":"call_matrix","name":"lookup","args":{"q":"x"}}}]},{"role":"user","parts":[{"functionResponse":{"id":"call_matrix","name":"lookup","response":{"content":"found"}}}]}],"tools":[{"functionDeclarations":[{"name":"lookup","parameters":{"type":"object"}}]}]}`
				}
				rec := newTestGinContextRecorder(http.MethodPost, "/matrix", requestBody)
				var err error
				if source == protocolconv.ProtocolOpenAIResponses {
					_, err = svc.ForwardResponses(context.Background(), rec.Context, account, []byte(requestBody), "matrix-model")
				} else {
					_, err = svc.ForwardGoogleGenAI(context.Background(), rec.Context, account, []byte(requestBody), "matrix-model", "matrix-model", false)
				}
				if err != nil {
					t.Fatalf("forward error: %v", err)
				}
				if actual == protocolconv.ProtocolOpenAIChat {
					if gjson.Get(upstream.body, "messages.0.tool_calls.0.id").String() != "call_matrix" || gjson.Get(upstream.body, "messages.0.tool_calls.0.function.name").String() != "lookup" || gjson.Get(upstream.body, "messages.1.tool_call_id").String() != "call_matrix" {
						t.Fatalf("Chat second turn lost tool correlation: %s", upstream.body)
					}
					toolResult := gjson.Get(upstream.body, "messages.1.content").String()
					if source == protocolconv.ProtocolGoogleGenAI {
						if gjson.Get(toolResult, "content").String() != "found" {
							t.Fatalf("Chat second turn lost structured Google tool result: %s", upstream.body)
						}
					} else if toolResult != "found" {
						t.Fatalf("Chat second turn lost Responses tool result: %s", upstream.body)
					}
				} else {
					if gjson.Get(upstream.body, "messages.0.content.0.id").String() != "call_matrix" || gjson.Get(upstream.body, "messages.0.content.0.name").String() != "lookup" || gjson.Get(upstream.body, "messages.1.content.0.tool_use_id").String() != "call_matrix" {
						t.Fatalf("Messages second turn lost tool correlation: %s", upstream.body)
					}
					toolResult := gjson.Get(upstream.body, "messages.1.content.0.content")
					if source == protocolconv.ProtocolGoogleGenAI {
						if toolResult.Type != gjson.String || gjson.Get(toolResult.String(), "content").String() != "found" {
							t.Fatalf("Messages second turn did not serialize the structured tool result as JSON text: %s", upstream.body)
						}
					} else if toolResult.String() != "found" {
						t.Fatalf("Messages second turn lost tool result: %s", upstream.body)
					}
				}
			})
		}
	}
}

func TestOpenCodeGoGatewayServiceUsesStableUpstreamUserAgent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := &openCodeGoHTTPUpstreamStub{}
	svc := &OpenCodeGoGatewayService{
		httpUpstream: upstream,
		cfg:          &config.Config{},
	}
	account := &Account{
		ID:          42,
		Platform:    PlatformOpenCodeGo,
		Type:        AccountTypeAPIKey,
		Concurrency: 1,
		Credentials: map[string]any{
			"api_key":  "ocg-secret",
			"base_url": "https://opencode.ai/zen/go/v1",
			"model_protocols": map[string]any{
				"kimi-k2.7-code": OpenCodeGoProtocolChatCompletions,
			},
		},
	}
	rec := newTestGinContextRecorder(http.MethodPost, "/v1/chat/completions", `{"model":"kimi-k2.7-code","messages":[{"role":"user","content":"hi"}],"stream":false}`)
	rec.Context.Request.Header.Set("User-Agent", "Python-urllib/3.13")
	rec.Context.Request.Header.Set("Accept-Language", "zh-CN,zh;q=0.9")

	_, err := svc.ForwardChatCompletions(context.Background(), rec.Context, account, []byte(`{"model":"kimi-k2.7-code","messages":[{"role":"user","content":"hi"}],"stream":false}`))
	if err != nil {
		t.Fatalf("ForwardChatCompletions error: %v", err)
	}
	const wantUserAgent = "opencode/1.0.0 (linux; x64) node/24.3.0"
	if got := upstream.req.Header.Get("User-Agent"); got != wantUserAgent {
		t.Fatalf("expected stable upstream user-agent %q, got %q", wantUserAgent, got)
	}
	if got := upstream.req.Header.Get("Accept-Language"); got != "zh-CN,zh;q=0.9" {
		t.Fatalf("expected forwarded accept-language, got %q", got)
	}
}

func TestOpenCodeGoGatewayServiceChatToMessagesUsesAnthropicAuthHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := &openCodeGoHTTPUpstreamStub{
		resp: &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"id":"msg_ok","type":"message","role":"assistant","model":"minimax-m3","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":3,"output_tokens":2}}`)),
		},
	}
	svc := &OpenCodeGoGatewayService{
		httpUpstream: upstream,
		cfg:          &config.Config{},
	}
	account := &Account{
		ID:          42,
		Platform:    PlatformOpenCodeGo,
		Type:        AccountTypeAPIKey,
		Concurrency: 1,
		Credentials: map[string]any{
			"api_key":  "ocg-secret",
			"base_url": "https://opencode.ai/zen/go/v1",
			"model_protocols": map[string]any{
				"minimax-m3": OpenCodeGoProtocolMessages,
			},
		},
	}
	rec := newTestGinContextRecorder(http.MethodPost, "/v1/chat/completions", `{"model":"minimax-m3","messages":[{"role":"user","content":"hi"}],"stream":false}`)

	result, err := svc.ForwardChatCompletions(context.Background(), rec.Context, account, []byte(`{"model":"minimax-m3","messages":[{"role":"user","content":"hi"}],"stream":false}`))
	if err != nil {
		t.Fatalf("ForwardChatCompletions error: %v", err)
	}
	if result == nil {
		t.Fatalf("expected result")
	}
	if got := upstream.req.URL.String(); got != "https://opencode.ai/zen/go/v1/messages" {
		t.Fatalf("unexpected upstream URL: %s", got)
	}
	if got := upstream.req.Header.Get("x-api-key"); got != "ocg-secret" {
		t.Fatalf("unexpected x-api-key header: %s", got)
	}
	if got := upstream.req.Header.Get("anthropic-version"); got != "2023-06-01" {
		t.Fatalf("unexpected anthropic-version header: %s", got)
	}
	if got := upstream.req.Header.Get("Authorization"); got != "" {
		t.Fatalf("messages upstream should not send authorization header, got %s", got)
	}
	if !strings.Contains(upstream.body, `"model":"minimax-m3"`) {
		t.Fatalf("expected upstream model rewrite, body=%s", upstream.body)
	}
}

func TestOpenCodeGoGatewayServiceForwardChatToMessagesPreparesConvertedBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := &openCodeGoHTTPUpstreamStub{
		resp: &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"id":"msg_ok","type":"message","role":"assistant","model":"qwen3.7-plus","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":3,"output_tokens":2}}`)),
		},
	}
	svc := &OpenCodeGoGatewayService{
		httpUpstream: upstream,
		cfg:          &config.Config{},
	}
	account := &Account{
		ID:          42,
		Platform:    PlatformOpenCodeGo,
		Type:        AccountTypeAPIKey,
		Concurrency: 1,
		Credentials: map[string]any{
			"api_key":  "ocg-secret",
			"base_url": "https://opencode.ai/zen/go/v1",
			"model_mapping": map[string]any{
				"opencode-go/qwen": "qwen3.7-plus",
			},
			"model_protocols": map[string]any{
				"qwen3.7-plus": OpenCodeGoProtocolMessages,
			},
		},
	}
	body := `{
		"model":"opencode-go/qwen",
		"messages":[
			{"role":"system","content":"stable project instructions"},
			{"role":"user","content":"turn 1"},
			{"role":"assistant","content":"ok"},
			{"role":"user","content":"turn 2"},
			{"role":"assistant","content":"ok"},
			{"role":"user","content":"turn 3"}
		],
		"tools":[{"type":"function","function":{"name":"sessions_list","parameters":{"type":"object"}}}],
		"stream":false
	}`
	rec := newTestGinContextRecorder(http.MethodPost, "/v1/chat/completions", body)

	if _, err := svc.ForwardChatCompletions(context.Background(), rec.Context, account, []byte(body)); err != nil {
		t.Fatalf("ForwardChatCompletions error: %v", err)
	}

	if got := upstream.req.URL.String(); got != "https://opencode.ai/zen/go/v1/messages" {
		t.Fatalf("unexpected upstream URL: %s", got)
	}
	assertOpenCodeGoCacheTTL(t, upstream.body, "system.0")
	assertOpenCodeGoCacheTTL(t, upstream.body, "messages.2.content.0")
	assertOpenCodeGoCacheTTL(t, upstream.body, "messages.4.content.0")
	assertOpenCodeGoCacheTTL(t, upstream.body, "tools.0")
	if got := gjson.Get(upstream.body, "tools.0.name").String(); got != "sessions_list" {
		t.Fatalf("expected tool name to remain unchanged, got %q body=%s", got, upstream.body)
	}
	if got := countOpenCodeGoCacheControlBlocks(upstream.body); got != 4 {
		t.Fatalf("expected exactly 4 cache_control blocks, got %d body=%s", got, upstream.body)
	}
	responseBody := rec.Recorder.Body.String()
	if got := gjson.Get(responseBody, "object").String(); got != "chat.completion" {
		t.Fatalf("expected Chat response, got %s", responseBody)
	}
	if got := gjson.Get(responseBody, "model").String(); got != "opencode-go/qwen" {
		t.Fatalf("expected client model restoration, got %q body=%s", got, responseBody)
	}
	if got := gjson.Get(responseBody, "choices.0.message.content").String(); got != "ok" {
		t.Fatalf("expected converted content, got %q body=%s", got, responseBody)
	}
}

func TestOpenCodeGoGatewayServiceMessagesToChatRestoresClientModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := &openCodeGoHTTPUpstreamStub{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(strings.NewReader(
			`{"id":"chatcmpl_ok","object":"chat.completion","model":"kimi-k2.7-code","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":2}}`,
		)),
	}}
	svc := &OpenCodeGoGatewayService{httpUpstream: upstream, cfg: &config.Config{}}
	account := &Account{
		ID: 42, Platform: PlatformOpenCodeGo, Type: AccountTypeAPIKey, Concurrency: 1,
		Credentials: map[string]any{
			"api_key": "ocg-secret", "base_url": "https://opencode.ai/zen/go/v1",
			"model_mapping":   map[string]any{"opencode-go/kimi": "kimi-k2.7-code"},
			"model_protocols": map[string]any{"kimi-k2.7-code": OpenCodeGoProtocolChatCompletions},
		},
	}
	body := `{"model":"opencode-go/kimi","messages":[{"role":"user","content":"hi"}],"max_tokens":32,"stream":false}`
	rec := newTestGinContextRecorder(http.MethodPost, "/v1/messages", body)

	result, err := svc.ForwardMessages(context.Background(), rec.Context, account, []byte(body))
	if err != nil {
		t.Fatalf("ForwardMessages error: %v", err)
	}
	if got := upstream.req.URL.String(); got != "https://opencode.ai/zen/go/v1/chat/completions" {
		t.Fatalf("unexpected upstream URL: %s", got)
	}
	responseBody := rec.Recorder.Body.String()
	if got := gjson.Get(responseBody, "type").String(); got != "message" {
		t.Fatalf("expected Anthropic response, got %s", responseBody)
	}
	if got := gjson.Get(responseBody, "model").String(); got != "opencode-go/kimi" {
		t.Fatalf("expected client model restoration, got %q body=%s", got, responseBody)
	}
	if got := gjson.Get(responseBody, "content.0.text").String(); got != "ok" {
		t.Fatalf("expected converted content, got %q body=%s", got, responseBody)
	}
	if result == nil || result.Usage.InputTokens != 5 || result.Usage.OutputTokens != 2 {
		t.Fatalf("unexpected usage result: %+v", result)
	}
}

func TestOpenCodeGoGatewayServiceMessagesDirectUsesAnthropicAuthHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := &openCodeGoHTTPUpstreamStub{
		resp: &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"id":"msg_ok","type":"message","role":"assistant","model":"qwen3.7-plus","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":3,"output_tokens":2}}`)),
		},
	}
	svc := &OpenCodeGoGatewayService{
		httpUpstream: upstream,
		cfg:          &config.Config{},
	}
	account := &Account{
		ID:          42,
		Platform:    PlatformOpenCodeGo,
		Type:        AccountTypeAPIKey,
		Concurrency: 1,
		Credentials: map[string]any{
			"api_key":  "ocg-secret",
			"base_url": "https://opencode.ai/zen/go/v1",
			"model_protocols": map[string]any{
				"qwen3.7-plus": OpenCodeGoProtocolMessages,
			},
		},
	}
	rec := newTestGinContextRecorder(http.MethodPost, "/v1/messages", `{"model":"qwen3.7-plus","messages":[{"role":"user","content":"hi"}],"max_tokens":10}`)

	result, err := svc.ForwardMessages(context.Background(), rec.Context, account, []byte(`{"model":"qwen3.7-plus","messages":[{"role":"user","content":"hi"}],"max_tokens":10}`))
	if err != nil {
		t.Fatalf("ForwardMessages error: %v", err)
	}
	if result == nil {
		t.Fatalf("expected result")
	}
	if got := upstream.req.URL.String(); got != "https://opencode.ai/zen/go/v1/messages" {
		t.Fatalf("unexpected upstream URL: %s", got)
	}
	if got := upstream.req.Header.Get("x-api-key"); got != "ocg-secret" {
		t.Fatalf("unexpected x-api-key header: %s", got)
	}
	if got := upstream.req.Header.Get("anthropic-version"); got != "2023-06-01" {
		t.Fatalf("unexpected anthropic-version header: %s", got)
	}
	if got := upstream.req.Header.Get("Authorization"); got != "" {
		t.Fatalf("messages upstream should not send authorization header, got %s", got)
	}
}

func TestOpenCodeGoGatewayServiceForwardMessagesPreparesMessagesProtocolBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := &openCodeGoHTTPUpstreamStub{
		resp: &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"id":"msg_ok","type":"message","role":"assistant","model":"qwen3.7-plus","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":3,"output_tokens":2}}`)),
		},
	}
	svc := &OpenCodeGoGatewayService{
		httpUpstream: upstream,
		cfg:          &config.Config{},
	}
	account := &Account{
		ID:          42,
		Platform:    PlatformOpenCodeGo,
		Type:        AccountTypeAPIKey,
		Concurrency: 1,
		Credentials: map[string]any{
			"api_key":  "ocg-secret",
			"base_url": "https://opencode.ai/zen/go/v1",
			"model_protocols": map[string]any{
				"qwen3.7-plus": OpenCodeGoProtocolMessages,
			},
		},
	}
	body := `{
		"model":"qwen3.7-plus",
		"system":"stable project instructions",
		"messages":[
			{"role":"user","content":[{"type":"text","text":"turn 1","cache_control":{"type":"ephemeral","ttl":"5m"}}]},
			{"role":"assistant","content":[{"type":"text","text":"ok"}]},
			{"role":"user","content":[{"type":"text","text":"turn 2"}]},
			{"role":"assistant","content":[{"type":"text","text":"ok"}]},
			{"role":"user","content":[{"type":"text","text":"turn 3","cache_control":{"type":"ephemeral","ttl":"5m"}}]}
		],
		"tools":[{"name":"sessions_list","input_schema":{"type":"object"}}],
		"max_tokens":10
	}`
	rec := newTestGinContextRecorder(http.MethodPost, "/v1/messages", body)

	if _, err := svc.ForwardMessages(context.Background(), rec.Context, account, []byte(body)); err != nil {
		t.Fatalf("ForwardMessages error: %v", err)
	}

	if got := upstream.req.URL.String(); got != "https://opencode.ai/zen/go/v1/messages" {
		t.Fatalf("unexpected upstream URL: %s", got)
	}
	assertOpenCodeGoCacheTTL(t, upstream.body, "system.0")
	assertOpenCodeGoCacheTTL(t, upstream.body, "messages.2.content.0")
	assertOpenCodeGoCacheTTL(t, upstream.body, "messages.4.content.0")
	assertOpenCodeGoCacheTTL(t, upstream.body, "tools.0")
	if got := gjson.Get(upstream.body, "tools.0.name").String(); got != "sessions_list" {
		t.Fatalf("expected tool name to remain unchanged, got %q body=%s", got, upstream.body)
	}
	if gjson.Get(upstream.body, "messages.0.content.0.cache_control").Exists() {
		t.Fatalf("expected stale first-turn message cache_control to be stripped, body=%s", upstream.body)
	}
	if got := countOpenCodeGoCacheControlBlocks(upstream.body); got != 4 {
		t.Fatalf("expected exactly 4 cache_control blocks, got %d body=%s", got, upstream.body)
	}
}

func TestOpenCodeGoGatewayServiceChatProtocolDoesNotInjectAnthropicCacheControl(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := &openCodeGoHTTPUpstreamStub{}
	svc := &OpenCodeGoGatewayService{
		httpUpstream: upstream,
		cfg:          &config.Config{},
	}
	account := &Account{
		ID:          42,
		Platform:    PlatformOpenCodeGo,
		Type:        AccountTypeAPIKey,
		Concurrency: 1,
		Credentials: map[string]any{
			"api_key":  "ocg-secret",
			"base_url": "https://opencode.ai/zen/go/v1",
			"model_protocols": map[string]any{
				"kimi-k2.7-code": OpenCodeGoProtocolChatCompletions,
			},
		},
	}
	body := `{
		"model":"kimi-k2.7-code",
		"messages":[
			{"role":"system","content":"stable project instructions"},
			{"role":"user","content":"hi"}
		],
		"tools":[{"type":"function","function":{"name":"sessions_list","parameters":{"type":"object"}}}],
		"stream":false
	}`
	rec := newTestGinContextRecorder(http.MethodPost, "/v1/chat/completions", body)

	if _, err := svc.ForwardChatCompletions(context.Background(), rec.Context, account, []byte(body)); err != nil {
		t.Fatalf("ForwardChatCompletions error: %v", err)
	}

	if got := upstream.req.URL.String(); got != "https://opencode.ai/zen/go/v1/chat/completions" {
		t.Fatalf("unexpected upstream URL: %s", got)
	}
	if strings.Contains(upstream.body, "cache_control") {
		t.Fatalf("chat_completions protocol should not inject Anthropic cache_control, body=%s", upstream.body)
	}
}

func TestOpenCodeGoGatewayServiceRejectsUnknownModelProtocol(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := &OpenCodeGoGatewayService{
		httpUpstream: &openCodeGoHTTPUpstreamStub{},
		cfg:          &config.Config{},
	}
	account := &Account{
		ID:       42,
		Platform: PlatformOpenCodeGo,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":  "ocg-secret",
			"base_url": "https://opencode.ai/zen/go/v1",
		},
	}
	rec := newTestGinContextRecorder(http.MethodPost, "/v1/chat/completions", `{"model":"unknown-model","messages":[{"role":"user","content":"hi"}]}`)

	_, err := svc.ForwardChatCompletions(context.Background(), rec.Context, account, []byte(`{"model":"unknown-model","messages":[{"role":"user","content":"hi"}]}`))
	if err == nil {
		t.Fatalf("expected unknown model protocol error")
	}
	if rec.Recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rec.Recorder.Code, rec.Recorder.Body.String())
	}
	if !strings.Contains(rec.Recorder.Body.String(), "model protocol") {
		t.Fatalf("expected protocol error body, got %s", rec.Recorder.Body.String())
	}
}

func TestOpenCodeGoGatewayServiceNormalizesCachedTokensInChatCompletionsUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := &openCodeGoHTTPUpstreamStub{
		resp: &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"id":"chatcmpl_ok","model":"kimi-k2.7-code","choices":[{"index":0,"message":{"role":"assistant","content":"ok"}}],"usage":{"prompt_tokens":100,"completion_tokens":20,"prompt_tokens_details":{"cached_tokens":60}}}`)),
		},
	}
	svc := &OpenCodeGoGatewayService{
		httpUpstream: upstream,
		cfg:          &config.Config{},
	}
	account := &Account{
		ID:          42,
		Platform:    PlatformOpenCodeGo,
		Type:        AccountTypeAPIKey,
		Concurrency: 1,
		Credentials: map[string]any{
			"api_key":  "ocg-secret",
			"base_url": "https://opencode.ai/zen/go/v1",
			"model_mapping": map[string]any{
				"opencode-go/kimi": "kimi-k2.7-code",
			},
			"model_protocols": map[string]any{
				"kimi-k2.7-code": OpenCodeGoProtocolChatCompletions,
			},
		},
	}

	rec := newTestGinContextRecorder(http.MethodPost, "/v1/chat/completions", `{"model":"opencode-go/kimi","messages":[{"role":"user","content":"hi"}],"stream":false}`)
	result, err := svc.ForwardChatCompletions(context.Background(), rec.Context, account, []byte(`{"model":"opencode-go/kimi","messages":[{"role":"user","content":"hi"}],"stream":false}`))
	if err != nil {
		t.Fatalf("ForwardChatCompletions error: %v", err)
	}
	if result == nil {
		t.Fatalf("expected result")
	}
	if result.Usage.InputTokens != 40 {
		t.Fatalf("expected actual input tokens 40, got %+v", result.Usage)
	}
	if result.Usage.CacheReadInputTokens != 60 {
		t.Fatalf("expected cache read tokens 60, got %+v", result.Usage)
	}
}

func TestOpenCodeGoGatewayServiceNormalizesCachedTokensInChatCompletionsStreamUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := &openCodeGoHTTPUpstreamStub{
		resp: &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body: io.NopCloser(strings.NewReader(
				`data: {"id":"chatcmpl_ok","object":"chat.completion.chunk","model":"kimi-k2.7-code","choices":[{"index":0,"delta":{"content":"ok"}}],"usage":{"prompt_tokens":100,"completion_tokens":20,"prompt_tokens_details":{"cached_tokens":60}}}` + "\n\n" +
					`data: [DONE]` + "\n\n",
			)),
		},
	}
	svc := &OpenCodeGoGatewayService{
		httpUpstream: upstream,
		cfg:          &config.Config{},
	}
	account := &Account{
		ID:          42,
		Platform:    PlatformOpenCodeGo,
		Type:        AccountTypeAPIKey,
		Concurrency: 1,
		Credentials: map[string]any{
			"api_key":  "ocg-secret",
			"base_url": "https://opencode.ai/zen/go/v1",
			"model_mapping": map[string]any{
				"opencode-go/kimi": "kimi-k2.7-code",
			},
			"model_protocols": map[string]any{
				"kimi-k2.7-code": OpenCodeGoProtocolChatCompletions,
			},
		},
	}

	rec := newTestGinContextRecorder(http.MethodPost, "/v1/chat/completions", `{"model":"opencode-go/kimi","messages":[{"role":"user","content":"hi"}],"stream":true}`)
	result, err := svc.ForwardChatCompletions(context.Background(), rec.Context, account, []byte(`{"model":"opencode-go/kimi","messages":[{"role":"user","content":"hi"}],"stream":true}`))
	if err != nil {
		t.Fatalf("ForwardChatCompletions error: %v", err)
	}
	if result == nil {
		t.Fatalf("expected result")
	}
	if !result.Stream {
		t.Fatalf("expected stream result")
	}
	if result.Usage.InputTokens != 40 {
		t.Fatalf("expected actual input tokens 40, got %+v", result.Usage)
	}
	if result.Usage.CacheReadInputTokens != 60 {
		t.Fatalf("expected cache read tokens 60, got %+v", result.Usage)
	}
}

func TestOpenCodeGoGatewayServiceMessagesToChatStreamUsesPipeline(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstreamBody := strings.Join([]string{
		`data: {"id":"chatcmpl_stream","object":"chat.completion.chunk","model":"kimi-k2.7-code","choices":[{"index":0,"delta":{"role":"assistant","content":"ok"},"finish_reason":null}]}`,
		"",
		`data: {"id":"chatcmpl_stream","object":"chat.completion.chunk","model":"kimi-k2.7-code","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":2}}`,
		"",
		"data: [DONE]",
		"",
	}, "\n")
	upstream := &openCodeGoHTTPUpstreamStub{resp: &http.Response{
		StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}},
		Body: io.NopCloser(strings.NewReader(upstreamBody)),
	}}
	svc := &OpenCodeGoGatewayService{httpUpstream: upstream, cfg: &config.Config{}}
	account := &Account{
		ID: 42, Platform: PlatformOpenCodeGo, Type: AccountTypeAPIKey, Concurrency: 1,
		Credentials: map[string]any{
			"api_key": "ocg-secret", "base_url": "https://opencode.ai/zen/go/v1",
			"model_mapping":   map[string]any{"opencode-go/kimi": "kimi-k2.7-code"},
			"model_protocols": map[string]any{"kimi-k2.7-code": OpenCodeGoProtocolChatCompletions},
		},
	}
	body := `{"model":"opencode-go/kimi","messages":[{"role":"user","content":"hi"}],"max_tokens":32,"stream":true}`
	rec := newTestGinContextRecorder(http.MethodPost, "/v1/messages", body)

	result, err := svc.ForwardMessages(context.Background(), rec.Context, account, []byte(body))
	if err != nil {
		t.Fatalf("ForwardMessages error: %v", err)
	}
	wire := rec.Recorder.Body.String()
	if !strings.Contains(wire, `event: message_start`) || !strings.Contains(wire, `"model":"opencode-go/kimi"`) {
		t.Fatalf("expected client-model Anthropic stream, got %s", wire)
	}
	if !strings.Contains(wire, `"text":"ok"`) || !strings.Contains(wire, `event: message_stop`) {
		t.Fatalf("expected completed Anthropic stream, got %s", wire)
	}
	if strings.Contains(wire, "data: [DONE]") {
		t.Fatalf("Anthropic stream must not emit Chat terminal sentinel: %s", wire)
	}
	if result == nil || result.Usage.InputTokens != 5 || result.Usage.OutputTokens != 2 {
		t.Fatalf("unexpected usage result: %+v", result)
	}
}

func TestOpenCodeGoGatewayServiceChatToMessagesStreamUsesPipeline(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstreamBody := strings.Join([]string{
		`event: message_start`,
		`data: {"type":"message_start","message":{"id":"msg_stream","type":"message","role":"assistant","model":"qwen3.7-plus","content":[],"usage":{"input_tokens":4,"output_tokens":0}}}`,
		"",
		`event: content_block_start`,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		"",
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"ok"}}`,
		"",
		`event: content_block_stop`,
		`data: {"type":"content_block_stop","index":0}`,
		"",
		`event: message_delta`,
		`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2}}`,
		"",
		`event: message_stop`,
		`data: {"type":"message_stop"}`,
		"",
	}, "\n")
	upstream := &openCodeGoHTTPUpstreamStub{resp: &http.Response{
		StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}},
		Body: io.NopCloser(strings.NewReader(upstreamBody)),
	}}
	svc := &OpenCodeGoGatewayService{httpUpstream: upstream, cfg: &config.Config{}}
	account := &Account{
		ID: 42, Platform: PlatformOpenCodeGo, Type: AccountTypeAPIKey, Concurrency: 1,
		Credentials: map[string]any{
			"api_key": "ocg-secret", "base_url": "https://opencode.ai/zen/go/v1",
			"model_mapping":   map[string]any{"opencode-go/qwen": "qwen3.7-plus"},
			"model_protocols": map[string]any{"qwen3.7-plus": OpenCodeGoProtocolMessages},
		},
	}
	body := `{"model":"opencode-go/qwen","messages":[{"role":"user","content":"hi"}],"stream":true}`
	rec := newTestGinContextRecorder(http.MethodPost, "/v1/chat/completions", body)

	result, err := svc.ForwardChatCompletions(context.Background(), rec.Context, account, []byte(body))
	if err != nil {
		t.Fatalf("ForwardChatCompletions error: %v", err)
	}
	wire := rec.Recorder.Body.String()
	if !strings.Contains(wire, `"model":"opencode-go/qwen"`) || !strings.Contains(wire, `"content":"ok"`) {
		t.Fatalf("expected client-model Chat stream, got %s", wire)
	}
	if !strings.Contains(wire, "data: [DONE]") {
		t.Fatalf("Chat stream must emit terminal sentinel: %s", wire)
	}
	if result == nil || result.Usage.InputTokens != 4 || result.Usage.OutputTokens != 2 {
		t.Fatalf("unexpected usage result: %+v", result)
	}
}

func TestCollectOpenCodeGoErrorResponsePreservesStructuredTransport(t *testing.T) {
	body := &openCodeGoCloseTrackingBody{Reader: strings.NewReader(`{"error":{"message":"bad input"}}`)}
	resp := &http.Response{
		StatusCode: http.StatusBadRequest,
		Header: http.Header{
			"Content-Type": []string{"application/json"},
			"X-Request-Id": []string{"req-structured"},
		},
		Body: body,
	}

	upstream, err := collectOpenCodeGoErrorResponse(resp, protocolconv.ProtocolOpenAIChat, false)
	if err != nil {
		t.Fatalf("collectOpenCodeGoErrorResponse error: %v", err)
	}
	if upstream.StatusCode != http.StatusBadRequest || upstream.ActualProtocol != protocolconv.ProtocolOpenAIChat {
		t.Fatalf("unexpected structured error: %+v", upstream)
	}
	if upstream.RequestID != "req-structured" || string(upstream.Body) != `{"error":{"message":"bad input"}}` {
		t.Fatalf("structured error lost metadata or body: %+v", upstream)
	}
	resp.Header.Set("X-Request-Id", "mutated")
	if upstream.Headers.Get("X-Request-Id") != "req-structured" {
		t.Fatalf("structured headers must be detached: %v", upstream.Headers)
	}
	if body.closeCount != 0 {
		t.Fatalf("non-stream collector must not close caller-owned body, got %d closes", body.closeCount)
	}
}

func TestCollectOpenCodeGoErrorStreamOwnsAndClosesBody(t *testing.T) {
	body := &openCodeGoCloseTrackingBody{Reader: strings.NewReader(`{"error":{"message":"bad stream"}}`)}
	resp := &http.Response{
		StatusCode: http.StatusBadRequest,
		Header:     http.Header{"X-Request-Id": []string{"req-stream-error"}},
		Body:       body,
	}

	upstream, err := collectOpenCodeGoErrorResponse(resp, protocolconv.ProtocolAnthropic, true)
	if err != nil {
		t.Fatalf("collectOpenCodeGoErrorResponse error: %v", err)
	}
	if upstream.ActualProtocol != protocolconv.ProtocolAnthropic || string(upstream.Body) != `{"error":{"message":"bad stream"}}` {
		t.Fatalf("unexpected structured stream error: %+v", upstream)
	}
	if body.closeCount != 1 {
		t.Fatalf("structured stream must close transferred error body exactly once, got %d", body.closeCount)
	}
	if resp.Body != http.NoBody {
		t.Fatalf("response body ownership must transfer to structured stream")
	}
}

func TestOpenCodeGoGatewayServiceImmediateCrossProtocolErrorStaysBeforeSSECommit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	errorBody := `{"error":{"type":"invalid_request_error","message":"max_tokens must be greater than zero"}}`
	responseBody := &openCodeGoCloseTrackingBody{Reader: strings.NewReader(errorBody)}
	upstream := &openCodeGoHTTPUpstreamStub{
		resp: &http.Response{
			StatusCode: http.StatusBadRequest,
			Header: http.Header{
				"Content-Type": []string{"application/json"},
				"X-Request-Id": []string{"req-immediate"},
			},
			Body: responseBody,
		},
	}
	svc := &OpenCodeGoGatewayService{httpUpstream: upstream, cfg: &config.Config{}}
	account := &Account{
		ID: 42, Platform: PlatformOpenCodeGo, Type: AccountTypeAPIKey, Concurrency: 1,
		Credentials: map[string]any{
			"api_key": "ocg-secret",
			"model_protocols": map[string]any{
				"kimi-k2.7-code": OpenCodeGoProtocolChatCompletions,
			},
		},
	}
	requestBody := `{"model":"kimi-k2.7-code","messages":[{"role":"user","content":"hi"}],"max_tokens":0,"stream":true}`
	rec := newTestGinContextRecorder(http.MethodPost, "/v1/messages", requestBody)

	_, err := svc.ForwardMessages(context.Background(), rec.Context, account, []byte(requestBody))
	if err == nil {
		t.Fatal("expected immediate upstream error")
	}
	if rec.Recorder.Code != http.StatusBadRequest {
		t.Fatalf("immediate stream error must retain upstream status before SSE commitment: code=%d body=%s", rec.Recorder.Code, rec.Recorder.Body.String())
	}
	if rec.Recorder.Body.String() != errorBody {
		t.Fatalf("immediate error body must remain raw passthrough: %s", rec.Recorder.Body.String())
	}
	if strings.Contains(rec.Recorder.Body.String(), "data:") || strings.Contains(rec.Recorder.Body.String(), "event:") {
		t.Fatalf("immediate error must not be buried in SSE: %s", rec.Recorder.Body.String())
	}
	if responseBody.closeCount != 1 {
		t.Fatalf("upstream error body must close exactly once, got %d", responseBody.closeCount)
	}
}

func TestOpenCodeGoCrossProtocolImmediateErrorsUseSourceEnvelope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, source := range []protocolconv.Protocol{protocolconv.ProtocolOpenAIResponses, protocolconv.ProtocolGoogleGenAI} {
		t.Run(source.String(), func(t *testing.T) {
			upstream := &openCodeGoHTTPUpstreamStub{resp: &http.Response{
				StatusCode: http.StatusBadRequest,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"error":{"type":"invalid_request_error","message":"bad provider input"}}`)),
			}}
			svc := &OpenCodeGoGatewayService{httpUpstream: upstream, cfg: &config.Config{}}
			account := &Account{ID: 42, Platform: PlatformOpenCodeGo, Type: AccountTypeAPIKey, Concurrency: 1, Credentials: map[string]any{
				"api_key": "ocg-secret", "base_url": "https://opencode.ai/zen/go/v1",
				"model_protocols": map[string]any{"matrix-model": OpenCodeGoProtocolChatCompletions},
			}}
			requestBody := `{"model":"matrix-model","input":"hi"}`
			if source == protocolconv.ProtocolGoogleGenAI {
				requestBody = `{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`
			}
			rec := newTestGinContextRecorder(http.MethodPost, "/matrix", requestBody)
			var err error
			if source == protocolconv.ProtocolOpenAIResponses {
				_, err = svc.ForwardResponses(context.Background(), rec.Context, account, []byte(requestBody), "matrix-model")
			} else {
				_, err = svc.ForwardGoogleGenAI(context.Background(), rec.Context, account, []byte(requestBody), "matrix-model", "matrix-model", false)
			}
			if err == nil {
				t.Fatal("expected upstream error")
			}
			if rec.Recorder.Code != http.StatusBadRequest {
				t.Fatalf("unexpected status: %d body=%s", rec.Recorder.Code, rec.Recorder.Body.String())
			}
			wire := rec.Recorder.Body.Bytes()
			if source == protocolconv.ProtocolOpenAIResponses {
				if gjson.GetBytes(wire, "error.message").String() != "bad provider input" || gjson.GetBytes(wire, "error.type").String() != "upstream_error" {
					t.Fatalf("invalid Responses error envelope: %s", wire)
				}
			} else if gjson.GetBytes(wire, "error.status").String() != "INVALID_ARGUMENT" || gjson.GetBytes(wire, "error.message").String() != "bad provider input" {
				t.Fatalf("invalid Google error envelope: %s", wire)
			}
		})
	}
}

func TestShouldFailoverOpenCodeGoResponse(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		want   bool
	}{
		{
			name:   "console go transient provider failure",
			status: http.StatusBadRequest,
			body:   `{"error":{"type":"invalid_request_error","message":"Error from provider (Console Go): Upstream request failed"}}`,
			want:   true,
		},
		{
			name:   "client validation failure",
			status: http.StatusBadRequest,
			body:   `{"error":{"type":"invalid_request_error","message":"max_tokens must be greater than zero"}}`,
			want:   false,
		},
		{
			name:   "provider wording alone is insufficient",
			status: http.StatusBadRequest,
			body:   `{"error":{"type":"invalid_request_error","message":"Error from provider: invalid model"}}`,
			want:   false,
		},
		{
			name:   "server failure",
			status: http.StatusBadGateway,
			body:   `{"error":{"message":"bad gateway"}}`,
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldFailoverOpenCodeGoResponse(tt.status, []byte(tt.body)); got != tt.want {
				t.Fatalf("shouldFailoverOpenCodeGoResponse() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestOpenCodeGoGatewayServiceProviderWrappedBadRequestTriggersFailover(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := `{"error":{"code":"invalid_request_error","message":"Error from provider (Console Go): Upstream request failed","type":"invalid_request_error"}}`
	upstream := &openCodeGoHTTPUpstreamStub{
		resp: &http.Response{
			StatusCode: http.StatusBadRequest,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(body)),
		},
	}
	svc := &OpenCodeGoGatewayService{httpUpstream: upstream, cfg: &config.Config{}}
	account := &Account{
		ID:          42,
		Platform:    PlatformOpenCodeGo,
		Type:        AccountTypeAPIKey,
		Concurrency: 1,
		Credentials: map[string]any{
			"api_key": "ocg-secret",
			"model_protocols": map[string]any{
				"kimi-k2.7-code": OpenCodeGoProtocolChatCompletions,
			},
		},
	}
	rec := newTestGinContextRecorder(http.MethodPost, "/v1/messages", `{"model":"kimi-k2.7-code","messages":[{"role":"user","content":"hi"}],"max_tokens":128,"stream":true}`)

	_, err := svc.ForwardMessages(context.Background(), rec.Context, account, []byte(`{"model":"kimi-k2.7-code","messages":[{"role":"user","content":"hi"}],"max_tokens":128,"stream":true}`))
	var failoverErr *UpstreamFailoverError
	if !errors.As(err, &failoverErr) {
		t.Fatalf("expected UpstreamFailoverError, got %T: %v", err, err)
	}
	if failoverErr.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected upstream status 400, got %d", failoverErr.StatusCode)
	}
	if rec.Recorder.Body.Len() != 0 {
		t.Fatalf("failover response must remain unwritten, got %s", rec.Recorder.Body.String())
	}
}

func TestOpenCodeGoGatewayServiceStreamFirstTokenIncludesUpstreamWait(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := &openCodeGoHTTPUpstreamStub{
		delay: 50 * time.Millisecond,
		resp: &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body: io.NopCloser(strings.NewReader(
				`data: {"id":"chatcmpl_ok","object":"chat.completion.chunk","model":"kimi-k2.7-code","choices":[{"index":0,"delta":{"content":"ok"}}],"usage":{"prompt_tokens":3,"completion_tokens":2}}` + "\n\n" +
					`data: [DONE]` + "\n\n",
			)),
		},
	}
	svc := &OpenCodeGoGatewayService{
		httpUpstream: upstream,
		cfg:          &config.Config{},
	}
	account := &Account{
		ID:          42,
		Platform:    PlatformOpenCodeGo,
		Type:        AccountTypeAPIKey,
		Concurrency: 1,
		Credentials: map[string]any{
			"api_key":  "ocg-secret",
			"base_url": "https://opencode.ai/zen/go/v1",
			"model_protocols": map[string]any{
				"kimi-k2.7-code": OpenCodeGoProtocolChatCompletions,
			},
		},
	}

	rec := newTestGinContextRecorder(http.MethodPost, "/v1/chat/completions", `{"model":"kimi-k2.7-code","messages":[{"role":"user","content":"hi"}],"stream":true}`)
	result, err := svc.ForwardChatCompletions(context.Background(), rec.Context, account, []byte(`{"model":"kimi-k2.7-code","messages":[{"role":"user","content":"hi"}],"stream":true}`))
	if err != nil {
		t.Fatalf("ForwardChatCompletions error: %v", err)
	}
	if result == nil || result.FirstTokenMs == nil {
		t.Fatalf("expected first token latency, result=%+v", result)
	}
	if *result.FirstTokenMs < 40 {
		t.Fatalf("expected first token latency to include upstream wait, got %dms", *result.FirstTokenMs)
	}
}

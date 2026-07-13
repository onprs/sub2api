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

package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func newCommandCodeGatewayForTest(upstream *commandCodeHTTPUpstreamStub) *CommandCodeGatewayService {
	client := NewCommandCodeClient(upstream, &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{Enabled: false}}}, nil)
	return NewCommandCodeGatewayService(client, &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{Enabled: false}}}, nil)
}

func commandCodeGatewayTestAccount() *Account {
	return &Account{
		ID: 201, Platform: PlatformCommandCode, Type: AccountTypeAPIKey, Concurrency: 1,
		Credentials: map[string]any{"api_key": "cc-key", "base_url": DefaultCommandCodeBaseURL},
	}
}

func newCommandCodeTestContext() (*httptest.ResponseRecorder, *gin.Context) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	return recorder, c
}

func TestCommandCodeGatewayRoutesChatModelToChatCompletions(t *testing.T) {
	upstream := &commandCodeHTTPUpstreamStub{responses: map[string]*http.Response{
		"/provider/v1/chat/completions": commandCodeJSONResponse(http.StatusOK, `{
			"id":"chatcmpl-cc",
			"object":"chat.completion",
			"model":"deepseek/deepseek-v4-flash",
			"choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],
			"usage":{"prompt_tokens":8,"completion_tokens":2,"total_tokens":10,"prompt_tokens_details":{"cached_tokens":3}}
		}`),
	}}
	svc := newCommandCodeGatewayForTest(upstream)
	recorder, c := newCommandCodeTestContext()

	result, err := svc.ForwardChatCompletions(context.Background(), c, commandCodeGatewayTestAccount(), []byte(`{
		"model":"deepseek/deepseek-v4-flash",
		"messages":[{"role":"user","content":"hi"}],
		"stream":false
	}`))
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "ok", gjson.Get(recorder.Body.String(), "choices.0.message.content").String())
	require.Equal(t, 3, result.Usage.CacheReadInputTokens)
	require.Equal(t, "deepseek/deepseek-v4-flash", result.Model)

	require.Len(t, upstream.requests, 1)
	req := upstream.requests[0]
	require.Equal(t, "https://api.commandcode.ai/provider/v1/chat/completions", req.URL.String())
	require.Equal(t, "Bearer cc-key", req.Header.Get("Authorization"))
	require.Empty(t, req.Header.Get("x-api-key"))
}

func TestCommandCodeGatewayRoutesClaudeModelToMessages(t *testing.T) {
	upstream := &commandCodeHTTPUpstreamStub{responses: map[string]*http.Response{
		"/provider/v1/messages": commandCodeJSONResponse(http.StatusOK, `{
			"id":"msg_cc",
			"type":"message",
			"role":"assistant",
			"model":"claude-sonnet-5",
			"content":[{"type":"text","text":"hello"}],
			"stop_reason":"end_turn",
			"usage":{"input_tokens":6,"output_tokens":3}
		}`),
	}}
	svc := newCommandCodeGatewayForTest(upstream)
	recorder, c := newCommandCodeTestContext()

	result, err := svc.ForwardChatCompletions(context.Background(), c, commandCodeGatewayTestAccount(), []byte(`{
		"model":"claude-sonnet-5",
		"messages":[{"role":"user","content":"hi"}],
		"stream":false
	}`))
	require.NoError(t, err)
	require.NotNil(t, result)
	// 入口是 Chat Completions，上游返回 Anthropic，渲染回 Chat 响应。
	require.Equal(t, "hello", gjson.Get(recorder.Body.String(), "choices.0.message.content").String())
	require.Equal(t, 6, result.Usage.InputTokens)
	require.Equal(t, 3, result.Usage.OutputTokens)
	require.Equal(t, "claude-sonnet-5", result.Model)

	require.Len(t, upstream.requests, 1)
	req := upstream.requests[0]
	require.Equal(t, "https://api.commandcode.ai/provider/v1/messages", req.URL.String())
	require.Equal(t, "Bearer cc-key", req.Header.Get("Authorization"))
	require.Equal(t, "cc-key", req.Header.Get("x-api-key"))
	require.Equal(t, "2023-06-01", req.Header.Get("anthropic-version"))

	body, readErr := io.ReadAll(req.Body)
	require.NoError(t, readErr)
	// Chat 入口请求已被转换为 Anthropic Messages 结构。
	require.Equal(t, "user", gjson.GetBytes(body, "messages.0.role").String())
	require.Equal(t, "claude-sonnet-5", gjson.GetBytes(body, "model").String())
}

func TestCommandCodeGatewayMessagesIngressToChatUpstream(t *testing.T) {
	upstream := &commandCodeHTTPUpstreamStub{responses: map[string]*http.Response{
		"/provider/v1/chat/completions": commandCodeJSONResponse(http.StatusOK, `{
			"id":"chatcmpl-cc",
			"object":"chat.completion",
			"model":"glm-5.2",
			"choices":[{"index":0,"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}],
			"usage":{"prompt_tokens":5,"completion_tokens":2,"total_tokens":7}
		}`),
	}}
	svc := newCommandCodeGatewayForTest(upstream)
	recorder, c := newCommandCodeTestContext()

	result, err := svc.ForwardMessages(context.Background(), c, commandCodeGatewayTestAccount(), []byte(`{
		"model":"zai-org/GLM-5.2",
		"max_tokens":64,
		"messages":[{"role":"user","content":"hi"}],
		"stream":false
	}`))
	require.NoError(t, err)
	require.NotNil(t, result)
	// 入口是 Anthropic Messages，上游返回 Chat，渲染回 Anthropic 响应。
	require.Equal(t, "done", gjson.Get(recorder.Body.String(), "content.0.text").String())
	require.Equal(t, 5, result.Usage.InputTokens)
	require.Equal(t, "zai-org/GLM-5.2", result.Model)

	require.Len(t, upstream.requests, 1)
	req := upstream.requests[0]
	require.Equal(t, "https://api.commandcode.ai/provider/v1/chat/completions", req.URL.String())
	require.Empty(t, req.Header.Get("x-api-key"))

	body, readErr := io.ReadAll(req.Body)
	require.NoError(t, readErr)
	require.Equal(t, "zai-org/GLM-5.2", gjson.GetBytes(body, "model").String())
	// Anthropic 消息已转换为 Chat messages 数组。
	require.Equal(t, "user", gjson.GetBytes(body, "messages.0.role").String())
}

func TestCommandCodeGatewayStreamChatToAnthropic(t *testing.T) {
	streamBody := strings.Join([]string{
		`data: {"id":"chatcmpl-cc","object":"chat.completion.chunk","model":"qwen/qwen3.7-plus","choices":[{"index":0,"delta":{"role":"assistant","content":"he"},"finish_reason":null}]}`,
		"",
		`data: {"id":"chatcmpl-cc","object":"chat.completion.chunk","model":"qwen/qwen3.7-plus","choices":[{"index":0,"delta":{"content":"llo"},"finish_reason":null}]}`,
		"",
		`data: {"id":"chatcmpl-cc","object":"chat.completion.chunk","model":"qwen/qwen3.7-plus","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":9,"completion_tokens":2,"total_tokens":11,"prompt_tokens_details":{"cached_tokens":4}}}`,
		"",
		"data: [DONE]",
		"",
	}, "\n")
	upstream := &commandCodeHTTPUpstreamStub{responses: map[string]*http.Response{
		"/provider/v1/chat/completions": {
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader(streamBody)),
		},
	}}
	svc := newCommandCodeGatewayForTest(upstream)
	recorder, c := newCommandCodeTestContext()

	result, err := svc.ForwardMessages(context.Background(), c, commandCodeGatewayTestAccount(), []byte(`{
		"model":"qwen/qwen3.7-plus","max_tokens":64,"messages":[{"role":"user","content":"hi"}],"stream":true
	}`))
	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.Stream)
	require.Equal(t, 4, result.Usage.CacheReadInputTokens)

	body := recorder.Body.String()
	require.Contains(t, body, "event: message_start")
	require.Contains(t, body, "event: content_block_delta")
	require.Contains(t, body, "event: message_stop")

	requestBody, readErr := io.ReadAll(upstream.requests[0].Body)
	require.NoError(t, readErr)
	require.True(t, gjson.GetBytes(requestBody, "stream_options.include_usage").Bool())
}

func TestCommandCodeGatewayFailoverOnAuthErrors(t *testing.T) {
	upstream := &commandCodeHTTPUpstreamStub{responses: map[string]*http.Response{
		"/provider/v1/chat/completions": commandCodeJSONResponse(http.StatusUnauthorized, `{
			"error":{"type":"authentication_error","code":"authentication_error","message":"Missing or invalid auth"}
		}`),
	}}
	svc := newCommandCodeGatewayForTest(upstream)
	recorder, c := newCommandCodeTestContext()

	result, err := svc.ForwardChatCompletions(context.Background(), c, commandCodeGatewayTestAccount(), []byte(`{
		"model":"deepseek/deepseek-v4-flash","messages":[{"role":"user","content":"hi"}],"stream":false
	}`))
	require.Error(t, err)
	require.Nil(t, result)
	var failover *UpstreamFailoverError
	require.ErrorAs(t, err, &failover)
	require.Equal(t, http.StatusUnauthorized, failover.StatusCode)
	_ = recorder
}

func TestCommandCodeGatewayRejectsInvalidAccount(t *testing.T) {
	upstream := &commandCodeHTTPUpstreamStub{}
	svc := newCommandCodeGatewayForTest(upstream)
	recorder, c := newCommandCodeTestContext()

	_, err := svc.ForwardChatCompletions(context.Background(), c, &Account{ID: 1, Platform: PlatformOpenRouter, Type: AccountTypeAPIKey}, []byte(`{"model":"x","messages":[]}`))
	require.Error(t, err)
	require.Equal(t, http.StatusBadGateway, recorder.Code)
}

func TestResolveCommandCodeModelProtocol(t *testing.T) {
	account := &Account{Platform: PlatformCommandCode, Type: AccountTypeAPIKey}

	protocol, ok := account.ResolveCommandCodeModelProtocol("claude-sonnet-5")
	require.True(t, ok)
	require.Equal(t, CommandCodeProtocolMessages, protocol)

	protocol, ok = account.ResolveCommandCodeModelProtocol("Claude-Opus-5")
	require.True(t, ok)
	require.Equal(t, CommandCodeProtocolMessages, protocol)

	protocol, ok = account.ResolveCommandCodeModelProtocol("deepseek/deepseek-v4-flash")
	require.True(t, ok)
	require.Equal(t, CommandCodeProtocolChatCompletions, protocol)

	protocol, ok = account.ResolveCommandCodeModelProtocol("Qwen/Qwen3.8-Max")
	require.True(t, ok)
	require.Equal(t, CommandCodeProtocolChatCompletions, protocol)

	_, ok = account.ResolveCommandCodeModelProtocol("  ")
	require.False(t, ok)
}

func TestNormalizeAndValidateCommandCodeAccount(t *testing.T) {
	credentials := map[string]any{"api_key": "cc-key"}
	require.NoError(t, normalizeAndValidateCommandCodeAccount(PlatformCommandCode, AccountTypeAPIKey, credentials))
	require.Equal(t, DefaultCommandCodeBaseURL, credentials["base_url"])

	credentials = map[string]any{"api_key": "cc-key", "base_url": "https://api.commandcode.ai/"}
	require.NoError(t, normalizeAndValidateCommandCodeAccount(PlatformCommandCode, AccountTypeAPIKey, credentials))
	require.Equal(t, "https://api.commandcode.ai", credentials["base_url"])

	err := normalizeAndValidateCommandCodeAccount(PlatformCommandCode, AccountTypeOAuth, map[string]any{"api_key": "cc-key"})
	require.Error(t, err)

	err = normalizeAndValidateCommandCodeAccount(PlatformCommandCode, AccountTypeAPIKey, map[string]any{})
	require.Error(t, err)

	// 其他平台不受影响。
	require.NoError(t, normalizeAndValidateCommandCodeAccount(PlatformAnthropic, AccountTypeOAuth, nil))
}

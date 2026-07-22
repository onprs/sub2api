package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestClinePassGatewayBufferedChatNormalizesTransport(t *testing.T) {
	upstream := &clinePassHTTPUpstreamStub{response: clinePassTestResponse(http.StatusOK, `{
		"success":true,
		"data":{
			"id":"chatcmpl-cp",
			"object":"chat.completion",
			"model":"minimax/minimax-m3",
			"choices":[{"index":0,"message":{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{\"id\":1}"}}]},"finish_reason":"stop"}],
			"usage":{"prompt_tokens":20,"completion_tokens":4,"total_tokens":24,"cache_creation_input_tokens":3,"prompt_tokens_details":{"cached_tokens":5},"completion_tokens_details":{"reasoning_tokens":2},"cost":0}
		}
	}`)}
	svc := newClinePassGatewayForTest(upstream)
	account := clinePassGatewayTestAccount()
	recorder, c := newClinePassTestContext()

	result, err := svc.ForwardChatCompletions(context.Background(), c, account, []byte(`{
		"model":"client-alias",
		"messages":[{"role":"developer","content":"rules"},{"role":"user","content":"run"}],
		"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}],
		"stream":false
	}`))
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "client-alias", gjson.Get(recorder.Body.String(), "model").String())
	require.Equal(t, "tool_calls", gjson.Get(recorder.Body.String(), "choices.0.finish_reason").String())
	require.Equal(t, 3, result.Usage.CacheCreationInputTokens)
	require.Equal(t, 5, result.Usage.CacheReadInputTokens)
	require.Equal(t, "client-alias", result.Model)
	require.Equal(t, "cline-pass/minimax-m3", result.UpstreamModel)

	requestBody, readErr := io.ReadAll(upstream.request.Body)
	require.NoError(t, readErr)
	require.Equal(t, "https://api.cline.bot/api/v1/chat/completions", upstream.request.URL.String())
	require.Equal(t, "Bearer cp-key", upstream.request.Header.Get("Authorization"))
	require.Equal(t, "cline-pass/minimax-m3", gjson.GetBytes(requestBody, "model").String())
	require.Equal(t, "system", gjson.GetBytes(requestBody, "messages.0.role").String())
}

func TestClinePassGatewayStreamNormalizesToolFinishAndUsage(t *testing.T) {
	streamBody := strings.Join([]string{
		`data: {"id":"chatcmpl-cp","object":"chat.completion.chunk","model":"provider/model","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{\"id\":"}}]},"finish_reason":null}]}`,
		"",
		`data: {"id":"chatcmpl-cp","object":"chat.completion.chunk","model":"provider/model","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"1}"}}]},"finish_reason":"stop"}]}`,
		"",
		`data: {"id":"chatcmpl-cp","object":"chat.completion.chunk","model":"provider/model","choices":[],"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12,"cache_creation_input_tokens":2,"prompt_tokens_details":{"cached_tokens":4}}}`,
		"",
		"data: [DONE]",
		"",
	}, "\n")
	upstream := &clinePassHTTPUpstreamStub{response: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(streamBody)),
	}}
	svc := newClinePassGatewayForTest(upstream)
	recorder, c := newClinePassTestContext()

	result, err := svc.ForwardChatCompletions(context.Background(), c, clinePassGatewayTestAccount(), []byte(`{
		"model":"client-alias","messages":[{"role":"user","content":"run"}],"stream":true
	}`))
	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.Stream)
	require.Contains(t, recorder.Body.String(), `"model":"client-alias"`)
	require.Contains(t, recorder.Body.String(), `"finish_reason":"tool_calls"`)
	require.Contains(t, recorder.Body.String(), "data: [DONE]")
	require.Equal(t, 2, result.Usage.CacheCreationInputTokens)
	require.Equal(t, 4, result.Usage.CacheReadInputTokens)

	requestBody, readErr := io.ReadAll(upstream.request.Body)
	require.NoError(t, readErr)
	require.True(t, gjson.GetBytes(requestBody, "stream_options.include_usage").Bool())
}

func TestClinePassGatewayInvalidBufferedEnvelopeFailsOverBeforeCommit(t *testing.T) {
	upstream := &clinePassHTTPUpstreamStub{response: clinePassTestResponse(http.StatusOK, `{"success":true}`)}
	svc := newClinePassGatewayForTest(upstream)
	recorder, c := newClinePassTestContext()

	result, err := svc.ForwardChatCompletions(context.Background(), c, clinePassGatewayTestAccount(), []byte(`{
		"model":"client-alias","messages":[{"role":"user","content":"hi"}]
	}`))
	require.Nil(t, result)
	var failover *UpstreamFailoverError
	require.ErrorAs(t, err, &failover)
	require.Equal(t, http.StatusBadGateway, failover.StatusCode)
	require.Equal(t, 0, recorder.Body.Len())
}

func TestClinePassGatewayStreamMissingTerminalFailsOverBeforeCommit(t *testing.T) {
	upstream := &clinePassHTTPUpstreamStub{response: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader("")),
	}}
	svc := newClinePassGatewayForTest(upstream)
	recorder, c := newClinePassTestContext()

	result, err := svc.ForwardChatCompletions(context.Background(), c, clinePassGatewayTestAccount(), []byte(`{
		"model":"client-alias","messages":[{"role":"user","content":"hi"}],"stream":true
	}`))
	require.Nil(t, result)
	var failover *UpstreamFailoverError
	require.ErrorAs(t, err, &failover)
	require.Equal(t, 0, recorder.Body.Len())
}

func TestClinePassGatewayNestedProvider400DoesNotFailOver(t *testing.T) {
	upstream := &clinePassHTTPUpstreamStub{response: clinePassTestResponse(http.StatusInternalServerError, `{
		"success":false,
		"error":"Provider request failed: {\"status\":400,\"error\":{\"type\":\"invalid_parameter_error\",\"message\":\"developer role is not supported\"}}"
	}`)}
	svc := newClinePassGatewayForTest(upstream)
	recorder, c := newClinePassTestContext()

	result, err := svc.ForwardChatCompletions(context.Background(), c, clinePassGatewayTestAccount(), []byte(`{
		"model":"client-alias","messages":[{"role":"user","content":"hi"}]
	}`))
	require.Nil(t, result)
	require.Error(t, err)
	var failover *UpstreamFailoverError
	require.NotErrorAs(t, err, &failover)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Contains(t, recorder.Body.String(), "developer role is not supported")
}

func TestNewClinePassPipelineRequestConvertsAllIngressProtocolsToChat(t *testing.T) {
	tests := []struct {
		name   string
		source protocolconv.Protocol
		body   string
	}{
		{name: "chat", source: protocolconv.ProtocolOpenAIChat, body: `{"model":"client","messages":[{"role":"developer","content":[{"type":"text","text":"rules","cache_control":{"type":"ephemeral"}}]},{"role":"user","content":"hi"}]}`},
		{name: "responses", source: protocolconv.ProtocolOpenAIResponses, body: `{"model":"client","instructions":[{"type":"input_text","text":"rules","cache_control":{"type":"ephemeral"}}],"input":"hi"}`},
		{name: "anthropic", source: protocolconv.ProtocolAnthropic, body: `{"model":"client","max_tokens":64,"system":[{"type":"text","text":"rules","cache_control":{"type":"ephemeral"}}],"messages":[{"role":"user","content":"hi"}]}`},
		{name: "google", source: protocolconv.ProtocolGoogleGenAI, body: `{"systemInstruction":{"parts":[{"text":"rules","cache_control":{"type":"ephemeral"}}]},"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, converted, err := newClinePassPipelineRequest([]byte(tc.body), tc.source, clinePassGatewayTestAccount(), "client", "cline-pass/glm-5.2")
			require.NoError(t, err)
			normalized, err := normalizeClinePassChatRequest(converted, "cline-pass/glm-5.2")
			require.NoError(t, err)
			require.Equal(t, "cline-pass/glm-5.2", gjson.GetBytes(normalized, "model").String())
			require.True(t, gjson.GetBytes(normalized, "messages").IsArray())
			require.Contains(t, string(normalized), `"cache_control":{"type":"ephemeral"}`)
			for _, role := range gjson.GetBytes(normalized, "messages.#.role").Array() {
				require.NotEqual(t, "developer", role.String())
			}
		})
	}
}

func TestClinePassChatCacheExtensionIsExplicitAndRejectsToolHints(t *testing.T) {
	body := []byte(`{"model":"client","max_tokens":64,"system":[{"type":"text","text":"rules","cache_control":{"type":"ephemeral"}}],"messages":[{"role":"user","content":"hi"}]}`)
	route := protocolconv.Route{Source: protocolconv.ProtocolAnthropic, IntendedTarget: protocolconv.ProtocolOpenAIChat, ClientModel: "client", UpstreamModel: "cline-pass/glm-5.2"}
	pipeline, err := protocolconv.NewPipeline(standardProtocolRegistry, protocolconv.PipelineConfig{
		Route:   route,
		Options: protocolconv.Options{LossPolicy: protocolconv.LossError},
	})
	require.NoError(t, err)
	_, err = pipeline.ConvertRequest(body)
	require.Error(t, err)

	toolBody := []byte(`{"model":"client","max_tokens":64,"messages":[{"role":"user","content":"hi"}],"tools":[{"name":"lookup","description":"lookup","input_schema":{"type":"object"},"cache_control":{"type":"ephemeral"}}]}`)
	_, _, err = newClinePassPipelineRequest(toolBody, protocolconv.ProtocolAnthropic, clinePassGatewayTestAccount(), "client", "cline-pass/glm-5.2")
	require.Error(t, err)
}

func newClinePassGatewayForTest(upstream HTTPUpstream) *ClinePassGatewayService {
	cfg := &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{Enabled: false}}}
	client := NewClinePassClient(upstream, cfg, nil)
	return NewClinePassGatewayService(client, cfg, nil)
}

func clinePassGatewayTestAccount() *Account {
	return &Account{
		ID:          73,
		Name:        "clinepass-test",
		Platform:    PlatformClinePass,
		Type:        AccountTypeAPIKey,
		Concurrency: 1,
		Credentials: map[string]any{
			"api_key":  "cp-key",
			"base_url": DefaultClinePassBaseURL,
			"model_mapping": map[string]any{
				"client-alias": "cline-pass/minimax-m3",
			},
		},
	}
}

func clinePassTestResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}, "X-Request-Id": []string{"req-cp"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func newClinePassTestContext() (*httptest.ResponseRecorder, *gin.Context) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	return recorder, c
}

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

func TestGeminiForwardAsResponses_NonStreamingUsesGooglePipeline(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstreamBody := `{"responseId":"google-resp","modelVersion":"gemini-upstream","candidates":[{"content":{"role":"model","parts":[{"functionCall":{"name":"lookup","args":{"q":"x"}}}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":5,"candidatesTokenCount":2,"totalTokenCount":7}}`
	upstream := &geminiCompatHTTPUpstreamStub{response: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}, "x-request-id": []string{"rid-google-responses"}},
		Body:       io.NopCloser(strings.NewReader(upstreamBody)),
	}}
	svc := &GeminiMessagesCompatService{httpUpstream: upstream, cfg: &config.Config{}}
	account := geminiResponsesAPIKeyAccount()
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	body := []byte(`{"model":"responses-client","input":[{"type":"function_call","call_id":"call_previous","name":"lookup","arguments":"{\"q\":\"x\"}"},{"type":"function_call_output","call_id":"call_previous","output":"found"}],"tools":[{"type":"function","name":"lookup","parameters":{"type":"object"}}]}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(string(body)))

	result, err := svc.ForwardAsResponses(context.Background(), c, account, body)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "responses-client", result.Model)
	require.Equal(t, "gemini-upstream", result.UpstreamModel)
	require.Equal(t, 5, result.Usage.InputTokens)
	require.Equal(t, 2, result.Usage.OutputTokens)
	require.Contains(t, upstream.lastReq.URL.String(), "/v1beta/models/gemini-upstream:generateContent")
	upstreamRequestBody, readErr := io.ReadAll(upstream.lastReq.Body)
	require.NoError(t, readErr)
	require.Equal(t, "lookup", gjson.GetBytes(upstreamRequestBody, "contents.1.parts.0.functionResponse.name").String())
	require.Equal(t, "found", gjson.GetBytes(upstreamRequestBody, "contents.1.parts.0.functionResponse.response.content").String())
	require.Equal(t, "responses-client", gjson.GetBytes(rec.Body.Bytes(), "model").String())
	require.Equal(t, "completed", gjson.GetBytes(rec.Body.Bytes(), "status").String())
	require.Equal(t, "function_call", gjson.GetBytes(rec.Body.Bytes(), "output.0.type").String())
	require.Equal(t, "lookup", gjson.GetBytes(rec.Body.Bytes(), "output.0.name").String())
	require.Equal(t, "call_google_0_0", gjson.GetBytes(rec.Body.Bytes(), "output.0.call_id").String())
	require.Equal(t, int64(5), gjson.GetBytes(rec.Body.Bytes(), "usage.input_tokens").Int())
}

func TestGeminiForwardAsResponses_ChannelAndAccountMappingRestoreInboundModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := &geminiCompatHTTPUpstreamStub{response: &http.Response{
		StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(strings.NewReader(`{"responseId":"google-map","modelVersion":"gemini-upstream","candidates":[{"content":{"parts":[{"text":"ok"}]},"finishReason":"STOP"}]}`)),
	}}
	svc := &GeminiMessagesCompatService{httpUpstream: upstream, cfg: &config.Config{}}
	account := geminiResponsesAPIKeyAccount()
	account.Credentials["model_mapping"] = map[string]any{"channel-target": "gemini-upstream"}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	body := []byte(`{"model":"channel-target","input":"hello"}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(string(body)))

	result, err := svc.ForwardAsResponsesWithClientModel(context.Background(), c, account, body, "client-alias")
	require.NoError(t, err)
	require.Equal(t, "client-alias", result.Model)
	require.Equal(t, "gemini-upstream", result.UpstreamModel)
	require.Equal(t, "client-alias", gjson.GetBytes(rec.Body.Bytes(), "model").String())
	require.Contains(t, upstream.lastReq.URL.String(), "/models/gemini-upstream:generateContent")
}

func TestGeminiForwardAsResponses_StreamingUsesResponsesRenderer(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstreamBody := strings.Join([]string{
		`data: {"responseId":"google-stream","modelVersion":"gemini-upstream","candidates":[{"content":{"role":"model","parts":[{"text":"hello"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":4,"candidatesTokenCount":1,"totalTokenCount":5}}`,
		"",
		"data: [DONE]",
		"",
	}, "\n")
	upstream := &geminiCompatHTTPUpstreamStub{response: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}, "x-request-id": []string{"rid-google-responses-stream"}},
		Body:       io.NopCloser(strings.NewReader(upstreamBody)),
	}}
	svc := &GeminiMessagesCompatService{httpUpstream: upstream, cfg: &config.Config{}}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	body := []byte(`{"model":"responses-client","stream":true,"input":"hello"}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(string(body)))

	result, err := svc.ForwardAsResponses(context.Background(), c, geminiResponsesAPIKeyAccount(), body)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.Stream)
	require.Contains(t, upstream.lastReq.URL.String(), "/v1beta/models/gemini-upstream:streamGenerateContent?alt=sse")
	require.Equal(t, 4, result.Usage.InputTokens)
	require.Equal(t, 1, result.Usage.OutputTokens)
	wire := rec.Body.String()
	require.Contains(t, wire, "event: response.created\n")
	require.Contains(t, wire, `"model":"responses-client"`)
	require.Contains(t, wire, `event: response.output_text.delta`)
	require.Contains(t, wire, `"delta":"hello"`)
	require.Contains(t, wire, "event: response.completed\n")
	require.True(t, strings.HasSuffix(wire, "data: [DONE]\n\n"))
}

func TestGeminiForwardAsResponses_ClientDisconnectDrainsTerminalUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstreamBody := strings.Join([]string{
		`data: {"responseId":"google-stream","candidates":[{"content":{"role":"model","parts":[{"text":"hello"}]}}]}`,
		"",
		`data: {"responseId":"google-stream","candidates":[{"content":{"role":"model","parts":[]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":9,"candidatesTokenCount":3,"totalTokenCount":12}}`,
		"",
		"data: [DONE]",
		"",
	}, "\n")
	upstream := &geminiCompatHTTPUpstreamStub{response: &http.Response{
		StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}},
		Body: io.NopCloser(strings.NewReader(upstreamBody)),
	}}
	svc := &GeminiMessagesCompatService{httpUpstream: upstream, cfg: &config.Config{}}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Writer = &failWriteResponseWriter{ResponseWriter: c.Writer}
	body := []byte(`{"model":"responses-client","stream":true,"input":"hello"}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(string(body)))

	result, err := svc.ForwardAsResponses(context.Background(), c, geminiResponsesAPIKeyAccount(), body)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.ClientDisconnect)
	require.Equal(t, 9, result.Usage.InputTokens)
	require.Equal(t, 3, result.Usage.OutputTokens)
}

func TestGeminiForwardAsResponses_ProviderErrorUsesResponsesRenderer(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := &geminiCompatHTTPUpstreamStub{response: &http.Response{
		StatusCode: http.StatusBadRequest,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"error":{"code":400,"status":"INVALID_ARGUMENT","message":"bad Google request"}}`)),
	}}
	svc := &GeminiMessagesCompatService{httpUpstream: upstream, cfg: &config.Config{}}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	body := []byte(`{"model":"responses-client","input":"hello"}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(string(body)))

	result, err := svc.ForwardAsResponses(context.Background(), c, geminiResponsesAPIKeyAccount(), body)
	require.Nil(t, result)
	require.Error(t, err)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Equal(t, "invalid_request_error", gjson.GetBytes(rec.Body.Bytes(), "error.type").String())
	require.Equal(t, "invalid_request_error", gjson.GetBytes(rec.Body.Bytes(), "error.code").String())
	require.Contains(t, gjson.GetBytes(rec.Body.Bytes(), "error.message").String(), "bad Google request")
}

func TestGeminiForwardAsResponses_MalformedFirstStreamEventDoesNotCommitSuccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := &geminiCompatHTTPUpstreamStub{response: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader("data: not-json\n\n")),
	}}
	svc := &GeminiMessagesCompatService{httpUpstream: upstream, cfg: &config.Config{}}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	body := []byte(`{"model":"responses-client","stream":true,"input":"hello"}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(string(body)))

	result, err := svc.ForwardAsResponses(context.Background(), c, geminiResponsesAPIKeyAccount(), body)
	require.Nil(t, result)
	require.Error(t, err)
	require.Equal(t, 0, rec.Body.Len())
	require.Equal(t, http.StatusOK, rec.Code)
}

func geminiResponsesAPIKeyAccount() *Account {
	return &Account{
		ID: 42, Name: "gemini-responses", Platform: PlatformGemini, Type: AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":       "test-key",
			"model_mapping": map[string]any{"responses-client": "gemini-upstream"},
		},
	}
}

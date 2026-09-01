package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv"
	protocoltransport "github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv/transport"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type nativeGeminiTrackingReadCloser struct {
	io.Reader
	closeCount atomic.Int32
}

func (r *nativeGeminiTrackingReadCloser) Close() error {
	r.closeCount.Add(1)
	return nil
}

type geminiCompatHTTPUpstreamStub struct {
	response  *http.Response
	responses []*http.Response
	err       error
	calls     int
	lastReq   *http.Request
}

func (s *geminiCompatHTTPUpstreamStub) Do(req *http.Request, proxyURL string, accountID int64, accountConcurrency int) (*http.Response, error) {
	s.calls++
	s.lastReq = req
	if s.err != nil {
		return nil, s.err
	}
	if len(s.responses) > 0 {
		resp := *s.responses[0]
		s.responses = s.responses[1:]
		return &resp, nil
	}
	if s.response == nil {
		return nil, fmt.Errorf("missing stub response")
	}
	resp := *s.response
	return &resp, nil
}

func (s *geminiCompatHTTPUpstreamStub) DoWithTLS(req *http.Request, proxyURL string, accountID int64, accountConcurrency int, profile *tlsfingerprint.Profile) (*http.Response, error) {
	return s.Do(req, proxyURL, accountID, accountConcurrency)
}

func TestGeminiMessagesCompatSignatureRepairRunsBeforeSkippedErrorPolicy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := &geminiCompatHTTPUpstreamStub{responses: []*http.Response{
		{
			StatusCode: http.StatusBadRequest,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(
				`{"error":{"code":400,"message":"Corrupted thought signature.","status":"INVALID_ARGUMENT"}}`,
			)),
		},
		{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(
				`{"candidates":[{"content":{"role":"model","parts":[{"text":"repaired"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":2,"candidatesTokenCount":1}}`,
			)),
		},
	}}
	rateLimitService := NewRateLimitService(nil, nil, &config.Config{}, nil, nil)
	svc := &GeminiMessagesCompatService{
		httpUpstream:     upstream,
		rateLimitService: rateLimitService,
		cfg:              &config.Config{},
	}
	account := &Account{
		ID:       107,
		Platform: PlatformGemini,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":                    "test-key",
			"custom_error_codes_enabled": true,
			"custom_error_codes":         []any{float64(http.StatusTooManyRequests)},
		},
	}
	body := []byte(`{"model":"claude-sonnet-4-5","max_tokens":64,"thinking":{"type":"enabled","budget_tokens":16},"messages":[{"role":"user","content":"hi"}]}`)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))

	result, err := svc.Forward(context.Background(), c, account, body)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, 2, upstream.calls)
	require.Contains(t, recorder.Body.String(), "repaired")
}

func TestGeminiForwardAsChatCompletions_OAuthRoutesToGeminiAndReturnsChatFormat(t *testing.T) {
	gin.SetMode(gin.TestMode)

	upstreamBody := `data: {"response":{"candidates":[{"content":{"parts":[{"text":"hello from gemini"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":7,"candidatesTokenCount":3}}}` + "\n\n" +
		"data: [DONE]\n\n"
	httpStub := &geminiCompatHTTPUpstreamStub{
		response: &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader(upstreamBody)),
		},
	}
	svc := &GeminiMessagesCompatService{
		tokenProvider: &GeminiTokenProvider{},
		httpUpstream:  httpStub,
		cfg:           &config.Config{},
	}
	account := &Account{
		ID:       101,
		Platform: PlatformGemini,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token": "ya29.test-token",
			"project_id":   "project-1",
		},
		Concurrency: 1,
	}

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	body := []byte(`{"model":"gemini-2.5-flash","messages":[{"role":"user","content":"hi"}]}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))

	result, err := svc.ForwardAsChatCompletions(context.Background(), c, account, body)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "gemini-2.5-flash", result.Model)
	require.Equal(t, 7, result.Usage.InputTokens)
	require.Equal(t, 3, result.Usage.OutputTokens)

	require.NotNil(t, httpStub.lastReq)
	require.Contains(t, httpStub.lastReq.URL.String(), "/v1internal:streamGenerateContent?alt=sse")
	require.Equal(t, "Bearer ya29.test-token", httpStub.lastReq.Header.Get("Authorization"))
	require.Empty(t, httpStub.lastReq.Header.Get("x-api-key"))
	require.Empty(t, httpStub.lastReq.Header.Get("anthropic-version"))

	var sent map[string]any
	sentBody, err := io.ReadAll(httpStub.lastReq.Body)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(sentBody, &sent))
	require.Equal(t, "gemini-2.5-flash", sent["model"])
	require.Equal(t, "project-1", sent["project"])
	require.Contains(t, fmt.Sprint(sent["request"]), "hi")

	var got map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Equal(t, "chat.completion", got["object"])
	require.Equal(t, "gemini-2.5-flash", got["model"])
	choices, ok := got["choices"].([]any)
	require.True(t, ok)
	require.NotEmpty(t, choices)
	choice, ok := choices[0].(map[string]any)
	require.True(t, ok)
	message, ok := choice["message"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "assistant", message["role"])
	require.Equal(t, "hello from gemini", message["content"])
	usage, ok := got["usage"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, float64(7), usage["prompt_tokens"])
	require.Equal(t, float64(3), usage["completion_tokens"])
	require.Equal(t, float64(10), usage["total_tokens"])
}

func TestGeminiForwardAsChatCompletions_RestoresClientModelAfterMappedGoogleResponse(t *testing.T) {
	upstream := &geminiCompatHTTPUpstreamStub{response: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"responseId":"google-1","modelVersion":"upstream-gemini","candidates":[{"content":{"role":"model","parts":[{"text":"ok"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":3,"candidatesTokenCount":1,"totalTokenCount":4}}`)),
	}}
	svc := &GeminiMessagesCompatService{httpUpstream: upstream, cfg: &config.Config{}}
	account := &Account{ID: 103, Platform: PlatformGemini, Type: AccountTypeAPIKey, Credentials: map[string]any{
		"api_key": "test-key", "model_mapping": map[string]any{"client-gemini": "upstream-gemini"},
	}}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	body := []byte(`{"model":"client-gemini","messages":[{"role":"user","content":"hi"}]}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))

	result, err := svc.ForwardAsChatCompletions(context.Background(), c, account, body)
	require.NoError(t, err)
	require.Equal(t, "upstream-gemini", result.UpstreamModel)
	require.Equal(t, "client-gemini", gjson.GetBytes(rec.Body.Bytes(), "model").String())
	require.Contains(t, upstream.lastReq.URL.String(), "/models/upstream-gemini:generateContent")
}

func TestGeminiForwardAsChatCompletions_ChannelMappingRestoresInboundModel(t *testing.T) {
	upstream := &geminiCompatHTTPUpstreamStub{response: &http.Response{
		StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(strings.NewReader(`{"responseId":"google-channel","modelVersion":"upstream-gemini","candidates":[{"content":{"parts":[{"text":"ok"}]},"finishReason":"STOP"}]}`)),
	}}
	svc := &GeminiMessagesCompatService{httpUpstream: upstream, cfg: &config.Config{}}
	account := &Account{ID: 104, Platform: PlatformGemini, Type: AccountTypeAPIKey, Credentials: map[string]any{
		"api_key": "test-key", "model_mapping": map[string]any{"channel-target": "upstream-gemini"},
	}}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	body := []byte(`{"model":"channel-target","messages":[{"role":"user","content":"hi"}]}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))

	result, err := svc.ForwardAsChatCompletionsWithClientModel(context.Background(), c, account, body, "client-alias")
	require.NoError(t, err)
	require.Equal(t, "client-alias", result.Model)
	require.Equal(t, "upstream-gemini", result.UpstreamModel)
	require.Equal(t, "client-alias", gjson.GetBytes(rec.Body.Bytes(), "model").String())
}

func TestGeminiForwardAsChatCompletions_StreamsOpenAIChunksFromGeminiSSE(t *testing.T) {
	gin.SetMode(gin.TestMode)

	upstreamBody := `data: {"candidates":[{"content":{"parts":[{"text":"hel"}]}}],"usageMetadata":{"promptTokenCount":2,"candidatesTokenCount":1}}` + "\n\n" +
		`data: {"candidates":[{"content":{"parts":[{"text":"hello"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":2,"candidatesTokenCount":2}}` + "\n\n" +
		"data: [DONE]\n\n"
	httpStub := &geminiCompatHTTPUpstreamStub{
		response: &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader(upstreamBody)),
		},
	}
	svc := &GeminiMessagesCompatService{
		httpUpstream: httpStub,
		cfg:          &config.Config{},
	}
	account := &Account{
		ID:       102,
		Platform: PlatformGemini,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key": "gemini-api-key",
		},
		Concurrency: 1,
	}

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	body := []byte(`{"model":"gemini-2.5-flash","stream":true,"stream_options":{"include_usage":true},"messages":[{"role":"user","content":"hi"}]}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))

	result, err := svc.ForwardAsChatCompletions(context.Background(), c, account, body)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, http.StatusOK, rec.Code)
	require.True(t, result.Stream)
	require.Equal(t, 2, result.Usage.InputTokens)
	require.Equal(t, 2, result.Usage.OutputTokens)

	require.NotNil(t, httpStub.lastReq)
	require.Contains(t, httpStub.lastReq.URL.String(), "/v1beta/models/gemini-2.5-flash:streamGenerateContent?alt=sse")
	require.Equal(t, "gemini-api-key", httpStub.lastReq.Header.Get("x-goog-api-key"))

	out := rec.Body.String()
	require.Contains(t, out, `"object":"chat.completion.chunk"`)
	require.Contains(t, out, `"role":"assistant"`)
	require.Contains(t, out, `"content":"hel"`)
	require.Contains(t, out, `"content":"lo"`)
	require.Contains(t, out, `"usage":{"prompt_tokens":2,"completion_tokens":2,"total_tokens":4}`)
	require.Contains(t, out, "data: [DONE]")
}

func TestGeminiForwardAsChatCompletions_OAuthStreamUnwrapsGoogleEnvelope(t *testing.T) {
	upstreamBody := `data: {"response":{"responseId":"google-oauth","modelVersion":"upstream-gemini","candidates":[{"content":{"parts":[{"text":"wrapped"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":2,"candidatesTokenCount":1,"totalTokenCount":3}}}` + "\n\n" +
		"data: [DONE]\n\n"
	upstream := &geminiCompatHTTPUpstreamStub{response: &http.Response{
		StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}},
		Body: io.NopCloser(strings.NewReader(upstreamBody)),
	}}
	svc := &GeminiMessagesCompatService{tokenProvider: &GeminiTokenProvider{}, httpUpstream: upstream, cfg: &config.Config{}}
	account := &Account{ID: 105, Platform: PlatformGemini, Type: AccountTypeOAuth, Credentials: map[string]any{
		"access_token": "test-token", "project_id": "project-1",
	}}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	body := []byte(`{"model":"client-gemini","stream":true,"messages":[{"role":"user","content":"hi"}]}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))

	result, err := svc.ForwardAsChatCompletions(context.Background(), c, account, body)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Contains(t, rec.Body.String(), `"content":"wrapped"`)
	require.Contains(t, rec.Body.String(), `"model":"client-gemini"`)
	require.Equal(t, 1, strings.Count(rec.Body.String(), "data: [DONE]"))
}

func TestGeminiForwardAsChatCompletions_MalformedFirstStreamEventDoesNotCommitSuccess(t *testing.T) {
	upstream := &geminiCompatHTTPUpstreamStub{response: &http.Response{
		StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}},
		Body: io.NopCloser(strings.NewReader("data: {not-json}\n\n")),
	}}
	svc := &GeminiMessagesCompatService{httpUpstream: upstream, cfg: &config.Config{}}
	account := &Account{ID: 106, Platform: PlatformGemini, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "test-key"}}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	body := []byte(`{"model":"client-gemini","stream":true,"messages":[{"role":"user","content":"hi"}]}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))

	result, err := svc.ForwardAsChatCompletions(context.Background(), c, account, body)
	require.Error(t, err)
	require.Nil(t, result)
	require.False(t, c.Writer.Written())
	require.Empty(t, rec.Body.String())
}

func TestGeminiForwardAsChatCompletions_StreamSynthesizesMissingGoogleToolCallID(t *testing.T) {
	upstreamBody := `data: {"candidates":[{"content":{"parts":[{"functionCall":{"name":"lookup","args":{"q":"x"}}}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":2,"candidatesTokenCount":1,"totalTokenCount":3}}` + "\n\n" +
		"data: [DONE]\n\n"
	upstream := &geminiCompatHTTPUpstreamStub{response: &http.Response{
		StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}},
		Body: io.NopCloser(strings.NewReader(upstreamBody)),
	}}
	svc := &GeminiMessagesCompatService{httpUpstream: upstream, cfg: &config.Config{}}
	account := &Account{ID: 104, Platform: PlatformGemini, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "test-key"}}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	body := []byte(`{"model":"gemini-2.5-flash","stream":true,"messages":[{"role":"user","content":"lookup"}],"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}]}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))

	result, err := svc.ForwardAsChatCompletions(context.Background(), c, account, body)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Contains(t, rec.Body.String(), `"id":"call_google_0_0"`)
	require.Contains(t, rec.Body.String(), `"name":"lookup"`)
	require.Equal(t, 1, strings.Count(rec.Body.String(), "data: [DONE]"))
}

func TestGeminiForwardAsChatCompletions_FunctionNamedWebSearchStaysClientSide(t *testing.T) {
	gin.SetMode(gin.TestMode)

	httpStub := &geminiCompatHTTPUpstreamStub{
		response: &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(
				`{"candidates":[{"content":{"parts":[{"text":"hello"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":2,"candidatesTokenCount":1}}`,
			)),
		},
	}
	svc := &GeminiMessagesCompatService{
		httpUpstream: httpStub,
		cfg:          &config.Config{},
	}
	account := &Account{
		ID:       103,
		Platform: PlatformGemini,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key": "gemini-api-key",
		},
		Concurrency: 1,
	}

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	body := []byte(`{
		"model":"gemini-3.6-flash-high",
		"messages":[{"role":"user","content":"search and read"}],
		"tools":[
			{"type":"function","function":{"name":"web_search","description":"Search through the Hermes client","parameters":{"type":"object","properties":{"query":{"type":"string"}},"required":["query"]}}},
			{"type":"function","function":{"name":"read_file","description":"Read a local file","parameters":{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}}}
		]
	}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))

	result, err := svc.ForwardAsChatCompletions(context.Background(), c, account, body)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, http.StatusOK, rec.Code)
	require.NotNil(t, httpStub.lastReq)

	postedBody, err := io.ReadAll(httpStub.lastReq.Body)
	require.NoError(t, err)

	var posted map[string]any
	require.NoError(t, json.Unmarshal(postedBody, &posted))
	tools, ok := posted["tools"].([]any)
	require.True(t, ok)
	require.Len(t, tools, 1, "Chat Completions function tools must not be promoted to Gemini built-ins by name")

	functionTool, ok := tools[0].(map[string]any)
	require.True(t, ok)
	functionDecls, ok := functionTool["functionDeclarations"].([]any)
	require.True(t, ok)
	require.Len(t, functionDecls, 2)
	webSearchDecl, ok := functionDecls[0].(map[string]any)
	require.True(t, ok)
	readFileDecl, ok := functionDecls[1].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "web_search", webSearchDecl["name"])
	require.Equal(t, "read_file", readFileDecl["name"])
	require.NotContains(t, functionTool, "googleSearch")
	require.NotContains(t, functionTool, "google_search")
}

// TestConvertClaudeToolsToGeminiTools_CustomType 测试custom类型工具转换
func TestGeminiHandleNativeNonStreamingResponse_DebugDisabledDoesNotEmitHeaderLogs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	logSink, restore := captureStructuredLog(t)
	defer restore()

	svc := &GeminiMessagesCompatService{
		cfg: &config.Config{
			Gateway: config.GatewayConfig{
				GeminiDebugResponseHeaders: false,
			},
		},
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type":      []string{"application/json"},
			"X-RateLimit-Limit": []string{"60"},
		},
		Body: io.NopCloser(strings.NewReader(`{"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":2}}`)),
	}

	pipeline, err := newGoogleIdentityPipeline(&Account{ID: 1, Platform: PlatformGemini}, "gemini-client", "gemini-upstream")
	require.NoError(t, err)
	_, err = pipeline.ConvertRequest([]byte(`{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`))
	require.NoError(t, err)
	usage, err := svc.handleNativeNonStreamingResponse(c, resp, pipeline, time.Now(), false)
	require.NoError(t, err)
	require.NotNil(t, usage)
	require.False(t, logSink.ContainsMessage("[GeminiAPI]"), "debug 关闭时不应输出 Gemini 响应头日志")
}

func TestGeminiHandleNativeNonStreamingResponse_StructuredGoogleOutputPreservesFieldsAndHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-client:generateContent", nil)
	account := &Account{ID: 201, Platform: PlatformGemini}
	pipeline, err := newGoogleIdentityPipeline(account, "gemini-client", "gemini-upstream")
	require.NoError(t, err)
	_, err = pipeline.ConvertRequest([]byte(`{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`))
	require.NoError(t, err)
	body := []byte(`{"responseId":"google-native","candidates":[{"content":{"role":"model","parts":[{"text":"ok"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":6,"candidatesTokenCount":2},"vendorExtension":{"kept":true}}`)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type":             []string{"application/vnd.google+json"},
			"X-Request-Id":             []string{"rid-native"},
			"X-RateLimit-Limit-Tokens": []string{"60"},
			"X-Internal-Upstream":      []string{"drop-me"},
		},
		Body: io.NopCloser(bytes.NewReader(body)),
	}
	svc := &GeminiMessagesCompatService{cfg: &config.Config{}}

	usage, err := svc.handleNativeNonStreamingResponse(c, resp, pipeline, time.Now(), false)

	require.NoError(t, err)
	require.Equal(t, 6, usage.InputTokens)
	require.Equal(t, 2, usage.OutputTokens)
	require.Equal(t, "application/json", recorder.Header().Get("Content-Type"))
	require.Equal(t, "rid-native", recorder.Header().Get("X-Request-Id"))
	require.Equal(t, "60", recorder.Header().Get("X-RateLimit-Limit-Tokens"))
	require.Empty(t, recorder.Header().Get("X-Internal-Upstream"))
	require.JSONEq(t, string(body), recorder.Body.String())
}

func TestGeminiHandleNativeNonStreamingResponse_InvalidJSONTriggersFailoverBeforeCommit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-client:generateContent", nil)
	account := &Account{ID: 202, Platform: PlatformGemini}
	pipeline, err := newGoogleIdentityPipeline(account, "gemini-client", "gemini-upstream")
	require.NoError(t, err)
	_, err = pipeline.ConvertRequest([]byte(`{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`))
	require.NoError(t, err)
	body := []byte("not-json")
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"X-Request-Id": []string{"rid-invalid"}},
		Body:       io.NopCloser(bytes.NewReader(body)),
	}
	svc := &GeminiMessagesCompatService{cfg: &config.Config{}}

	usage, err := svc.handleNativeNonStreamingResponse(c, resp, pipeline, time.Now(), false)

	require.Nil(t, usage)
	var failoverErr *UpstreamFailoverError
	require.True(t, errors.As(err, &failoverErr))
	require.Equal(t, http.StatusBadGateway, failoverErr.StatusCode)
	require.Equal(t, body, failoverErr.ResponseBody)
	require.Equal(t, "rid-invalid", failoverErr.ResponseHeaders.Get("X-Request-Id"))
	require.True(t, failoverErr.RetryableOnSameAccount)
	require.False(t, c.Writer.Written())
}

func TestGeminiHandleNativeNonStreamingResponse_RequiresCompletedRequestPipeline(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-client:generateContent", nil)
	account := &Account{ID: 202, Platform: PlatformGemini}
	pipeline, err := newGoogleIdentityPipeline(account, "gemini-client", "gemini-upstream")
	require.NoError(t, err)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{"candidates":[]}`)),
	}
	svc := &GeminiMessagesCompatService{cfg: &config.Config{}}

	usage, err := svc.handleNativeNonStreamingResponse(c, resp, pipeline, time.Now(), false)

	require.Nil(t, usage)
	var failoverErr *UpstreamFailoverError
	require.True(t, errors.As(err, &failoverErr))
	require.Equal(t, http.StatusBadGateway, failoverErr.StatusCode)
	require.True(t, failoverErr.RetryableOnSameAccount)
	require.False(t, c.Writer.Written())
}

func TestGeminiMessagesCompatServiceForwardNative_APIKeyUsesIdentityPipeline(t *testing.T) {
	upstreamBody := `{"responseId":"native-api-key","candidates":[{"content":{"role":"model","parts":[{"text":"ok"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":4,"candidatesTokenCount":1},"vendorExtension":{"kept":true}}`
	upstream := &geminiCompatHTTPUpstreamStub{response: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}, "X-Request-Id": []string{"rid-api-key"}},
		Body:       io.NopCloser(strings.NewReader(upstreamBody)),
	}}
	svc := &GeminiMessagesCompatService{httpUpstream: upstream, cfg: &config.Config{}}
	account := &Account{ID: 203, Platform: PlatformGemini, Type: AccountTypeAPIKey, Credentials: map[string]any{
		"api_key": "test-key", "model_mapping": map[string]any{"gemini-client": "gemini-upstream"},
	}}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	body := []byte(`{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-client:generateContent", bytes.NewReader(body))

	result, err := svc.ForwardNative(context.Background(), c, account, "gemini-client", "generateContent", false, body)

	require.NoError(t, err)
	require.Equal(t, protocolconv.ProtocolGoogleGenAI, result.ActualProtocol)
	require.Equal(t, "gemini-client", result.Model)
	require.Equal(t, "gemini-upstream", result.UpstreamModel)
	require.Equal(t, 4, result.Usage.InputTokens)
	require.Equal(t, 1, result.Usage.OutputTokens)
	require.Contains(t, upstream.lastReq.URL.String(), "/models/gemini-upstream:generateContent")
	require.JSONEq(t, string(body), string(readRequestBodyForTest(t, upstream.lastReq)))
	require.JSONEq(t, upstreamBody, recorder.Body.String())
}

func TestGeminiMessagesCompatServiceForwardNative_OAuthStreamAsUnaryUsesIdentityPipeline(t *testing.T) {
	inner := `{"responseId":"native-oauth","candidates":[{"content":{"role":"model","parts":[{"text":"wrapped"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":5,"candidatesTokenCount":2},"vendorExtension":{"kept":true}}`
	upstreamSSE := "data: {\"response\":" + inner + "}\n\ndata: [DONE]\n\n"
	upstream := &geminiCompatHTTPUpstreamStub{response: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}, "X-Request-Id": []string{"rid-oauth"}},
		Body:       io.NopCloser(strings.NewReader(upstreamSSE)),
	}}
	svc := &GeminiMessagesCompatService{tokenProvider: &GeminiTokenProvider{}, httpUpstream: upstream, cfg: &config.Config{}}
	account := &Account{ID: 204, Platform: PlatformGemini, Type: AccountTypeOAuth, Credentials: map[string]any{
		"access_token": "test-token", "project_id": "project-1",
	}}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	body := []byte(`{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-client:generateContent", bytes.NewReader(body))

	result, err := svc.ForwardNative(context.Background(), c, account, "gemini-client", "generateContent", false, body)

	require.NoError(t, err)
	require.Equal(t, 5, result.Usage.InputTokens)
	require.Equal(t, 2, result.Usage.OutputTokens)
	require.Contains(t, upstream.lastReq.URL.String(), "/v1internal:streamGenerateContent?alt=sse")
	sent := readRequestBodyForTest(t, upstream.lastReq)
	require.JSONEq(t, string(body), gjson.GetBytes(sent, "request").Raw)
	require.JSONEq(t, inner, recorder.Body.String())
	require.NotContains(t, recorder.Body.String(), `"response":`)
}

func TestCollectGeminiSSEWithRaw_BoundedStructuredAggregation(t *testing.T) {
	upstreamBody := strings.Join([]string{
		": keepalive",
		"",
		"event: message",
		`data: {"response":{"responseId":"aggregate-1",`,
		`data: "candidates":[{"content":{"parts":[{"text":"hel"}]}}]}}`,
		"",
		`data: {"response":{"responseId":"aggregate-1","candidates":[{"content":{"parts":[{"text":"lo"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":6,"candidatesTokenCount":2},"vendorExtension":{"kept":true}}}`,
		"",
		"data: [DONE]",
		"",
	}, "\n")
	body := &nativeGeminiTrackingReadCloser{Reader: strings.NewReader(upstreamBody)}
	svc := &GeminiMessagesCompatService{cfg: &config.Config{Gateway: config.GatewayConfig{MaxLineSize: 1024}}}

	collected, usage, raw, err := svc.collectGeminiSSEWithRaw(body, true)

	require.NoError(t, err)
	require.Equal(t, int32(1), body.closeCount.Load())
	require.Equal(t, upstreamBody, string(raw))
	require.Equal(t, 6, usage.InputTokens)
	require.Equal(t, 2, usage.OutputTokens)
	encoded, err := json.Marshal(collected)
	require.NoError(t, err)
	require.Equal(t, "hello", gjson.GetBytes(encoded, "candidates.0.content.parts.0.text").String())
	require.True(t, gjson.GetBytes(encoded, "vendorExtension.kept").Bool())
}

func TestCollectGeminiSSEWithRaw_EOFWithoutDoneRemainsCompatible(t *testing.T) {
	upstreamBody := `data: {"candidates":[{"content":{"parts":[{"text":"ok"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":2,"candidatesTokenCount":1}}` + "\n\n"
	body := &nativeGeminiTrackingReadCloser{Reader: strings.NewReader(upstreamBody)}
	svc := &GeminiMessagesCompatService{cfg: &config.Config{}}

	collected, usage, raw, err := svc.collectGeminiSSEWithRaw(body, false)

	require.NoError(t, err)
	require.Equal(t, int32(1), body.closeCount.Load())
	require.Equal(t, upstreamBody, string(raw))
	require.Equal(t, 2, usage.InputTokens)
	require.Equal(t, 1, usage.OutputTokens)
	encoded, err := json.Marshal(collected)
	require.NoError(t, err)
	require.Equal(t, "ok", gjson.GetBytes(encoded, "candidates.0.content.parts.0.text").String())
}

func TestCollectGeminiSSEWithRaw_RejectsMalformedAndOversizedInput(t *testing.T) {
	t.Run("malformed JSON", func(t *testing.T) {
		body := &nativeGeminiTrackingReadCloser{Reader: strings.NewReader("data: {not-json}\n\n")}
		svc := &GeminiMessagesCompatService{cfg: &config.Config{}}

		_, _, raw, err := svc.collectGeminiSSEWithRaw(body, false)

		require.ErrorContains(t, err, "malformed SSE JSON")
		require.Equal(t, "data: {not-json}\n\n", string(raw))
		require.Equal(t, int32(1), body.closeCount.Load())
	})

	t.Run("record limit", func(t *testing.T) {
		body := &nativeGeminiTrackingReadCloser{Reader: strings.NewReader(`data: {"value":"` + strings.Repeat("x", 128) + `"}` + "\n\n")}
		svc := &GeminiMessagesCompatService{cfg: &config.Config{Gateway: config.GatewayConfig{MaxLineSize: 64}}}

		_, _, _, err := svc.collectGeminiSSEWithRaw(body, false)

		var tooLarge *protocoltransport.SSERecordTooLargeError
		require.ErrorAs(t, err, &tooLarge)
		require.Equal(t, 64, tooLarge.MaxBytes)
		require.Equal(t, int32(1), body.closeCount.Load())
	})

	t.Run("aggregate limit", func(t *testing.T) {
		body := &nativeGeminiTrackingReadCloser{Reader: strings.NewReader(`data: {"value":"` + strings.Repeat("x", 128) + `"}` + "\n\n")}
		svc := &GeminiMessagesCompatService{cfg: &config.Config{Gateway: config.GatewayConfig{
			MaxLineSize: 1024, UpstreamResponseReadMaxBytes: 64,
		}}}

		_, _, _, err := svc.collectGeminiSSEWithRaw(body, false)

		require.ErrorIs(t, err, ErrUpstreamResponseBodyTooLarge)
		require.Equal(t, int32(1), body.closeCount.Load())
	})
}

func TestGeminiHandleNativeStreamingResponse_RequiresCompletedRequestPipeline(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-client:streamGenerateContent?alt=sse", nil)
	pipeline, err := newGoogleIdentityPipeline(&Account{ID: 204, Platform: PlatformGemini}, "gemini-client", "gemini-upstream")
	require.NoError(t, err)
	body := &nativeGeminiTrackingReadCloser{Reader: strings.NewReader(`data: {"candidates":[]}` + "\n\n")}
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: body}
	svc := &GeminiMessagesCompatService{cfg: &config.Config{}}

	result, err := svc.handleNativeStreamingResponse(c, resp, pipeline, time.Now(), false)

	require.Nil(t, result)
	require.ErrorContains(t, err, "request conversion has not completed")
	require.False(t, c.Writer.Written())
	require.Equal(t, int32(1), body.closeCount.Load())
}

func TestGeminiMessagesCompatServiceForwardNative_StreamUsesStructuredIdentityPipeline(t *testing.T) {
	upstreamBody := strings.Join([]string{
		": keepalive",
		"",
		"event: message",
		"id: event-1",
		`data: {"responseId":"native-stream",`,
		`data: "candidates":[{"content":{"role":"model","parts":[{"text":"hello"}]},"finishReason":"STOP"}],`,
		`data: "usageMetadata":{"promptTokenCount":7,"candidatesTokenCount":3},`,
		`data: "vendorExtension":{"kept":true}}`,
		"",
	}, "\n")
	upstreamBodyReader := &nativeGeminiTrackingReadCloser{Reader: strings.NewReader(upstreamBody)}
	upstream := &geminiCompatHTTPUpstreamStub{response: &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type":             []string{"text/event-stream; charset=utf-8"},
			"X-Request-Id":             []string{"rid-native-stream"},
			"X-RateLimit-Limit-Tokens": []string{"80"},
			"X-Internal-Upstream":      []string{"drop-me"},
		},
		Body: upstreamBodyReader,
	}}
	svc := &GeminiMessagesCompatService{httpUpstream: upstream, cfg: &config.Config{}}
	account := &Account{ID: 205, Platform: PlatformGemini, Type: AccountTypeAPIKey, Credentials: map[string]any{
		"api_key": "test-key", "model_mapping": map[string]any{"gemini-client": "gemini-upstream"},
	}}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	body := []byte(`{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-client:streamGenerateContent?alt=sse", bytes.NewReader(body))

	result, err := svc.ForwardNative(context.Background(), c, account, "gemini-client", "streamGenerateContent", true, body)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.Stream)
	require.False(t, result.ClientDisconnect)
	require.NotNil(t, result.FirstTokenMs)
	require.Equal(t, 7, result.Usage.InputTokens)
	require.Equal(t, 3, result.Usage.OutputTokens)
	require.Equal(t, int32(1), upstreamBodyReader.closeCount.Load())
	require.Contains(t, upstream.lastReq.URL.String(), "/models/gemini-upstream:streamGenerateContent?alt=sse")
	require.Equal(t, "text/event-stream", recorder.Header().Get("Content-Type"))
	require.Equal(t, "rid-native-stream", recorder.Header().Get("X-Request-Id"))
	require.Equal(t, "80", recorder.Header().Get("X-RateLimit-Limit-Tokens"))
	require.Empty(t, recorder.Header().Get("X-Internal-Upstream"))
	require.NotContains(t, recorder.Body.String(), ": keepalive")
	require.NotContains(t, recorder.Body.String(), "event: message")
	require.NotContains(t, recorder.Body.String(), "data: [DONE]")

	parser := protocoltransport.NewSSEParser(io.NopCloser(bytes.NewReader(recorder.Body.Bytes())), 0)
	t.Cleanup(func() { require.NoError(t, parser.Close()) })
	record, err := parser.Next(context.Background())
	require.NoError(t, err)
	require.JSONEq(t, `{"responseId":"native-stream","candidates":[{"content":{"role":"model","parts":[{"text":"hello"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":7,"candidatesTokenCount":3},"vendorExtension":{"kept":true}}`, string(record.Data))
	_, err = parser.Next(context.Background())
	require.ErrorIs(t, err, io.EOF)
}

func TestGeminiMessagesCompatServiceForwardNative_OAuthStreamUnwrapsEnvelope(t *testing.T) {
	inner := `{"responseId":"native-oauth-stream","candidates":[{"content":{"role":"model","parts":[{"text":"wrapped"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":5,"candidatesTokenCount":2},"vendorExtension":{"kept":true}}`
	upstreamBody := "data: {\"response\":" + inner + ",\"trace\":\"vendor-only\"}\n\ndata: [DONE]\n\n"
	upstream := &geminiCompatHTTPUpstreamStub{response: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}, "X-Request-Id": []string{"rid-oauth-stream"}},
		Body:       io.NopCloser(strings.NewReader(upstreamBody)),
	}}
	svc := &GeminiMessagesCompatService{tokenProvider: &GeminiTokenProvider{}, httpUpstream: upstream, cfg: &config.Config{}}
	account := &Account{ID: 206, Platform: PlatformGemini, Type: AccountTypeOAuth, Credentials: map[string]any{
		"access_token": "test-token", "project_id": "project-1",
	}}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	body := []byte(`{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-client:streamGenerateContent?alt=sse", bytes.NewReader(body))

	result, err := svc.ForwardNative(context.Background(), c, account, "gemini-client", "streamGenerateContent", true, body)

	require.NoError(t, err)
	require.Equal(t, 5, result.Usage.InputTokens)
	require.Equal(t, 2, result.Usage.OutputTokens)
	require.Contains(t, upstream.lastReq.URL.String(), "/v1internal:streamGenerateContent?alt=sse")
	parser := protocoltransport.NewSSEParser(io.NopCloser(bytes.NewReader(recorder.Body.Bytes())), 0)
	t.Cleanup(func() { require.NoError(t, parser.Close()) })
	record, err := parser.Next(context.Background())
	require.NoError(t, err)
	require.JSONEq(t, inner, string(record.Data))
	require.NotContains(t, recorder.Body.String(), `"response":`)
	require.NotContains(t, recorder.Body.String(), "vendor-only")
	_, err = parser.Next(context.Background())
	require.ErrorIs(t, err, io.EOF)
}

func TestGeminiMessagesCompatServiceForwardNative_OAuthAIStudioStreamDoesNotUnwrapDirectPayload(t *testing.T) {
	payload := `{"responseId":"oauth-ai-studio","response":{"vendorField":"ordinary-google-field"},"candidates":[{"content":{"parts":[{"text":"direct"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":3,"candidatesTokenCount":1}}`
	upstream := &geminiCompatHTTPUpstreamStub{response: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader("data: " + payload + "\n\n")),
	}}
	svc := &GeminiMessagesCompatService{tokenProvider: &GeminiTokenProvider{}, httpUpstream: upstream, cfg: &config.Config{}}
	account := &Account{ID: 211, Platform: PlatformGemini, Type: AccountTypeOAuth, Credentials: map[string]any{"access_token": "test-token"}}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	body := []byte(`{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-client:streamGenerateContent?alt=sse", bytes.NewReader(body))

	result, err := svc.ForwardNative(context.Background(), c, account, "gemini-client", "streamGenerateContent", true, body)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Contains(t, upstream.lastReq.URL.String(), "/v1beta/models/gemini-client:streamGenerateContent?alt=sse")
	parser := protocoltransport.NewSSEParser(io.NopCloser(bytes.NewReader(recorder.Body.Bytes())), 0)
	t.Cleanup(func() { require.NoError(t, parser.Close()) })
	record, err := parser.Next(context.Background())
	require.NoError(t, err)
	require.JSONEq(t, payload, string(record.Data))
}

func TestGeminiMessagesCompatServiceForwardNative_StreamEOFWithoutDoneRemainsCompatible(t *testing.T) {
	upstream := &geminiCompatHTTPUpstreamStub{response: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body: io.NopCloser(strings.NewReader(
			`data: {"candidates":[{"content":{"parts":[{"text":"ok"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":2,"candidatesTokenCount":1}}` + "\n\n",
		)),
	}}
	svc := &GeminiMessagesCompatService{httpUpstream: upstream, cfg: &config.Config{}}
	account := &Account{ID: 207, Platform: PlatformGemini, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "test-key"}}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	body := []byte(`{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-client:streamGenerateContent?alt=sse", bytes.NewReader(body))

	result, err := svc.ForwardNative(context.Background(), c, account, "gemini-client", "streamGenerateContent", true, body)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, 2, result.Usage.InputTokens)
	require.Equal(t, 1, result.Usage.OutputTokens)
	require.Contains(t, recorder.Body.String(), `"text":"ok"`)
	require.NotContains(t, recorder.Body.String(), "[DONE]")
}

func TestGeminiMessagesCompatServiceForwardNative_MalformedFirstStreamRecordFailsOverBeforeCommit(t *testing.T) {
	upstream := &geminiCompatHTTPUpstreamStub{response: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}, "X-Request-Id": []string{"rid-malformed-stream"}},
		Body:       io.NopCloser(strings.NewReader("data: {not-json}\n\n")),
	}}
	svc := &GeminiMessagesCompatService{httpUpstream: upstream, cfg: &config.Config{}}
	account := &Account{ID: 208, Platform: PlatformGemini, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "test-key"}}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	body := []byte(`{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-client:streamGenerateContent?alt=sse", bytes.NewReader(body))

	result, err := svc.ForwardNative(context.Background(), c, account, "gemini-client", "streamGenerateContent", true, body)

	require.Nil(t, result)
	var failoverErr *UpstreamFailoverError
	require.True(t, errors.As(err, &failoverErr))
	require.Equal(t, http.StatusBadGateway, failoverErr.StatusCode)
	require.Equal(t, "rid-malformed-stream", failoverErr.ResponseHeaders.Get("X-Request-Id"))
	require.True(t, failoverErr.RetryableOnSameAccount)
	require.False(t, c.Writer.Written())
	require.Empty(t, recorder.Body.String())
}

func TestGeminiMessagesCompatServiceForwardNative_MalformedStreamRecordAfterOutputDoesNotFailOver(t *testing.T) {
	upstreamBody := strings.Join([]string{
		`data: {"responseId":"native-partial","candidates":[{"content":{"parts":[{"text":"hello"}]}}],"usageMetadata":{"promptTokenCount":4,"candidatesTokenCount":1}}`,
		"",
		"data: {not-json}",
		"",
	}, "\n")
	upstream := &geminiCompatHTTPUpstreamStub{response: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(upstreamBody)),
	}}
	svc := &GeminiMessagesCompatService{httpUpstream: upstream, cfg: &config.Config{}}
	account := &Account{ID: 212, Platform: PlatformGemini, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "test-key"}}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	body := []byte(`{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-client:streamGenerateContent?alt=sse", bytes.NewReader(body))

	result, err := svc.ForwardNative(context.Background(), c, account, "gemini-client", "streamGenerateContent", true, body)

	require.Nil(t, result)
	require.ErrorContains(t, err, "read native Google stream")
	var failoverErr *UpstreamFailoverError
	require.False(t, errors.As(err, &failoverErr))
	require.True(t, c.Writer.Written())
	require.Contains(t, recorder.Body.String(), `"text":"hello"`)
}

func TestGeminiMessagesCompatServiceForwardNative_OversizedFirstStreamRecordFailsOverBeforeCommit(t *testing.T) {
	upstream := &geminiCompatHTTPUpstreamStub{response: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(`data: {"value":"` + strings.Repeat("x", 128) + `"}` + "\n\n")),
	}}
	svc := &GeminiMessagesCompatService{httpUpstream: upstream, cfg: &config.Config{Gateway: config.GatewayConfig{MaxLineSize: 64}}}
	account := &Account{ID: 209, Platform: PlatformGemini, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "test-key"}}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	body := []byte(`{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-client:streamGenerateContent?alt=sse", bytes.NewReader(body))

	result, err := svc.ForwardNative(context.Background(), c, account, "gemini-client", "streamGenerateContent", true, body)

	require.Nil(t, result)
	var failoverErr *UpstreamFailoverError
	require.True(t, errors.As(err, &failoverErr))
	require.True(t, failoverErr.RetryableOnSameAccount)
	require.False(t, c.Writer.Written())
}

func TestGeminiMessagesCompatServiceForwardNative_ClientDisconnectDrainsTerminalUsage(t *testing.T) {
	upstreamBody := strings.Join([]string{
		`data: {"responseId":"native-disconnect","candidates":[{"content":{"parts":[{"text":"hello"}]}}]}`,
		"",
		`data: {"responseId":"native-disconnect","candidates":[{"content":{"parts":[]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":9,"candidatesTokenCount":4}}`,
		"",
		"data: [DONE]",
		"",
	}, "\n")
	upstream := &geminiCompatHTTPUpstreamStub{response: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(upstreamBody)),
	}}
	svc := &GeminiMessagesCompatService{httpUpstream: upstream, cfg: &config.Config{}}
	account := &Account{ID: 210, Platform: PlatformGemini, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "test-key"}}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Writer = &failWriteResponseWriter{ResponseWriter: c.Writer}
	body := []byte(`{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-client:streamGenerateContent?alt=sse", bytes.NewReader(body))

	result, err := svc.ForwardNative(context.Background(), c, account, "gemini-client", "streamGenerateContent", true, body)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.ClientDisconnect)
	require.Equal(t, 9, result.Usage.InputTokens)
	require.Equal(t, 4, result.Usage.OutputTokens)
}

func TestGeminiMessagesCompatServiceForward_PreservesRequestedModelAndMappedUpstreamModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	httpStub := &geminiCompatHTTPUpstreamStub{
		response: &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"x-request-id": []string{"gemini-req-1"}},
			Body:       io.NopCloser(strings.NewReader(`{"candidates":[{"content":{"parts":[{"text":"hello"}]}}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":5}}`)),
		},
	}
	svc := &GeminiMessagesCompatService{httpUpstream: httpStub, cfg: &config.Config{}}
	account := &Account{
		ID:   1,
		Type: AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key": "test-key",
			"model_mapping": map[string]any{
				"claude-sonnet-4": "claude-sonnet-4-20250514",
			},
		},
	}
	body := []byte(`{"model":"claude-sonnet-4","max_tokens":16,"messages":[{"role":"user","content":"hello"}]}`)

	result, err := svc.Forward(context.Background(), c, account, body)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "claude-sonnet-4", result.Model)
	require.Equal(t, "claude-sonnet-4-20250514", result.UpstreamModel)
	require.Equal(t, 1, httpStub.calls)
	require.NotNil(t, httpStub.lastReq)
	require.Contains(t, httpStub.lastReq.URL.String(), "/models/claude-sonnet-4-20250514:")
	require.Equal(t, "claude-sonnet-4", gjson.GetBytes(w.Body.Bytes(), "model").String())
	require.Equal(t, "message", gjson.GetBytes(w.Body.Bytes(), "type").String())
	require.Regexp(t, `^msg_01[0-9A-Za-z]{22}$`, gjson.GetBytes(w.Body.Bytes(), "id").String())
}

func TestGeminiMessagesCompatServiceForward_StreamUsesPipelineAndAnthropicRenderer(t *testing.T) {
	upstreamBody := `data: {"responseId":"google-msg","modelVersion":"gemini-upstream","candidates":[{"content":{"parts":[{"functionCall":{"name":"lookup","args":{"q":"x"}}}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":3,"candidatesTokenCount":1,"totalTokenCount":4}}` + "\n\n" +
		"data: [DONE]\n\n"
	upstream := &geminiCompatHTTPUpstreamStub{response: &http.Response{
		StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}},
		Body: io.NopCloser(strings.NewReader(upstreamBody)),
	}}
	svc := &GeminiMessagesCompatService{httpUpstream: upstream, cfg: &config.Config{}}
	account := &Account{ID: 2, Platform: PlatformGemini, Type: AccountTypeAPIKey, Credentials: map[string]any{
		"api_key": "test-key", "model_mapping": map[string]any{"claude-client": "gemini-upstream"},
	}}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	body := []byte(`{"model":"claude-client","stream":true,"max_tokens":16,"messages":[{"role":"user","content":"lookup"}],"tools":[{"name":"lookup","input_schema":{"type":"object"}}]}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))

	result, err := svc.Forward(context.Background(), c, account, body)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "gemini-upstream", result.UpstreamModel)
	out := rec.Body.String()
	require.Contains(t, out, `event: message_start`)
	require.Regexp(t, `"id":"msg_01[0-9A-Za-z]{22}"`, out)
	require.Contains(t, out, `"model":"claude-client"`)
	require.Contains(t, out, `"id":"call_google_0_0"`)
	require.Contains(t, out, `"stop_reason":"tool_use"`)
	require.Contains(t, out, `event: message_stop`)
	require.NotContains(t, out, "data: [DONE]")
}

func TestGeminiMessagesCompatServiceForward_ClientDisconnectDrainsTerminalUsage(t *testing.T) {
	upstreamBody := strings.Join([]string{
		`data: {"responseId":"google-msg","candidates":[{"content":{"parts":[{"text":"hello"}]}}]}`,
		"",
		`data: {"responseId":"google-msg","candidates":[{"content":{"parts":[]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":8,"candidatesTokenCount":2,"totalTokenCount":10}}`,
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
	body := []byte(`{"model":"claude-client","stream":true,"max_tokens":16,"messages":[{"role":"user","content":"hello"}]}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))

	result, err := svc.Forward(context.Background(), c, &Account{
		ID: 3, Platform: PlatformGemini, Type: AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "test-key", "model_mapping": map[string]any{"claude-client": "gemini-upstream"}},
	}, body)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.ClientDisconnect)
	require.Equal(t, 8, result.Usage.InputTokens)
	require.Equal(t, 2, result.Usage.OutputTokens)
}

func TestGeminiMessagesCompatServiceForward_NormalizesWebSearchToolForAIStudio(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	httpStub := &geminiCompatHTTPUpstreamStub{
		response: &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"x-request-id": []string{"gemini-req-2"}},
			Body:       io.NopCloser(strings.NewReader(`{"candidates":[{"content":{"parts":[{"text":"hello"}]}}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":5}}`)),
		},
	}
	svc := &GeminiMessagesCompatService{httpUpstream: httpStub, cfg: &config.Config{}}
	account := &Account{
		ID:   1,
		Type: AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key": "test-key",
		},
	}
	body := []byte(`{"model":"claude-sonnet-4","max_tokens":16,"messages":[{"role":"user","content":"hello"}],"tools":[{"name":"get_weather","description":"Get weather info","input_schema":{"type":"object"}},{"type":"web_search_20250305","name":"web_search"}]}`)

	result, err := svc.Forward(context.Background(), c, account, body)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, httpStub.lastReq)

	postedBody, err := io.ReadAll(httpStub.lastReq.Body)
	require.NoError(t, err)

	var posted map[string]any
	require.NoError(t, json.Unmarshal(postedBody, &posted))
	tools, ok := posted["tools"].([]any)
	require.True(t, ok)
	require.Len(t, tools, 2)

	searchTool, ok := tools[1].(map[string]any)
	require.True(t, ok)
	_, hasSnake := searchTool["google_search"]
	_, hasCamel := searchTool["googleSearch"]
	require.True(t, hasSnake)
	require.False(t, hasCamel)
	_, hasFuncDecl := searchTool["functionDeclarations"]
	require.False(t, hasFuncDecl)
}

func TestConvertClaudeMessagesToGeminiGenerateContent_AddsThoughtSignatureForToolUse(t *testing.T) {
	claudeReq := map[string]any{
		"model":      "claude-haiku-4-5-20251001",
		"max_tokens": 10,
		"messages": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{"type": "text", "text": "hi"},
				},
			},
			map[string]any{
				"role": "assistant",
				"content": []any{
					map[string]any{"type": "text", "text": "ok"},
					map[string]any{
						"type":  "tool_use",
						"id":    "toolu_123",
						"name":  "default_api:write_file",
						"input": map[string]any{"path": "a.txt", "content": "x"},
						// no signature on purpose
					},
				},
			},
		},
		"tools": []any{
			map[string]any{
				"name":        "default_api:write_file",
				"description": "write file",
				"input_schema": map[string]any{
					"type":       "object",
					"properties": map[string]any{"path": map[string]any{"type": "string"}},
				},
			},
		},
	}
	b, _ := json.Marshal(claudeReq)

	out, err := convertClaudeMessagesToGeminiGenerateContent(b)
	if err != nil {
		t.Fatalf("convert failed: %v", err)
	}
	s := string(out)
	if !strings.Contains(s, "\"functionCall\"") {
		t.Fatalf("expected functionCall in output, got: %s", s)
	}
	if !strings.Contains(s, "\"thoughtSignature\":\""+geminiDummyThoughtSignature+"\"") {
		t.Fatalf("expected injected thoughtSignature %q, got: %s", geminiDummyThoughtSignature, s)
	}
}

func TestEnsureGeminiFunctionCallThoughtSignatures_InsertsWhenMissing(t *testing.T) {
	geminiReq := map[string]any{
		"contents": []any{
			map[string]any{
				"role": "user",
				"parts": []any{
					map[string]any{
						"functionCall": map[string]any{
							"name": "default_api:write_file",
							"args": map[string]any{"path": "a.txt"},
						},
					},
				},
			},
		},
	}
	b, _ := json.Marshal(geminiReq)
	out := ensureGeminiFunctionCallThoughtSignatures(b)
	s := string(out)
	if !strings.Contains(s, "\"thoughtSignature\":\""+geminiDummyThoughtSignature+"\"") {
		t.Fatalf("expected injected thoughtSignature %q, got: %s", geminiDummyThoughtSignature, s)
	}
}

// TestUnwrapGeminiResponse 测试 unwrapGeminiResponse 的各种输入场景
// 关键区别：只有 response 为 JSON 对象/数组时才解包
func TestUnwrapGeminiResponse(t *testing.T) {
	// 构造 >50KB 的大型 JSON 对象
	largePadding := strings.Repeat("x", 50*1024)
	largeInput := []byte(fmt.Sprintf(`{"response":{"id":"big","pad":"%s"}}`, largePadding))
	largeExpected := fmt.Sprintf(`{"id":"big","pad":"%s"}`, largePadding)

	tests := []struct {
		name     string
		input    []byte
		expected string
		wantErr  bool
	}{
		{
			name:     "正常 response 包装（JSON 对象）",
			input:    []byte(`{"response":{"key":"val"}}`),
			expected: `{"key":"val"}`,
		},
		{
			name:     "无包装直接返回",
			input:    []byte(`{"key":"val"}`),
			expected: `{"key":"val"}`,
		},
		{
			name:     "空 JSON",
			input:    []byte(`{}`),
			expected: `{}`,
		},
		{
			name:     "null response 返回原始 body",
			input:    []byte(`{"response":null}`),
			expected: `{"response":null}`,
		},
		{
			name:     "非法 JSON 返回原始 body",
			input:    []byte(`not json`),
			expected: `not json`,
		},
		{
			name:     "response 为基础类型 string 返回原始 body",
			input:    []byte(`{"response":"hello"}`),
			expected: `{"response":"hello"}`,
		},
		{
			name:     "嵌套 response 只解一层",
			input:    []byte(`{"response":{"response":{"inner":true}}}`),
			expected: `{"response":{"inner":true}}`,
		},
		{
			name:     "大型 JSON >50KB",
			input:    largeInput,
			expected: largeExpected,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := unwrapGeminiResponse(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.expected, strings.TrimSpace(string(got)))
		})
	}
}

// ---------------------------------------------------------------------------
// Task 8.1 — extractGeminiUsage 测试
// ---------------------------------------------------------------------------

func TestExtractGeminiUsage(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantNil   bool
		wantUsage *ClaudeUsage
	}{
		{
			name:    "完整 usageMetadata",
			input:   `{"usageMetadata":{"promptTokenCount":100,"candidatesTokenCount":50,"cachedContentTokenCount":20}}`,
			wantNil: false,
			wantUsage: &ClaudeUsage{
				InputTokens:          80,
				OutputTokens:         50,
				CacheReadInputTokens: 20,
			},
		},
		{
			name:    "包含 thoughtsTokenCount",
			input:   `{"usageMetadata":{"promptTokenCount":100,"candidatesTokenCount":20,"thoughtsTokenCount":50}}`,
			wantNil: false,
			wantUsage: &ClaudeUsage{
				InputTokens:          100,
				OutputTokens:         70,
				CacheReadInputTokens: 0,
			},
		},
		{
			name:    "包含 thoughtsTokenCount 与缓存",
			input:   `{"usageMetadata":{"promptTokenCount":100,"candidatesTokenCount":20,"cachedContentTokenCount":30,"thoughtsTokenCount":50}}`,
			wantNil: false,
			wantUsage: &ClaudeUsage{
				InputTokens:          70,
				OutputTokens:         70,
				CacheReadInputTokens: 30,
			},
		},
		{
			name:    "缺失 cachedContentTokenCount",
			input:   `{"usageMetadata":{"promptTokenCount":100,"candidatesTokenCount":50}}`,
			wantNil: false,
			wantUsage: &ClaudeUsage{
				InputTokens:          100,
				OutputTokens:         50,
				CacheReadInputTokens: 0,
			},
		},
		{
			name:    "无 usageMetadata",
			input:   `{"candidates":[]}`,
			wantNil: true,
		},
		{
			// gjson 对 null 返回 Exists()=true，因此函数不会返回 nil，
			// 而是返回全零的 ClaudeUsage。
			name:    "null usageMetadata — gjson Exists 为 true",
			input:   `{"usageMetadata":null}`,
			wantNil: false,
			wantUsage: &ClaudeUsage{
				InputTokens:          0,
				OutputTokens:         0,
				CacheReadInputTokens: 0,
			},
		},
		{
			name:    "零值字段",
			input:   `{"usageMetadata":{"promptTokenCount":0,"candidatesTokenCount":0,"cachedContentTokenCount":0}}`,
			wantNil: false,
			wantUsage: &ClaudeUsage{
				InputTokens:          0,
				OutputTokens:         0,
				CacheReadInputTokens: 0,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractGeminiUsage([]byte(tt.input))
			if tt.wantNil {
				if got != nil {
					t.Fatalf("期望返回 nil，实际返回 %+v", got)
				}
				return
			}
			if got == nil {
				t.Fatalf("期望返回非 nil，实际返回 nil")
			}
			if got.InputTokens != tt.wantUsage.InputTokens {
				t.Errorf("InputTokens: 期望 %d，实际 %d", tt.wantUsage.InputTokens, got.InputTokens)
			}
			if got.OutputTokens != tt.wantUsage.OutputTokens {
				t.Errorf("OutputTokens: 期望 %d，实际 %d", tt.wantUsage.OutputTokens, got.OutputTokens)
			}
			if got.CacheReadInputTokens != tt.wantUsage.CacheReadInputTokens {
				t.Errorf("CacheReadInputTokens: 期望 %d，实际 %d", tt.wantUsage.CacheReadInputTokens, got.CacheReadInputTokens)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Task 8.2 — estimateGeminiCountTokens 测试
// ---------------------------------------------------------------------------

func TestEstimateGeminiCountTokens(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantGt0   bool // 期望结果 > 0
		wantExact *int // 如果非 nil，期望精确匹配
	}{
		{
			name: "含 systemInstruction 和 contents",
			input: `{
				"systemInstruction":{"parts":[{"text":"You are a helpful assistant."}]},
				"contents":[{"parts":[{"text":"Hello, how are you?"}]}]
			}`,
			wantGt0: true,
		},
		{
			name: "仅 contents，无 systemInstruction",
			input: `{
				"contents":[{"parts":[{"text":"Hello, how are you?"}]}]
			}`,
			wantGt0: true,
		},
		{
			name:      "空 parts",
			input:     `{"contents":[{"parts":[]}]}`,
			wantGt0:   false,
			wantExact: intPtr(0),
		},
		{
			name:      "非文本 parts（inlineData）",
			input:     `{"contents":[{"parts":[{"inlineData":{"mimeType":"image/png"}}]}]}`,
			wantGt0:   false,
			wantExact: intPtr(0),
		},
		{
			name:      "空白文本",
			input:     `{"contents":[{"parts":[{"text":"   "}]}]}`,
			wantGt0:   false,
			wantExact: intPtr(0),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := estimateGeminiCountTokens([]byte(tt.input))
			if tt.wantExact != nil {
				if got != *tt.wantExact {
					t.Errorf("期望精确值 %d，实际 %d", *tt.wantExact, got)
				}
				return
			}
			if tt.wantGt0 && got <= 0 {
				t.Errorf("期望返回 > 0，实际 %d", got)
			}
			if !tt.wantGt0 && got != 0 {
				t.Errorf("期望返回 0，实际 %d", got)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Task 8.3 — ParseGeminiRateLimitResetTime 测试
// ---------------------------------------------------------------------------

func TestParseGeminiRateLimitResetTime(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantNil     bool
		approxDelta int64 // 预期的 (返回值 - now) 大约是多少秒
	}{
		{
			name:        "正常 quotaResetDelay",
			input:       `{"error":{"details":[{"metadata":{"quotaResetDelay":"12.345s"}}]}}`,
			wantNil:     false,
			approxDelta: 13, // 向上取整 12.345 -> 13
		},
		{
			name:        "daily quota",
			input:       `{"error":{"message":"quota per day exceeded"}}`,
			wantNil:     false,
			approxDelta: -1, // 不检查精确 delta，仅检查非 nil
		},
		{
			name:    "无 details 且无 regex 匹配",
			input:   `{"error":{"message":"rate limit"}}`,
			wantNil: true,
		},
		{
			name:        "regex 回退匹配",
			input:       `Please retry in 30s`,
			wantNil:     false,
			approxDelta: 30,
		},
		{
			name:    "完全无匹配",
			input:   `{"error":{"code":429}}`,
			wantNil: true,
		},
		{
			name:        "非法 JSON 但 regex 回退仍工作",
			input:       `not json but Please retry in 10s`,
			wantNil:     false,
			approxDelta: 10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			now := time.Now().Unix()
			got := ParseGeminiRateLimitResetTime([]byte(tt.input))

			if tt.wantNil {
				if got != nil {
					t.Fatalf("期望返回 nil，实际返回 %d", *got)
				}
				return
			}

			if got == nil {
				t.Fatalf("期望返回非 nil，实际返回 nil")
			}

			// approxDelta == -1 表示只检查非 nil，不检查具体值（如 daily quota 场景）
			if tt.approxDelta == -1 {
				// 仅验证返回的时间戳在合理范围内（未来的某个时间）
				if *got < now {
					t.Errorf("期望返回的时间戳 >= now(%d)，实际 %d", now, *got)
				}
				return
			}

			// 使用 +/-2 秒容差进行范围检查
			delta := *got - now
			if delta < tt.approxDelta-2 || delta > tt.approxDelta+2 {
				t.Errorf("期望 delta 约为 %d 秒（+/-2），实际 delta = %d 秒（返回值=%d, now=%d）",
					tt.approxDelta, delta, *got, now)
			}
		})
	}
}

// TestGeminiMessagesHandleStreamingResponse_ClosesToolBlockBeforeText guards the
// tool→text ordering in the Gemini→Anthropic (messages) streaming bridge. When
// Gemini emits a functionCall part followed by a text part, the tool_use content
// block must be closed before the text block opens; otherwise the Anthropic SSE
// stream contains overlapping content blocks. The chat-completions sibling
// already enforces this via closeOpenTool().
func TestGeminiMessagesHandleStreamingResponse_ClosesToolBlockBeforeText(t *testing.T) {
	gin.SetMode(gin.TestMode)

	upstreamBody := `data: {"candidates":[{"content":{"parts":[{"functionCall":{"name":"get_weather","args":{"city":"SF"}}}]}}]}` + "\n\n" +
		`data: {"candidates":[{"content":{"parts":[{"text":"All done."}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":5,"candidatesTokenCount":3}}` + "\n\n" +
		"data: [DONE]\n\n"

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(upstreamBody)),
	}

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)

	svc := &GeminiMessagesCompatService{}
	pipeline, _, err := newClaudeMessagesGooglePipeline(nil, []byte(`{"model":"claude-3-5-sonnet","max_tokens":16,"messages":[{"role":"user","content":"hi"}]}`), "claude-3-5-sonnet", "gemini-test")
	require.NoError(t, err)
	result, err := svc.handleStreamingResponse(c, resp, pipeline, time.Now())
	require.NoError(t, err)
	require.NotNil(t, result)

	events := parseAnthropicContentBlockEvents(t, rec.Body.String())

	// Anthropic allows at most one content block open at a time: every
	// content_block_start must be matched by a content_block_stop before the
	// next start. Replay the lifecycle and assert there is no overlap.
	open := -1
	blockTypes := map[int]string{}
	textStarted := false
	toolClosed := false
	toolClosedBeforeText := false
	for _, ev := range events {
		switch ev.event {
		case "content_block_start":
			require.Equalf(t, -1, open,
				"content block %d opened while block %d was still open (overlapping blocks)", ev.index, open)
			open = ev.index
			blockTypes[ev.index] = ev.blockType
			if ev.blockType == "text" {
				textStarted = true
				if toolClosed {
					toolClosedBeforeText = true
				}
			}
		case "content_block_stop":
			require.Equalf(t, open, ev.index,
				"content_block_stop index %d does not match the open block %d", ev.index, open)
			if blockTypes[ev.index] == "tool_use" {
				toolClosed = true
			}
			open = -1
		}
	}

	require.True(t, textStarted, "expected a text content block to be emitted after the tool call")
	require.True(t, toolClosedBeforeText, "tool_use block must be closed before the text block starts")
	require.Equal(t, -1, open, "stream ended with a content block still open")
}

type anthropicContentBlockEvent struct {
	event     string
	index     int
	blockType string
}

// parseAnthropicContentBlockEvents extracts content_block_start/stop events (with
// their index and, for starts, the content block type) from an Anthropic SSE body.
func parseAnthropicContentBlockEvents(t *testing.T, raw string) []anthropicContentBlockEvent {
	t.Helper()
	var events []anthropicContentBlockEvent
	for _, chunk := range strings.Split(raw, "\n\n") {
		var eventName, dataLine string
		for _, line := range strings.Split(chunk, "\n") {
			switch {
			case strings.HasPrefix(line, "event:"):
				eventName = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			case strings.HasPrefix(line, "data:"):
				dataLine = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			}
		}
		if eventName != "content_block_start" && eventName != "content_block_stop" {
			continue
		}
		var payload struct {
			Index        int `json:"index"`
			ContentBlock struct {
				Type string `json:"type"`
			} `json:"content_block"`
		}
		require.NoError(t, json.Unmarshal([]byte(dataLine), &payload))
		events = append(events, anthropicContentBlockEvent{
			event:     eventName,
			index:     payload.Index,
			blockType: payload.ContentBlock.Type,
		})
	}
	return events
}

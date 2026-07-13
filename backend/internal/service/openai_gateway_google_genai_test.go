package service

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestForwardGoogleGenAIDoesNotReplaceGinResponseWriter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	originalWriter := c.Writer
	body := []byte(`{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/client-google-model:generateContent", bytes.NewReader(body))
	upstream := &responseWriterInspectingUpstream{
		context: c, expected: originalWriter,
		response: &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(`{
				"id":"resp_writer","model":"gpt-5.4","status":"completed",
				"output":[{"type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"ok"}]}],
				"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}
			}`)),
		},
	}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}

	_, err := svc.ForwardGoogleGenAI(context.Background(), c, googleIngressOpenAIAccount(), "client-google-model", "client-google-model", false, body)
	require.NoError(t, err)
	require.True(t, upstream.writerUnchanged)
	require.Same(t, originalWriter, c.Writer)
}

type responseWriterInspectingUpstream struct {
	HTTPUpstream
	context         *gin.Context
	expected        gin.ResponseWriter
	response        *http.Response
	writerUnchanged bool
}

func (u *responseWriterInspectingUpstream) Do(_ *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	u.writerUnchanged = u.context != nil && u.context.Writer == u.expected
	return u.response, nil
}

func (u *responseWriterInspectingUpstream) DoWithTLS(_ *http.Request, _ string, _ int64, _ int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	u.writerUnchanged = u.context != nil && u.context.Writer == u.expected
	return u.response, nil
}

func TestForwardGoogleGenAINonStreamingUsesResponsesPipeline(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{"application/json"},
			"x-request-id": []string{"req_google_nonstream"},
		},
		Body: io.NopCloser(strings.NewReader(`{
			"id":"resp_google_nonstream","model":"gpt-5.4","status":"completed",
			"output":[{"type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"hello"}]}],
			"usage":{"input_tokens":7,"output_tokens":3,"total_tokens":10,"input_tokens_details":{"cached_tokens":2}}
		}`)),
	}}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	account := googleIngressOpenAIAccount()
	body := []byte(`{"contents":[{"role":"user","parts":[{"text":"hi"}]}],"generationConfig":{"maxOutputTokens":32}}`)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/client-google-model:generateContent", bytes.NewReader(body))

	result, err := svc.ForwardGoogleGenAI(context.Background(), c, account, "client-google-model", "client-google-model", false, body)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "client-google-model", result.Model)
	require.Equal(t, "gpt-5.4", result.UpstreamModel)
	require.Equal(t, 7, result.Usage.InputTokens)
	require.Equal(t, 3, result.Usage.OutputTokens)
	require.Equal(t, 2, result.Usage.CacheReadInputTokens)

	require.Equal(t, "gpt-5.4", gjson.GetBytes(upstream.lastBody, "model").String())
	require.True(t, gjson.GetBytes(upstream.lastBody, "input").IsArray())
	require.False(t, gjson.GetBytes(upstream.lastBody, "contents").Exists())
	require.False(t, gjson.GetBytes(upstream.lastBody, "stream").Bool())

	responseBody := recorder.Body.Bytes()
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "application/json", recorder.Header().Get("Content-Type"))
	require.Equal(t, "resp_google_nonstream", gjson.GetBytes(responseBody, "responseId").String())
	require.Equal(t, "client-google-model", gjson.GetBytes(responseBody, "modelVersion").String())
	require.Equal(t, "hello", gjson.GetBytes(responseBody, "candidates.0.content.parts.0.text").String())
	require.Equal(t, int64(7), gjson.GetBytes(responseBody, "usageMetadata.promptTokenCount").Int())
	require.Equal(t, int64(3), gjson.GetBytes(responseBody, "usageMetadata.candidatesTokenCount").Int())
	require.Equal(t, int64(2), gjson.GetBytes(responseBody, "usageMetadata.cachedContentTokenCount").Int())
}

func TestForwardGoogleGenAIRestoresClientModelAcrossRoutingAlias(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(strings.NewReader(`{
			"id":"resp_alias","model":"gpt-5.4","status":"completed",
			"output":[{"type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"ok"}]}],
			"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}
		}`)),
	}}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	account := googleIngressOpenAIAccount()
	account.Credentials["model_mapping"] = map[string]any{"channel-routed-model": "gpt-5.4"}
	body := []byte(`{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/client-google-model:generateContent", bytes.NewReader(body))

	result, err := svc.ForwardGoogleGenAI(context.Background(), c, account, "client-google-model", "channel-routed-model", false, body)
	require.NoError(t, err)
	require.Equal(t, "gpt-5.4", gjson.GetBytes(upstream.lastBody, "model").String())
	require.Equal(t, "client-google-model", gjson.GetBytes(recorder.Body.Bytes(), "modelVersion").String())
	require.Equal(t, "client-google-model", result.Model)
	require.Equal(t, "gpt-5.4", result.UpstreamModel)
}

func TestForwardGoogleGenAIStreamingUsesResponsesPipeline(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstreamBody := strings.Join([]string{
		`data: {"type":"response.created","sequence_number":0,"response":{"id":"resp_google_stream","model":"gpt-5.4","status":"in_progress","output":[]}}`,
		"",
		`data: {"type":"response.output_item.added","sequence_number":1,"output_index":0,"item":{"id":"msg_1","type":"message","role":"assistant","status":"in_progress","content":[]}}`,
		"",
		`data: {"type":"response.content_part.added","sequence_number":2,"item_id":"msg_1","output_index":0,"content_index":0,"part":{"type":"output_text","text":""}}`,
		"",
		`data: {"type":"response.output_text.delta","sequence_number":3,"item_id":"msg_1","output_index":0,"content_index":0,"delta":"hello"}`,
		"",
		`data: {"type":"response.output_text.done","sequence_number":4,"item_id":"msg_1","output_index":0,"content_index":0,"text":"hello"}`,
		"",
		`data: {"type":"response.content_part.done","sequence_number":5,"item_id":"msg_1","output_index":0,"content_index":0,"part":{"type":"output_text","text":"hello"}}`,
		"",
		`data: {"type":"response.output_item.done","sequence_number":6,"output_index":0,"item":{"id":"msg_1","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"hello"}]}}`,
		"",
		`data: {"type":"response.completed","sequence_number":7,"response":{"id":"resp_google_stream","model":"gpt-5.4","status":"completed","output":[{"id":"msg_1","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"hello"}]}],"usage":{"input_tokens":6,"output_tokens":2,"total_tokens":8}}}`,
		"",
		`data: [DONE]`,
		"",
	}, "\n")
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{"text/event-stream"},
			"x-request-id": []string{"req_google_stream"},
		},
		Body: io.NopCloser(strings.NewReader(upstreamBody)),
	}}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	account := googleIngressOpenAIAccount()
	body := []byte(`{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/client-google-model:streamGenerateContent?alt=sse", bytes.NewReader(body))

	result, err := svc.ForwardGoogleGenAI(context.Background(), c, account, "client-google-model", "client-google-model", true, body)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.Stream)
	require.Equal(t, 6, result.Usage.InputTokens)
	require.Equal(t, 2, result.Usage.OutputTokens)
	require.True(t, gjson.GetBytes(upstream.lastBody, "stream").Bool())

	wire := recorder.Body.String()
	require.Equal(t, "text/event-stream", recorder.Header().Get("Content-Type"))
	require.Contains(t, wire, `"responseId":"resp_google_stream"`)
	require.Contains(t, wire, `"modelVersion":"client-google-model"`)
	require.Contains(t, wire, `"text":"hello"`)
	require.Contains(t, wire, `"promptTokenCount":6`)
	require.Contains(t, wire, `"candidatesTokenCount":2`)
	require.NotContains(t, wire, "event:")
	require.NotContains(t, wire, "[DONE]")
}

func TestForwardGoogleGenAINonStreamingPassthroughUsesStructuredResponses(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(strings.NewReader(`{
			"id":"resp_passthrough","model":"gpt-5.4","status":"completed",
			"output":[{"type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"passthrough"}]}],
			"usage":{"input_tokens":3,"output_tokens":1,"total_tokens":4}
		}`)),
	}}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	account := googleIngressOpenAIAccount()
	account.Extra["openai_passthrough"] = true
	body := []byte(`{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/client-google-model:generateContent", bytes.NewReader(body))

	result, err := svc.ForwardGoogleGenAI(context.Background(), c, account, "client-google-model", "client-google-model", false, body)
	require.NoError(t, err)
	require.Equal(t, "passthrough", gjson.GetBytes(recorder.Body.Bytes(), "candidates.0.content.parts.0.text").String())
	require.Equal(t, "client-google-model", gjson.GetBytes(recorder.Body.Bytes(), "modelVersion").String())
	require.Equal(t, 3, result.Usage.InputTokens)
}

func TestForwardGoogleGenAIStreamingRawChatFallbackUsesActualProtocol(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstreamBody := strings.Join([]string{
		`data: {"id":"chatcmpl_google","object":"chat.completion.chunk","model":"gpt-5.4","choices":[{"index":0,"delta":{"role":"assistant","content":"chat"},"finish_reason":null}]}`,
		"",
		`data: {"id":"chatcmpl_google","object":"chat.completion.chunk","model":"gpt-5.4","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":4,"completion_tokens":2,"total_tokens":6}}`,
		"",
		"data: [DONE]",
		"",
	}, "\n")
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(upstreamBody)),
	}}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	account := googleIngressOpenAIAccount()
	account.Extra["openai_responses_supported"] = false
	body := []byte(`{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/client-google-model:streamGenerateContent?alt=sse", bytes.NewReader(body))

	result, err := svc.ForwardGoogleGenAI(context.Background(), c, account, "client-google-model", "client-google-model", true, body)
	require.NoError(t, err)
	wire := recorder.Body.String()
	require.Contains(t, wire, `"text":"chat"`)
	require.Contains(t, wire, `"modelVersion":"client-google-model"`)
	require.Contains(t, wire, `"promptTokenCount":4`)
	require.NotContains(t, wire, "[DONE]")
	require.Equal(t, 4, result.Usage.InputTokens)
	require.Equal(t, 2, result.Usage.OutputTokens)
}

func TestForwardGoogleGenAIPreservesPreOutputFailover(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusTooManyRequests,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"rate limited"}}`)),
	}}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	body := []byte(`{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/client-google-model:generateContent", bytes.NewReader(body))

	result, err := svc.ForwardGoogleGenAI(context.Background(), c, googleIngressOpenAIAccount(), "client-google-model", "client-google-model", false, body)
	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.Nil(t, result)
	require.Equal(t, http.StatusTooManyRequests, failoverErr.StatusCode)
	require.Equal(t, 0, recorder.Body.Len())
	require.Equal(t, http.StatusOK, recorder.Code)
}

func TestForwardGoogleGenAIRejectsInvalidRequestBeforeUpstream(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := &httpUpstreamRecorder{}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	body := []byte(`{"contents":"invalid"}`)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/client-google-model:generateContent", bytes.NewReader(body))

	result, err := svc.ForwardGoogleGenAI(context.Background(), c, googleIngressOpenAIAccount(), "client-google-model", "client-google-model", false, body)
	require.Error(t, err)
	require.Nil(t, result)
	require.Nil(t, upstream.lastReq)
	require.Equal(t, 0, recorder.Body.Len())
}

func googleIngressOpenAIAccount() *Account {
	return &Account{
		ID: 91, Name: "openai-google-ingress", Platform: PlatformOpenAI,
		Type: AccountTypeAPIKey, Concurrency: 1,
		Credentials: map[string]any{
			"api_key":       "test-openai-key",
			"model_mapping": map[string]any{"client-google-model": "gpt-5.4"},
		},
		Extra: map[string]any{"openai_responses_supported": true},
	}
}

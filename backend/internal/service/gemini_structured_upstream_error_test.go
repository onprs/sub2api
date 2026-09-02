package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type geminiErrorTrackingBody struct {
	reader     io.Reader
	closeCount atomic.Int32
}

type geminiSequenceHTTPUpstream struct {
	responses []*http.Response
	calls     int
}

func (s *geminiSequenceHTTPUpstream) Do(*http.Request, string, int64, int) (*http.Response, error) {
	if s.calls >= len(s.responses) {
		return nil, io.EOF
	}
	response := s.responses[s.calls]
	s.calls++
	return response, nil
}

func (s *geminiSequenceHTTPUpstream) DoWithTLS(req *http.Request, proxyURL string, accountID int64, accountConcurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return s.Do(req, proxyURL, accountID, accountConcurrency)
}

func (b *geminiErrorTrackingBody) Read(p []byte) (int, error) { return b.reader.Read(p) }
func (b *geminiErrorTrackingBody) Close() error {
	b.closeCount.Add(1)
	return nil
}

func newGeminiErrorTrackingBody(body string) *geminiErrorTrackingBody {
	return &geminiErrorTrackingBody{reader: strings.NewReader(body)}
}

func TestCollectGeminiStructuredUpstreamErrorOwnsBodyOnce(t *testing.T) {
	for _, streamRequested := range []bool{false, true} {
		t.Run(map[bool]string{false: "buffered", true: "stream"}[streamRequested], func(t *testing.T) {
			body := newGeminiErrorTrackingBody(`{"error":{"message":"bad request"}}`)
			resp := &http.Response{
				StatusCode: http.StatusBadRequest,
				Header: http.Header{
					"X-Goog-Request-Id": []string{"rid-google-error"},
					"X-Vendor-Detail":   []string{"preserved"},
				},
				Body: body,
			}
			svc := &GeminiMessagesCompatService{cfg: &config.Config{}}

			upstream, err := svc.collectGeminiStructuredUpstreamError(resp, streamRequested, "x-request-id")

			require.NoError(t, err)
			require.Equal(t, http.StatusBadRequest, upstream.StatusCode)
			require.Equal(t, protocolconv.ProtocolGoogleGenAI, upstream.ActualProtocol)
			require.Equal(t, "rid-google-error", upstream.RequestID)
			require.JSONEq(t, `{"error":{"message":"bad request"}}`, string(upstream.Body))
			require.Equal(t, int32(1), body.closeCount.Load())
			require.Equal(t, http.NoBody, resp.Body)

			resp.Header.Set("X-Vendor-Detail", "mutated")
			require.Equal(t, "preserved", upstream.Headers.Get("X-Vendor-Detail"))
		})
	}
}

func TestGeminiCompatibilityStreamingHTTPErrorBeforeSSECommit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const upstreamError = `{"error":{"code":400,"status":"INVALID_ARGUMENT","message":"bad Google request"}}`

	tests := []struct {
		name        string
		path        string
		body        []byte
		account     *Account
		forward     func(*GeminiMessagesCompatService, context.Context, *gin.Context, *Account, []byte) (*ForwardResult, error)
		wantType    string
		wantMessage string
	}{
		{
			name: "chat", path: "/v1/chat/completions",
			body:    []byte(`{"model":"chat-client","stream":true,"messages":[{"role":"user","content":"hello"}]}`),
			account: &Account{ID: 301, Platform: PlatformGemini, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "test-key", "model_mapping": map[string]any{"chat-client": "gemini-upstream"}}},
			forward: func(s *GeminiMessagesCompatService, ctx context.Context, c *gin.Context, account *Account, body []byte) (*ForwardResult, error) {
				return s.ForwardAsChatCompletions(ctx, c, account, body)
			},
			wantType: "invalid_request_error", wantMessage: "bad Google request",
		},
		{
			name: "responses", path: "/v1/responses",
			body:    []byte(`{"model":"responses-client","stream":true,"input":"hello"}`),
			account: &Account{ID: 302, Platform: PlatformGemini, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "test-key", "model_mapping": map[string]any{"responses-client": "gemini-upstream"}}},
			forward: func(s *GeminiMessagesCompatService, ctx context.Context, c *gin.Context, account *Account, body []byte) (*ForwardResult, error) {
				return s.ForwardAsResponses(ctx, c, account, body)
			},
			wantType: "invalid_request_error", wantMessage: "bad Google request",
		},
		{
			name: "messages", path: "/v1/messages",
			body:    []byte(`{"model":"claude-client","stream":true,"max_tokens":16,"messages":[{"role":"user","content":"hello"}]}`),
			account: &Account{ID: 303, Platform: PlatformGemini, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "test-key", "model_mapping": map[string]any{"claude-client": "gemini-upstream"}}},
			forward: func(s *GeminiMessagesCompatService, ctx context.Context, c *gin.Context, account *Account, body []byte) (*ForwardResult, error) {
				return s.Forward(ctx, c, account, body)
			},
			wantType: "invalid_request_error", wantMessage: "bad Google request",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			trackingBody := newGeminiErrorTrackingBody(upstreamError)
			upstream := &geminiCompatHTTPUpstreamStub{response: &http.Response{
				StatusCode: http.StatusBadRequest,
				Header: http.Header{
					"Content-Type":      []string{"application/json"},
					"X-Goog-Request-Id": []string{"rid-immediate-400"},
				},
				Body: trackingBody,
			}}
			svc := &GeminiMessagesCompatService{httpUpstream: upstream, cfg: &config.Config{}}
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, tt.path, strings.NewReader(string(tt.body)))

			result, err := tt.forward(svc, context.Background(), c, tt.account, tt.body)

			require.Nil(t, result)
			require.Error(t, err)
			require.Equal(t, http.StatusBadRequest, recorder.Code)
			require.NotEqual(t, "text/event-stream", recorder.Header().Get("Content-Type"))
			require.Equal(t, tt.wantType, gjson.GetBytes(recorder.Body.Bytes(), "error.type").String())
			require.Contains(t, gjson.GetBytes(recorder.Body.Bytes(), "error.message").String(), tt.wantMessage)
			require.Equal(t, int32(1), trackingBody.closeCount.Load())
			require.Equal(t, 1, upstream.calls)
		})
	}
}

func TestGeminiStructuredErrorRetryUsesSuccessfulAttempt(t *testing.T) {
	gin.SetMode(gin.TestMode)
	failedBody := newGeminiErrorTrackingBody(`{"error":{"code":500,"status":"INTERNAL","message":"first attempt"}}`)
	successBody := newGeminiErrorTrackingBody(strings.Join([]string{
		`data: {"responseId":"google-retry","modelVersion":"gemini-upstream","candidates":[{"content":{"role":"model","parts":[{"text":"retry ok"}]} ,"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":2,"candidatesTokenCount":1}}`,
		"", "data: [DONE]", "",
	}, "\n"))
	upstream := &geminiSequenceHTTPUpstream{responses: []*http.Response{
		{
			StatusCode: http.StatusInternalServerError,
			Header:     http.Header{"Content-Type": []string{"application/json"}, "X-Goog-Request-Id": []string{"rid-first"}},
			Body:       failedBody,
		},
		{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}, "X-Goog-Request-Id": []string{"rid-success"}},
			Body:       successBody,
		},
	}}
	svc := &GeminiMessagesCompatService{httpUpstream: upstream, cfg: &config.Config{}}
	account := &Account{ID: 305, Platform: PlatformGemini, Type: AccountTypeAPIKey, Credentials: map[string]any{
		"api_key": "test-key", "model_mapping": map[string]any{"chat-client": "gemini-upstream"},
	}}
	body := []byte(`{"model":"chat-client","stream":true,"messages":[{"role":"user","content":"hello"}]}`)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(string(body)))

	result, err := svc.ForwardAsChatCompletions(context.Background(), c, account, body)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "rid-success", result.RequestID)
	require.Contains(t, recorder.Body.String(), "retry ok")
	require.Equal(t, int32(1), failedBody.closeCount.Load())
	require.Equal(t, int32(1), successBody.closeCount.Load())
	require.Equal(t, 2, upstream.calls)
}

func TestGeminiStructuredErrorFailoverPreservesDetachedResult(t *testing.T) {
	gin.SetMode(gin.TestMode)
	trackingBody := newGeminiErrorTrackingBody(`{"error":{"code":401,"status":"UNAUTHENTICATED","message":"bad credential"}}`)
	headers := http.Header{
		"Content-Type":      []string{"application/json"},
		"X-Goog-Request-Id": []string{"rid-failover"},
		"X-Vendor-Detail":   []string{"preserved"},
	}
	upstream := &geminiCompatHTTPUpstreamStub{response: &http.Response{
		StatusCode: http.StatusUnauthorized, Header: headers, Body: trackingBody,
	}}
	svc := &GeminiMessagesCompatService{httpUpstream: upstream, cfg: &config.Config{}}
	account := &Account{ID: 306, Platform: PlatformGemini, Type: AccountTypeAPIKey, Credentials: map[string]any{
		"api_key": "test-key", "model_mapping": map[string]any{"chat-client": "gemini-upstream"},
	}}
	body := []byte(`{"model":"chat-client","stream":true,"messages":[{"role":"user","content":"hello"}]}`)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(string(body)))

	result, err := svc.ForwardAsChatCompletions(context.Background(), c, account, body)

	require.Nil(t, result)
	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.Equal(t, http.StatusUnauthorized, failoverErr.StatusCode)
	require.JSONEq(t, `{"error":{"code":401,"status":"UNAUTHENTICATED","message":"bad credential"}}`, string(failoverErr.ResponseBody))
	require.Equal(t, "rid-failover", failoverErr.ResponseHeaders.Get("X-Goog-Request-Id"))
	require.Equal(t, "preserved", failoverErr.ResponseHeaders.Get("X-Vendor-Detail"))
	headers.Set("X-Vendor-Detail", "mutated")
	require.Equal(t, "preserved", failoverErr.ResponseHeaders.Get("X-Vendor-Detail"))
	require.Equal(t, int32(1), trackingBody.closeCount.Load())
	require.False(t, c.Writer.Written())
}

func TestGeminiNativeStreamingHTTPErrorBeforeSSECommit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	trackingBody := newGeminiErrorTrackingBody(`{"error":{"code":400,"status":"INVALID_ARGUMENT","message":"native bad request"}}`)
	upstream := &geminiCompatHTTPUpstreamStub{response: &http.Response{
		StatusCode: http.StatusBadRequest,
		Header: http.Header{
			"Content-Type":      []string{"application/json"},
			"X-Goog-Request-Id": []string{"rid-native-400"},
		},
		Body: trackingBody,
	}}
	svc := &GeminiMessagesCompatService{httpUpstream: upstream, cfg: &config.Config{}}
	account := &Account{ID: 304, Platform: PlatformGemini, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "test-key"}}
	body := []byte(`{"contents":[{"role":"user","parts":[{"text":"hello"}]}]}`)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-client:streamGenerateContent", strings.NewReader(string(body)))

	result, err := svc.ForwardNative(context.Background(), c, account, "gemini-client", "streamGenerateContent", true, body)

	require.Nil(t, result)
	require.Error(t, err)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.NotEqual(t, "text/event-stream", recorder.Header().Get("Content-Type"))
	require.Equal(t, "INVALID_ARGUMENT", gjson.GetBytes(recorder.Body.Bytes(), "error.status").String())
	require.Equal(t, "native bad request", gjson.GetBytes(recorder.Body.Bytes(), "error.message").String())
	require.Equal(t, int32(1), trackingBody.closeCount.Load())
	require.Equal(t, 1, upstream.calls)
}

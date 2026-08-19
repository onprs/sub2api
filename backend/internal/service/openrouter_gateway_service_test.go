package service

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type openRouterHTTPUpstreamStub struct {
	request  *http.Request
	response *http.Response
	err      error
}

func (s *openRouterHTTPUpstreamStub) Do(req *http.Request, proxyURL string, accountID int64, concurrency int) (*http.Response, error) {
	s.request = req
	if s.err != nil {
		return nil, s.err
	}
	return s.response, nil
}

func (s *openRouterHTTPUpstreamStub) DoWithTLS(req *http.Request, proxyURL string, accountID int64, concurrency int, profile *tlsfingerprint.Profile) (*http.Response, error) {
	return s.Do(req, proxyURL, accountID, concurrency)
}

func newOpenRouterTestResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewBufferString(body)),
	}
}

func newOpenRouterTestContext() (*httptest.ResponseRecorder, *gin.Context) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString("{}"))
	return w, c
}

func TestOpenRouterGatewayBufferedChat(t *testing.T) {
	upstream := &openRouterHTTPUpstreamStub{response: newOpenRouterTestResponse(http.StatusOK, `{
		"id":"gen-123",
		"object":"chat.completion",
		"model":"openrouter/free",
		"choices":[{"index":0,"message":{"role":"assistant","content":"Hello world!"},"finish_reason":"stop"}],
		"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}
	}`)}
	client := NewOpenRouterClient(upstream, nil, nil)
	svc := NewOpenRouterGatewayService(client, nil, nil)

	account := &Account{
		ID:          1,
		Platform:    PlatformOpenRouter,
		Type:        AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "or-test-key", "base_url": "https://openrouter.ai/api/v1"},
	}
	recorder, c := newOpenRouterTestContext()

	result, err := svc.ForwardChatCompletions(context.Background(), c, account, []byte(`{
		"model":"openrouter/free",
		"messages":[{"role":"user","content":"Hello"}],
		"stream":false
	}`))
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "openrouter/free", gjson.Get(recorder.Body.String(), "model").String())
	require.Equal(t, "Hello world!", gjson.Get(recorder.Body.String(), "choices.0.message.content").String())
	require.Equal(t, 10, result.Usage.InputTokens)
	require.Equal(t, 5, result.Usage.OutputTokens)

	reqBody, _ := io.ReadAll(upstream.request.Body)
	require.Equal(t, "https://openrouter.ai/api/v1/chat/completions", upstream.request.URL.String())
	require.Equal(t, "Bearer or-test-key", upstream.request.Header.Get("Authorization"))
	require.Equal(t, "openrouter/free", gjson.GetBytes(reqBody, "model").String())
}

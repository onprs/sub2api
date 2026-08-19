package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
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

type openRouterTestAccountRepo struct {
	AccountRepository
	tempUnschedCalls int
	lastTempReason   string
}

func (r *openRouterTestAccountRepo) SetTempUnschedulable(ctx context.Context, id int64, until time.Time, reason string) error {
	r.tempUnschedCalls++
	r.lastTempReason = reason
	return nil
}

func TestOpenRouterGatewayTempUnschedulableRuleFailover(t *testing.T) {
	upstream := &openRouterHTTPUpstreamStub{
		response: newOpenRouterTestResponse(http.StatusBadRequest, `{
			"error": {
				"message": "upstream provider is temporarily overloaded, please retry",
				"type": "invalid_request_error",
				"code": "provider_error"
			}
		}`),
	}
	client := NewOpenRouterClient(upstream, nil, nil)
	repo := &openRouterTestAccountRepo{}
	rateLimitSvc := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
	svc := NewOpenRouterGatewayService(client, nil, rateLimitSvc)

	account := &Account{
		ID:          101,
		Platform:    PlatformOpenRouter,
		Type:        AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":                    "or-test-key",
			"base_url":                   "https://openrouter.ai/api/v1",
			"temp_unschedulable_enabled": true,
			"temp_unschedulable_rules": []any{
				map[string]any{
					"error_code":       400,
					"keywords":         []any{"overloaded", "temporarily"},
					"duration_minutes": 15,
					"description":      "upstream overload rule",
				},
			},
		},
	}
	_, c := newOpenRouterTestContext()

	result, err := svc.ForwardChatCompletions(context.Background(), c, account, []byte(`{
		"model":"openrouter/free",
		"messages":[{"role":"user","content":"Hello"}],
		"stream":false
	}`))
	require.Nil(t, result)
	require.Error(t, err)

	var failoverErr *UpstreamFailoverError
	require.True(t, errors.As(err, &failoverErr), "expected UpstreamFailoverError on temp unschedulable match")
	require.Equal(t, http.StatusBadRequest, failoverErr.StatusCode)
	require.Equal(t, 1, repo.tempUnschedCalls, "should have called SetTempUnschedulable")
}

package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func newOpenCodeGoogleTestAccount(mode string) *Account {
	credentials := map[string]any{"api_key": "sk-opencode-test"}
	if mode != "" {
		credentials["account_mode"] = mode
	}
	return &Account{
		ID: 7, Platform: PlatformOpenCodeGo, Type: AccountTypeAPIKey, Name: "OpenCode test",
		Credentials: credentials,
	}
}

func newOpenCodeGoogleTestContext(t *testing.T, body string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-4-argon:generateContent", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	return c, recorder
}

func TestForwardOpenCodeGoogleGenAIUsesOfficialEndpointAndBillsUsage(t *testing.T) {
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}, "X-Request-Id": []string{"req-opencode-google"}},
		Body:       io.NopCloser(strings.NewReader(`{"responseId":"resp-1","modelVersion":"gemini-4-argon","candidates":[{"content":{"role":"model","parts":[{"text":"hello"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":3,"thoughtsTokenCount":2,"cachedContentTokenCount":4}}`)),
	}}
	svc := &OpenAIGatewayService{httpUpstream: upstream, cfg: upstreamModelSyncTestConfig()}
	c, recorder := newOpenCodeGoogleTestContext(t, `{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`)
	result, err := svc.ForwardGoogleGenAI(context.Background(), c, newOpenCodeGoogleTestAccount(AccountModeGo), "gemini-4-argon", "gemini-4-argon", false, []byte(`{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`))
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "https://opencode.ai/inference/go/google/v1beta/models/gemini-4-argon:generateContent", upstream.lastReq.URL.String())
	require.Equal(t, "Bearer sk-opencode-test", upstream.lastReq.Header.Get("Authorization"))
	require.Equal(t, "req-opencode-google", result.RequestID)
	require.Equal(t, 10, result.Usage.InputTokens)
	require.Equal(t, 5, result.Usage.OutputTokens)
	require.Equal(t, 4, result.Usage.CacheReadInputTokens)
	require.Equal(t, "resp-1", result.ResponseID)
	require.Equal(t, "gemini-4-argon", result.UpstreamResponseModel)
	require.Contains(t, recorder.Body.String(), `"candidates"`)
}

func TestForwardOpenCodeGoogleGenAIPreservesStreamAndCollectsUsage(t *testing.T) {
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body: io.NopCloser(strings.NewReader(
			"data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"hel\"}]}}]}\n\n" +
				"data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"lo\"}]},\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":7,\"candidatesTokenCount\":3}}\n\n",
		)),
	}}
	svc := &OpenAIGatewayService{httpUpstream: upstream, cfg: upstreamModelSyncTestConfig()}
	c, recorder := newOpenCodeGoogleTestContext(t, `{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`)
	result, err := svc.ForwardGoogleGenAI(context.Background(), c, newOpenCodeGoogleTestAccount(AccountModeGo), "gemini-4-argon", "gemini-4-argon", true, []byte(`{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`))
	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.Stream)
	require.NotNil(t, result.FirstTokenMs)
	require.Equal(t, 7, result.Usage.InputTokens)
	require.Equal(t, 3, result.Usage.OutputTokens)
	require.Contains(t, upstream.lastReq.URL.RawQuery, "alt=sse")
	require.Contains(t, recorder.Body.String(), "data: ")
	require.Contains(t, recorder.Body.String(), `"finishReason":"STOP"`)
}

func TestForwardOpenCodeGoogleGenAIFailsOverOnUpstreamError(t *testing.T) {
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusTooManyRequests,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"error":{"code":429,"message":"rate limited"}}`)),
	}}
	svc := &OpenAIGatewayService{httpUpstream: upstream, cfg: upstreamModelSyncTestConfig()}
	c, _ := newOpenCodeGoogleTestContext(t, `{"contents":[]}`)
	_, err := svc.ForwardGoogleGenAI(context.Background(), c, newOpenCodeGoogleTestAccount(AccountModeGo), "gemini-4-argon", "gemini-4-argon", false, []byte(`{"contents":[]}`))
	require.Error(t, err)
	var failover *UpstreamFailoverError
	require.ErrorAs(t, err, &failover)
	require.Equal(t, http.StatusTooManyRequests, failover.StatusCode)
}

func TestForwardOpenCodeGoogleGenAIUsesCustomGoogleBaseURL(t *testing.T) {
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"usageMetadata":{"promptTokenCount":1,"candidatesTokenCount":1}}`)),
	}}
	account := newOpenCodeGoogleTestAccount(AccountModeZen)
	account.Credentials["google_base_url"] = "https://relay.example/google"
	account.Credentials["account_mode"] = AccountModeZen
	svc := &OpenAIGatewayService{httpUpstream: upstream, cfg: upstreamModelSyncTestConfig()}
	c, _ := newOpenCodeGoogleTestContext(t, `{"contents":[]}`)
	_, err := svc.ForwardGoogleGenAI(context.Background(), c, account, "gemini-4-argon", "gemini-4-argon", false, []byte(`{"contents":[]}`))
	require.NoError(t, err)
	require.Equal(t, "https://relay.example/google/v1beta/models/gemini-4-argon:generateContent", upstream.lastReq.URL.String())
}

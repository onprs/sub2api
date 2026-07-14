package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type standardAnthropicErrorCloseTrackingBody struct {
	io.Reader
	closeCount int
}

func (b *standardAnthropicErrorCloseTrackingBody) Close() error {
	b.closeCount++
	return nil
}

func TestCollectGatewayStructuredUpstreamErrorPreservesProtocolAndOwnership(t *testing.T) {
	for _, streamRequested := range []bool{false, true} {
		t.Run(map[bool]string{false: "buffered", true: "stream"}[streamRequested], func(t *testing.T) {
			body := &standardAnthropicErrorCloseTrackingBody{Reader: strings.NewReader(`{"type":"error","error":{"type":"invalid_request_error","message":"bad request"}}`)}
			resp := &http.Response{
				StatusCode: http.StatusBadRequest,
				Header:     http.Header{"Content-Type": []string{"application/json"}, "X-Request-Id": []string{"rid-anthropic-error"}},
				Body:       body,
			}
			svc := &GatewayService{}

			upstream, err := svc.collectGatewayStructuredUpstreamError(resp, protocolconv.ProtocolAnthropic, streamRequested)
			require.NoError(t, err)
			require.Equal(t, protocolconv.ProtocolAnthropic, upstream.ActualProtocol)
			require.Equal(t, http.StatusBadRequest, upstream.StatusCode)
			require.Equal(t, "rid-anthropic-error", upstream.RequestID)
			require.JSONEq(t, `{"type":"error","error":{"type":"invalid_request_error","message":"bad request"}}`, string(upstream.Body))
			require.Equal(t, 1, body.closeCount)
			require.Equal(t, http.NoBody, resp.Body)

			resp.Header.Set("X-Request-Id", "mutated")
			require.Equal(t, "rid-anthropic-error", upstream.Headers.Get("X-Request-Id"))
		})
	}
}

func TestForwardStandardProtocolToAnthropicImmediateErrorsBeforeStreamCommit(t *testing.T) {
	gin.SetMode(gin.TestMode)

	for _, test := range []struct {
		name       string
		statusCode int
		wantWriter bool
		wantFail   bool
	}{
		{name: "source error callback", statusCode: http.StatusBadRequest, wantWriter: true},
		{name: "failover", statusCode: http.StatusTooManyRequests, wantFail: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			responseBody := &standardAnthropicErrorCloseTrackingBody{Reader: strings.NewReader(`{"type":"error","error":{"type":"invalid_request_error","message":"bad request"}}`)}
			upstream := &httpUpstreamRecorder{resp: &http.Response{
				StatusCode: test.statusCode,
				Header:     http.Header{"Content-Type": []string{"application/json"}, "X-Request-Id": []string{"rid-standard-anthropic"}},
				Body:       responseBody,
			}}
			svc := &GatewayService{cfg: &config.Config{}, httpUpstream: upstream}
			account := &Account{ID: 1, Name: "anthropic", Platform: PlatformAnthropic, Type: AccountTypeAPIKey, Concurrency: 1, Credentials: map[string]any{"api_key": "test", "base_url": "https://example.com"}}
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			writerCalled := false

			resp, err := svc.forwardStandardProtocolToAnthropic(context.Background(), c, account, []byte(`{"model":"claude-test","messages":[{"role":"user","content":"hi"}],"stream":true}`), nil, "claude-test", func(statusCode int, errorType, message string) {
				writerCalled = true
				require.Equal(t, http.StatusBadRequest, statusCode)
				require.Equal(t, "server_error", errorType)
				require.Equal(t, "bad request", message)
			})
			require.Error(t, err)
			require.Nil(t, resp)
			require.Equal(t, test.wantWriter, writerCalled)
			require.Equal(t, 1, responseBody.closeCount)
			require.False(t, c.Writer.Written())

			var failoverErr *UpstreamFailoverError
			require.Equal(t, test.wantFail, errors.As(err, &failoverErr))
			if test.wantFail {
				require.Equal(t, "rid-standard-anthropic", failoverErr.ResponseHeaders.Get("X-Request-Id"))
			}
		})
	}
}

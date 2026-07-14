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

func TestResolveStandardAnthropicTargetModel(t *testing.T) {
	tests := []struct {
		name      string
		account   *Account
		requested string
		want      string
		wantErr   string
	}{
		{
			name: "bedrock alias and region", requested: "client-model", want: "eu.anthropic.claude-sonnet-4-6-v1",
			account: &Account{Platform: PlatformAnthropic, Type: AccountTypeBedrock, Credentials: map[string]any{
				"aws_region": "eu-west-1", "model_mapping": map[string]any{"client-model": "us.anthropic.claude-sonnet-4-6-v1"},
			}},
		},
		{
			name: "api key mapping", requested: "client-model", want: "provider-model",
			account: &Account{Platform: PlatformAnthropic, Type: AccountTypeAPIKey, Credentials: map[string]any{
				"model_mapping": map[string]any{"client-model": "provider-model"},
			}},
		},
		{
			name: "vertex normalization", requested: "claude-sonnet-4-5", want: "claude-sonnet-4-5@20250929",
			account: &Account{Platform: PlatformAnthropic, Type: AccountTypeServiceAccount},
		},
		{
			name: "oauth normalization", requested: "claude-sonnet-4-6", want: "claude-sonnet-4-6",
			account: &Account{Platform: PlatformAnthropic, Type: AccountTypeOAuth},
		},
		{name: "nil account", requested: "model", wantErr: "nil account"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := resolveStandardAnthropicTargetModel(test.account, test.requested)
			if test.wantErr != "" {
				require.ErrorContains(t, err, test.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.want, got)
		})
	}
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

func TestGatewayService_AnthropicGenericImmediateStream400BeforeSSECommit(t *testing.T) {
	gin.SetMode(gin.TestMode)

	responseBody := &standardAnthropicErrorCloseTrackingBody{Reader: strings.NewReader(`{"type":"error","error":{"type":"invalid_request_error","message":"bad request"}}`)}
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusBadRequest,
		Header:     http.Header{"Content-Type": []string{"application/json"}, "X-Request-Id": []string{"rid-generic-stream-400"}},
		Body:       responseBody,
	}}
	svc := &GatewayService{
		cfg:              &config.Config{},
		httpUpstream:     upstream,
		rateLimitService: &RateLimitService{},
	}
	account := &Account{
		ID: 2, Name: "anthropic-oauth", Platform: PlatformAnthropic, Type: AccountTypeOAuth, Concurrency: 1,
		Credentials: map[string]any{"access_token": "test-token"},
	}
	parsed, err := ParseGatewayRequest(NewRequestBodyRef([]byte(`{"model":"claude-3-5-sonnet-latest","messages":[{"role":"user","content":"hi"}],"stream":true}`)), PlatformAnthropic)
	require.NoError(t, err)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	result, err := svc.Forward(context.Background(), c, account, parsed)
	require.Error(t, err)
	require.Nil(t, result)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.JSONEq(t, `{"type":"error","error":{"type":"invalid_request_error","message":"bad request"}}`, recorder.Body.String())
	require.NotContains(t, recorder.Body.String(), "event:")
	require.NotContains(t, recorder.Body.String(), "data:")
	require.Equal(t, 1, responseBody.closeCount)
	require.Equal(t, http.NoBody, upstream.resp.Body)
}

func TestGatewayService_AnthropicGenericRetryUsesFinalStructuredError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	firstBody := &standardAnthropicErrorCloseTrackingBody{Reader: strings.NewReader(`{"type":"error","error":{"type":"permission_error","message":"retry me"}}`)}
	finalBody := &standardAnthropicErrorCloseTrackingBody{Reader: strings.NewReader(`{"type":"error","error":{"type":"invalid_request_error","message":"final bad request"}}`)}
	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		{
			StatusCode: http.StatusForbidden,
			Header:     http.Header{"Content-Type": []string{"application/json"}, "X-Request-Id": []string{"rid-first-retry"}},
			Body:       firstBody,
		},
		{
			StatusCode: http.StatusBadRequest,
			Header:     http.Header{"Content-Type": []string{"application/json"}, "X-Request-Id": []string{"rid-final-error"}},
			Body:       finalBody,
		},
	}}
	svc := &GatewayService{
		cfg:              &config.Config{},
		httpUpstream:     upstream,
		rateLimitService: &RateLimitService{},
	}
	account := &Account{
		ID: 3, Name: "anthropic-oauth-retry", Platform: PlatformAnthropic, Type: AccountTypeOAuth, Concurrency: 1,
		Credentials: map[string]any{"access_token": "test-token"},
	}
	parsed, err := ParseGatewayRequest(NewRequestBodyRef([]byte(`{"model":"claude-3-5-sonnet-latest","messages":[{"role":"user","content":"hi"}]}`)), PlatformAnthropic)
	require.NoError(t, err)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	result, err := svc.Forward(context.Background(), c, account, parsed)
	require.Error(t, err)
	require.Nil(t, result)
	require.Len(t, upstream.requests, 2)
	require.Equal(t, 1, firstBody.closeCount)
	require.Equal(t, 1, finalBody.closeCount)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.JSONEq(t, `{"type":"error","error":{"type":"invalid_request_error","message":"final bad request"}}`, recorder.Body.String())
	require.NotContains(t, recorder.Body.String(), "retry me")
}

func TestGatewayService_AnthropicGenericFailoverPreservesDetachedError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	responseBody := &standardAnthropicErrorCloseTrackingBody{Reader: strings.NewReader(`{"type":"error","error":{"type":"api_error","message":"temporary outage"}}`)}
	responseHeader := http.Header{"Content-Type": []string{"application/json"}, "X-Request-Id": []string{"rid-generic-failover"}, "X-Vendor-Detail": []string{"preserved"}}
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusInternalServerError,
		Header:     responseHeader,
		Body:       responseBody,
	}}
	svc := &GatewayService{
		cfg:              &config.Config{},
		httpUpstream:     upstream,
		rateLimitService: &RateLimitService{},
	}
	account := &Account{
		ID: 4, Name: "anthropic-oauth-failover", Platform: PlatformAnthropic, Type: AccountTypeOAuth, Concurrency: 1,
		Credentials: map[string]any{"access_token": "test-token"},
	}
	parsed, err := ParseGatewayRequest(NewRequestBodyRef([]byte(`{"model":"claude-3-5-sonnet-latest","messages":[{"role":"user","content":"hi"}]}`)), PlatformAnthropic)
	require.NoError(t, err)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	result, err := svc.Forward(context.Background(), c, account, parsed)
	require.Error(t, err)
	require.Nil(t, result)
	require.False(t, c.Writer.Written())
	require.Equal(t, 1, responseBody.closeCount)

	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.Equal(t, http.StatusInternalServerError, failoverErr.StatusCode)
	require.JSONEq(t, `{"type":"error","error":{"type":"api_error","message":"temporary outage"}}`, string(failoverErr.ResponseBody))
	require.Equal(t, "rid-generic-failover", failoverErr.ResponseHeaders.Get("X-Request-Id"))
	require.Equal(t, "preserved", failoverErr.ResponseHeaders.Get("X-Vendor-Detail"))

	responseHeader.Set("X-Vendor-Detail", "mutated")
	require.Equal(t, "preserved", failoverErr.ResponseHeaders.Get("X-Vendor-Detail"))
}

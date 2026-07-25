package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func (s *GatewayService) handleStreamingResponse(
	ctx context.Context,
	resp *http.Response,
	c *gin.Context,
	account *Account,
	startTime time.Time,
	originalModel, mappedModel string,
	_ bool,
) (*streamingResult, error) {
	pipeline, err := newAnthropicIdentityPipeline(account, originalModel, mappedModel)
	if err != nil {
		return nil, err
	}
	request, err := json.Marshal(map[string]any{
		"model":    mappedModel,
		"stream":   true,
		"messages": []any{},
	})
	if err != nil {
		return nil, err
	}
	if _, err := pipeline.ConvertRequest(request); err != nil {
		return nil, err
	}
	return s.handleStructuredStreamingResponse(ctx, resp, c, account, pipeline, startTime, originalModel, mappedModel)
}

func TestHandleStructuredStreamingResponseUsesRequestPipelineAndMultilineSSE(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	account := &Account{ID: 41, Platform: PlatformAnthropic}
	pipeline := newAnthropicPassthroughTestPipeline(t, account)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}, "X-Request-Id": []string{"rid-native"}},
		Body: io.NopCloser(strings.NewReader(strings.Join([]string{
			"event: message_start",
			`data: {"type":"message_start",`,
			`data: "message":{"id":"msg_1","model":"claude-upstream","usage":{"input_tokens":3}},"vendor":{"kept":true}}`,
			"",
			"event: message_delta",
			`data: {"type":"message_delta","usage":{"output_tokens":2}}`,
			"",
			"event: message_stop",
			`data: {"type":"message_stop"}`,
			"",
		}, "\n"))),
	}
	svc := &GatewayService{cfg: &config.Config{}, rateLimitService: &RateLimitService{}}

	result, err := svc.handleStructuredStreamingResponse(
		context.Background(), resp, c, account, pipeline, time.Now(), "claude-client", "claude-upstream",
	)

	require.NoError(t, err)
	require.Equal(t, 3, result.usage.InputTokens)
	require.Equal(t, 2, result.usage.OutputTokens)
	require.Equal(t, "rid-native", recorder.Header().Get("X-Request-Id"))
	require.Contains(t, recorder.Body.String(), `"model":"claude-client"`)
	require.Contains(t, recorder.Body.String(), `"vendor":{"kept":true}`)
	require.Contains(t, recorder.Body.String(), "event: message_stop")
	require.NotContains(t, recorder.Body.String(), "[DONE]")
}

func TestHandleStructuredStreamingResponseRequiresCompletedRequestPipeline(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	account := &Account{ID: 42, Platform: PlatformAnthropic}
	pipeline, err := newAnthropicIdentityPipeline(account, "client", "upstream")
	require.NoError(t, err)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body: io.NopCloser(strings.NewReader(
			"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
		)),
	}
	svc := &GatewayService{cfg: &config.Config{}, rateLimitService: &RateLimitService{}}

	result, err := svc.handleStructuredStreamingResponse(
		context.Background(), resp, c, account, pipeline, time.Now(), "client", "upstream",
	)

	require.Nil(t, result)
	require.ErrorContains(t, err, "request conversion has not completed")
	require.False(t, c.Writer.Written())
}

func TestHandleStructuredStreamingResponsePreservesErrorEventDoneData(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	account := &Account{ID: 43, Platform: PlatformAnthropic}
	pipeline := newAnthropicPassthroughTestPipeline(t, account)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader("event: error\ndata: [DONE]\n\n")),
	}
	svc := &GatewayService{cfg: &config.Config{}, rateLimitService: &RateLimitService{}}

	result, err := svc.handleStructuredStreamingResponse(
		context.Background(), resp, c, account, pipeline, time.Now(), "client", "upstream",
	)

	require.Nil(t, result)
	var streamError *sseStreamErrorEventError
	require.ErrorAs(t, err, &streamError)
	require.Equal(t, "[DONE]", streamError.RawData)
	require.False(t, c.Writer.Written())
}

func TestHandleStructuredStreamingResponseRejectsMalformedBeforeCommit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	account := &Account{ID: 43, Platform: PlatformAnthropic}
	pipeline := newAnthropicPassthroughTestPipeline(t, account)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader("data: {broken}\n\n")),
	}
	svc := &GatewayService{cfg: &config.Config{}, rateLimitService: &RateLimitService{}}

	result, err := svc.handleStructuredStreamingResponse(
		context.Background(), resp, c, account, pipeline, time.Now(), "client", "upstream",
	)

	require.NotNil(t, result)
	require.ErrorContains(t, err, "decode Anthropic stream event")
	require.False(t, c.Writer.Written())
}

func TestHandleStructuredStreamingResponseRejectsOversizedRecord(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	account := &Account{ID: 44, Platform: PlatformAnthropic}
	pipeline := newAnthropicPassthroughTestPipeline(t, account)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader("data: " + strings.Repeat("x", 128) + "\n\n")),
	}
	svc := &GatewayService{
		cfg:              &config.Config{Gateway: config.GatewayConfig{MaxLineSize: 64}},
		rateLimitService: &RateLimitService{},
	}

	result, err := svc.handleStructuredStreamingResponse(
		context.Background(), resp, c, account, pipeline, time.Now(), "client", "upstream",
	)

	require.NotNil(t, result)
	require.ErrorContains(t, err, "SSE record exceeds 64 bytes")
	require.Contains(t, recorder.Body.String(), "event: error")
	require.Contains(t, recorder.Body.String(), "response_too_large")
}

func TestHandleStructuredStreamingResponseDataIntervalTimeout(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	account := &Account{ID: 45, Platform: PlatformAnthropic}
	pipeline := newAnthropicPassthroughTestPipeline(t, account)
	reader, writer := io.Pipe()
	defer func() { _ = writer.Close() }()
	resp := &http.Response{StatusCode: http.StatusOK, Body: reader}
	svc := &GatewayService{
		cfg:              &config.Config{Gateway: config.GatewayConfig{StreamDataIntervalTimeout: 1}},
		rateLimitService: &RateLimitService{},
	}

	result, err := svc.handleStructuredStreamingResponse(
		context.Background(), resp, c, account, pipeline, time.Now(), "client", "upstream",
	)

	require.NotNil(t, result)
	require.ErrorContains(t, err, "stream data interval timeout")
	require.Contains(t, recorder.Body.String(), "event: error")
	require.Contains(t, recorder.Body.String(), "stream_timeout")
}

func TestHandleStructuredStreamingResponseDrainsUsageAfterClientDisconnect(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	c.Writer = &failWriteResponseWriter{ResponseWriter: c.Writer}
	account := &Account{ID: 46, Platform: PlatformAnthropic}
	pipeline := newAnthropicPassthroughTestPipeline(t, account)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body: io.NopCloser(strings.NewReader(strings.Join([]string{
			`data: {"type":"message_start","message":{"usage":{"input_tokens":7}}}`,
			"",
			`data: {"type":"message_delta","usage":{"output_tokens":5}}`,
			"",
			`data: {"type":"message_stop"}`,
			"",
		}, "\n"))),
	}
	svc := &GatewayService{cfg: &config.Config{}, rateLimitService: &RateLimitService{}}

	result, err := svc.handleStructuredStreamingResponse(
		context.Background(), resp, c, account, pipeline, time.Now(), "client", "upstream",
	)

	require.NoError(t, err)
	require.True(t, result.clientDisconnect)
	require.Equal(t, 7, result.usage.InputTokens)
	require.Equal(t, 5, result.usage.OutputTokens)
}

func TestHandleStructuredStreamingResponseKeepaliveDoesNotSplitPartialRecord(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	account := &Account{ID: 47, Platform: PlatformAnthropic}
	pipeline := newAnthropicPassthroughTestPipeline(t, account)
	reader, writer := io.Pipe()
	resp := &http.Response{StatusCode: http.StatusOK, Body: reader}
	svc := &GatewayService{
		cfg:              &config.Config{Gateway: config.GatewayConfig{StreamKeepaliveInterval: 1}},
		rateLimitService: &RateLimitService{},
	}

	go func() {
		defer func() { _ = writer.Close() }()
		_, _ = io.WriteString(writer, "event: message_start\n")
		time.Sleep(1100 * time.Millisecond)
		_, _ = io.WriteString(writer, "data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":1}}}\n\n")
		_, _ = io.WriteString(writer, "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
	}()

	result, err := svc.handleStructuredStreamingResponse(
		context.Background(), resp, c, account, pipeline, time.Now(), "client", "upstream",
	)

	require.NoError(t, err)
	require.Equal(t, 1, result.usage.InputTokens)
	require.NotContains(t, recorder.Body.String(), "event: ping")
}

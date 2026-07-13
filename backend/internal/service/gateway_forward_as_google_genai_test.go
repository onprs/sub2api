//go:build unit

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
	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestForwardGoogleGenAI_AnthropicBufferedUsesPipelineAndMappedEndpoint(t *testing.T) {
	body := []byte(`{"contents":[{"role":"user","parts":[{"text":"lookup"}]},{"role":"model","parts":[{"functionCall":{"id":"call_history","name":"lookup","args":{"q":"x"}}}]},{"role":"user","parts":[{"functionResponse":{"id":"call_history","name":"lookup","response":{"value":"ok"}}}]}]}`)
	upstreamBody := strings.Join([]string{
		`event: message_start`,
		`data: {"type":"message_start","message":{"id":"msg_google_buffered","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4-6","usage":{"input_tokens":9,"cache_read_input_tokens":2}}}`,
		``,
		`event: content_block_start`,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"call_buffered","name":"lookup","input":{}}}`,
		``,
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"q\":\"next\"}"}}`,
		``,
		`event: content_block_stop`,
		`data: {"type":"content_block_stop","index":0}`,
		``,
		`event: message_delta`,
		`data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":4}}`,
		``,
		`event: message_stop`,
		`data: {"type":"message_stop"}`,
		``,
	}, "\n")
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}, "x-request-id": []string{"rid_google_buffered"}},
		Body:       io.NopCloser(strings.NewReader(upstreamBody)),
	}}
	svc := &GatewayService{httpUpstream: upstream, cfg: &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{Enabled: false}}}}
	account := &Account{
		ID: 81, Name: "anthropic-google", Platform: PlatformAnthropic, Type: AccountTypeAPIKey, Concurrency: 1,
		Credentials: map[string]any{
			"api_key":       "test-key",
			"model_mapping": map[string]any{"google-client-model": "claude-sonnet-4-6"},
		},
	}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/google-client-model:generateContent", bytes.NewReader(body))

	result, err := svc.ForwardGoogleGenAI(context.Background(), c, account, "google-client-model", "google-client-model", false, body)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "google-client-model", result.Model)
	require.Equal(t, "claude-sonnet-4-6", result.UpstreamModel)
	require.Equal(t, 9, result.Usage.InputTokens)
	require.Equal(t, 4, result.Usage.OutputTokens)
	require.Equal(t, 2, result.Usage.CacheReadInputTokens)
	require.Equal(t, "/v1/messages", upstream.lastReq.URL.Path)
	require.Equal(t, "claude-sonnet-4-6", gjson.GetBytes(upstream.lastBody, "model").String())
	require.True(t, gjson.GetBytes(upstream.lastBody, "stream").Bool())
	require.Equal(t, int64(8192), gjson.GetBytes(upstream.lastBody, "max_tokens").Int())
	require.Equal(t, "lookup", gjson.GetBytes(upstream.lastBody, "messages.0.content.0.text").String())
	require.Equal(t, "call_history", gjson.GetBytes(upstream.lastBody, "messages.1.content.0.id").String())
	require.Equal(t, "lookup", gjson.GetBytes(upstream.lastBody, "messages.1.content.0.name").String())
	require.Equal(t, "call_history", gjson.GetBytes(upstream.lastBody, "messages.2.content.0.tool_use_id").String())
	require.Equal(t, "ok", gjson.GetBytes(upstream.lastBody, "messages.2.content.0.content.value").String())
	require.Equal(t, "call_buffered", gjson.GetBytes(rec.Body.Bytes(), "candidates.0.content.parts.0.functionCall.id").String())
	require.Equal(t, "lookup", gjson.GetBytes(rec.Body.Bytes(), "candidates.0.content.parts.0.functionCall.name").String())
	require.Equal(t, "next", gjson.GetBytes(rec.Body.Bytes(), "candidates.0.content.parts.0.functionCall.args.q").String())
	require.Equal(t, "STOP", gjson.GetBytes(rec.Body.Bytes(), "candidates.0.finishReason").String())
	require.Equal(t, "google-client-model", gjson.GetBytes(rec.Body.Bytes(), "modelVersion").String())
	require.Equal(t, int64(11), gjson.GetBytes(rec.Body.Bytes(), "usageMetadata.promptTokenCount").Int())
	require.Equal(t, int64(2), gjson.GetBytes(rec.Body.Bytes(), "usageMetadata.cachedContentTokenCount").Int())
}

func TestForwardGoogleGenAI_AnthropicRejectsUnsupportedAudioBeforeNetwork(t *testing.T) {
	body := []byte(`{"contents":[{"role":"user","parts":[{"inlineData":{"mimeType":"audio/wav","data":"AA=="}}]}]}`)
	upstream := &httpUpstreamRecorder{}
	svc := &GatewayService{httpUpstream: upstream, cfg: &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{Enabled: false}}}}
	account := &Account{ID: 83, Platform: PlatformAnthropic, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "test-key"}}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/claude-sonnet-4-6:generateContent", bytes.NewReader(body))

	result, err := svc.ForwardGoogleGenAI(context.Background(), c, account, "claude-sonnet-4-6", "claude-sonnet-4-6", false, body)
	require.Error(t, err)
	require.Nil(t, result)
	var conversionErr *protocolconv.Error
	require.ErrorAs(t, err, &conversionErr)
	require.Equal(t, protocolconv.ErrorUnsupportedCapability, conversionErr.Code)
	require.Nil(t, upstream.lastReq)
}

func TestForwardGoogleGenAI_AnthropicStreamUsesGoogleFramingAndToolLifecycle(t *testing.T) {
	body := []byte(`{"contents":[{"role":"user","parts":[{"text":"lookup"}]}],"tools":[{"functionDeclarations":[{"name":"lookup","parameters":{"type":"object"}}]}],"generationConfig":{"maxOutputTokens":32}}`)
	upstreamBody := strings.Join([]string{
		`event: message_start`,
		`data: {"type":"message_start","message":{"id":"msg_google_stream","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4-6","usage":{"input_tokens":8,"cache_creation_input_tokens":1}}}`,
		``,
		`event: content_block_start`,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"call_google","name":"lookup","input":{}}}`,
		``,
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"q\":\"x\"}"}}`,
		``,
		`event: content_block_stop`,
		`data: {"type":"content_block_stop","index":0}`,
		``,
		`event: message_delta`,
		`data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":3}}`,
		``,
		`event: message_stop`,
		`data: {"type":"message_stop"}`,
		``,
	}, "\n")
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}, "x-request-id": []string{"rid_google_stream"}},
		Body:       io.NopCloser(strings.NewReader(upstreamBody)),
	}}
	svc := &GatewayService{httpUpstream: upstream, cfg: &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{Enabled: false}}}}
	account := &Account{
		ID: 82, Name: "anthropic-google", Platform: PlatformAnthropic, Type: AccountTypeAPIKey, Concurrency: 1,
		Credentials: map[string]any{"api_key": "test-key", "model_mapping": map[string]any{"google-client-model": "claude-sonnet-4-6"}},
	}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/google-client-model:streamGenerateContent", bytes.NewReader(body))

	result, err := svc.ForwardGoogleGenAI(context.Background(), c, account, "google-client-model", "google-client-model", true, body)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, 8, result.Usage.InputTokens)
	require.Equal(t, 3, result.Usage.OutputTokens)
	require.Equal(t, 1, result.Usage.CacheCreationInputTokens)
	require.Equal(t, "text/event-stream", rec.Header().Get("Content-Type"))
	wire := rec.Body.String()
	require.NotContains(t, wire, "event:")
	require.NotContains(t, wire, "[DONE]")
	require.Contains(t, wire, `"id":"call_google"`)
	require.Contains(t, wire, `"name":"lookup"`)
	require.Contains(t, wire, `"q":"x"`)
	require.Contains(t, wire, `"finishReason":"STOP"`)
	require.Contains(t, wire, `"modelVersion":"google-client-model"`)
	require.Contains(t, wire, `"promptTokenCount":9`)
}

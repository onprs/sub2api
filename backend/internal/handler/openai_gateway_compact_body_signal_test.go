package handler

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

func newCompactBodySignalTestContext(t *testing.T, path string, body []byte) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	return c
}

// 原生 remote compaction v2 必须保持流式 /responses 线型，不能降级为
// 已停用的 unary /responses/compact。
func TestNormalizeOpenAIResponsesCompactRequest_RemoteV2StaysOnResponses(t *testing.T) {
	h := &OpenAIGatewayHandler{}
	body := []byte(`{
		"model":"gpt-5.6-sol",
		"stream":true,
		"store":true,
		"prompt_cache_key":"pck-signal-1",
		"reasoning":{"effort":"max","context":"all_turns"},
		"input":[
			{"type":"message","role":"user","content":"hello"},
			{"type":"compaction_trigger"}
		]
	}`)
	c := newCompactBodySignalTestContext(t, "/v1/responses", body)
	c.Request.Header.Set("x-codex-beta-features", "responses_websockets_v2, remote_compaction_v2, another_feature")

	normalized, ok := h.normalizeOpenAIResponsesCompactRequest(c, zap.NewNop(), body)
	require.True(t, ok)
	require.Equal(t, "/v1/responses", c.Request.URL.Path)
	require.False(t, isOpenAILegacyCompactPath(c))
	require.True(t, isOpenAIRemoteCompactionV2Request(normalized))
	require.Equal(t, body, normalized)
	require.True(t, gjson.GetBytes(normalized, "stream").Bool())
	require.True(t, gjson.GetBytes(normalized, "store").Bool())
	require.Equal(t, "pck-signal-1", gjson.GetBytes(normalized, "prompt_cache_key").String())
	require.Equal(t, "max", gjson.GetBytes(normalized, "reasoning.effort").String())
	require.Equal(t, "all_turns", gjson.GetBytes(normalized, "reasoning.context").String())

	reqStream, streamOK := parseOpenAICompatibleStream(normalized)
	require.True(t, streamOK)
	require.True(t, reqStream)
	_, seedExists := c.Get(service.OpenAICompactSessionSeedKeyForTest())
	require.False(t, seedExists)
	_, streamMarkerExists := c.Get(service.OpenAICompactClientStreamKeyForTest())
	require.False(t, streamMarkerExists)
}

func TestNormalizeOpenAIResponsesCompactRequest_RemoteV2PathAliasesStayOnResponses(t *testing.T) {
	h := &OpenAIGatewayHandler{}
	body := []byte(`{"model":"gpt-5.6-sol","stream":true,"input":[{"type":"compaction_trigger"}]}`)
	for _, path := range []string{
		"/v1/responses",
		"/v1/responses/",
		"/openai/v1/responses",
		"/responses/",
		"/backend-api/codex/responses",
	} {
		t.Run(path, func(t *testing.T) {
			c := newCompactBodySignalTestContext(t, path, body)
			normalized, ok := h.normalizeOpenAIResponsesCompactRequest(c, zap.NewNop(), body)
			require.True(t, ok)
			require.Equal(t, path, c.Request.URL.Path)
			require.Equal(t, body, normalized)
			require.True(t, isOpenAIRemoteCompactionV2Request(normalized))
		})
	}
}

func TestNormalizeOpenAIResponsesCompactRequest_BodySignalTrailingSlashPromoted(t *testing.T) {
	h := &OpenAIGatewayHandler{}
	body := []byte(`{"model":"gpt-5.5","input":[{"type":"compaction_trigger"}]}`)
	c := newCompactBodySignalTestContext(t, "/v1/responses/", body)

	_, ok := h.normalizeOpenAIResponsesCompactRequest(c, zap.NewNop(), body)
	require.True(t, ok)
	require.Equal(t, "/v1/responses/compact", c.Request.URL.Path)
}

func TestNormalizeOpenAIResponsesCompactRequest_CodexDirectAliasPromoted(t *testing.T) {
	h := &OpenAIGatewayHandler{}
	body := []byte(`{"model":"gpt-5.5","input":[{"type":"compaction_trigger"}]}`)
	c := newCompactBodySignalTestContext(t, "/backend-api/codex/responses", body)

	_, ok := h.normalizeOpenAIResponsesCompactRequest(c, zap.NewNop(), body)
	require.True(t, ok)
	require.Equal(t, "/backend-api/codex/responses/compact", c.Request.URL.Path)
}

func TestNormalizeOpenAIResponsesCompactRequest_NoTriggerUntouched(t *testing.T) {
	h := &OpenAIGatewayHandler{}
	body := []byte(`{"model":"gpt-5.5","stream":true,"input":[{"type":"message","role":"user","content":"hello"}]}`)
	c := newCompactBodySignalTestContext(t, "/v1/responses", body)

	normalized, ok := h.normalizeOpenAIResponsesCompactRequest(c, zap.NewNop(), body)
	require.True(t, ok)
	require.Equal(t, "/v1/responses", c.Request.URL.Path)
	require.False(t, isOpenAILegacyCompactPath(c))
	require.Equal(t, body, normalized)
	require.True(t, gjson.GetBytes(normalized, "stream").Bool())
}

func TestNormalizeOpenAIResponsesCompactRequest_PathBasedNoDoubleSuffix(t *testing.T) {
	h := &OpenAIGatewayHandler{}
	body := []byte(`{"model":"gpt-5.5","stream":true,"store":true,"input":[{"type":"message","role":"user","content":"hello"}]}`)
	c := newCompactBodySignalTestContext(t, "/v1/responses/compact", body)
	c.Request.Header.Set("x-codex-beta-features", "remote_compaction_v2")

	normalized, ok := h.normalizeOpenAIResponsesCompactRequest(c, zap.NewNop(), body)
	require.True(t, ok)
	require.Equal(t, "/v1/responses/compact", c.Request.URL.Path)
	require.False(t, gjson.GetBytes(normalized, "stream").Exists())
	require.False(t, gjson.GetBytes(normalized, "store").Exists())
}

func TestNormalizeOpenAIResponsesCompactRequest_SubpathNotPromoted(t *testing.T) {
	h := &OpenAIGatewayHandler{}
	body := []byte(`{"model":"gpt-5.5","input":[{"type":"compaction_trigger"}]}`)
	c := newCompactBodySignalTestContext(t, "/v1/responses/resp_123/cancel", body)

	normalized, ok := h.normalizeOpenAIResponsesCompactRequest(c, zap.NewNop(), body)
	require.True(t, ok)
	require.Equal(t, "/v1/responses/resp_123/cancel", c.Request.URL.Path)
	require.Equal(t, body, normalized)
}

// 原生 v2 不再启用 unary-to-SSE 桥接标记。
func TestNormalizeOpenAIResponsesCompactRequest_RemoteV2DoesNotMarkClientStream(t *testing.T) {
	h := &OpenAIGatewayHandler{}
	body := []byte(`{"model":"gpt-5.5","stream":true,"input":[{"type":"compaction_trigger"}]}`)
	c := newCompactBodySignalTestContext(t, "/v1/responses", body)

	_, ok := h.normalizeOpenAIResponsesCompactRequest(c, zap.NewNop(), body)
	require.True(t, ok)
	require.Equal(t, "/v1/responses", c.Request.URL.Path)
	_, exists := c.Get(service.OpenAICompactClientStreamKeyForTest())
	require.False(t, exists)
}

func TestNormalizeOpenAIResponsesCompactRequest_BodySignalStreamFalseNotMarked(t *testing.T) {
	h := &OpenAIGatewayHandler{}
	for name, body := range map[string][]byte{
		"stream_false":  []byte(`{"model":"gpt-5.5","stream":false,"input":[{"type":"compaction_trigger"}]}`),
		"stream_absent": []byte(`{"model":"gpt-5.5","input":[{"type":"compaction_trigger"}]}`),
	} {
		c := newCompactBodySignalTestContext(t, "/v1/responses", body)
		_, ok := h.normalizeOpenAIResponsesCompactRequest(c, zap.NewNop(), body)
		require.True(t, ok, name)
		require.Equal(t, "/v1/responses/compact", c.Request.URL.Path, name)
		_, exists := c.Get(service.OpenAICompactClientStreamKeyForTest())
		require.False(t, exists, "case %s 不应标记 client-stream", name)
	}
}

func TestOpenAIResponsesRequiredCapabilityForRequest(t *testing.T) {
	require.Equal(t, service.OpenAIEndpointCapabilityChatCompletions,
		openAIResponsesRequiredCapabilityForRequest(false, false, service.PlatformOpenAI))
	require.Equal(t, service.OpenAIEndpointCapabilityResponses,
		openAIResponsesRequiredCapabilityForRequest(false, true, service.PlatformOpenAI))
	require.Equal(t, service.OpenAIEndpointCapabilityResponses,
		openAIResponsesRequiredCapabilityForRequest(true, false, service.PlatformOpenAI))
	require.Equal(t, service.OpenAIEndpointCapabilityChatCompletions,
		openAIResponsesRequiredCapabilityForRequest(false, true, service.PlatformGrok))
}

// path-based compact（Codex v1 unary 协议）即使 body 带 stream:true 也不标记，
// 保持 JSON 写回行为不变。
func TestNormalizeOpenAIResponsesCompactRequest_PathBasedStreamTrueNotMarked(t *testing.T) {
	h := &OpenAIGatewayHandler{}
	body := []byte(`{"model":"gpt-5.5","stream":true,"input":[{"type":"message","role":"user","content":"hello"}]}`)
	c := newCompactBodySignalTestContext(t, "/v1/responses/compact", body)

	_, ok := h.normalizeOpenAIResponsesCompactRequest(c, zap.NewNop(), body)
	require.True(t, ok)
	_, exists := c.Get(service.OpenAICompactClientStreamKeyForTest())
	require.False(t, exists)
}

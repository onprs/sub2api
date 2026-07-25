package service

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestHandleGeminiStreamToNonStreamingPreservesOrderedParts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	writer := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(writer)

	stream := bytes.NewBufferString(
		"data: {\"response\":{\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"inspect\",\"thought\":true,\"thoughtSignature\":\"sig-thought\"},{\"functionCall\":{\"name\":\"read_file\",\"args\":{\"path\":\"demo.txt\"}},\"thoughtSignature\":\"sig-call\"}]}}]}}\n\n" +
			"data: {\"response\":{\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"\"}]},\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":10,\"candidatesTokenCount\":2,\"totalTokenCount\":12}}}\n\n",
	)
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(stream)}

	service := &AntigravityGatewayService{settingService: &SettingService{cfg: &config.Config{}}}
	result, err := service.handleGeminiStreamToNonStreaming(ctx, resp, time.Now())
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, 10, result.usage.InputTokens)
	require.Equal(t, 2, result.usage.OutputTokens)

	var body map[string]any
	require.NoError(t, json.Unmarshal(writer.Body.Bytes(), &body))
	parts := extractGeminiParts(body)
	require.Len(t, parts, 2)
	require.Equal(t, "inspect", parts[0]["text"])
	require.Equal(t, true, parts[0]["thought"])
	require.Equal(t, "sig-thought", parts[0]["thoughtSignature"])
	require.Equal(t, "sig-call", parts[1]["thoughtSignature"])
	call, ok := parts[1]["functionCall"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "read_file", call["name"])
	candidates, ok := body["candidates"].([]any)
	require.True(t, ok)
	require.NotEmpty(t, candidates)
	candidate, ok := candidates[0].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "STOP", candidate["finishReason"])
}

func TestMergeCollectedPartsPreservesMediaAndMergesAdjacentText(t *testing.T) {
	base := map[string]any{"candidates": []any{map[string]any{"finishReason": "STOP"}}}
	parts := []map[string]any{
		{"text": "hel"},
		{"text": "lo"},
		{"inlineData": map[string]any{"mimeType": "image/png", "data": "AA=="}},
		{"functionCall": map[string]any{"name": "read_file", "args": map[string]any{}}},
	}

	merged := mergeCollectedPartsToResponse(base, parts)
	got := extractGeminiParts(merged)
	require.Len(t, got, 3)
	require.Equal(t, "hello", got[0]["text"])
	require.Contains(t, got[1], "inlineData")
	require.Contains(t, got[2], "functionCall")
}

package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRunCompletesChatToolRoundTrip(t *testing.T) {
	t.Setenv("SUB2API_PROBE_API_KEY", "test-key")
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
		attempt := calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if attempt == 1 {
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "chat-1", "object": "chat.completion", "model": "model", "choices": []any{map[string]any{"index": 0, "finish_reason": "tool_calls", "message": map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"id": "call-1", "type": "function", "function": map[string]any{"name": "read_file", "arguments": `{"path":"demo.txt"}`}}}}}}, "usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 2, "total_tokens": 12}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "chat-2", "object": "chat.completion", "model": "model", "choices": []any{map[string]any{"index": 0, "finish_reason": "stop", "message": map[string]any{"role": "assistant", "content": "demo content"}}}, "usage": map[string]any{"prompt_tokens": 20, "completion_tokens": 3, "total_tokens": 23}})
	}))
	defer server.Close()
	result := run("openai_chat_completions", server.URL, "model")
	require.Empty(t, result.Error)
	require.True(t, result.ToolCall)
	require.True(t, result.ToolCallIDPreserved)
	require.True(t, result.FinalText)
	require.Equal(t, int32(2), calls.Load())
}

func TestRunRejectsMissingToolCallID(t *testing.T) {
	t.Setenv("SUB2API_PROBE_API_KEY", "test-key")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "chat-1", "object": "chat.completion", "model": "model", "choices": []any{map[string]any{"index": 0, "finish_reason": "tool_calls", "message": map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"type": "function", "function": map[string]any{"name": "read_file", "arguments": `{}`}}}}}}})
	}))
	defer server.Close()

	result := run("openai_chat_completions", server.URL, "model")
	require.Contains(t, result.Error, "tool call requires ID and name")
	require.False(t, result.ToolCallIDPreserved)
}

func TestSanitizeRedactsKey(t *testing.T) {
	t.Setenv("SUB2API_PROBE_API_KEY", "secret-key")
	require.NotContains(t, sanitize("bad secret-key"), "secret-key")
}

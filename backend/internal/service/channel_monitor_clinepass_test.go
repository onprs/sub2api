package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestChannelMonitorClinePassProviderAdapter(t *testing.T) {
	adapter, apiMode, ok := providerAdapterFor(MonitorProviderClinePass, MonitorAPIModeChatCompletions)
	require.True(t, ok)
	require.Equal(t, MonitorAPIModeChatCompletions, apiMode)
	require.Equal(t, "/chat/completions", adapter.buildPath("cline-pass/glm-5.2"))
	require.Equal(t, "Bearer cp-key", adapter.buildHeaders("cp-key")["Authorization"])
	require.Equal(t, "text/event-stream", adapter.buildHeaders("cp-key")["Accept"])
	require.Equal(t, "data.choices.0.message.content", adapter.textPath)
	require.Equal(t, "channel_monitor_provider_clinepass", adapter.releaseGuardMarker)
	require.ErrorIs(t, validateAPIMode(MonitorProviderClinePass, MonitorAPIModeMessages), ErrChannelMonitorInvalidAPIMode)
}

func TestRunCheckForModelClinePassUsesStreamingContract(t *testing.T) {
	oldClient := monitorHTTPClient
	monitorHTTPClient = &http.Client{Transport: http.DefaultTransport}
	t.Cleanup(func() { monitorHTTPClient = oldClient })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/chat/completions", r.URL.Path)
		require.Equal(t, "Bearer cp-key", r.Header.Get("Authorization"))
		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		require.Equal(t, "cline-pass/glm-5.2", body["model"])
		require.EqualValues(t, monitorClinePassChallengeMaxTokens, body["max_tokens"])
		require.Equal(t, true, body["stream"])
		require.NotContains(t, body, "temperature")
		require.Equal(t, "text/event-stream", r.Header.Get("Accept"))

		messages := body["messages"].([]any)
		require.Len(t, messages, 2)
		require.Equal(t, "system", messages[0].(map[string]any)["role"])
		require.Equal(t, "user", messages[1].(map[string]any)["role"])
		prompt := messages[1].(map[string]any)["content"].(string)
		expected := expectedClinePassCheckCode(t, prompt)
		w.Header().Set("Content-Type", "text/event-stream")
		chunk, err := json.Marshal(map[string]any{
			"choices": []map[string]any{{
				"delta": map[string]any{"content": expected},
			}},
		})
		require.NoError(t, err)
		_, _ = w.Write([]byte(": keepalive\n\nevent: completion\ndata: " + string(chunk) + "\n\ndata: [DONE]\n\n"))
	}))
	defer srv.Close()

	result := runCheckForModel(context.Background(), MonitorProviderClinePass, srv.URL, "cp-key", "cline-pass/glm-5.2", nil)
	require.Equal(t, MonitorStatusOperational, result.Status, result.Message)
}

func TestExtractClinePassChatTextSupportsRootAndContentBlocks(t *testing.T) {
	text, err := extractClinePassChatText([]byte(`{
		"choices":[{"message":{"content":[{"type":"text","text":"654321"}]}}]
	}`))
	require.NoError(t, err)
	require.Equal(t, "654321", text)
}

func TestExtractClinePassChatTextSupportsBufferedEnvelope(t *testing.T) {
	text, err := extractClinePassChatText([]byte(`{
		"success":true,
		"data":{"choices":[{"message":{"content":"654321"}}]}
	}`))
	require.NoError(t, err)
	require.Equal(t, "654321", text)
}

func TestRunCheckForModelClinePassExplainsReasoningOnlyResponse(t *testing.T) {
	oldClient := monitorHTTPClient
	monitorHTTPClient = &http.Client{Transport: http.DefaultTransport}
	t.Cleanup(func() { monitorHTTPClient = oldClient })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"working\"}}]}\n\n" +
			"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"length\"}]}\n\n" +
			"data: [DONE]\n\n"))
	}))
	defer srv.Close()

	result := runCheckForModel(context.Background(), MonitorProviderClinePass, srv.URL, "cp-key", "cline-pass/glm-5.2", nil)
	require.Equal(t, MonitorStatusFailed, result.Status)
	require.Contains(t, result.Message, "exhausted the output budget")
}

func expectedClinePassCheckCode(t *testing.T, prompt string) string {
	t.Helper()
	matches := regexp.MustCompile(`check code:\s*(\d+)`).FindStringSubmatch(prompt)
	require.Len(t, matches, 2)
	return matches[1]
}

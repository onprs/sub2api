package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestChannelMonitorClinePassProviderAdapter(t *testing.T) {
	adapter, apiMode, ok := providerAdapterFor(MonitorProviderClinePass, MonitorAPIModeChatCompletions)
	require.True(t, ok)
	require.Equal(t, MonitorAPIModeChatCompletions, apiMode)
	require.Equal(t, "/chat/completions", adapter.buildPath("cline-pass/glm-5.2"))
	require.Equal(t, "Bearer cp-key", adapter.buildHeaders("cp-key")["Authorization"])
	require.Equal(t, "data.choices.0.message.content", adapter.textPath)
	require.Equal(t, "channel_monitor_provider_clinepass", adapter.releaseGuardMarker)
	require.ErrorIs(t, validateAPIMode(MonitorProviderClinePass, MonitorAPIModeMessages), ErrChannelMonitorInvalidAPIMode)
}

func TestRunCheckForModelClinePassUnwrapsEnvelope(t *testing.T) {
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

		messages := body["messages"].([]any)
		prompt := messages[0].(map[string]any)["content"].(string)
		expected := expectedChallengeAnswer(t, prompt)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"data": map[string]any{
				"choices": []map[string]any{{
					"message": map[string]any{
						"content":           expected,
						"reasoning_content": "ignored reasoning",
					},
				}},
			},
		})
	}))
	defer srv.Close()

	result := runCheckForModel(context.Background(), MonitorProviderClinePass, srv.URL, "cp-key", "cline-pass/glm-5.2", nil)
	require.Equal(t, MonitorStatusOperational, result.Status, result.Message)
}

func expectedChallengeAnswer(t *testing.T, prompt string) string {
	t.Helper()
	matches := regexp.MustCompile(`Q: (\d+) ([+-]) (\d+) = \?`).FindAllStringSubmatch(prompt, -1)
	require.NotEmpty(t, matches)
	last := matches[len(matches)-1]
	left, err := strconv.Atoi(last[1])
	require.NoError(t, err)
	right, err := strconv.Atoi(last[3])
	require.NoError(t, err)
	if last[2] == "+" {
		return strconv.Itoa(left + right)
	}
	return strconv.Itoa(left - right)
}

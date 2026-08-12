package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestChannelMonitorOpenCodeGoEndpointAllowsVersionedBasePath(t *testing.T) {
	const publicEndpoint = "https://8.8.8.8/v1"

	// Use an IP literal so this validation test does not depend on local DNS.
	require.NoError(t, validateEndpointForProvider(MonitorProviderOpenCodeGo, publicEndpoint))
	require.Equal(t, publicEndpoint, normalizeEndpoint(" "+publicEndpoint+"/ "))

	require.ErrorIs(t, validateEndpointForProvider(MonitorProviderOpenAI, publicEndpoint), ErrChannelMonitorEndpointPath)
}

func TestRunCheckForModel_OpenCodeGoEmptyExtractedTextHintsEndpointOrAPIMode(t *testing.T) {
	orig := monitorHTTPClient
	monitorHTTPClient = &http.Client{Timeout: 5 * time.Second}
	t.Cleanup(func() { monitorHTTPClient = orig })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)

	res := runCheckForModel(context.Background(), MonitorProviderOpenCodeGo, srv.URL, "sk-test", "deepseek-v4-flash", &CheckOptions{
		APIMode: MonitorAPIModeChatCompletions,
	})

	require.Equal(t, MonitorStatusFailed, res.Status)
	require.Contains(t, strings.ToLower(res.Message), "empty")
	require.Contains(t, strings.ToLower(res.Message), "endpoint")
	require.Contains(t, strings.ToLower(res.Message), "api_mode")
}

func TestRunCheckForModel_OpenCodeGoChatRequestUsesMonitorBudget(t *testing.T) {
	orig := monitorHTTPClient
	monitorHTTPClient = &http.Client{Timeout: 5 * time.Second}
	t.Cleanup(func() { monitorHTTPClient = orig })

	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, providerOpenCodeGoChatPath, r.URL.Path)
		defer func() { _ = r.Body.Close() }()
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))

		answer := answerFromOpenCodeGoMonitorRequest(body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]any{"content": answer}}},
		})
	}))
	t.Cleanup(srv.Close)

	res := runCheckForModel(context.Background(), MonitorProviderOpenCodeGo, srv.URL, "sk-test", "deepseek-v4-flash", &CheckOptions{
		APIMode: MonitorAPIModeChatCompletions,
	})

	require.Equal(t, MonitorStatusOperational, res.Status)
	require.Equal(t, float64(monitorOpenCodeGoChallengeMaxTokens), body["max_tokens"])
	require.Equal(t, float64(0), body["temperature"])
	require.Equal(t, false, body["stream"])
}

func TestRunCheckForModel_OpenCodeGoMessagesRequestUsesMonitorBudget(t *testing.T) {
	orig := monitorHTTPClient
	monitorHTTPClient = &http.Client{Timeout: 5 * time.Second}
	t.Cleanup(func() { monitorHTTPClient = orig })

	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, providerOpenCodeGoMessagesPath, r.URL.Path)
		require.Equal(t, "sk-test", r.Header.Get("x-api-key"))
		require.Equal(t, monitorAnthropicAPIVersion, r.Header.Get("anthropic-version"))
		require.Empty(t, r.Header.Get("Authorization"))
		defer func() { _ = r.Body.Close() }()
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))

		answer := answerFromOpenCodeGoMonitorRequest(body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"content": []map[string]any{{"type": "text", "text": answer}},
		})
	}))
	t.Cleanup(srv.Close)

	res := runCheckForModel(context.Background(), MonitorProviderOpenCodeGo, srv.URL, "sk-test", "qwen3.7-plus", &CheckOptions{
		APIMode: MonitorAPIModeMessages,
	})

	require.Equal(t, MonitorStatusOperational, res.Status)
	require.Equal(t, float64(monitorOpenCodeGoChallengeMaxTokens), body["max_tokens"])
	require.Equal(t, float64(0), body["temperature"])
}

func TestRunCheckForModel_OpenCodeGoResponsesRequestUsesMonitorBudget(t *testing.T) {
	orig := monitorHTTPClient
	monitorHTTPClient = &http.Client{Timeout: 5 * time.Second}
	t.Cleanup(func() { monitorHTTPClient = orig })

	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, providerOpenCodeGoResponsesPath, r.URL.Path)
		require.Equal(t, "Bearer sk-test", r.Header.Get("Authorization"))
		defer func() { _ = r.Body.Close() }()
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))

		answer := answerFromOpenCodeGoResponsesMonitorRequest(body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"output": []map[string]any{
				{"type": "reasoning", "summary": []map[string]any{{"type": "summary_text", "text": "ignore"}}},
				{"type": "message", "content": []map[string]any{{"type": "output_text", "text": answer}}},
			},
		})
	}))
	t.Cleanup(srv.Close)

	res := runCheckForModel(context.Background(), MonitorProviderOpenCodeGo, srv.URL, "sk-test", "gpt-5.6-luna", &CheckOptions{
		APIMode: MonitorAPIModeResponses,
	})

	require.Equal(t, MonitorStatusOperational, res.Status)
	require.Equal(t, float64(monitorOpenCodeGoChallengeMaxTokens), body["max_output_tokens"])
	require.Equal(t, false, body["stream"])
	require.NotEmpty(t, body["instructions"])
}

func TestRunCheckForModel_OpenCodeGoResponsesSemanticFailurePreservesMessage(t *testing.T) {
	orig := monitorHTTPClient
	monitorHTTPClient = &http.Client{Timeout: 5 * time.Second}
	t.Cleanup(func() { monitorHTTPClient = orig })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, providerOpenCodeGoResponsesPath, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"resp_failed","status":"failed","output":[],"error":{"code":"server_is_overloaded","message":"provider overloaded"}}`))
	}))
	t.Cleanup(srv.Close)

	res := runCheckForModel(context.Background(), MonitorProviderOpenCodeGo, srv.URL, "sk-test", "gpt-5.6-luna", &CheckOptions{
		APIMode: MonitorAPIModeResponses,
	})

	require.Equal(t, MonitorStatusFailed, res.Status)
	require.Contains(t, res.Message, "OpenCode Go Responses failed")
	require.Contains(t, res.Message, "provider overloaded")
}

func TestExtractOpenCodeGoChatTextAggregatesVisibleContentBlocks(t *testing.T) {
	body := []byte(`{
		"choices": [
			{"message": {"content": [
				{"type":"reasoning","text":"ignore 999"},
				{"type":"text","text":"4"},
				{"type":"output_text","text":"2"}
			]}}
		]
	}`)

	require.Equal(t, "42", extractOpenCodeGoChatText(body))
}

var openCodeGoMonitorQuestionRegex = regexp.MustCompile(`Q: (\d+) ([+-]) (\d+) = \?\nA:$`)

func answerFromOpenCodeGoResponsesMonitorRequest(body map[string]any) string {
	prompt, _ := body["input"].(string)
	return answerFromOpenCodeGoMonitorPrompt(prompt)
}

func answerFromOpenCodeGoMonitorRequest(body map[string]any) string {
	messages, _ := body["messages"].([]any)
	if len(messages) == 0 {
		return "0"
	}
	msg, _ := messages[0].(map[string]any)
	prompt, _ := msg["content"].(string)
	return answerFromOpenCodeGoMonitorPrompt(prompt)
}

func answerFromOpenCodeGoMonitorPrompt(prompt string) string {
	parts := openCodeGoMonitorQuestionRegex.FindStringSubmatch(prompt)
	if len(parts) != 4 {
		return "0"
	}
	left, _ := strconv.Atoi(parts[1])
	right, _ := strconv.Atoi(parts[3])
	if parts[2] == "+" {
		return strconv.Itoa(left + right)
	}
	return strconv.Itoa(left - right)
}

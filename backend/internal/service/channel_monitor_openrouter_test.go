package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestChannelMonitorOpenRouterAdapter(t *testing.T) {
	adapter, mode, ok := providerAdapterFor(MonitorProviderOpenRouter, MonitorAPIModeChatCompletions)
	require.True(t, ok)
	require.Equal(t, MonitorAPIModeChatCompletions, mode)
	require.Equal(t, "choices.0.message.content", adapter.textPath)
	require.Equal(t, "channel_monitor_provider_openrouter", adapter.releaseGuardMarker)

	headers := adapter.buildHeaders("test-key")
	require.Equal(t, "Bearer test-key", headers["Authorization"])
	require.Equal(t, "https://sub2api.local", headers["HTTP-Referer"])
	require.Equal(t, "sub2api", headers["X-Title"])

	path := adapter.buildPath("openrouter/free")
	require.Equal(t, "/chat/completions", path)

	body, err := adapter.buildBody("openrouter/free", "PING 1234")
	require.NoError(t, err)
	require.Contains(t, string(body), `"model":"openrouter/free"`)
	require.Contains(t, string(body), `"content":"PING 1234"`)
	require.Contains(t, string(body), `"max_tokens":4096`)
}

func TestOpenRouterMonitorDiagnostics(t *testing.T) {
	// 截断 length 诊断
	lengthBody := `{"choices":[{"finish_reason":"length","message":{"reasoning":"thinking...","content":""}}]}`
	msgLength := emptyMonitorResponseMessage(MonitorProviderOpenRouter, lengthBody)
	require.Contains(t, msgLength, "exhausted the output budget")

	// 仅返回 reasoning 诊断
	reasoningBody := `{"choices":[{"finish_reason":"stop","message":{"reasoning_content":"thinking...","content":""}}]}`
	msgReasoning := emptyMonitorResponseMessage(MonitorProviderOpenRouter, reasoningBody)
	require.Contains(t, msgReasoning, "returned reasoning but no final response text")

	// 提取常规 text
	normalBody := []byte(`{"choices":[{"message":{"content":"PING-1234"}}]}`)
	require.Equal(t, "PING-1234", extractOpenRouterChatText(normalBody))
}

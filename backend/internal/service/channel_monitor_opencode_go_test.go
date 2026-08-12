package service

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/ent/channelmonitor"
	"github.com/Wei-Shaw/sub2api/ent/channelmonitorrequesttemplate"
	"github.com/stretchr/testify/require"
)

func TestChannelMonitorOpenCodeGoEntEnums(t *testing.T) {
	require.NoError(t, channelmonitor.ProviderValidator(channelmonitor.ProviderOpencodeGo))
	require.NoError(t, channelmonitorrequesttemplate.ProviderValidator(channelmonitorrequesttemplate.ProviderOpencodeGo))
}

func TestChannelMonitorOpenCodeGoProviderAdapters(t *testing.T) {
	chatAdapter, apiMode, ok := providerAdapterFor(MonitorProviderOpenCodeGo, MonitorAPIModeChatCompletions)
	require.True(t, ok)
	require.Equal(t, MonitorAPIModeChatCompletions, apiMode)
	require.Equal(t, "/chat/completions", chatAdapter.buildPath("kimi-k2.7-code"))
	require.Equal(t, "Bearer ocg-key", chatAdapter.buildHeaders("ocg-key")["Authorization"])
	require.Equal(t, "choices.0.message.content", chatAdapter.textPath)

	messagesAdapter, apiMode, ok := providerAdapterFor(MonitorProviderOpenCodeGo, MonitorAPIModeMessages)
	require.True(t, ok)
	require.Equal(t, MonitorAPIModeMessages, apiMode)
	require.Equal(t, "/messages", messagesAdapter.buildPath("qwen3.7-plus"))
	messagesHeaders := messagesAdapter.buildHeaders("ocg-key")
	require.Equal(t, "ocg-key", messagesHeaders["x-api-key"])
	require.Equal(t, monitorAnthropicAPIVersion, messagesHeaders["anthropic-version"])
	require.Empty(t, messagesHeaders["Authorization"])
	require.Equal(t, "content.0.text", messagesAdapter.textPath)

	responsesAdapter, apiMode, ok := providerAdapterFor(MonitorProviderOpenCodeGo, MonitorAPIModeResponses)
	require.True(t, ok)
	require.Equal(t, MonitorAPIModeResponses, apiMode)
	require.Equal(t, "/responses", responsesAdapter.buildPath("gpt-5.6-luna"))
	require.Equal(t, "Bearer ocg-key", responsesAdapter.buildHeaders("ocg-key")["Authorization"])
	require.Equal(t, "output.0.content.0.text", responsesAdapter.textPath)
}

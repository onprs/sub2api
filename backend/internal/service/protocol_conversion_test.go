package service

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestConvertStandardResponseOverridesClientFacingModel(t *testing.T) {
	tests := []struct {
		name   string
		body   string
		source protocolconv.Protocol
		target protocolconv.Protocol
	}{
		{
			name:   "anthropic to chat completions",
			body:   `{"id":"msg-1","type":"message","role":"assistant","model":"upstream-model","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":3,"output_tokens":2}}`,
			source: protocolconv.ProtocolAnthropic,
			target: protocolconv.ProtocolOpenAIChat,
		},
		{
			name:   "chat completions to anthropic",
			body:   `{"id":"chatcmpl-1","object":"chat.completion","created":1,"model":"upstream-model","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`,
			source: protocolconv.ProtocolOpenAIChat,
			target: protocolconv.ProtocolAnthropic,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			converted, warnings, err := convertStandardResponse(
				[]byte(tt.body), tt.source, tt.target, "upstream-model", "client-model",
			)
			require.NoError(t, err)
			require.Empty(t, warnings)
			require.Equal(t, "client-model", gjson.GetBytes(converted, "model").String())
		})
	}
}

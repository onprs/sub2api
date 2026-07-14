package service

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestStandardProtocolPipelineRestoresClientFacingResponseModel(t *testing.T) {
	tests := []struct {
		name             string
		request          string
		upstreamResponse string
		source           protocolconv.Protocol
		target           protocolconv.Protocol
	}{
		{
			name:             "anthropic client through chat completions upstream",
			request:          `{"model":"client-model","max_tokens":16,"messages":[{"role":"user","content":"hello"}]}`,
			upstreamResponse: `{"id":"chatcmpl-1","object":"chat.completion","created":1,"model":"upstream-model","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`,
			source:           protocolconv.ProtocolAnthropic,
			target:           protocolconv.ProtocolOpenAIChat,
		},
		{
			name:             "chat completions client through anthropic upstream",
			request:          `{"model":"client-model","messages":[{"role":"user","content":"hello"}]}`,
			upstreamResponse: `{"id":"msg-1","type":"message","role":"assistant","model":"upstream-model","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":3,"output_tokens":2}}`,
			source:           protocolconv.ProtocolOpenAIChat,
			target:           protocolconv.ProtocolAnthropic,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pipeline, err := protocolconv.NewPipeline(standardProtocolRegistry, protocolconv.PipelineConfig{
				Route: protocolconv.Route{
					Source:         tt.source,
					IntendedTarget: tt.target,
					ClientModel:    "client-model",
					UpstreamModel:  "upstream-model",
				},
				Options: protocolconv.Options{SourceModel: "upstream-model", LossPolicy: protocolconv.LossError},
			})
			require.NoError(t, err)
			_, err = pipeline.ConvertRequest([]byte(tt.request))
			require.NoError(t, err)

			converted, err := pipeline.ConvertResponse([]byte(tt.upstreamResponse), tt.target)
			require.NoError(t, err)
			require.Empty(t, converted.Warnings)
			require.Equal(t, "client-model", gjson.GetBytes(converted.Body, "model").String())
		})
	}
}

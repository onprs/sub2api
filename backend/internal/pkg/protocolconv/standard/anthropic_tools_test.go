package standard

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestPipelineConvertsFunctionToolsToAnthropicCustomTools(t *testing.T) {
	for _, test := range []struct {
		name   string
		source protocolconv.Protocol
		body   string
		schema string
	}{
		{
			name:   "responses",
			source: protocolconv.ProtocolOpenAIResponses,
			body:   `{"model":"client-model","input":"查询状态","tools":[{"type":"function","name":"lookup_status","description":"查询状态","parameters":{"type":"object","properties":{"id":{"type":"string"}},"required":["id"]}}]}`,
			schema: `{"type":"object","properties":{"id":{"type":"string"}},"required":["id"]}`,
		},
		{
			name:   "responses_without_parameters",
			source: protocolconv.ProtocolOpenAIResponses,
			body:   `{"model":"client-model","input":"查询状态","tools":[{"type":"function","name":"lookup_status","description":"查询状态"}]}`,
			schema: `{"type":"object"}`,
		},
		{
			name:   "chat_completions",
			source: protocolconv.ProtocolOpenAIChat,
			body:   `{"model":"client-model","messages":[{"role":"user","content":"查询状态"}],"tools":[{"type":"function","function":{"name":"lookup_status","description":"查询状态","parameters":{"type":"object","properties":{"id":{"type":"string"}},"required":["id"]}}}]}`,
			schema: `{"type":"object","properties":{"id":{"type":"string"}},"required":["id"]}`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			registry, err := NewRegistry()
			require.NoError(t, err)
			pipeline, err := protocolconv.NewPipeline(registry, protocolconv.PipelineConfig{
				Route: protocolconv.Route{
					Source: test.source, IntendedTarget: protocolconv.ProtocolAnthropic,
					ClientModel: "client-model", UpstreamModel: "upstream-model",
				},
				Options: protocolconv.Options{LossPolicy: protocolconv.LossError},
			})
			require.NoError(t, err)

			converted, err := pipeline.ConvertRequest([]byte(test.body))
			require.NoError(t, err)
			require.Empty(t, converted.Warnings)
			require.Len(t, gjson.GetBytes(converted.Body, "tools").Array(), 1)
			require.False(t, gjson.GetBytes(converted.Body, "tools.0.type").Exists(), "Anthropic 自定义工具不能携带 OpenAI 的 function 标签")
			require.Equal(t, "lookup_status", gjson.GetBytes(converted.Body, "tools.0.name").String())
			require.Equal(t, "查询状态", gjson.GetBytes(converted.Body, "tools.0.description").String())
			require.JSONEq(t, test.schema, gjson.GetBytes(converted.Body, "tools.0.input_schema").Raw)
		})
	}
}

package standard

import (
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv"
	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv/ir"
	"github.com/stretchr/testify/require"
)

func TestAnthropicToResponsesPreservesMultimodalToolResult(t *testing.T) {
	registry, err := NewRegistry()
	require.NoError(t, err)
	body := []byte(`{
		"model":"claude-test",
		"max_tokens":64,
		"messages":[
			{"role":"assistant","content":[{"type":"tool_use","id":"call-image","name":"inspect","input":{}}]},
			{"role":"user","content":[{"type":"tool_result","tool_use_id":"call-image","content":[
				{"type":"text","text":"chart"},
				{"type":"image","source":{"type":"base64","media_type":"image/png","data":"AAAA"}}
			]}]}
		]
	}`)

	converted, warnings, err := registry.ConvertRequest(body, protocolconv.ProtocolAnthropic, protocolconv.ProtocolOpenAIResponses, protocolconv.Options{LossPolicy: protocolconv.LossError})
	require.NoError(t, err)
	require.Empty(t, warnings)

	var wire struct {
		Input []struct {
			Type   string `json:"type"`
			CallID string `json:"call_id"`
			Output []struct {
				Type     string `json:"type"`
				Text     string `json:"text"`
				ImageURL string `json:"image_url"`
			} `json:"output"`
		} `json:"input"`
	}
	require.NoError(t, json.Unmarshal(converted, &wire))
	require.Len(t, wire.Input, 2)
	require.Equal(t, "function_call_output", wire.Input[1].Type)
	require.Equal(t, "call-image", wire.Input[1].CallID)
	require.Len(t, wire.Input[1].Output, 2)
	require.Equal(t, "chart", wire.Input[1].Output[0].Text)
	require.Equal(t, "data:image/png;base64,AAAA", wire.Input[1].Output[1].ImageURL)
}

func TestRoundTripDoesNotInflateHistory(t *testing.T) {
	registry, err := NewRegistry()
	require.NoError(t, err)
	for _, source := range protocolconv.StandardProtocols() {
		for _, target := range protocolconv.StandardProtocols() {
			options := protocolconv.Options{SourceModel: "test-model", LossPolicy: protocolconv.LossWarn}
			original, _, err := registry.DecodeRequest(requestFixture(t, source), source, options)
			require.NoError(t, err)
			encoded, _, err := registry.EncodeRequest(original, target, options)
			require.NoError(t, err)
			roundTrip, _, err := registry.DecodeRequest(encoded, target, options)
			require.NoError(t, err)
			reencoded, _, err := registry.EncodeRequest(roundTrip, target, options)
			require.NoError(t, err)
			final, _, err := registry.DecodeRequest(reencoded, target, options)
			require.NoError(t, err)
			require.Equal(t, semanticCounts(roundTrip), semanticCounts(final), "%s to %s", source, target)
		}
	}
}

func semanticCounts(request *ir.Request) map[string]int {
	out := map[string]int{"messages": len(request.Messages), "system": len(request.SystemInstruction), "tools": len(request.Tools)}
	for _, message := range request.Messages {
		for _, part := range message.Content {
			out[string(part.Type)]++
		}
	}
	return out
}

func BenchmarkRequestMatrix(b *testing.B) {
	registry, err := NewRegistry()
	if err != nil {
		b.Fatal(err)
	}
	value := map[string]any{
		"model":    "bench-model",
		"messages": []any{map[string]any{"role": "system", "content": "stable"}, map[string]any{"role": "user", "content": "hello"}},
		"tools":    []any{map[string]any{"type": "function", "function": map[string]any{"name": "tool", "parameters": map[string]any{"type": "object"}}}},
	}
	body, err := json.Marshal(value)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for _, target := range protocolconv.StandardProtocols() {
		target := target
		b.Run("openai_chat_to_"+target.String(), func(b *testing.B) {
			options := protocolconv.Options{SourceModel: "bench-model", LossPolicy: protocolconv.LossWarn}
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, _, err := registry.ConvertRequest(body, protocolconv.ProtocolOpenAIChat, target, options); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

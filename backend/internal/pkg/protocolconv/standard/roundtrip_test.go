package standard

import (
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv"
	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv/ir"
	"github.com/stretchr/testify/require"
)

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

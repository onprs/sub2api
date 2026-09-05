package googlegenai

import (
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv/ir"
	streamstate "github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv/stream"
	"github.com/stretchr/testify/require"
)

func decodeGoogleStreamForTest(t *testing.T, chunks ...string) []ir.StreamEvent {
	t.Helper()
	decoder := newStreamDecoder()
	state := streamstate.NewContext()
	var events []ir.StreamEvent
	for _, chunk := range chunks {
		out, warnings, err := decoder.Decode([]byte(chunk))
		require.NoError(t, err)
		require.Empty(t, warnings)
		for _, event := range out {
			require.NoError(t, state.Apply(event))
		}
		events = append(events, out...)
	}
	out, warnings, err := decoder.Finalize()
	require.NoError(t, err)
	require.Empty(t, warnings)
	for _, event := range out {
		require.NoError(t, state.Apply(event))
	}
	require.True(t, state.Ended())
	return append(events, out...)
}

func TestStreamDecoderPreservesIncrementalTextAndReasoning(t *testing.T) {
	events := decodeGoogleStreamForTest(t,
		`{"candidates":[{"content":{"parts":[{"text":"go","thought":true}]}}]}`,
		`{"candidates":[{"content":{"parts":[{"text":"go","thought":true},{"text":"goal","thought":true}]}}]}`,
		`{"candidates":[{"content":{"parts":[{"text":"ha"}]}}]}`,
		`{"candidates":[{"content":{"parts":[{"text":"ha"},{"text":"happy"}]}}]}`,
		`{"candidates":[{"finishReason":"STOP"}]}`,
	)
	var text, reasoning strings.Builder
	for _, event := range events {
		_, _ = text.WriteString(event.Text)
		_, _ = reasoning.WriteString(event.Reasoning)
	}
	require.Equal(t, "hahahappy", text.String())
	require.Equal(t, "gogogoal", reasoning.String())
}

func TestStreamDecoderKeepsMissingToolIDsUniqueAcrossChunks(t *testing.T) {
	for _, secondName := range []string{"lookup", "inspect"} {
		t.Run(secondName, func(t *testing.T) {
			events := decodeGoogleStreamForTest(t,
				`{"candidates":[{"content":{"parts":[{"functionCall":{"name":"lookup","args":{"q":"first"}},"thoughtSignature":"sig-first"}]}}]}`,
				`{"candidates":[{"content":{"parts":[{"functionCall":{"name":"`+secondName+`","args":{"q":"second"}},"thoughtSignature":"sig-second"}]},"finishReason":"STOP"}]}`,
			)
			var ids, names, signatures []string
			arguments := map[string]string{}
			for _, event := range events {
				switch event.Type {
				case ir.EventToolCallStart:
					ids = append(ids, event.ToolCallID)
					names = append(names, event.ToolName)
				case ir.EventToolCallDelta:
					arguments[event.ToolCallID] += event.ArgumentsDelta
				case ir.EventToolCallEnd:
					signatures = append(signatures, event.Signature)
				}
			}
			require.Len(t, ids, 2)
			require.NotEqual(t, ids[0], ids[1])
			require.Equal(t, []string{"lookup", secondName}, names)
			require.Equal(t, []string{"sig-first", "sig-second"}, signatures)
			require.JSONEq(t, `{"q":"first"}`, arguments[ids[0]])
			require.JSONEq(t, `{"q":"second"}`, arguments[ids[1]])
		})
	}
}

func TestStreamDecoderPreservesLatestUsageBeforeFinish(t *testing.T) {
	events := decodeGoogleStreamForTest(t,
		`{"usageMetadata":{"promptTokenCount":8,"candidatesTokenCount":1,"totalTokenCount":9}}`,
		`{"candidates":[{"content":{"parts":[{"text":"ok"}]}}],"usageMetadata":{"promptTokenCount":8,"candidatesTokenCount":3,"totalTokenCount":11,"cachedContentTokenCount":4,"thoughtsTokenCount":1}}`,
		`{"candidates":[{"finishReason":"STOP"}]}`,
	)
	var usage *ir.Usage
	for _, event := range events {
		if event.Type == ir.EventUsage {
			usage = event.Usage
		}
	}
	require.NotNil(t, usage)
	require.Equal(t, 8, usage.InputTokens)
	require.Equal(t, 4, usage.OutputTokens)
	require.Equal(t, 11, usage.TotalTokens)
	require.Equal(t, 4, usage.CacheReadTokens)
	require.Equal(t, 1, usage.ReasoningTokens)
}

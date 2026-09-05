package standard

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv"
	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv/ir"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestStreamMatrixPreservesLengthLimit(t *testing.T) {
	registry, err := NewRegistry()
	require.NoError(t, err)
	fixture := []ir.StreamEvent{
		{Type: ir.EventStreamStart, ResponseID: "resp-limited", Model: "model"},
		{Type: ir.EventContentBlockStart, BlockIndex: 0, BlockType: ir.ContentText},
		{Type: ir.EventTextDelta, BlockIndex: 0, Text: "partial"},
		{Type: ir.EventContentBlockEnd, BlockIndex: 0},
		{Type: ir.EventFinish, FinishReason: &ir.FinishReason{Reason: "length"}},
		{Type: ir.EventUsage, Usage: &ir.Usage{InputTokens: 3, OutputTokens: 2, TotalTokens: 5}},
		{Type: ir.EventStreamEnd},
	}
	for _, source := range protocolconv.StandardProtocols() {
		for _, target := range protocolconv.StandardProtocols() {
			t.Run(source.String()+"/"+target.String(), func(t *testing.T) {
				session, err := registry.NewStreamSession(source, target)
				require.NoError(t, err)
				var converted [][]byte
				for _, chunk := range encodeFixture(t, registry, source, fixture) {
					out, warnings, err := session.Convert(chunk)
					require.NoError(t, err)
					require.Empty(t, warnings)
					converted = append(converted, out...)
				}
				out, warnings, err := session.Finalize()
				require.NoError(t, err)
				require.Empty(t, warnings)
				converted = append(converted, out...)
				if target == protocolconv.ProtocolOpenAIResponses {
					terminal := converted[len(converted)-1]
					require.Equal(t, "response.incomplete", gjson.GetBytes(terminal, "type").String())
					require.Equal(t, "incomplete", gjson.GetBytes(terminal, "response.status").String())
				}
				var finish *ir.FinishReason
				var usage *ir.Usage
				for _, event := range decodePayloads(t, registry, target, converted) {
					if event.Type == ir.EventFinish {
						finish = event.FinishReason
					}
					if event.Type == ir.EventUsage {
						usage = event.Usage
					}
				}
				require.NotNil(t, finish)
				require.Equal(t, "length", finish.Reason)
				require.NotNil(t, usage)
				require.Equal(t, 2, usage.OutputTokens)
			})
		}
	}
}

func TestGoogleStreamToolCallsRemainDistinctAcrossProtocols(t *testing.T) {
	registry, err := NewRegistry()
	require.NoError(t, err)
	chunks := []string{
		`{"responseId":"resp-tools","modelVersion":"model","candidates":[{"content":{"parts":[{"functionCall":{"name":"lookup","args":{"q":"first"}}}]}}]}`,
		`{"candidates":[{"content":{"parts":[{"functionCall":{"name":"lookup","args":{"q":"second"}}}]}}],"usageMetadata":{"promptTokenCount":3,"candidatesTokenCount":2,"totalTokenCount":5}}`,
		`{"candidates":[{"finishReason":"STOP"}]}`,
	}
	for _, target := range protocolconv.StandardProtocols() {
		t.Run(target.String(), func(t *testing.T) {
			session, err := registry.NewStreamSession(protocolconv.ProtocolGoogleGenAI, target)
			require.NoError(t, err)
			var converted [][]byte
			for _, chunk := range chunks {
				out, warnings, err := session.Convert([]byte(chunk))
				require.NoError(t, err)
				require.Empty(t, warnings)
				converted = append(converted, out...)
			}
			out, _, err := session.Finalize()
			require.NoError(t, err)
			converted = append(converted, out...)
			var ids []string
			arguments := map[string]string{}
			var usage *ir.Usage
			for _, event := range decodePayloads(t, registry, target, converted) {
				switch event.Type {
				case ir.EventToolCallStart:
					ids = append(ids, event.ToolCallID)
					require.Equal(t, "lookup", event.ToolName)
				case ir.EventToolCallDelta:
					arguments[event.ToolCallID] += event.ArgumentsDelta
				case ir.EventUsage:
					usage = event.Usage
				}
			}
			require.Len(t, ids, 2)
			require.NotEqual(t, ids[0], ids[1])
			require.JSONEq(t, `{"q":"first"}`, arguments[ids[0]])
			require.JSONEq(t, `{"q":"second"}`, arguments[ids[1]])
			require.NotNil(t, usage)
			require.Equal(t, 2, usage.OutputTokens)
		})
	}
}

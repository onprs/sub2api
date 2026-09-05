package standard

import (
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv"
	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv/ir"
	streamstate "github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv/stream"
	"github.com/stretchr/testify/require"
)

func TestStreamMatrixPreservesLifecycleAndSemantics(t *testing.T) {
	registry, err := NewRegistry()
	require.NoError(t, err)
	canonical := streamFixture()

	for _, source := range protocolconv.StandardProtocols() {
		source := source
		t.Run(source.String(), func(t *testing.T) {
			sourcePayloads := encodeFixture(t, registry, source, canonical)
			for _, target := range protocolconv.StandardProtocols() {
				target := target
				t.Run(target.String(), func(t *testing.T) {
					session, err := registry.NewStreamSessionWithOptions(source, target, protocolconv.Options{LossPolicy: protocolconv.LossWarn})
					require.NoError(t, err)
					var converted [][]byte
					var warnings []protocolconv.Warning
					for _, payload := range sourcePayloads {
						out, chunkWarnings, err := session.Convert(payload)
						require.NoError(t, err)
						converted = append(converted, out...)
						warnings = append(warnings, chunkWarnings...)
					}
					final, finalWarnings, err := session.Finalize()
					require.NoError(t, err)
					converted = append(converted, final...)
					warnings = append(warnings, finalWarnings...)
					require.NotEmpty(t, converted)

					decoded := decodePayloads(t, registry, target, converted)
					requireStreamSemantics(t, decoded)
					requireStreamSignaturePolicy(t, source, target, decoded, warnings)
				})
			}
		})
	}
}

func TestStreamSessionsDoNotShareState(t *testing.T) {
	registry, err := NewRegistry()
	require.NoError(t, err)
	first, err := registry.NewStreamSession(protocolconv.ProtocolOpenAIResponses, protocolconv.ProtocolGoogleGenAI)
	require.NoError(t, err)
	second, err := registry.NewStreamSession(protocolconv.ProtocolOpenAIResponses, protocolconv.ProtocolGoogleGenAI)
	require.NoError(t, err)

	startOne := []byte(`{"type":"response.created","response":{"id":"resp-one","object":"response","model":"model-one","status":"in_progress","output":[]}}`)
	startTwo := []byte(`{"type":"response.created","response":{"id":"resp-two","object":"response","model":"model-two","status":"in_progress","output":[]}}`)
	_, _, err = first.Convert(startOne)
	require.NoError(t, err)
	_, _, err = second.Convert(startTwo)
	require.NoError(t, err)

	// A duplicate start is rejected only within the first session.
	_, _, err = first.Convert(startOne)
	require.Error(t, err)
	_, _, err = second.Convert([]byte(`{"type":"response.output_item.added","output_index":0,"item":{"type":"message","role":"assistant","status":"in_progress"}}`))
	require.NoError(t, err)
}

func encodeFixture(t *testing.T, registry *protocolconv.Registry, target protocolconv.Protocol, events []ir.StreamEvent) [][]byte {
	t.Helper()
	converter, err := registry.Converter(target)
	require.NoError(t, err)
	encoder := converter.NewStreamEncoder()
	if factory, ok := converter.(protocolconv.StreamFactoryWithOptions); ok {
		encoder = factory.NewStreamEncoderWithOptions(protocolconv.Options{LossPolicy: protocolconv.LossWarn})
	}
	require.NotNil(t, encoder)
	var payloads [][]byte
	for _, event := range events {
		out, _, err := encoder.Encode(event)
		require.NoError(t, err, event.Type)
		payloads = append(payloads, out...)
	}
	final, _, err := encoder.Finalize()
	require.NoError(t, err)
	return append(payloads, final...)
}

func decodePayloads(t *testing.T, registry *protocolconv.Registry, source protocolconv.Protocol, payloads [][]byte) []ir.StreamEvent {
	t.Helper()
	converter, err := registry.Converter(source)
	require.NoError(t, err)
	decoder := converter.NewStreamDecoder()
	require.NotNil(t, decoder)
	state := streamstate.NewContext()
	var events []ir.StreamEvent
	for _, payload := range payloads {
		decoded, _, err := decoder.Decode(payload)
		require.NoError(t, err, string(payload))
		for _, event := range decoded {
			require.NoError(t, state.Apply(event), event.Type)
		}
		events = append(events, decoded...)
	}
	final, _, err := decoder.Finalize()
	require.NoError(t, err)
	for _, event := range final {
		require.NoError(t, state.Apply(event), event.Type)
	}
	events = append(events, final...)
	require.True(t, state.Ended())
	return events
}

func requireStreamSemantics(t *testing.T, events []ir.StreamEvent) {
	t.Helper()
	var text, reasoning, arguments strings.Builder
	finishCount, endCount := 0, 0
	var usage *ir.Usage
	for _, event := range events {
		switch event.Type {
		case ir.EventTextDelta:
			_, _ = text.WriteString(event.Text)
		case ir.EventReasoningDelta:
			_, _ = reasoning.WriteString(event.Reasoning)
		case ir.EventToolCallDelta:
			_, _ = arguments.WriteString(event.ArgumentsDelta)
		case ir.EventFinish:
			finishCount++
		case ir.EventUsage:
			usage = event.Usage
		case ir.EventStreamEnd:
			endCount++
		}
	}
	require.Equal(t, "dododone", text.String())
	require.Equal(t, "inspect inspect inspect first", reasoning.String())
	require.JSONEq(t, `{"path":"demo"}`, arguments.String())
	require.Equal(t, 1, finishCount)
	require.Equal(t, 1, endCount)
	require.NotNil(t, usage)
	require.Equal(t, 60, usage.CacheReadTokens)
}

func requireStreamSignaturePolicy(t *testing.T, source, target protocolconv.Protocol, events []ir.StreamEvent, warnings []protocolconv.Warning) {
	t.Helper()
	if source == protocolconv.ProtocolOpenAIChat {
		return
	}
	if target == protocolconv.ProtocolOpenAIChat {
		for _, warning := range warnings {
			if warning.Capability == protocolconv.CapabilitySignature {
				return
			}
		}
		t.Fatal("Chat stream conversion dropped a reasoning signature without warning")
	}
	for _, event := range events {
		if event.Type == ir.EventReasoningDelta && event.Signature == "sig-1" {
			return
		}
	}
	t.Fatalf("target %s lost reasoning signature in stream conversion", target)
}

func streamFixture() []ir.StreamEvent {
	return []ir.StreamEvent{
		{Type: ir.EventStreamStart, ResponseID: "resp-1", Model: "test-model"},
		{Type: ir.EventContentBlockStart, BlockIndex: 0, BlockType: ir.ContentReasoning},
		{Type: ir.EventReasoningDelta, BlockIndex: 0, Reasoning: "inspect "},
		{Type: ir.EventReasoningDelta, BlockIndex: 0, Reasoning: "inspect "},
		{Type: ir.EventReasoningDelta, BlockIndex: 0, Reasoning: "inspect first", Signature: "sig-1"},
		{Type: ir.EventContentBlockEnd, BlockIndex: 0},
		{Type: ir.EventContentBlockStart, BlockIndex: 1, BlockType: ir.ContentToolCall},
		{Type: ir.EventToolCallStart, BlockIndex: 1, ToolCallIndex: 0, ToolCallID: "call-1", ToolName: "read_file"},
		{Type: ir.EventToolCallDelta, BlockIndex: 1, ToolCallIndex: 0, ToolCallID: "call-1", ArgumentsDelta: `{"path":`},
		{Type: ir.EventToolCallDelta, BlockIndex: 1, ToolCallIndex: 0, ToolCallID: "call-1", ArgumentsDelta: `"demo"}`},
		{Type: ir.EventToolCallEnd, BlockIndex: 1, ToolCallIndex: 0, ToolCallID: "call-1"},
		{Type: ir.EventContentBlockEnd, BlockIndex: 1},
		{Type: ir.EventContentBlockStart, BlockIndex: 2, BlockType: ir.ContentText},
		{Type: ir.EventTextDelta, BlockIndex: 2, Text: "do"},
		{Type: ir.EventTextDelta, BlockIndex: 2, Text: "do"},
		{Type: ir.EventTextDelta, BlockIndex: 2, Text: "done"},
		{Type: ir.EventContentBlockEnd, BlockIndex: 2},
		{Type: ir.EventFinish, FinishReason: &ir.FinishReason{Reason: "tool_calls"}},
		{Type: ir.EventUsage, Usage: &ir.Usage{InputTokens: 100, OutputTokens: 30, TotalTokens: 130, CacheReadTokens: 60, ReasoningTokens: 10}},
		{Type: ir.EventStreamEnd},
	}
}

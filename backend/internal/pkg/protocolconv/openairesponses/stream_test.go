package openairesponses

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv/ir"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestStreamDecoderAllowsInProgressAfterCreated(t *testing.T) {
	decoder := newStreamDecoder()
	events, _, err := decoder.Decode([]byte(`{"type":"response.created","response":{"id":"resp-1","object":"response","model":"model","status":"in_progress","output":[]}}`))
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, ir.EventStreamStart, events[0].Type)

	events, _, err = decoder.Decode([]byte(`{"type":"response.in_progress","response":{"id":"resp-1","object":"response","model":"model","status":"in_progress","output":[]}}`))
	require.NoError(t, err)
	require.Empty(t, events)
}

func TestStreamDecoderPreservesReasoningSignatureFromCompletedItem(t *testing.T) {
	decoder := newStreamDecoder()
	payloads := [][]byte{
		[]byte(`{"type":"response.created","response":{"id":"resp-1","model":"model","status":"in_progress"}}`),
		[]byte(`{"type":"response.output_item.added","output_index":0,"item":{"type":"reasoning","id":"reasoning-1","encrypted_content":"sig-added"}}`),
		[]byte(`{"type":"response.reasoning_summary_text.delta","output_index":0,"delta":"plan"}`),
		[]byte(`{"type":"response.output_item.done","output_index":0,"item":{"type":"reasoning","id":"reasoning-1","encrypted_content":"sig-done","summary":[{"type":"summary_text","text":"plan"}]}}`),
	}
	var events []ir.StreamEvent
	for _, payload := range payloads {
		out, _, err := decoder.Decode(payload)
		require.NoError(t, err)
		events = append(events, out...)
	}
	require.Equal(t, []ir.StreamEventType{
		ir.EventStreamStart, ir.EventContentBlockStart, ir.EventReasoningDelta,
		ir.EventReasoningDelta, ir.EventContentBlockEnd,
	}, streamEventTypes(events))
	require.Equal(t, "plan", events[2].Reasoning)
	require.Equal(t, "sig-done", events[3].Signature)
}

func TestStreamEncoderPreservesSignatureOnlyReasoningDelta(t *testing.T) {
	encoder := newStreamEncoder()
	sequence := []ir.StreamEvent{
		{Type: ir.EventStreamStart, ResponseID: "resp-1", Model: "model"},
		{Type: ir.EventContentBlockStart, BlockIndex: 0, BlockType: ir.ContentReasoning},
		{Type: ir.EventReasoningDelta, BlockIndex: 0, Reasoning: "plan"},
		{Type: ir.EventReasoningDelta, BlockIndex: 0, Signature: "sig-1"},
		{Type: ir.EventContentBlockEnd, BlockIndex: 0},
	}
	var payloads [][]byte
	for _, event := range sequence {
		out, warnings, err := encoder.Encode(event)
		require.NoError(t, err)
		require.Empty(t, warnings)
		payloads = append(payloads, out...)
	}
	require.NotEmpty(t, payloads)
	last := payloads[len(payloads)-1]
	require.Equal(t, "response.output_item.done", gjson.GetBytes(last, "type").String())
	require.Equal(t, "sig-1", gjson.GetBytes(last, "item.encrypted_content").String())
	require.Equal(t, "plan", gjson.GetBytes(last, "item.summary.0.text").String())
}

func TestStreamDecoderSynthesizesSparseTextLifecycleAndTopLevelUsage(t *testing.T) {
	decoder := newStreamDecoder()
	payloads := [][]byte{
		[]byte(`{"type":"response.created","response":{"id":"resp-1","model":"upstream","status":"in_progress"}}`),
		[]byte(`{"type":"response.output_text.delta","output_index":0,"delta":"ok"}`),
		[]byte(`{"type":"response.completed","response":{"id":"resp-1","model":"upstream","status":"completed"},"usage":{"input_tokens":4,"output_tokens":2,"total_tokens":6}}`),
	}
	var events []ir.StreamEvent
	for _, payload := range payloads {
		out, _, err := decoder.Decode(payload)
		require.NoError(t, err)
		events = append(events, out...)
	}
	require.Equal(t, []ir.StreamEventType{
		ir.EventStreamStart, ir.EventContentBlockStart, ir.EventTextDelta,
		ir.EventContentBlockEnd, ir.EventFinish, ir.EventUsage, ir.EventStreamEnd,
	}, streamEventTypes(events))
	require.NotNil(t, events[5].Usage)
	require.Equal(t, 4, events[5].Usage.InputTokens)
	require.NoError(t, func() error { _, _, err := decoder.Finalize(); return err }())
}

func TestStreamDecoderSynthesizesSparseToolLifecycle(t *testing.T) {
	decoder := newStreamDecoder()
	payloads := [][]byte{
		[]byte(`{"type":"response.function_call_arguments.delta","output_index":1,"call_id":"call-1","name":"lookup","delta":"{\"q\":"}`),
		[]byte(`{"type":"response.function_call_arguments.delta","output_index":1,"call_id":"call-1","delta":"\"x\"}"}`),
		[]byte(`{"type":"response.completed","response":{"id":"resp-1","model":"upstream","status":"completed"}}`),
	}
	var events []ir.StreamEvent
	for _, payload := range payloads {
		out, _, err := decoder.Decode(payload)
		require.NoError(t, err)
		events = append(events, out...)
	}
	require.Equal(t, []ir.StreamEventType{
		ir.EventStreamStart, ir.EventContentBlockStart, ir.EventToolCallStart,
		ir.EventToolCallDelta, ir.EventToolCallDelta, ir.EventToolCallEnd,
		ir.EventContentBlockEnd, ir.EventFinish, ir.EventStreamEnd,
	}, streamEventTypes(events))
	require.Equal(t, "lookup", events[2].ToolName)
	require.Equal(t, "call-1", events[5].ToolCallID)
}

func streamEventTypes(events []ir.StreamEvent) []ir.StreamEventType {
	types := make([]ir.StreamEventType, 0, len(events))
	for _, event := range events {
		types = append(types, event.Type)
	}
	return types
}

func TestStreamDecoderRejectsDuplicateCreated(t *testing.T) {
	decoder := newStreamDecoder()
	payload := []byte(`{"type":"response.created","response":{"id":"resp-1","object":"response","model":"model","status":"in_progress","output":[]}}`)
	_, _, err := decoder.Decode(payload)
	require.NoError(t, err)
	_, _, err = decoder.Decode(payload)
	require.ErrorContains(t, err, "duplicate response.created")
}

package openairesponses

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv/ir"
	"github.com/stretchr/testify/require"
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

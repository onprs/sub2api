package openairesponses

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv"
	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv/ir"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestStreamDecoderTreatsCancelledResponsesAsTerminal(t *testing.T) {
	for _, eventType := range []string{"response.cancelled", "response.canceled"} {
		t.Run(eventType, func(t *testing.T) {
			decoder := newStreamDecoder()
			events, _, err := decoder.Decode([]byte(`{"type":"` + eventType + `","response":{"id":"resp-cancelled","object":"response","model":"model","status":"cancelled","output":[]}}`))
			require.NoError(t, err)
			require.Equal(t, []ir.StreamEventType{ir.EventStreamStart, ir.EventFinish, ir.EventStreamEnd}, streamEventTypes(events))
			require.NoError(t, func() error { _, _, err := decoder.Finalize(); return err }())
		})
	}
}

func TestStreamRoundTripPreservesTerminalServiceTier(t *testing.T) {
	decoder := newStreamDecoder()
	events, warnings, err := decoder.Decode([]byte(`{
		"type":"response.completed",
		"response":{"id":"resp-tier","model":"gpt-5.5","status":"completed","service_tier":"default","output":[]}
	}`))
	require.NoError(t, err)
	require.Empty(t, warnings)
	require.Equal(t, []ir.StreamEventType{ir.EventStreamStart, ir.EventFinish, ir.EventStreamEnd}, streamEventTypes(events))
	require.JSONEq(t, `"default"`, string(events[1].ProviderMetadata[openAIResponsesServiceTierMetadataKey]))

	encoder := newStreamEncoder()
	var payloads [][]byte
	for _, event := range events {
		out, encodeWarnings, encodeErr := encoder.Encode(event)
		require.NoError(t, encodeErr)
		require.Empty(t, encodeWarnings)
		payloads = append(payloads, out...)
	}
	require.Equal(t, "default", gjson.GetBytes(payloads[len(payloads)-1], "response.service_tier").String())
}

func TestStreamRoundTripPreservesCreatedAt(t *testing.T) {
	const createdAt = int64(1700000123)
	decoder := newStreamDecoder()
	events, warnings, err := decoder.Decode([]byte(`{
		"type":"response.completed",
		"response":{"id":"resp-created","created_at":1700000123,"model":"gpt-5.5","status":"completed","output":[]}
	}`))
	require.NoError(t, err)
	require.Empty(t, warnings)
	require.Equal(t, createdAt, events[0].Created)

	encoder := newStreamEncoder()
	var payloads [][]byte
	for _, event := range events {
		out, encodeWarnings, encodeErr := encoder.Encode(event)
		require.NoError(t, encodeErr)
		require.Empty(t, encodeWarnings)
		payloads = append(payloads, out...)
	}
	require.Equal(t, createdAt, gjson.GetBytes(payloads[0], "response.created_at").Int())
	require.Equal(t, createdAt, gjson.GetBytes(payloads[len(payloads)-1], "response.created_at").Int())
}

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
	require.Equal(t, []string{
		"response.created",
		"response.output_item.added",
		"response.reasoning_summary_part.added",
		"response.reasoning_summary_text.delta",
		"response.reasoning_summary_text.done",
		"response.reasoning_summary_part.done",
		"response.output_item.done",
	}, responseStreamPayloadTypes(payloads))
	last := payloads[len(payloads)-1]
	require.Equal(t, "sig-1", gjson.GetBytes(last, "item.encrypted_content").String())
	require.Equal(t, "plan", gjson.GetBytes(last, "item.summary.0.text").String())
}

func TestStreamEncoderEmitsCompleteTextLifecycle(t *testing.T) {
	encoder := newStreamEncoderWithOptions(protocolconv.Options{ResponseModel: "client-model"})
	sequence := []ir.StreamEvent{
		{Type: ir.EventStreamStart, ResponseID: "resp-text", Model: "upstream-model"},
		{Type: ir.EventContentBlockStart, BlockIndex: 0, BlockType: ir.ContentText},
		{Type: ir.EventTextDelta, BlockIndex: 0, Text: "hello"},
		{Type: ir.EventContentBlockEnd, BlockIndex: 0},
		{Type: ir.EventFinish, FinishReason: &ir.FinishReason{Reason: "stop"}},
		{Type: ir.EventUsage, Usage: &ir.Usage{InputTokens: 3, OutputTokens: 2, TotalTokens: 5}},
		{Type: ir.EventStreamEnd},
	}
	var payloads [][]byte
	for _, event := range sequence {
		out, warnings, err := encoder.Encode(event)
		require.NoError(t, err)
		require.Empty(t, warnings)
		payloads = append(payloads, out...)
	}

	require.Equal(t, []string{
		"response.created",
		"response.output_item.added",
		"response.content_part.added",
		"response.output_text.delta",
		"response.output_text.done",
		"response.content_part.done",
		"response.output_item.done",
		"response.completed",
	}, responseStreamPayloadTypes(payloads))

	created := payloads[0]
	require.Equal(t, "client-model", gjson.GetBytes(created, "response.model").String())
	require.Greater(t, gjson.GetBytes(created, "response.created_at").Int(), int64(0))
	require.Equal(t,
		gjson.GetBytes(created, "response.created_at").Int(),
		gjson.GetBytes(payloads[len(payloads)-1], "response.created_at").Int(),
	)
	require.True(t, gjson.GetBytes(created, "response.output").IsArray())
	require.Equal(t, int64(0), gjson.GetBytes(payloads[1], "output_index").Int())
	require.Equal(t, "item_0", gjson.GetBytes(payloads[2], "item_id").String())
	require.Equal(t, int64(0), gjson.GetBytes(payloads[2], "content_index").Int())
	require.Equal(t, "hello", gjson.GetBytes(payloads[3], "delta").String())
	require.Equal(t, "hello", gjson.GetBytes(payloads[4], "text").String())
	require.Equal(t, "hello", gjson.GetBytes(payloads[5], "part.text").String())
	require.Equal(t, "hello", gjson.GetBytes(payloads[6], "item.content.0.text").String())
	require.Equal(t, "hello", gjson.GetBytes(payloads[7], "response.output.0.content.0.text").String())
	require.Equal(t, int64(3), gjson.GetBytes(payloads[7], "response.usage.input_tokens").Int())
	require.Equal(t, int64(2), gjson.GetBytes(payloads[7], "response.usage.output_tokens").Int())
}

func responseStreamPayloadTypes(payloads [][]byte) []string {
	types := make([]string, 0, len(payloads))
	for _, payload := range payloads {
		types = append(types, gjson.GetBytes(payload, "type").String())
	}
	return types
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

func TestStreamDecoderRecoversTextFromDoneOnlyLifecycle(t *testing.T) {
	decoder := newStreamDecoder()
	payloads := [][]byte{
		[]byte(`{"type":"response.created","response":{"id":"resp-1","model":"upstream","status":"in_progress"}}`),
		[]byte(`{"type":"response.output_item.added","output_index":0,"item":{"type":"message","id":"msg-1","role":"assistant"}}`),
		[]byte(`{"type":"response.output_text.done","output_index":0,"item_id":"msg-1","content_index":0,"text":"done-only text"}`),
		[]byte(`{"type":"response.output_item.done","output_index":0,"item":{"type":"message","id":"msg-1","role":"assistant","content":[{"type":"output_text","text":"done-only text"}]}}`),
	}
	var events []ir.StreamEvent
	for _, payload := range payloads {
		out, _, err := decoder.Decode(payload)
		require.NoError(t, err)
		events = append(events, out...)
	}
	require.Equal(t, []ir.StreamEventType{
		ir.EventStreamStart, ir.EventContentBlockStart, ir.EventTextDelta, ir.EventContentBlockEnd,
	}, streamEventTypes(events))
	require.Equal(t, "done-only text", events[2].Text)
}

func TestStreamDecoderRecoversReasoningAndToolArgumentsFromDoneOnlyLifecycle(t *testing.T) {
	decoder := newStreamDecoder()
	payloads := [][]byte{
		[]byte(`{"type":"response.output_item.added","output_index":0,"item":{"type":"reasoning","id":"reasoning-1"}}`),
		[]byte(`{"type":"response.reasoning_summary_text.done","output_index":0,"item_id":"reasoning-1","summary_index":0,"text":"reasoning"}`),
		[]byte(`{"type":"response.output_item.done","output_index":0,"item":{"type":"reasoning","id":"reasoning-1","summary":[{"type":"summary_text","text":"reasoning"}]}}`),
		[]byte(`{"type":"response.output_item.added","output_index":1,"item":{"type":"function_call","id":"tool-1","call_id":"call-1","name":"lookup"}}`),
		[]byte(`{"type":"response.function_call_arguments.done","output_index":1,"item_id":"tool-1","call_id":"call-1","name":"lookup","arguments":"{\"q\":\"x\"}"}`),
		[]byte(`{"type":"response.output_item.done","output_index":1,"item":{"type":"function_call","id":"tool-1","call_id":"call-1","name":"lookup","arguments":"{\"q\":\"x\"}"}}`),
	}
	var events []ir.StreamEvent
	for _, payload := range payloads {
		out, _, err := decoder.Decode(payload)
		require.NoError(t, err)
		events = append(events, out...)
	}
	require.Equal(t, []ir.StreamEventType{
		ir.EventStreamStart, ir.EventContentBlockStart, ir.EventReasoningDelta, ir.EventContentBlockEnd,
		ir.EventContentBlockStart, ir.EventToolCallStart, ir.EventToolCallDelta, ir.EventToolCallEnd, ir.EventContentBlockEnd,
	}, streamEventTypes(events))
	require.Equal(t, "reasoning", events[2].Reasoning)
	require.Equal(t, `{"q":"x"}`, events[6].ArgumentsDelta)
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

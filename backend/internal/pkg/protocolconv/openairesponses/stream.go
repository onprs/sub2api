package openairesponses

import (
	"encoding/json"
	"fmt"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv"
	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv/ir"
)

type streamDecoder struct {
	started bool
	ended   bool
	model   string
	id      string
	blocks  map[int]ir.ContentType
	calls   map[int]struct{ id, name string }
}

type streamEncoder struct {
	sequence int
	id       string
	model    string
	response *apicompat.ResponsesResponse
	blocks   map[int]ir.ContentType
}

func newStreamDecoder() *streamDecoder {
	return &streamDecoder{blocks: map[int]ir.ContentType{}, calls: map[int]struct{ id, name string }{}}
}
func newStreamEncoder() *streamEncoder { return &streamEncoder{blocks: map[int]ir.ContentType{}} }

func (d *streamDecoder) Decode(chunk []byte) ([]ir.StreamEvent, []protocolconv.Warning, error) {
	var event apicompat.ResponsesStreamEvent
	if err := json.Unmarshal(chunk, &event); err != nil {
		return nil, nil, &protocolconv.Error{Code: protocolconv.ErrorInvalidJSON, Protocol: protocolconv.ProtocolOpenAIResponses, Cause: err}
	}
	var out []ir.StreamEvent
	start := func(response *apicompat.ResponsesResponse) {
		if d.started {
			return
		}
		d.started = true
		if response != nil {
			d.id = response.ID
			d.model = response.Model
		}
		out = append(out, ir.StreamEvent{Type: ir.EventStreamStart, ResponseID: d.id, Model: d.model})
	}
	switch event.Type {
	case "response.created", "response.in_progress":
		if d.started {
			return nil, nil, &protocolconv.Error{Code: protocolconv.ErrorInvalidStream, Protocol: protocolconv.ProtocolOpenAIResponses, Message: "duplicate response.created"}
		}
		start(event.Response)
	case "response.output_item.added":
		start(nil)
		if event.Item == nil {
			break
		}
		index := event.OutputIndex
		switch event.Item.Type {
		case "message":
			d.blocks[index] = ir.ContentText
			out = append(out, ir.StreamEvent{Type: ir.EventContentBlockStart, BlockIndex: index, BlockType: ir.ContentText})
		case "reasoning":
			d.blocks[index] = ir.ContentReasoning
			out = append(out, ir.StreamEvent{Type: ir.EventContentBlockStart, BlockIndex: index, BlockType: ir.ContentReasoning})
		case "function_call":
			d.blocks[index] = ir.ContentToolCall
			d.calls[index] = struct{ id, name string }{event.Item.CallID, event.Item.Name}
			out = append(out, ir.StreamEvent{Type: ir.EventContentBlockStart, BlockIndex: index, BlockType: ir.ContentToolCall}, ir.StreamEvent{Type: ir.EventToolCallStart, BlockIndex: index, ToolCallIndex: index, ToolCallID: event.Item.CallID, ToolName: event.Item.Name})
		}
	case "response.output_text.delta":
		start(nil)
		out = append(out, ir.StreamEvent{Type: ir.EventTextDelta, BlockIndex: event.OutputIndex, ChoiceIndex: 0, Text: event.Delta})
	case "response.reasoning_summary_text.delta", "response.reasoning_text.delta":
		start(nil)
		out = append(out, ir.StreamEvent{Type: ir.EventReasoningDelta, BlockIndex: event.OutputIndex, ChoiceIndex: 0, Reasoning: event.Delta})
	case "response.function_call_arguments.delta":
		start(nil)
		call := d.calls[event.OutputIndex]
		id := event.CallID
		if id == "" {
			id = call.id
		}
		out = append(out, ir.StreamEvent{Type: ir.EventToolCallDelta, BlockIndex: event.OutputIndex, ToolCallIndex: event.OutputIndex, ToolCallID: id, ArgumentsDelta: event.Delta})
	case "response.output_item.done":
		if _, ok := d.blocks[event.OutputIndex]; ok {
			if d.blocks[event.OutputIndex] == ir.ContentToolCall {
				call := d.calls[event.OutputIndex]
				out = append(out, ir.StreamEvent{Type: ir.EventToolCallEnd, BlockIndex: event.OutputIndex, ToolCallID: call.id})
			}
			out = append(out, ir.StreamEvent{Type: ir.EventContentBlockEnd, BlockIndex: event.OutputIndex})
			delete(d.blocks, event.OutputIndex)
		}
	case "response.completed", "response.done", "response.incomplete", "response.failed":
		start(event.Response)
		response := event.Response
		reason := ir.FinishReason{Reason: "stop"}
		var usage *ir.Usage
		if response != nil {
			reason.Reason = responsesFinishReason(response)
			usage = decodeUsage(response.Usage)
		}
		out = append(out, ir.StreamEvent{Type: ir.EventFinish, FinishReason: &reason})
		if usage != nil {
			out = append(out, ir.StreamEvent{Type: ir.EventUsage, Usage: usage})
		}
		out = append(out, ir.StreamEvent{Type: ir.EventStreamEnd})
		d.ended = true
	case "error":
		start(nil)
		out = append(out, ir.StreamEvent{Type: ir.EventError, Error: &ir.ErrorInfo{Code: event.Code, Message: event.Delta, Param: event.Param}})
	}
	return out, nil, nil
}
func (d *streamDecoder) Finalize() ([]ir.StreamEvent, []protocolconv.Warning, error) {
	if !d.ended {
		return nil, nil, &protocolconv.Error{Code: protocolconv.ErrorInvalidStream, Protocol: protocolconv.ProtocolOpenAIResponses, Message: "Responses stream ended without terminal event"}
	}
	return nil, nil, nil
}

func (e *streamEncoder) Encode(event ir.StreamEvent) ([][]byte, []protocolconv.Warning, error) {
	makeEvent := func(kind string) *apicompat.ResponsesStreamEvent {
		e.sequence++
		return &apicompat.ResponsesStreamEvent{Type: kind, SequenceNumber: e.sequence}
	}
	var events []*apicompat.ResponsesStreamEvent
	switch event.Type {
	case ir.EventStreamStart:
		e.id = event.ResponseID
		e.model = event.Model
		e.response = &apicompat.ResponsesResponse{ID: e.id, Object: "response", Model: e.model, Status: "in_progress"}
		x := makeEvent("response.created")
		x.Response = e.response
		events = append(events, x)
	case ir.EventContentBlockStart:
		e.blocks[event.BlockIndex] = event.BlockType
		item := &apicompat.ResponsesOutput{ID: fmt.Sprintf("item_%d", event.BlockIndex), Status: "in_progress"}
		switch event.BlockType {
		case ir.ContentText:
			item.Type = "message"
			item.Role = "assistant"
		case ir.ContentReasoning:
			item.Type = "reasoning"
		case ir.ContentToolCall:
			return nil, nil, nil
		}
		x := makeEvent("response.output_item.added")
		x.OutputIndex = event.BlockIndex
		x.Item = item
		events = append(events, x)
	case ir.EventToolCallStart:
		item := &apicompat.ResponsesOutput{Type: "function_call", ID: fmt.Sprintf("item_%d", event.BlockIndex), CallID: event.ToolCallID, Name: event.ToolName, Status: "in_progress"}
		x := makeEvent("response.output_item.added")
		x.OutputIndex = event.BlockIndex
		x.Item = item
		events = append(events, x)
	case ir.EventTextDelta:
		x := makeEvent("response.output_text.delta")
		x.OutputIndex = event.BlockIndex
		x.Delta = event.Text
		events = append(events, x)
	case ir.EventReasoningDelta:
		x := makeEvent("response.reasoning_summary_text.delta")
		x.OutputIndex = event.BlockIndex
		x.Delta = event.Reasoning
		events = append(events, x)
	case ir.EventToolCallDelta:
		x := makeEvent("response.function_call_arguments.delta")
		x.OutputIndex = event.BlockIndex
		x.CallID = event.ToolCallID
		x.Delta = event.ArgumentsDelta
		events = append(events, x)
	case ir.EventContentBlockEnd:
		x := makeEvent("response.output_item.done")
		x.OutputIndex = event.BlockIndex
		events = append(events, x)
	case ir.EventFinish:
		if e.response == nil {
			e.response = &apicompat.ResponsesResponse{ID: e.id, Object: "response", Model: e.model}
		}
		e.response.Status = "completed"
		if event.FinishReason != nil && event.FinishReason.Reason == "length" {
			e.response.Status = "incomplete"
			e.response.IncompleteDetails = &apicompat.ResponsesIncompleteDetails{Reason: "max_output_tokens"}
		}
	case ir.EventUsage:
		if e.response != nil {
			e.response.Usage = encodeUsage(event.Usage)
		}
	case ir.EventError:
		x := makeEvent("error")
		if event.Error != nil {
			x.Code = event.Error.Code
			x.Delta = event.Error.Message
			x.Param = event.Error.Param
		}
		events = append(events, x)
	case ir.EventStreamEnd:
		if e.response == nil {
			e.response = &apicompat.ResponsesResponse{ID: e.id, Object: "response", Model: e.model, Status: "completed"}
		}
		x := makeEvent("response.completed")
		x.Response = e.response
		events = append(events, x)
	}
	out := make([][]byte, 0, len(events))
	for _, event := range events {
		body, err := json.Marshal(event)
		if err != nil {
			return nil, nil, err
		}
		out = append(out, body)
	}
	return out, nil, nil
}
func (e *streamEncoder) Finalize() ([][]byte, []protocolconv.Warning, error) { return nil, nil, nil }

// NewStreamDecoder creates isolated Responses source state for adapters.
func NewStreamDecoder() protocolconv.StreamDecoder { return newStreamDecoder() }

// NewStreamEncoder creates isolated Responses target state for adapters.
func NewStreamEncoder() protocolconv.StreamEncoder { return newStreamEncoder() }

func (*Converter) NewStreamDecoder() protocolconv.StreamDecoder { return NewStreamDecoder() }
func (*Converter) NewStreamEncoder() protocolconv.StreamEncoder { return NewStreamEncoder() }

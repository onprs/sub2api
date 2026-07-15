package openairesponses

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv"
	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv/ir"
)

type streamDecoder struct {
	started    bool
	ended      bool
	model      string
	id         string
	blocks     map[int]ir.ContentType
	calls      map[int]struct{ id, name string }
	signatures map[int]string
}

type streamEncoder struct {
	sequence int
	id       string
	model    string
	response *apicompat.ResponsesResponse
	blocks   map[int]ir.ContentType
	items    map[int]*apicompat.ResponsesOutput
	tools    map[int]*streamToolState
	options  protocolconv.Options
}

type streamToolState struct {
	kind      string
	name      string
	namespace string
	callID    string
	arguments string
}

func newStreamDecoder() *streamDecoder {
	return &streamDecoder{
		blocks:     map[int]ir.ContentType{},
		calls:      map[int]struct{ id, name string }{},
		signatures: map[int]string{},
	}
}
func newStreamEncoder() *streamEncoder {
	return newStreamEncoderWithOptions(protocolconv.Options{})
}

func newStreamEncoderWithOptions(options protocolconv.Options) *streamEncoder {
	return &streamEncoder{
		blocks:  map[int]ir.ContentType{},
		items:   map[int]*apicompat.ResponsesOutput{},
		tools:   map[int]*streamToolState{},
		options: options,
	}
}

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
	ensureBlock := func(index int, blockType ir.ContentType) {
		if _, exists := d.blocks[index]; exists {
			return
		}
		d.blocks[index] = blockType
		out = append(out, ir.StreamEvent{Type: ir.EventContentBlockStart, BlockIndex: index, BlockType: blockType})
	}
	closeBlocks := func() {
		indices := make([]int, 0, len(d.blocks))
		for index := range d.blocks {
			indices = append(indices, index)
		}
		sort.Ints(indices)
		for _, index := range indices {
			if d.blocks[index] == ir.ContentToolCall {
				call := d.calls[index]
				out = append(out, ir.StreamEvent{Type: ir.EventToolCallEnd, BlockIndex: index, ToolCallIndex: index, ToolCallID: call.id})
			}
			if d.blocks[index] == ir.ContentReasoning && d.signatures[index] != "" {
				out = append(out, ir.StreamEvent{Type: ir.EventReasoningDelta, BlockIndex: index, Signature: d.signatures[index]})
				delete(d.signatures, index)
			}
			out = append(out, ir.StreamEvent{Type: ir.EventContentBlockEnd, BlockIndex: index})
			delete(d.blocks, index)
		}
	}
	switch event.Type {
	case "response.created":
		if d.started {
			return nil, nil, &protocolconv.Error{Code: protocolconv.ErrorInvalidStream, Protocol: protocolconv.ProtocolOpenAIResponses, Message: "duplicate response.created"}
		}
		start(event.Response)
	case "response.in_progress":
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
			d.signatures[index] = event.Item.EncryptedContent
			out = append(out, ir.StreamEvent{Type: ir.EventContentBlockStart, BlockIndex: index, BlockType: ir.ContentReasoning})
		case "function_call":
			d.blocks[index] = ir.ContentToolCall
			d.calls[index] = struct{ id, name string }{event.Item.CallID, event.Item.Name}
			out = append(out, ir.StreamEvent{Type: ir.EventContentBlockStart, BlockIndex: index, BlockType: ir.ContentToolCall}, ir.StreamEvent{Type: ir.EventToolCallStart, BlockIndex: index, ToolCallIndex: index, ToolCallID: event.Item.CallID, ToolName: event.Item.Name})
		}
	case "response.output_text.delta":
		start(nil)
		ensureBlock(event.OutputIndex, ir.ContentText)
		out = append(out, ir.StreamEvent{Type: ir.EventTextDelta, BlockIndex: event.OutputIndex, ChoiceIndex: 0, Text: event.Delta})
	case "response.reasoning_summary_text.delta", "response.reasoning_text.delta":
		start(nil)
		ensureBlock(event.OutputIndex, ir.ContentReasoning)
		out = append(out, ir.StreamEvent{Type: ir.EventReasoningDelta, BlockIndex: event.OutputIndex, ChoiceIndex: 0, Reasoning: event.Delta})
	case "response.function_call_arguments.delta":
		start(nil)
		if _, exists := d.blocks[event.OutputIndex]; !exists {
			d.blocks[event.OutputIndex] = ir.ContentToolCall
			d.calls[event.OutputIndex] = struct{ id, name string }{event.CallID, event.Name}
			out = append(out,
				ir.StreamEvent{Type: ir.EventContentBlockStart, BlockIndex: event.OutputIndex, BlockType: ir.ContentToolCall},
				ir.StreamEvent{Type: ir.EventToolCallStart, BlockIndex: event.OutputIndex, ToolCallIndex: event.OutputIndex, ToolCallID: event.CallID, ToolName: event.Name},
			)
		}
		call := d.calls[event.OutputIndex]
		id := event.CallID
		if id == "" {
			id = call.id
		}
		out = append(out, ir.StreamEvent{Type: ir.EventToolCallDelta, BlockIndex: event.OutputIndex, ToolCallIndex: event.OutputIndex, ToolCallID: id, ArgumentsDelta: event.Delta})
	case "response.output_item.done":
		if blockType, ok := d.blocks[event.OutputIndex]; ok {
			if blockType == ir.ContentToolCall {
				call := d.calls[event.OutputIndex]
				out = append(out, ir.StreamEvent{Type: ir.EventToolCallEnd, BlockIndex: event.OutputIndex, ToolCallID: call.id})
			}
			if blockType == ir.ContentReasoning {
				signature := d.signatures[event.OutputIndex]
				if event.Item != nil && event.Item.EncryptedContent != "" {
					signature = event.Item.EncryptedContent
				}
				if signature != "" {
					out = append(out, ir.StreamEvent{Type: ir.EventReasoningDelta, BlockIndex: event.OutputIndex, Signature: signature})
				}
				delete(d.signatures, event.OutputIndex)
			}
			out = append(out, ir.StreamEvent{Type: ir.EventContentBlockEnd, BlockIndex: event.OutputIndex})
			delete(d.blocks, event.OutputIndex)
		}
	case "response.completed", "response.done", "response.incomplete", "response.failed":
		start(event.Response)
		closeBlocks()
		response := event.Response
		reason := ir.FinishReason{Reason: "stop"}
		var usage *ir.Usage
		if response != nil {
			reason.Reason = responsesFinishReason(response)
			usage = decodeUsage(response.Usage)
		}
		if usage == nil {
			usage = decodeUsage(event.Usage)
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
		if e.options.ResponseModel != "" {
			e.model = e.options.ResponseModel
		}
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
		e.items[event.BlockIndex] = item
		x := makeEvent("response.output_item.added")
		x.OutputIndex = event.BlockIndex
		x.Item = item
		events = append(events, x)
	case ir.EventToolCallStart:
		kind, name, namespace := event.ToolKind, event.ToolName, event.ToolNamespace
		if route, ok := e.options.ToolRoutes[event.ToolName]; ok {
			kind, name, namespace = route.SourceKind, route.SourceName, route.Namespace
		}
		if kind == "" {
			kind = "function_call"
		}
		e.tools[event.BlockIndex] = &streamToolState{kind: kind, name: name, namespace: namespace, callID: event.ToolCallID}
		item := &apicompat.ResponsesOutput{Type: kind, ID: fmt.Sprintf("item_%d", event.BlockIndex), CallID: event.ToolCallID, Name: name, Namespace: namespace, Status: "in_progress"}
		e.items[event.BlockIndex] = item
		x := makeEvent("response.output_item.added")
		x.OutputIndex = event.BlockIndex
		x.Item = item
		events = append(events, x)
	case ir.EventTextDelta:
		if item := e.items[event.BlockIndex]; item != nil {
			if len(item.Content) == 0 {
				item.Content = []apicompat.ResponsesContentPart{{Type: "output_text"}}
			}
			item.Content[0].Text += event.Text
		}
		x := makeEvent("response.output_text.delta")
		x.OutputIndex = event.BlockIndex
		x.Delta = event.Text
		events = append(events, x)
	case ir.EventReasoningDelta:
		if item := e.items[event.BlockIndex]; item != nil {
			if event.Reasoning != "" {
				if len(item.Summary) == 0 {
					item.Summary = []apicompat.ResponsesSummary{{Type: "summary_text"}}
				}
				item.Summary[0].Text += event.Reasoning
			}
			item.EncryptedContent += event.Signature
		}
		if event.Reasoning != "" {
			x := makeEvent("response.reasoning_summary_text.delta")
			x.OutputIndex = event.BlockIndex
			x.Delta = event.Reasoning
			events = append(events, x)
		}
	case ir.EventToolCallDelta:
		tool := e.tools[event.BlockIndex]
		if tool != nil {
			tool.arguments += event.ArgumentsDelta
		}
		if tool == nil || tool.kind == "function_call" {
			x := makeEvent("response.function_call_arguments.delta")
			x.OutputIndex = event.BlockIndex
			x.CallID = event.ToolCallID
			x.Delta = event.ArgumentsDelta
			events = append(events, x)
		}
	case ir.EventToolCallEnd:
		tool := e.tools[event.BlockIndex]
		if tool == nil {
			break
		}
		switch tool.kind {
		case "function_call":
			done := makeEvent("response.function_call_arguments.done")
			done.OutputIndex = event.BlockIndex
			done.CallID = tool.callID
			done.Name = tool.name
			done.Arguments = tool.arguments
			events = append(events, done)
		case "custom_tool_call":
			input := customToolStreamInput(tool.arguments)
			if input != "" {
				delta := makeEvent("response.custom_tool_call_input.delta")
				delta.OutputIndex = event.BlockIndex
				delta.CallID = tool.callID
				delta.Name = tool.name
				delta.Delta = input
				events = append(events, delta)
			}
			done := makeEvent("response.custom_tool_call_input.done")
			done.OutputIndex = event.BlockIndex
			done.CallID = tool.callID
			done.Name = tool.name
			done.Input = input
			events = append(events, done)
		}
	case ir.EventContentBlockEnd:
		x := makeEvent("response.output_item.done")
		x.OutputIndex = event.BlockIndex
		item := e.items[event.BlockIndex]
		if tool := e.tools[event.BlockIndex]; tool != nil {
			item = completedStreamToolItem(event.BlockIndex, tool)
			delete(e.tools, event.BlockIndex)
		} else if item != nil {
			item.Status = "completed"
		}
		x.Item = item
		if item != nil && e.response != nil {
			e.response.Output = append(e.response.Output, *item)
		}
		delete(e.items, event.BlockIndex)
		delete(e.blocks, event.BlockIndex)
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

func customToolStreamInput(arguments string) string {
	var value struct {
		Input *string `json:"input"`
	}
	if json.Unmarshal([]byte(arguments), &value) == nil && value.Input != nil {
		return *value.Input
	}
	return arguments
}

func completedStreamToolItem(index int, tool *streamToolState) *apicompat.ResponsesOutput {
	item := &apicompat.ResponsesOutput{
		Type:      tool.kind,
		ID:        fmt.Sprintf("item_%d", index),
		CallID:    tool.callID,
		Name:      tool.name,
		Namespace: tool.namespace,
		Status:    "completed",
	}
	switch tool.kind {
	case "custom_tool_call":
		item.Input = customToolStreamInput(tool.arguments)
	default:
		item.Arguments = tool.arguments
	}
	return item
}

// NewStreamDecoder creates isolated Responses source state for adapters.
func NewStreamDecoder() protocolconv.StreamDecoder { return newStreamDecoder() }

// NewStreamEncoder creates isolated Responses target state for adapters.
func NewStreamEncoder() protocolconv.StreamEncoder { return newStreamEncoder() }

// NewStreamEncoderWithOptions creates target state with request-scoped route metadata.
func NewStreamEncoderWithOptions(options protocolconv.Options) protocolconv.StreamEncoder {
	return newStreamEncoderWithOptions(options)
}

func (*Converter) NewStreamDecoder() protocolconv.StreamDecoder { return NewStreamDecoder() }
func (*Converter) NewStreamEncoder() protocolconv.StreamEncoder { return NewStreamEncoder() }
func (*Converter) NewStreamDecoderWithOptions(protocolconv.Options) protocolconv.StreamDecoder {
	return NewStreamDecoder()
}
func (*Converter) NewStreamEncoderWithOptions(options protocolconv.Options) protocolconv.StreamEncoder {
	return NewStreamEncoderWithOptions(options)
}

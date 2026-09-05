package googlegenai

import (
	"encoding/json"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv"
	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv/ir"
)

type streamDecoder struct {
	started     bool
	ended       bool
	id          string
	model       string
	nextBlock   int
	nextPart    map[int]int
	usage       *ir.Usage
	current     *googleBlock
	sawToolCall bool
}

type googleBlock struct {
	index       int
	partType    ir.ContentType
	choiceIndex int
	toolIndex   int
	toolCallID  string
	toolName    string
	signature   string
	seen        string
}

type streamEncoder struct {
	id             string
	model          string
	usage          *ir.Usage
	finishReason   ir.FinishReason
	calls          map[string]*functionCallWire
	callArgs       map[string]string
	callSignatures map[string]string
}

func newStreamDecoder() *streamDecoder {
	return &streamDecoder{nextPart: make(map[int]int)}
}
func newStreamEncoder() *streamEncoder {
	return newStreamEncoderWithOptions(protocolconv.Options{})
}
func newStreamEncoderWithOptions(options protocolconv.Options) *streamEncoder {
	return &streamEncoder{
		model:          options.ResponseModel,
		calls:          make(map[string]*functionCallWire),
		callArgs:       make(map[string]string),
		callSignatures: make(map[string]string),
	}
}

func (d *streamDecoder) Decode(chunk []byte) ([]ir.StreamEvent, []protocolconv.Warning, error) {
	var wire responseWire
	if err := json.Unmarshal(chunk, &wire); err != nil {
		return nil, nil, &protocolconv.Error{Code: protocolconv.ErrorInvalidJSON, Protocol: protocolconv.ProtocolGoogleGenAI, Cause: err}
	}
	if d.ended {
		return nil, nil, &protocolconv.Error{Code: protocolconv.ErrorInvalidStream, Protocol: protocolconv.ProtocolGoogleGenAI, Message: "chunk received after terminal finishReason"}
	}

	var out []ir.StreamEvent
	if !d.started {
		d.started = true
		d.id = wire.ResponseID
		d.model = wire.ModelVersion
		out = append(out, ir.StreamEvent{Type: ir.EventStreamStart, ResponseID: d.id, Model: d.model})
	}

	if wire.UsageMetadata != nil {
		d.usage = usageFromGoogle(wire.UsageMetadata)
	}
	finished := false
	finishReason := ir.FinishReason{Reason: "stop"}
	for candidatePosition, candidate := range wire.Candidates {
		candidateIndex := googleCandidateIndex(candidatePosition, candidate)
		for partIndex, part := range candidate.Content.Parts {
			// 分片中的 parts 索引会重置，缺失 ID 必须使用请求内持续递增的索引。
			part = ensureGoogleFunctionCallID(part, candidateIndex, d.nextPart[candidateIndex])
			d.nextPart[candidateIndex]++
			partType := googlePartType(part)
			identity := googlePartIdentity(part)
			if d.current == nil || d.current.choiceIndex != candidateIndex || d.current.partType != partType || (partType == ir.ContentToolCall && d.current.toolCallID != identity) {
				out = append(out, d.closeCurrent()...)
				d.current = &googleBlock{index: d.nextBlock, partType: partType, choiceIndex: candidateIndex, toolIndex: partIndex}
				d.nextBlock++
				out = append(out, ir.StreamEvent{Type: ir.EventContentBlockStart, BlockIndex: d.current.index, BlockType: partType, ChoiceIndex: candidateIndex})
				if part.FunctionCall != nil {
					d.current.toolCallID = part.FunctionCall.ID
					d.current.toolName = part.FunctionCall.Name
					d.current.signature = part.ThoughtSignature
					out = append(out, ir.StreamEvent{Type: ir.EventToolCallStart, BlockIndex: d.current.index, ChoiceIndex: candidateIndex, ToolCallIndex: partIndex, ToolCallID: part.FunctionCall.ID, ToolName: part.FunctionCall.Name, Signature: part.ThoughtSignature})
				}
			}

			switch {
			case part.FunctionCall != nil:
				d.sawToolCall = true
				if part.ThoughtSignature != "" {
					d.current.signature = part.ThoughtSignature
				}
				args := string(part.FunctionCall.Args)
				if args == "" {
					args = "{}"
				}
				delta := cumulativeDelta(d.current.seen, args)
				if delta != "" {
					out = append(out, ir.StreamEvent{Type: ir.EventToolCallDelta, BlockIndex: d.current.index, ChoiceIndex: candidateIndex, ToolCallIndex: partIndex, ToolCallID: d.current.toolCallID, ArgumentsDelta: delta})
					d.current.seen = args
				}
			case part.Thought:
				if part.Text != "" || part.ThoughtSignature != "" {
					out = append(out, ir.StreamEvent{Type: ir.EventReasoningDelta, BlockIndex: d.current.index, ChoiceIndex: candidateIndex, Reasoning: part.Text, Signature: part.ThoughtSignature})
				}
			default:
				// 标准 Google 文本分片是增量，重复内容和共同前缀都属于输出。
				if part.Text != "" {
					out = append(out, ir.StreamEvent{Type: ir.EventTextDelta, BlockIndex: d.current.index, ChoiceIndex: candidateIndex, Text: part.Text})
				}
			}
		}
		if candidate.FinishReason != "" {
			finished = true
			finishReason = finishFromGoogle(candidate.FinishReason)
		}
	}

	if finished {
		if d.sawToolCall {
			finishReason.Reason = "tool_calls"
		}
		out = append(out, d.closeCurrent()...)
		out = append(out, ir.StreamEvent{Type: ir.EventFinish, FinishReason: &finishReason})
		if d.usage != nil {
			out = append(out, ir.StreamEvent{Type: ir.EventUsage, Usage: d.usage})
		}
		out = append(out, ir.StreamEvent{Type: ir.EventStreamEnd})
		d.ended = true
	}
	return out, nil, nil
}

func (d *streamDecoder) closeCurrent() []ir.StreamEvent {
	if d.current == nil {
		return nil
	}
	block := d.current
	d.current = nil
	out := make([]ir.StreamEvent, 0, 2)
	if block.partType == ir.ContentToolCall {
		out = append(out, ir.StreamEvent{Type: ir.EventToolCallEnd, BlockIndex: block.index, ChoiceIndex: block.choiceIndex, ToolCallIndex: block.toolIndex, ToolCallID: block.toolCallID, Signature: block.signature})
	}
	return append(out, ir.StreamEvent{Type: ir.EventContentBlockEnd, BlockIndex: block.index, ChoiceIndex: block.choiceIndex})
}

func (d *streamDecoder) Finalize() ([]ir.StreamEvent, []protocolconv.Warning, error) {
	if !d.ended {
		return nil, nil, &protocolconv.Error{Code: protocolconv.ErrorInvalidStream, Protocol: protocolconv.ProtocolGoogleGenAI, Message: "Google stream ended without finishReason"}
	}
	return nil, nil, nil
}

func (e *streamEncoder) Encode(event ir.StreamEvent) ([][]byte, []protocolconv.Warning, error) {
	wire := responseWire{ResponseID: e.id, ModelVersion: e.model}
	emit := false
	switch event.Type {
	case ir.EventStreamStart:
		e.id = event.ResponseID
		if e.model == "" {
			e.model = event.Model
		}
	case ir.EventTextDelta:
		wire.Candidates = []candidateWire{{Index: event.ChoiceIndex, Content: contentWire{Role: "model", Parts: []partWire{{Text: event.Text}}}}}
		emit = true
	case ir.EventReasoningDelta:
		wire.Candidates = []candidateWire{{Index: event.ChoiceIndex, Content: contentWire{Role: "model", Parts: []partWire{{Text: event.Reasoning, Thought: true, ThoughtSignature: event.Signature}}}}}
		emit = true
	case ir.EventToolCallStart:
		e.calls[event.ToolCallID] = &functionCallWire{ID: event.ToolCallID, Name: event.ToolName}
		if event.Signature != "" {
			e.callSignatures[event.ToolCallID] = event.Signature
		}
	case ir.EventToolCallDelta:
		e.callArgs[event.ToolCallID] += event.ArgumentsDelta
	case ir.EventToolCallEnd:
		if event.Signature != "" {
			e.callSignatures[event.ToolCallID] = event.Signature
		}
		call := e.calls[event.ToolCallID]
		if call == nil {
			return nil, nil, &protocolconv.Error{Code: protocolconv.ErrorInvalidStream, Protocol: protocolconv.ProtocolGoogleGenAI, Message: "tool call ended before start"}
		}
		args := []byte(e.callArgs[event.ToolCallID])
		if !json.Valid(args) {
			return nil, nil, &protocolconv.Error{Code: protocolconv.ErrorInvalidStream, Protocol: protocolconv.ProtocolGoogleGenAI, Message: "tool call arguments are not complete JSON"}
		}
		call.Args = json.RawMessage(args)
		wire.Candidates = []candidateWire{{Index: event.ChoiceIndex, Content: contentWire{Role: "model", Parts: []partWire{{FunctionCall: call, ThoughtSignature: e.callSignatures[event.ToolCallID]}}}}}
		emit = true
	case ir.EventUsage:
		e.usage = event.Usage
	case ir.EventFinish:
		e.finishReason = ir.FinishReason{Reason: "stop"}
		if event.FinishReason != nil {
			e.finishReason = *event.FinishReason
		}
	case ir.EventStreamEnd:
		wire.Candidates = []candidateWire{{FinishReason: finishToGoogle(e.finishReason), Content: contentWire{Role: "model", Parts: []partWire{}}}}
		wire.UsageMetadata = usageToGoogle(e.usage)
		emit = true
	case ir.EventError:
		return nil, nil, &protocolconv.Error{Code: protocolconv.ErrorInvalidStream, Protocol: protocolconv.ProtocolGoogleGenAI, Message: "Google standard stream has no portable in-band error event"}
	}
	if !emit {
		return nil, nil, nil
	}
	wire.ResponseID = e.id
	wire.ModelVersion = e.model
	body, err := json.Marshal(&wire)
	if err != nil {
		return nil, nil, err
	}
	return [][]byte{body}, nil, nil
}

func (e *streamEncoder) Finalize() ([][]byte, []protocolconv.Warning, error) { return nil, nil, nil }

func googlePartType(part partWire) ir.ContentType {
	if part.FunctionCall != nil {
		return ir.ContentToolCall
	}
	if part.Thought {
		return ir.ContentReasoning
	}
	return ir.ContentText
}
func googlePartIdentity(part partWire) string {
	if part.FunctionCall != nil {
		return part.FunctionCall.ID
	}
	return ""
}
func cumulativeDelta(seen, incoming string) string {
	if strings.HasPrefix(incoming, seen) {
		return strings.TrimPrefix(incoming, seen)
	}
	if incoming == seen {
		return ""
	}
	return incoming
}

func (*Converter) NewStreamDecoder() protocolconv.StreamDecoder { return newStreamDecoder() }
func (*Converter) NewStreamEncoder() protocolconv.StreamEncoder { return newStreamEncoder() }
func (*Converter) NewStreamDecoderWithOptions(protocolconv.Options) protocolconv.StreamDecoder {
	return newStreamDecoder()
}
func (*Converter) NewStreamEncoderWithOptions(options protocolconv.Options) protocolconv.StreamEncoder {
	return newStreamEncoderWithOptions(options)
}

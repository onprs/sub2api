package anthropic

import (
	"encoding/json"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv"
	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv/ir"
	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv/toolrouting"
)

func decodeDirectRequest(body []byte, options protocolconv.Options) (*ir.Request, []protocolconv.Warning, error) {
	var wire requestWire
	if err := decode(body, &wire); err != nil {
		return nil, nil, err
	}
	out := &ir.Request{Model: wire.Model, Generation: ir.GenerationConfig{Temperature: wire.Temperature, TopP: wire.TopP, StopSequences: append([]string(nil), wire.StopSequences...)}, Stream: ir.StreamConfig{Enabled: wire.Stream}}
	if wire.MaxTokens > 0 {
		value := wire.MaxTokens
		out.Generation.MaxTokens = &value
	}
	if err := decodeSystem(wire.System, &out.SystemInstruction); err != nil {
		return nil, nil, err
	}
	for _, message := range wire.Messages {
		parts, err := decodeBlocks(message.Content)
		if err != nil {
			return nil, nil, err
		}
		role := ir.RoleUser
		if message.Role == "assistant" {
			role = ir.RoleAssistant
		}
		if role == ir.RoleUser && allToolResults(parts) {
			role = ir.RoleTool
		}
		out.Messages = append(out.Messages, ir.Message{Role: role, Content: parts})
	}
	for _, tool := range wire.Tools {
		out.Tools = append(out.Tools, ir.ToolDefinition{Type: "function", ProviderType: tool.Type, Name: tool.Name, Description: tool.Description, Parameters: cloneRaw(tool.InputSchema), CacheHint: cloneRaw(tool.CacheControl)})
	}
	out.ToolChoice = decodeChoice(wire.ToolChoice)
	if wire.Thinking != nil {
		budget := wire.Thinking.BudgetTokens
		out.Reasoning = &ir.ReasoningConfig{Mode: wire.Thinking.Type}
		if budget > 0 {
			out.Reasoning.BudgetTokens = &budget
		}
	}
	if wire.OutputConfig != nil {
		if out.Reasoning == nil {
			out.Reasoning = &ir.ReasoningConfig{Mode: "enabled"}
		}
		out.Reasoning.Effort = effortFromAnthropic(wire.OutputConfig.Effort)
	}
	if err := ir.ValidateRequest(out); err != nil {
		return nil, nil, &protocolconv.Error{Code: protocolconv.ErrorInvalidIR, Protocol: protocolconv.ProtocolAnthropic, Cause: err}
	}
	return out, nil, nil
}

func encodeDirectRequest(request *ir.Request, options protocolconv.Options) ([]byte, []protocolconv.Warning, error) {
	if err := ir.ValidateRequest(request); err != nil {
		return nil, nil, &protocolconv.Error{Code: protocolconv.ErrorInvalidIR, Protocol: protocolconv.ProtocolAnthropic, Cause: err}
	}
	wire := requestWire{Model: request.Model, Stream: request.Stream.Enabled, Temperature: request.Generation.Temperature, TopP: request.Generation.TopP, StopSequences: append([]string(nil), request.Generation.StopSequences...)}
	if request.Generation.MaxTokens != nil {
		wire.MaxTokens = *request.Generation.MaxTokens
	}
	var err error
	wire.System, err = encodeSystem(request.SystemInstruction)
	if err != nil {
		return nil, nil, err
	}
	var warnings []protocolconv.Warning
	for _, message := range request.Messages {
		blocks, blockWarnings, err := encodeBlocks(message.Content, options)
		warnings = append(warnings, blockWarnings...)
		if err != nil {
			return nil, warnings, err
		}
		role := "user"
		if message.Role == ir.RoleAssistant {
			role = "assistant"
		}
		content, err := json.Marshal(blocks)
		if err != nil {
			return nil, warnings, err
		}
		wire.Messages = append(wire.Messages, messageWire{Role: role, Content: content})
	}
	wire.Tools = encodeRequestTools(request.Tools)
	wire.ToolChoice = encodeRequestChoice(request.ToolChoice)
	if request.Reasoning != nil {
		wire.Thinking = &thinkingWire{Type: request.Reasoning.Mode}
		if wire.Thinking.Type == "" {
			wire.Thinking.Type = "enabled"
		}
		if request.Reasoning.BudgetTokens != nil {
			wire.Thinking.BudgetTokens = *request.Reasoning.BudgetTokens
		}
		if request.Reasoning.Effort != "" {
			wire.OutputConfig = &outputWire{Effort: effortToAnthropic(request.Reasoning.Effort)}
		}
	}
	body, err := json.Marshal(&wire)
	return body, warnings, err
}

func decodeSystem(raw json.RawMessage, out *[]ir.ContentPart) error {
	if len(raw) == 0 {
		return nil
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		*out = append(*out, ir.ContentPart{Type: ir.ContentText, Text: text})
		return nil
	}
	var blocks []blockWire
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return err
	}
	for _, block := range blocks {
		if block.Type == "text" {
			*out = append(*out, ir.ContentPart{Type: ir.ContentText, Text: block.Text, CacheHint: cloneRaw(block.CacheControl)})
		}
	}
	return nil
}
func encodeSystem(parts []ir.ContentPart) (json.RawMessage, error) {
	if len(parts) == 0 {
		return nil, nil
	}
	blocks := make([]blockWire, 0, len(parts))
	for _, part := range parts {
		blocks = append(blocks, blockWire{Type: "text", Text: part.Text, CacheControl: cloneRaw(part.CacheHint)})
	}
	return json.Marshal(blocks)
}

func decodeBlocks(raw json.RawMessage) ([]ir.ContentPart, error) {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return []ir.ContentPart{{Type: ir.ContentText, Text: text}}, nil
	}
	var blocks []blockWire
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return nil, err
	}
	return decodeBlocksFromSlice(blocks)
}

func decodeBlocksFromSlice(blocks []blockWire) ([]ir.ContentPart, error) {
	out := make([]ir.ContentPart, 0, len(blocks))
	for _, b := range blocks {
		switch b.Type {
		case "text":
			out = append(out, ir.ContentPart{Type: ir.ContentText, Text: b.Text, CacheHint: cloneRaw(b.CacheControl)})
		case "thinking":
			out = append(out, ir.ContentPart{Type: ir.ContentReasoning, Reasoning: b.Thinking, Signature: b.Signature, CacheHint: cloneRaw(b.CacheControl)})
		case "redacted_thinking":
			out = append(out, ir.ContentPart{Type: ir.ContentReasoning, Signature: b.Data})
		case "image":
			if b.Source != nil {
				out = append(out, ir.ContentPart{Type: ir.ContentImage, Data: b.Source.Data, MediaType: b.Source.MediaType})
			}
		case "tool_use":
			input := cloneRaw(b.Input)
			if len(input) == 0 {
				input = json.RawMessage(`{}`)
			}
			out = append(out, ir.ContentPart{Type: ir.ContentToolCall, ToolCallID: b.ID, ToolName: b.Name, ToolInput: input})
		case "tool_result":
			result := cloneRaw(b.Content)
			if len(result) == 0 {
				result = json.RawMessage(`""`)
			}
			out = append(out, ir.ContentPart{Type: ir.ContentToolResult, ToolCallID: b.ToolUseID, ToolResult: result, IsError: b.IsError})
		}
	}
	return out, nil
}
func encodeBlocks(parts []ir.ContentPart, options protocolconv.Options) ([]blockWire, []protocolconv.Warning, error) {
	out := make([]blockWire, 0, len(parts))
	var warnings []protocolconv.Warning
	for _, p := range parts {
		switch p.Type {
		case ir.ContentText:
			out = append(out, blockWire{Type: "text", Text: p.Text, CacheControl: cloneRaw(p.CacheHint)})
		case ir.ContentReasoning:
			out = append(out, blockWire{Type: "thinking", Thinking: p.Reasoning, Signature: p.Signature, CacheControl: cloneRaw(p.CacheHint)})
		case ir.ContentImage:
			if p.Data == "" {
				if options.LossPolicy == protocolconv.LossWarn {
					warnings = append(warnings, protocolconv.Warning{Code: protocolconv.WarningUnsupportedCapability, Protocol: protocolconv.ProtocolAnthropic, Capability: protocolconv.CapabilityImageURL, Message: "URL image dropped"})
					continue
				}
				return nil, warnings, &protocolconv.Error{Code: protocolconv.ErrorUnsupportedCapability, Protocol: protocolconv.ProtocolAnthropic, Capability: protocolconv.CapabilityImageURL}
			}
			out = append(out, blockWire{Type: "image", Source: &imageWire{Type: "base64", MediaType: p.MediaType, Data: p.Data}})
		case ir.ContentToolCall:
			out = append(out, blockWire{Type: "tool_use", ID: p.ToolCallID, Name: p.ToolName, Input: cloneRaw(p.ToolInput)})
		case ir.ContentToolResult:
			out = append(out, blockWire{Type: "tool_result", ToolUseID: p.ToolCallID, Content: cloneRaw(p.ToolResult), IsError: p.IsError})
		case ir.ContentRefusal:
			out = append(out, blockWire{Type: "text", Text: p.Refusal})
		default:
			if options.LossPolicy == protocolconv.LossWarn {
				warnings = append(warnings, protocolconv.Warning{Code: protocolconv.WarningUnsupportedCapability, Protocol: protocolconv.ProtocolAnthropic, Message: "content part dropped"})
				continue
			}
			return nil, warnings, &protocolconv.Error{Code: protocolconv.ErrorUnsupportedCapability, Protocol: protocolconv.ProtocolAnthropic}
		}
	}
	return out, warnings, nil
}
func allToolResults(parts []ir.ContentPart) bool {
	if len(parts) == 0 {
		return false
	}
	for _, p := range parts {
		if p.Type != ir.ContentToolResult {
			return false
		}
	}
	return true
}
func encodeRequestTools(tools []ir.ToolDefinition) []toolWire {
	var out []toolWire
	for _, tool := range tools {
		switch tool.ProviderType {
		case "namespace":
			for _, child := range tool.Children {
				out = append(out, toolWire{
					Name:         toolrouting.FlattenNamespaceName(tool.Name, child.Name),
					Description:  child.Description,
					InputSchema:  functionToolSchema(child.Parameters),
					CacheControl: cloneRaw(child.CacheHint),
				})
			}
		case "tool_search":
			out = append(out, toolWire{Name: "tool_search", Description: tool.Description, InputSchema: functionToolSchema(tool.Parameters), CacheControl: cloneRaw(tool.CacheHint)})
		case "custom":
			out = append(out, toolWire{Name: tool.Name, Description: tool.Description, InputSchema: customToolSchema(), CacheControl: cloneRaw(tool.CacheHint)})
		default:
			out = append(out, toolWire{Type: tool.ProviderType, Name: tool.Name, Description: tool.Description, InputSchema: cloneRaw(tool.Parameters), CacheControl: cloneRaw(tool.CacheHint)})
		}
	}
	return out
}

func functionToolSchema(schema json.RawMessage) json.RawMessage {
	if len(schema) > 0 && json.Valid(schema) && strings.TrimSpace(string(schema)) != "null" {
		return cloneRaw(schema)
	}
	return json.RawMessage(`{"type":"object"}`)
}

func customToolSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"input":{"type":"string"}},"required":["input"]}`)
}

func encodeRequestChoice(choice *ir.ToolChoice) json.RawMessage {
	if choice == nil {
		return nil
	}
	if choice.Kind == "tool_search" && choice.Name == "" {
		copy := *choice
		copy.Name = "tool_search"
		copy.Mode = "tool"
		return encodeChoice(&copy)
	}
	return encodeChoice(choice)
}

func decodeChoice(raw json.RawMessage) *ir.ToolChoice {
	if len(raw) == 0 {
		return nil
	}
	var v struct {
		Type string `json:"type"`
		Name string `json:"name"`
	}
	if json.Unmarshal(raw, &v) != nil {
		return nil
	}
	mode := v.Type
	if mode == "any" {
		mode = "any"
	}
	return &ir.ToolChoice{Mode: mode, Name: v.Name}
}
func encodeChoice(choice *ir.ToolChoice) json.RawMessage {
	if choice == nil {
		return nil
	}
	typ := choice.Mode
	if typ == "tool" {
		body, _ := json.Marshal(map[string]any{"type": "tool", "name": choice.Name})
		return body
	}
	body, _ := json.Marshal(map[string]any{"type": typ})
	return body
}
func finishFromAnthropic(reason string) string {
	switch reason {
	case "max_tokens":
		return "length"
	case "tool_use":
		return "tool_calls"
	case "refusal":
		return "refusal"
	default:
		return "stop"
	}
}
func finishToAnthropic(reason ir.FinishReason) string {
	if reason.ProviderReason != "" {
		return reason.ProviderReason
	}
	switch reason.Reason {
	case "length":
		return "max_tokens"
	case "tool_calls":
		return "tool_use"
	case "refusal":
		return "refusal"
	default:
		if reason.StopSequence != "" {
			return "stop_sequence"
		}
		return "end_turn"
	}
}

func effortFromAnthropic(v string) string {
	if v == "max" {
		return "xhigh"
	}
	return v
}
func effortToAnthropic(v string) string {
	if v == "xhigh" {
		return "max"
	}
	return v
}
func cloneRaw(raw json.RawMessage) json.RawMessage { return append(json.RawMessage(nil), raw...) }
func joinText(parts []ir.ContentPart) string {
	var b strings.Builder
	for _, p := range parts {
		if p.Type == ir.ContentText {
			b.WriteString(p.Text)
		}
	}
	return b.String()
}

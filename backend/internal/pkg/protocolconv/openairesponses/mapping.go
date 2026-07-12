package openairesponses

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv"
	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv/ir"
)

func decodeInstructions(raw json.RawMessage, out *[]ir.ContentPart) error {
	if len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		*out = append(*out, ir.ContentPart{Type: ir.ContentText, Text: text})
		return nil
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &parts); err != nil {
		return err
	}
	for _, part := range parts {
		if part.Type == "input_text" || part.Type == "text" {
			*out = append(*out, ir.ContentPart{Type: ir.ContentText, Text: part.Text})
		}
	}
	return nil
}

func encodeInstructions(parts []ir.ContentPart) (json.RawMessage, error) {
	if len(parts) == 0 {
		return nil, nil
	}
	var text strings.Builder
	for _, part := range parts {
		if part.Type != ir.ContentText {
			return nil, &protocolconv.Error{Code: protocolconv.ErrorUnsupportedCapability, Protocol: protocolconv.ProtocolOpenAIResponses, Capability: protocolconv.CapabilitySystem, Path: "system_instruction"}
		}
		text.WriteString(part.Text)
	}
	return json.Marshal(text.String())
}

func decodeInput(raw json.RawMessage, messages *[]ir.Message) error {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		*messages = append(*messages, ir.Message{Role: ir.RoleUser, Content: []ir.ContentPart{{Type: ir.ContentText, Text: text}}})
		return nil
	}
	var items []inputItemWire
	if err := json.Unmarshal(raw, &items); err != nil {
		return err
	}
	for _, item := range items {
		switch item.Type {
		case "function_call":
			args := json.RawMessage(item.Arguments)
			if !json.Valid(args) {
				args = json.RawMessage(`{}`)
			}
			*messages = append(*messages, ir.Message{Role: ir.RoleAssistant, Content: []ir.ContentPart{{Type: ir.ContentToolCall, ToolCallID: item.CallID, ToolName: item.Name, ToolInput: args}}})
		case "function_call_output":
			result := item.Output
			if len(result) == 0 {
				result = json.RawMessage(`""`)
			}
			if !json.Valid(result) {
				result, _ = json.Marshal(string(result))
			}
			*messages = append(*messages, ir.Message{Role: ir.RoleTool, Content: []ir.ContentPart{{Type: ir.ContentToolResult, ToolCallID: item.CallID, ToolResult: result}}})
		case "reasoning":
			var text strings.Builder
			for _, summary := range item.Summary {
				text.WriteString(summary.Text)
			}
			*messages = append(*messages, ir.Message{Role: ir.RoleAssistant, Content: []ir.ContentPart{{Type: ir.ContentReasoning, Reasoning: text.String(), Signature: item.EncryptedContent}}})
		default:
			role := parseRole(item.Role)
			parts, err := decodeMessageContent(item.Content, role)
			if err != nil {
				return err
			}
			*messages = append(*messages, ir.Message{Role: role, Content: parts})
		}
	}
	return nil
}

func decodeMessageContent(raw json.RawMessage, role ir.Role) ([]ir.ContentPart, error) {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return []ir.ContentPart{{Type: ir.ContentText, Text: text}}, nil
	}
	var parts []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &parts); err != nil {
		return nil, err
	}
	out := make([]ir.ContentPart, 0, len(parts))
	for _, part := range parts {
		var kind string
		_ = json.Unmarshal(part["type"], &kind)
		switch kind {
		case "input_text", "output_text", "text":
			var text string
			_ = json.Unmarshal(part["text"], &text)
			out = append(out, ir.ContentPart{Type: ir.ContentText, Text: text})
		case "input_image":
			var url string
			_ = json.Unmarshal(part["image_url"], &url)
			out = append(out, imagePartFromURL(url))
		case "refusal":
			var refusal string
			_ = json.Unmarshal(part["refusal"], &refusal)
			if refusal == "" {
				_ = json.Unmarshal(part["text"], &refusal)
			}
			out = append(out, ir.ContentPart{Type: ir.ContentRefusal, Refusal: refusal})
		}
	}
	return out, nil
}

func encodeInput(messages []ir.Message, capabilities protocolconv.CapabilitySet, options protocolconv.Options) (json.RawMessage, []protocolconv.Warning, error) {
	items := make([]any, 0, len(messages))
	var warnings []protocolconv.Warning
	for i, message := range messages {
		var content []map[string]any
		flush := func() {
			if len(content) > 0 {
				items = append(items, map[string]any{"role": responsesRole(message.Role), "content": content})
				content = nil
			}
		}
		for j, part := range message.Content {
			path := "messages"
			_ = i
			_ = j
			switch part.Type {
			case ir.ContentText:
				partType := "input_text"
				if message.Role == ir.RoleAssistant {
					partType = "output_text"
				}
				content = append(content, map[string]any{"type": partType, "text": part.Text})
			case ir.ContentImage:
				content = append(content, map[string]any{"type": "input_image", "image_url": imagePartURL(part)})
			case ir.ContentRefusal:
				content = append(content, map[string]any{"type": "refusal", "refusal": part.Refusal})
			case ir.ContentReasoning:
				flush()
				items = append(items, map[string]any{"type": "reasoning", "summary": []any{map[string]any{"type": "summary_text", "text": part.Reasoning}}, "encrypted_content": part.Signature})
			case ir.ContentToolCall:
				flush()
				items = append(items, map[string]any{"type": "function_call", "call_id": part.ToolCallID, "name": part.ToolName, "arguments": rawJSONString(part.ToolInput)})
			case ir.ContentToolResult:
				flush()
				items = append(items, map[string]any{"type": "function_call_output", "call_id": part.ToolCallID, "output": toolResultString(part.ToolResult)})
			default:
				warning, err := unsupported(protocolconv.ProtocolOpenAIResponses, capabilityForPart(part.Type), path, options)
				warnings = append(warnings, warning...)
				if err != nil {
					return nil, warnings, err
				}
			}
		}
		flush()
	}
	body, err := json.Marshal(items)
	return body, warnings, err
}

func decodeTools(tools []apicompat.ResponsesTool) []ir.ToolDefinition {
	out := make([]ir.ToolDefinition, 0, len(tools))
	for _, tool := range tools {
		out = append(out, ir.ToolDefinition{Type: "function", ProviderType: tool.Type, Name: tool.Name, Description: tool.Description, Parameters: cloneRaw(tool.Parameters), Strict: tool.Strict})
	}
	return out
}

func encodeTools(tools []ir.ToolDefinition) []apicompat.ResponsesTool {
	out := make([]apicompat.ResponsesTool, 0, len(tools))
	for _, tool := range tools {
		kind := tool.ProviderType
		if kind == "" {
			kind = tool.Type
		}
		if kind == "" {
			kind = "function"
		}
		out = append(out, apicompat.ResponsesTool{Type: kind, Name: tool.Name, Description: tool.Description, Parameters: cloneRaw(tool.Parameters), Strict: tool.Strict})
	}
	return out
}

func decodeToolChoice(raw json.RawMessage) *ir.ToolChoice {
	if len(raw) == 0 {
		return nil
	}
	var mode string
	if json.Unmarshal(raw, &mode) == nil {
		return &ir.ToolChoice{Mode: mode}
	}
	var value struct {
		Type string `json:"type"`
		Name string `json:"name"`
	}
	if json.Unmarshal(raw, &value) == nil {
		return &ir.ToolChoice{Mode: "tool", Name: value.Name}
	}
	return nil
}

func encodeToolChoice(choice *ir.ToolChoice) json.RawMessage {
	if choice == nil {
		return nil
	}
	if choice.Mode == "tool" {
		body, _ := json.Marshal(map[string]any{"type": "function", "name": choice.Name})
		return body
	}
	body, _ := json.Marshal(choice.Mode)
	return body
}

func decodeResponseFormat(raw json.RawMessage) *ir.ResponseFormat {
	var value struct {
		Type   string          `json:"type"`
		Name   string          `json:"name"`
		Schema json.RawMessage `json:"schema"`
	}
	if json.Unmarshal(raw, &value) != nil {
		return nil
	}
	return &ir.ResponseFormat{Type: value.Type, JSONSchema: cloneRaw(value.Schema)}
}

func encodeResponseFormat(format *ir.ResponseFormat, _ protocolconv.CapabilitySet, _ protocolconv.Options) (json.RawMessage, []protocolconv.Warning, error) {
	if format == nil {
		return nil, nil, nil
	}
	var value any
	switch format.Type {
	case "json_schema":
		value = map[string]any{"type": "json_schema", "name": "response", "schema": json.RawMessage(format.JSONSchema)}
	case "json_object":
		value = map[string]any{"type": "json_object"}
	default:
		value = map[string]any{"type": "text"}
	}
	body, err := json.Marshal(value)
	return body, nil, err
}

func decodeUsage(usage *apicompat.ResponsesUsage) *ir.Usage {
	if usage == nil {
		return nil
	}
	out := &ir.Usage{InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens, TotalTokens: usage.TotalTokens, CacheCreationTokens: usage.CacheCreationInputTokens}
	if usage.InputTokensDetails != nil {
		out.CacheReadTokens = usage.InputTokensDetails.CachedTokens
		out.InputTokenDetails = map[string]int{"cached_tokens": usage.InputTokensDetails.CachedTokens, "audio_tokens": usage.InputTokensDetails.AudioTokens, "cache_creation_5m_tokens": usage.InputTokensDetails.CacheCreation5mTokens, "cache_creation_1h_tokens": usage.InputTokensDetails.CacheCreation1hTokens}
	}
	if usage.OutputTokensDetails != nil {
		out.ReasoningTokens = usage.OutputTokensDetails.ReasoningTokens
		out.OutputTokenDetails = map[string]int{"reasoning_tokens": usage.OutputTokensDetails.ReasoningTokens, "audio_tokens": usage.OutputTokensDetails.AudioTokens}
	}
	return out
}

func encodeUsage(usage *ir.Usage) *apicompat.ResponsesUsage {
	if usage == nil {
		return nil
	}
	out := &apicompat.ResponsesUsage{InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens, TotalTokens: usage.TotalTokens, CacheCreationInputTokens: usage.CacheCreationTokens}
	if usage.CacheReadTokens != 0 || len(usage.InputTokenDetails) > 0 {
		out.InputTokensDetails = &apicompat.ResponsesInputTokensDetails{CachedTokens: usage.CacheReadTokens}
	}
	if usage.ReasoningTokens != 0 || len(usage.OutputTokenDetails) > 0 {
		out.OutputTokensDetails = &apicompat.ResponsesOutputTokensDetails{ReasoningTokens: usage.ReasoningTokens}
	}
	return out
}

func responsesFinishReason(response *apicompat.ResponsesResponse) string {
	if response.Status == "failed" {
		return "error"
	}
	if response.Status == "incomplete" && response.IncompleteDetails != nil {
		if response.IncompleteDetails.Reason == "content_filter" {
			return "content_filter"
		}
		return "length"
	}
	return "stop"
}

func parseRole(role string) ir.Role {
	switch role {
	case "system":
		return ir.RoleSystem
	case "developer":
		return ir.RoleDeveloper
	case "assistant":
		return ir.RoleAssistant
	case "tool":
		return ir.RoleTool
	default:
		return ir.RoleUser
	}
}
func responsesRole(role ir.Role) string {
	if role == ir.RoleTool {
		return "user"
	}
	return string(role)
}

func imagePartFromURL(value string) ir.ContentPart {
	if strings.HasPrefix(value, "data:") {
		media, data, ok := strings.Cut(strings.TrimPrefix(value, "data:"), ";base64,")
		if ok {
			return ir.ContentPart{Type: ir.ContentImage, MediaType: media, Data: data}
		}
	}
	return ir.ContentPart{Type: ir.ContentImage, URL: value}
}
func imagePartURL(part ir.ContentPart) string {
	if part.URL != "" {
		return part.URL
	}
	return "data:" + part.MediaType + ";base64," + part.Data
}
func cloneRaw(raw json.RawMessage) json.RawMessage { return append(json.RawMessage(nil), raw...) }

func toolResultString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	return string(raw)
}

func encodeRequestExtensions(wire requestWire) map[string]json.RawMessage {
	out := make(map[string]json.RawMessage)
	put := func(key string, value any) {
		body, _ := json.Marshal(value)
		if string(body) != "null" && string(body) != `""` {
			out[key] = body
		}
	}
	put("openai_responses.service_tier", wire.ServiceTier)
	put("openai_responses.previous_response_id", wire.PreviousResponseID)
	put("openai_responses.store", wire.Store)
	put("openai_responses.include", wire.Include)
	if len(out) == 0 {
		return nil
	}
	return out
}
func decodeRequestExtensions(ext map[string]json.RawMessage, wire *requestWire) {
	_ = json.Unmarshal(ext["openai_responses.service_tier"], &wire.ServiceTier)
	_ = json.Unmarshal(ext["openai_responses.previous_response_id"], &wire.PreviousResponseID)
	_ = json.Unmarshal(ext["openai_responses.store"], &wire.Store)
	_ = json.Unmarshal(ext["openai_responses.include"], &wire.Include)
}

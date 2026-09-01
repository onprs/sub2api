package openairesponses

import (
	"bytes"
	"encoding/json"
	"fmt"
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
		Type         string          `json:"type"`
		Text         string          `json:"text"`
		CacheControl json.RawMessage `json:"cache_control"`
	}
	if err := json.Unmarshal(raw, &parts); err != nil {
		return err
	}
	for _, part := range parts {
		if part.Type == "input_text" || part.Type == "text" {
			*out = append(*out, ir.ContentPart{Type: ir.ContentText, Text: part.Text, CacheHint: cloneRaw(part.CacheControl)})
		}
	}
	return nil
}

func encodeInstructions(parts []ir.ContentPart) (json.RawMessage, error) {
	if len(parts) == 0 {
		return nil, nil
	}
	var text strings.Builder
	hasCacheHints := false
	for _, part := range parts {
		if part.Type != ir.ContentText {
			return nil, &protocolconv.Error{Code: protocolconv.ErrorUnsupportedCapability, Protocol: protocolconv.ProtocolOpenAIResponses, Capability: protocolconv.CapabilitySystem, Path: "system_instruction"}
		}
		if len(part.CacheHint) > 0 {
			hasCacheHints = true
		}
		_, _ = text.WriteString(part.Text)
	}
	if !hasCacheHints {
		return json.Marshal(text.String())
	}

	encoded := make([]map[string]any, 0, len(parts))
	for _, part := range parts {
		item := map[string]any{"type": "input_text", "text": part.Text}
		if len(part.CacheHint) > 0 {
			item["cache_control"] = cloneRaw(part.CacheHint)
		}
		encoded = append(encoded, item)
	}
	return json.Marshal(encoded)
}

func decodeInput(raw json.RawMessage, messages *[]ir.Message, additionalTools *[]apicompat.ResponsesTool) error {
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
		case "additional_tools":
			*additionalTools = append(*additionalTools, item.Tools...)
		case "function_call", "custom_tool_call", "tool_search_call":
			args := decodeToolArguments(item.Type, item.Arguments, item.Input)
			*messages = append(*messages, ir.Message{Role: ir.RoleAssistant, Content: []ir.ContentPart{{
				Type:          ir.ContentToolCall,
				ToolCallID:    item.CallID,
				ToolName:      item.Name,
				ToolKind:      item.Type,
				ToolNamespace: item.Namespace,
				ToolInput:     args,
			}}})
		case "function_call_output", "custom_tool_call_output", "tool_search_output":
			result, content, err := decodeToolResultValue(item.Output)
			if err != nil {
				return err
			}
			*messages = append(*messages, ir.Message{Role: ir.RoleTool, Content: []ir.ContentPart{{
				Type:              ir.ContentToolResult,
				ToolCallID:        item.CallID,
				ToolKind:          toolCallKindFromOutput(item.Type),
				ToolResult:        result,
				ToolResultContent: content,
			}}})
		case "reasoning":
			var text strings.Builder
			for _, summary := range item.Summary {
				_, _ = text.WriteString(summary.Text)
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
			out = append(out, ir.ContentPart{Type: ir.ContentText, Text: text, CacheHint: cloneRaw(part["cache_control"])})
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
				item := map[string]any{"type": partType, "text": part.Text}
				if len(part.CacheHint) > 0 {
					item["cache_control"] = cloneRaw(part.CacheHint)
				}
				content = append(content, item)
			case ir.ContentImage:
				content = append(content, map[string]any{"type": "input_image", "image_url": imagePartURL(part)})
			case ir.ContentRefusal:
				content = append(content, map[string]any{"type": "refusal", "refusal": part.Refusal})
			case ir.ContentReasoning:
				flush()
				items = append(items, map[string]any{"type": "reasoning", "summary": []any{map[string]any{"type": "summary_text", "text": part.Reasoning}}, "encrypted_content": part.Signature})
			case ir.ContentToolCall:
				flush()
				items = append(items, encodeInputToolCall(part))
			case ir.ContentToolResult:
				flush()
				output, outputWarnings, err := encodeToolResultValue(part, capabilities, options, fmt.Sprintf("messages[%d].content[%d]", i, j))
				warnings = append(warnings, outputWarnings...)
				if err != nil {
					return nil, warnings, err
				}
				items = append(items, map[string]any{"type": toolOutputKind(part.ToolKind), "call_id": part.ToolCallID, "output": output})
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

func encodeInputToolCall(part ir.ContentPart) map[string]any {
	kind := part.ToolKind
	if kind == "" {
		kind = "function_call"
	}
	item := map[string]any{"type": kind, "call_id": part.ToolCallID, "name": part.ToolName}
	if part.ToolNamespace != "" {
		item["namespace"] = part.ToolNamespace
	}
	switch kind {
	case "custom_tool_call":
		var input string
		if json.Unmarshal(part.ToolInput, &input) != nil {
			input = string(part.ToolInput)
		}
		item["input"] = input
	case "tool_search_call":
		arguments := json.RawMessage(part.ToolInput)
		if !json.Valid(arguments) {
			arguments = json.RawMessage(`{}`)
		}
		item["arguments"] = arguments
	default:
		item["arguments"] = rawJSONString(part.ToolInput)
	}
	return item
}

func toolCallKindFromOutput(kind string) string {
	switch kind {
	case "custom_tool_call_output":
		return "custom_tool_call"
	case "tool_search_output":
		return "tool_search_call"
	default:
		return "function_call"
	}
}

func toolOutputKind(kind string) string {
	switch kind {
	case "custom_tool_call":
		return "custom_tool_call_output"
	case "tool_search_call":
		return "tool_search_output"
	default:
		return "function_call_output"
	}
}

func decodeToolResultValue(raw json.RawMessage) (json.RawMessage, []ir.ContentPart, error) {
	if len(raw) == 0 {
		return json.RawMessage(`""`), nil, nil
	}
	if bytes.HasPrefix(bytes.TrimSpace(raw), []byte("[")) {
		var parts []map[string]json.RawMessage
		if json.Unmarshal(raw, &parts) == nil && len(parts) > 0 {
			for _, part := range parts {
				var kind string
				_ = json.Unmarshal(part["type"], &kind)
				if kind != "input_text" && kind != "text" && kind != "input_image" {
					return cloneRaw(raw), nil, nil
				}
			}
			content, err := decodeMessageContent(raw, ir.RoleTool)
			if err != nil {
				return nil, nil, err
			}
			return nil, content, nil
		}
	}
	if !json.Valid(raw) {
		encoded, _ := json.Marshal(string(raw))
		return encoded, nil, nil
	}
	return cloneRaw(raw), nil, nil
}

func encodeToolResultValue(part ir.ContentPart, capabilities protocolconv.CapabilitySet, options protocolconv.Options, path string) (any, []protocolconv.Warning, error) {
	if len(part.ToolResultContent) == 0 {
		if part.ToolKind != "tool_search_call" {
			return toolResultString(part.ToolResult), nil, nil
		}
		if len(part.ToolResult) == 0 {
			return "", nil, nil
		}
		var value any
		if json.Unmarshal(part.ToolResult, &value) == nil {
			return value, nil, nil
		}
		return string(part.ToolResult), nil, nil
	}

	content := make([]map[string]any, 0, len(part.ToolResultContent))
	var warnings []protocolconv.Warning
	for i, resultPart := range part.ToolResultContent {
		partPath := fmt.Sprintf("%s.tool_result_content[%d]", path, i)
		switch resultPart.Type {
		case ir.ContentText:
			content = append(content, map[string]any{"type": "input_text", "text": resultPart.Text})
		case ir.ContentImage:
			capability := protocolconv.CapabilityImageURL
			if resultPart.Data != "" {
				capability = protocolconv.CapabilityImageData
			}
			capabilityWarnings, err := checkContentCapability(capabilities, capability, partPath, options)
			warnings = append(warnings, capabilityWarnings...)
			if err != nil {
				return nil, warnings, err
			}
			content = append(content, map[string]any{"type": "input_image", "image_url": imagePartURL(resultPart)})
		default:
			capability := protocolconv.CapabilityFile
			if resultPart.Type == ir.ContentAudio {
				capability = protocolconv.CapabilityAudio
			}
			capabilityWarnings, err := unsupported(protocolconv.ProtocolOpenAIResponses, capability, partPath, options)
			warnings = append(warnings, capabilityWarnings...)
			if err != nil {
				return nil, warnings, err
			}
		}
	}
	return content, warnings, nil
}

func checkContentCapability(capabilities protocolconv.CapabilitySet, capability protocolconv.Capability, path string, options protocolconv.Options) ([]protocolconv.Warning, error) {
	if capabilities.Support(capability) != protocolconv.SupportNone {
		return nil, nil
	}
	if options.LossPolicy == protocolconv.LossWarn {
		return []protocolconv.Warning{{Code: protocolconv.WarningUnsupportedCapability, Protocol: protocolconv.ProtocolOpenAIResponses, Capability: capability, Path: path, Message: "content dropped from tool result"}}, nil
	}
	return nil, &protocolconv.Error{Code: protocolconv.ErrorUnsupportedCapability, Protocol: protocolconv.ProtocolOpenAIResponses, Capability: capability, Path: path}
}

func decodeTools(tools []apicompat.ResponsesTool) []ir.ToolDefinition {
	out := make([]ir.ToolDefinition, 0, len(tools))
	for _, tool := range tools {
		definition := ir.ToolDefinition{Type: "function", ProviderType: tool.Type, Name: tool.Name, Description: tool.Description, Parameters: cloneRaw(tool.Parameters), Strict: tool.Strict}
		if tool.Type == "namespace" {
			children := tool.Tools
			if len(children) == 0 {
				children = tool.Children
			}
			definition.Children = decodeTools(children)
			for i := range definition.Children {
				definition.Children[i].Namespace = tool.Name
			}
		}
		out = append(out, definition)
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
		wire := apicompat.ResponsesTool{Type: kind, Name: tool.Name, Description: tool.Description, Parameters: cloneRaw(tool.Parameters), Strict: tool.Strict}
		if kind == "namespace" {
			wire.Tools = encodeTools(tool.Children)
		}
		out = append(out, wire)
	}
	return out
}

func decodeToolChoice(raw json.RawMessage) *ir.ToolChoice {
	if len(raw) == 0 {
		return nil
	}
	var mode string
	if json.Unmarshal(raw, &mode) == nil {
		if mode == "required" {
			mode = "any"
		}
		return &ir.ToolChoice{Mode: mode}
	}
	var value struct {
		Type     string `json:"type"`
		Name     string `json:"name"`
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	}
	if json.Unmarshal(raw, &value) == nil {
		name := value.Name
		if name == "" {
			name = value.Function.Name
		}
		return &ir.ToolChoice{Mode: "tool", Kind: value.Type, Name: name}
	}
	return nil
}

func encodeToolChoice(choice *ir.ToolChoice) json.RawMessage {
	if choice == nil {
		return nil
	}
	if choice.Mode == "tool" {
		kind := choice.Kind
		if kind == "" {
			kind = "function"
		}
		value := map[string]any{"type": kind}
		if choice.Name != "" {
			value["name"] = choice.Name
		}
		body, _ := json.Marshal(value)
		return body
	}
	mode := choice.Mode
	if mode == "any" {
		mode = "required"
	}
	body, _ := json.Marshal(mode)
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
	return &ir.ResponseFormat{Type: strings.ToLower(strings.TrimSpace(value.Type)), JSONSchema: cloneRaw(value.Schema)}
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

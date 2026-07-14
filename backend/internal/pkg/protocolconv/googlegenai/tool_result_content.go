package googlegenai

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv"
	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv/ir"
)

type googleToolResultContentWire struct {
	Type      string `json:"type"`
	Text      string `json:"text,omitempty"`
	URL       string `json:"url,omitempty"`
	Data      string `json:"data,omitempty"`
	MediaType string `json:"media_type,omitempty"`
}

func googleToolResultToParts(part ir.ContentPart, target protocolconv.Protocol, options protocolconv.Options) ([]partWire, []protocolconv.Warning, error) {
	if len(part.ToolResultContent) == 0 {
		response := googleFunctionResponse(part.ToolResult)
		if part.IsError {
			response = googleNamedFunctionResponse("error", part.ToolResult)
		}
		return []partWire{{FunctionResponse: &functionResponseWire{ID: part.ToolCallID, Name: part.ToolName, Response: response}}}, nil, nil
	}

	content := make([]googleToolResultContentWire, 0, len(part.ToolResultContent))
	parts := make([]partWire, 0, len(part.ToolResultContent)+1)
	var warnings []protocolconv.Warning
	for i, resultPart := range part.ToolResultContent {
		path := fmt.Sprintf("tool_result_content[%d]", i)
		switch resultPart.Type {
		case ir.ContentText:
			content = append(content, googleToolResultContentWire{Type: "text", Text: resultPart.Text})
		case ir.ContentImage, ir.ContentFile, ir.ContentAudio:
			kind := string(resultPart.Type)
			content = append(content, googleToolResultContentWire{Type: kind, URL: resultPart.URL, Data: resultPart.Data, MediaType: resultPart.MediaType})
			converted, convertedWarnings, err := partToGoogle(resultPart, target, options)
			warnings = append(warnings, convertedWarnings...)
			if err != nil {
				return nil, warnings, err
			}
			parts = append(parts, converted...)
		default:
			capability := protocolconv.CapabilityFile
			if options.LossPolicy == protocolconv.LossWarn {
				warnings = append(warnings, protocolconv.Warning{Code: protocolconv.WarningUnsupportedCapability, Protocol: target, Capability: capability, Path: path, Message: "content dropped from tool result"})
				continue
			}
			return nil, warnings, &protocolconv.Error{Code: protocolconv.ErrorUnsupportedCapability, Protocol: target, Capability: capability, Path: path, Message: "content part is not supported in a Google tool result"}
		}
	}

	encoded, err := json.Marshal(content)
	if err != nil {
		return nil, warnings, err
	}
	key := "content"
	if part.IsError {
		key = "error"
	}
	response := googleNamedFunctionResponse(key, encoded)
	functionPart := partWire{FunctionResponse: &functionResponseWire{ID: part.ToolCallID, Name: part.ToolName, Response: response}}
	return append([]partWire{functionPart}, parts...), warnings, nil
}

func googleNamedFunctionResponse(name string, value json.RawMessage) json.RawMessage {
	if len(bytes.TrimSpace(value)) == 0 {
		value = json.RawMessage(`""`)
	}
	body, _ := json.Marshal(map[string]json.RawMessage{name: cloneRaw(value)})
	return body
}

func googleToolResultFromResponse(raw json.RawMessage) (json.RawMessage, []ir.ContentPart, bool, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return json.RawMessage(`{}`), nil, false, nil
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(trimmed, &object) != nil {
		return cloneRaw(trimmed), nil, false, nil
	}

	value, isError := object["error"]
	if !isError {
		value = object["content"]
	}
	content, ok, err := decodeGoogleToolResultContent(value)
	if err != nil {
		return nil, nil, false, err
	}
	if ok {
		return nil, content, isError, nil
	}
	if isError {
		return cloneRaw(value), nil, true, nil
	}
	return cloneRaw(trimmed), nil, false, nil
}

func decodeGoogleToolResultContent(raw json.RawMessage) ([]ir.ContentPart, bool, error) {
	if len(bytes.TrimSpace(raw)) == 0 || bytes.TrimSpace(raw)[0] != '[' {
		return nil, false, nil
	}
	var parts []googleToolResultContentWire
	if json.Unmarshal(raw, &parts) != nil || len(parts) == 0 {
		return nil, false, nil
	}
	content := make([]ir.ContentPart, 0, len(parts))
	for _, part := range parts {
		switch part.Type {
		case "text":
			content = append(content, ir.ContentPart{Type: ir.ContentText, Text: part.Text})
		case "image":
			content = append(content, ir.ContentPart{Type: ir.ContentImage, URL: part.URL, Data: part.Data, MediaType: part.MediaType})
		case "file":
			content = append(content, ir.ContentPart{Type: ir.ContentFile, URL: part.URL, Data: part.Data, MediaType: part.MediaType})
		case "audio":
			content = append(content, ir.ContentPart{Type: ir.ContentAudio, URL: part.URL, Data: part.Data, MediaType: part.MediaType})
		default:
			return nil, false, nil
		}
	}
	return content, true, nil
}

func googleToolResultDuplicateNativePrefix(content []ir.ContentPart, parts []partWire) int {
	matched := 0
	for _, part := range content {
		if part.Type != ir.ContentImage && part.Type != ir.ContentFile && part.Type != ir.ContentAudio {
			continue
		}
		if matched >= len(parts) || !googleNativePartMatches(part, parts[matched]) {
			return 0
		}
		matched++
	}
	return matched
}

func googleNativePartMatches(part ir.ContentPart, wire partWire) bool {
	if part.Data != "" {
		return wire.InlineData != nil && wire.InlineData.MIMEType == part.MediaType && wire.InlineData.Data == part.Data
	}
	return wire.FileData != nil && wire.FileData.MIMEType == part.MediaType && wire.FileData.FileURI == part.URL
}

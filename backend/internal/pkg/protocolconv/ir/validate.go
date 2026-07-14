package ir

import (
	"encoding/json"
	"fmt"
)

// ValidationError identifies an invalid semantic field in the IR.
type ValidationError struct {
	Path    string
	Message string
}

func (e *ValidationError) Error() string {
	if e.Path == "" {
		return e.Message
	}
	return fmt.Sprintf("%s at %s", e.Message, e.Path)
}

// ValidateRequest verifies invariants shared by all target converters.
func ValidateRequest(request *Request) error {
	if request == nil {
		return &ValidationError{Message: "request is nil"}
	}
	if request.Model == "" {
		return &ValidationError{Path: "model", Message: "model is required"}
	}
	for i := range request.SystemInstruction {
		if request.SystemInstruction[i].Type != ContentText {
			return &ValidationError{Path: fmt.Sprintf("system_instruction[%d]", i), Message: "system instruction only supports text parts"}
		}
	}
	for i := range request.Messages {
		if err := ValidateMessage(&request.Messages[i], fmt.Sprintf("messages[%d]", i)); err != nil {
			return err
		}
	}
	for i := range request.Tools {
		if err := validateToolDefinition(&request.Tools[i], fmt.Sprintf("tools[%d]", i)); err != nil {
			return err
		}
	}
	return nil
}

func validateToolDefinition(tool *ToolDefinition, path string) error {
	if tool == nil {
		return &ValidationError{Path: path, Message: "tool is nil"}
	}
	requiresName := tool.ProviderType == "" || tool.ProviderType == "function" || tool.ProviderType == "custom" || tool.ProviderType == "namespace"
	if requiresName && tool.Name == "" {
		return &ValidationError{Path: path + ".name", Message: "tool name is required"}
	}
	if len(tool.Parameters) > 0 && !json.Valid(tool.Parameters) {
		return &ValidationError{Path: path + ".parameters", Message: "tool parameters must be valid JSON"}
	}
	for i := range tool.Children {
		if err := validateToolDefinition(&tool.Children[i], fmt.Sprintf("%s.children[%d]", path, i)); err != nil {
			return err
		}
	}
	return nil
}

// ValidateResponse verifies complete response invariants.
func ValidateResponse(response *Response) error {
	if response == nil {
		return &ValidationError{Message: "response is nil"}
	}
	for i := range response.Choices {
		choice := &response.Choices[i]
		if err := ValidateMessage(&choice.Message, fmt.Sprintf("choices[%d].message", i)); err != nil {
			return err
		}
	}
	return nil
}

// ValidateMessage verifies role/content combinations while allowing empty
// assistant content for valid empty or tool-only provider responses.
func ValidateMessage(message *Message, path string) error {
	if message == nil {
		return &ValidationError{Path: path, Message: "message is nil"}
	}
	switch message.Role {
	case RoleSystem, RoleDeveloper, RoleUser, RoleAssistant, RoleTool:
	default:
		return &ValidationError{Path: path + ".role", Message: fmt.Sprintf("unsupported role %q", message.Role)}
	}
	for i := range message.Content {
		part := &message.Content[i]
		partPath := fmt.Sprintf("%s.content[%d]", path, i)
		if err := validateContentPart(message.Role, part, partPath); err != nil {
			return err
		}
	}
	return nil
}

func validateContentPart(role Role, part *ContentPart, path string) error {
	if part == nil {
		return &ValidationError{Path: path, Message: "content part is nil"}
	}
	switch part.Type {
	case ContentText:
		return nil
	case ContentImage:
		if part.URL == "" && part.Data == "" {
			return &ValidationError{Path: path, Message: "image requires URL or data"}
		}
	case ContentFile:
		if part.URL == "" && part.Data == "" {
			return &ValidationError{Path: path, Message: "file requires URL or data"}
		}
	case ContentToolCall:
		if role != RoleAssistant {
			return &ValidationError{Path: path, Message: "tool call requires assistant role"}
		}
		if part.ToolCallID == "" || part.ToolName == "" {
			return &ValidationError{Path: path, Message: "tool call requires ID and name"}
		}
		if len(part.ToolInput) > 0 && !json.Valid(part.ToolInput) {
			return &ValidationError{Path: path + ".tool_input", Message: "tool input must be valid JSON"}
		}
	case ContentToolResult:
		if role != RoleTool && role != RoleUser {
			return &ValidationError{Path: path, Message: "tool result requires tool or user role"}
		}
		if part.ToolCallID == "" {
			return &ValidationError{Path: path, Message: "tool result requires call ID"}
		}
		if len(part.ToolResult) > 0 && !json.Valid(part.ToolResult) {
			return &ValidationError{Path: path + ".tool_result", Message: "tool result must be valid JSON"}
		}
		if len(part.ToolResult) > 0 && len(part.ToolResultContent) > 0 {
			return &ValidationError{Path: path, Message: "tool result scalar and multipart content are mutually exclusive"}
		}
		for i := range part.ToolResultContent {
			contentPath := fmt.Sprintf("%s.tool_result_content[%d]", path, i)
			if err := validateToolResultContentPart(&part.ToolResultContent[i], contentPath); err != nil {
				return err
			}
		}
	case ContentReasoning, ContentRefusal, ContentCitation, ContentAudio:
		return nil
	default:
		return &ValidationError{Path: path + ".type", Message: fmt.Sprintf("unsupported content type %q", part.Type)}
	}
	return nil
}

func validateToolResultContentPart(part *ContentPart, path string) error {
	if part == nil {
		return &ValidationError{Path: path, Message: "tool result content part is nil"}
	}
	switch part.Type {
	case ContentText:
		return nil
	case ContentImage, ContentFile, ContentAudio:
		if part.URL == "" && part.Data == "" {
			return &ValidationError{Path: path, Message: fmt.Sprintf("%s requires URL or data", part.Type)}
		}
		return nil
	default:
		return &ValidationError{Path: path + ".type", Message: fmt.Sprintf("unsupported tool result content type %q", part.Type)}
	}
}

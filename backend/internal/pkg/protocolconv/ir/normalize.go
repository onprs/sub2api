package ir

import "fmt"

// NormalizeSystemInstruction moves system and developer messages into the
// dedicated instruction field while preserving their relative content order.
// Standard targets without distinct conversation roles consume this form.
func NormalizeSystemInstruction(request *Request) {
	if request == nil {
		return
	}
	messages := request.Messages[:0]
	for _, message := range request.Messages {
		if message.Role == RoleSystem || message.Role == RoleDeveloper {
			for _, part := range message.Content {
				if part.Type == ContentText {
					request.SystemInstruction = append(request.SystemInstruction, part)
				}
			}
			continue
		}
		messages = append(messages, message)
	}
	request.Messages = messages
}

// LinkToolResults fills a result's function name from the preceding call with
// the same ID. Some protocols carry only the call ID on tool results, while
// targets such as Google require the function name as well.
func LinkToolResults(request *Request) error {
	if request == nil {
		return nil
	}
	names := make(map[string]string)
	for messageIndex := range request.Messages {
		for partIndex := range request.Messages[messageIndex].Content {
			part := &request.Messages[messageIndex].Content[partIndex]
			switch part.Type {
			case ContentToolCall:
				if existing := names[part.ToolCallID]; existing != "" && existing != part.ToolName {
					return &ValidationError{Path: fmt.Sprintf("messages[%d].content[%d].tool_call_id", messageIndex, partIndex), Message: "tool call ID maps to multiple names"}
				}
				names[part.ToolCallID] = part.ToolName
			case ContentToolResult:
				if part.ToolName == "" {
					part.ToolName = names[part.ToolCallID]
				}
			}
		}
	}
	return nil
}

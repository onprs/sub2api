package ir

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateRequest(t *testing.T) {
	request := &Request{
		Model:             "model",
		SystemInstruction: []ContentPart{{Type: ContentText, Text: "be concise"}},
		Messages: []Message{
			{Role: RoleUser, Content: []ContentPart{{Type: ContentText, Text: "hello"}}},
			{Role: RoleAssistant, Content: []ContentPart{{Type: ContentToolCall, ToolCallID: "call-1", ToolName: "read", ToolInput: json.RawMessage(`{"path":"a"}`)}}},
			{Role: RoleTool, Content: []ContentPart{{Type: ContentToolResult, ToolCallID: "call-1", ToolResult: json.RawMessage(`"ok"`)}}},
		},
		Tools: []ToolDefinition{{Name: "read", Parameters: json.RawMessage(`{"type":"object"}`)}},
	}
	require.NoError(t, ValidateRequest(request))
}

func TestValidateRequestRejectsInvalidRolePartCombination(t *testing.T) {
	request := &Request{
		Model: "model",
		Messages: []Message{{
			Role:    RoleUser,
			Content: []ContentPart{{Type: ContentToolCall, ToolCallID: "call-1", ToolName: "read"}},
		}},
	}
	err := ValidateRequest(request)
	var validationErr *ValidationError
	require.ErrorAs(t, err, &validationErr)
	require.Equal(t, "messages[0].content[0]", validationErr.Path)
}

func TestLinkToolResultsFillsNameFromPrecedingCall(t *testing.T) {
	request := &Request{Messages: []Message{
		{Role: RoleAssistant, Content: []ContentPart{{Type: ContentToolCall, ToolCallID: "call-1", ToolName: "read_file"}}},
		{Role: RoleTool, Content: []ContentPart{{Type: ContentToolResult, ToolCallID: "call-1"}}},
	}}
	require.NoError(t, LinkToolResults(request))
	require.Equal(t, "read_file", request.Messages[1].Content[0].ToolName)
}

func TestLinkToolResultsRejectsConflictingCallNames(t *testing.T) {
	request := &Request{Messages: []Message{{Role: RoleAssistant, Content: []ContentPart{
		{Type: ContentToolCall, ToolCallID: "call-1", ToolName: "read_file"},
		{Type: ContentToolCall, ToolCallID: "call-1", ToolName: "write_file"},
	}}}}
	require.ErrorContains(t, LinkToolResults(request), "multiple names")
}

func TestValidateRequestRejectsInvalidRawJSON(t *testing.T) {
	request := &Request{
		Model: "model",
		Messages: []Message{{
			Role:    RoleAssistant,
			Content: []ContentPart{{Type: ContentToolCall, ToolCallID: "call-1", ToolName: "read", ToolInput: json.RawMessage(`{`)}},
		}},
	}
	err := ValidateRequest(request)
	require.ErrorContains(t, err, "tool input must be valid JSON")
}

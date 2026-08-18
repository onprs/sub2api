package openairesponses

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv"
	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv/ir"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestEncodeRequestPreservesInstructionAndMessageCacheControl(t *testing.T) {
	request := &ir.Request{
		Model: "model",
		SystemInstruction: []ir.ContentPart{{
			Type: ir.ContentText, Text: "rules", CacheHint: []byte(`{"type":"ephemeral"}`),
		}},
		Messages: []ir.Message{{Role: ir.RoleUser, Content: []ir.ContentPart{{
			Type: ir.ContentText, Text: "hello", CacheHint: []byte(`{"type":"ephemeral","ttl":"1h"}`),
		}}}},
	}

	body, warnings, err := New().EncodeRequest(request, protocolconv.Options{})
	require.NoError(t, err)
	require.Empty(t, warnings)
	require.Equal(t, "ephemeral", gjson.GetBytes(body, "instructions.0.cache_control.type").String())
	require.Equal(t, "1h", gjson.GetBytes(body, "input.0.content.0.cache_control.ttl").String())

	restored, warnings, err := New().DecodeRequest(body, protocolconv.Options{})
	require.NoError(t, err)
	require.Empty(t, warnings)
	require.JSONEq(t, `{"type":"ephemeral"}`, string(restored.SystemInstruction[0].CacheHint))
	require.JSONEq(t, `{"type":"ephemeral","ttl":"1h"}`, string(restored.Messages[0].Content[0].CacheHint))
}

func TestDecodeRequestRejectsMultipleJSONValues(t *testing.T) {
	_, _, err := New().DecodeRequest([]byte(`{"model":"model","input":"hello"}{}`), protocolconv.Options{})
	require.ErrorContains(t, err, "multiple JSON values")
}

func TestRequestRoundTripPreservesExtendedToolSemantics(t *testing.T) {
	body := []byte(`{
		"model":"gpt-5.4",
		"input":[
			{"type":"custom_tool_call","call_id":"custom-1","name":"exec","input":"dir /b"},
			{"type":"custom_tool_call_output","call_id":"custom-1","output":"main.go"},
			{"type":"tool_search_call","call_id":"search-1","name":"tool_search","arguments":{"query":"gmail"}},
			{"type":"tool_search_output","call_id":"search-1","output":{"groups":["gmail"]}},
			{"type":"function_call","call_id":"ns-1","namespace":"gmail","name":"send","arguments":"{\"to\":\"a@example.com\"}"},
			{"type":"function_call_output","call_id":"ns-1","output":{"ok":true}}
		],
		"tools":[
			{"type":"custom","name":"exec"},
			{"type":"tool_search"},
			{"type":"namespace","name":"gmail","tools":[{"type":"function","name":"send","parameters":{"type":"object"}}]}
		]
	}`)
	converter := New()
	options := protocolconv.Options{LossPolicy: protocolconv.LossError}

	request, warnings, err := converter.DecodeRequest(body, options)
	require.NoError(t, err)
	require.Empty(t, warnings)
	require.Len(t, request.Tools, 3)
	require.Equal(t, "custom", request.Tools[0].ProviderType)
	require.Equal(t, "tool_search", request.Tools[1].ProviderType)
	require.Equal(t, "namespace", request.Tools[2].ProviderType)
	require.Len(t, request.Tools[2].Children, 1)
	require.Equal(t, "gmail", request.Tools[2].Children[0].Namespace)
	require.Equal(t, "send", request.Tools[2].Children[0].Name)

	require.Equal(t, "custom_tool_call", request.Messages[0].Content[0].ToolKind)
	require.JSONEq(t, `"dir /b"`, string(request.Messages[0].Content[0].ToolInput))
	require.Equal(t, "custom_tool_call", request.Messages[1].Content[0].ToolKind)
	require.Equal(t, "tool_search_call", request.Messages[2].Content[0].ToolKind)
	require.JSONEq(t, `{"query":"gmail"}`, string(request.Messages[2].Content[0].ToolInput))
	require.Equal(t, "tool_search_call", request.Messages[3].Content[0].ToolKind)
	require.JSONEq(t, `{"groups":["gmail"]}`, string(request.Messages[3].Content[0].ToolResult))
	require.Equal(t, "gmail", request.Messages[4].Content[0].ToolNamespace)

	encoded, warnings, err := converter.EncodeRequest(request, options)
	require.NoError(t, err)
	require.Empty(t, warnings)
	require.Equal(t, "custom_tool_call", gjson.GetBytes(encoded, "input.0.type").String())
	require.Equal(t, "dir /b", gjson.GetBytes(encoded, "input.0.input").String())
	require.Equal(t, "custom_tool_call_output", gjson.GetBytes(encoded, "input.1.type").String())
	require.Equal(t, "tool_search_call", gjson.GetBytes(encoded, "input.2.type").String())
	require.True(t, gjson.GetBytes(encoded, "input.2.arguments").IsObject())
	require.True(t, gjson.GetBytes(encoded, "input.3.output").IsObject())
	require.Equal(t, "gmail", gjson.GetBytes(encoded, "input.4.namespace").String())
	require.Equal(t, "namespace", gjson.GetBytes(encoded, "tools.2.type").String())
	require.Equal(t, "send", gjson.GetBytes(encoded, "tools.2.tools.0.name").String())
}

func TestRequestRoundTripPreservesMultimodalToolResult(t *testing.T) {
	body := []byte(`{
		"model":"gpt-5.4",
		"input":[
			{"type":"function_call","call_id":"call-image","name":"inspect","arguments":"{}"},
			{"type":"function_call_output","call_id":"call-image","output":[
				{"type":"input_text","text":"chart"},
				{"type":"input_image","image_url":"data:image/png;base64,AAAA"}
			]}
		]
	}`)
	converter := New()
	options := protocolconv.Options{LossPolicy: protocolconv.LossError}

	request, warnings, err := converter.DecodeRequest(body, options)
	require.NoError(t, err)
	require.Empty(t, warnings)
	require.Len(t, request.Messages, 2)
	result := request.Messages[1].Content[0]
	require.Empty(t, result.ToolResult)
	require.Len(t, result.ToolResultContent, 2)
	require.Equal(t, "chart", result.ToolResultContent[0].Text)
	require.Equal(t, "image/png", result.ToolResultContent[1].MediaType)
	require.Equal(t, "AAAA", result.ToolResultContent[1].Data)

	encoded, warnings, err := converter.EncodeRequest(request, options)
	require.NoError(t, err)
	require.Empty(t, warnings)
	require.Equal(t, "input_text", gjson.GetBytes(encoded, "input.1.output.0.type").String())
	require.Equal(t, "chart", gjson.GetBytes(encoded, "input.1.output.0.text").String())
	require.Equal(t, "data:image/png;base64,AAAA", gjson.GetBytes(encoded, "input.1.output.1.image_url").String())
}

func TestToolChoiceAnyUsesResponsesRequiredWireValue(t *testing.T) {
	converter := New()
	options := protocolconv.Options{LossPolicy: protocolconv.LossError}
	request, warnings, err := converter.DecodeRequest([]byte(`{
		"model":"gpt-5.4",
		"input":"hello",
		"tool_choice":"required"
	}`), options)
	require.NoError(t, err)
	require.Empty(t, warnings)
	require.NotNil(t, request.ToolChoice)
	require.Equal(t, "any", request.ToolChoice.Mode)

	encoded, warnings, err := converter.EncodeRequest(request, options)
	require.NoError(t, err)
	require.Empty(t, warnings)
	require.Equal(t, "required", gjson.GetBytes(encoded, "tool_choice").String())
}

func TestResponseRoundTripPreservesExtendedToolCalls(t *testing.T) {
	body := []byte(`{
		"id":"resp-tools",
		"object":"response",
		"model":"gpt-5.4",
		"status":"completed",
		"output":[
			{"type":"custom_tool_call","call_id":"custom-1","name":"exec","input":"dir /b","status":"completed"},
			{"type":"tool_search_call","call_id":"search-1","name":"tool_search","execution":"client","arguments":{"query":"gmail"},"status":"completed"},
			{"type":"function_call","call_id":"ns-1","namespace":"gmail","name":"send","arguments":"{\"to\":\"a@example.com\"}","status":"completed"}
		],
		"usage":{"input_tokens":4,"output_tokens":2,"total_tokens":6}
	}`)
	converter := New()
	options := protocolconv.Options{LossPolicy: protocolconv.LossError}

	response, warnings, err := converter.DecodeResponse(body, options)
	require.NoError(t, err)
	require.Empty(t, warnings)
	require.Len(t, response.Choices, 1)
	require.Len(t, response.Choices[0].Message.Content, 3)
	require.Equal(t, "custom_tool_call", response.Choices[0].Message.Content[0].ToolKind)
	require.JSONEq(t, `"dir /b"`, string(response.Choices[0].Message.Content[0].ToolInput))
	require.Equal(t, "tool_search_call", response.Choices[0].Message.Content[1].ToolKind)
	require.JSONEq(t, `{"query":"gmail"}`, string(response.Choices[0].Message.Content[1].ToolInput))
	require.Equal(t, "gmail", response.Choices[0].Message.Content[2].ToolNamespace)

	encoded, warnings, err := converter.EncodeResponse(response, options)
	require.NoError(t, err)
	require.Empty(t, warnings)
	require.Equal(t, "custom_tool_call", gjson.GetBytes(encoded, "output.0.type").String())
	require.Equal(t, "dir /b", gjson.GetBytes(encoded, "output.0.input").String())
	require.Equal(t, "tool_search_call", gjson.GetBytes(encoded, "output.1.type").String())
	require.True(t, gjson.GetBytes(encoded, "output.1.arguments").IsObject())
	require.Equal(t, "client", gjson.GetBytes(encoded, "output.1.execution").String())
	require.Equal(t, "gmail", gjson.GetBytes(encoded, "output.2.namespace").String())
}

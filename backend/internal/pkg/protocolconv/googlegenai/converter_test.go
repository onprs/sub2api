package googlegenai

import (
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv"
	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv/ir"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestGoogleStreamEncoderRestoresRequestScopedResponseModel(t *testing.T) {
	encoder := New().NewStreamEncoderWithOptions(protocolconv.Options{ResponseModel: "client-model"})
	events := []ir.StreamEvent{
		{Type: ir.EventStreamStart, ResponseID: "resp_1", Model: "upstream-model"},
		{Type: ir.EventTextDelta, ChoiceIndex: 0, Text: "ok"},
		{Type: ir.EventFinish, FinishReason: &ir.FinishReason{Reason: "stop"}},
		{Type: ir.EventStreamEnd},
	}
	var payloads [][]byte
	for _, event := range events {
		out, _, err := encoder.Encode(event)
		require.NoError(t, err)
		payloads = append(payloads, out...)
	}
	require.Len(t, payloads, 2)
	for _, payload := range payloads {
		var wire responseWire
		require.NoError(t, json.Unmarshal(payload, &wire))
		require.Equal(t, "client-model", wire.ModelVersion)
	}
}

func TestEncodeRequestMapsServerSearchOutsideFunctionDeclarations(t *testing.T) {
	maxTokens := 16
	request := &ir.Request{
		Model:    "gemini-test",
		Messages: []ir.Message{{Role: ir.RoleUser, Content: []ir.ContentPart{{Type: ir.ContentText, Text: "search"}}}},
		Tools: []ir.ToolDefinition{
			{Type: "function", Name: "get_weather", Parameters: []byte(`{"type":"object"}`)},
			{Type: "function", ProviderType: "web_search_20250305", Name: "web_search"},
		},
		Generation: ir.GenerationConfig{MaxTokens: &maxTokens},
	}

	body, warnings, err := New().EncodeRequest(request, protocolconv.Options{})
	require.NoError(t, err)
	require.Empty(t, warnings)
	require.Equal(t, int64(2), gjson.GetBytes(body, "tools.#").Int())
	require.Equal(t, "get_weather", gjson.GetBytes(body, "tools.0.functionDeclarations.0.name").String())
	require.True(t, gjson.GetBytes(body, "tools.1.googleSearch").Exists())
	require.False(t, gjson.GetBytes(body, "tools.1.functionDeclarations").Exists())
}

func TestEncodeRequestWrapsScalarFunctionResponseInStruct(t *testing.T) {
	request := &ir.Request{
		Model: "gemini-test",
		Messages: []ir.Message{
			{Role: ir.RoleTool, Content: []ir.ContentPart{{Type: ir.ContentToolResult, ToolCallID: "call-1", ToolName: "read_file", ToolResult: []byte(`"demo content"`)}}},
		},
	}

	body, warnings, err := New().EncodeRequest(request, protocolconv.Options{})
	require.NoError(t, err)
	require.Empty(t, warnings)
	require.Equal(t, "call-1", gjson.GetBytes(body, "contents.0.parts.0.functionResponse.id").String())
	require.Equal(t, "read_file", gjson.GetBytes(body, "contents.0.parts.0.functionResponse.name").String())
	require.Equal(t, "demo content", gjson.GetBytes(body, "contents.0.parts.0.functionResponse.response.content").String())
}

func TestEncodeRequestPreservesObjectFunctionResponse(t *testing.T) {
	request := &ir.Request{
		Model: "gemini-test",
		Messages: []ir.Message{
			{Role: ir.RoleTool, Content: []ir.ContentPart{{Type: ir.ContentToolResult, ToolCallID: "call-1", ToolName: "read_file", ToolResult: []byte(`{"output":"demo content"}`)}}},
		},
	}

	body, warnings, err := New().EncodeRequest(request, protocolconv.Options{})
	require.NoError(t, err)
	require.Empty(t, warnings)
	require.Equal(t, "demo content", gjson.GetBytes(body, "contents.0.parts.0.functionResponse.response.output").String())
	require.False(t, gjson.GetBytes(body, "contents.0.parts.0.functionResponse.response.content").Exists())
}

func TestDecodeRequestPreservesGoogleSearchProviderType(t *testing.T) {
	request, _, err := New().DecodeRequest([]byte(`{"contents":[{"role":"user","parts":[{"text":"search"}]}],"tools":[{"googleSearch":{}}]}`), protocolconv.Options{SourceModel: "gemini-test"})
	require.NoError(t, err)
	require.Len(t, request.Tools, 1)
	require.Equal(t, "google_search", request.Tools[0].ProviderType)
}

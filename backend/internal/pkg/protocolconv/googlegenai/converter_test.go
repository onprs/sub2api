package googlegenai

import (
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv"
	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv/ir"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestEncodeResponseDoesNotLeakForeignProviderFinishReason(t *testing.T) {
	response := &ir.Response{
		ID: "resp_1", Model: "client-model", Status: "completed",
		Choices: []ir.Choice{{
			Index: 0, Message: ir.Message{Role: ir.RoleAssistant, Content: []ir.ContentPart{{Type: ir.ContentText, Text: "ok"}}},
			FinishReason: ir.FinishReason{Reason: "stop", ProviderReason: "end_turn"},
		}},
	}
	body, warnings, err := New().EncodeResponse(response, protocolconv.Options{})
	require.NoError(t, err)
	require.Empty(t, warnings)
	require.Equal(t, "STOP", gjson.GetBytes(body, "candidates.0.finishReason").String())
}

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

func TestDecodeResponseSynthesizesMissingFunctionCallIDs(t *testing.T) {
	response, _, err := New().DecodeResponse([]byte(`{
		"candidates":[{"content":{"role":"model","parts":[
			{"functionCall":{"name":"first","args":{"x":1}}},
			{"functionCall":{"name":"second","args":{"y":2}}}
		]},"finishReason":"STOP"}]
	}`), protocolconv.Options{SourceModel: "upstream-model"})
	require.NoError(t, err)
	require.Len(t, response.Choices, 1)
	require.Len(t, response.Choices[0].Message.Content, 2)
	require.Equal(t, "call_google_0_0", response.Choices[0].Message.Content[0].ToolCallID)
	require.Equal(t, "call_google_0_1", response.Choices[0].Message.Content[1].ToolCallID)
	require.Equal(t, "tool_calls", response.Choices[0].FinishReason.Reason)
}

func TestStreamDecoderSynthesizesMissingFunctionCallID(t *testing.T) {
	decoder := New().NewStreamDecoder()
	events, _, err := decoder.Decode([]byte(`{
		"candidates":[{"content":{"role":"model","parts":[{"functionCall":{"name":"lookup","args":{"q":"x"}}}]},"finishReason":"STOP"}],
		"usageMetadata":{"promptTokenCount":2,"candidatesTokenCount":1}
	}`))
	require.NoError(t, err)
	var startID, deltaID, endID, finishReason string
	for _, event := range events {
		switch event.Type {
		case ir.EventToolCallStart:
			startID = event.ToolCallID
		case ir.EventToolCallDelta:
			deltaID = event.ToolCallID
		case ir.EventToolCallEnd:
			endID = event.ToolCallID
		case ir.EventFinish:
			finishReason = event.FinishReason.Reason
		}
	}
	require.Equal(t, "call_google_0_0", startID)
	require.Equal(t, startID, deltaID)
	require.Equal(t, startID, endID)
	require.Equal(t, "tool_calls", finishReason)
	require.NoError(t, func() error { _, _, err := decoder.Finalize(); return err }())
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

func TestRequestRoundTripPreservesMultimodalFunctionResponse(t *testing.T) {
	request := &ir.Request{
		Model: "gemini-test",
		Messages: []ir.Message{{Role: ir.RoleTool, Content: []ir.ContentPart{{
			Type:       ir.ContentToolResult,
			ToolCallID: "call-image",
			ToolName:   "inspect",
			ToolResultContent: []ir.ContentPart{
				{Type: ir.ContentText, Text: "chart"},
				{Type: ir.ContentImage, MediaType: "image/png", Data: "AAAA"},
			},
		}}}},
	}
	converter := New()
	body, warnings, err := converter.EncodeRequest(request, protocolconv.Options{LossPolicy: protocolconv.LossError})
	require.NoError(t, err)
	require.Empty(t, warnings)
	require.Equal(t, "chart", gjson.GetBytes(body, "contents.0.parts.0.functionResponse.response.content.0.text").String())
	require.Equal(t, "image", gjson.GetBytes(body, "contents.0.parts.0.functionResponse.response.content.1.type").String())
	require.Equal(t, "image/png", gjson.GetBytes(body, "contents.0.parts.1.inlineData.mimeType").String())
	require.Equal(t, "AAAA", gjson.GetBytes(body, "contents.0.parts.1.inlineData.data").String())

	restored, warnings, err := converter.DecodeRequest(body, protocolconv.Options{SourceModel: "gemini-test", LossPolicy: protocolconv.LossError})
	require.NoError(t, err)
	require.Empty(t, warnings)
	require.Len(t, restored.Messages, 1)
	require.Len(t, restored.Messages[0].Content, 1)
	result := restored.Messages[0].Content[0]
	require.Empty(t, result.ToolResult)
	require.Len(t, result.ToolResultContent, 2)
	require.Equal(t, "chart", result.ToolResultContent[0].Text)
	require.Equal(t, "image/png", result.ToolResultContent[1].MediaType)
	require.Equal(t, "AAAA", result.ToolResultContent[1].Data)
}

func TestDecodeRequestPreservesFunctionResponseError(t *testing.T) {
	request, warnings, err := New().DecodeRequest([]byte(`{
		"contents":[{"role":"user","parts":[{"functionResponse":{"id":"call-1","name":"lookup","response":{"error":"failed"}}}]}]
	}`), protocolconv.Options{SourceModel: "gemini-test"})
	require.NoError(t, err)
	require.Empty(t, warnings)
	require.Len(t, request.Messages, 1)
	result := request.Messages[0].Content[0]
	require.True(t, result.IsError)
	require.JSONEq(t, `"failed"`, string(result.ToolResult))

	body, warnings, err := New().EncodeRequest(request, protocolconv.Options{})
	require.NoError(t, err)
	require.Empty(t, warnings)
	require.Equal(t, "failed", gjson.GetBytes(body, "contents.0.parts.0.functionResponse.response.error").String())
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

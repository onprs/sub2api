package openaichat

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv"
	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv/ir"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestDecodeRequestRejectsMultipleJSONValues(t *testing.T) {
	_, _, err := New().DecodeRequest([]byte(`{"model":"model","messages":[]}{}`), protocolconv.Options{})
	require.ErrorContains(t, err, "multiple JSON values")
}

func TestEncodeRequestRejectsSignatureInStrictMode(t *testing.T) {
	request := &ir.Request{Model: "model", Messages: []ir.Message{{Role: ir.RoleAssistant, Content: []ir.ContentPart{{Type: ir.ContentReasoning, Reasoning: "plan", Signature: "sig"}}}}}
	_, _, err := New().EncodeRequest(request, protocolconv.Options{LossPolicy: protocolconv.LossError})
	var conversionErr *protocolconv.Error
	require.ErrorAs(t, err, &conversionErr)
	require.Equal(t, protocolconv.ErrorUnsupportedCapability, conversionErr.Code)
	require.Equal(t, protocolconv.CapabilitySignature, conversionErr.Capability)
}

func TestEncodeRequestPreservesMultiPartSystemCacheControl(t *testing.T) {
	request := &ir.Request{
		Model: "model",
		SystemInstruction: []ir.ContentPart{
			{Type: ir.ContentText, Text: "base rules\n"},
			{Type: ir.ContentText, Text: "cached context", CacheHint: []byte(`{"type":"ephemeral"}`)},
		},
		Messages: []ir.Message{{Role: ir.RoleUser, Content: []ir.ContentPart{{
			Type: ir.ContentText, Text: "base rules\n", CacheHint: []byte(`{"type":"ephemeral","ttl":"1h"}`),
		}}}},
	}

	body, warnings, err := New().EncodeRequest(request, protocolconv.Options{
		LossPolicy:     protocolconv.LossError,
		ChatExtensions: protocolconv.ChatExtensions{AnthropicCacheControl: true},
	})
	require.NoError(t, err)
	require.Empty(t, warnings)
	require.Equal(t, "system", gjson.GetBytes(body, "messages.0.role").String())
	require.Equal(t, int64(2), gjson.GetBytes(body, "messages.0.content.#").Int())
	require.Equal(t, "base rules\n", gjson.GetBytes(body, "messages.0.content.0.text").String())
	require.False(t, gjson.GetBytes(body, "messages.0.content.0.cache_control").Exists())
	require.Equal(t, "cached context", gjson.GetBytes(body, "messages.0.content.1.text").String())
	require.Equal(t, "ephemeral", gjson.GetBytes(body, "messages.0.content.1.cache_control.type").String())
	require.Equal(t, "user", gjson.GetBytes(body, "messages.1.role").String())
	require.Equal(t, "base rules\n", gjson.GetBytes(body, "messages.1.content.0.text").String())
	require.Equal(t, "1h", gjson.GetBytes(body, "messages.1.content.0.cache_control.ttl").String())
}

func TestEncodeRequestPreservesMergedMessageCacheControl(t *testing.T) {
	request := &ir.Request{
		Model: "model",
		Messages: []ir.Message{{Role: ir.RoleUser, Content: []ir.ContentPart{
			{Type: ir.ContentText, Text: "first context block"},
			{Type: ir.ContentText, Text: "cached tail", CacheHint: []byte(`{"type":"ephemeral"}`)},
		}}},
	}

	body, warnings, err := New().EncodeRequest(request, protocolconv.Options{
		LossPolicy:     protocolconv.LossError,
		ChatExtensions: protocolconv.ChatExtensions{AnthropicCacheControl: true},
	})
	require.NoError(t, err)
	require.Empty(t, warnings)
	require.Equal(t, "user", gjson.GetBytes(body, "messages.0.role").String())
	require.Equal(t, int64(2), gjson.GetBytes(body, "messages.0.content.#").Int())
	require.Equal(t, "first context block", gjson.GetBytes(body, "messages.0.content.0.text").String())
	require.False(t, gjson.GetBytes(body, "messages.0.content.0.cache_control").Exists())
	require.Equal(t, "cached tail", gjson.GetBytes(body, "messages.0.content.1.text").String())
	require.Equal(t, "ephemeral", gjson.GetBytes(body, "messages.0.content.1.cache_control.type").String())
}

func TestStreamEncoderRejectsReasoningSignatureInStrictMode(t *testing.T) {
	encoder := newStreamEncoderWithOptions(protocolconv.Options{LossPolicy: protocolconv.LossError})
	_, warnings, err := encoder.Encode(ir.StreamEvent{
		Type: ir.EventReasoningDelta, BlockIndex: 0, Reasoning: "plan", Signature: "sig",
	})
	var conversionErr *protocolconv.Error
	require.ErrorAs(t, err, &conversionErr)
	require.Empty(t, warnings)
	require.Equal(t, protocolconv.ErrorUnsupportedCapability, conversionErr.Code)
	require.Equal(t, protocolconv.CapabilitySignature, conversionErr.Capability)
	require.Equal(t, "stream.reasoning.signature", conversionErr.Path)
}

func TestStreamEncoderWarnsAndDropsReasoningSignature(t *testing.T) {
	encoder := newStreamEncoderWithOptions(protocolconv.Options{LossPolicy: protocolconv.LossWarn})
	payloads, warnings, err := encoder.Encode(ir.StreamEvent{
		Type: ir.EventReasoningDelta, BlockIndex: 0, Reasoning: "plan", Signature: "sig",
	})
	require.NoError(t, err)
	require.Len(t, warnings, 1)
	require.Equal(t, protocolconv.WarningDroppedField, warnings[0].Code)
	require.Equal(t, protocolconv.CapabilitySignature, warnings[0].Capability)
	require.Len(t, payloads, 1)
	require.Equal(t, "plan", gjson.GetBytes(payloads[0], "choices.0.delta.reasoning_content").String())
	require.NotContains(t, string(payloads[0]), "sig")
	require.NotContains(t, string(payloads[0]), "signature")
}

func TestRequestRoundTripPreservesMultimodalToolResult(t *testing.T) {
	request := &ir.Request{
		Model: "model",
		Messages: []ir.Message{
			{Role: ir.RoleAssistant, Content: []ir.ContentPart{{Type: ir.ContentToolCall, ToolCallID: "call-image", ToolName: "inspect", ToolInput: []byte(`{}`)}}},
			{Role: ir.RoleTool, Content: []ir.ContentPart{{
				Type:       ir.ContentToolResult,
				ToolCallID: "call-image",
				ToolResultContent: []ir.ContentPart{
					{Type: ir.ContentText, Text: "chart"},
					{Type: ir.ContentImage, MediaType: "image/png", Data: "AAAA"},
				},
			}}},
		},
	}
	converter := New()
	body, warnings, err := converter.EncodeRequest(request, protocolconv.Options{LossPolicy: protocolconv.LossError})
	require.NoError(t, err)
	require.Empty(t, warnings)
	require.Equal(t, int64(3), gjson.GetBytes(body, "messages.#").Int())
	require.Equal(t, "tool", gjson.GetBytes(body, "messages.1.role").String())
	require.Equal(t, "user", gjson.GetBytes(body, "messages.2.role").String())
	require.Equal(t, `<tool-content call-id="call-image">`, gjson.GetBytes(body, "messages.2.content.0.text").String())
	require.Equal(t, "image_url", gjson.GetBytes(body, "messages.2.content.2.type").String())
	require.Equal(t, "data:image/png;base64,AAAA", gjson.GetBytes(body, "messages.2.content.2.image_url.url").String())

	restored, warnings, err := converter.DecodeRequest(body, protocolconv.Options{LossPolicy: protocolconv.LossError})
	require.NoError(t, err)
	require.Empty(t, warnings)
	require.Len(t, restored.Messages, 2)
	result := restored.Messages[1].Content[0]
	require.Empty(t, result.ToolResult)
	require.Len(t, result.ToolResultContent, 2)
	require.Equal(t, "chart", result.ToolResultContent[0].Text)
	require.Equal(t, "image/png", result.ToolResultContent[1].MediaType)
	require.Equal(t, "AAAA", result.ToolResultContent[1].Data)
}

func TestDecodeRequestSeparatesReasoningFromVisibleText(t *testing.T) {
	body := []byte(`{"model":"model","messages":[{"role":"assistant","reasoning_content":"plan","content":"answer"}]}`)
	request, _, err := New().DecodeRequest(body, protocolconv.Options{})
	require.NoError(t, err)
	require.Len(t, request.Messages, 1)
	require.Equal(t, ir.ContentReasoning, request.Messages[0].Content[0].Type)
	require.Equal(t, "plan", request.Messages[0].Content[0].Reasoning)
	require.Equal(t, ir.ContentText, request.Messages[0].Content[1].Type)
	require.Equal(t, "answer", request.Messages[0].Content[1].Text)
}

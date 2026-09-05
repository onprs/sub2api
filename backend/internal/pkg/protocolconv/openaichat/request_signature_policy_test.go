package openaichat

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv"
	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv/ir"
	"github.com/stretchr/testify/require"
)

func TestRequestSignatureLossIsExplicitAndIsolated(t *testing.T) {
	request := &ir.Request{Model: "model", Messages: []ir.Message{{Role: ir.RoleAssistant, Content: []ir.ContentPart{
		{Type: ir.ContentReasoning, Reasoning: "check records", Signature: "test-opaque-signature"},
		{Type: ir.ContentText, Text: "answer"},
	}}}}
	options := protocolconv.Options{AllowChatRequestSignatureLoss: true}
	body, warnings, err := New().EncodeRequest(request, options)
	require.NoError(t, err)
	require.Len(t, warnings, 1)
	require.Equal(t, protocolconv.WarningDroppedField, warnings[0].Code)
	require.Equal(t, protocolconv.CapabilitySignature, warnings[0].Capability)
	require.Equal(t, "messages[0].content[0].signature", warnings[0].Path)
	require.NotContains(t, string(body), "test-opaque-signature")
	require.Contains(t, string(body), "check records")
	require.Contains(t, string(body), "answer")
	require.Equal(t, "test-opaque-signature", request.Messages[0].Content[0].Signature)

	_, _, err = New().EncodeRequest(request, protocolconv.Options{})
	require.Error(t, err)
	response := &ir.Response{ID: "resp-test", Model: "model", Status: "completed", Choices: []ir.Choice{{Message: request.Messages[0], FinishReason: ir.FinishReason{Reason: "stop"}}}}
	_, _, err = New().EncodeResponse(response, options)
	require.Error(t, err)
	_, _, err = New().NewStreamEncoderWithOptions(options).Encode(ir.StreamEvent{Type: ir.EventReasoningDelta, Reasoning: "check records", Signature: "test-opaque-signature"})
	require.Error(t, err)

	request.Messages[0].Content[1].CacheHint = []byte(`{"type":"ephemeral"}`)
	_, _, err = New().EncodeRequest(request, options)
	var conversionErr *protocolconv.Error
	require.ErrorAs(t, err, &conversionErr)
	require.Equal(t, protocolconv.CapabilityCacheControl, conversionErr.Capability)
}

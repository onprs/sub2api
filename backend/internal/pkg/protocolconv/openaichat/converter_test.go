package openaichat

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv"
	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv/ir"
	"github.com/stretchr/testify/require"
)

func TestEncodeRequestRejectsSignatureInStrictMode(t *testing.T) {
	request := &ir.Request{Model: "model", Messages: []ir.Message{{Role: ir.RoleAssistant, Content: []ir.ContentPart{{Type: ir.ContentReasoning, Reasoning: "plan", Signature: "sig"}}}}}
	_, _, err := New().EncodeRequest(request, protocolconv.Options{LossPolicy: protocolconv.LossError})
	var conversionErr *protocolconv.Error
	require.ErrorAs(t, err, &conversionErr)
	require.Equal(t, protocolconv.ErrorUnsupportedCapability, conversionErr.Code)
	require.Equal(t, protocolconv.CapabilitySignature, conversionErr.Capability)
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

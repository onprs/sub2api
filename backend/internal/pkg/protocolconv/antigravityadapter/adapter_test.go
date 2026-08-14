package antigravityadapter

import (
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv"
	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv/standard"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestConvertRequestRequiresExplicitFamilyAndEnvelopeFields(t *testing.T) {
	_, _, err := ConvertRequest([]byte(`{}`), protocolconv.ProtocolGoogleGenAI, Options{})
	require.ErrorContains(t, err, "family")

	_, _, err = ConvertRequest([]byte(`{}`), protocolconv.ProtocolGoogleGenAI, Options{Family: FamilyGemini})
	require.ErrorContains(t, err, "project ID")
}

func TestConvertClaudeFamilyUsesVendorTransformer(t *testing.T) {
	body := []byte(`{"model":"client-model","max_tokens":100,"system":"stable","messages":[{"role":"user","content":"hello"}]}`)
	converted, warnings, err := ConvertRequest(body, protocolconv.ProtocolAnthropic, Options{
		Family: FamilyClaude, ProjectID: "project-1", SourceModel: "claude-sonnet-4-6", WireModel: "claude-sonnet-4-6",
	})
	require.NoError(t, err)
	require.Empty(t, warnings)
	require.Equal(t, "project-1", gjson.GetBytes(converted, "project").String())
	require.Equal(t, "claude-sonnet-4-6", gjson.GetBytes(converted, "model").String())
	require.Equal(t, "agent", gjson.GetBytes(converted, "requestType").String())
	require.Contains(t, gjson.GetBytes(converted, "request.systemInstruction.parts.0.text").String(), "<identity>")
}

func TestConvertClaudeFamilyRestoresGoogleToolSignaturesByID(t *testing.T) {
	body := []byte(`{
		"contents":[
			{"role":"model","parts":[
				{"functionCall":{"id":"call-a","name":"lookup","args":{"q":"a"}},"thoughtSignature":"signature-a"},
				{"functionCall":{"id":"call-b","name":"lookup","args":{"q":"b"}},"thoughtSignature":"signature-b"}
			]},
			{"role":"user","parts":[
				{"functionResponse":{"id":"call-a","name":"lookup","response":{"result":"a"}}},
				{"functionResponse":{"id":"call-b","name":"lookup","response":{"result":"b"}}}
			]}
		]
	}`)
	converted, warnings, err := ConvertRequest(body, protocolconv.ProtocolGoogleGenAI, Options{
		Family: FamilyClaude, ProjectID: "project-1", SourceModel: "claude-sonnet-4-6", WireModel: "claude-sonnet-4-6",
	})
	require.NoError(t, err)
	require.Empty(t, warnings)
	require.Equal(t, "signature-a", gjson.GetBytes(converted, "request.contents.0.parts.0.thoughtSignature").String())
	require.Equal(t, "signature-b", gjson.GetBytes(converted, "request.contents.0.parts.1.thoughtSignature").String())

	converted, warnings, err = ConvertRequest(body, protocolconv.ProtocolGoogleGenAI, Options{
		Family: FamilyClaude, ProjectID: "project-1", SourceModel: "claude-sonnet-4-6", WireModel: "claude-sonnet-4-6", RectifySignatures: true,
	})
	require.NoError(t, err)
	require.Empty(t, warnings)
	require.Empty(t, gjson.GetBytes(converted, "request.contents.0.parts.0.thoughtSignature").String())
	require.Empty(t, gjson.GetBytes(converted, "request.contents.0.parts.1.thoughtSignature").String())
}

func TestConvertGeminiFamilyAppliesVendorPolicyOnlyInAdapter(t *testing.T) {
	body := []byte(`{
		"contents":[
			{"role":"user","parts":[{"text":"hello"}]},
			{"role":"model","parts":[{"text":"thinking","thought":true,"thoughtSignature":"bad-signature"}]}
		],
		"tools":[{"functionDeclarations":[{"name":"read","parameters":{"type":"object","$defs":{"unused":{"type":"string"}},"additionalProperties":false}}]}]
	}`)
	converted, warnings, err := ConvertRequest(body, protocolconv.ProtocolGoogleGenAI, Options{
		Family: FamilyGemini, ProjectID: "project-1", SourceModel: "gemini-3.1-pro-high", WireModel: "gemini-pro-agent",
		RectifySignatures: true, RequestID: "request-1", UserAgent: "test-agent",
	})
	require.NoError(t, err)
	require.Empty(t, warnings)
	require.Equal(t, "request-1", gjson.GetBytes(converted, "requestId").String())
	require.Equal(t, "test-agent", gjson.GetBytes(converted, "userAgent").String())
	require.Equal(t, "gemini-pro-agent", gjson.GetBytes(converted, "model").String())
	require.Contains(t, gjson.GetBytes(converted, "request.systemInstruction.parts.0.text").String(), "<identity>")
	require.Equal(t, dummyThoughtSignature, gjson.GetBytes(converted, "request.contents.1.parts.0.thoughtSignature").String())
	require.False(t, gjson.GetBytes(converted, "request.tools.0.functionDeclarations.0.parameters.$defs").Exists())

	registry, err := standard.NewRegistry()
	require.NoError(t, err)
	standardBody, _, err := registry.ConvertRequest(body, protocolconv.ProtocolGoogleGenAI, protocolconv.ProtocolGoogleGenAI, protocolconv.Options{SourceModel: "gemini-3.1-pro-high"})
	require.NoError(t, err)
	require.Equal(t, body, standardBody)
	require.NotContains(t, string(standardBody), dummyThoughtSignature)
}

func TestConvertRequestFromChatToGeminiFamily(t *testing.T) {
	body := []byte(`{"model":"alias","messages":[{"role":"user","content":"hello"}]}`)
	converted, _, err := ConvertRequest(body, protocolconv.ProtocolOpenAIChat, Options{Family: FamilyGemini, ProjectID: "project-1", SourceModel: "gemini-3-flash", WireModel: "gemini-3-flash", RequestID: "request-1", UserAgent: "test"})
	require.NoError(t, err)
	var value map[string]any
	require.NoError(t, json.Unmarshal(converted, &value))
	require.Equal(t, "gemini-3-flash", value["model"])
	require.Equal(t, "hello", gjson.GetBytes(converted, "request.contents.0.parts.0.text").String())
}

package protocolconv

import (
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestIdentityConversionPreservesBytesForCacheAffinity(t *testing.T) {
	body := []byte(`{ "model" : "kimi-k2.7-code", "messages" : [ {"role":"user","content":"hi"} ] }`)
	out, err := ConvertRequest(body, ProtocolOpenAICompat, Target{Protocol: ProtocolOpenAICompat}, Options{})
	require.NoError(t, err)
	require.Equal(t, body, out)
}

func TestAnthropicToOpenAICompatKeepsKimiNativeControls(t *testing.T) {
	body := []byte(`{
		"model":"kimi-k2.7-code",
		"max_tokens":8192,
		"thinking":{"type":"enabled","budget_tokens":4096},
		"messages":[
			{"role":"user","content":"hi"},
			{"role":"assistant","content":[{"type":"thinking","thinking":"plan","signature":"sig-1"},{"type":"text","text":"ok"}]},
			{"role":"user","content":"continue"}
		]
	}`)
	out, err := ConvertRequest(body, ProtocolAnthropic, Target{Protocol: ProtocolOpenAICompat}, Options{})
	require.NoError(t, err)
	require.Equal(t, "kimi-k2.7-code", gjson.GetBytes(out, "model").String())
	require.Equal(t, int64(8192), gjson.GetBytes(out, "max_tokens").Int())
	require.False(t, gjson.GetBytes(out, "max_completion_tokens").Exists())
	require.False(t, gjson.GetBytes(out, "reasoning_effort").Exists())
	require.False(t, gjson.GetBytes(out, "thinking").Exists())
	require.False(t, gjson.GetBytes(out, "enable_thinking").Exists())
	require.Equal(t, "plan", gjson.GetBytes(out, "messages.1.reasoning_content").String())
}

func TestResponsesLowEffortEnablesAnthropicThinking(t *testing.T) {
	body := []byte(`{"model":"claude-sonnet-4-6","input":"hi","reasoning":{"effort":"low"}}`)
	out, err := ConvertRequest(body, ProtocolOpenAIResponses, Target{Protocol: ProtocolAnthropic}, Options{})
	require.NoError(t, err)
	require.Equal(t, "enabled", gjson.GetBytes(out, "thinking.type").String())
	require.Equal(t, int64(1024), gjson.GetBytes(out, "thinking.budget_tokens").Int())
	require.Equal(t, "low", gjson.GetBytes(out, "output_config.effort").String())
}

func TestAnthropicThinkingSignatureSurvivesResponsesRoundTrip(t *testing.T) {
	body := []byte(`{"model":"gpt-5","messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"plan","signature":"sig-1"},{"type":"text","text":"ok"}]},{"role":"user","content":"go"}],"max_tokens":1024}`)
	responses, err := ConvertRequest(body, ProtocolAnthropic, Target{Protocol: ProtocolOpenAIResponses}, Options{})
	require.NoError(t, err)
	require.Equal(t, "reasoning", gjson.GetBytes(responses, "input.0.type").String())
	require.Equal(t, "sig-1", gjson.GetBytes(responses, "input.0.encrypted_content").String())

	roundTrip, err := ConvertRequest(responses, ProtocolOpenAIResponses, Target{Protocol: ProtocolAnthropic}, Options{})
	require.NoError(t, err)
	require.Equal(t, "thinking", gjson.GetBytes(roundTrip, "messages.0.content.0.type").String())
	require.Equal(t, "plan", gjson.GetBytes(roundTrip, "messages.0.content.0.thinking").String())
	require.Equal(t, "sig-1", gjson.GetBytes(roundTrip, "messages.0.content.0.signature").String())
}

func TestAnthropicToGeminiPreservesToolsAndThoughtSignature(t *testing.T) {
	body := []byte(`{
		"model":"gemini-3-pro",
		"max_tokens":128,
		"messages":[
			{"role":"user","content":"hi"},
			{"role":"assistant","content":[{"type":"thinking","thinking":"plan","signature":"sig-1"},{"type":"tool_use","id":"call-1","name":"read_file","input":{"path":"a"}}]}
		],
		"tools":[{"name":"read_file","description":"read","input_schema":{"type":"object"}}]
	}`)
	out, err := ConvertRequest(body, ProtocolAnthropic, Target{Protocol: ProtocolGemini}, Options{})
	require.NoError(t, err)
	require.Equal(t, "plan", gjson.GetBytes(out, "contents.1.parts.0.text").String())
	require.True(t, gjson.GetBytes(out, "contents.1.parts.0.thought").Bool())
	require.Equal(t, "sig-1", gjson.GetBytes(out, "contents.1.parts.0.thoughtSignature").String())
	require.Equal(t, "read_file", gjson.GetBytes(out, "contents.1.parts.1.functionCall.name").String())
	require.Equal(t, "read_file", gjson.GetBytes(out, "tools.0.functionDeclarations.0.name").String())
}

func TestAntigravityEndpointFamilyMustBeExplicit(t *testing.T) {
	_, err := ConvertRequest([]byte(`{"contents":[]}`), ProtocolGemini, Target{
		Protocol:          ProtocolGemini,
		AntigravityFamily: "unknown",
	}, Options{})
	require.Error(t, err)

	body := []byte(`{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"hi"}],"max_tokens":128}`)
	out, err := ConvertAntigravityRequest(body, ProtocolAnthropic, AntigravityFamilyClaude, AntigravityOptions{
		ProjectID:        "project-1",
		MappedModel:      "claude-sonnet-4-6",
		TransformOptions: antigravity.DefaultTransformOptions(),
	})
	require.NoError(t, err)
	require.Equal(t, "project-1", gjson.GetBytes(out, "project").String())
	require.Equal(t, "claude-sonnet-4-6", gjson.GetBytes(out, "model").String())
}

func TestGeminiResponseRoundTripPreservesThinkingAndCacheUsage(t *testing.T) {
	body := []byte(`{
		"responseId":"gem-1",
		"modelVersion":"gemini-3-pro",
		"candidates":[{"content":{"role":"model","parts":[
			{"text":"plan","thought":true,"thoughtSignature":"sig-1"},
			{"text":"answer"}
		]},"finishReason":"STOP"}],
		"usageMetadata":{"promptTokenCount":100,"cachedContentTokenCount":60,"candidatesTokenCount":20,"thoughtsTokenCount":10,"totalTokenCount":130}
	}`)
	responses, err := ConvertResponse(body, ProtocolGemini, ProtocolOpenAIResponses, "gemini-3-pro")
	require.NoError(t, err)
	require.Equal(t, "plan", gjson.GetBytes(responses, "output.0.summary.0.text").String())
	require.Equal(t, "sig-1", gjson.GetBytes(responses, "output.0.encrypted_content").String())
	require.Equal(t, int64(60), gjson.GetBytes(responses, "usage.input_tokens_details.cached_tokens").Int())
	require.Equal(t, int64(10), gjson.GetBytes(responses, "usage.output_tokens_details.reasoning_tokens").Int())

	anthropic, err := ConvertResponse(responses, ProtocolOpenAIResponses, ProtocolAnthropic, "gemini-3-pro")
	require.NoError(t, err)
	require.Equal(t, "thinking", gjson.GetBytes(anthropic, "content.0.type").String())
	require.Equal(t, "plan", gjson.GetBytes(anthropic, "content.0.thinking").String())
	require.Equal(t, "answer", gjson.GetBytes(anthropic, "content.1.text").String())
}

func TestAllProtocolRequestsProduceValidJSON(t *testing.T) {
	cases := []struct {
		from Protocol
		to   Protocol
		body []byte
		opts Options
	}{
		{ProtocolAnthropic, ProtocolOpenAIResponses, []byte(`{"model":"gpt-5","messages":[{"role":"user","content":"hi"}],"max_tokens":128}`), Options{}},
		{ProtocolOpenAICompat, ProtocolAnthropic, []byte(`{"model":"claude","messages":[{"role":"user","content":"hi"}]}`), Options{}},
		{ProtocolOpenAIResponses, ProtocolGemini, []byte(`{"model":"gemini","input":"hi"}`), Options{}},
		{ProtocolGemini, ProtocolOpenAIResponses, []byte(`{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`), Options{Model: "gemini-3-pro"}},
	}
	for _, tc := range cases {
		out, err := ConvertRequest(tc.body, tc.from, Target{Protocol: tc.to}, tc.opts)
		require.NoError(t, err)
		require.True(t, json.Valid(out), "%s to %s: %s", tc.from, tc.to, out)
	}
}

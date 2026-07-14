package service

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestApplyOpenAICodexMessagesRequestPolicyAddsOnlyProviderPolicy(t *testing.T) {
	converted := []byte(`{
		"model":"source-model",
		"instructions":"pipeline instruction",
		"input":[{"role":"user","content":[{"type":"input_text","text":"hello"}]}],
		"max_output_tokens":16,
		"temperature":0.2,
		"top_p":0.8,
		"tools":[
			{"type":"function","name":"lookup","parameters":{"type":"object"}},
			{"type":"computer_20250124","name":"computer","parameters":{"type":"object","properties":{}}}
		]
	}`)
	system := json.RawMessage(`[
		{"type":"text","text":"x-anthropic-billing-header: cc_version=test;"},
		{"type":"text","text":"project instructions","cache_control":{"type":"ephemeral"}}
	]`)

	body, err := applyOpenAICodexMessagesRequestPolicy(converted, system, "gpt-5.5")
	require.NoError(t, err)
	require.Equal(t, "gpt-5.5", gjson.GetBytes(body, "model").String())
	require.True(t, gjson.GetBytes(body, "stream").Bool())
	require.False(t, gjson.GetBytes(body, "store").Bool())
	require.True(t, gjson.GetBytes(body, "parallel_tool_calls").Bool())
	require.Equal(t, int64(openAICodexMessagesMinOutputTokens), gjson.GetBytes(body, "max_output_tokens").Int())
	require.False(t, gjson.GetBytes(body, "temperature").Exists())
	require.False(t, gjson.GetBytes(body, "top_p").Exists())
	require.Equal(t, "reasoning.encrypted_content", gjson.GetBytes(body, "include.0").String())
	require.Equal(t, "medium", gjson.GetBytes(body, "reasoning.effort").String())
	require.Equal(t, "auto", gjson.GetBytes(body, "reasoning.summary").String())
	require.Equal(t, "medium", gjson.GetBytes(body, "text.verbosity").String())
	require.Equal(t, "developer", gjson.GetBytes(body, "input.0.role").String())
	require.Equal(t, "project instructions", gjson.GetBytes(body, "input.0.content.0.text").String())
	require.NotContains(t, string(body), "x-anthropic-billing-header")
	require.False(t, gjson.GetBytes(body, "instructions").Exists())
	require.False(t, gjson.GetBytes(body, "tools.0.strict").Bool())
	require.True(t, gjson.GetBytes(body, "tools.0.parameters.properties").IsObject())
	require.Equal(t, "function", gjson.GetBytes(body, "tools.1.type").String())
}

func TestTrimOpenAICompatResponsesBodyToLatestTurnPreservesMultimodalOutput(t *testing.T) {
	var body map[string]any
	require.NoError(t, json.Unmarshal([]byte(`{
		"input":[
			{"type":"message","role":"developer","content":[{"type":"input_text","text":"system"}]},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"old"}]},
			{"type":"function_call","call_id":"call-image","name":"inspect","arguments":"{}"},
			{"type":"function_call_output","call_id":"call-image","output":[
				{"type":"input_text","text":"chart"},
				{"type":"input_image","image_url":"data:image/png;base64,AAAA"}
			]}
		]
	}`), &body))

	trimOpenAICompatResponsesBodyToLatestTurn(body)
	encoded, err := json.Marshal(body)
	require.NoError(t, err)
	require.Equal(t, int64(2), gjson.GetBytes(encoded, "input.#").Int())
	require.Equal(t, "function_call", gjson.GetBytes(encoded, "input.0.type").String())
	require.Equal(t, "function_call_output", gjson.GetBytes(encoded, "input.1.type").String())
	require.Equal(t, "input_image", gjson.GetBytes(encoded, "input.1.output.1.type").String())
	require.Equal(t, "data:image/png;base64,AAAA", gjson.GetBytes(encoded, "input.1.output.1.image_url").String())
}

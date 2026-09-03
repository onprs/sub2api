//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// TestStripEmptyChatToolCallIdentity_FirstChunkIdentityUntouched 首包带合法
// id/name 的 delta 必须原样保留（changed=false）。
func TestStripEmptyChatToolCallIdentity_FirstChunkIdentityUntouched(t *testing.T) {
	payload := []byte(`{"id":"chatcmpl_tool","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_example","type":"function","function":{"name":"web_search","arguments":""}}]}}]}`)
	rewritten, changed := stripEmptyChatToolCallIdentity(payload)
	require.False(t, changed)
	require.Equal(t, string(payload), string(rewritten))
	require.Equal(t, "call_example", gjson.GetBytes(rewritten, "choices.0.delta.tool_calls.0.id").String())
	require.Equal(t, "web_search", gjson.GetBytes(rewritten, "choices.0.delta.tool_calls.0.function.name").String())
	// 首包 arguments 为空串也不应被删除。
	require.True(t, gjson.GetBytes(rewritten, "choices.0.delta.tool_calls.0.function.arguments").Exists())
	require.Equal(t, "", gjson.GetBytes(rewritten, "choices.0.delta.tool_calls.0.function.arguments").String())
}

// TestStripEmptyChatToolCallIdentity_FollowUpDelta 后续参数 delta 的
// `"id":""` 与 `"function":{"name":""}` 应被删除；arguments 碎片、
// index、type 保留。
func TestStripEmptyChatToolCallIdentity_FollowingDelta(t *testing.T) {
	payload := []byte(`{"id":"chatcmpl_tool","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"","type":"function","function":{"name":"","arguments":"{\"query\":"}}]}}]}`)
	rewritten, changed := stripEmptyChatToolCallIdentity(payload)
	require.True(t, changed)
	require.False(t, gjson.GetBytes(rewritten, "choices.0.delta.tool_calls.0.id").Exists())
	require.False(t, gjson.GetBytes(rewritten, "choices.0.delta.tool_calls.0.function.name").Exists())
	require.Equal(t, `{"query":`, gjson.GetBytes(rewritten, "choices.0.delta.tool_calls.0.function.arguments").String())
	require.Equal(t, int64(0), gjson.GetBytes(rewritten, "choices.0.delta.tool_calls.0.index").Int())
	require.Equal(t, "function", gjson.GetBytes(rewritten, "choices.0.delta.tool_calls.0.type").String())
	// 空串字段不得再出现在 payload 里。
	require.NotContains(t, string(rewritten), `"id":""`)
	require.NotContains(t, string(rewritten), `"name":""`)
}

// TestStripEmptyChatToolCallIdentity_OnlyEmptyName / _OnlyEmptyID 只删
// 空的那一个，非空字段必须保留。
func TestStripEmptyChatToolCallIdentity_OnlyEmptyName(t *testing.T) {
	payload := []byte(`{"id":"chatcmpl_tool","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"","arguments":"{}"}}]}}]}`)
	rewritten, changed := stripEmptyChatToolCallIdentity(payload)
	require.True(t, changed)
	require.Equal(t, "call_1", gjson.GetBytes(rewritten, "choices.0.delta.tool_calls.0.id").String())
	require.False(t, gjson.GetBytes(rewritten, "choices.0.delta.tool_calls.0.function.name").Exists())
	require.Equal(t, "{}", gjson.GetBytes(rewritten, "choices.0.delta.tool_calls.0.function.arguments").String())
}

// TestStripEmptyChatToolCallIdentity_OnlyEmptyID 覆盖 `"id": ""` 带空格的
// JSON 形式，确认 gjson/sjson 均能识别。
func TestStripEmptyChatToolCallIdentity_OnlyEmptyID(t *testing.T) {
	payload := []byte(`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id" : "" , "type" : "function","function":{"name":"get_weather","arguments":"{}"}}]}}]}`)
	rewritten, changed := stripEmptyChatToolCallIdentity(payload)
	require.True(t, changed)
	require.False(t, gjson.GetBytes(rewritten, "choices.0.delta.tool_calls.0.id").Exists())
	require.Equal(t, "get_weather", gjson.GetBytes(rewritten, "choices.0.delta.tool_calls.0.function.name").String())
	require.Equal(t, "{}", gjson.GetBytes(rewritten, "choices.0.delta.tool_calls.0.function.arguments").String())
}

// TestStripEmptyChatToolCallIdentity_EmptyArgumentsKept 空 arguments 不删，
// 只有 id/name 空串被剔除。
func TestStripEmptyChatToolCallIdentity_EmptyArgumentsKept(t *testing.T) {
	payload := []byte(`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"","type":"function","function":{"name":"","arguments":""}}]}}]}`)
	rewritten, changed := stripEmptyChatToolCallIdentity(payload)
	require.True(t, changed)
	require.False(t, gjson.GetBytes(rewritten, "choices.0.delta.tool_calls.0.id").Exists())
	require.False(t, gjson.GetBytes(rewritten, "choices.0.delta.tool_calls.0.function.name").Exists())
	require.True(t, gjson.GetBytes(rewritten, "choices.0.delta.tool_calls.0.function.arguments").Exists())
	require.Equal(t, "", gjson.GetBytes(rewritten, "choices.0.delta.tool_calls.0.function.arguments").String())
}

// TestStripEmptyChatToolCallIdentity_TwoParallelToolCalls 两个并行 index 都要
// 处理：合法的 index 0 保留，后续参数 delta 的 index 1 剔除空 id/name。
func TestStripEmptyChatToolCallIdentity_TwoParallelToolCalls(t *testing.T) {
	payload := []byte(`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_a","type":"function","function":{"name":"tool_a","arguments":"{\"x\":"}},{"index":1,"id":"","type":"function","function":{"name":"","arguments":"{\"y\":"}}]}}]}`)
	rewritten, changed := stripEmptyChatToolCallIdentity(payload)
	require.True(t, changed)
	require.Equal(t, "call_a", gjson.GetBytes(rewritten, "choices.0.delta.tool_calls.0.id").String())
	require.Equal(t, "tool_a", gjson.GetBytes(rewritten, "choices.0.delta.tool_calls.0.function.name").String())
	require.Equal(t, `{"x":`, gjson.GetBytes(rewritten, "choices.0.delta.tool_calls.0.function.arguments").String())
	require.False(t, gjson.GetBytes(rewritten, "choices.0.delta.tool_calls.1.id").Exists())
	require.False(t, gjson.GetBytes(rewritten, "choices.0.delta.tool_calls.1.function.name").Exists())
	require.Equal(t, `{"y":`, gjson.GetBytes(rewritten, "choices.0.delta.tool_calls.1.function.arguments").String())
}

// TestStripEmptyChatToolCallIdentity_Passthrough 无 tool_calls、无 choices、
// 非数组 tool_calls、非法 JSON 一律原样返回。
func TestStripEmptyChatToolCallIdentity_Passthrough(t *testing.T) {
	tests := []struct {
		name    string
		payload string
	}{
		{"no tool_calls", `{"choices":[{"index":0,"delta":{"content":"hi"}}]}`},
		{"no choices", `{"id":"chatcmpl_x"}`},
		{"empty choices", `{"choices":[]}`},
		{"tool_calls not array", `{"choices":[{"index":0,"delta":{"tool_calls":{"foo":1}}}]}`},
		{"invalid JSON", `{"choices":[{`},
		{"empty string", ``},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rewritten, changed := stripEmptyChatToolCallIdentity([]byte(tt.payload))
			require.False(t, changed)
			require.Equal(t, tt.payload, string(rewritten))
		})
	}
}

// TestStripEmptyChatToolCallIdentity_DshClientMerge 模拟 dsh rc.2 adapter 的
// 合并逻辑（字段存在——含空串——才覆盖）：sanitize 后合并，最终 id/name 必须
// 仍是首包合法值，arguments 为各碎片拼接。
func TestStripEmptyChatToolCallIdentity_DshClientMerge(t *testing.T) {
	payloads := []string{
		`{"id":"chatcmpl_tool","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_example","type":"function","function":{"name":"web_search","arguments":""}}]}}]}`,
		`{"id":"chatcmpl_tool","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"","type":"function","function":{"name":"","arguments":"{\"query\":"}}]}}]}`,
		`{"id":"chatcmpl_tool","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"","type":"function","function":{"name":"","arguments":"\"example\"}"}}]}}]}`,
	}

	var mergedID, mergedName, mergedArgs string
	for _, payload := range payloads {
		sanitized, _ := stripEmptyChatToolCallIdentity([]byte(payload))
		for _, tc := range gjson.GetBytes(sanitized, "choices.0.delta.tool_calls").Array() {
			if v := tc.Get("id"); v.Exists() {
				mergedID = v.String()
			}
			if v := tc.Get("function.name"); v.Exists() {
				mergedName = v.String()
			}
			if v := tc.Get("function.arguments"); v.Exists() {
				mergedArgs += v.String()
			}
		}
	}

	require.Equal(t, "call_example", mergedID)
	require.Equal(t, "web_search", mergedName)
	require.Equal(t, `{"query":"example"}`, mergedArgs)
}

package apicompat

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Chat 历史 reasoning 默认折进 assistant 正文的 "<thinking>…</thinking>" 前缀是
// Grok / IR 桥约定的回传载体，这里锁定其兼容行为。
func TestChatCompletionsToResponses_DefaultKeepsThinkingText(t *testing.T) {
	req := &ChatCompletionsRequest{
		Model: "grok-4.5",
		Messages: []ChatMessage{
			{Role: "assistant", Content: json.RawMessage(`"answer"`), ReasoningContent: "plan"},
		},
	}
	resp, err := ChatCompletionsToResponses(req)
	require.NoError(t, err)
	var items []ResponsesInputItem
	require.NoError(t, json.Unmarshal(resp.Input, &items))
	require.Len(t, items, 1)
	var content []ResponsesContentPart
	require.NoError(t, json.Unmarshal(items[0].Content, &content))
	assert.Equal(t, "<thinking>plan</thinking>\nanswer", content[0].Text)
}

// 结构化变体把历史 reasoning 输出为独立 reasoning item：它不再进入 assistant
// 正文。GLM 等上游会通过 in-context learning 模仿正文里的推理标签，把思考写进
// 可见文本（zai-org/GLM-5#92），CN Anthropic 转换路径依赖本变体断掉该链路。
func TestChatCompletionsToResponsesWithReasoningItems_StructuredHistory(t *testing.T) {
	req := &ChatCompletionsRequest{
		Model: "glm-5.3-flash",
		Messages: []ChatMessage{
			{Role: "user", Content: json.RawMessage(`"读取目录"`)},
			{
				Role:             "assistant",
				Content:          json.RawMessage(`"我来看一下。"`),
				ReasoningContent: "需要先查看目录。",
				ToolCalls: []ChatToolCall{{
					ID: "call_1", Type: "function",
					Function: ChatFunctionCall{Name: "read", Arguments: `{"path":"."}`},
				}},
			},
			{Role: "tool", ToolCallID: "call_1", Content: json.RawMessage(`"a.go"`)},
			{Role: "assistant", ReasoningContent: "工具返回了文件列表。"},
			{Role: "user", Content: json.RawMessage(`"继续"`)},
		},
	}

	resp, err := ChatCompletionsToResponsesWithReasoningItems(req)
	require.NoError(t, err)

	var items []ResponsesInputItem
	require.NoError(t, json.Unmarshal(resp.Input, &items))
	// reasoning-only 的 assistant 不再产生空正文 message item。
	require.Len(t, items, 7)
	for i, want := range []struct{ typ, role string }{
		{"message", "user"},
		{"reasoning", ""},
		{"message", "assistant"},
		{"function_call", ""},
		{"function_call_output", ""},
		{"reasoning", ""},
		{"message", "user"},
	} {
		assert.Equal(t, want.typ, items[i].Type, "input item %d type", i)
		assert.Equal(t, want.role, items[i].Role, "input item %d role", i)
	}

	require.Len(t, items[1].Summary, 1)
	assert.Equal(t, "summary_text", items[1].Summary[0].Type)
	assert.Equal(t, "需要先查看目录。", items[1].Summary[0].Text)
	require.Len(t, items[5].Summary, 1)
	assert.Equal(t, "工具返回了文件列表。", items[5].Summary[0].Text)
	assert.Empty(t, items[1].EncryptedContent)

	var content []ResponsesContentPart
	require.NoError(t, json.Unmarshal(items[2].Content, &content))
	require.Len(t, content, 1)
	assert.Equal(t, "我来看一下。", content[0].Text)
	assert.NotContains(t, content[0].Text, "<thinking>")
	assert.Equal(t, "call_1", items[3].CallID)
	assert.Equal(t, "call_1", items[4].CallID)
}

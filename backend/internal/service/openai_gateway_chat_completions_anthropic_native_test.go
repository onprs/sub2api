//go:build unit

package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func chatNativeAnthropicTestContext(path string, body []byte) (*gin.Context, *httptest.ResponseRecorder) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	return c, recorder
}

func chatNativeAnthropicChunks(t *testing.T, sse string) []string {
	t.Helper()
	var out []string
	for _, line := range strings.Split(sse, "\n") {
		if !strings.HasPrefix(line, "data: ") || strings.Contains(line, "[DONE]") {
			continue
		}
		out = append(out, strings.TrimPrefix(line, "data: "))
	}
	return out
}

// pi 等 OpenAI 客户端会把上一轮的 thinking 放进 assistant 历史的
// reasoning_content 字段。该历史经 Chat → Responses → Anthropic 转换后必须
// 以结构化 reasoning 传递，不能被折成 "<thinking>…</thinking>" assistant 正文：
// GLM 等上游会通过 in-context learning 模仿正文中的推理标签，把思考输出到
// 可见正文（zai-org/GLM-5#92）。同时锁定 GLM-5.3 家族的参数规范化
// （effort/预算配对、采样参数清理）与工具回传。
func TestChatCompletionsViaNativeAnthropicStructuredReasoningHistory(t *testing.T) {
	gin.SetMode(gin.TestMode)

	reqBody := map[string]any{
		"model":            "glm-5.3-flash",
		"stream":           true,
		"reasoning_effort": "max",
		"thinking":         map[string]any{"type": "enabled", "clear_thinking": false},
		"max_tokens":       131072,
		"messages": []any{
			map[string]any{"role": "system", "content": "You are pi."},
			map[string]any{"role": "user", "content": "读取文件"},
			map[string]any{
				"role":              "assistant",
				"content":           "我来看一下。",
				"reasoning_content": "用户想读取文件，我需要调用 read 工具。",
				"tool_calls": []any{
					map[string]any{"id": "call_1", "type": "function", "function": map[string]any{"name": "read", "arguments": `{"path":"a.go"}`}},
				},
			},
			map[string]any{"role": "tool", "tool_call_id": "call_1", "content": "package main"},
		},
		"tools": []any{
			map[string]any{"type": "function", "function": map[string]any{
				"name":        "read",
				"description": "Read a file",
				"parameters":  map[string]any{"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string"}}, "required": []any{"path"}},
			}},
		},
		"temperature": 0.7,
		"top_p":       0.9,
	}
	body, _ := json.Marshal(reqBody)

	upstream := &httpUpstreamRecorder{resp: nativeAnthropicStreamResponse()}
	account := nativeAnthropicGLMTestAccount()
	account.Credentials["model_mapping"] = map[string]any{"*": "glm-5.3-flash"}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
	c, _ := chatNativeAnthropicTestContext("/v1/chat/completions", body)

	_, err := svc.forwardChatCompletionsViaNativeAnthropic(context.Background(), c, account, body, "")
	require.NoError(t, err)
	require.NotNil(t, upstream.lastBody)
	sent := string(upstream.lastBody)

	require.NotContains(t, sent, "<thinking>", "历史 reasoning 不得折成正文文本")
	require.NotContains(t, sent, "用户想读取文件")
	messages := gjson.Get(sent, "messages").Array()
	require.Len(t, messages, 3)
	require.Equal(t, "读取文件", messages[0].Get("content").String())
	require.Equal(t, "package main", messages[2].Get("content.0.content").String())

	assistant := messages[1].Get("content").Array()
	require.Len(t, assistant, 2)
	require.Equal(t, "text", assistant[0].Get("type").String())
	require.Equal(t, "我来看一下。", assistant[0].Get("text").String())
	require.Equal(t, "tool_use", assistant[1].Get("type").String())
	require.Equal(t, "call_1", assistant[1].Get("id").String())
	require.Equal(t, "read", assistant[1].Get("name").String())

	// GLM-5.3 家族：effort 与配对预算齐全，采样参数按上游契约清理。
	require.Equal(t, "enabled", gjson.Get(sent, "thinking.type").String())
	require.EqualValues(t, 32000, gjson.Get(sent, "thinking.budget_tokens").Int())
	require.Equal(t, "max", gjson.Get(sent, "output_config.effort").String())
	require.False(t, gjson.Get(sent, "temperature").Exists())
	require.False(t, gjson.Get(sent, "top_p").Exists())
	require.EqualValues(t, 131072, gjson.Get(sent, "max_tokens").Int())
	require.Equal(t, "read", gjson.Get(sent, "tools.0.name").String())
	require.Equal(t, "object", gjson.Get(sent, "tools.0.input_schema.type").String())
}

func chatNativeAnthropicThinkingVariantSSE(blockType, deltaType string) string {
	return fmt.Sprintf(`event: message_start
data: {"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"glm-5.3-flash","content":[],"usage":{"input_tokens":10,"output_tokens":1}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"%s"}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"%s","text":"这是思考内容","thinking":"这是思考内容"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: content_block_start
data: {"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"这是正文"}}

event: content_block_stop
data: {"type":"content_block_stop","index":1}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":20}}

event: message_stop
data: {"type":"message_stop"}

`, blockType, deltaType)
}

// Anthropic → Chat 的响应转换以 delta 类型归类：thinking_delta 进
// reasoning_content，text_delta 进 content。上游若不区分类型把思考塞进
// text_delta，就会表现为"思考进正文"。
func TestChatCompletionsFromNativeAnthropicThinkingDeltaRouting(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cases := []struct {
		name          string
		blockType     string
		deltaType     string
		wantReasoning bool
	}{
		{"thinking block + thinking delta", "thinking", "thinking_delta", true},
		{"thinking block + text delta", "thinking", "text_delta", false},
		{"text block + thinking delta", "text", "thinking_delta", true},
		{"text block + text delta", "text", "text_delta", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			upstream := &httpUpstreamRecorder{resp: &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body:       io.NopCloser(strings.NewReader(chatNativeAnthropicThinkingVariantSSE(tc.blockType, tc.deltaType))),
			}}
			body := []byte(`{"model":"glm-5.3-flash","messages":[{"role":"user","content":"hi"}],"stream":true,"reasoning_effort":"max"}`)
			account := nativeAnthropicGLMTestAccount()
			account.Credentials["model_mapping"] = map[string]any{"*": "glm-5.3-flash"}
			svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
			c, recorder := chatNativeAnthropicTestContext("/v1/chat/completions", body)

			_, err := svc.forwardChatCompletionsViaNativeAnthropic(context.Background(), c, account, body, "")
			require.NoError(t, err)

			var reasoning, content strings.Builder
			for _, payload := range chatNativeAnthropicChunks(t, recorder.Body.String()) {
				var chunk map[string]any
				require.NoError(t, json.Unmarshal([]byte(payload), &chunk))
				choices, _ := chunk["choices"].([]any)
				if len(choices) == 0 {
					continue
				}
				choice, _ := choices[0].(map[string]any)
				delta, _ := choice["delta"].(map[string]any)
				if v, ok := delta["reasoning_content"].(string); ok {
					reasoning.WriteString(v)
				}
				if v, ok := delta["content"].(string); ok {
					content.WriteString(v)
				}
			}
			if tc.wantReasoning {
				require.Equal(t, "这是思考内容", reasoning.String())
			} else {
				require.Empty(t, reasoning.String())
				require.Contains(t, content.String(), "这是思考内容")
			}
			require.Contains(t, content.String(), "这是正文")
		})
	}
}

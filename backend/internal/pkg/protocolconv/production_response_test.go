package protocolconv

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestProductionTextModelsResponseMatrixPreservesReasoningToolsAndCacheUsage(t *testing.T) {
	for _, model := range productionTextModels {
		model := model
		t.Run(model, func(t *testing.T) {
			for _, from := range productionProtocols {
				from := from
				source := productionResponseFixture(t, from, model)
				for _, to := range productionProtocols {
					to := to
					t.Run(fmt.Sprintf("%s_to_%s", from, to), func(t *testing.T) {
						converted, err := ConvertResponse(source, from, to, model)
						require.NoError(t, err)
						require.True(t, json.Valid(converted), "%s", converted)
						if from == to {
							require.Equal(t, source, converted)
						}
						requireResponseSemantics(t, to, converted, model, from != ProtocolAnthropic)
					})
				}
			}
		})
	}
}

func productionResponseFixture(t *testing.T, protocol Protocol, model string) []byte {
	t.Helper()
	var value any
	switch protocol {
	case ProtocolOpenAIResponses:
		value = map[string]any{
			"id": "resp-1", "object": "response", "status": "completed", "model": model,
			"output": []any{
				map[string]any{"type": "reasoning", "summary": []any{map[string]any{"type": "summary_text", "text": productionReasoning}}, "encrypted_content": "sig-1"},
				map[string]any{"type": "function_call", "call_id": "call-1", "name": productionToolName, "arguments": `{"path":"demo.txt"}`, "status": "completed"},
				map[string]any{"type": "message", "role": "assistant", "status": "completed", "content": []any{map[string]any{"type": "output_text", "text": "OK"}}},
			},
			"usage": map[string]any{
				"input_tokens": 100, "output_tokens": 30, "total_tokens": 130,
				"input_tokens_details":  map[string]any{"cached_tokens": 60},
				"output_tokens_details": map[string]any{"reasoning_tokens": 10},
			},
		}
	case ProtocolOpenAICompat:
		value = map[string]any{
			"id": "chatcmpl-1", "object": "chat.completion", "model": model,
			"choices": []any{map[string]any{
				"index": 0, "finish_reason": "tool_calls",
				"message": map[string]any{
					"role": "assistant", "content": "OK", "reasoning_content": productionReasoning,
					"tool_calls": []any{map[string]any{"id": "call-1", "type": "function", "function": map[string]any{"name": productionToolName, "arguments": `{"path":"demo.txt"}`}}},
				},
			}},
			"usage": map[string]any{
				"prompt_tokens": 100, "completion_tokens": 30, "total_tokens": 130,
				"prompt_tokens_details":     map[string]any{"cached_tokens": 60},
				"completion_tokens_details": map[string]any{"reasoning_tokens": 10},
			},
		}
	case ProtocolAnthropic:
		value = map[string]any{
			"id": "msg-1", "type": "message", "role": "assistant", "model": model,
			"content": []any{
				map[string]any{"type": "thinking", "thinking": productionReasoning, "signature": "sig-1"},
				map[string]any{"type": "tool_use", "id": "call-1", "name": productionToolName, "input": map[string]any{"path": "demo.txt"}},
				map[string]any{"type": "text", "text": "OK"},
			},
			"stop_reason": "tool_use",
			"usage":       map[string]any{"input_tokens": 40, "cache_read_input_tokens": 60, "output_tokens": 30},
		}
	case ProtocolGemini:
		value = map[string]any{
			"responseId": "gem-1", "modelVersion": model,
			"candidates": []any{map[string]any{
				"content": map[string]any{"role": "model", "parts": []any{
					map[string]any{"text": productionReasoning, "thought": true, "thoughtSignature": "sig-1"},
					map[string]any{"functionCall": map[string]any{"id": "call-1", "name": productionToolName, "args": map[string]any{"path": "demo.txt"}}},
					map[string]any{"text": "OK"},
				}},
				"finishReason": "STOP",
			}},
			"usageMetadata": map[string]any{
				"promptTokenCount": 100, "cachedContentTokenCount": 60,
				"candidatesTokenCount": 20, "thoughtsTokenCount": 10, "totalTokenCount": 130,
			},
		}
	default:
		t.Fatalf("unsupported response fixture protocol %q", protocol)
	}
	body, err := json.Marshal(value)
	require.NoError(t, err)
	return body
}

func requireResponseSemantics(t *testing.T, protocol Protocol, body []byte, model string, hasReasoningTokenCount bool) {
	t.Helper()
	require.Contains(t, string(body), productionReasoning)
	require.Contains(t, string(body), productionToolName)
	switch protocol {
	case ProtocolOpenAIResponses:
		require.Equal(t, model, gjson.GetBytes(body, "model").String())
		require.Equal(t, int64(60), gjson.GetBytes(body, "usage.input_tokens_details.cached_tokens").Int())
		expectedReasoningTokens := int64(0)
		if hasReasoningTokenCount {
			expectedReasoningTokens = 10
		}
		require.Equal(t, expectedReasoningTokens, gjson.GetBytes(body, "usage.output_tokens_details.reasoning_tokens").Int())
	case ProtocolOpenAICompat:
		require.Equal(t, model, gjson.GetBytes(body, "model").String())
		require.Equal(t, int64(60), gjson.GetBytes(body, "usage.prompt_tokens_details.cached_tokens").Int())
		expectedReasoningTokens := int64(0)
		if hasReasoningTokenCount {
			expectedReasoningTokens = 10
		}
		require.Equal(t, expectedReasoningTokens, gjson.GetBytes(body, "usage.completion_tokens_details.reasoning_tokens").Int())
	case ProtocolAnthropic:
		require.Equal(t, model, gjson.GetBytes(body, "model").String())
		require.Equal(t, int64(60), gjson.GetBytes(body, "usage.cache_read_input_tokens").Int())
	case ProtocolGemini:
		require.Equal(t, model, gjson.GetBytes(body, "modelVersion").String())
		require.Equal(t, int64(60), gjson.GetBytes(body, "usageMetadata.cachedContentTokenCount").Int())
		expectedReasoningTokens := int64(0)
		if hasReasoningTokenCount {
			expectedReasoningTokens = 10
		}
		require.Equal(t, expectedReasoningTokens, gjson.GetBytes(body, "usageMetadata.thoughtsTokenCount").Int())
	default:
		t.Fatalf("unsupported response protocol %q", protocol)
	}
}

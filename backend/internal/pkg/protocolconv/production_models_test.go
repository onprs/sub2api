package protocolconv

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

var productionTextModels = []string{
	// OpenAI groups: explicit mappings plus the current dynamic/default GPT variants.
	"gpt-5.2",
	"gpt-5.2-2025-12-11",
	"gpt-5.2-chat-latest",
	"gpt-5.2-pro",
	"gpt-5.2-pro-2025-12-11",
	"gpt-5.3-codex",
	"gpt-5.3-codex-spark",
	"gpt-5.4",
	"gpt-5.4-2026-03-05",
	"gpt-5.4-mini",
	"gpt-5.5",
	"gpt-5.6",
	"gpt-5.6-luna",
	"gpt-5.6-sol",
	"gpt-5.6-terra",

	// OpenCode Go chat/completions models.
	"deepseek-v4-flash",
	"deepseek-v4-pro",
	"glm-5",
	"glm-5.1",
	"glm-5.2",
	"kimi-k2.5",
	"kimi-k2.6",
	"kimi-k2.7-code",
	"mimo-v2-omni",
	"mimo-v2-pro",
	"mimo-v2.5",
	"mimo-v2.5-pro",

	// OpenCode Go messages models.
	"minimax-m2.5",
	"minimax-m2.7",
	"minimax-m3",
	"qwen3.5-plus",
	"qwen3.6-plus",
	"qwen3.7-max",
	"qwen3.7-plus",
	"hy3-preview",

	// Antigravity Claude and Gemini endpoint families.
	"claude-opus-4-6",
	"claude-sonnet-4-6",
	"gemini-2.5-flash",
	"gemini-3-flash",
	"gemini-3.1-flash-lite",
	"gemini-3.1-pro-high",
	"gemini-3.1-pro-low",
	"gemini-3.5-flash-high",
	"gemini-3.5-flash-low",
	"gemini-3.5-flash-medium",
}

var productionProtocols = []Protocol{
	ProtocolAnthropic,
	ProtocolGemini,
	ProtocolOpenAICompat,
	ProtocolOpenAIResponses,
}

const (
	productionStablePrefix = "production-cache-prefix-v2"
	productionReasoning    = "inspect then call the tool"
	productionToolName     = "read_file"
	productionContinuation = "Now summarize the tool result."
)

func TestProductionTextModelsRequestMatrixPreservesSemanticAndCachePrefix(t *testing.T) {
	for _, model := range productionTextModels {
		model := model
		t.Run(model, func(t *testing.T) {
			for _, from := range productionProtocols {
				from := from
				for _, to := range productionProtocols {
					to := to
					t.Run(fmt.Sprintf("%s_to_%s", from, to), func(t *testing.T) {
						firstBody, firstOpts := productionRequestFixture(t, from, model, false)
						secondBody, secondOpts := productionRequestFixture(t, from, model, true)

						first, err := ConvertRequest(firstBody, from, Target{Protocol: to}, firstOpts)
						require.NoError(t, err)
						second, err := ConvertRequest(secondBody, from, Target{Protocol: to}, secondOpts)
						require.NoError(t, err)
						require.True(t, json.Valid(first), "first conversion: %s", first)
						require.True(t, json.Valid(second), "second conversion: %s", second)

						if to != ProtocolGemini {
							require.Equal(t, model, gjson.GetBytes(second, "model").String())
						}
						require.Contains(t, string(second), productionStablePrefix)
						require.Contains(t, string(second), productionToolName)
						require.Contains(t, string(second), productionContinuation)
						require.Contains(t, string(second), productionReasoning)
						requireHistoryExtensionPreservesPrefix(t, to, first, second)
					})
				}
			}
		})
	}
}

func productionRequestFixture(t *testing.T, protocol Protocol, model string, continuation bool) ([]byte, Options) {
	t.Helper()
	var value any
	opts := Options{}
	switch protocol {
	case ProtocolOpenAIResponses:
		input := []any{
			map[string]any{"role": "user", "content": []any{map[string]any{"type": "input_text", "text": "Read demo.txt."}}},
			map[string]any{"type": "reasoning", "summary": []any{map[string]any{"type": "summary_text", "text": productionReasoning}}, "encrypted_content": "sig-1"},
			map[string]any{"type": "function_call", "call_id": "call-1", "name": productionToolName, "arguments": `{"path":"demo.txt"}`},
			map[string]any{"type": "function_call_output", "call_id": "call-1", "output": "demo content"},
		}
		if continuation {
			input = append(input,
				map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "output_text", "text": "Tool result acknowledged."}}},
				map[string]any{"role": "user", "content": []any{map[string]any{"type": "input_text", "text": productionContinuation}}},
			)
		}
		value = map[string]any{
			"model": model, "instructions": productionStablePrefix, "input": input,
			"tools":     []any{map[string]any{"type": "function", "name": productionToolName, "parameters": map[string]any{"type": "object"}}},
			"reasoning": map[string]any{"effort": "low", "summary": "auto"}, "max_output_tokens": 2048,
		}
	case ProtocolOpenAICompat:
		messages := []any{
			map[string]any{"role": "system", "content": productionStablePrefix},
			map[string]any{"role": "user", "content": "Read demo.txt."},
			map[string]any{"role": "assistant", "reasoning_content": productionReasoning, "tool_calls": []any{map[string]any{"id": "call-1", "type": "function", "function": map[string]any{"name": productionToolName, "arguments": `{"path":"demo.txt"}`}}}},
			map[string]any{"role": "tool", "tool_call_id": "call-1", "content": "demo content"},
		}
		if continuation {
			messages = append(messages,
				map[string]any{"role": "assistant", "content": "Tool result acknowledged."},
				map[string]any{"role": "user", "content": productionContinuation},
			)
		}
		value = map[string]any{
			"model": model, "messages": messages,
			"tools":            []any{map[string]any{"type": "function", "function": map[string]any{"name": productionToolName, "parameters": map[string]any{"type": "object"}}}},
			"reasoning_effort": "low", "max_tokens": 2048,
		}
	case ProtocolAnthropic:
		messages := []any{
			map[string]any{"role": "user", "content": "Read demo.txt."},
			map[string]any{"role": "assistant", "content": []any{
				map[string]any{"type": "thinking", "thinking": productionReasoning, "signature": "sig-1"},
				map[string]any{"type": "tool_use", "id": "call-1", "name": productionToolName, "input": map[string]any{"path": "demo.txt"}},
			}},
			map[string]any{"role": "user", "content": []any{map[string]any{"type": "tool_result", "tool_use_id": "call-1", "content": "demo content"}}},
		}
		if continuation {
			messages = append(messages,
				map[string]any{"role": "assistant", "content": "Tool result acknowledged."},
				map[string]any{"role": "user", "content": productionContinuation},
			)
		}
		value = map[string]any{
			"model":    model,
			"system":   []any{map[string]any{"type": "text", "text": productionStablePrefix, "cache_control": map[string]any{"type": "ephemeral", "ttl": "1h"}}},
			"messages": messages,
			"tools":    []any{map[string]any{"name": productionToolName, "input_schema": map[string]any{"type": "object"}}},
			"thinking": map[string]any{"type": "enabled", "budget_tokens": 1024}, "output_config": map[string]any{"effort": "low"}, "max_tokens": 2048,
		}
	case ProtocolGemini:
		opts.Model = model
		functionCall := map[string]any{
			"id": "call-1", "name": productionToolName, "args": map[string]any{"path": "demo.txt"},
		}
		functionResponse := map[string]any{
			"id": "call-1", "name": productionToolName, "response": map[string]any{"output": "demo content"},
		}
		contents := []any{
			map[string]any{"role": "user", "parts": []any{map[string]any{"text": "Read demo.txt."}}},
			map[string]any{"role": "model", "parts": []any{
				map[string]any{"text": productionReasoning, "thought": true, "thoughtSignature": "sig-1"},
				map[string]any{"functionCall": functionCall},
			}},
			map[string]any{"role": "user", "parts": []any{map[string]any{"functionResponse": functionResponse}}},
		}
		if continuation {
			contents = append(contents,
				map[string]any{"role": "model", "parts": []any{map[string]any{"text": "Tool result acknowledged."}}},
				map[string]any{"role": "user", "parts": []any{map[string]any{"text": productionContinuation}}},
			)
		}
		declaration := map[string]any{
			"name": productionToolName, "parameters": map[string]any{"type": "object"},
		}
		value = map[string]any{
			"systemInstruction": map[string]any{"parts": []any{map[string]any{"text": productionStablePrefix}}},
			"contents":          contents,
			"tools":             []any{map[string]any{"functionDeclarations": []any{declaration}}},
			"generationConfig": map[string]any{
				"maxOutputTokens": 2048,
				"thinkingConfig":  map[string]any{"includeThoughts": true, "thinkingBudget": 1024},
			},
		}
	default:
		t.Fatalf("unsupported fixture protocol %q", protocol)
	}
	body, err := json.Marshal(value)
	require.NoError(t, err)
	return body, opts
}

func requireHistoryExtensionPreservesPrefix(t *testing.T, protocol Protocol, first, second []byte) {
	t.Helper()
	var firstValue map[string]any
	var secondValue map[string]any
	require.NoError(t, json.Unmarshal(first, &firstValue))
	require.NoError(t, json.Unmarshal(second, &secondValue))

	switch protocol {
	case ProtocolOpenAIResponses:
		require.Equal(t, firstValue["instructions"], secondValue["instructions"])
		requireSlicePrefix(t, firstValue["input"], secondValue["input"])
	case ProtocolOpenAICompat:
		requireSlicePrefix(t, firstValue["messages"], secondValue["messages"])
	case ProtocolAnthropic:
		require.Equal(t, firstValue["system"], secondValue["system"])
		requireSlicePrefix(t, firstValue["messages"], secondValue["messages"])
	case ProtocolGemini:
		require.Equal(t, firstValue["systemInstruction"], secondValue["systemInstruction"])
		firstContents, ok := firstValue["contents"].([]any)
		require.True(t, ok)
		secondContents, ok := secondValue["contents"].([]any)
		require.True(t, ok)
		require.LessOrEqual(t, len(firstContents), len(secondContents))
		for i := range firstContents {
			firstContent := firstContents[i].(map[string]any)
			secondContent := secondContents[i].(map[string]any)
			require.Equal(t, firstContent["role"], secondContent["role"])
			requireSlicePrefix(t, firstContent["parts"], secondContent["parts"])
		}
	default:
		t.Fatalf("unsupported target protocol %q", protocol)
	}
}

func requireSlicePrefix(t *testing.T, firstValue, secondValue any) {
	t.Helper()
	first, ok := firstValue.([]any)
	require.True(t, ok, "first value is not an array: %#v", firstValue)
	second, ok := secondValue.([]any)
	require.True(t, ok, "second value is not an array: %#v", secondValue)
	require.LessOrEqual(t, len(first), len(second))
	require.Equal(t, first, second[:len(first)], "existing history changed after appending a turn")
}

func TestProductionTextModelInventoryHasNoDuplicates(t *testing.T) {
	seen := make(map[string]struct{}, len(productionTextModels))
	for _, model := range productionTextModels {
		model = strings.TrimSpace(model)
		require.NotEmpty(t, model)
		_, duplicate := seen[model]
		require.False(t, duplicate, "duplicate production model %q", model)
		seen[model] = struct{}{}
	}
}

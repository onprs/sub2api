package standard

import (
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv"
	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv/ir"
	"github.com/stretchr/testify/require"
)

func TestRequestMatrixPreservesCoreSemantics(t *testing.T) {
	registry, err := NewRegistry()
	require.NoError(t, err)

	for _, source := range protocolconv.StandardProtocols() {
		source := source
		t.Run(source.String(), func(t *testing.T) {
			body := requestFixture(t, source)
			for _, target := range protocolconv.StandardProtocols() {
				target := target
				t.Run(target.String(), func(t *testing.T) {
					options := protocolconv.Options{SourceModel: "test-model", LossPolicy: protocolconv.LossWarn}
					converted, warnings, err := registry.ConvertRequest(body, source, target, options)
					require.NoError(t, err)
					require.True(t, json.Valid(converted))
					if source == target {
						require.Equal(t, body, converted)
					}

					converter, err := registry.Converter(target)
					require.NoError(t, err)
					decoded, _, err := converter.DecodeRequest(converted, options)
					require.NoError(t, err)
					requireRequestSemantics(t, decoded)
					requireSignaturePolicy(t, source, target, decoded.Messages, warnings)
				})
			}
		})
	}
}

func TestResponseMatrixPreservesCoreSemantics(t *testing.T) {
	registry, err := NewRegistry()
	require.NoError(t, err)
	for _, source := range protocolconv.StandardProtocols() {
		source := source
		t.Run(source.String(), func(t *testing.T) {
			body := responseFixture(t, source)
			for _, target := range protocolconv.StandardProtocols() {
				target := target
				t.Run(target.String(), func(t *testing.T) {
					options := protocolconv.Options{SourceModel: "test-model", LossPolicy: protocolconv.LossWarn}
					converted, warnings, err := registry.ConvertResponse(body, source, target, options)
					require.NoError(t, err)
					require.True(t, json.Valid(converted))
					if source == target {
						require.Equal(t, body, converted)
					}
					converter, err := registry.Converter(target)
					require.NoError(t, err)
					decoded, _, err := converter.DecodeResponse(converted, options)
					require.NoError(t, err)
					requireResponseSemantics(t, decoded)
					requireSignaturePolicy(t, source, target, []ir.Message{decoded.Choices[0].Message}, warnings)
				})
			}
		})
	}
}

func requireRequestSemantics(t *testing.T, request *ir.Request) {
	t.Helper()
	require.Equal(t, "test-model", request.Model)
	systemText := partsText(request.SystemInstruction) + roleText(request.Messages, ir.RoleSystem) + roleText(request.Messages, ir.RoleDeveloper)
	require.Contains(t, systemText, "stable system")
	require.Contains(t, messagesText(request.Messages), "read demo")
	require.Contains(t, messagesText(request.Messages), "continue")
	require.True(t, hasPart(request.Messages, ir.ContentToolCall, "call-1"))
	require.True(t, hasPart(request.Messages, ir.ContentToolResult, "call-1"))
	require.True(t, hasReasoning(request.Messages, "inspect first"))
	require.NotContains(t, messagesText(request.Messages), "<thinking>")
	require.NotContains(t, messagesText(request.Messages), "inspect first")
	require.NotEmpty(t, request.Tools)
	require.Equal(t, "read_file", request.Tools[0].Name)
}

func requireSignaturePolicy(t *testing.T, source, target protocolconv.Protocol, messages []ir.Message, warnings []protocolconv.Warning) {
	t.Helper()
	if source == protocolconv.ProtocolOpenAIChat {
		return
	}
	if target == protocolconv.ProtocolOpenAIChat {
		for _, warning := range warnings {
			if warning.Capability == protocolconv.CapabilitySignature {
				return
			}
		}
		t.Fatal("Chat conversion dropped a reasoning signature without warning")
	}
	for _, message := range messages {
		for _, part := range message.Content {
			if part.Type == ir.ContentReasoning && part.Signature == "sig-1" {
				return
			}
		}
	}
	t.Fatalf("target %s lost reasoning signature", target)
}

func requireResponseSemantics(t *testing.T, response *ir.Response) {
	t.Helper()
	require.Equal(t, "test-model", response.Model)
	require.NotEmpty(t, response.Choices)
	messages := []ir.Message{response.Choices[0].Message}
	require.Contains(t, messagesText(messages), "done")
	require.True(t, hasPart(messages, ir.ContentToolCall, "call-1"))
	require.True(t, hasReasoning(messages, "inspect first"))
	require.NotNil(t, response.Usage)
	require.Equal(t, 60, response.Usage.CacheReadTokens)
	require.GreaterOrEqual(t, response.Usage.ReasoningTokens, 0)
}

func requestFixture(t *testing.T, protocol protocolconv.Protocol) []byte {
	t.Helper()
	var value any
	switch protocol {
	case protocolconv.ProtocolOpenAIChat:
		value = map[string]any{"model": "test-model", "messages": []any{
			map[string]any{"role": "system", "content": "stable system"},
			map[string]any{"role": "user", "content": "read demo"},
			map[string]any{"role": "assistant", "reasoning_content": "inspect first", "tool_calls": []any{map[string]any{"id": "call-1", "type": "function", "function": map[string]any{"name": "read_file", "arguments": `{"path":"demo"}`}}}},
			map[string]any{"role": "tool", "tool_call_id": "call-1", "content": "demo content"},
			map[string]any{"role": "user", "content": "continue"},
		}, "tools": []any{map[string]any{"type": "function", "function": map[string]any{"name": "read_file", "parameters": map[string]any{"type": "object"}}}}, "max_tokens": 100}
	case protocolconv.ProtocolOpenAIResponses:
		value = map[string]any{"model": "test-model", "instructions": "stable system", "input": []any{
			map[string]any{"role": "user", "content": "read demo"},
			map[string]any{"type": "reasoning", "summary": []any{map[string]any{"type": "summary_text", "text": "inspect first"}}, "encrypted_content": "sig-1"},
			map[string]any{"type": "function_call", "call_id": "call-1", "name": "read_file", "arguments": `{"path":"demo"}`},
			map[string]any{"type": "function_call_output", "call_id": "call-1", "output": "demo content"},
			map[string]any{"role": "user", "content": "continue"},
		}, "tools": []any{map[string]any{"type": "function", "name": "read_file", "parameters": map[string]any{"type": "object"}}}, "max_output_tokens": 100}
	case protocolconv.ProtocolAnthropic:
		value = map[string]any{"model": "test-model", "system": "stable system", "messages": []any{
			map[string]any{"role": "user", "content": "read demo"},
			map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "thinking", "thinking": "inspect first", "signature": "sig-1"}, map[string]any{"type": "tool_use", "id": "call-1", "name": "read_file", "input": map[string]any{"path": "demo"}}}},
			map[string]any{"role": "user", "content": []any{map[string]any{"type": "tool_result", "tool_use_id": "call-1", "content": "demo content"}}},
			map[string]any{"role": "user", "content": "continue"},
		}, "tools": []any{map[string]any{"name": "read_file", "input_schema": map[string]any{"type": "object"}}}, "max_tokens": 100, "thinking": map[string]any{"type": "enabled", "budget_tokens": 20}}
	case protocolconv.ProtocolGoogleGenAI:
		value = map[string]any{"systemInstruction": map[string]any{"parts": []any{map[string]any{"text": "stable system"}}}, "contents": []any{
			map[string]any{"role": "user", "parts": []any{map[string]any{"text": "read demo"}}},
			map[string]any{"role": "model", "parts": []any{map[string]any{"text": "inspect first", "thought": true, "thoughtSignature": "sig-1"}, map[string]any{"functionCall": map[string]any{"id": "call-1", "name": "read_file", "args": map[string]any{"path": "demo"}}}}},
			map[string]any{"role": "user", "parts": []any{map[string]any{"functionResponse": map[string]any{"id": "call-1", "name": "read_file", "response": map[string]any{"output": "demo content"}}}}},
			map[string]any{"role": "user", "parts": []any{map[string]any{"text": "continue"}}},
		}, "tools": []any{map[string]any{"functionDeclarations": []any{map[string]any{"name": "read_file", "parameters": map[string]any{"type": "object"}}}}}, "generationConfig": map[string]any{"maxOutputTokens": 100}}
	default:
		t.Fatalf("unsupported protocol %q", protocol)
	}
	body, err := json.Marshal(value)
	require.NoError(t, err)
	return body
}

func responseFixture(t *testing.T, protocol protocolconv.Protocol) []byte {
	t.Helper()
	var value any
	usage := map[string]any{"input_tokens": 100, "output_tokens": 30, "total_tokens": 130, "input_tokens_details": map[string]any{"cached_tokens": 60}, "output_tokens_details": map[string]any{"reasoning_tokens": 10}}
	switch protocol {
	case protocolconv.ProtocolOpenAIChat:
		value = map[string]any{"id": "chat-1", "object": "chat.completion", "model": "test-model", "choices": []any{map[string]any{"index": 0, "finish_reason": "tool_calls", "message": map[string]any{"role": "assistant", "content": "done", "reasoning_content": "inspect first", "tool_calls": []any{map[string]any{"id": "call-1", "type": "function", "function": map[string]any{"name": "read_file", "arguments": `{"path":"demo"}`}}}}}}, "usage": map[string]any{"prompt_tokens": 100, "completion_tokens": 30, "total_tokens": 130, "prompt_tokens_details": map[string]any{"cached_tokens": 60}, "completion_tokens_details": map[string]any{"reasoning_tokens": 10}}}
	case protocolconv.ProtocolOpenAIResponses:
		value = map[string]any{"id": "resp-1", "object": "response", "status": "completed", "model": "test-model", "output": []any{map[string]any{"type": "reasoning", "summary": []any{map[string]any{"type": "summary_text", "text": "inspect first"}}, "encrypted_content": "sig-1"}, map[string]any{"type": "function_call", "call_id": "call-1", "name": "read_file", "arguments": `{"path":"demo"}`}, map[string]any{"type": "message", "role": "assistant", "status": "completed", "content": []any{map[string]any{"type": "output_text", "text": "done"}}}}, "usage": usage}
	case protocolconv.ProtocolAnthropic:
		value = map[string]any{"id": "msg-1", "type": "message", "role": "assistant", "model": "test-model", "content": []any{map[string]any{"type": "thinking", "thinking": "inspect first", "signature": "sig-1"}, map[string]any{"type": "tool_use", "id": "call-1", "name": "read_file", "input": map[string]any{"path": "demo"}}, map[string]any{"type": "text", "text": "done"}}, "stop_reason": "tool_use", "usage": map[string]any{"input_tokens": 40, "cache_read_input_tokens": 60, "output_tokens": 30}}
	case protocolconv.ProtocolGoogleGenAI:
		value = map[string]any{"responseId": "gem-1", "modelVersion": "test-model", "candidates": []any{map[string]any{"index": 0, "finishReason": "STOP", "content": map[string]any{"role": "model", "parts": []any{map[string]any{"text": "inspect first", "thought": true, "thoughtSignature": "sig-1"}, map[string]any{"functionCall": map[string]any{"id": "call-1", "name": "read_file", "args": map[string]any{"path": "demo"}}}, map[string]any{"text": "done"}}}}}, "usageMetadata": map[string]any{"promptTokenCount": 100, "cachedContentTokenCount": 60, "candidatesTokenCount": 20, "thoughtsTokenCount": 10, "totalTokenCount": 130}}
	default:
		t.Fatalf("unsupported protocol %q", protocol)
	}
	body, err := json.Marshal(value)
	require.NoError(t, err)
	return body
}

func partsText(parts []ir.ContentPart) string {
	out := ""
	for _, part := range parts {
		out += part.Text
	}
	return out
}
func messagesText(messages []ir.Message) string {
	out := ""
	for _, message := range messages {
		for _, part := range message.Content {
			out += part.Text + string(part.ToolResult)
		}
	}
	return out
}
func roleText(messages []ir.Message, role ir.Role) string {
	out := ""
	for _, message := range messages {
		if message.Role != role {
			continue
		}
		for _, part := range message.Content {
			out += part.Text
		}
	}
	return out
}
func hasPart(messages []ir.Message, kind ir.ContentType, id string) bool {
	for _, message := range messages {
		for _, part := range message.Content {
			if part.Type == kind && part.ToolCallID == id {
				return true
			}
		}
	}
	return false
}
func hasReasoning(messages []ir.Message, text string) bool {
	for _, message := range messages {
		for _, part := range message.Content {
			if part.Type == ir.ContentReasoning && part.Reasoning == text {
				return true
			}
		}
	}
	return false
}

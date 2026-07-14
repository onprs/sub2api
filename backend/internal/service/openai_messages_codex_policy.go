package service

import (
	"encoding/json"
	"fmt"
	"strings"
)

const openAICodexMessagesMinOutputTokens = 128

// applyOpenAICodexMessagesRequestPolicy applies provider defaults after the
// standard Anthropic-to-Responses Pipeline has produced the wire body.
func applyOpenAICodexMessagesRequestPolicy(converted []byte, sourceSystem json.RawMessage, upstreamModel string) ([]byte, error) {
	var body map[string]any
	decoder := json.NewDecoder(strings.NewReader(string(converted)))
	decoder.UseNumber()
	if err := decoder.Decode(&body); err != nil {
		return nil, fmt.Errorf("decode standard Responses request: %w", err)
	}
	if body == nil {
		return nil, fmt.Errorf("decode standard Responses request: body is null")
	}

	body["model"] = upstreamModel
	body["stream"] = true
	body["include"] = []string{"reasoning.encrypted_content"}
	body["store"] = false
	body["parallel_tool_calls"] = true
	body["text"] = map[string]any{"verbosity": "medium"}

	reasoning, _ := body["reasoning"].(map[string]any)
	if reasoning == nil {
		reasoning = make(map[string]any, 2)
	}
	if strings.TrimSpace(firstNonEmptyString(reasoning["effort"])) == "" {
		reasoning["effort"] = "medium"
	}
	reasoning["summary"] = "auto"
	body["reasoning"] = reasoning

	if maxTokens, ok := jsonInteger(body["max_output_tokens"]); ok && maxTokens > 0 && maxTokens < openAICodexMessagesMinOutputTokens {
		body["max_output_tokens"] = openAICodexMessagesMinOutputTokens
	}
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(upstreamModel)), "gpt-5") {
		delete(body, "temperature")
		delete(body, "top_p")
	}
	normalizeOpenAICodexMessagesTools(body)

	delete(body, "instructions")
	developer, err := openAICodexMessagesDeveloperInput(sourceSystem)
	if err != nil {
		return nil, err
	}
	if developer != nil {
		input, _ := body["input"].([]any)
		body["input"] = append([]any{developer}, input...)
	}

	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("encode Codex Messages request policy: %w", err)
	}
	return encoded, nil
}

func jsonInteger(value any) (int, bool) {
	switch typed := value.(type) {
	case json.Number:
		parsed, err := typed.Int64()
		return int(parsed), err == nil
	case float64:
		return int(typed), typed == float64(int(typed))
	case int:
		return typed, true
	default:
		return 0, false
	}
}

func normalizeOpenAICodexMessagesTools(body map[string]any) {
	tools, _ := body["tools"].([]any)
	for _, raw := range tools {
		tool, _ := raw.(map[string]any)
		if tool == nil {
			continue
		}
		kind := strings.TrimSpace(firstNonEmptyString(tool["type"]))
		if strings.HasPrefix(kind, "web_search") {
			tool["type"] = "web_search"
			delete(tool, "name")
			delete(tool, "description")
			delete(tool, "parameters")
			delete(tool, "strict")
			continue
		}
		tool["type"] = "function"
		normalizeOpenAICodexMessagesToolParameters(tool)
		if _, exists := tool["strict"]; !exists {
			tool["strict"] = false
		}
	}
}

func normalizeOpenAICodexMessagesToolParameters(tool map[string]any) {
	parameters, _ := tool["parameters"].(map[string]any)
	if parameters == nil {
		tool["parameters"] = map[string]any{"type": "object", "properties": map[string]any{}}
		return
	}
	if strings.TrimSpace(firstNonEmptyString(parameters["type"])) == "object" {
		if _, exists := parameters["properties"]; !exists {
			parameters["properties"] = map[string]any{}
		}
	}
}

func openAICodexMessagesDeveloperInput(system json.RawMessage) (map[string]any, error) {
	if len(system) == 0 {
		return nil, nil
	}
	var text string
	if json.Unmarshal(system, &text) == nil {
		if strings.TrimSpace(text) == "" || isOpenAICodexBillingHeader(text) {
			return nil, nil
		}
		return responsesDeveloperInput([]any{map[string]any{"type": "input_text", "text": text}}), nil
	}

	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(system, &blocks); err != nil {
		return nil, fmt.Errorf("decode Anthropic system for Codex policy: %w", err)
	}
	parts := make([]any, 0, len(blocks))
	for _, block := range blocks {
		if block.Type != "text" || block.Text == "" || isOpenAICodexBillingHeader(block.Text) {
			continue
		}
		parts = append(parts, map[string]any{"type": "input_text", "text": block.Text})
	}
	return responsesDeveloperInput(parts), nil
}

func responsesDeveloperInput(parts []any) map[string]any {
	if len(parts) == 0 {
		return nil
	}
	return map[string]any{"type": "message", "role": "developer", "content": parts}
}

func isOpenAICodexBillingHeader(text string) bool {
	return strings.HasPrefix(text, "x-anthropic-billing-header: ")
}

func trimOpenAICompatResponsesBodyToLatestTurn(body map[string]any) {
	items, _ := body["input"].([]any)
	if len(items) == 0 {
		return
	}
	start := latestOpenAICompatResponsesBodyTurnStart(items)
	if start <= 0 {
		return
	}
	body["input"] = append([]any(nil), items[start:]...)
}

func latestOpenAICompatResponsesBodyTurnStart(items []any) int {
	start := len(items) - 1
	last, _ := items[start].(map[string]any)
	switch {
	case openAICompatResponsesItemType(last) == "function_call_output":
		for start > 0 && openAICompatResponsesItemTypeMap(items[start-1]) == "function_call_output" {
			start--
		}
	case openAICompatResponsesItemType(last) == "message" && strings.TrimSpace(firstNonEmptyString(last["role"])) == "user":
		for start > 0 && openAICompatResponsesItemTypeMap(items[start-1]) == "function_call_output" {
			start--
		}
	default:
		return start
	}

	needed := make(map[string]struct{})
	for i := start; i < len(items); i++ {
		item, _ := items[i].(map[string]any)
		if openAICompatResponsesItemType(item) == "function_call_output" {
			if callID := strings.TrimSpace(firstNonEmptyString(item["call_id"])); callID != "" {
				needed[callID] = struct{}{}
			}
		}
	}
	expanded := start
	for i := start - 1; i >= 0 && len(needed) > 0; i-- {
		item, _ := items[i].(map[string]any)
		if openAICompatResponsesItemType(item) != "function_call" {
			continue
		}
		callID := strings.TrimSpace(firstNonEmptyString(item["call_id"]))
		if _, ok := needed[callID]; ok {
			delete(needed, callID)
			expanded = i
		}
	}
	return expanded
}

func openAICompatResponsesItemTypeMap(raw any) string {
	item, _ := raw.(map[string]any)
	return openAICompatResponsesItemType(item)
}

func openAICompatResponsesItemType(item map[string]any) string {
	if item == nil {
		return ""
	}
	kind := strings.TrimSpace(firstNonEmptyString(item["type"]))
	if kind == "" && firstNonEmptyString(item["role"]) != "" {
		return "message"
	}
	return kind
}

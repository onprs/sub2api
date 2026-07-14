package service

import (
	"encoding/json"
	"strings"
)

const (
	openAICompatClaudeCodeTodoGuardMarker = "<sub2api-claude-code-todo-guard>"
	openAICompatClaudeCodeTodoGuardText   = openAICompatClaudeCodeTodoGuardMarker + "\nWhen using Claude Code todo or task tracking tools, keep the visible task list consistent. Do not send final or summary text while any item remains in_progress. Before finishing, asking the user to choose, or reporting a blocker, update the todo list so completed work is completed and deferred work is pending/open; leave an item in_progress only when active work will continue in the same turn.\n</sub2api-claude-code-todo-guard>"
)

func appendOpenAICompatClaudeCodeTodoGuardToRequestBody(reqBody map[string]any) bool {
	if reqBody == nil {
		return false
	}

	input, ok := reqBody["input"].([]any)
	if !ok || len(input) == 0 || inputContainsText(input, openAICompatClaudeCodeTodoGuardMarker) {
		return false
	}

	guard := map[string]any{
		"type": "message",
		"role": "developer",
		"content": []any{
			map[string]any{
				"type": "input_text",
				"text": openAICompatClaudeCodeTodoGuardText,
			},
		},
	}

	insertAt := 0
	for insertAt < len(input) {
		item, ok := input[insertAt].(map[string]any)
		if !ok || strings.TrimSpace(firstNonEmptyString(item["type"])) != "message" || strings.TrimSpace(firstNonEmptyString(item["role"])) != "developer" {
			break
		}
		insertAt++
	}

	input = append(input, nil)
	copy(input[insertAt+1:], input[insertAt:])
	input[insertAt] = guard
	reqBody["input"] = input
	return true
}

func inputContainsText(input []any, needle string) bool {
	needle = strings.TrimSpace(needle)
	if needle == "" {
		return false
	}
	for _, item := range input {
		b, err := json.Marshal(item)
		if err == nil && strings.Contains(string(b), needle) {
			return true
		}
	}
	return false
}

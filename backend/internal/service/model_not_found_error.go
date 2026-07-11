package service

import (
	"net/http"
	"strings"
)

var upstreamModelNotFoundKeywords = []string{"model not found", "unknown model", "not found"}

func isUpstreamModelNotFoundError(statusCode int, body []byte) bool {
	normalized := normalizeModelNotFoundBody(body)
	if normalized == "" || !strings.Contains(normalized, "model") {
		return false
	}
	if statusCode == http.StatusNotFound {
		return containsModelNotFoundKeyword(normalized)
	}
	if statusCode == http.StatusBadRequest {
		return isOpenAIChatGPTCodexModelUnsupported(normalized)
	}
	return false
}

func isModelNotFoundError(statusCode int, body []byte) bool {
	return isUpstreamModelNotFoundError(statusCode, body) || statusCode == http.StatusNotFound
}

func containsModelNotFoundKeyword(normalizedBody string) bool {
	if normalizedBody == "" {
		return false
	}
	for _, keyword := range upstreamModelNotFoundKeywords {
		if strings.Contains(normalizedBody, keyword) {
			return true
		}
	}
	return false
}

func isOpenAIChatGPTCodexModelUnsupported(normalizedBody string) bool {
	if normalizedBody == "" {
		return false
	}
	return strings.Contains(normalizedBody, "not supported") &&
		strings.Contains(normalizedBody, "codex") &&
		strings.Contains(normalizedBody, "chatgpt account")
}

func normalizeModelNotFoundBody(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	normalized := strings.ToLower(string(body))
	normalized = strings.NewReplacer("_", " ", "-", " ", "\n", " ", "\r", " ", "\t", " ").Replace(normalized)
	return strings.Join(strings.Fields(normalized), " ")
}

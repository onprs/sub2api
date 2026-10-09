package service

import (
	"net/http"
	"strings"

	"github.com/tidwall/gjson"
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

func isUpstreamModelNotFoundErrorForAccount(account *Account, statusCode int, body []byte) bool {
	if isUpstreamModelNotFoundError(statusCode, body) {
		return true
	}
	if account != nil && statusCode == http.StatusUnauthorized && account.Type == AccountTypeAPIKey &&
		account.IsOpenAICompatible() && isOpenAICompatibleModelNotFoundBody(body) {
		return true
	}
	if account != nil && account.IsOpenCodeGo() &&
		isOpenCodeGoModelUnsupportedError(statusCode, body) {
		return true
	}
	// Command Code 400 unsupported_model：模型不在目录中，属于确定性的
	// 账号×模型能力反馈，与 OpenCode Go 的模型不支持同构。
	// 判定必须与 model 参数/模型不支持文案互证，避免其它参数错误被错误
	// 分类为 unsupported_model 时冷却模型（2026-10-10 排查要求）。
	if account != nil && account.Platform == PlatformCommandCode &&
		statusCode == http.StatusBadRequest &&
		isCommandCodeUnsupportedModelError(body) {
		return true
	}
	// Command Code 403 MODEL_NOT_IN_PLAN：模型不在当前账号套餐内。同样是确定性的
	// 账号×模型能力反馈，必须按模型级冷却处理：否则每次请求套餐外模型都会给账号
	// 记一次 403（连续 3 次永久禁用账号），把模型能力问题放大成整账号处罚。
	if account != nil && account.Platform == PlatformCommandCode && isCommandCodeModelNotInPlanError(statusCode, body) {
		return true
	}
	return false
}

// isCommandCodeUnsupportedModelError 判定 Command Code 的 unsupported_model 错误
// 确实指向模型本身（而不是被错误分类的其它请求参数错误）。
//
// 实测响应（2026-10-10）：
//
//	400 {"error":{"message":"Model \"xxx\" is not supported on this endpoint.",
//	"type":"invalid_request_error","param":"model","code":"unsupported_model"}}
//
// 错误码为 unsupported_model 但 param/消息并不指向模型时，说明是请求参数级失败，
// 不能按模型能力反馈冷却模型。
func isCommandCodeUnsupportedModelError(body []byte) bool {
	if !strings.EqualFold(strings.TrimSpace(extractUpstreamErrorCode(body)), "unsupported_model") {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(gjson.GetBytes(body, "error.param").String()), "model") {
		return true
	}
	message := strings.ToLower(strings.TrimSpace(extractUpstreamErrorMessage(body)))
	return strings.Contains(message, "model") && strings.Contains(message, "not supported")
}

// commandCodeModelNotInPlanMarker 是 Command Code「模型不在当前套餐」错误的稳定标识。
//
// 实测响应（2026-10-10）：
//
//	403 {"error":{"message":"MODEL_NOT_IN_PLAN: GPT-5.4 available in Pro and above
//	plans or extra on demand usage","type":"permission_error","code":"FORBIDDEN"}}
//
// 该错误只说明当前账号的套餐不含该模型，账号本身完全可用；按模型级冷却即可。
const commandCodeModelNotInPlanMarker = "model_not_in_plan"

func isCommandCodeModelNotInPlanError(statusCode int, body []byte) bool {
	if statusCode != http.StatusForbidden {
		return false
	}
	message := strings.ToLower(strings.TrimSpace(extractUpstreamErrorMessage(body)))
	if strings.Contains(message, commandCodeModelNotInPlanMarker) {
		return true
	}
	// 文案变体兜底：描述模型在某个套餐档位可用。
	return strings.Contains(message, "available in") && strings.Contains(message, "plans")
}

func isOpenCodeGoModelUnsupportedError(statusCode int, body []byte) bool {
	if statusCode != http.StatusUnauthorized && statusCode != http.StatusForbidden {
		return false
	}
	message := normalizeModelNotFoundBody([]byte(extractUpstreamErrorMessage(body)))
	return strings.Contains(message, "model") && strings.Contains(message, "not supported")
}

func isModelNotFoundError(statusCode int, body []byte) bool {
	return isUpstreamModelNotFoundError(statusCode, body) || statusCode == http.StatusNotFound
}

// openAICodexPlanGatedModelPhrase matches the deterministic Codex 400 returned
// when a ChatGPT OAuth account's plan cannot serve the requested model, e.g.
// {"detail":"The 'gpt-5.6-sol' model is not supported when using Codex with a ChatGPT account."}
// The phrase is compared against the normalized body (lowercased, "_"/"-"
// folded to spaces), so it also matches the same message embedded in
// error.message-style payloads.
const openAICodexPlanGatedModelPhrase = "model is not supported when using codex"

// isOpenAICodexPlanGatedModelError reports whether the upstream response is the
// deterministic Codex rejection of a plan-gated model on a ChatGPT account.
// Unlike transient failures, retrying the same account cannot succeed until the
// account's plan changes, so callers should treat it like model-not-found and
// cool the (account, model) pair down instead of re-selecting the account.
func isOpenAICodexPlanGatedModelError(statusCode int, body []byte) bool {
	if statusCode != http.StatusBadRequest {
		return false
	}
	normalized := normalizeModelNotFoundBody(body)
	if normalized == "" {
		return false
	}
	return strings.Contains(normalized, openAICodexPlanGatedModelPhrase)
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

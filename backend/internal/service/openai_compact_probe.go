package service

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	AccountTestModeDefault = "default"
	// AccountTestModeCompact drives the remote-compaction probe test
	// (native v2: streaming /responses with a compaction_trigger input item).
	AccountTestModeCompact = "compact"
)

func normalizeAccountTestMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case AccountTestModeCompact:
		return AccountTestModeCompact
	default:
		return AccountTestModeDefault
	}
}

// createOpenAICompactProbePayload 构造原生 remote compaction v2 探测载荷：
// 流式 /responses + input 末尾 {"type":"compaction_trigger"}。上游已下线
// legacy unary /responses/compact（v1 形态恒 404，#5598/#5624），现行 codex
// 默认协议即 v2（RemoteCompactionV2 Stable + default_enabled）。
func createOpenAICompactProbePayload(model string, isOAuth bool) map[string]any {
	payload := map[string]any{
		"model":        strings.TrimSpace(model),
		"instructions": "You are a helpful coding assistant.",
		"input": []any{
			map[string]any{
				"type":    "message",
				"role":    "user",
				"content": "Respond with OK.",
			},
			map[string]any{"type": "compaction_trigger"},
		},
		"stream": true,
	}
	// ChatGPT internal API 要求 store: false，与真实转发一致。
	if isOAuth {
		payload["store"] = false
	}
	return payload
}

// openAICompactProbeFoundCompactionItem 判定探测响应是否产出了 compaction
// 输出 item，缺少该 item 时 Codex 会报 "got 0 items"。兼容 SSE item、
// response.completed.response.output[] 与整体 JSON output[] 三种形态。
func openAICompactProbeFoundCompactionItem(body []byte) bool {
	if len(body) == 0 {
		return false
	}
	bodyText := string(body)
	if _, found := findRawCompactionItemFromSSE(bodyText); found {
		return true
	}
	if finalResponse, ok := extractCodexFinalResponse(bodyText); ok &&
		responsesOutputHasCompactionItem(finalResponse) {
		return true
	}
	return responsesOutputHasCompactionItem(body)
}

func shouldMarkOpenAICompactUnsupported(status int, body []byte) bool {
	switch status {
	case http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusNotImplemented:
		return true
	case http.StatusBadRequest, http.StatusForbidden, http.StatusUnprocessableEntity:
		lower := strings.ToLower(strings.TrimSpace(extractUpstreamErrorMessage(body) + " " + string(body)))
		if strings.Contains(lower, "compact") {
			for _, keyword := range []string{"unsupported", "not support", "does not support", "not available", "disabled"} {
				if strings.Contains(lower, keyword) {
					return true
				}
			}
		}
	}
	return false
}

// buildOpenAICompactProbeExtraUpdates 计算探测结果的账号 extra 更新。
// 2xx 但无 compaction item 时同样记为不支持；账号级 force_on 仍可覆盖。
func buildOpenAICompactProbeExtraUpdates(resp *http.Response, body []byte, probeErr error, compactionFound bool, now time.Time) map[string]any {
	updates := map[string]any{
		"openai_compact_checked_at":  now.Format(time.RFC3339),
		"openai_compact_last_status": nil,
	}
	if resp != nil {
		updates["openai_compact_last_status"] = resp.StatusCode
	}

	switch {
	case probeErr != nil:
		updates["openai_compact_last_error"] = truncateString(sanitizeUpstreamErrorMessage(probeErr.Error()), 2048)
	case resp == nil:
		updates["openai_compact_last_error"] = "compact probe failed"
	default:
		errMsg := strings.TrimSpace(extractUpstreamErrorMessage(body))
		if errMsg == "" && len(body) > 0 {
			errMsg = strings.TrimSpace(string(body))
		}
		if errMsg == "" && (resp.StatusCode < 200 || resp.StatusCode >= 300) {
			errMsg = "HTTP " + strconv.Itoa(resp.StatusCode)
		}
		errMsg = truncateString(sanitizeUpstreamErrorMessage(errMsg), 2048)
		switch {
		case resp.StatusCode >= 200 && resp.StatusCode < 300 && compactionFound:
			updates["openai_compact_supported"] = true
			updates["openai_compact_last_error"] = ""
		case resp.StatusCode >= 200 && resp.StatusCode < 300:
			updates["openai_compact_supported"] = false
			updates["openai_compact_last_error"] = "upstream returned 2xx without a compaction output item (native remote compaction v2 unsupported)"
		default:
			if shouldMarkOpenAICompactUnsupported(resp.StatusCode, body) {
				updates["openai_compact_supported"] = false
			}
			updates["openai_compact_last_error"] = errMsg
		}
	}
	return updates
}

func mergeExtraUpdates(base map[string]any, more map[string]any) map[string]any {
	if len(base) == 0 && len(more) == 0 {
		return nil
	}
	out := make(map[string]any, len(base)+len(more))
	for key, value := range base {
		out[key] = value
	}
	for key, value := range more {
		out[key] = value
	}
	return out
}

// compactProbeSessionID 返回账号级稳定、不可直接识别为探测流量的 UUIDv4 标识。
func compactProbeSessionID(accountID int64) string {
	if accountID <= 0 {
		return deriveStableUUIDv4("sub2api:codex-compact-probe:v1:anonymous")
	}
	return deriveStableUUIDv4("sub2api:codex-compact-probe:v1:" + strconv.FormatInt(accountID, 10))
}

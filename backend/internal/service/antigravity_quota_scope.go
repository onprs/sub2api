package service

import (
	"context"
	"strings"
	"time"
)

func normalizeAntigravityModelName(model string) string {
	normalized := strings.ToLower(strings.TrimSpace(model))
	if idx := strings.LastIndex(normalized, "/publishers/google/models/"); idx != -1 {
		normalized = normalized[idx+len("/publishers/google/models/"):]
	} else if idx := strings.LastIndex(normalized, "/publishers/anthropic/models/"); idx != -1 {
		normalized = normalized[idx+len("/publishers/anthropic/models/"):]
	} else if idx := strings.LastIndex(normalized, "/models/"); idx != -1 {
		normalized = normalized[idx+len("/models/"):]
	} else {
		normalized = strings.TrimPrefix(normalized, "publishers/google/models/")
		normalized = strings.TrimPrefix(normalized, "publishers/anthropic/models/")
		normalized = strings.TrimPrefix(normalized, "models/")
	}
	return normalized
}

// resolveAntigravityModelKey 根据请求的模型名解析限流 key
// 返回空字符串表示无法解析
func resolveAntigravityModelKey(requestedModel string) string {
	return normalizeAntigravityModelName(requestedModel)
}

// GetRateLimitRemainingTime 获取限流剩余时间（模型级限流）
// 返回 0 表示未限流或已过期
func (a *Account) GetRateLimitRemainingTime(requestedModel string) time.Duration {
	return a.GetRateLimitRemainingTimeWithContext(context.Background(), requestedModel)
}

// hasAntigravityQuotaRateLimit 报告当前请求的模型冷却是否由明确的配额耗尽产生。
// 只有这类冷却可以触发 AI Credits 超量重试；普通 429 限流不是配额问题。
func (a *Account) hasAntigravityQuotaRateLimit(ctx context.Context, requestedModel string) bool {
	if a == nil {
		return false
	}
	for _, key := range a.modelRateLimitKeysForRequest(ctx, requestedModel) {
		if a.isRateLimitActiveForKey(key) && a.modelRateLimitReason(key) == antigravityRateLimitReasonQuotaExhausted {
			return true
		}
	}
	return false
}

// hasAntigravityHardModelBlock 报告当前模型是否存在不可用或非配额限流类冷却。
// 这类冷却不能通过 AI Credits 超量请求绕过。
func (a *Account) hasAntigravityHardModelBlock(ctx context.Context, requestedModel string) bool {
	if a == nil {
		return false
	}
	for _, key := range a.modelRateLimitKeysForRequest(ctx, requestedModel) {
		if !a.isRateLimitActiveForKey(key) {
			continue
		}
		switch a.modelRateLimitReason(key) {
		case upstreamModelNotFoundReason,
			antigravityRateLimitReasonRateLimited,
			antigravityRateLimitReasonInternal500:
			return true
		}
	}
	return false
}

// antigravityCooldownKeysForAttempt 返回当前请求的冷却检查 key 集合（已去重）：
// 既包含按 requestedModel 解析出的映射/家族池 key，也直接包含 resolvedModel（如具体思考档位）及其基名，
// 避免二次映射自定义映射链。
func (a *Account) antigravityCooldownKeysForAttempt(ctx context.Context, requestedModel, resolvedModel string) []string {
	if a == nil {
		return nil
	}
	baseKeys := a.modelRateLimitKeysForRequest(ctx, requestedModel)
	resolved := strings.TrimSpace(resolvedModel)
	if resolved == "" {
		return baseKeys
	}
	extraKeys := antigravityModelRateLimitKeys(a, resolved)
	if len(extraKeys) == 0 {
		return baseKeys
	}
	seen := make(map[string]struct{}, len(baseKeys)+len(extraKeys))
	merged := make([]string, 0, len(baseKeys)+len(extraKeys))
	for _, key := range baseKeys {
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		merged = append(merged, key)
	}
	for _, key := range extraKeys {
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		merged = append(merged, key)
	}
	return merged
}

func (a *Account) hasAntigravityQuotaRateLimitForAttempt(ctx context.Context, requestedModel, resolvedModel string) bool {
	if a == nil {
		return false
	}
	for _, key := range a.antigravityCooldownKeysForAttempt(ctx, requestedModel, resolvedModel) {
		if a.isRateLimitActiveForKey(key) && a.modelRateLimitReason(key) == antigravityRateLimitReasonQuotaExhausted {
			return true
		}
	}
	return false
}

func (a *Account) hasAntigravityHardModelBlockForAttempt(ctx context.Context, requestedModel, resolvedModel string) bool {
	if a == nil {
		return false
	}
	for _, key := range a.antigravityCooldownKeysForAttempt(ctx, requestedModel, resolvedModel) {
		if !a.isRateLimitActiveForKey(key) {
			continue
		}
		switch a.modelRateLimitReason(key) {
		case upstreamModelNotFoundReason,
			antigravityRateLimitReasonRateLimited,
			antigravityRateLimitReasonInternal500:
			return true
		}
	}
	return false
}

func (a *Account) hasAntigravityModelNotFoundForAttempt(ctx context.Context, requestedModel, resolvedModel string) bool {
	if a == nil {
		return false
	}
	for _, key := range a.antigravityCooldownKeysForAttempt(ctx, requestedModel, resolvedModel) {
		if a.isRateLimitActiveForKey(key) && a.modelRateLimitReason(key) == upstreamModelNotFoundReason {
			return true
		}
	}
	return false
}

func (a *Account) antigravityRateLimitRemainingForAttempt(ctx context.Context, requestedModel, resolvedModel string) (bool, time.Duration) {
	if a == nil {
		return false, 0
	}
	limited := false
	maxRemaining := time.Duration(0)
	for _, key := range a.antigravityCooldownKeysForAttempt(ctx, requestedModel, resolvedModel) {
		if a.isRateLimitActiveForKey(key) {
			limited = true
			if rem := a.getRateLimitRemainingForKey(key); rem > maxRemaining {
				maxRemaining = rem
			}
		}
	}
	return limited, maxRemaining
}

// GetRateLimitRemainingTimeWithContext 获取限流剩余时间（模型级限流）
// 返回 0 表示未限流或已过期
func (a *Account) GetRateLimitRemainingTimeWithContext(ctx context.Context, requestedModel string) time.Duration {
	if a == nil {
		return 0
	}
	return a.GetModelRateLimitRemainingTimeWithContext(ctx, requestedModel)
}

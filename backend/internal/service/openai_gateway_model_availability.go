package service

import (
	"context"
	"strings"
)

// DiagnoseModelAvailabilityForPlatform reports whether the requested model
// is configured to be served by any OpenAI-compatible account in the group
// for the given platform (e.g. PlatformOpenAI, PlatformGrok). The platform
// scopes the candidate pool so distinct OpenAI-compatible platforms do not
// cross-contaminate diagnosis results.
//
// Safe to call on the error path: returns {true,true} on any internal
// failure or when the inputs preclude meaningful diagnosis (empty model,
// nil service), so callers stay on the 503 fallback branch.
func (s *OpenAIGatewayService) DiagnoseModelAvailabilityForPlatform(
	ctx context.Context,
	groupID *int64,
	requestedModel string,
	platform string,
) ModelAvailabilityDiagnosis {
	if s == nil {
		return ModelAvailabilityDiagnosis{HasAccountsInPool: true, HasModelSupport: true}
	}
	requestedModel = strings.TrimSpace(requestedModel)
	if requestedModel == "" {
		return ModelAvailabilityDiagnosis{HasAccountsInPool: true, HasModelSupport: true}
	}
	if groupID != nil {
		mapping, _ := s.ResolveChannelMappingAndRestrict(ctx, groupID, requestedModel)
		if mapping.Mapped && strings.TrimSpace(mapping.MappedModel) != "" {
			requestedModel = strings.TrimSpace(mapping.MappedModel)
		}
	}

	accounts, err := s.listSchedulableAccounts(ctx, groupID, platform)
	if err != nil {
		// Conservative fallback so the caller keeps returning 503; we do not
		// want a transient lookup failure to flip into 404 model_not_found.
		return ModelAvailabilityDiagnosis{HasAccountsInPool: true, HasModelSupport: true}
	}

	diag := ModelAvailabilityDiagnosis{}
	for i := range accounts {
		diag.HasAccountsInPool = true
		// 显式映射仍可声明包含空白的兼容别名；空映射仅对格式正常的
		// 自定义 ID 保持“允许全部”。带内部空白的值通常是客户端误传的
		// display_name，不能在错误诊断时据此把模型判为可用。
		if len(accounts[i].GetExplicitModelMapping()) == 0 && openAIModelIDContainsWhitespace(requestedModel) {
			continue
		}
		if accounts[i].IsModelSupported(requestedModel) {
			diag.HasModelSupport = true
			return diag
		}
	}
	return diag
}

func openAIModelIDContainsWhitespace(model string) bool {
	return strings.ContainsAny(model, " \t\r\n")
}

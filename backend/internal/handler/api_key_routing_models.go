package handler

import (
	"context"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func dynamicAPIKeyAvailableModels(ctx context.Context, gateway *service.GatewayService, apiKey *service.APIKey, platform string) []string {
	if gateway == nil || apiKey == nil {
		return nil
	}
	bindings := apiKey.ConfiguredRoutingGroups()
	if len(bindings) == 0 {
		return nil
	}

	var merged []string
	for _, binding := range bindings {
		group := binding.Group
		if group == nil || !group.IsActive() || group.Platform != apiKey.RoutingPlatformValue() {
			continue
		}
		groupPlatform := group.Platform
		if strings.TrimSpace(platform) != "" {
			groupPlatform = strings.TrimSpace(platform)
		}
		groupID := group.ID
		var available []string
		if groupPlatform == service.PlatformComposite {
			available = compositeAvailableModelsForGroup(ctx, gateway, &groupID)
		} else {
			available = gateway.GetAvailableModels(ctx, &groupID, groupPlatform)
		}
		fallback := defaultModelIDsForPlatform(groupPlatform)
		if group.CustomModelsListEnabled() {
			available = filterModelsByCustomList(customModelsListSource(groupPlatform, available, fallback), fallback, group.ModelsListConfig.Models)
		} else if len(available) == 0 {
			available = fallback
		}
		merged = mergeModelIDs(merged, available)
	}
	return merged
}

func compositeAvailableModelsForGroup(ctx context.Context, gateway *service.GatewayService, groupID *int64) []string {
	if gateway == nil {
		return nil
	}
	seen := make(map[string]struct{})
	models := make([]string, 0)
	schedulablePlatforms := gateway.GetSchedulablePlatforms(ctx, groupID)
	for _, platform := range []string{service.PlatformAnthropic, service.PlatformGemini, service.PlatformOpenAI, service.PlatformAntigravity, service.PlatformGrok} {
		platformModels := gateway.GetAvailableModels(ctx, groupID, platform)
		if len(platformModels) == 0 {
			if _, ok := schedulablePlatforms[platform]; ok {
				platformModels = defaultModelIDsForPlatform(platform)
			}
		}
		for _, model := range platformModels {
			model = strings.TrimSpace(model)
			if model == "" {
				continue
			}
			if _, ok := seen[model]; ok {
				continue
			}
			seen[model] = struct{}{}
			models = append(models, model)
		}
	}
	return models
}

func dynamicAPIKeyAntigravityMappedModels(ctx context.Context, gateway *service.GatewayService, apiKey *service.APIKey, protocol string) []string {
	if gateway == nil || apiKey == nil {
		return nil
	}
	var merged []string
	for _, binding := range apiKey.ConfiguredRoutingGroups() {
		if binding.Group == nil || !binding.Group.IsActive() {
			continue
		}
		groupID := binding.GroupID
		merged = mergeModelIDs(merged, gateway.GetAntigravityMappedModels(ctx, &groupID, protocol))
	}
	return merged
}

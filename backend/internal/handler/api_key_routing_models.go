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
		available := gateway.GetAvailableModels(ctx, &groupID, groupPlatform)
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

package domain

import "strings"

// AntigravityReasoningModel 只聚合已核实的 Gemini 档位，不推断未知模型或自定义别名。
func AntigravityReasoningModel(model string) (string, string) {
	model = strings.TrimPrefix(strings.TrimSpace(model), "models/")
	for _, base := range []string{"gemini-3.8-flash", "gemini-3.7-flash", "gemini-3.6-flash", "gemini-3.5-flash", "gemini-3.1-pro"} {
		if model == base {
			return base, ""
		}
		for _, effort := range []string{"high", "medium", "low"} {
			if base == "gemini-3.1-pro" && effort == "medium" {
				continue
			}
			if model == base+"-"+effort {
				return base, effort
			}
		}
	}
	return "", ""
}

func AntigravityPublicModelID(model string) string {
	if base, _ := AntigravityReasoningModel(model); base != "" {
		return base
	}
	return model
}

// ResolveAntigravityReasoningRoute 将思考程度选为实际档位；默认 high，旧后缀仅在未传程度时生效。
func ResolveAntigravityReasoningRoute(model, effort string) (AntigravityModelRoute, bool) {
	base, legacyEffort := AntigravityReasoningModel(model)
	if base == "" {
		return AntigravityModelRoute{}, false
	}
	if effort == "" {
		effort = legacyEffort
	}
	if effort == "" {
		effort = "high"
	}
	for _, route := range antigravityUserModelRoutes {
		if route.ModelID == base+"-"+effort {
			return route, true
		}
	}
	return AntigravityModelRoute{}, false
}

// AntigravityPublicModelRoutes 的公开身份不包含档位，具体 wire 由每次请求选择。
func AntigravityPublicModelRoutes() []AntigravityModelRoute {
	var routes []AntigravityModelRoute
	seen := make(map[string]bool)
	for _, route := range AntigravityUserModelRoutes() {
		base, effort := AntigravityReasoningModel(route.ModelID)
		if base != "" {
			if seen[base] {
				continue
			}
			seen[base] = true
			route.ModelID = base
			route.DisplayName = strings.TrimSuffix(route.DisplayName, " ("+strings.ToUpper(effort[:1])+effort[1:]+")")
			route.WireModel = ""
			route.InternalModel = ""
			route.ResponseModel = base
			route.HasThinkingBudget = false
		}
		routes = append(routes, route)
	}
	return routes
}

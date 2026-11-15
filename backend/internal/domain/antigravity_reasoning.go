package domain

import "strings"

// antigravityReasoningModelTiers 是按物理模型族列出的可聚合思考档位白名单。
// 档位顺序沿用 Antigravity 目录（默认 high 在前），只列已核实的档位；
// 未列出的档位不得推断为该模型可用。Claude 的 max 档在当前用户目录未展示，
// 但物理 wire ID 仍可路由，因此保留在兼容档位中。
var antigravityReasoningModelTiers = []struct {
	base    string
	efforts []string
}{
	{"gemini-4-carbon", []string{"high", "medium", "low"}},
	{"gemini-4-hydrogen", []string{"high", "xhigh", "medium", "low"}},
	{"gemini-4-argon", []string{"high", "medium", "low"}},
	{"gemini-3.8-flash", []string{"high", "medium", "low"}},
	{"gemini-3.7-flash", []string{"high", "medium", "low"}},
	{"gemini-3.6-flash", []string{"high", "medium", "low"}},
	{"gemini-3.5-flash", []string{"high", "medium", "low"}},
	{"gemini-3.1-pro", []string{"high", "low"}},
	{"claude-opus-5-5", []string{"high", "max", "medium", "low"}},
	{"claude-sonnet-5-5", []string{"high", "max", "medium", "low"}},
}

// AntigravityReasoningModel 只聚合已核实的档位，不推断未知模型或自定义别名。
func AntigravityReasoningModel(model string) (string, string) {
	model = strings.TrimPrefix(strings.TrimSpace(model), "models/")
	for _, def := range antigravityReasoningModelTiers {
		if model == def.base {
			return def.base, ""
		}
		for _, effort := range def.efforts {
			if model == def.base+"-"+effort {
				return def.base, effort
			}
		}
	}
	return "", ""
}

// normalizeAntigravityReasoningEffort 归一化常见外部写法，不改变未知值。
func normalizeAntigravityReasoningEffort(effort string) string {
	effort = strings.ToLower(strings.TrimSpace(effort))
	switch effort {
	case "extra-high", "extra_high", "extra high", "x-high":
		return "xhigh"
	case "maximum":
		return "max"
	}
	return effort
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
	effort = normalizeAntigravityReasoningEffort(effort)
	if effort == "" {
		effort = "high"
	}
	// Claude API 的 xhigh 在 Antigravity 用户目录中没有独立档位，物理最高档为 max。
	if effort == "xhigh" && (base == "claude-opus-5-5" || base == "claude-sonnet-5-5") {
		effort = "max"
	}
	for _, def := range antigravityReasoningModelTiers {
		if def.base != base {
			continue
		}
		supported := false
		for _, candidate := range def.efforts {
			if candidate == effort {
				supported = true
				break
			}
		}
		if !supported {
			return AntigravityModelRoute{}, false
		}
		break
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
		if route.Legacy {
			continue
		}
		base, _ := AntigravityReasoningModel(route.ModelID)
		if base != "" {
			if seen[base] {
				continue
			}
			seen[base] = true
			route.ModelID = base
			route.DisplayName = antigravityBaseDisplayName(route.DisplayName)
			route.WireModel = ""
			route.InternalModel = ""
			route.ResponseModel = base
			route.HasThinkingBudget = false
		}
		routes = append(routes, route)
	}
	return routes
}

// antigravityBaseDisplayName 去掉档位后缀的展示名，兼容 High、XHigh 和 Max。
func antigravityBaseDisplayName(name string) string {
	if index := strings.Index(name, " ("); index > 0 {
		return name[:index]
	}
	return name
}
